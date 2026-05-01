package query

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryquery"
)

type testRequestFact struct {
	EventID                  string
	RequestID                string
	Timestamp                time.Time
	Path                     string
	RequestedModel           string
	EffectiveModel           string
	ProviderID               string
	RouteMode                string
	StatusCode               int
	LatencyMs                int64
	Attempts                 int
	PromptTokens             int64
	CachedPromptTokens       int64
	OutputTokens             int64
	PricingStatus            string
	PricingSourceID          string
	PricingCurrency          string
	PricingInputPer1M        float64
	PricingCachedInputPer1M  float64
	PricingPromptCost        float64
	PricingCompletionCost    float64
	PricingTotalCost         float64
	PricingPromptCostUSD     float64
	PricingCompletionCostUSD float64
	PricingTotalCostUSD      float64
	SyntheticKind            string
	BenchmarkRunID           string
	BenchmarkTargetID        string
	BenchmarkCaseID          string
	Stream                   bool
	ErrorMessage             string
}

type testAggBucket struct {
	Bucket             time.Time
	Model              string
	Provider           string
	Requests           int64
	Successes          int64
	Failures           int64
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	TotalLatency       int64
}

func TestQueryWindowMetricsAndAvailableModels(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	insertAggBucket(t, store, testAggBucket{
		Bucket:             now.Add(-10 * time.Minute),
		Model:              "gpt-4o",
		Provider:           "openai",
		Requests:           5,
		Successes:          4,
		Failures:           1,
		InputTokens:        1000,
		CachedPromptTokens: 150,
		OutputTokens:       500,
		TotalLatency:       1250,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:             now.Add(-2 * time.Hour),
		Model:              "claude-sonnet-4-5",
		Provider:           "anthropic",
		Requests:           7,
		Successes:          7,
		Failures:           0,
		InputTokens:        2000,
		CachedPromptTokens: 0,
		OutputTokens:       800,
		TotalLatency:       2100,
	})

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-3 * time.Minute),
		Path:           "/v1/chat/completions",
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      120,
		Attempts:       1,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-2",
		RequestID:      "req-2",
		Timestamp:      now.Add(-4 * time.Minute),
		Path:           "/v1/responses",
		RequestedModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		StatusCode:     200,
		LatencyMs:      180,
		Attempts:       1,
	})

	metrics, err := store.QueryWindowMetrics(time.Hour)
	if err != nil {
		t.Fatalf("QueryWindowMetrics returned error: %v", err)
	}
	if metrics.Requests != 5 || metrics.Successes != 4 || metrics.Failures != 1 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
	if metrics.InputTokens != 1000 || metrics.CachedPromptTokens != 150 || metrics.OutputTokens != 500 {
		t.Fatalf("unexpected token metrics: %+v", metrics)
	}
	if metrics.AvgLatencyMs != 250 {
		t.Fatalf("expected avg latency 250, got %v", metrics.AvgLatencyMs)
	}

	models, err := store.ListAvailableModels()
	if err != nil {
		t.Fatalf("ListAvailableModels returned error: %v", err)
	}
	if len(models) != 2 || models[0] != "claude-sonnet-4-5" || models[1] != "gpt-4o" {
		t.Fatalf("unexpected available models: %#v", models)
	}
}

func TestQueryTelemetryFiltersAndPagination(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:            "evt-a",
		RequestID:          "req-a",
		Timestamp:          now.Add(-1 * time.Minute),
		Path:               "/v1/chat/completions",
		RequestedModel:     "router-model",
		EffectiveModel:     "gpt-4o",
		ProviderID:         "openai",
		RouteMode:          "bridge_fallback",
		StatusCode:         500,
		LatencyMs:          1800,
		Attempts:           2,
		PromptTokens:       1200,
		CachedPromptTokens: 200,
		OutputTokens:       400,
		ErrorMessage:       "upstream timeout",
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-b",
		RequestID:      "req-b",
		Timestamp:      now.Add(-2 * time.Minute),
		Path:           "/v1/chat/completions",
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		RouteMode:      "direct",
		StatusCode:     200,
		LatencyMs:      220,
		Attempts:       1,
		PromptTokens:   800,
		OutputTokens:   250,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-c",
		RequestID:      "req-c",
		Timestamp:      now.Add(-3 * time.Minute),
		Path:           "/v1/responses",
		EffectiveModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		RouteMode:      "direct",
		StatusCode:     429,
		LatencyMs:      950,
		Attempts:       1,
		ErrorMessage:   "rate limited",
	})

	filtered, total, windowHours, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
		Filters: telemetryquery.TelemetryFilters{
			Models:       []string{"router-model"},
			Providers:    []string{"openai"},
			StatusCodes:  []int{500},
			ErrorsOnly:   true,
			MinLatencyMs: 1000,
		},
	})
	if err != nil {
		t.Fatalf("QueryTelemetry(filtered) returned error: %v", err)
	}
	if windowHours != 24 {
		t.Fatalf("expected normalized window hours 24, got %d", windowHours)
	}
	if total != 1 || len(filtered) != 1 {
		t.Fatalf("expected one filtered event, total=%d len=%d", total, len(filtered))
	}
	if filtered[0].EventID != "evt-a" || filtered[0].Attempts != 2 || !filtered[0].Timestamp.Equal(now.Add(-1*time.Minute)) {
		t.Fatalf("unexpected filtered event: %+v", filtered[0])
	}

	page, total, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       1,
		Offset:      1,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry(paginated) returned error: %v", err)
	}
	if total != 3 || len(page) != 1 {
		t.Fatalf("expected paginated result with total=3 len=1, got total=%d len=%d", total, len(page))
	}
	if page[0].EventID != "evt-b" {
		t.Fatalf("expected second-most-recent event evt-b, got %+v", page[0])
	}
}

func TestQueryTelemetryDistributionsUseFullWindow(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 4; i++ {
		insertRequestFact(t, store, testRequestFact{
			EventID:        fmt.Sprintf("evt-model-a-%d", i),
			RequestID:      fmt.Sprintf("req-model-a-%d", i),
			Timestamp:      now.Add(-time.Duration(i+1) * time.Minute),
			Path:           "/v1/chat/completions",
			EffectiveModel: "gpt-4o",
			ProviderID:     "openai",
			StatusCode:     200,
			LatencyMs:      int64(100 + i),
			PromptTokens:   100,
			OutputTokens:   20,
		})
	}
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-model-b",
		RequestID:      "req-model-b",
		Timestamp:      now.Add(-10 * time.Minute),
		Path:           "/v1/messages",
		EffectiveModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		StatusCode:     500,
		LatencyMs:      900,
		PromptTokens:   50,
		OutputTokens:   5,
		ErrorMessage:   "upstream failed",
	})

	events, total, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry returned error: %v", err)
	}
	if total != 5 || len(events) != 2 {
		t.Fatalf("expected paginated events len=2 total=5, got len=%d total=%d", len(events), total)
	}

	models, upstreams, windowHours, err := store.QueryTelemetryDistributions(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       2,
	})
	if err != nil {
		t.Fatalf("QueryTelemetryDistributions returned error: %v", err)
	}
	if windowHours != 24 {
		t.Fatalf("windowHours = %d, want 24", windowHours)
	}
	if len(models) != 2 || models[0].Value != "gpt-4o" || models[0].Requests != 4 || models[1].Value != "claude-sonnet-4-5" || models[1].Failures != 1 {
		t.Fatalf("unexpected model distributions: %+v", models)
	}
	if len(upstreams) != 2 || upstreams[0].Value != "openai" || upstreams[0].Requests != 4 || upstreams[1].Value != "anthropic" || upstreams[1].Failures != 1 {
		t.Fatalf("unexpected upstream distributions: %+v", upstreams)
	}
}

func TestQueryTelemetryDistributionsExcludeSyntheticByDefault(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-real",
		RequestID:      "req-real",
		Timestamp:      now.Add(-time.Minute),
		Path:           "/v1/chat/completions",
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-probe",
		RequestID:      "req-probe",
		Timestamp:      now.Add(-time.Minute),
		Path:           "/v1/chat/completions",
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		SyntheticKind:  "probe",
	})

	models, upstreams, _, err := store.QueryTelemetryDistributions(telemetryquery.TelemetryRequest{WindowHours: 24})
	if err != nil {
		t.Fatalf("QueryTelemetryDistributions returned error: %v", err)
	}
	if len(models) != 1 || models[0].Requests != 1 {
		t.Fatalf("default distributions should exclude synthetic traffic, got models=%+v", models)
	}
	if len(upstreams) != 1 || upstreams[0].Requests != 1 {
		t.Fatalf("default distributions should exclude synthetic traffic, got upstreams=%+v", upstreams)
	}

	models, _, _, err = store.QueryTelemetryDistributions(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Filters: telemetryquery.TelemetryFilters{
			SyntheticKind: "probe",
		},
	})
	if err != nil {
		t.Fatalf("QueryTelemetryDistributions(synthetic) returned error: %v", err)
	}
	if len(models) != 1 || models[0].Requests != 1 {
		t.Fatalf("synthetic distributions should include filtered synthetic traffic, got models=%+v", models)
	}
}

func TestQueryTimeSeriesAndModelBenchmark(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	groupBase := now.Truncate(5 * time.Minute).Add(-10 * time.Minute)

	insertAggBucket(t, store, testAggBucket{
		Bucket:             groupBase.Add(1 * time.Minute),
		Model:              "gpt-4o",
		Provider:           "openai",
		Requests:           2,
		Successes:          2,
		Failures:           0,
		InputTokens:        2000,
		CachedPromptTokens: 200,
		OutputTokens:       1000,
		TotalLatency:       300,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:             groupBase.Add(2 * time.Minute),
		Model:              "gpt-4o",
		Provider:           "openai",
		Requests:           1,
		Successes:          0,
		Failures:           1,
		InputTokens:        1000,
		CachedPromptTokens: 100,
		OutputTokens:       500,
		TotalLatency:       150,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:             groupBase.Add(2 * time.Minute),
		Model:              "claude-sonnet-4-5",
		Provider:           "anthropic",
		Requests:           4,
		Successes:          3,
		Failures:           1,
		InputTokens:        3200,
		CachedPromptTokens: 0,
		OutputTokens:       1400,
		TotalLatency:       1200,
	})

	for i, latency := range []int64{100, 200, 400} {
		insertRequestFact(t, store, testRequestFact{
			EventID:            "bench-gpt-4o-" + string(rune('a'+i)),
			RequestID:          "bench-req-" + string(rune('a'+i)),
			Timestamp:          now.Add(time.Duration(-(i + 1)) * time.Minute),
			Path:               "/v1/chat/completions",
			EffectiveModel:     "gpt-4o",
			ProviderID:         "openai",
			StatusCode:         []int{200, 500, 200}[i],
			LatencyMs:          latency,
			Attempts:           1,
			PromptTokens:       1000,
			CachedPromptTokens: 200,
			OutputTokens:       500,
		})
	}
	insertRequestFact(t, store, testRequestFact{
		EventID:        "bench-claude",
		RequestID:      "bench-claude",
		Timestamp:      now.Add(-2 * time.Minute),
		Path:           "/v1/responses",
		EffectiveModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		StatusCode:     200,
		LatencyMs:      250,
		Attempts:       1,
		PromptTokens:   900,
		OutputTokens:   300,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "bench-old",
		RequestID:      "bench-old",
		Timestamp:      now.Add(-3 * time.Hour),
		Path:           "/v1/chat/completions",
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      999,
		Attempts:       1,
		PromptTokens:   9999,
		OutputTokens:   9999,
	})

	series, windowHours, bucketMinutes, err := store.QueryTimeSeries(telemetryquery.TimeSeriesRequest{
		WindowHours:   1,
		BucketMinutes: 5,
		GroupBy:       "model",
	})
	if err != nil {
		t.Fatalf("QueryTimeSeries returned error: %v", err)
	}
	if windowHours != 1 || bucketMinutes != 5 {
		t.Fatalf("unexpected normalized window/bucket: %d %d", windowHours, bucketMinutes)
	}
	if len(series) != 2 {
		t.Fatalf("expected two grouped series rows, got %d", len(series))
	}
	seriesByModel := make(map[string]telemetryquery.TimeBucket, len(series))
	for _, bucket := range series {
		seriesByModel[bucket.GroupValue] = bucket
	}
	if seriesByModel["gpt-4o"].Requests != 3 || seriesByModel["claude-sonnet-4-5"].Requests != 4 {
		t.Fatalf("unexpected grouped series: %#v", seriesByModel)
	}

	start := now.Add(-30 * time.Minute)
	end := now
	benchmarks, benchmarkHours, err := store.QueryModelBenchmark(telemetryquery.BenchmarkRequest{
		WindowHours: 24,
		Models:      []string{"gpt-4o"},
		StartTime:   &start,
		EndTime:     &end,
	})
	if err != nil {
		t.Fatalf("QueryModelBenchmark returned error: %v", err)
	}
	if benchmarkHours != 1 {
		t.Fatalf("expected explicit range to normalize to 1 hour, got %d", benchmarkHours)
	}
	if len(benchmarks) != 1 {
		t.Fatalf("expected one benchmark row, got %d", len(benchmarks))
	}

	benchmark := benchmarks[0]
	if benchmark.Model != "gpt-4o" || benchmark.Requests != 3 || benchmark.Successes != 2 || benchmark.Failures != 1 {
		t.Fatalf("unexpected benchmark row: %+v", benchmark)
	}
	if !approxEqual(benchmark.AvgLatencyMs, 700.0/3.0, 1e-9) {
		t.Fatalf("unexpected avg latency: %+v", benchmark)
	}
	if benchmark.P50LatencyMs != 200 || benchmark.P95LatencyMs != 400 || benchmark.P99LatencyMs != 400 || benchmark.MaxLatencyMs != 400 {
		t.Fatalf("unexpected percentile values: %+v", benchmark)
	}
	if !approxEqual(benchmark.SuccessRate, 66.6666666667, 1e-6) {
		t.Fatalf("unexpected success rate: %+v", benchmark)
	}
	if !approxEqual(benchmark.EstimatedCostUSD, 0.02175, 1e-9) {
		t.Fatalf("unexpected estimated cost: %+v", benchmark)
	}
}

func TestQueryModelBenchmarkGroupsByUpstream(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	start := now.Add(-30 * time.Minute)
	end := now

	for i, fact := range []testRequestFact{
		{
			EffectiveModel:       "gpt-4o",
			ProviderID:           "provider-a",
			StatusCode:           200,
			LatencyMs:            100,
			PromptTokens:         100,
			OutputTokens:         20,
			PricingStatus:        PricingStatusFixed,
			PricingTotalCostUSD:  0.10,
			PricingPromptCostUSD: 0.06,
		},
		{
			EffectiveModel:      "gpt-4o",
			ProviderID:          "provider-a",
			StatusCode:          200,
			LatencyMs:           300,
			PromptTokens:        120,
			OutputTokens:        30,
			PricingStatus:       PricingStatusEstimatedLegacy,
			PricingTotalCostUSD: 0.20,
		},
		{
			EffectiveModel:      "gpt-4o-mini",
			ProviderID:          "provider-a",
			StatusCode:          500,
			LatencyMs:           500,
			PromptTokens:        80,
			OutputTokens:        15,
			PricingStatus:       PricingStatusFixed,
			PricingTotalCostUSD: 0.05,
		},
		{
			EffectiveModel:      "claude-sonnet-4-5",
			ProviderID:          "provider-b",
			StatusCode:          200,
			LatencyMs:           50,
			PromptTokens:        90,
			OutputTokens:        10,
			PricingStatus:       PricingStatusFixed,
			PricingTotalCostUSD: 0.01,
		},
		{
			EffectiveModel:      "claude-sonnet-4-5",
			ProviderID:          "provider-b",
			StatusCode:          200,
			LatencyMs:           150,
			PromptTokens:        110,
			OutputTokens:        25,
			PricingStatus:       PricingStatusFixed,
			PricingTotalCostUSD: 0.02,
		},
	} {
		fact.EventID = fmt.Sprintf("upstream-bench-%d", i)
		fact.RequestID = fmt.Sprintf("upstream-req-%d", i)
		fact.Timestamp = now.Add(-time.Duration(i+1) * time.Minute)
		fact.Path = "/v1/chat/completions"
		fact.Attempts = 1
		insertRequestFact(t, store, fact)
	}

	benchmarks, benchmarkHours, err := store.QueryModelBenchmark(telemetryquery.BenchmarkRequest{
		Group:     "upstream",
		StartTime: &start,
		EndTime:   &end,
	})
	if err != nil {
		t.Fatalf("QueryModelBenchmark(group=upstream) returned error: %v", err)
	}
	if benchmarkHours != 1 {
		t.Fatalf("expected explicit range to normalize to 1 hour, got %d", benchmarkHours)
	}
	if len(benchmarks) != 2 {
		t.Fatalf("expected two upstream benchmark rows, got %d: %+v", len(benchmarks), benchmarks)
	}

	providerA := benchmarks[0]
	if providerA.Upstream != "provider-a" || providerA.Model != "provider-a" || providerA.Label != "provider-a" {
		t.Fatalf("unexpected upstream labels: %+v", providerA)
	}
	if providerA.Requests != 3 || providerA.Successes != 2 || providerA.Failures != 1 {
		t.Fatalf("unexpected provider-a counts: %+v", providerA)
	}
	if !approxEqual(providerA.SuccessRate, 66.6666666667, 1e-6) {
		t.Fatalf("unexpected provider-a success rate: %+v", providerA)
	}
	if providerA.P50LatencyMs != 300 || providerA.P95LatencyMs != 500 || providerA.P99LatencyMs != 500 || providerA.MaxLatencyMs != 500 {
		t.Fatalf("unexpected provider-a latency percentiles: %+v", providerA)
	}
	if !approxEqual(providerA.ExactCostUSD, 0.15, 1e-9) || !approxEqual(providerA.EstimatedLegacyCostUSD, 0.20, 1e-9) || !approxEqual(providerA.EstimatedCostUSD, 0.35, 1e-9) {
		t.Fatalf("unexpected provider-a costs: %+v", providerA)
	}

	providerB := benchmarks[1]
	if providerB.Upstream != "provider-b" || providerB.Requests != 2 || providerB.P95LatencyMs != 150 {
		t.Fatalf("unexpected provider-b row: %+v", providerB)
	}
	if !approxEqual(providerB.EstimatedCostUSD, 0.03, 1e-9) {
		t.Fatalf("unexpected provider-b total cost: %+v", providerB)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})
	return store
}

func insertRequestFact(t *testing.T, store *Store, fact testRequestFact) {
	t.Helper()

	streamInt := 0
	if fact.Stream {
		streamInt = 1
	}

	_, err := store.GetDB().Exec(`
INSERT INTO request_facts (
  event_id, request_id, timestamp, path, requested_model, effective_model,
  provider_id, route_mode, status_code, latency_ms, attempts,
  prompt_tokens, cached_prompt_tokens, completion_tokens,
  pricing_status, pricing_source_id, pricing_currency, pricing_input_per_1m, pricing_cached_input_per_1m,
  pricing_prompt_cost, pricing_completion_cost, pricing_total_cost,
  pricing_prompt_cost_usd, pricing_completion_cost_usd, pricing_total_cost_usd,
  synthetic_kind, benchmark_run_id, benchmark_target_id, benchmark_case_id, stream, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fact.EventID,
		fact.RequestID,
		fact.Timestamp.UTC().Format(time.RFC3339Nano),
		fact.Path,
		fact.RequestedModel,
		fact.EffectiveModel,
		fact.ProviderID,
		fact.RouteMode,
		fact.StatusCode,
		fact.LatencyMs,
		fact.Attempts,
		fact.PromptTokens,
		fact.CachedPromptTokens,
		fact.OutputTokens,
		fact.PricingStatus,
		fact.PricingSourceID,
		fact.PricingCurrency,
		fact.PricingInputPer1M,
		fact.PricingCachedInputPer1M,
		fact.PricingPromptCost,
		fact.PricingCompletionCost,
		fact.PricingTotalCost,
		fact.PricingPromptCostUSD,
		fact.PricingCompletionCostUSD,
		fact.PricingTotalCostUSD,
		fact.SyntheticKind,
		fact.BenchmarkRunID,
		fact.BenchmarkTargetID,
		fact.BenchmarkCaseID,
		streamInt,
		fact.ErrorMessage,
	)
	if err != nil {
		t.Fatalf("insert request fact: %v", err)
	}
}

func insertAggBucket(t *testing.T, store *Store, bucket testAggBucket) {
	t.Helper()

	_, err := store.GetDB().Exec(`
INSERT INTO agg_buckets (
  bucket, model, provider, requests, successes, failures,
  input_tokens, cached_prompt_tokens, output_tokens, total_latency
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bucket.Bucket.UTC().Format(time.RFC3339Nano),
		bucket.Model,
		bucket.Provider,
		bucket.Requests,
		bucket.Successes,
		bucket.Failures,
		bucket.InputTokens,
		bucket.CachedPromptTokens,
		bucket.OutputTokens,
		bucket.TotalLatency,
	)
	if err != nil {
		t.Fatalf("insert agg bucket: %v", err)
	}
}

func approxEqual(got, want, tolerance float64) bool {
	return math.Abs(got-want) <= tolerance
}

func TestQueryWindowMetricsDefaultWindow(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	insertAggBucket(t, store, testAggBucket{
		Bucket:       now.Add(-1 * time.Minute),
		Model:        "gpt-4o",
		Provider:     "openai",
		Requests:     3,
		Successes:    3,
		Failures:     0,
		InputTokens:  500,
		OutputTokens: 200,
		TotalLatency: 600,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:       now.Add(-10 * time.Minute),
		Model:        "gpt-4o",
		Provider:     "openai",
		Requests:     2,
		Successes:    2,
		Failures:     0,
		InputTokens:  300,
		OutputTokens: 100,
		TotalLatency: 400,
	})

	metrics, err := store.QueryWindowMetrics(0)
	if err != nil {
		t.Fatalf("QueryWindowMetrics returned error: %v", err)
	}
	if metrics.Requests != 3 {
		t.Fatalf("expected 3 requests with default window, got %d", metrics.Requests)
	}
}

func TestQueryPricingEconomicsCountsDistinctPricedModelsAndPerCurrencyTotals(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:                  "evt-fixed-usd",
		RequestID:                "req-fixed-usd",
		Timestamp:                now.Add(-1 * time.Minute),
		RequestedModel:           "router-alpha",
		EffectiveModel:           "model-alpha",
		ProviderID:               "provider-a",
		StatusCode:               200,
		PromptTokens:             1000,
		CachedPromptTokens:       100,
		OutputTokens:             500,
		PricingStatus:            PricingStatusFixed,
		PricingSourceID:          "official-openai",
		PricingCurrency:          "USD",
		PricingInputPer1M:        10,
		PricingCachedInputPer1M:  1,
		PricingPromptCost:        0.01,
		PricingCompletionCost:    0.02,
		PricingTotalCost:         0.03,
		PricingPromptCostUSD:     0.01,
		PricingCompletionCostUSD: 0.02,
		PricingTotalCostUSD:      0.03,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:             "evt-legacy-alpha",
		RequestID:           "req-legacy-alpha",
		Timestamp:           now.Add(-2 * time.Minute),
		RequestedModel:      "router-alpha",
		EffectiveModel:      "model-alpha",
		ProviderID:          "provider-a",
		StatusCode:          200,
		PromptTokens:        800,
		OutputTokens:        200,
		PricingStatus:       PricingStatusEstimatedLegacy,
		PricingTotalCostUSD: 0.05,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:                  "evt-fixed-cny",
		RequestID:                "req-fixed-cny",
		Timestamp:                now.Add(-3 * time.Minute),
		RequestedModel:           "router-beta",
		EffectiveModel:           "model-beta",
		ProviderID:               "provider-b",
		StatusCode:               200,
		PromptTokens:             1500,
		OutputTokens:             600,
		PricingStatus:            PricingStatusFixed,
		PricingSourceID:          "official-zhipu",
		PricingCurrency:          "CNY",
		PricingPromptCost:        0.20,
		PricingCompletionCost:    0.10,
		PricingTotalCost:         0.30,
		PricingPromptCostUSD:     0.027,
		PricingCompletionCostUSD: 0.0135,
		PricingTotalCostUSD:      0.0405,
	})

	economics := store.QueryPricingEconomics(24)
	if economics.Summary.ExactModels != 2 {
		t.Fatalf("ExactModels = %d, want 2", economics.Summary.ExactModels)
	}
	if economics.Summary.EstimatedModels != 1 {
		t.Fatalf("EstimatedModels = %d, want 1", economics.Summary.EstimatedModels)
	}
	if economics.Summary.PricedModels != 2 {
		t.Fatalf("PricedModels = %d, want distinct union count 2", economics.Summary.PricedModels)
	}
	if len(economics.Summary.TotalsByCurrency) != 2 {
		t.Fatalf("TotalsByCurrency len = %d, want 2", len(economics.Summary.TotalsByCurrency))
	}
	for _, total := range economics.Summary.TotalsByCurrency {
		if total.PricedModels != 1 {
			t.Fatalf("currency %s priced_models = %d, want 1", total.Currency, total.PricedModels)
		}
	}
}

func TestQueryPricingEconomicsUsesRequestedModelForBridgeTraffic(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-bridge-requested-pricing",
		RequestID:      "req-bridge-requested-pricing",
		Timestamp:      now.Add(-1 * time.Minute),
		RequestedModel: "gpt-5.5",
		EffectiveModel: "gpt-4o-mini",
		ProviderID:     "openai",
		StatusCode:     200,
		PromptTokens:   1_000,
		OutputTokens:   500,
	})

	economics := store.QueryPricingEconomics(24)
	if len(economics.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(economics.Models))
	}

	model := economics.Models[0]
	if model.DisplayModel != "gpt-5.5" {
		t.Fatalf("display model = %q, want gpt-5.5", model.DisplayModel)
	}
	if model.PricingModel != "gpt-5.5" {
		t.Fatalf("pricing model = %q, want gpt-5.5", model.PricingModel)
	}
	if model.EffectiveModel != "gpt-4o-mini" {
		t.Fatalf("effective model = %q, want gpt-4o-mini", model.EffectiveModel)
	}
	if model.PricingStatus != PricingStatusEstimatedLegacy {
		t.Fatalf("pricing status = %q, want %q", model.PricingStatus, PricingStatusEstimatedLegacy)
	}
	if !approxEqual(model.Cost.TotalUsd, 0.02, 1e-9) {
		t.Fatalf("total usd = %f, want 0.02", model.Cost.TotalUsd)
	}
}

func TestQueryWindowMetricsEmpty(t *testing.T) {
	store := newTestStore(t)

	metrics, err := store.QueryWindowMetrics(time.Hour)
	if err != nil {
		t.Fatalf("QueryWindowMetrics returned error: %v", err)
	}
	if metrics.Requests != 0 || metrics.Successes != 0 || metrics.Failures != 0 {
		t.Fatalf("expected zero metrics for empty store, got %+v", metrics)
	}
}

func TestQueryTelemetryNegativeOffset(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		Attempts:       -1,
	})

	events, total, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
		Offset:      -5,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry returned error: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("expected 1 event, got total=%d len=%d", total, len(events))
	}
	if events[0].Attempts != 0 {
		t.Fatalf("expected attempts normalized to 0, got %d", events[0].Attempts)
	}
}

func TestQueryTelemetryMaxLatencyFilter(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-fast",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      50,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-slow",
		RequestID:      "req-2",
		Timestamp:      now.Add(-2 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      500,
	})

	events, total, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
		Filters: telemetryquery.TelemetryFilters{
			MaxLatencyMs: 100,
		},
	})
	if err != nil {
		t.Fatalf("QueryTelemetry returned error: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("expected 1 filtered event, got total=%d len=%d", total, len(events))
	}
}

func TestQueryTelemetryStreamFlag(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-stream",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		Stream:         true,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-nostream",
		RequestID:      "req-2",
		Timestamp:      now.Add(-2 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		Stream:         false,
	})

	events, _, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if !events[0].Stream {
		t.Fatalf("expected first event Stream=true, got false")
	}
	if events[1].Stream {
		t.Fatalf("expected second event Stream=false, got true")
	}
}

func TestQueryTelemetrySyntheticBenchmarkFilters(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:             "evt-normal",
		RequestID:           "req-normal",
		Timestamp:           now.Add(-1 * time.Minute),
		RequestedModel:      "gpt-4o",
		EffectiveModel:      "gpt-4o",
		ProviderID:          "openai",
		StatusCode:          200,
		LatencyMs:           100,
		PromptTokens:        100,
		OutputTokens:        50,
		PricingStatus:       PricingStatusFixed,
		PricingTotalCostUSD: 0.01,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:             "evt-benchmark",
		RequestID:           "req-benchmark",
		Timestamp:           now.Add(-2 * time.Minute),
		RequestedModel:      "gpt-4o",
		EffectiveModel:      "gpt-4o-upstream",
		ProviderID:          "provider-a",
		RouteMode:           "bridged",
		StatusCode:          200,
		LatencyMs:           180,
		PromptTokens:        120,
		OutputTokens:        60,
		PricingStatus:       PricingStatusFixed,
		PricingTotalCostUSD: 0.02,
		SyntheticKind:       "benchmark",
		BenchmarkRunID:      "run-1",
		BenchmarkTargetID:   "target-1",
		BenchmarkCaseID:     "reasoning_exact",
	})

	events, total, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry(default) returned error: %v", err)
	}
	if total != 1 || len(events) != 1 || events[0].EventID != "evt-normal" {
		t.Fatalf("default telemetry should exclude synthetic benchmark event, got total=%d events=%#v", total, events)
	}

	events, total, _, err = store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
		Filters: telemetryquery.TelemetryFilters{
			SyntheticKind:  "benchmark",
			BenchmarkRunID: "run-1",
		},
	})
	if err != nil {
		t.Fatalf("QueryTelemetry(synthetic) returned error: %v", err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("synthetic telemetry total=%d len=%d, want 1", total, len(events))
	}
	if events[0].BenchmarkCaseID != "reasoning_exact" {
		t.Fatalf("BenchmarkCaseID = %q, want reasoning_exact", events[0].BenchmarkCaseID)
	}
	if events[0].BenchmarkTargetID != "target-1" {
		t.Fatalf("BenchmarkTargetID = %q, want target-1", events[0].BenchmarkTargetID)
	}
	if events[0].SyntheticKind != "benchmark" {
		t.Fatalf("SyntheticKind = %q, want benchmark", events[0].SyntheticKind)
	}
	if events[0].PricingTotalCostUSD != 0.02 {
		t.Fatalf("PricingTotalCostUSD = %v, want 0.02", events[0].PricingTotalCostUSD)
	}

	events, total, _, err = store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
		Filters: telemetryquery.TelemetryFilters{
			SyntheticKind:     "benchmark",
			BenchmarkRunID:    "run-1",
			BenchmarkTargetID: "target-1",
			BenchmarkCaseID:   "reasoning_exact",
		},
	})
	if err != nil {
		t.Fatalf("QueryTelemetry(target-filtered) returned error: %v", err)
	}
	if total != 1 || len(events) != 1 || events[0].EventID != "evt-benchmark" {
		t.Fatalf("target-filtered telemetry total=%d events=%#v, want evt-benchmark", total, events)
	}
}

func TestQueryTimeSeriesNoGroupBy(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	groupBase := now.Truncate(5 * time.Minute).Add(-5 * time.Minute)

	insertAggBucket(t, store, testAggBucket{
		Bucket:       groupBase,
		Model:        "gpt-4o",
		Provider:     "openai",
		Requests:     5,
		Successes:    5,
		Failures:     0,
		InputTokens:  1000,
		OutputTokens: 500,
		TotalLatency: 1000,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:       groupBase,
		Model:        "claude-sonnet-4-5",
		Provider:     "anthropic",
		Requests:     3,
		Successes:    3,
		Failures:     0,
		InputTokens:  600,
		OutputTokens: 300,
		TotalLatency: 600,
	})

	series, _, _, err := store.QueryTimeSeries(telemetryquery.TimeSeriesRequest{
		WindowHours:   1,
		BucketMinutes: 5,
		GroupBy:       "",
	})
	if err != nil {
		t.Fatalf("QueryTimeSeries returned error: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 aggregated bucket, got %d", len(series))
	}
	if series[0].Requests != 8 {
		t.Fatalf("expected 8 total requests, got %d", series[0].Requests)
	}
}

func TestQueryTimeSeriesGroupByProvider(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	groupBase := now.Truncate(5 * time.Minute).Add(-5 * time.Minute)

	insertAggBucket(t, store, testAggBucket{
		Bucket:       groupBase,
		Model:        "gpt-4o",
		Provider:     "openai",
		Requests:     5,
		Successes:    5,
		Failures:     0,
		InputTokens:  1000,
		OutputTokens: 500,
		TotalLatency: 1000,
	})
	insertAggBucket(t, store, testAggBucket{
		Bucket:       groupBase,
		Model:        "gpt-4o-mini",
		Provider:     "openai",
		Requests:     3,
		Successes:    3,
		Failures:     0,
		InputTokens:  600,
		OutputTokens: 300,
		TotalLatency: 600,
	})

	series, _, _, err := store.QueryTimeSeries(telemetryquery.TimeSeriesRequest{
		WindowHours:   1,
		BucketMinutes: 5,
		GroupBy:       "provider",
	})
	if err != nil {
		t.Fatalf("QueryTimeSeries returned error: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected 1 provider bucket, got %d", len(series))
	}
	if series[0].GroupValue != "openai" {
		t.Fatalf("expected GroupValue=openai, got %s", series[0].GroupValue)
	}
}

func TestQueryTimeSeriesUnsupportedGroupBy(t *testing.T) {
	store := newTestStore(t)

	_, _, _, err := store.QueryTimeSeries(telemetryquery.TimeSeriesRequest{
		WindowHours:   1,
		BucketMinutes: 5,
		GroupBy:       "invalid",
	})
	if err == nil {
		t.Fatal("expected error for unsupported groupBy")
	}
	if !strings.Contains(err.Error(), "unsupported group_by") {
		t.Fatalf("expected unsupported group_by error, got: %v", err)
	}
}

func TestQueryModelBenchmarkWithStartTimeOnly(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
	})

	start := now.Add(-30 * time.Minute)
	benchmarks, _, err := store.QueryModelBenchmark(telemetryquery.BenchmarkRequest{
		WindowHours: 24,
		StartTime:   &start,
		EndTime:     nil,
	})
	if err != nil {
		t.Fatalf("QueryModelBenchmark returned error: %v", err)
	}
	if len(benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(benchmarks))
	}
}

func TestQueryModelBenchmarkEndTimeBeforeStartTime(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
	})

	start := now.Add(-1 * time.Minute)
	end := now.Add(-2 * time.Hour)
	benchmarks, _, err := store.QueryModelBenchmark(telemetryquery.BenchmarkRequest{
		WindowHours: 24,
		StartTime:   &start,
		EndTime:     &end,
	})
	if err != nil {
		t.Fatalf("QueryModelBenchmark returned error: %v", err)
	}
	if len(benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark with fallback window, got %d", len(benchmarks))
	}
}

func TestPopulateLatencyPercentilesEvenCount(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)

	for i, latency := range []int64{100, 200, 300, 400} {
		insertRequestFact(t, store, testRequestFact{
			EventID:        fmt.Sprintf("evt-%d", i),
			RequestID:      fmt.Sprintf("req-%d", i),
			Timestamp:      now.Add(-time.Duration(i+1) * time.Minute),
			EffectiveModel: "gpt-4o",
			ProviderID:     "openai",
			StatusCode:     200,
			LatencyMs:      latency,
		})
	}

	benchmarks, _, err := store.QueryModelBenchmark(telemetryquery.BenchmarkRequest{
		WindowHours: 24,
		Models:      []string{"gpt-4o"},
	})
	if err != nil {
		t.Fatalf("QueryModelBenchmark returned error: %v", err)
	}
	if len(benchmarks) != 1 {
		t.Fatalf("expected 1 benchmark, got %d", len(benchmarks))
	}
	if benchmarks[0].P50LatencyMs != 250 {
		t.Fatalf("expected P50=250 for even count, got %v", benchmarks[0].P50LatencyMs)
	}
}

func TestNormalizeLimit(t *testing.T) {
	tests := []struct {
		value    int
		fallback int
		max      int
		want     int
	}{
		{0, 10, 100, 10},
		{-5, 10, 100, 10},
		{50, 10, 100, 50},
		{150, 10, 100, 100},
		{150, 10, 0, 150},
		{5, 10, 100, 5}, // positive value is used, not fallback
	}

	for _, tt := range tests {
		got := normalizeLimit(tt.value, tt.fallback, tt.max)
		if got != tt.want {
			t.Errorf("normalizeLimit(%d, %d, %d) = %d, want %d", tt.value, tt.fallback, tt.max, got, tt.want)
		}
	}
}

func TestPlaceholders(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, ""},
		{-1, ""},
		{1, "?"},
		{3, "?,?,?"},
	}

	for _, tt := range tests {
		got := placeholders(tt.count)
		if got != tt.want {
			t.Errorf("placeholders(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestParseStoredTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"empty string", "", false},
		{"RFC3339Nano", "2024-01-15T10:30:00.123456789Z", true},
		{"RFC3339", "2024-01-15T10:30:00Z", true},
		{"invalid format", "not-a-timestamp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStoredTimestamp(tt.input)
			if tt.valid && got.IsZero() {
				t.Errorf("parseStoredTimestamp(%q) returned zero time for valid input", tt.input)
			}
			if !tt.valid && !got.IsZero() {
				t.Errorf("parseStoredTimestamp(%q) returned non-zero time for invalid input: %v", tt.input, got)
			}
		})
	}
}

func TestCleanStringsEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"nil input", nil, nil},
		{"empty slice", []string{}, nil},
		{"all whitespace", []string{"  ", "", "\t"}, nil},
		{"duplicates", []string{"a", "a", "b", "b"}, []string{"a", "b"}},
		{"with whitespace", []string{"  a  ", "b", "\tc\t"}, []string{"a", "b", "c"}},
		{"mixed valid and empty", []string{"a", "", "b", "  ", "c"}, []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanStrings(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("cleanStrings(%v) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cleanStrings(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCleanStatusCodesEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  []int
	}{
		{"nil input", nil, nil},
		{"empty slice", []int{}, nil},
		{"all invalid", []int{0, -1, -100}, nil},
		{"duplicates", []int{200, 200, 500, 500}, []int{200, 500}},
		{"mixed valid and invalid", []int{200, 0, 500, -1, 400}, []int{200, 500, 400}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanStatusCodes(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("cleanStatusCodes(%v) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("cleanStatusCodes(%v)[%d] = %d, want %d", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestListAvailableModelsEmpty(t *testing.T) {
	store := newTestStore(t)

	models, err := store.ListAvailableModels()
	if err != nil {
		t.Fatalf("ListAvailableModels returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected empty models, got %v", models)
	}
}

func TestListAvailableModelsOnlyRequestedModel(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		RequestedModel: "claude-sonnet-4-5",
		EffectiveModel: "",
		ProviderID:     "anthropic",
		StatusCode:     200,
		LatencyMs:      100,
	})

	models, err := store.ListAvailableModels()
	if err != nil {
		t.Fatalf("ListAvailableModels returned error: %v", err)
	}
	if len(models) != 1 || models[0] != "claude-sonnet-4-5" {
		t.Fatalf("expected [claude-sonnet-4-5], got %v", models)
	}
}

// --- Cost Query Tests ---

func TestCostQueryByModel(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		PromptTokens:   1000,
		OutputTokens:   500,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-2",
		RequestID:      "req-2",
		Timestamp:      now.Add(-2 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      150,
		PromptTokens:   800,
		OutputTokens:   400,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-3",
		RequestID:      "req-3",
		Timestamp:      now.Add(-3 * time.Minute),
		EffectiveModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		StatusCode:     200,
		LatencyMs:      200,
		PromptTokens:   2000,
		OutputTokens:   600,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByModel(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ByModel returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 model entries, got %d", len(entries))
	}
	// Find gpt-4o entry
	var gpt4oEntry *CostEntry
	for i := range entries {
		if entries[i].Model == "gpt-4o" {
			gpt4oEntry = &entries[i]
			break
		}
	}
	if gpt4oEntry == nil {
		t.Fatal("expected gpt-4o entry not found")
	}
	if gpt4oEntry.PromptTokens != 1800 {
		t.Fatalf("expected prompt tokens 1800, got %d", gpt4oEntry.PromptTokens)
	}
	if gpt4oEntry.CompletionTokens != 900 {
		t.Fatalf("expected completion tokens 900, got %d", gpt4oEntry.CompletionTokens)
	}
}

func TestCostQueryByProvider(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		PromptTokens:   1000,
		OutputTokens:   500,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-2",
		RequestID:      "req-2",
		Timestamp:      now.Add(-2 * time.Minute),
		EffectiveModel: "claude-sonnet-4-5",
		ProviderID:     "anthropic",
		StatusCode:     200,
		LatencyMs:      200,
		PromptTokens:   2000,
		OutputTokens:   600,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByProvider(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ByProvider returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 provider entries, got %d", len(entries))
	}
}

func TestCostQueryByTimeRange(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		PromptTokens:   1000,
		OutputTokens:   500,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByTimeRange(context.Background(), start, end, 300)
	if err != nil {
		t.Fatalf("ByTimeRange returned error: %v", err)
	}
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 time bucket entry, got %d", len(entries))
	}
}

func TestCostQueryByTimeRangeDefaultBucket(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		PromptTokens:   1000,
		OutputTokens:   500,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	// bucketSec <= 0 should default to 300
	entries, err := cq.ByTimeRange(context.Background(), start, end, 0)
	if err != nil {
		t.Fatalf("ByTimeRange returned error: %v", err)
	}
	if len(entries) < 1 {
		t.Fatalf("expected at least 1 time bucket entry, got %d", len(entries))
	}
}

func TestCostQueryEmpty(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByModel(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ByModel returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty store, got %d", len(entries))
	}
}

// --- Store Tests ---

func TestLoadProjectionCheckpointNotFound(t *testing.T) {
	store := newTestStore(t)

	id, err := store.LoadProjectionCheckpoint(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("LoadProjectionCheckpoint returned error: %v", err)
	}
	if id != 0 {
		t.Fatalf("expected 0 for nonexistent checkpoint, got %d", id)
	}
}

func TestApplyProjectionBatchEmpty(t *testing.T) {
	store := newTestStore(t)

	count, err := store.ApplyProjectionBatch(context.Background(), "test-projection", 1, nil)
	if err != nil {
		t.Fatalf("ApplyProjectionBatch returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 for empty batch, got %d", count)
	}

	// Verify checkpoint was saved
	id, err := store.LoadProjectionCheckpoint(context.Background(), "test-projection")
	if err != nil {
		t.Fatalf("LoadProjectionCheckpoint returned error: %v", err)
	}
	if id != 1 {
		t.Fatalf("expected checkpoint 1, got %d", id)
	}
}

func TestApplyProjectionBatchWithFacts(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	facts := []ProjectionFact{
		{
			EventID:          "evt-1",
			RequestID:        "req-1",
			Timestamp:        now.Format(time.RFC3339Nano),
			Bucket:           bucket.Format(time.RFC3339Nano),
			Path:             "/v1/chat/completions",
			EffectiveModel:   "gpt-4o",
			ProviderID:       "openai",
			RouteMode:        "direct",
			StatusCode:       200,
			LatencyMs:        100,
			Attempts:         1,
			PromptTokens:     1000,
			CompletionTokens: 500,
			Stream:           true,
		},
		{
			EventID:          "evt-2",
			RequestID:        "req-2",
			Timestamp:        now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
			Bucket:           bucket.Format(time.RFC3339Nano),
			Path:             "/v1/chat/completions",
			EffectiveModel:   "gpt-4o",
			ProviderID:       "openai",
			RouteMode:        "direct",
			StatusCode:       500,
			LatencyMs:        200,
			Attempts:         1,
			PromptTokens:     800,
			CompletionTokens: 0,
			Stream:           false,
			ErrorMessage:     "upstream error",
		},
	}

	count, err := store.ApplyProjectionBatch(context.Background(), "test-projection", 10, facts)
	if err != nil {
		t.Fatalf("ApplyProjectionBatch returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 inserted, got %d", count)
	}

	// Verify checkpoint
	id, err := store.LoadProjectionCheckpoint(context.Background(), "test-projection")
	if err != nil {
		t.Fatalf("LoadProjectionCheckpoint returned error: %v", err)
	}
	if id != 10 {
		t.Fatalf("expected checkpoint 10, got %d", id)
	}

	// Verify aggregates were created
	var requests int64
	err = store.GetDB().QueryRow("SELECT SUM(requests) FROM agg_buckets").Scan(&requests)
	if err != nil {
		t.Fatalf("query agg_buckets: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected 2 total requests in agg_buckets, got %d", requests)
	}
}

func TestApplyProjectionBatchDuplicate(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	fact := ProjectionFact{
		EventID:          "evt-dup",
		RequestID:        "req-1",
		Timestamp:        now.Format(time.RFC3339Nano),
		Bucket:           bucket.Format(time.RFC3339Nano),
		Path:             "/v1/chat/completions",
		EffectiveModel:   "gpt-4o",
		ProviderID:       "openai",
		RouteMode:        "direct",
		StatusCode:       200,
		LatencyMs:        100,
		Attempts:         1,
		PromptTokens:     1000,
		CompletionTokens: 500,
	}

	// Insert once
	count, err := store.ApplyProjectionBatch(context.Background(), "test-projection", 1, []ProjectionFact{fact})
	if err != nil {
		t.Fatalf("first ApplyProjectionBatch returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 inserted, got %d", count)
	}

	// Insert same fact again (should be ignored as duplicate)
	count, err = store.ApplyProjectionBatch(context.Background(), "test-projection", 2, []ProjectionFact{fact})
	if err != nil {
		t.Fatalf("second ApplyProjectionBatch returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 inserted for duplicate, got %d", count)
	}
}

func TestNewStoreDirectoryCreation(t *testing.T) {
	// Test that NewStore creates the parent directory if it doesn't exist
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	path := filepath.Join(dir, "query.db")

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	defer store.Close()

	// Verify the directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected parent directory to be created")
	}
}

// --- Additional coverage tests ---

func TestBuildCostEntriesModelAggregation(t *testing.T) {
	// Test the model aggregation path in buildCostEntries
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	// Multiple providers for same model - should aggregate by model
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai-primary",
		StatusCode:     200,
		LatencyMs:      100,
		PromptTokens:   1000,
		OutputTokens:   500,
	})
	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-2",
		RequestID:      "req-2",
		Timestamp:      now.Add(-2 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai-backup",
		StatusCode:     200,
		LatencyMs:      150,
		PromptTokens:   800,
		OutputTokens:   400,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByModel(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ByModel returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 model entry (aggregated), got %d", len(entries))
	}
	// Should have combined tokens from both providers
	if entries[0].PromptTokens != 1800 {
		t.Fatalf("expected combined prompt tokens 1800, got %d", entries[0].PromptTokens)
	}
}

func TestCostQueryWithCachedTokens(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:            "evt-1",
		RequestID:          "req-1",
		Timestamp:          now.Add(-1 * time.Minute),
		EffectiveModel:     "gpt-4o",
		ProviderID:         "openai",
		StatusCode:         200,
		LatencyMs:          100,
		PromptTokens:       1000,
		CachedPromptTokens: 500,
		OutputTokens:       500,
	})

	cq := NewCostQuery(store.GetDB())
	start := now.Add(-5 * time.Minute)
	end := now.Add(5 * time.Minute)

	entries, err := cq.ByModel(context.Background(), start, end)
	if err != nil {
		t.Fatalf("ByModel returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].CachedTokens != 500 {
		t.Fatalf("expected cached tokens 500, got %d", entries[0].CachedTokens)
	}
}

func TestApplyProjectionBatchMultipleModels(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	facts := []ProjectionFact{
		{
			EventID:          "evt-1",
			RequestID:        "req-1",
			Timestamp:        now.Format(time.RFC3339Nano),
			Bucket:           bucket.Format(time.RFC3339Nano),
			EffectiveModel:   "gpt-4o",
			ProviderID:       "openai",
			StatusCode:       200,
			LatencyMs:        100,
			Attempts:         1,
			PromptTokens:     1000,
			CompletionTokens: 500,
		},
		{
			EventID:          "evt-2",
			RequestID:        "req-2",
			Timestamp:        now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
			Bucket:           bucket.Format(time.RFC3339Nano),
			EffectiveModel:   "claude-sonnet-4-5",
			ProviderID:       "anthropic",
			StatusCode:       200,
			LatencyMs:        200,
			Attempts:         1,
			PromptTokens:     2000,
			CompletionTokens: 600,
		},
	}

	count, err := store.ApplyProjectionBatch(context.Background(), "test-projection", 5, facts)
	if err != nil {
		t.Fatalf("ApplyProjectionBatch returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 inserted, got %d", count)
	}

	// Check aggregates for both models
	var buckets []string
	rows, err := store.GetDB().Query("SELECT DISTINCT model FROM agg_buckets ORDER BY model")
	if err != nil {
		t.Fatalf("query agg_buckets: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			t.Fatalf("scan model: %v", err)
		}
		buckets = append(buckets, model)
	}
	if len(buckets) != 2 {
		t.Fatalf("expected 2 models in agg_buckets, got %d", len(buckets))
	}
}

func TestApplyProjectionBatchWithCachedTokens(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	facts := []ProjectionFact{
		{
			EventID:            "evt-1",
			RequestID:          "req-1",
			Timestamp:          now.Format(time.RFC3339Nano),
			Bucket:             bucket.Format(time.RFC3339Nano),
			EffectiveModel:     "gpt-4o",
			ProviderID:         "openai",
			StatusCode:         200,
			LatencyMs:          100,
			Attempts:           1,
			PromptTokens:       1000,
			CachedPromptTokens: 200,
			CompletionTokens:   500,
		},
	}

	count, err := store.ApplyProjectionBatch(context.Background(), "test-projection", 1, facts)
	if err != nil {
		t.Fatalf("ApplyProjectionBatch returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 inserted, got %d", count)
	}

	var cachedTokens int64
	err = store.GetDB().QueryRow("SELECT cached_prompt_tokens FROM agg_buckets WHERE model = 'gpt-4o'").Scan(&cachedTokens)
	if err != nil {
		t.Fatalf("query cached tokens: %v", err)
	}
	if cachedTokens != 200 {
		t.Fatalf("expected cached_prompt_tokens 200, got %d", cachedTokens)
	}
}

func TestQueryTelemetryWithNegativeAttempts(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	insertRequestFact(t, store, testRequestFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Add(-1 * time.Minute),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
		Attempts:       -5, // Should be normalized to 0 in the query result
	})

	events, _, _, err := store.QueryTelemetry(telemetryquery.TelemetryRequest{
		WindowHours: 24,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("QueryTelemetry returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Attempts != 0 {
		t.Fatalf("expected attempts normalized to 0, got %d", events[0].Attempts)
	}
}

func TestLoadProjectionCheckpointExisting(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	// Insert facts to create a checkpoint
	facts := []ProjectionFact{
		{
			EventID:        "evt-1",
			RequestID:      "req-1",
			Timestamp:      now.Format(time.RFC3339Nano),
			Bucket:         bucket.Format(time.RFC3339Nano),
			EffectiveModel: "gpt-4o",
			ProviderID:     "openai",
			StatusCode:     200,
			LatencyMs:      100,
		},
	}

	_, err := store.ApplyProjectionBatch(context.Background(), "my-projection", 42, facts)
	if err != nil {
		t.Fatalf("ApplyProjectionBatch returned error: %v", err)
	}

	// Load the checkpoint
	id, err := store.LoadProjectionCheckpoint(context.Background(), "my-projection")
	if err != nil {
		t.Fatalf("LoadProjectionCheckpoint returned error: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected checkpoint 42, got %d", id)
	}
}

func TestApplyProjectionBatchCheckpointUpdate(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Minute)
	bucket := now.Truncate(5 * time.Minute)

	fact := ProjectionFact{
		EventID:        "evt-1",
		RequestID:      "req-1",
		Timestamp:      now.Format(time.RFC3339Nano),
		Bucket:         bucket.Format(time.RFC3339Nano),
		EffectiveModel: "gpt-4o",
		ProviderID:     "openai",
		StatusCode:     200,
		LatencyMs:      100,
	}

	// First batch with checkpoint 10
	_, err := store.ApplyProjectionBatch(context.Background(), "test", 10, []ProjectionFact{fact})
	if err != nil {
		t.Fatalf("first batch: %v", err)
	}

	// Second batch with checkpoint 20 (higher)
	fact.EventID = "evt-2"
	fact.RequestID = "req-2"
	_, err = store.ApplyProjectionBatch(context.Background(), "test", 20, []ProjectionFact{fact})
	if err != nil {
		t.Fatalf("second batch: %v", err)
	}

	// Checkpoint should be 20
	id, err := store.LoadProjectionCheckpoint(context.Background(), "test")
	if err != nil {
		t.Fatalf("LoadProjectionCheckpoint: %v", err)
	}
	if id != 20 {
		t.Fatalf("expected checkpoint 20, got %d", id)
	}
}

func TestNewStoreInvalidPath(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file parent: %v", err)
	}
	_, err := NewStore(filepath.Join(notDir, "query.db"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNewStoreExistingDirectory(t *testing.T) {
	// Test that NewStore works when directory already exists
	dir := t.TempDir()
	path := filepath.Join(dir, "query.db")

	// Create directory first
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	defer store.Close()
}
