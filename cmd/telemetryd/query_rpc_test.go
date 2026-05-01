package main

import (
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/telemetry/query"
)

func TestTelemetryQueryRPCUsesProjectedStoreData(t *testing.T) {
	store, err := query.NewStore(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	now := time.Now().UTC().Truncate(time.Minute)
	mustExec(t, store, `
INSERT INTO agg_buckets (
  bucket, model, provider, requests, successes, failures,
  input_tokens, cached_prompt_tokens, output_tokens, total_latency
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Add(-2*time.Minute).Format(time.RFC3339Nano),
		"gpt-4o",
		"openai",
		3,
		2,
		1,
		3000,
		600,
		1500,
		750,
	)
	mustExec(t, store, `
INSERT INTO request_facts (
  event_id, request_id, timestamp, path, requested_model, effective_model,
  provider_id, route_mode, status_code, latency_ms, attempts,
  prompt_tokens, cached_prompt_tokens, completion_tokens, stream, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt-rpc-1",
		"req-rpc-1",
		now.Add(-2*time.Minute).Format(time.RFC3339Nano),
		"/v1/chat/completions",
		"",
		"gpt-4o",
		"openai",
		"direct",
		200,
		150,
		1,
		1000,
		200,
		500,
		0,
		"",
	)
	mustExec(t, store, `
INSERT INTO request_facts (
  event_id, request_id, timestamp, path, requested_model, effective_model,
  provider_id, route_mode, status_code, latency_ms, attempts,
  prompt_tokens, cached_prompt_tokens, completion_tokens, stream, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"evt-rpc-2",
		"req-rpc-2",
		now.Add(-1*time.Minute).Format(time.RFC3339Nano),
		"/v1/chat/completions",
		"",
		"gpt-4o",
		"openai",
		"direct",
		500,
		450,
		1,
		1000,
		200,
		500,
		0,
		"upstream timeout",
	)

	rpc := &TelemetryQueryRPC{
		daemon: &Daemon{queryStore: store},
	}

	var overview telemetryquery.OverviewResponse
	if err := rpc.GetOverview(telemetryquery.OverviewRequest{
		WindowSets: []telemetryquery.WindowSpec{{Name: "last_1h", Duration: time.Hour}},
	}, &overview); err != nil {
		t.Fatalf("GetOverview returned error: %v", err)
	}
	if overview.Windows["last_1h"].Requests != 3 {
		t.Fatalf("unexpected overview response: %+v", overview)
	}
	if len(overview.AvailableModels) != 1 || overview.AvailableModels[0] != "gpt-4o" {
		t.Fatalf("unexpected available models: %+v", overview.AvailableModels)
	}
	if overview.Runtime.ProviderCount != 0 || overview.Runtime.RouterStrategy != "" {
		t.Fatalf("expected zero-value runtime info, got %+v", overview.Runtime)
	}

	var telemetryResp telemetryquery.TelemetryResponse
	if err := rpc.GetTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
	}, &telemetryResp); err != nil {
		t.Fatalf("GetTelemetry returned error: %v", err)
	}
	if telemetryResp.Total != 2 || len(telemetryResp.Events) != 2 {
		t.Fatalf("unexpected telemetry response: %+v", telemetryResp)
	}
	if telemetryResp.Events[0].EventID != "evt-rpc-2" {
		t.Fatalf("expected most recent event first, got %+v", telemetryResp.Events)
	}
	if len(telemetryResp.Models) != 1 || telemetryResp.Models[0].Value != "gpt-4o" || telemetryResp.Models[0].Requests != 2 {
		t.Fatalf("unexpected model distribution: %+v", telemetryResp.Models)
	}
	if len(telemetryResp.Upstreams) != 1 || telemetryResp.Upstreams[0].Value != "openai" || telemetryResp.Upstreams[0].Failures != 1 {
		t.Fatalf("unexpected upstream distribution: %+v", telemetryResp.Upstreams)
	}

	var timeSeries telemetryquery.TimeSeriesResponse
	if err := rpc.GetTimeSeries(telemetryquery.TimeSeriesRequest{
		WindowHours:   24,
		BucketMinutes: 5,
		GroupBy:       "provider",
	}, &timeSeries); err != nil {
		t.Fatalf("GetTimeSeries returned error: %v", err)
	}
	if len(timeSeries.Buckets) != 1 || timeSeries.Buckets[0].GroupValue != "openai" || timeSeries.Buckets[0].Requests != 3 {
		t.Fatalf("unexpected timeseries response: %+v", timeSeries)
	}

	var benchmark telemetryquery.BenchmarkResponse
	if err := rpc.GetModelBenchmark(telemetryquery.BenchmarkRequest{
		WindowHours: 24,
	}, &benchmark); err != nil {
		t.Fatalf("GetModelBenchmark returned error: %v", err)
	}
	if benchmark.ModelCount != 1 || len(benchmark.Benchmarks) != 1 {
		t.Fatalf("unexpected benchmark response: %+v", benchmark)
	}
	if benchmark.Benchmarks[0].Model != "gpt-4o" || benchmark.Benchmarks[0].Requests != 2 {
		t.Fatalf("unexpected benchmark rows: %+v", benchmark.Benchmarks)
	}
}

func mustExec(t *testing.T, store *query.Store, statement string, args ...interface{}) {
	t.Helper()

	if _, err := store.GetDB().Exec(statement, args...); err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
}
