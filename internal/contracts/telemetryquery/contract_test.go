package telemetryquery

import (
	"testing"
	"time"
)

// TestWindowSpec tests the window specification struct.
func TestWindowSpec(t *testing.T) {
	ws := WindowSpec{
		Name:     "last_1h",
		Duration: time.Hour,
	}

	if ws.Name != "last_1h" {
		t.Errorf("Name = %q, want last_1h", ws.Name)
	}
	if ws.Duration != time.Hour {
		t.Errorf("Duration = %v, want %v", ws.Duration, time.Hour)
	}
}

// TestWindowMetrics tests the window metrics struct.
func TestWindowMetrics(t *testing.T) {
	wm := WindowMetrics{
		Requests:           100,
		Successes:          90,
		Failures:           10,
		AvgLatencyMs:       150.5,
		InputTokens:        5000,
		CachedPromptTokens: 1000,
		OutputTokens:       2000,
	}

	if wm.Requests != 100 {
		t.Errorf("Requests = %d, want 100", wm.Requests)
	}
	if wm.Successes != 90 {
		t.Errorf("Successes = %d, want 90", wm.Successes)
	}
	if wm.Failures != 10 {
		t.Errorf("Failures = %d, want 10", wm.Failures)
	}
	if wm.AvgLatencyMs != 150.5 {
		t.Errorf("AvgLatencyMs = %f, want 150.5", wm.AvgLatencyMs)
	}
	if wm.InputTokens != 5000 {
		t.Errorf("InputTokens = %d, want 5000", wm.InputTokens)
	}
	if wm.CachedPromptTokens != 1000 {
		t.Errorf("CachedPromptTokens = %d, want 1000", wm.CachedPromptTokens)
	}
	if wm.OutputTokens != 2000 {
		t.Errorf("OutputTokens = %d, want 2000", wm.OutputTokens)
	}
}

// TestRuntimeInfo tests the runtime info struct.
func TestRuntimeInfo(t *testing.T) {
	ri := RuntimeInfo{
		ProviderCount:         5,
		EnabledProviderCount:  3,
		RouterStrategy:        "weighted",
		HealthEnabled:         true,
		StickySessionsEnabled: false,
		BridgeEnabled:         true,
	}

	if ri.ProviderCount != 5 {
		t.Errorf("ProviderCount = %d, want 5", ri.ProviderCount)
	}
	if ri.EnabledProviderCount != 3 {
		t.Errorf("EnabledProviderCount = %d, want 3", ri.EnabledProviderCount)
	}
	if ri.RouterStrategy != "weighted" {
		t.Errorf("RouterStrategy = %q, want weighted", ri.RouterStrategy)
	}
	if !ri.HealthEnabled {
		t.Error("HealthEnabled should be true")
	}
	if ri.StickySessionsEnabled {
		t.Error("StickySessionsEnabled should be false")
	}
	if !ri.BridgeEnabled {
		t.Error("BridgeEnabled should be true")
	}
}

// TestOverviewRequest tests the overview request struct.
func TestOverviewRequest(t *testing.T) {
	req := OverviewRequest{
		WindowSets: []WindowSpec{
			{Name: "last_1m", Duration: time.Minute},
			{Name: "last_1h", Duration: time.Hour},
		},
	}

	if len(req.WindowSets) != 2 {
		t.Errorf("WindowSets length = %d, want 2", len(req.WindowSets))
	}
	if req.WindowSets[0].Name != "last_1m" {
		t.Errorf("WindowSets[0].Name = %q, want last_1m", req.WindowSets[0].Name)
	}
}

// TestOverviewResponse tests the overview response struct.
func TestOverviewResponse(t *testing.T) {
	resp := OverviewResponse{
		Windows: map[string]WindowMetrics{
			"last_1m": {Requests: 10},
		},
		Runtime: RuntimeInfo{
			ProviderCount: 2,
		},
		AvailableModels: []string{"gpt-4", "claude-3"},
	}

	if len(resp.Windows) != 1 {
		t.Errorf("Windows length = %d, want 1", len(resp.Windows))
	}
	if resp.Runtime.ProviderCount != 2 {
		t.Errorf("Runtime.ProviderCount = %d, want 2", resp.Runtime.ProviderCount)
	}
	if len(resp.AvailableModels) != 2 {
		t.Errorf("AvailableModels length = %d, want 2", len(resp.AvailableModels))
	}
}

// TestTelemetryFilters tests the telemetry filters struct.
func TestTelemetryFilters(t *testing.T) {
	f := TelemetryFilters{
		Models:       []string{"gpt-4", "claude-3"},
		Providers:    []string{"openai", "anthropic"},
		StatusCodes:  []int{200, 500},
		ErrorsOnly:   true,
		MinLatencyMs: 100,
		MaxLatencyMs: 5000,
	}

	if len(f.Models) != 2 {
		t.Errorf("Models length = %d, want 2", len(f.Models))
	}
	if !f.ErrorsOnly {
		t.Error("ErrorsOnly should be true")
	}
	if f.MinLatencyMs != 100 {
		t.Errorf("MinLatencyMs = %d, want 100", f.MinLatencyMs)
	}
	if f.MaxLatencyMs != 5000 {
		t.Errorf("MaxLatencyMs = %d, want 5000", f.MaxLatencyMs)
	}
}

// TestTelemetryRequest tests the telemetry request struct.
func TestTelemetryRequest(t *testing.T) {
	req := TelemetryRequest{
		WindowHours: 24,
		Limit:       100,
		Offset:      0,
		Filters: TelemetryFilters{
			Models: []string{"gpt-4"},
		},
	}

	if req.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", req.WindowHours)
	}
	if req.Limit != 100 {
		t.Errorf("Limit = %d, want 100", req.Limit)
	}
	if req.Offset != 0 {
		t.Errorf("Offset = %d, want 0", req.Offset)
	}
}

// TestTelemetryResponse tests the telemetry response struct.
func TestTelemetryResponse(t *testing.T) {
	resp := TelemetryResponse{
		Events: []TelemetryEvent{
			{EventID: "evt-001"},
			{EventID: "evt-002"},
		},
		Total:       2,
		WindowHours: 1,
	}

	if len(resp.Events) != 2 {
		t.Errorf("Events length = %d, want 2", len(resp.Events))
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if resp.WindowHours != 1 {
		t.Errorf("WindowHours = %d, want 1", resp.WindowHours)
	}
}

// TestTelemetryEvent tests the telemetry event struct.
func TestTelemetryEvent(t *testing.T) {
	now := time.Now()
	te := TelemetryEvent{
		EventID:        "evt-001",
		Timestamp:      now,
		RequestID:      "req-001",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-4",
		EffectiveModel: "gpt-4-turbo",
		Provider:       "openai",
		RouteMode:      "direct",
		StatusCode:     200,
		LatencyMs:      150,
		Attempts:       1,
		InputTokens:    100,
		CachedPromptTokens: 50,
		OutputTokens:   200,
		Stream:         true,
		Error:          "",
	}

	if te.EventID != "evt-001" {
		t.Errorf("EventID = %q, want evt-001", te.EventID)
	}
	if te.RequestID != "req-001" {
		t.Errorf("RequestID = %q, want req-001", te.RequestID)
	}
	if te.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", te.StatusCode)
	}
	if te.LatencyMs != 150 {
		t.Errorf("LatencyMs = %d, want 150", te.LatencyMs)
	}
}

// TestTimeSeriesRequest tests the time series request struct.
func TestTimeSeriesRequest(t *testing.T) {
	req := TimeSeriesRequest{
		WindowHours:   24,
		BucketMinutes: 5,
		GroupBy:       "model",
	}

	if req.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", req.WindowHours)
	}
	if req.BucketMinutes != 5 {
		t.Errorf("BucketMinutes = %d, want 5", req.BucketMinutes)
	}
	if req.GroupBy != "model" {
		t.Errorf("GroupBy = %q, want model", req.GroupBy)
	}
}

// TestTimeSeriesResponse tests the time series response struct.
func TestTimeSeriesResponse(t *testing.T) {
	resp := TimeSeriesResponse{
		Buckets: []TimeBucket{
			{Bucket: "2024-01-01T00:00:00Z", Requests: 10},
			{Bucket: "2024-01-01T00:05:00Z", Requests: 20},
		},
		WindowHours:   24,
		BucketMinutes: 5,
	}

	if len(resp.Buckets) != 2 {
		t.Errorf("Buckets length = %d, want 2", len(resp.Buckets))
	}
	if resp.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", resp.WindowHours)
	}
}

// TestTimeBucket tests the time bucket struct.
func TestTimeBucket(t *testing.T) {
	tb := TimeBucket{
		Bucket:           "2024-01-01T00:00:00Z",
		Requests:         100,
		Successes:        95,
		Failures:         5,
		InputTokens:      1000,
		CachedPromptTokens: 200,
		OutputTokens:     500,
		AvgLatencyMs:     120.5,
		GroupValue:       "gpt-4",
	}

	if tb.Bucket != "2024-01-01T00:00:00Z" {
		t.Errorf("Bucket = %q, want 2024-01-01T00:00:00Z", tb.Bucket)
	}
	if tb.Requests != 100 {
		t.Errorf("Requests = %d, want 100", tb.Requests)
	}
	if tb.GroupValue != "gpt-4" {
		t.Errorf("GroupValue = %q, want gpt-4", tb.GroupValue)
	}
}

// TestBenchmarkRequest tests the benchmark request struct.
func TestBenchmarkRequest(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()
	req := BenchmarkRequest{
		WindowHours: 24,
		Models:      []string{"gpt-4", "claude-3"},
		StartTime:   &start,
		EndTime:     &end,
	}

	if req.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", req.WindowHours)
	}
	if len(req.Models) != 2 {
		t.Errorf("Models length = %d, want 2", len(req.Models))
	}
	if req.StartTime == nil {
		t.Error("StartTime should not be nil")
	}
	if req.EndTime == nil {
		t.Error("EndTime should not be nil")
	}
}

// TestBenchmarkResponse tests the benchmark response struct.
func TestBenchmarkResponse(t *testing.T) {
	resp := BenchmarkResponse{
		Benchmarks: []ModelBenchmark{
			{Model: "gpt-4", Requests: 100},
		},
		WindowHours: 24,
		ModelCount:  1,
	}

	if len(resp.Benchmarks) != 1 {
		t.Errorf("Benchmarks length = %d, want 1", len(resp.Benchmarks))
	}
	if resp.ModelCount != 1 {
		t.Errorf("ModelCount = %d, want 1", resp.ModelCount)
	}
}

// TestModelBenchmark tests the model benchmark struct.
func TestModelBenchmark(t *testing.T) {
	mb := ModelBenchmark{
		Model:            "gpt-4",
		Requests:         1000,
		Successes:        950,
		Failures:         50,
		InputTokens:      50000,
		CachedPromptTokens: 10000,
		OutputTokens:     20000,
		AvgLatencyMs:     200.0,
		P50LatencyMs:     180.0,
		P95LatencyMs:     350.0,
		P99LatencyMs:     500.0,
		MaxLatencyMs:     1000,
		SuccessRate:      95.0,
		EstimatedCostUSD: 12.50,
	}

	if mb.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", mb.Model)
	}
	if mb.Requests != 1000 {
		t.Errorf("Requests = %d, want 1000", mb.Requests)
	}
	if mb.P50LatencyMs != 180.0 {
		t.Errorf("P50LatencyMs = %f, want 180.0", mb.P50LatencyMs)
	}
	if mb.P95LatencyMs != 350.0 {
		t.Errorf("P95LatencyMs = %f, want 350.0", mb.P95LatencyMs)
	}
	if mb.P99LatencyMs != 500.0 {
		t.Errorf("P99LatencyMs = %f, want 500.0", mb.P99LatencyMs)
	}
	if mb.MaxLatencyMs != 1000 {
		t.Errorf("MaxLatencyMs = %d, want 1000", mb.MaxLatencyMs)
	}
	if mb.SuccessRate != 95.0 {
		t.Errorf("SuccessRate = %f, want 95.0", mb.SuccessRate)
	}
	if mb.EstimatedCostUSD != 12.50 {
		t.Errorf("EstimatedCostUSD = %f, want 12.50", mb.EstimatedCostUSD)
	}
}

// TestPingRequest tests the ping request struct.
func TestPingRequest(t *testing.T) {
	now := time.Now()
	req := PingRequest{Timestamp: now}
	if !req.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", req.Timestamp, now)
	}
}

// TestPingResponse tests the ping response struct.
func TestPingResponse(t *testing.T) {
	now := time.Now()
	resp := PingResponse{
		Version:    "1.0.0",
		ServerTime: now,
		EventCount: 5000,
		Healthy:    true,
	}

	if resp.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", resp.Version)
	}
	if resp.EventCount != 5000 {
		t.Errorf("EventCount = %d, want 5000", resp.EventCount)
	}
	if !resp.Healthy {
		t.Error("Healthy should be true")
	}
}

// TestTelemetryQueryRPCInterface ensures the interface is correctly defined.
func TestTelemetryQueryRPCInterface(t *testing.T) {
	var _ TelemetryQueryRPC = (*mockTelemetryQuery)(nil)
}

type mockTelemetryQuery struct{}

func (m *mockTelemetryQuery) GetOverview(req OverviewRequest, resp *OverviewResponse) error {
	return nil
}

func (m *mockTelemetryQuery) GetTelemetry(req TelemetryRequest, resp *TelemetryResponse) error {
	return nil
}

func (m *mockTelemetryQuery) GetTimeSeries(req TimeSeriesRequest, resp *TimeSeriesResponse) error {
	return nil
}

func (m *mockTelemetryQuery) GetModelBenchmark(req BenchmarkRequest, resp *BenchmarkResponse) error {
	return nil
}

func (m *mockTelemetryQuery) Ping(req PingRequest, resp *PingResponse) error {
	return nil
}
