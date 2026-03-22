package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/proxy"
	"ai-model-gateway/internal/router"
	"ai-model-gateway/internal/state"
	"ai-model-gateway/internal/telemetry"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

type ModelItem struct {
	ID string `json:"id"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelItem `json:"data"`
}

type AdminConfigView struct {
	Admin     configViewAdmin      `json:"admin"`
	Health    configViewHealth     `json:"health"`
	Bridge    configViewBridge     `json:"bridge"`
	Router    configViewRouter     `json:"router"`
	Proxy     configViewProxy      `json:"proxy"`
	Upstreams []configViewUpstream `json:"upstreams"`
}

type configViewAdmin struct {
	Language string `json:"language"`
}

type configViewHealth struct {
	Enabled     bool   `json:"enabled"`
	IntervalSec int    `json:"interval_sec"`
	TimeoutMs   int    `json:"timeout_ms"`
	Path        string `json:"path"`
}

type configViewBridge struct {
	Enabled           bool                   `json:"enabled"`
	ExcludeUserAgents []string               `json:"exclude_user_agents"`
	Rules             []configViewBridgeRule `json:"rules"`
}

type configViewBridgeRule struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type configViewRouter struct {
	Strategy                   string `json:"strategy"`
	MaxRetries                 int    `json:"max_retries"`
	RetryBackoffMs             int    `json:"retry_backoff_ms"`
	RetryBackoffMaxMs          int    `json:"retry_backoff_max_ms"`
	FailureThreshold           int    `json:"failure_threshold"`
	CooldownSec                int    `json:"cooldown_sec"`
	FailurePassthroughAfterSec int    `json:"failure_passthrough_after_sec"`
}

type configViewProxy struct {
	Retry      configViewRetry       `json:"retry"`
	Intercepts []configViewIntercept `json:"intercepts"`
}

type configViewRetry struct {
	InfiniteOnError bool     `json:"infinite_on_error"`
	StatusCodes     []int    `json:"status_codes"`
	StatusCodeMin   *int     `json:"status_code_min"`
	MessageKeywords []string `json:"message_keywords"`
}

type configViewIntercept struct {
	Name            string   `json:"name"`
	Enabled         *bool    `json:"enabled"`
	Paths           []string `json:"paths"`
	StatusCodes     []int    `json:"status_codes"`
	StatusCodeMin   *int     `json:"status_code_min"`
	MessageKeywords []string `json:"message_keywords"`
	Action          string   `json:"action"`
}

type configViewUpstream struct {
	Name                string            `json:"name"`
	BaseURL             string            `json:"base_url"`
	APIKey              string            `json:"api_key"`
	ProviderClass       string            `json:"provider_class"`
	Models              []string          `json:"models"`
	Weight              int               `json:"weight"`
	TimeoutMs           int               `json:"timeout_ms"`
	SameUpstreamRetries int               `json:"same_upstream_retries"`
	Enabled             bool              `json:"enabled"`
	Headers             map[string]string `json:"headers"`
}

type configHistoryResponse struct {
	Versions []configHistoryItem `json:"versions"`
}

type configHistoryItem struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	CreatedAt string `json:"created_at"`
	Size      int64  `json:"size"`
}

type configHistoryDiffResponse struct {
	Version configHistoryItem `json:"version"`
	Summary configDiffSummary `json:"summary"`
	Lines   []configDiffLine  `json:"lines"`
}

type configDiffSummary struct {
	AddedLines    int `json:"added_lines"`
	RemovedLines  int `json:"removed_lines"`
	ChangedBlocks int `json:"changed_blocks"`
}

type configDiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type configRollbackRequest struct {
	VersionID string `json:"version_id"`
}

type upstreamProbeRequest struct {
	Upstream configViewUpstream `json:"upstream"`
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

type adminRuntimeView struct {
	RouterStrategy             string `json:"router_strategy"`
	MaxRetries                 int    `json:"max_retries"`
	RetryInfiniteOnError       bool   `json:"retry_infinite_on_error"`
	RetryBackoffMs             int    `json:"retry_backoff_ms"`
	RetryBackoffMaxMs          int    `json:"retry_backoff_max_ms"`
	FailurePassthroughAfterSec int    `json:"failure_passthrough_after_sec"`
	HealthEnabled              bool   `json:"health_enabled"`
	HealthPath                 string `json:"health_path"`
	BridgeEnabled              bool   `json:"bridge_enabled"`
	TotalUpstreams             int    `json:"total_upstreams"`
	EnabledUpstreams           int    `json:"enabled_upstreams"`
}

func NewRouter(manager *router.Manager, stats *telemetry.Store, pricingCatalog *telemetry.PricingCatalog) http.Handler {
	r := chi.NewRouter()
	r.Use(withRequestID)
	r.Use(accessLog)
	r.Use(requireAdminAuth(manager.CurrentConfig))

	p := proxy.NewHandler(manager, stats)
	var adminDataCache struct {
		mu       sync.Mutex
		cond     *sync.Cond
		expires  time.Time
		payload  []byte
		building bool
	}
	adminDataCache.cond = sync.NewCond(&adminDataCache.mu)

	buildAdminDataPayload := func() ([]byte, error) {
		cfg := manager.CurrentConfig()
		snapshot := stats.Snapshot()
		pricingState := telemetry.BootstrapPricingSnapshot()
		if pricingCatalog != nil {
			pricingState = pricingCatalog.Snapshot()
		}
		pricing := telemetry.BuildPricingSnapshot(snapshot, pricingState)
		var buffer bytes.Buffer
		if err := json.NewEncoder(&buffer).Encode(map[string]any{
			"generated_at":     snapshot.GeneratedAt,
			"router_strategy":  cfg.Router.Strategy,
			"bridge":           cfg.Bridge,
			"runtime":          buildAdminRuntimeView(cfg),
			"available_models": manager.Models(),
			"upstreams":        manager.Snapshot(),
			"telemetry":        snapshot,
			"pricing":          pricing,
		}); err != nil {
			return nil, err
		}
		return append([]byte(nil), buffer.Bytes()...), nil
	}

	r.Get("/-/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"request_id":       observability.RequestIDFromContext(r.Context()),
			"router_strategy":  manager.CurrentConfig().Router.Strategy,
			"bridge":           manager.CurrentConfig().Bridge,
			"available_models": manager.Models(),
			"upstreams":        manager.Snapshot(),
		})
	})
	r.Get("/-/admin/data", func(w http.ResponseWriter, r *http.Request) {
		adminDataCache.mu.Lock()
		deadline := time.Now().Add(3 * time.Second)
		for {
			now := time.Now()
			if len(adminDataCache.payload) > 0 && now.Before(adminDataCache.expires) {
				payload := adminDataCache.payload
				adminDataCache.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(payload)
				return
			}
			if !adminDataCache.building {
				adminDataCache.building = true
				break
			}
			if time.Now().After(deadline) {
				// Timed out waiting for builder; serve stale data if available
				if adminDataCache.payload != nil {
					payload := adminDataCache.payload
					adminDataCache.mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(payload)
					return
				}
				// No stale data, become the builder ourselves
				adminDataCache.building = true
				break
			}
			adminDataCache.cond.Wait()
		}
		stalePayload := adminDataCache.payload
		adminDataCache.mu.Unlock()

		payload, err := buildAdminDataPayload()
		adminDataCache.mu.Lock()
		if err == nil {
			adminDataCache.payload = payload
			adminDataCache.expires = time.Now().Add(2 * time.Second)
		}
		adminDataCache.building = false
		adminDataCache.cond.Broadcast()
		if err != nil && len(stalePayload) > 0 {
			payload = stalePayload
			err = nil
		}
		adminDataCache.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
	r.Get("/-/admin/timeseries", func(w http.ResponseWriter, r *http.Request) {
		hours := 24
		bucketMinutes := 60
		if v := r.URL.Query().Get("hours"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
				hours = n
			}
		}
		if v := r.URL.Query().Get("bucket"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1440 {
				bucketMinutes = n
			}
		}
		ts := stats.QueryTimeSeries(hours, bucketMinutes)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ts)
	})
	r.Get("/-/admin/config", func(w http.ResponseWriter, r *http.Request) {
		cfg := manager.CurrentConfig()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(renderConfigView(cfg))
	})
	r.Get("/-/admin/config/export", func(w http.ResponseWriter, r *http.Request) {
		data, err := exportConfigPayload(manager.ConfigStore(), manager.CurrentConfig())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Content-Disposition", `attachment; filename="config.export.yaml"`)
		_, _ = w.Write(data)
	})
	r.Get("/-/admin/config/history", func(w http.ResponseWriter, r *http.Request) {
		store := manager.ConfigStore()
		if store == nil {
			http.Error(w, "config store is not available", http.StatusInternalServerError)
			return
		}
		versions, err := store.ListVersions()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(renderConfigHistory(versions))
	})
	r.Get("/-/admin/config/history/{version_id}/diff", func(w http.ResponseWriter, r *http.Request) {
		store := manager.ConfigStore()
		if store == nil {
			http.Error(w, "config store is not available", http.StatusInternalServerError)
			return
		}
		versionID := chi.URLParam(r, "version_id")
		diff, err := buildConfigHistoryDiff(store, versionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(diff)
	})
	r.Put("/-/admin/config", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10 MB limit
		var payload AdminConfigView
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid config payload", http.StatusBadRequest)
			return
		}

		cfg := manager.CurrentConfig()
		cfg.Admin = applyAdminConfig(cfg.Admin, payload.Admin)
		cfg.Health = applyHealthConfig(cfg.Health, payload.Health)
		cfg.Bridge = applyBridgeConfig(cfg.Bridge, payload.Bridge)
		cfg.Router = applyRouterConfig(cfg.Router, payload.Router)
		cfg.Proxy = applyProxyConfig(cfg.Proxy, payload.Proxy)
		cfg.Upstreams = applyUpstreamConfig(payload.Upstreams)
		cfg.Normalize()
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		store := manager.ConfigStore()
		if store != nil {
			if err := store.Save(cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			store.Set(cfg)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(renderConfigView(cfg))
	})
	r.Post("/-/admin/config/rollback", func(w http.ResponseWriter, r *http.Request) {
		store := manager.ConfigStore()
		if store == nil {
			http.Error(w, "config store is not available", http.StatusInternalServerError)
			return
		}
		var payload configRollbackRequest
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		cfg, err := rollbackConfigVersion(store, payload.VersionID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		store.Set(cfg)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(renderConfigView(cfg))
	})
	r.Post("/-/admin/upstreams/test", func(w http.ResponseWriter, r *http.Request) {
		var payload upstreamProbeRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid upstream probe payload", http.StatusBadRequest)
			return
		}

		result := probeUpstream(payload.Upstream, manager.CurrentConfig().Health)
		status := http.StatusOK
		if !result.OK && result.StatusCode == 0 {
			status = http.StatusBadGateway
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	})
	r.Get("/admin", adminPage(false, manager))
	r.Get("/admin/settings", adminPage(true, manager))
	r.Get("/favicon.svg", adminFavicon())
	r.Get("/favicon.ico", adminFavicon())

	r.Get("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		models := manager.Models()
		data := make([]ModelItem, 0, len(models))
		for _, model := range models {
			data = append(data, ModelItem{ID: model})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ModelsResponse{
			Object: "list",
			Data:   data,
		})
	})

	r.Post("/v1/chat/completions", p.ChatCompletions)
	r.Post("/v1/completions", p.Completions)
	r.Post("/v1/embeddings", p.Embeddings)
	r.Post("/v1/messages", p.Messages)
	r.Post("/v1/messages/count_tokens", p.MessageCountTokens)
	r.Post("/v1/responses", p.Responses)
	r.Post("/v1/responses/compact", p.ResponsesCompact)
	r.Get("/v1/responses/{response_id}", p.ResponseResource)
	r.Delete("/v1/responses/{response_id}", p.ResponseResource)
	r.Post("/v1/moderations", p.Moderations)
	r.Post("/v1/images/generations", p.ImageGenerations)
	r.Post("/v1/images/edits", p.ImageEdits)
	r.Post("/v1/images/variations", p.ImageVariations)
	r.Post("/v1/audio/speech", p.AudioSpeech)
	r.Post("/v1/audio/transcriptions", p.AudioTranscriptions)
	r.Post("/v1/audio/translations", p.AudioTranslations)
	r.Get("/v1/files", p.Files)
	r.Post("/v1/files", p.Files)
	r.Get("/v1/files/{file_id}", p.FileResource)
	r.Delete("/v1/files/{file_id}", p.FileResource)
	r.Get("/v1/files/{file_id}/content", p.FileContent)

	return r
}

func renderConfigView(cfg config.Config) AdminConfigView {
	return AdminConfigView{
		Admin: configViewAdmin{
			Language: cfg.Admin.Language,
		},
		Health: configViewHealth{
			Enabled:     cfg.Health.Enabled,
			IntervalSec: cfg.Health.IntervalSec,
			TimeoutMs:   cfg.Health.TimeoutMs,
			Path:        cfg.Health.Path,
		},
		Bridge: configViewBridge{
			Enabled:           cfg.Bridge.Enabled,
			ExcludeUserAgents: append([]string(nil), cfg.Bridge.ExcludeUserAgents...),
			Rules:             renderBridgeRules(cfg.Bridge.Rules),
		},
		Router: configViewRouter{
			Strategy:                   cfg.Router.Strategy,
			MaxRetries:                 cfg.Router.MaxRetries,
			RetryBackoffMs:             cfg.Router.RetryBackoffMs,
			RetryBackoffMaxMs:          cfg.Router.RetryBackoffMaxMs,
			FailureThreshold:           cfg.Router.FailureThreshold,
			CooldownSec:                cfg.Router.CooldownSec,
			FailurePassthroughAfterSec: cfg.Router.FailurePassthroughAfterSec,
		},
		Proxy: configViewProxy{
			Retry: configViewRetry{
				InfiniteOnError: cfg.Proxy.Retry.InfiniteOnError,
				StatusCodes:     append([]int(nil), cfg.Proxy.Retry.StatusCodes...),
				StatusCodeMin:   cfg.Proxy.Retry.StatusCodeMin,
				MessageKeywords: append([]string(nil), cfg.Proxy.Retry.MessageKeywords...),
			},
			Intercepts: renderIntercepts(cfg.Proxy.Intercepts),
		},
		Upstreams: renderUpstreams(cfg.Upstreams),
	}
}

func applyAdminConfig(current config.AdminConfig, incoming configViewAdmin) config.AdminConfig {
	if strings.TrimSpace(incoming.Language) != "" {
		current.Language = config.NormalizeAdminLanguage(incoming.Language)
	}
	return current
}

func renderBridgeRules(rules []config.ModelBridgeRule) []configViewBridgeRule {
	if len(rules) == 0 {
		return nil
	}
	items := make([]configViewBridgeRule, 0, len(rules))
	for _, rule := range rules {
		items = append(items, configViewBridgeRule{
			From: rule.From,
			To:   rule.To,
		})
	}
	return items
}

func applyHealthConfig(current config.HealthConfig, incoming configViewHealth) config.HealthConfig {
	current.Enabled = incoming.Enabled
	current.IntervalSec = incoming.IntervalSec
	current.TimeoutMs = incoming.TimeoutMs
	current.Path = strings.TrimSpace(incoming.Path)
	return current
}

func applyBridgeConfig(current config.ModelBridgeConfig, incoming configViewBridge) config.ModelBridgeConfig {
	current.Enabled = incoming.Enabled
	current.ExcludeUserAgents = append([]string(nil), incoming.ExcludeUserAgents...)
	current.Rules = make([]config.ModelBridgeRule, 0, len(incoming.Rules))
	for _, rule := range incoming.Rules {
		current.Rules = append(current.Rules, config.ModelBridgeRule{
			From: strings.TrimSpace(rule.From),
			To:   strings.TrimSpace(rule.To),
		})
	}
	return current
}

func renderIntercepts(rules []config.ResponseInterceptRule) []configViewIntercept {
	if len(rules) == 0 {
		return nil
	}
	items := make([]configViewIntercept, 0, len(rules))
	for _, rule := range rules {
		items = append(items, configViewIntercept{
			Name:            rule.Name,
			Enabled:         rule.Enabled,
			Paths:           append([]string(nil), rule.Paths...),
			StatusCodes:     append([]int(nil), rule.StatusCodes...),
			StatusCodeMin:   rule.StatusCodeMin,
			MessageKeywords: append([]string(nil), rule.MessageKeywords...),
			Action:          strings.ToLower(strings.TrimSpace(rule.Action)),
		})
	}
	return items
}

func applyRouterConfig(current config.RouterConfig, incoming configViewRouter) config.RouterConfig {
	if incoming.Strategy != "" {
		current.Strategy = incoming.Strategy
	}
	current.MaxRetries = incoming.MaxRetries
	current.RetryBackoffMs = incoming.RetryBackoffMs
	current.RetryBackoffMaxMs = incoming.RetryBackoffMaxMs
	current.FailureThreshold = incoming.FailureThreshold
	current.CooldownSec = incoming.CooldownSec
	current.FailurePassthroughAfterSec = incoming.FailurePassthroughAfterSec
	return current
}

func applyProxyConfig(current config.ProxyPolicyConfig, incoming configViewProxy) config.ProxyPolicyConfig {
	current.Retry.InfiniteOnError = incoming.Retry.InfiniteOnError
	current.Retry.StatusCodes = append([]int(nil), incoming.Retry.StatusCodes...)
	current.Retry.StatusCodeMin = incoming.Retry.StatusCodeMin
	current.Retry.MessageKeywords = append([]string(nil), incoming.Retry.MessageKeywords...)
	current.Intercepts = make([]config.ResponseInterceptRule, 0, len(incoming.Intercepts))
	for _, rule := range incoming.Intercepts {
		current.Intercepts = append(current.Intercepts, config.ResponseInterceptRule{
			Name:            rule.Name,
			Enabled:         rule.Enabled,
			Paths:           append([]string(nil), rule.Paths...),
			StatusCodes:     append([]int(nil), rule.StatusCodes...),
			StatusCodeMin:   rule.StatusCodeMin,
			MessageKeywords: append([]string(nil), rule.MessageKeywords...),
			Action:          strings.TrimSpace(rule.Action),
		})
	}
	return current
}

func renderUpstreams(upstreams []config.Upstream) []configViewUpstream {
	if len(upstreams) == 0 {
		return nil
	}
	items := make([]configViewUpstream, 0, len(upstreams))
	for _, upstream := range upstreams {
		items = append(items, configViewUpstream{
			Name:                upstream.Name,
			BaseURL:             upstream.BaseURL,
			APIKey:              upstream.APIKey,
			ProviderClass:       upstream.ProviderClassNormalized(),
			Models:              append([]string(nil), upstream.Models...),
			Weight:              upstream.Weight,
			TimeoutMs:           upstream.TimeoutMs,
			SameUpstreamRetries: upstream.SameUpstreamRetries,
			Enabled:             upstream.IsEnabled(),
			Headers:             cloneStringMap(upstream.Headers),
		})
	}
	return items
}

func applyUpstreamConfig(incoming []configViewUpstream) []config.Upstream {
	items := make([]config.Upstream, 0, len(incoming))
	for _, upstream := range incoming {
		enabled := upstream.Enabled
		items = append(items, config.Upstream{
			Name:                strings.TrimSpace(upstream.Name),
			BaseURL:             strings.TrimSpace(upstream.BaseURL),
			APIKey:              strings.TrimSpace(upstream.APIKey),
			ProviderClass:       config.NormalizeUpstreamClass(upstream.ProviderClass),
			Models:              append([]string(nil), upstream.Models...),
			Weight:              upstream.Weight,
			TimeoutMs:           upstream.TimeoutMs,
			SameUpstreamRetries: upstream.SameUpstreamRetries,
			Enabled:             &enabled,
			Headers:             cloneStringMap(upstream.Headers),
		})
	}
	return items
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

func probeUpstream(view configViewUpstream, healthCfg config.HealthConfig) upstreamProbeResponse {
	items := applyUpstreamConfig([]configViewUpstream{view})
	if len(items) == 0 {
		return upstreamProbeResponse{
			OK:        false,
			Error:     "upstream payload is empty",
			CheckedAt: time.Now().Format(time.RFC3339),
		}
	}
	upstream := items[0]
	if upstream.BaseURL == "" {
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
	targetURL := strings.TrimRight(upstream.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")
	timeoutMs := upstream.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = healthCfg.TimeoutMs
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return upstreamProbeResponse{
			OK:        false,
			TargetURL: targetURL,
			Error:     err.Error(),
			CheckedAt: time.Now().Format(time.RFC3339),
		}
	}
	if upstream.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range upstream.Headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
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
		BodyPreview: strings.TrimSpace(string(bodyPreview)),
		CheckedAt:   time.Now().Format(time.RFC3339),
	}
}

func buildAdminRuntimeView(cfg config.Config) adminRuntimeView {
	enabledUpstreams := 0
	for _, upstream := range cfg.Upstreams {
		if upstream.IsEnabled() {
			enabledUpstreams++
		}
	}

	return adminRuntimeView{
		RouterStrategy:             cfg.Router.Strategy,
		MaxRetries:                 cfg.Router.MaxRetries,
		RetryInfiniteOnError:       cfg.Proxy.Retry.InfiniteOnError,
		RetryBackoffMs:             cfg.Router.RetryBackoffMs,
		RetryBackoffMaxMs:          cfg.Router.RetryBackoffMaxMs,
		FailurePassthroughAfterSec: cfg.Router.FailurePassthroughAfterSec,
		HealthEnabled:              cfg.Health.Enabled,
		HealthPath:                 cfg.Health.Path,
		BridgeEnabled:              cfg.Bridge.Enabled,
		TotalUpstreams:             len(cfg.Upstreams),
		EnabledUpstreams:           enabledUpstreams,
	}
}

func renderConfigHistory(versions []state.ConfigVersion) configHistoryResponse {
	items := make([]configHistoryItem, 0, len(versions))
	for _, version := range versions {
		items = append(items, renderConfigHistoryItem(version))
	}
	return configHistoryResponse{Versions: items}
}

func rollbackConfigVersion(store interface {
	Rollback() (config.Config, error)
	RollbackVersion(string) (config.Config, error)
}, versionID string) (config.Config, error) {
	if strings.TrimSpace(versionID) == "" {
		return store.Rollback()
	}
	return store.RollbackVersion(versionID)
}

func exportConfigPayload(store interface{ Path() string }, cfg config.Config) ([]byte, error) {
	if store != nil && store.Path() != "" {
		data, err := os.ReadFile(store.Path())
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	exportCfg := cfg
	if store != nil && store.Path() != "" {
		config.RelativizePaths(&exportCfg, store.Path())
	}
	return yaml.Marshal(exportCfg)
}

func renderConfigHistoryItem(version state.ConfigVersion) configHistoryItem {
	return configHistoryItem{
		ID:        version.ID,
		Filename:  version.Filename,
		CreatedAt: version.CreatedAt.Format(http.TimeFormat),
		Size:      version.Size,
	}
}

func buildConfigHistoryDiff(store interface {
	ReadCurrentFile() ([]byte, error)
	ReadVersionFile(string) (state.ConfigVersion, []byte, error)
}, versionID string) (configHistoryDiffResponse, error) {
	currentData, err := store.ReadCurrentFile()
	if err != nil {
		return configHistoryDiffResponse{}, err
	}
	version, versionData, err := store.ReadVersionFile(versionID)
	if err != nil {
		return configHistoryDiffResponse{}, err
	}
	lines := buildConfigDiffLines(currentData, versionData)
	return configHistoryDiffResponse{
		Version: renderConfigHistoryItem(version),
		Summary: summarizeConfigDiff(lines),
		Lines:   lines,
	}, nil
}

func buildConfigDiffLines(current []byte, previous []byte) []configDiffLine {
	left := normalizeDiffLines(previous)
	right := normalizeDiffLines(current)

	dp := make([][]int, len(left)+1)
	for i := range dp {
		dp[i] = make([]int, len(right)+1)
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			if left[i-1] == right[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
				continue
			}
			if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	var reversed []configDiffLine
	for i, j := len(left), len(right); i > 0 || j > 0; {
		switch {
		case i > 0 && j > 0 && left[i-1] == right[j-1]:
			reversed = append(reversed, configDiffLine{Kind: "context", Text: left[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] > dp[i-1][j]):
			reversed = append(reversed, configDiffLine{Kind: "add", Text: right[j-1]})
			j--
		default:
			reversed = append(reversed, configDiffLine{Kind: "remove", Text: left[i-1]})
			i--
		}
	}

	lines := make([]configDiffLine, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		lines = append(lines, reversed[i])
	}
	return lines
}

func summarizeConfigDiff(lines []configDiffLine) configDiffSummary {
	var summary configDiffSummary
	inChangedBlock := false
	hasAdd := false
	hasRemove := false

	flushBlock := func() {
		if hasAdd && hasRemove {
			summary.ChangedBlocks++
		}
		inChangedBlock = false
		hasAdd = false
		hasRemove = false
	}

	for _, line := range lines {
		switch line.Kind {
		case "add":
			summary.AddedLines++
			inChangedBlock = true
			hasAdd = true
		case "remove":
			summary.RemovedLines++
			inChangedBlock = true
			hasRemove = true
		default:
			if inChangedBlock {
				flushBlock()
			}
		}
	}
	if inChangedBlock {
		flushBlock()
	}
	return summary
}

func normalizeDiffLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
