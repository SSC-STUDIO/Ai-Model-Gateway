package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/benchmarking"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
)

// Stub implementations for testing

type stubTelemetryQuerier struct {
	overview         *telemetryquery.OverviewResponse
	telemetry        *telemetryquery.TelemetryResponse
	timeseries       *telemetryquery.TimeSeriesResponse
	benchmark        *telemetryquery.BenchmarkResponse
	ping             *telemetryquery.PingResponse
	lastTelemetryReq telemetryquery.TelemetryRequest
	err              error
}

func (s *stubTelemetryQuerier) GetOverview(req telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.overview, nil
}

func (s *stubTelemetryQuerier) GetTelemetry(req telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error) {
	s.lastTelemetryReq = req
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

func (s *stubTelemetryQuerier) Ping() (*telemetryquery.PingResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.ping != nil {
		return s.ping, nil
	}
	return &telemetryquery.PingResponse{
		Version:    "test-telemetry",
		EventCount: 0,
		Healthy:    true,
	}, nil
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

func (s *stubGatewayController) GetPricingStatus() (*gatewaycontrol.GetPricingStatusResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &gatewaycontrol.GetPricingStatusResponse{}, nil
}

func (s *stubGatewayController) RefreshPricing() (*gatewaycontrol.RefreshPricingResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &gatewaycontrol.RefreshPricingResponse{Refreshed: true}, nil
}

type stubVerificationBenchmarker struct {
	snapshots []benchmarking.BaselineSnapshot
	runs      []benchmarking.RunSummary
	run       *benchmarking.RunDetail
	err       error
}

func (s *stubVerificationBenchmarker) ListBaselineSnapshots(ctx context.Context) ([]benchmarking.BaselineSnapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]benchmarking.BaselineSnapshot(nil), s.snapshots...), nil
}

func (s *stubVerificationBenchmarker) ImportBaseline(ctx context.Context, req benchmarking.ImportBaselineRequest) (*benchmarking.BaselineSnapshot, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &benchmarking.BaselineSnapshot{SnapshotID: "baseline_test", Kind: req.Kind, SourceName: req.SourceName, RowCount: 1}, nil
}

func (s *stubVerificationBenchmarker) ListRuns(ctx context.Context, limit int) ([]benchmarking.RunSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]benchmarking.RunSummary(nil), s.runs...), nil
}

func (s *stubVerificationBenchmarker) StartRun(ctx context.Context, req benchmarking.StartRunRequest) (*benchmarking.RunDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &benchmarking.RunDetail{
		RunSummary: benchmarking.RunSummary{
			RunID:        "run_test",
			Status:       benchmarking.RunStatusRunning,
			SuiteVersion: core.BenchmarkSuiteGeneralProtocolV1,
			TargetCount:  1,
		},
	}, nil
}

func (s *stubVerificationBenchmarker) GetRun(ctx context.Context, runID string) (*benchmarking.RunDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.run, nil
}

// Handler tests

func TestOverviewHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
	}, nil)

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
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/telemetry?hours=12&limit=50", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBenchmarkVerificationHandlers_Success(t *testing.T) {
	mux := http.NewServeMux()
	telemetry := &stubTelemetryQuerier{
		telemetry: &telemetryquery.TelemetryResponse{Total: 2},
	}
	benchmarker := &stubVerificationBenchmarker{
		snapshots: []benchmarking.BaselineSnapshot{
			{SnapshotID: "baseline_public", Kind: benchmarking.BaselineKindPublicStandard, RowCount: 5},
		},
		runs: []benchmarking.RunSummary{
			{RunID: "run_1", Status: benchmarking.RunStatusCompleted, SuiteVersion: core.BenchmarkSuiteGeneralProtocolV1, TargetCount: 1},
		},
		run: &benchmarking.RunDetail{
			RunSummary: benchmarking.RunSummary{
				RunID:        "run_1",
				Status:       benchmarking.RunStatusCompleted,
				SuiteVersion: core.BenchmarkSuiteGeneralProtocolV1,
				TargetCount:  1,
			},
		},
	}
	Mount(mux, Deps{Benchmarking: benchmarker, TelemetryRPC: telemetry}, nil)

	for _, tc := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{method: http.MethodGet, path: "/api/admin/benchmark/baselines", status: http.StatusOK},
		{method: http.MethodPost, path: "/api/admin/benchmark/baselines/import", body: `{"kind":"public_standard","source_name":"test","file_name":"a.json","contents":"[]"}`, status: http.StatusOK},
		{method: http.MethodGet, path: "/api/admin/benchmark/runs", status: http.StatusOK},
		{method: http.MethodPost, path: "/api/admin/benchmark/runs", body: `{"provider_id":"p","public_model":"m","public_snapshot_id":"b"}`, status: http.StatusOK},
		{method: http.MethodGet, path: "/api/admin/benchmark/runs/run_1", status: http.StatusOK},
		{method: http.MethodGet, path: "/api/admin/benchmark/runs/run_1/telemetry?providers=p&models=m&target_id=target-1&case_id=reasoning_exact", status: http.StatusOK},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, rec.Code, tc.status)
		}
	}
	if telemetry.lastTelemetryReq.Filters.SyntheticKind != "benchmark" {
		t.Fatalf("SyntheticKind = %q, want benchmark", telemetry.lastTelemetryReq.Filters.SyntheticKind)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkRunID != "run_1" {
		t.Fatalf("BenchmarkRunID = %q, want run_1", telemetry.lastTelemetryReq.Filters.BenchmarkRunID)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkTargetID != "target-1" {
		t.Fatalf("BenchmarkTargetID = %q, want target-1", telemetry.lastTelemetryReq.Filters.BenchmarkTargetID)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkCaseID != "reasoning_exact" {
		t.Fatalf("BenchmarkCaseID = %q, want reasoning_exact", telemetry.lastTelemetryReq.Filters.BenchmarkCaseID)
	}
	if len(telemetry.lastTelemetryReq.Filters.Providers) != 1 || telemetry.lastTelemetryReq.Filters.Providers[0] != "p" {
		t.Fatalf("Providers = %#v, want [p]", telemetry.lastTelemetryReq.Filters.Providers)
	}
	if len(telemetry.lastTelemetryReq.Filters.Models) != 1 || telemetry.lastTelemetryReq.Filters.Models[0] != "m" {
		t.Fatalf("Models = %#v, want [m]", telemetry.lastTelemetryReq.Filters.Models)
	}
}

func TestBenchmarkVerificationRunTelemetryHandlerForwardsPaginationAndFilters(t *testing.T) {
	mux := http.NewServeMux()
	telemetry := &stubTelemetryQuerier{
		telemetry: &telemetryquery.TelemetryResponse{Total: 1},
	}
	Mount(mux, Deps{
		Benchmarking: &stubVerificationBenchmarker{},
		TelemetryRPC: telemetry,
	}, nil)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/admin/benchmark/runs/run_2/telemetry?hours=6&limit=25&offset=10&providers=p1,p2&models=m1,m2&target_id=target-2&case_id=tool_json",
		nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if telemetry.lastTelemetryReq.WindowHours != 6 {
		t.Fatalf("WindowHours = %d, want 6", telemetry.lastTelemetryReq.WindowHours)
	}
	if telemetry.lastTelemetryReq.Limit != 25 {
		t.Fatalf("Limit = %d, want 25", telemetry.lastTelemetryReq.Limit)
	}
	if telemetry.lastTelemetryReq.Offset != 10 {
		t.Fatalf("Offset = %d, want 10", telemetry.lastTelemetryReq.Offset)
	}
	if telemetry.lastTelemetryReq.Filters.SyntheticKind != "benchmark" {
		t.Fatalf("SyntheticKind = %q, want benchmark", telemetry.lastTelemetryReq.Filters.SyntheticKind)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkRunID != "run_2" {
		t.Fatalf("BenchmarkRunID = %q, want run_2", telemetry.lastTelemetryReq.Filters.BenchmarkRunID)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkTargetID != "target-2" {
		t.Fatalf("BenchmarkTargetID = %q, want target-2", telemetry.lastTelemetryReq.Filters.BenchmarkTargetID)
	}
	if telemetry.lastTelemetryReq.Filters.BenchmarkCaseID != "tool_json" {
		t.Fatalf("BenchmarkCaseID = %q, want tool_json", telemetry.lastTelemetryReq.Filters.BenchmarkCaseID)
	}
	if got := telemetry.lastTelemetryReq.Filters.Providers; len(got) != 2 || got[0] != "p1" || got[1] != "p2" {
		t.Fatalf("Providers = %#v, want [p1 p2]", got)
	}
	if got := telemetry.lastTelemetryReq.Filters.Models; len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("Models = %#v, want [m1 m2]", got)
	}
}

func TestBenchmarkVerificationRunDetailHandler_RequiresRunID(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{Benchmarking: &stubVerificationBenchmarker{}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark/runs/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "run id is required") {
		t.Fatalf("expected run id error, got %s", rec.Body.String())
	}
}

func TestBenchmarkVerificationRunDetailHandler_BenchmarkingUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark/runs/run_3", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "benchmarking not available") {
		t.Fatalf("expected benchmarking unavailable error, got %s", rec.Body.String())
	}
}

func TestBenchmarkVerificationRunDetailHandler_TelemetryUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{Benchmarking: &stubVerificationBenchmarker{}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark/runs/run_4/telemetry", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "telemetry not connected") {
		t.Fatalf("expected telemetry unavailable error, got %s", rec.Body.String())
	}
}

func TestBenchmarkVerificationRunDetailHandler_UnknownSubresource(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{Benchmarking: &stubVerificationBenchmarker{}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark/runs/run_5/weird", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if !strings.Contains(rec.Body.String(), "benchmark run subresource not found") {
		t.Fatalf("expected unknown subresource error, got %s", rec.Body.String())
	}
}

func TestTelemetryHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/timeseries?hours=24&bucket=10&group_by=model", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestTimeseriesHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/benchmark?hours=24&models=gpt-4,claude", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestBenchmarkHandler_TelemetryNotConnected(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
		TelemetryRPC: &stubTelemetryQuerier{
			ping: &telemetryquery.PingResponse{
				Version:    "telemetry-1.2.3",
				EventCount: 42,
				Healthy:    true,
			},
		},
	}, nil)

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
	if resp["telemetry_version"] != "telemetry-1.2.3" {
		t.Errorf("expected telemetry_version telemetry-1.2.3, got %v", resp["telemetry_version"])
	}
	if resp["telemetry_event_count"] != float64(42) {
		t.Errorf("expected telemetry_event_count 42, got %v", resp["telemetry_event_count"])
	}
}

func TestStatusHandler_WithoutGateway(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version:   "1.0.0",
		StartedAt: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
	}, nil)

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
	if resp["telemetry_last_checked_at"] == nil {
		t.Errorf("expected telemetry_last_checked_at to be present")
	}
}

func TestStatusHandler_GatewayError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version: "1.0.0",
		GatewayRPC: &stubGatewayController{
			err: http.ErrHandlerTimeout,
		},
	}, nil)

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

func TestStatusHandler_TelemetryError(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		Version: "1.0.0",
		TelemetryRPC: &stubTelemetryQuerier{
			err: http.ErrHandlerTimeout,
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["telemetry_status"] != "error" {
		t.Fatalf("expected telemetry_status error, got %v", resp["telemetry_status"])
	}
	if resp["telemetry_error"] == nil {
		t.Fatalf("expected telemetry_error to be present")
	}
}

func TestOverviewHandler_UsesTelemetryProvider(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{
		TelemetryRPCProvider: func() TelemetryQuerier {
			return &stubTelemetryQuerier{
				overview: &telemetryquery.OverviewResponse{
					Windows: map[string]telemetryquery.WindowMetrics{
						"last_1h": {Requests: 7},
					},
				},
			}
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestConfigHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigHandler_QueryNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigHistoryHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigHistoryHandler_QueryNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestConfigPublishHandler_MethodNotAllowed(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/publish", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigPublishHandler_CommandsNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
	}, nil)

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
	Mount(mux, Deps{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/config/rollback", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestConfigRollbackHandler_CommandsNotAvailable(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, Deps{}, nil)

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
	}, nil)

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
		{"http://example.com?hours=all", "hours", 1, 365 * 24},
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
	bundle := &AdminFrontendBundle{
		index:  []byte(`<!DOCTYPE html><html><body>test</body></html>`),
		static: http.FileServer(http.Dir(".")),
	}
	root, assets := bundle.Handlers()

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
	}, nil)

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
	}, nil)

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
	}, nil)

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
	}, nil)

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
	}, nil)

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
	}, nil)

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
	}, nil)

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
	}, nil)

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

func (e *errorConfigCommands) ReloadConfig() (*publish.PublishResult, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorConfigCommands) Rollback(revisionID string) (*publish.PublishResult, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorConfigCommands) UpdateConfig(cfg interface{}, description string) (*publish.PublishResult, error) {
	return nil, http.ErrHandlerTimeout
}

func (e *errorConfigCommands) ValidateConfig(cfg interface{}) (*publish.ConfigValidationResult, error) {
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
