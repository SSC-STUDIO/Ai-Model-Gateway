// Package api provides HTTP handlers for the control plane admin API.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/publish"
)

// Deps groups the dependencies for the control API.
type Deps struct {
	ConfigQuery     ConfigQuery
	ConfigCommands  ConfigCommands
	TelemetryRPC    TelemetryQuerier
	GatewayRPC      GatewayController
	Version         string
	StartedAt       time.Time
	AdminMiddleware func(http.Handler) http.Handler
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
}

// TelemetryQuerier is the interface for querying telemetry.
type TelemetryQuerier interface {
	GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error)
	GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error)
	GetTimeSeries(req telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error)
	GetModelBenchmark(req telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error)
}

// GatewayController is the interface for controlling gatewayd.
type GatewayController interface {
	GetStatus() (*gatewaycontrol.GetStatusResponse, error)
	Drain(req gatewaycontrol.DrainRequest) (*gatewaycontrol.DrainResponse, error)
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
func Mount(mux *http.ServeMux, deps Deps) {
	adminHandler, adminAssetsHandler := adminFrontendHandlers()
	wrap := deps.AdminMiddleware
	if wrap == nil {
		wrap = func(next http.Handler) http.Handler { return next }
	}

	// Admin API routes
	mux.Handle("/api/admin/overview", wrap(http.HandlerFunc(overviewHandler(deps))))
	mux.Handle("/api/admin/config", wrap(http.HandlerFunc(configHandler(deps))))
	mux.Handle("/api/admin/config/history", wrap(http.HandlerFunc(configHistoryHandler(deps))))
	mux.Handle("/api/admin/config/publish", wrap(http.HandlerFunc(configPublishHandler(deps))))
	mux.Handle("/api/admin/config/rollback", wrap(http.HandlerFunc(configRollbackHandler(deps))))
	mux.Handle("/api/admin/telemetry", wrap(http.HandlerFunc(telemetryHandler(deps))))
	mux.Handle("/api/admin/timeseries", wrap(http.HandlerFunc(timeseriesHandler(deps))))
	mux.Handle("/api/admin/benchmark", wrap(http.HandlerFunc(benchmarkHandler(deps))))
	mux.Handle("/api/admin/status", wrap(http.HandlerFunc(statusHandler(deps))))

	// Admin UI
	mux.Handle("/admin", wrap(http.HandlerFunc(adminHandler)))
	mux.Handle("/admin/", wrap(adminAssetsHandler))
}

// overviewHandler handles overview requests.
func overviewHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelemetryRPC == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := deps.TelemetryRPC.GetOverview(telemetryquery.OverviewRequest{
			WindowSets: []telemetryquery.WindowSpec{
				{Name: "last_1m", Duration: time.Minute},
				{Name: "last_5m", Duration: 5 * time.Minute},
				{Name: "last_1h", Duration: time.Hour},
				{Name: "last_24h", Duration: 24 * time.Hour},
			},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

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
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// telemetryHandler handles telemetry requests.
func telemetryHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelemetryRPC == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := deps.TelemetryRPC.GetTelemetry(telemetryquery.TelemetryRequest{
			WindowHours: intQuery(r, "hours", 24),
			Limit:       intQuery(r, "limit", 100),
			Offset:      intQuery(r, "offset", 0),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// timeseriesHandler handles timeseries requests.
func timeseriesHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelemetryRPC == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := deps.TelemetryRPC.GetTimeSeries(telemetryquery.TimeSeriesRequest{
			WindowHours:   intQuery(r, "hours", 24),
			BucketMinutes: intQuery(r, "bucket", 5),
			GroupBy:       r.URL.Query().Get("group_by"),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// benchmarkHandler handles benchmark requests.
func benchmarkHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.TelemetryRPC == nil {
			writeError(w, http.StatusServiceUnavailable, "telemetry not connected")
			return
		}

		resp, err := deps.TelemetryRPC.GetModelBenchmark(telemetryquery.BenchmarkRequest{
			WindowHours: intQuery(r, "hours", 24),
			Models:      stringListQuery(r, "models"),
			StartTime:   timeQuery(r, "start"),
			EndTime:     timeQuery(r, "end"),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// statusHandler handles status requests.
func statusHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"version":   deps.Version,
			"startedAt": deps.StartedAt.UTC().Format(time.RFC3339),
			"uptime":    time.Since(deps.StartedAt).String(),
		}

		// Check gateway status
		if deps.GatewayRPC != nil {
			status, err := deps.GatewayRPC.GetStatus()
			if err != nil {
				resp["gateway_status"] = "error"
				resp["gateway_error"] = err.Error()
			} else {
				resp["gateway_status"] = "connected"
				resp["gateway"] = status
			}
		} else {
			resp["gateway_status"] = "disconnected"
		}

		// Check telemetry status
		if deps.TelemetryRPC != nil {
			resp["telemetry_status"] = "connected"
		} else {
			resp["telemetry_status"] = "disconnected"
		}

		writeJSON(w, http.StatusOK, resp)
	}
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

func intQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
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
