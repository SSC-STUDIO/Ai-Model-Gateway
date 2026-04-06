// Package adminapi assembles the v2 admin management API routes.
// Routes are mounted at /api/admin/v2/* with the admin frontend at /admin/*.
package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/proxy"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/i18n"
	"ai-model-gateway/internal/infra/auth"
	"ai-model-gateway/internal/infra/telemetrydb"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

var adminSSRFChecker = proxy.NewSSRFChecker()

// Deps groups the dependencies the admin API needs.
type Deps struct {
	Auth      *auth.Authenticator
	Store     *telemetrydb.Store
	Selector  core.RouteSelector
	GetConfig func() *core.Config

	// Optional hook to expose pricing/economics data on /data.
	PricingEconomics func() (interface{}, error)

	// Optional runtime hooks for config history and rollback functionality.
	ConfigExport      func() ([]byte, error)
	ConfigSave        func(payload json.RawMessage) (interface{}, error)
	ConfigHistory     func() (interface{}, error)
	ConfigHistoryDiff func(versionID string) (interface{}, error)
	ConfigRollback    func(versionID string) (interface{}, error)

	// Optional hook for custom upstream probe behavior.
	UpstreamTest func(upstream core.Provider, health core.HealthCheckConfig) (interface{}, int, error)

	// I18n bundle for translated error messages
	I18n *i18n.Bundle
}

// Mount registers admin API and frontend routes on the given router.
func Mount(r chi.Router, d Deps) {
	if d.Auth == nil {
		return
	}

	r.Route("/api/admin/v2", func(api chi.Router) {
		// Public auth endpoints
		api.Post("/auth/login", loginHandler(d))
		api.Post("/auth/logout", logoutHandler(d.Auth))

		// Protected endpoints
		api.Group(func(p chi.Router) {
			p.Use(requireAuth(d.Auth, d.I18n))
			p.Use(sameOriginCookieWriteProtection(d.I18n))
			p.Get("/overview", overviewHandler(d))
			p.Get("/data", dataHandler(d))
			p.Get("/timeseries", timeseriesHandler(d))
			p.Get("/models", modelsHandler(d))
			p.Get("/config", configHandler(d))
			p.Put("/config", configSaveHandler(d))
			p.Get("/config/export", configExportHandler(d))
			p.Get("/config/history", configHistoryHandler(d))
			p.Get("/config/history/{version_id}/diff", configHistoryDiffHandler(d))
			p.Post("/config/rollback", configRollbackHandler(d))
			p.Post("/upstreams/test", upstreamsTestHandler(d))
			p.Get("/logs/stream", logsStreamHandler(d, DefaultLogStreamManager))
			p.Get("/logs/export", logsExportHandler(d, DefaultLogStreamManager))
			p.Get("/models/benchmark", modelsBenchmarkHandler(d))
		})
	})

	// Admin frontend (serve local dist when available, otherwise fallback page).
	adminHandler, adminAssetsHandler := adminFrontendHandlers()
	r.Get("/admin", adminHandler)
	r.Get("/admin/*", func(w http.ResponseWriter, r *http.Request) {
		adminAssetsHandler.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func requireAuth(a *auth.Authenticator, bundle *i18n.Bundle) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := a.Authenticate(r); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				msg := "unauthorized"
				if bundle != nil {
					msg = bundle.T(i18n.ErrUnauthorized)
				}
				json.NewEncoder(w).Encode(map[string]string{"error": msg})
				return
			}
			authMode := requestAuthMode(r)
			ctx := context.WithValue(r.Context(), authModeContextKey{}, authMode)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type requestAuthKind int

const (
	requestAuthUnknown requestAuthKind = iota
	requestAuthBearer
	requestAuthCookie
)

type authModeContextKey struct{}

func requestAuthMode(r *http.Request) requestAuthKind {
	if bearerToken(r) != "" {
		return requestAuthBearer
	}
	if _, err := r.Cookie(auth.CookieName); err == nil {
		return requestAuthCookie
	}
	return requestAuthUnknown
}

func sameOriginCookieWriteProtection(bundle *i18n.Bundle) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAdminMutation(r) {
				next.ServeHTTP(w, r)
				return
			}

			authMode, _ := r.Context().Value(authModeContextKey{}).(requestAuthKind)
			if authMode != requestAuthCookie {
				next.ServeHTTP(w, r)
				return
			}
			if sameOriginAdminRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			msg := "admin same-origin write required"
			if bundle != nil {
				msg = bundle.T(i18n.ErrForbidden)
			}
			writeJSON(w, http.StatusForbidden, map[string]string{"error": msg})
		})
	}
}

func isAdminMutation(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch {
	case r.Method == http.MethodPut && r.URL.Path == "/api/admin/v2/config":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/admin/v2/config/rollback":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/admin/v2/upstreams/test":
		return true
	}
	return false
}

func sameOriginAdminRequest(r *http.Request) bool {
	if sameOriginRequestURL(r.Host, r.Header.Get("Origin")) {
		return true
	}
	return sameOriginRequestURL(r.Host, r.Header.Get("Referer"))
}

func sameOriginRequestURL(host, raw string) bool {
	host = strings.TrimSpace(host)
	raw = strings.TrimSpace(raw)
	if host == "" || raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(parsed.Host, host) {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func bearerToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "bearer "
	if len(authHeader) < len(prefix) {
		return ""
	}
	if !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(authHeader[len(prefix):])
}

// ---------------------------------------------------------------------------
// Auth handlers
// ---------------------------------------------------------------------------

func loginHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			msg := "invalid request body"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidRequestBody)
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		if err := d.Auth.Login(w, body.Token); err != nil {
			msg := "invalid token"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidToken)
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": msg})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func logoutHandler(a *auth.Authenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.Logout(w)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// Data handlers
// ---------------------------------------------------------------------------

// overviewHandler returns a dashboard overview with windowed metrics.
func overviewHandler(d Deps) http.HandlerFunc {
	type windowMetrics struct {
		Requests     int     `json:"requests"`
		Successes    int     `json:"successes"`
		Failures     int     `json:"failures"`
		AvgLatencyMs float64 `json:"avg_latency_ms"`
	}
	type runtimeView struct {
		RouterStrategy       string `json:"router_strategy"`
		HealthEnabled        bool   `json:"health_enabled"`
		HealthPath           string `json:"health_path"`
		StickySessions       bool   `json:"sticky_sessions_enabled"`
		BridgeEnabled        bool   `json:"bridge_enabled"`
		ProviderCount        int    `json:"provider_count"`
		EnabledProviderCount int    `json:"enabled_provider_count"`
	}
	type overview struct {
		Last1m          windowMetrics `json:"last_1m"`
		Last5m          windowMetrics `json:"last_5m"`
		Last1h          windowMetrics `json:"last_1h"`
		Last24h         windowMetrics `json:"last_24h"`
		Runtime         runtimeView   `json:"runtime"`
		AvailableModels []string      `json:"available_models"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		cfg := &core.Config{}
		if d.GetConfig != nil {
			if current := d.GetConfig(); current != nil {
				cfg = current
			}
		}
		enabledProviders := 0
		for _, provider := range cfg.Providers {
			if provider.IsEnabled() {
				enabledProviders++
			}
		}
		query := func(window time.Duration) windowMetrics {
			reqs, succ, fail, avg := d.Store.QueryWindowMetrics(window)
			return windowMetrics{
				Requests:     reqs,
				Successes:    succ,
				Failures:     fail,
				AvgLatencyMs: avg,
			}
		}

		data := overview{
			Last1m:  query(time.Minute),
			Last5m:  query(5 * time.Minute),
			Last1h:  query(time.Hour),
			Last24h: query(24 * time.Hour),
			Runtime: runtimeView{
				RouterStrategy:       cfg.Routing.Strategy,
				HealthEnabled:        cfg.Routing.Health.Enabled,
				HealthPath:           cfg.Routing.Health.Path,
				StickySessions:       cfg.Routing.StickySessions.Enabled,
				BridgeEnabled:        cfg.Compat.Bridge.Enabled,
				ProviderCount:        len(cfg.Providers),
				EnabledProviderCount: enabledProviders,
			},
			AvailableModels: d.Selector.ListModels(),
		}
		writeJSON(w, http.StatusOK, data)
	}
}

// dataHandler returns the latest telemetry rows plus grouped summaries.
func dataHandler(d Deps) http.HandlerFunc {
	type summary struct {
		Requests     int     `json:"requests"`
		Successes    int     `json:"successes"`
		Failures     int     `json:"failures"`
		AvgLatencyMs float64 `json:"avg_latency_ms"`
	}
	type groupedSummary struct {
		Value        string  `json:"value"`
		Requests     int     `json:"requests"`
		Successes    int     `json:"successes"`
		Failures     int     `json:"failures"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		AvgLatencyMs float64 `json:"avg_latency_ms"`
	}
	convertGroups := func(rows []telemetrydb.GroupSummary) []groupedSummary {
		result := make([]groupedSummary, len(rows))
		for i, row := range rows {
			result[i] = groupedSummary{
				Value:        row.GroupValue,
				Requests:     row.Requests,
				Successes:    row.Successes,
				Failures:     row.Failures,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				AvgLatencyMs: row.AvgLatencyMs,
			}
		}
		return result
	}

	return func(w http.ResponseWriter, r *http.Request) {
		windowHours := parsePositiveInt(r.URL.Query().Get("hours"), 24, 1, 24*7)
		limit := parsePositiveInt(r.URL.Query().Get("limit"), 50, 1, 500)
		window := time.Duration(windowHours) * time.Hour
		cfg := &core.Config{}
		if d.GetConfig != nil {
			if current := d.GetConfig(); current != nil {
				cfg = current
			}
		}

		reqs, succ, fail, avg := d.Store.QueryWindowMetrics(window)
		payload := map[string]interface{}{
			"window_hours":     windowHours,
			"available_models": d.Selector.ListModels(),
			"summary": summary{
				Requests:     reqs,
				Successes:    succ,
				Failures:     fail,
				AvgLatencyMs: avg,
			},
			"runtime": map[string]interface{}{
				"router_strategy":         cfg.Routing.Strategy,
				"health_enabled":          cfg.Routing.Health.Enabled,
				"health_path":             cfg.Routing.Health.Path,
				"sticky_sessions_enabled": cfg.Routing.StickySessions.Enabled,
				"bridge_enabled":          cfg.Compat.Bridge.Enabled,
				"provider_count":          len(cfg.Providers),
			},
			"requests":  d.Store.QueryRecentRequests(limit),
			"errors":    d.Store.QueryRecentErrors(limit),
			"models":    convertGroups(d.Store.QueryModelSummaries(window, limit)),
			"upstreams": convertGroups(d.Store.QueryUpstreamSummaries(window, limit)),
		}
		if d.PricingEconomics != nil {
			if economics, err := d.PricingEconomics(); err == nil && economics != nil {
				payload["pricing_economics"] = economics
			}
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// timeseriesHandler returns bucketed telemetry metrics for charting.
func timeseriesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hours := parsePositiveInt(r.URL.Query().Get("hours"), 24, 1, 24*7)
		bucketMinutes := parsePositiveInt(r.URL.Query().Get("bucket"), 60, 1, 24*60)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"window_hours":   hours,
			"bucket_minutes": bucketMinutes,
			"points": d.Store.QueryTimeSeriesBuckets(
				time.Duration(hours)*time.Hour,
				time.Duration(bucketMinutes)*time.Minute,
			),
		})
	}
}

// modelsHandler returns all routable models and provider count.
func modelsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		models := d.Selector.ListModels()
		cfg := d.GetConfig()
		enabledProviders := 0
		for _, p := range cfg.Providers {
			if p.IsEnabled() {
				enabledProviders++
			}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"models":            models,
			"model_count":       len(models),
			"provider_count":    len(cfg.Providers),
			"enabled_providers": enabledProviders,
		})
	}
}

// configHandler returns the current (sanitised) config without API keys.
func configHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := d.GetConfig()
		writeJSON(w, http.StatusOK, sanitizedConfigView(cfg))
	}
}

func configExportHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			data []byte
			err  error
		)
		if d.ConfigExport != nil {
			data, err = d.ConfigExport()
		} else if d.GetConfig != nil {
			data, err = yaml.Marshal(sanitizedConfigExportView(d.GetConfig()))
		} else {
			msg := "config export is not available"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrConfigExportUnavailable)
			}
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": msg})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Content-Disposition", `attachment; filename="config.v2.export.yaml"`)
		_, _ = w.Write(data)
	}
}

func configSaveHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigSave == nil {
			msg := "config save is not available in this runtime"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrConfigSaveUnavailable)
			}
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": msg})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			msg := "invalid config payload"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidConfigPayload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			msg := "invalid config payload"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidConfigPayload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}

		var payload json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			msg := "invalid config payload"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidConfigPayload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}

		response, err := d.ConfigSave(payload)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if response == nil && d.GetConfig != nil {
			response = sanitizedConfigView(d.GetConfig())
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func configHistoryHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigHistory == nil {
			msg := "config history is not available in this runtime"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrConfigHistoryUnavailable)
			}
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": msg})
			return
		}
		payload, err := d.ConfigHistory()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func configHistoryDiffHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigHistoryDiff == nil {
			msg := "config history diff is not available in this runtime"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrConfigDiffUnavailable)
			}
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": msg})
			return
		}
		payload, err := d.ConfigHistoryDiff(chi.URLParam(r, "version_id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func configRollbackHandler(d Deps) http.HandlerFunc {
	type rollbackRequest struct {
		VersionID string `json:"version_id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ConfigRollback == nil {
			msg := "config rollback is not available in this runtime"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrConfigRollbackUnavailable)
			}
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": msg})
			return
		}
		var payload rollbackRequest
		if r.Body != nil {
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&payload); err != nil && err != io.EOF {
				msg := "invalid rollback payload"
				if d.I18n != nil {
					msg = d.I18n.T(i18n.ErrInvalidRollbackPayload)
				}
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
				return
			}
		}
		response, err := d.ConfigRollback(payload.VersionID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if response == nil && d.GetConfig != nil {
			response = sanitizedConfigView(d.GetConfig())
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// modelsBenchmarkHandler returns detailed benchmark metrics for model comparison.
func modelsBenchmarkHandler(d Deps) http.HandlerFunc {
	type benchmarkMetrics struct {
		Model              string  `json:"model"`
		Requests           int     `json:"requests"`
		Successes          int     `json:"successes"`
		Failures           int     `json:"failures"`
		InputTokens        int64   `json:"input_tokens"`
		CachedPromptTokens int64   `json:"cached_prompt_tokens"`
		OutputTokens       int64   `json:"output_tokens"`
		AvgLatencyMs       float64 `json:"avg_latency_ms"`
		P50LatencyMs       float64 `json:"p50_latency_ms"`
		P95LatencyMs       float64 `json:"p95_latency_ms"`
		P99LatencyMs       float64 `json:"p99_latency_ms"`
		MaxLatencyMs       int64   `json:"max_latency_ms"`
		SuccessRate        float64 `json:"success_rate"`
		EstimatedCostUSD   float64 `json:"estimated_cost_usd"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Parse time range parameters
		startTime := r.URL.Query().Get("start_time")
		endTime := r.URL.Query().Get("end_time")
		hours := parsePositiveInt(r.URL.Query().Get("hours"), 24, 1, 24*30)

		// Parse model filter
		modelsParam := r.URL.Query().Get("models")
		var modelFilter []string
		if modelsParam != "" {
			modelFilter = strings.Split(modelsParam, ",")
			for i := range modelFilter {
				modelFilter[i] = strings.TrimSpace(modelFilter[i])
			}
		}

		// Calculate window
		var window time.Duration
		if startTime != "" && endTime != "" {
			start, err1 := time.Parse(time.RFC3339, startTime)
			end, err2 := time.Parse(time.RFC3339, endTime)
			if err1 == nil && err2 == nil && end.After(start) {
				window = end.Sub(start)
			} else {
				window = time.Duration(hours) * time.Hour
			}
		} else {
			window = time.Duration(hours) * time.Hour
		}

		// Query benchmark data
		benchmarks := d.Store.QueryModelBenchmark(window, modelFilter)

		// Convert to response format
		result := make([]benchmarkMetrics, len(benchmarks))
		for i, bm := range benchmarks {
			result[i] = benchmarkMetrics{
				Model:              bm.Model,
				Requests:           bm.Requests,
				Successes:          bm.Successes,
				Failures:           bm.Failures,
				InputTokens:        bm.InputTokens,
				CachedPromptTokens: bm.CachedPromptTokens,
				OutputTokens:       bm.OutputTokens,
				AvgLatencyMs:       bm.AvgLatencyMs,
				P50LatencyMs:       bm.P50LatencyMs,
				P95LatencyMs:       bm.P95LatencyMs,
				P99LatencyMs:       bm.P99LatencyMs,
				MaxLatencyMs:       bm.MaxLatencyMs,
				SuccessRate:        bm.SuccessRate,
				EstimatedCostUSD:   bm.EstimatedCostUSD,
			}
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"window_hours":   hours,
			"start_time":     startTime,
			"end_time":       endTime,
			"model_count":    len(result),
			"models":         d.Selector.ListModels(),
			"benchmarks":     result,
		})
	}
}

func upstreamsTestHandler(d Deps) http.HandlerFunc {
	type upstreamProbeRequest struct {
		Upstream core.Provider `json:"upstream"`
		Provider core.Provider `json:"provider"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var payload upstreamProbeRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			msg := "invalid upstream probe payload"
			if d.I18n != nil {
				msg = d.I18n.T(i18n.ErrInvalidUpstreamProbePayload)
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
			return
		}

		target := payload.Upstream
		if target.BaseURL == "" {
			target = payload.Provider
		}
		healthCfg := core.HealthCheckConfig{}
		if d.GetConfig != nil {
			healthCfg = d.GetConfig().Routing.Health
		}

		var (
			result interface{}
			status int
			err    error
		)
		if d.UpstreamTest != nil {
			result, status, err = d.UpstreamTest(target, healthCfg)
		} else {
			resp := probeUpstream(r.Context(), target, healthCfg)
			result = redactProbeResponse(resp)
			status = http.StatusOK
			if !resp.OK && resp.StatusCode == 0 {
				status = http.StatusBadGateway
			}
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, status, result)
	}
}

type upstreamProbeResponse struct {
	OK          bool   `json:"ok"`
	TargetURL   string `json:"target_url"`
	StatusCode  int    `json:"status_code,omitempty"`
	LatencyMs   int64  `json:"latency_ms"`
	Error       string `json:"error,omitempty"`
	BodyPreview string `json:"body_preview,omitempty"`
	CheckedAt   string `json:"checked_at"`
}

func probeUpstream(ctx context.Context, provider core.Provider, healthCfg core.HealthCheckConfig) upstreamProbeResponse {
	if strings.TrimSpace(provider.BaseURL) == "" {
		return upstreamProbeResponse{
			OK:        false,
			Error:     "base_url is required",
			CheckedAt: time.Now().Format(time.RFC3339),
		}
	}

	path := strings.TrimSpace(healthCfg.Path)
	if path == "" {
		path = "/v1/models"
	}
	targetURL := strings.TrimRight(provider.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	if adminSSRFChecker != nil {
		if err := adminSSRFChecker.ValidateURL(targetURL); err != nil {
			return upstreamProbeResponse{
				OK:        false,
				TargetURL: targetURL,
				Error:     fmt.Sprintf("SSRF validation failed: %v", err),
				CheckedAt: time.Now().Format(time.RFC3339),
			}
		}
	}

	timeoutMs := provider.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = healthCfg.TimeoutMs
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	if timeoutMs > 60000 {
		timeoutMs = 60000 // Cap at 60 seconds to prevent resource exhaustion
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return upstreamProbeResponse{
			OK:        false,
			TargetURL: targetURL,
			Error:     err.Error(),
			CheckedAt: time.Now().Format(time.RFC3339),
		}
	}
	if strings.TrimSpace(provider.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range provider.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		// Skip sensitive headers to prevent credential leakage
		if isSensitiveHeaderKey(key) {
			continue
		}
		req.Header.Set(key, value)
	}

	client := &http.Client{
		Timeout: time.Duration(timeoutMs) * time.Millisecond,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	start := time.Now()
	resp, err := client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return upstreamProbeResponse{
			OK:        false,
			TargetURL: targetURL,
			LatencyMs: latencyMs,
			Error:     err.Error(),
			CheckedAt: time.Now().Format(time.RFC3339),
		}
	}
	defer resp.Body.Close()

	bodyPreview, _ := io.ReadAll(io.LimitReader(resp.Body, 1536))
	return upstreamProbeResponse{
		OK:          resp.StatusCode >= 200 && resp.StatusCode < 300,
		TargetURL:   targetURL,
		StatusCode:  resp.StatusCode,
		LatencyMs:   latencyMs,
		BodyPreview: redactSecretText(strings.TrimSpace(string(bodyPreview))),
		CheckedAt:   time.Now().Format(time.RFC3339),
	}
}

func redactProbeResponse(resp upstreamProbeResponse) upstreamProbeResponse {
	resp.BodyPreview = redactSecretText(resp.BodyPreview)
	resp.Error = redactSecretText(resp.Error)
	return resp
}

func redactSecretText(text string) string {
	redacted := text
	for _, marker := range []string{"sk-", "Bearer ", "bearer "} {
		for {
			idx := strings.Index(redacted, marker)
			if idx < 0 {
				break
			}
			end := idx + len(marker)
			for end < len(redacted) {
				ch := redacted[end]
				if ch == '"' || ch == '\'' || ch == '\n' || ch == '\r' || ch == ' ' || ch == '\t' || ch == ',' || ch == '}' || ch == ']' {
					break
				}
				end++
			}
			redacted = redacted[:idx] + marker + "[REDACTED]" + redacted[end:]
		}
	}
	return redacted
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func redactSensitiveHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	for key, value := range headers {
		if isSensitiveHeaderKey(key) && strings.TrimSpace(value) != "" {
			headers[key] = "[REDACTED]"
		}
	}
	return headers
}

func isSensitiveHeaderKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "proxy-authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func sanitizedConfigView(cfg *core.Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}
	type safeAdmin struct {
		Enabled  bool   `json:"enabled"`
		Language string `json:"language"`
	}
	type safeProvider struct {
		Name             string            `json:"name"`
		BaseURL          string            `json:"base_url"`
		AnthropicBaseURL string            `json:"anthropic_base_url,omitempty"`
		ProviderClass    string            `json:"provider_class"`
		Models           []string          `json:"models"`
		Weight           int               `json:"weight"`
		TimeoutMs        int               `json:"timeout_ms"`
		SameRetries      int               `json:"same_retries"`
		Enabled          bool              `json:"enabled"`
		Headers          map[string]string `json:"headers,omitempty"`
	}
	providers := make([]safeProvider, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = safeProvider{
			Name:             p.Name,
			BaseURL:          p.BaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			ProviderClass:    string(p.ProviderClass),
			Models:           append([]string(nil), p.Models...),
			Weight:           p.Weight,
			TimeoutMs:        p.TimeoutMs,
			SameRetries:      p.SameRetries,
			Enabled:          p.IsEnabled(),
			Headers:          redactSensitiveHeaders(cloneStringMap(p.Headers)),
		}
	}
	return map[string]interface{}{
		"server":    cfg.Server,
		"admin":     safeAdmin{Enabled: cfg.Admin.Enabled, Language: cfg.Admin.Language},
		"routing":   cfg.Routing,
		"telemetry": cfg.Telemetry,
		"pricing":   cfg.Pricing,
		"compat":    cfg.Compat,
		"providers": providers,
	}
}

func sanitizedConfigExportView(cfg *core.Config) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}
	type safeAdmin struct {
		Enabled  bool   `yaml:"enabled" json:"enabled"`
		Language string `yaml:"language" json:"language"`
	}
	type safeProvider struct {
		Name             string   `yaml:"name" json:"name"`
		BaseURL          string   `yaml:"base_url" json:"base_url"`
		AnthropicBaseURL string   `yaml:"anthropic_base_url,omitempty" json:"anthropic_base_url,omitempty"`
		ProviderClass    string   `yaml:"provider_class" json:"provider_class"`
		Models           []string `yaml:"models" json:"models"`
		Weight           int      `yaml:"weight" json:"weight"`
		TimeoutMs        int      `yaml:"timeout_ms" json:"timeout_ms"`
		SameRetries      int      `yaml:"same_retries" json:"same_retries"`
		Enabled          bool     `yaml:"enabled" json:"enabled"`
	}
	providers := make([]safeProvider, len(cfg.Providers))
	for i, p := range cfg.Providers {
		providers[i] = safeProvider{
			Name:             p.Name,
			BaseURL:          p.BaseURL,
			AnthropicBaseURL: p.AnthropicBaseURL,
			ProviderClass:    string(p.ProviderClass),
			Models:           p.Models,
			Weight:           p.Weight,
			TimeoutMs:        p.TimeoutMs,
			SameRetries:      p.SameRetries,
			Enabled:          p.IsEnabled(),
		}
	}
	return map[string]interface{}{
		"server":    cfg.Server,
		"admin":     safeAdmin{Enabled: cfg.Admin.Enabled, Language: cfg.Admin.Language},
		"routing":   cfg.Routing,
		"providers": providers,
		"telemetry": cfg.Telemetry,
		"pricing":   cfg.Pricing,
		"compat":    cfg.Compat,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func parsePositiveInt(raw string, fallback, minVal, maxVal int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
