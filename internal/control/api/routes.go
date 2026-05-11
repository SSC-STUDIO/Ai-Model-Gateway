// Package api provides HTTP handlers for the control plane admin API.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/benchmarking"
	"ai-model-gateway/internal/control/publish"
	authinfra "ai-model-gateway/internal/infra/auth"
)

// Deps groups the dependencies for the control API.
type Deps struct {
	ConfigQuery          ConfigQuery
	ConfigCommands       ConfigCommands
	ConfigTools          ConfigTools
	AuditLog             AuditLog
	Runtime              RuntimeConfig
	ProbeRunner          ProbeRunner
	Replay               http.Handler
	TelemetryRPC         TelemetryQuerier
	TelemetryRPCProvider func() TelemetryQuerier
	GatewayRPC           GatewayController
	GatewayRPCProvider   func() GatewayController
	Benchmarking         VerificationBenchmarker
	Version              string
	StartedAt            time.Time
	AdminMiddleware      func(http.Handler) http.Handler
}

// ConfigQuery is the read side of the config control API.
type ConfigQuery interface {
	GetCurrentConfigView() (*publish.CurrentConfigView, error)
	GetHistory(limit int) ([]publish.RevisionInfo, error)
}

// ConfigCommands is the write side of the config control API.
type ConfigCommands interface {
	Publish(revisionID string) (*publish.PublishResult, error)
	Rollback(revisionID string) (*publish.PublishResult, error)
	ReloadConfig() (*publish.PublishResult, error)
	ValidateConfig(cfg interface{}) (*publish.ConfigValidationResult, error)
	UpdateConfig(cfg interface{}, description string) (*publish.PublishResult, error)
}

// TelemetryQuerier is the interface for querying telemetry.
type TelemetryQuerier interface {
	GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error)
	GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error)
	GetTimeSeries(req telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error)
	GetModelBenchmark(req telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error)
}

type telemetryPinger interface {
	Ping() (*telemetryquery.PingResponse, error)
}

// GatewayController is the interface for controlling gatewayd.
type GatewayController interface {
	GetStatus() (*gatewaycontrol.GetStatusResponse, error)
	Drain(req gatewaycontrol.DrainRequest) (*gatewaycontrol.DrainResponse, error)
	GetPricingStatus() (*gatewaycontrol.GetPricingStatusResponse, error)
	RefreshPricing() (*gatewaycontrol.RefreshPricingResponse, error)
}

// VerificationBenchmarker is the control-plane verification benchmark service.
type VerificationBenchmarker interface {
	ListBaselineSnapshots(ctx context.Context) ([]benchmarking.BaselineSnapshot, error)
	ImportBaseline(ctx context.Context, req benchmarking.ImportBaselineRequest) (*benchmarking.BaselineSnapshot, error)
	ListRuns(ctx context.Context, limit int) ([]benchmarking.RunSummary, error)
	StartRun(ctx context.Context, req benchmarking.StartRunRequest) (*benchmarking.RunDetail, error)
	GetRun(ctx context.Context, runID string) (*benchmarking.RunDetail, error)
}

// RevisionInfo contains information about a config revision.
type RevisionInfo struct {
	RevisionID  string    `json:"revision_id"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
}

// PublishResult contains the result of a publish operation.
type PublishResult struct {
	Success      bool      `json:"success"`
	SnapshotID   string    `json:"snapshot_id"`
	RevisionID   string    `json:"revision_id"`
	PublishedAt  time.Time `json:"published_at"`
	ErrorMessage string    `json:"error,omitempty"`
}

// Mount mounts the admin API routes.
// If frontendBundle is nil, it falls back to embedded assets.
func Mount(mux *http.ServeMux, deps Deps, frontendBundle *AdminFrontendBundle) {
	var adminHandler http.HandlerFunc
	var adminAssetsHandler http.Handler

	if frontendBundle == nil {
		if bundled, err := NewAdminFrontendBundle(""); err == nil {
			frontendBundle = bundled
		}
	}
	if frontendBundle != nil {
		adminHandler, adminAssetsHandler = frontendBundle.Handlers()
	} else {
		adminHandler, adminAssetsHandler = adminFrontendPlaceholderHandlers()
	}

	wrap := deps.AdminMiddleware
	if wrap == nil {
		wrap = func(next http.Handler) http.Handler { return next }
	}

	// Admin API routes
	mux.Handle("/api/admin/overview", wrap(http.HandlerFunc(overviewHandler(deps))))
	mux.Handle("/api/admin/config", wrap(http.HandlerFunc(configHandler(deps))))
	mux.Handle("/api/admin/config/history", wrap(http.HandlerFunc(configHistoryHandler(deps))))
	mux.Handle("/api/admin/config/publish", wrap(http.HandlerFunc(configPublishHandler(deps))))
	mux.Handle("/api/admin/config/reload", wrap(http.HandlerFunc(configReloadHandler(deps))))
	mux.Handle("/api/admin/config/rollback", wrap(http.HandlerFunc(configRollbackHandler(deps))))
	mux.Handle("/api/admin/config/validate", wrap(http.HandlerFunc(configValidateHandler(deps))))
	mux.Handle("/api/admin/config/update", wrap(http.HandlerFunc(configUpdateHandler(deps))))
	mux.Handle("/api/admin/config/preview", wrap(http.HandlerFunc(configPreviewHandler(deps))))
	mux.Handle("/api/admin/config/diff", wrap(http.HandlerFunc(configDiffHandler(deps))))
	mux.Handle("/api/admin/telemetry", wrap(http.HandlerFunc(telemetryHandler(deps))))
	mux.Handle("/api/admin/timeseries", wrap(http.HandlerFunc(timeseriesHandler(deps))))
	mux.Handle("/api/admin/benchmark", wrap(http.HandlerFunc(benchmarkHandler(deps))))
	mux.Handle("/api/admin/benchmark/baselines", wrap(http.HandlerFunc(benchmarkBaselinesHandler(deps))))
	mux.Handle("/api/admin/benchmark/baselines/import", wrap(http.HandlerFunc(benchmarkBaselineImportHandler(deps))))
	mux.Handle("/api/admin/benchmark/runs", wrap(http.HandlerFunc(benchmarkRunsHandler(deps))))
	mux.Handle("/api/admin/benchmark/runs/", wrap(http.HandlerFunc(benchmarkRunDetailHandler(deps))))
	mux.Handle("/api/admin/status", wrap(http.HandlerFunc(statusHandler(deps))))
	mux.Handle("/api/admin/runtime/status", wrap(http.HandlerFunc(runtimeStatusHandler(deps))))
	mux.Handle("/api/admin/runtime/preflight", wrap(http.HandlerFunc(runtimePreflightHandler(deps))))
	mux.Handle("/api/admin/audit", wrap(http.HandlerFunc(auditHandler(deps))))
	mux.Handle("/api/admin/probe/provider", wrap(http.HandlerFunc(probeProviderHandler(deps))))
	mux.Handle("/api/admin/probe/model", wrap(http.HandlerFunc(probeModelHandler(deps))))
	mux.Handle("/api/admin/diagnostics", wrap(http.HandlerFunc(diagnosticsHandler(deps))))
	mux.Handle("/api/admin/client-error", wrap(http.HandlerFunc(clientErrorHandler(deps))))
	mux.Handle("/api/admin/secrets/status", wrap(http.HandlerFunc(secretsStatusHandler(deps))))
	if deps.Replay != nil {
		mux.Handle("/api/admin/replay", wrap(deps.Replay))
	} else {
		mux.Handle("/api/admin/replay", wrap(http.HandlerFunc(replayUnavailableHandler)))
	}
	mux.Handle("/api/admin/pricing/status", wrap(http.HandlerFunc(pricingStatusHandler(deps))))
	mux.Handle("/api/admin/pricing/refresh", wrap(http.HandlerFunc(pricingRefreshHandler(deps))))
	mux.Handle("/metrics", http.HandlerFunc(metricsHandler(deps)))

	// Admin UI
	mux.Handle("/admin", wrap(http.HandlerFunc(adminHandler)))
	mux.Handle("/admin/", wrap(adminAssetsHandler))

	// Root-level static resources (for PWA manifest icons)
	mux.Handle("/icon.svg", adminAssetsHandler)
	mux.Handle("/favicon.svg", adminAssetsHandler)
	mux.Handle("/manifest.json", adminAssetsHandler)
}

func (d Deps) telemetryRPC() TelemetryQuerier {
	if d.TelemetryRPCProvider != nil {
		if rpc := d.TelemetryRPCProvider(); rpc != nil {
			return rpc
		}
	}
	return d.TelemetryRPC
}

func (d Deps) gatewayRPC() GatewayController {
	if d.GatewayRPCProvider != nil {
		if rpc := d.GatewayRPCProvider(); rpc != nil {
			return rpc
		}
	}
	return d.GatewayRPC
}

// overviewHandler handles overview requests.
func overviewHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telemetry := deps.telemetryRPC()
		if telemetry == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := telemetry.GetOverview(telemetryquery.OverviewRequest{
			WindowSets: []telemetryquery.WindowSpec{
				{Name: "last_1m", Duration: time.Minute},
				{Name: "last_5m", Duration: 5 * time.Minute},
				{Name: "last_1h", Duration: time.Hour},
				{Name: "last_24h", Duration: 24 * time.Hour},
			},
		})
		if err != nil {
			writeTelemetryError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// configHandler handles config requests.
func configHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Get current config
			if deps.ConfigQuery == nil {
				writeError(w, http.StatusServiceUnavailable, "config query not available")
				return
			}
			view, err := deps.ConfigQuery.GetCurrentConfigView()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if role := AdminRoleFromRequest(r); role != "" && role != authinfra.RoleAdmin && view != nil {
				redacted := *view
				redacted.Config = nil
				redacted.RawYAML = ""
				view = &redacted
			}
			writeJSON(w, http.StatusOK, view)

		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// configHistoryHandler handles config history requests.
func configHistoryHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigQuery == nil {
			writeError(w, http.StatusServiceUnavailable, "config query not available")
			return
		}

		history, err := deps.ConfigQuery.GetHistory(50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, history)
	}
}

// configPublishHandler handles config publish requests.
func configPublishHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigCommands == nil {
			writeError(w, http.StatusServiceUnavailable, "config commands not available")
			return
		}

		var req struct {
			RevisionID string `json:"revision_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		result, err := deps.ConfigCommands.Publish(req.RevisionID)
		if err != nil {
			recordAudit(deps, r, "config.publish", req.RevisionID, false, err.Error(), nil)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(deps, r, "config.publish", req.RevisionID, result != nil && result.Success, resultError(result), nil)

		writeJSON(w, http.StatusOK, result)
	}
}

// configReloadHandler handles config source reload requests.
func configReloadHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigCommands == nil {
			writeError(w, http.StatusServiceUnavailable, "config commands not available")
			return
		}

		result, err := deps.ConfigCommands.ReloadConfig()
		if err != nil {
			recordAudit(deps, r, "config.reload", "source", false, err.Error(), nil)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(deps, r, "config.reload", "source", result != nil && result.Success, resultError(result), nil)

		writeJSON(w, http.StatusOK, result)
	}
}

// configRollbackHandler handles config rollback requests.
func configRollbackHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigCommands == nil {
			writeError(w, http.StatusServiceUnavailable, "config commands not available")
			return
		}

		var req struct {
			RevisionID string `json:"revision_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		result, err := deps.ConfigCommands.Rollback(req.RevisionID)
		if err != nil {
			recordAudit(deps, r, "config.rollback", req.RevisionID, false, err.Error(), nil)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(deps, r, "config.rollback", req.RevisionID, result != nil && result.Success, resultError(result), nil)

		writeJSON(w, http.StatusOK, result)
	}
}

// configValidateHandler handles config validation requests.
func configValidateHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigCommands == nil {
			writeError(w, http.StatusServiceUnavailable, "config commands not available")
			return
		}

		var req struct {
			Config interface{} `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Config == nil {
			writeError(w, http.StatusBadRequest, "config is required")
			return
		}

		result, err := deps.ConfigCommands.ValidateConfig(req.Config)
		if err != nil {
			recordAudit(deps, r, "config.validate", "draft", false, err.Error(), nil)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordAudit(deps, r, "config.validate", "draft", result != nil && result.Valid, validationError(result), nil)

		writeJSON(w, http.StatusOK, result)
	}
}

// configUpdateHandler handles config update requests.
func configUpdateHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		if deps.ConfigCommands == nil {
			writeError(w, http.StatusServiceUnavailable, "config commands not available")
			return
		}

		var req struct {
			Config      interface{} `json:"config"`
			Description string      `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Config == nil {
			writeError(w, http.StatusBadRequest, "config is required")
			return
		}

		result, err := deps.ConfigCommands.UpdateConfig(req.Config, req.Description)
		if err != nil {
			recordAudit(deps, r, "config.update", "draft", false, err.Error(), nil)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resource := ""
		if result != nil {
			resource = result.RevisionID
		}
		recordAudit(deps, r, "config.update", resource, result != nil && result.Success, resultError(result), map[string]any{"description": req.Description})

		writeJSON(w, http.StatusOK, result)
	}
}

// telemetryHandler handles telemetry requests.
func telemetryHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telemetry := deps.telemetryRPC()
		if telemetry == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := telemetry.GetTelemetry(telemetryquery.TelemetryRequest{
			WindowHours: intQuery(r, "hours", 24),
			Limit:       intQuery(r, "limit", 100),
			Offset:      intQuery(r, "offset", 0),
		})
		if err != nil {
			writeTelemetryError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// timeseriesHandler handles timeseries requests.
func timeseriesHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telemetry := deps.telemetryRPC()
		if telemetry == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := telemetry.GetTimeSeries(telemetryquery.TimeSeriesRequest{
			WindowHours:   intQuery(r, "hours", 24),
			BucketMinutes: intQuery(r, "bucket", 5),
			GroupBy:       r.URL.Query().Get("group_by"),
		})
		if err != nil {
			writeTelemetryError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// benchmarkHandler handles benchmark requests.
func benchmarkHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		telemetry := deps.telemetryRPC()
		if telemetry == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := telemetry.GetModelBenchmark(telemetryquery.BenchmarkRequest{
			WindowHours: intQuery(r, "hours", 24),
			Models:      stringListQuery(r, "models"),
			Group:       firstQuery(r, "group", "group_by"),
			StartTime:   timeQuery(r, "start"),
			EndTime:     timeQuery(r, "end"),
		})
		if err != nil {
			writeTelemetryError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// statusHandler handles status requests.
func statusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, buildStatusPayload(deps))
	}
}

func pricingStatusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gateway := deps.gatewayRPC()
		if gateway == nil {
			writeError(w, http.StatusServiceUnavailable, "gateway not connected")
			return
		}
		resp, err := gateway.GetPricingStatus()
		if err != nil {
			writeGatewayError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func pricingRefreshHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		gateway := deps.gatewayRPC()
		if gateway == nil {
			writeError(w, http.StatusServiceUnavailable, "gateway not connected")
			return
		}
		resp, err := gateway.RefreshPricing()
		if err != nil {
			recordAudit(deps, r, "pricing.refresh", "pricing", false, err.Error(), nil)
			writeGatewayError(w, err)
			return
		}
		recordAudit(deps, r, "pricing.refresh", "pricing", resp.Refreshed, resp.Error, nil)
		writeJSON(w, http.StatusOK, resp)
	}
}

func benchmarkBaselinesHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Benchmarking == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmarking not available")
			return
		}
		resp, err := deps.Benchmarking.ListBaselineSnapshots(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"snapshots": resp})
	}
}

func benchmarkBaselineImportHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if deps.Benchmarking == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmarking not available")
			return
		}
		var req benchmarking.ImportBaselineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		resp, err := deps.Benchmarking.ImportBaseline(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func benchmarkRunsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Benchmarking == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmarking not available")
			return
		}
		switch r.Method {
		case http.MethodGet:
			limit := intQuery(r, "limit", 50)
			resp, err := deps.Benchmarking.ListRuns(r.Context(), limit)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"runs": resp})
		case http.MethodPost:
			var req benchmarking.StartRunRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			resp, err := deps.Benchmarking.StartRun(r.Context(), req)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, resp)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func benchmarkRunDetailHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		runID, subresource := parseBenchmarkRunPath(r.URL.Path)
		if runID == "" {
			writeError(w, http.StatusBadRequest, "run id is required")
			return
		}
		switch subresource {
		case "":
			if deps.Benchmarking == nil {
				writeError(w, http.StatusServiceUnavailable, "benchmarking not available")
				return
			}
			resp, err := deps.Benchmarking.GetRun(r.Context(), runID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if resp == nil {
				writeError(w, http.StatusNotFound, "run not found")
				return
			}
			writeJSON(w, http.StatusOK, resp)
		case "telemetry":
			telemetry := deps.telemetryRPC()
			if telemetry == nil {
				writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
				return
			}
			req := telemetryquery.TelemetryRequest{
				WindowHours: intQuery(r, "hours", 24),
				Limit:       intQuery(r, "limit", 200),
				Offset:      intQuery(r, "offset", 0),
				Filters: telemetryquery.TelemetryFilters{
					Models:            stringListQuery(r, "models"),
					Providers:         stringListQuery(r, "providers"),
					SyntheticKind:     "benchmark",
					BenchmarkRunID:    runID,
					BenchmarkTargetID: strings.TrimSpace(r.URL.Query().Get("target_id")),
					BenchmarkCaseID:   strings.TrimSpace(r.URL.Query().Get("case_id")),
				},
			}
			resp, err := telemetry.GetTelemetry(req)
			if err != nil {
				writeTelemetryError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, resp)
		default:
			writeError(w, http.StatusNotFound, "benchmark run subresource not found")
		}
	}
}

func parseBenchmarkRunPath(path string) (runID string, subresource string) {
	remainder := strings.TrimPrefix(path, "/api/admin/benchmark/runs/")
	remainder = strings.Trim(remainder, "/")
	if remainder == "" {
		return "", ""
	}
	parts := strings.SplitN(remainder, "/", 2)
	runID = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		subresource = strings.TrimSpace(parts[1])
	}
	return runID, subresource
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeTelemetryError(w http.ResponseWriter, err error) {
	if isRPCDisconnectedError(err) {
		writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeGatewayError(w http.ResponseWriter, err error) {
	if isRPCDisconnectedError(err) {
		writeError(w, http.StatusServiceUnavailable, "gateway not connected")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func isRPCDisconnectedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	for _, fragment := range []string{
		"connection is shut down",
		"broken pipe",
		"connection refused",
		"transport is closing",
		"use of closed network connection",
		"no such file or directory",
		"connect: cannot assign requested address",
		"unexpected eof",
		"eof",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func intQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	if strings.EqualFold(raw, "all") {
		return 365 * 24
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

func stringListQuery(r *http.Request, key string) []string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}

	items := strings.Split(raw, ",")
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		result = append(result, item)
	}

	return result
}

func firstQuery(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func timeQuery(r *http.Request, key string) *time.Time {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}

	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}

	return &value
}
