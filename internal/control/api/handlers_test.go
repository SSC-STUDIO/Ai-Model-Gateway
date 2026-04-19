package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/publish"
)

// Stub implementations for testing

type stubTelemetryQuerier struct {
	overview   *telemetryquery.OverviewResponse
	telemetry  *telemetryquery.TelemetryResponse
	timeseries *telemetryquery.TimeSeriesResponse
	benchmark  *telemetryquery.BenchmarkResponse
	err        error
}

func (s *stubTelemetryQuerier) GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.overview, nil
}

func (s *stubTelemetryQuerier) GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.telemetry, nil
}

func (s *stubTelemetryQuerier) GetTimeSeries(req telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.timeseries, nil
}

func (s *stubTelemetryQuerier) GetModelBenchmark(req telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.benchmark, nil
}

type stubGatewayController struct {
	status *gatewaycontrol.GetStatusResponse
	err    error
}

func (s *stubGatewayController) GetStatus() (*gatewaycontrol.GetStatusResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubGatewayController) Drain(req gatewaycontrol.DrainRequest) (*gatewaycontrol.DrainResponse, error) {
	return &gatewaycontrol.DrainResponse{Success: true}, nil
}

// Handler tests

func TestOverviewHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestOverviewHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &stubTelemetryQuerier{
			overview: &telemetryquery.OverviewResponse{
				Windows: map[string]telemetryquery.WindowMetrics{
					"last_1h": {Requests: 100},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestTelemetryHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &stubTelemetryQuerier{
			telemetry: &telemetryquery.TelemetryResponse{Total: 50},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/telemetry?hours=12&limit=50", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestTelemetryHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/telemetry", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestTimeseriesHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &stubTelemetryQuerier{
			timeseries: &telemetryquery.TimeSeriesResponse{},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/timeseries?hours=24&bucket=10&group_by=model", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestTimeseriesHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/timeseries", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestBenchmarkHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &stubTelemetryQuerier{
			benchmark: &telemetryquery.BenchmarkResponse{},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark?hours=24&models=gpt-4,claude", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBenchmarkHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestStatusHandler_WithGateway(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version:   "1.0.0",
		StartedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		GatewayRPC: &stubGatewayController{
			status: &gatewaycontrol.GetStatusResponse{

				ActiveRequests: 5,
			},
		},
		TelemetryRPC: &stubTelemetryQuerier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", resp["version"])
	}
	if resp["gateway_status"] != "connected" {
		t.Errorf("expected gateway_status connected, got %v", resp["gateway_status"])
	}
	if resp["telemetry_status"] != "connected" {
		t.Errorf("expected telemetry_status connected, got %v", resp["telemetry_status"])
	}
}

func TestStatusHandler_WithoutGateway(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version:   "1.0.0",
		StartedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["gateway_status"] != "disconnected" {
		t.Errorf("expected gateway_status disconnected, got %v", resp["gateway_status"])
	}
	if resp["telemetry_status"] != "disconnected" {
		t.Errorf("expected telemetry_status disconnected, got %v", resp["telemetry_status"])
	}
}

func TestStatusHandler_GatewayError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version: "1.0.0",
		GatewayRPC: &stubGatewayController{
			err: http.ErrHandlerTimeout,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["gateway_status"] != "error" {
		t.Errorf("expected gateway_status error, got %v", resp["gateway_status"])
	}
}

func TestConfigHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigHandler_QueryNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigHistoryHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigHistoryHandler_QueryNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigPublishHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/publish", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigPublishHandler_CommandsNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigPublishHandler_InvalidBody(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigCommands: &stubConfigCommands{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestConfigRollbackHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/rollback", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigRollbackHandler_CommandsNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/rollback", strings.NewReader(`{"revision_id":"rev-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigRollbackHandler_Success(t *testing.T) {
	mux := http.NewServeMux()
	commands := &stubConfigCommands{
		rollbackResult: &publish.PublishResult{Success: true, RevisionID: "rev-001"},
	}
	Mount(mux, Deps{
		ConfigCommands: commands,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/rollback", strings.NewReader(`{"revision_id":"rev-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if commands.rollbackCalls != 1 {
		t.Errorf("expected 1 rollback call, got %d", commands.rollbackCalls)
	}
}

// Query helper tests

func TestIntQuery(t *testing.T) {
	tests := []struct {
		url      string
		key      string
		fallback int
		expected int
	}{
		{"http://example.com?hours=24", "hours", 1, 24},
		{"http://example.com?hours=abc", "hours", 10, 10},
		{"http://example.com", "hours", 5, 5},
		{"http://example.com?hours=", "hours", 8, 8},
		{"http://example.com?hours=%20%2012%20%20", "hours", 1, 12},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			result := intQuery(req, tt.key, tt.fallback)
			if result != tt.expected {
				t.Errorf("intQuery(%q, %q, %d) = %d, want %d", tt.url, tt.key, tt.fallback, result, tt.expected)
			}
		})
	}
}

func TestStringListQuery(t *testing.T) {
	tests := []struct {
		url      string
		key      string
		expected []string
	}{
		{"http://example.com?models=gpt-4,claude", "models", []string{"gpt-4", "claude"}},
		{"http://example.com?models=gpt-4,claude,gemini", "models", []string{"gpt-4", "claude", "gemini"}},
		{"http://example.com", "models", nil},
		{"http://example.com?models=", "models", nil},
		{"http://example.com?models=,,,", "models", nil},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			result := stringListQuery(req, tt.key)
			if len(result) != len(tt.expected) {
				t.Errorf("stringListQuery(%q, %q) = %v, want %v", tt.url, tt.key, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("stringListQuery(%q, %q)[%d] = %q, want %q", tt.url, tt.key, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestTimeQuery(t *testing.T) {
	tests := []struct {
		url      string
		key      string
		expected *time.Time
	}{
		{"http://example.com?start=2026-04-18T12:00:00Z", "start", ptrTime(time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC))},
		{"http://example.com", "start", nil},
		{"http://example.com?start=invalid", "start", nil},
		{"http://example.com?start=", "start", nil},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			result := timeQuery(req, tt.key)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("timeQuery(%q, %q) = %v, want nil", tt.url, tt.key, result)
				}
			} else {
				if result == nil {
					t.Errorf("timeQuery(%q, %q) = nil, want %v", tt.url, tt.key, tt.expected)
				} else if !result.Equal(*tt.expected) {
					t.Errorf("timeQuery(%q, %q) = %v, want %v", tt.url, tt.key, result, tt.expected)
				}
			}
		})
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestAdminFrontendPlaceholderHandlers(t *testing.T) {
	root, assets := adminFrontendPlaceholderHandlers()

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "AI-Model-Gateway Admin") {
		t.Errorf("expected placeholder HTML, got %s", rec.Body.String())
	}

	// Test assets handler
	req2 := httptest.NewRequest(http.MethodGet, "/admin/assets/test.js", nil)
	rec2 := httptest.NewRecorder()
	assets.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}
}

func TestAdminFrontendBundle_Handlers(t *testing.T) {
	bundle := adminFrontendBundle{
		index:  []byte(`<!DOCTYPE html><html><body>test</body></html>`),
		static: http.FileServer(http.Dir(".")),
	}
	root, assets := bundle.handlers()

	// Test root handler
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Test assets handler with /admin path
	req2 := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec2 := httptest.NewRecorder()
	assets.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	// Test assets handler with /admin/assets path
	req3 := httptest.NewRequest(http.MethodGet, "/admin/assets/test.js", nil)
	rec3 := httptest.NewRecorder()
	assets.ServeHTTP(rec3, req3)
	// This will 404 since there's no actual file, but the handler should be called
}

func TestLoadDiskAdminFrontend_NonExistent(t *testing.T) {
	_, ok := loadDiskAdminFrontend("/nonexistent/path")
	if ok {
		t.Error("expected false for non-existent path")
	}
}

func TestLoadEmbeddedAdminFrontend(t *testing.T) {
	// This should succeed since we have embedded files
	_, ok := loadEmbeddedAdminFrontend()
	// The result depends on whether embedded files exist
	_ = ok // Just checking it doesn't panic
}

func TestMustSubFS(t *testing.T) {
	// Test with valid subdirectory
	sub := mustSubFS(embeddedAdminFiles, ".")
	if sub == nil {
		t.Error("expected non-nil FS")
	}
}

func TestConfigHandler_QueryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigQuery: &errorConfigQuery{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestConfigHistoryHandler_QueryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigQuery: &errorConfigQuery{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestConfigPublishHandler_CommandError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigCommands: &errorConfigCommands{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/publish", strings.NewReader(`{"revision_id":"rev-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestConfigRollbackHandler_CommandError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		ConfigCommands: &errorConfigCommands{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/rollback", strings.NewReader(`{"revision_id":"rev-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestOverviewHandler_TelemetryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &errorTelemetryQuerier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestTelemetryHandler_TelemetryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &errorTelemetryQuerier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/telemetry", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestTimeseriesHandler_TelemetryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &errorTelemetryQuerier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/timeseries", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestBenchmarkHandler_TelemetryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPC: &errorTelemetryQuerier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}

// Error stubs for testing error paths

type errorConfigQuery struct{}

func (e *errorConfigQuery) GetCurrentConfigView() (*publish.CurrentConfigView, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorConfigQuery) GetHistory(limit int) ([]publish.RevisionInfo, error) {
	return nil, http.ErrHandlerTimeout
}

type errorConfigCommands struct{}

func (e *errorConfigCommands) Publish(revisionID string) (*publish.PublishResult, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorConfigCommands) Rollback(revisionID string) (*publish.PublishResult, error) {
	return nil, http.ErrHandlerTimeout
}

type errorTelemetryQuerier struct{}

func (e *errorTelemetryQuerier) GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorTelemetryQuerier) GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorTelemetryQuerier) GetTimeSeries(req telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorTelemetryQuerier) GetModelBenchmark(req telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error) {
	return nil, http.ErrHandlerTimeout
}
