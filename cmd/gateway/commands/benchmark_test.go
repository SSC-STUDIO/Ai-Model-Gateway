package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/cli"
)

func TestBenchmarkCommandTelemetryText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.String(); got != "/api/admin/benchmark/runs/run-1/telemetry?hours=24&limit=200" {
			t.Fatalf("unexpected path: %s", got)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Fatalf("unexpected authorization header: %q", auth)
		}
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 1,
			Events: []cli.EventRecord{
				{
					RequestID:          "req-1",
					Timestamp:          time.Date(2026, time.April, 22, 12, 34, 56, 0, time.UTC),
					Provider:           "provider-a",
					RequestedModel:     "gpt-4o",
					EffectiveModel:     "claude-3-7-sonnet",
					BenchmarkCaseID:    "reasoning_exact",
					RouteMode:          "bridged",
					StatusCode:         http.StatusOK,
					LatencyMs:          321,
					InputTokens:        100,
					CachedPromptTokens: 20,
					OutputTokens:       80,
					TotalCostUSD:       0.0123,
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.Telemetry(context.Background(), "run-1", nil, "text"); err != nil {
		t.Fatalf("Telemetry returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"Run run-1 telemetry fetched=1 available=1 truncated=false",
		"TIME",
		"CASE",
		"reasoning_exact",
		"provider-a",
		"claude-3-7-sonnet",
		"321ms",
		"200",
		"$0.012300",
		"bridged",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestBenchmarkCommandTelemetryTextMarksTruncatedResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 5,
			Events: []cli.EventRecord{
				{
					RequestID:      "req-1",
					Timestamp:      time.Date(2026, time.April, 22, 12, 34, 56, 0, time.UTC),
					Provider:       "provider-a",
					RequestedModel: "gpt-4o",
					StatusCode:     http.StatusOK,
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.Telemetry(context.Background(), "run-trunc", nil, "text"); err != nil {
		t.Fatalf("Telemetry returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Run run-trunc telemetry fetched=1 available=5 truncated=true") {
		t.Fatalf("expected truncation banner, got:\n%s", out.String())
	}
}

func TestBenchmarkCommandTelemetryCSV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 1,
			Events: []cli.EventRecord{
				{
					EventID:            "evt-1",
					RequestID:          "req-2",
					Timestamp:          time.Date(2026, time.April, 22, 1, 2, 3, 0, time.UTC),
					Path:               "/v1/chat/completions",
					Provider:           "provider-b",
					RequestedModel:     "gpt-5",
					EffectiveModel:     "claude-opus",
					RouteMode:          "bridge_fallback",
					StatusCode:         http.StatusBadGateway,
					LatencyMs:          456,
					InputTokens:        90,
					CachedPromptTokens: 10,
					OutputTokens:       30,
					PricingStatus:      "fixed",
					TotalCostUSD:       0.0456,
					SyntheticKind:      "benchmark",
					BenchmarkRunID:     "run-9",
					BenchmarkTargetID:  "target-9",
					BenchmarkCaseID:    "tool_json",
					Error:              "bad upstream payload",
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.Telemetry(context.Background(), "run-9", nil, "csv"); err != nil {
		t.Fatalf("Telemetry returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"timestamp,event_id,request_id,benchmark_run_id,benchmark_target_id,benchmark_case_id,provider,requested_model,effective_model,path,route_mode,status_code,latency_ms,input_tokens,cached_prompt_tokens,output_tokens,total_tokens,pricing_status,total_cost_usd,synthetic_kind,error",
		"2026-04-22T01:02:03Z,evt-1,req-2,run-9,target-9,tool_json,provider-b,gpt-5,claude-opus,/v1/chat/completions,bridge_fallback,502,456,90,10,30,130,fixed,0.045600,benchmark,bad upstream payload",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected csv output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestBenchmarkCommandTelemetryCSVLeavesEffectiveModelBlankWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 1,
			Events: []cli.EventRecord{
				{
					RequestID:      "req-blank",
					Timestamp:      time.Date(2026, time.April, 22, 1, 2, 3, 0, time.UTC),
					Provider:       "provider-b",
					RequestedModel: "gpt-5",
					StatusCode:     http.StatusOK,
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.Telemetry(context.Background(), "run-blank", nil, "csv"); err != nil {
		t.Fatalf("Telemetry returned error: %v", err)
	}

	if !strings.Contains(out.String(), "req-blank,,,,") {
		t.Fatalf("expected blank effective model column, got:\n%s", out.String())
	}
}

func TestBenchmarkCommandTelemetrySummaryText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 3,
			Events: []cli.EventRecord{
				{
					RequestID:          "req-1",
					Timestamp:          time.Date(2026, time.April, 22, 2, 0, 0, 0, time.UTC),
					Provider:           "provider-a",
					RequestedModel:     "gpt-4o",
					EffectiveModel:     "claude-3-7-sonnet",
					BenchmarkCaseID:    "reasoning_exact",
					RouteMode:          "bridged",
					StatusCode:         http.StatusOK,
					LatencyMs:          200,
					InputTokens:        100,
					CachedPromptTokens: 20,
					OutputTokens:       80,
					PricingStatus:      "fixed",
					TotalCostUSD:       0.0100,
				},
				{
					RequestID:          "req-2",
					Timestamp:          time.Date(2026, time.April, 22, 2, 1, 0, 0, time.UTC),
					Provider:           "provider-a",
					RequestedModel:     "gpt-4o",
					EffectiveModel:     "claude-3-7-sonnet",
					BenchmarkCaseID:    "reasoning_exact",
					RouteMode:          "bridged",
					StatusCode:         http.StatusOK,
					LatencyMs:          400,
					InputTokens:        150,
					CachedPromptTokens: 0,
					OutputTokens:       50,
					PricingStatus:      "fixed",
					TotalCostUSD:       0.0200,
				},
				{
					RequestID:          "req-3",
					Timestamp:          time.Date(2026, time.April, 22, 2, 2, 0, 0, time.UTC),
					Provider:           "provider-b",
					RequestedModel:     "gpt-5",
					EffectiveModel:     "gpt-5",
					BenchmarkCaseID:    "tool_json",
					RouteMode:          "bridge_fallback",
					StatusCode:         http.StatusBadGateway,
					LatencyMs:          600,
					InputTokens:        50,
					CachedPromptTokens: 10,
					OutputTokens:       40,
					PricingStatus:      "unpriced",
					TotalCostUSD:       0,
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.TelemetrySummary(context.Background(), "run-11", nil, "text"); err != nil {
		t.Fatalf("TelemetrySummary returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"Run run-11 telemetry summary fetched=3 available=3 truncated=false groups=2 success=2 failed=1 total_tokens=500 total_cost=$0.030000",
		"CASE",
		"reasoning_exact",
		"provider-a",
		"claude-3-7-sonnet",
		"bridged",
		"fixed",
		"2",
		"300.0ms",
		"400",
		"200.0",
		"$0.030000",
		"tool_json",
		"provider-b",
		"bridge_fallback",
		"unpriced",
		"600.0ms",
		"100",
		"100.0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected summary output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestBenchmarkCommandTelemetrySummaryCSV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
			Total: 1,
			Events: []cli.EventRecord{
				{
					RequestID:       "req-4",
					Timestamp:       time.Date(2026, time.April, 22, 3, 4, 5, 0, time.UTC),
					Provider:        "provider-c",
					RequestedModel:  "gpt-4.1",
					EffectiveModel:  "gpt-4.1",
					BenchmarkCaseID: "instruction_compact",
					RouteMode:       "direct",
					StatusCode:      http.StatusOK,
					LatencyMs:       250,
					InputTokens:     40,
					OutputTokens:    20,
					PricingStatus:   "fixed",
					TotalCostUSD:    0.0042,
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.TelemetrySummary(context.Background(), "run-12", nil, "csv"); err != nil {
		t.Fatalf("TelemetrySummary returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"run_id,case_id,provider,model,route_mode,pricing_status,requests,successes,failures,avg_latency_ms,total_tokens,avg_tokens,total_cost_usd,first_seen_at,last_seen_at",
		"run-12,instruction_compact,provider-c,gpt-4.1,direct,fixed,1,1,0,250.00,60,60.00,0.004200,2026-04-22T03:04:05Z,2026-04-22T03:04:05Z",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected summary csv output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestBenchmarkCommandTargetSummaryText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/admin/benchmark/runs/run-21":
			if err := json.NewEncoder(w).Encode(cli.VerificationRunDetail{
				VerificationRunSummary: cli.VerificationRunSummary{
					RunID:        "run-21",
					Status:       "completed",
					SuiteVersion: "general_protocol_v1",
					Protocol:     "openai_chat_completions",
				},
				Targets: []cli.VerificationRunTarget{
					{
						TargetID:         "target-2",
						ProviderID:       "provider-b",
						PublicModel:      "gpt-5",
						EffectiveModel:   "gpt-5",
						Status:           "completed",
						Verdict:          "normal",
						PublicGap:        3.0,
						VendorGap:        2.0,
						SuspicionScore:   5.0,
						CompletionRate:   1.0,
						EstimatedCostUSD: 0.0042,
					},
					{
						TargetID:                 "target-1",
						ProviderID:               "provider-a",
						PublicModel:              "gpt-4o",
						EffectiveModel:           "claude-3-7-sonnet",
						Status:                   "completed",
						Verdict:                  "suspect",
						PublicGap:                12.5,
						VendorGap:                4.0,
						SuspicionScore:           36.0,
						CriticalProtocolFailures: 1,
						CompletionRate:           1.0,
						ReasonCodes:              []string{"critical_tool_failed"},
						EstimatedCostUSD:         0.0300,
					},
				},
			}); err != nil {
				t.Fatalf("encode run detail: %v", err)
			}
		case "/api/admin/benchmark/runs/run-21/telemetry?hours=24&limit=200":
			if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
				Total: 3,
				Events: []cli.EventRecord{
					{
						RequestID:          "req-1",
						Timestamp:          time.Date(2026, time.April, 22, 4, 0, 0, 0, time.UTC),
						Provider:           "provider-a",
						RequestedModel:     "gpt-4o",
						EffectiveModel:     "claude-3-7-sonnet",
						RouteMode:          "bridged",
						StatusCode:         http.StatusOK,
						LatencyMs:          200,
						InputTokens:        100,
						CachedPromptTokens: 20,
						OutputTokens:       80,
						PricingStatus:      "fixed",
						TotalCostUSD:       0.0100,
					},
					{
						RequestID:      "req-2",
						Timestamp:      time.Date(2026, time.April, 22, 4, 1, 0, 0, time.UTC),
						Provider:       "provider-a",
						RequestedModel: "gpt-4o",
						EffectiveModel: "claude-3-7-sonnet",
						RouteMode:      "bridge_fallback",
						StatusCode:     http.StatusBadGateway,
						LatencyMs:      400,
						InputTokens:    150,
						OutputTokens:   50,
						PricingStatus:  "fixed",
						TotalCostUSD:   0.0200,
					},
					{
						RequestID:      "req-3",
						Timestamp:      time.Date(2026, time.April, 22, 4, 2, 0, 0, time.UTC),
						Provider:       "provider-b",
						RequestedModel: "gpt-5",
						EffectiveModel: "gpt-5",
						RouteMode:      "direct",
						StatusCode:     http.StatusOK,
						LatencyMs:      250,
						InputTokens:    40,
						OutputTokens:   20,
						PricingStatus:  "fixed",
						TotalCostUSD:   0.0042,
					},
				},
			}); err != nil {
				t.Fatalf("encode telemetry: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.TargetSummary(context.Background(), "run-21", nil, "severity", "text"); err != nil {
		t.Fatalf("TargetSummary returned error: %v", err)
	}

	output := out.String()
	indexA := strings.Index(output, "provider-a")
	indexB := strings.Index(output, "provider-b")
	if indexA == -1 || indexB == -1 {
		t.Fatalf("expected provider rows in output, got:\n%s", output)
	}
	if indexA >= indexB {
		t.Fatalf("expected severity sorting to place provider-a before provider-b, got:\n%s", output)
	}

	for _, want := range []string{
		"Run run-21 target summary targets=2 fetched=3 available=3 truncated=false matched=3 exact=0 legacy=3 unmatched=0 total_tokens=460 total_cost=$0.034200",
		"CRIT",
		"EXACT",
		"LEGACY",
		"FAIL RATE",
		"provider-a",
		"gpt-4o",
		"claude-3-7-sonnet",
		"suspect",
		"12.5",
		"36.0",
		"50.0%",
		"2",
		"1",
		"300.0ms",
		"400",
		"$0.030000",
		"bridge_fallback=1,bridged=1",
		"fixed=2",
		"critical_tool_failed",
		"provider-b",
		"normal",
		"0.0%",
		"60",
		"$0.004200",
		"direct=1",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected target summary output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestBenchmarkCommandTargetSummaryTextSortByLatency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/admin/benchmark/runs/run-23":
			if err := json.NewEncoder(w).Encode(cli.VerificationRunDetail{
				VerificationRunSummary: cli.VerificationRunSummary{
					RunID:        "run-23",
					Status:       "completed",
					SuiteVersion: "general_protocol_v1",
					Protocol:     "openai_chat_completions",
				},
				Targets: []cli.VerificationRunTarget{
					{
						TargetID:       "target-fast",
						ProviderID:     "provider-fast",
						PublicModel:    "gpt-fast",
						EffectiveModel: "gpt-fast",
						Status:         "completed",
						Verdict:        "normal",
					},
					{
						TargetID:       "target-slow",
						ProviderID:     "provider-slow",
						PublicModel:    "gpt-slow",
						EffectiveModel: "gpt-slow",
						Status:         "completed",
						Verdict:        "normal",
					},
				},
			}); err != nil {
				t.Fatalf("encode run detail: %v", err)
			}
		case "/api/admin/benchmark/runs/run-23/telemetry?hours=24&limit=200":
			if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
				Total: 2,
				Events: []cli.EventRecord{
					{
						RequestID:      "req-fast",
						Timestamp:      time.Date(2026, time.April, 22, 6, 0, 0, 0, time.UTC),
						Provider:       "provider-fast",
						RequestedModel: "gpt-fast",
						EffectiveModel: "gpt-fast",
						RouteMode:      "direct",
						StatusCode:     http.StatusOK,
						LatencyMs:      100,
						InputTokens:    10,
						OutputTokens:   10,
					},
					{
						RequestID:      "req-slow",
						Timestamp:      time.Date(2026, time.April, 22, 6, 1, 0, 0, time.UTC),
						Provider:       "provider-slow",
						RequestedModel: "gpt-slow",
						EffectiveModel: "gpt-slow",
						RouteMode:      "direct",
						StatusCode:     http.StatusOK,
						LatencyMs:      900,
						InputTokens:    10,
						OutputTokens:   10,
					},
				},
			}); err != nil {
				t.Fatalf("encode telemetry: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.TargetSummary(context.Background(), "run-23", nil, "latency", "text"); err != nil {
		t.Fatalf("TargetSummary returned error: %v", err)
	}

	output := out.String()
	indexSlow := strings.Index(output, "provider-slow")
	indexFast := strings.Index(output, "provider-fast")
	if indexSlow == -1 || indexFast == -1 {
		t.Fatalf("expected provider rows in output, got:\n%s", output)
	}
	if indexSlow >= indexFast {
		t.Fatalf("expected latency sorting to place slower target first, got:\n%s", output)
	}
}

func TestBenchmarkCommandTargetSummaryCSV(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.String() {
		case "/api/admin/benchmark/runs/run-22":
			if err := json.NewEncoder(w).Encode(cli.VerificationRunDetail{
				VerificationRunSummary: cli.VerificationRunSummary{
					RunID:        "run-22",
					Status:       "completed",
					SuiteVersion: "general_protocol_v1",
					Protocol:     "anthropic_messages",
				},
				Targets: []cli.VerificationRunTarget{
					{
						TargetID:                 "target-3",
						ProviderID:               "provider-c",
						PublicModel:              "claude-sonnet",
						EffectiveModel:           "claude-3-7-sonnet",
						Status:                   "completed",
						Verdict:                  "highly_suspect",
						PublicGap:                24.0,
						VendorGap:                18.0,
						SuspicionScore:           81.0,
						CompletionRate:           0.83,
						CriticalProtocolFailures: 2,
						ReasonCodes:              []string{"public_gap_high", "protocol_failure"},
						EstimatedCostUSD:         0.1200,
					},
				},
			}); err != nil {
				t.Fatalf("encode run detail: %v", err)
			}
		case "/api/admin/benchmark/runs/run-22/telemetry?hours=24&limit=200":
			if err := json.NewEncoder(w).Encode(cli.TelemetryResult{
				Total: 1,
				Events: []cli.EventRecord{
					{
						RequestID:      "req-4",
						Timestamp:      time.Date(2026, time.April, 22, 5, 6, 7, 0, time.UTC),
						Provider:       "provider-c",
						RequestedModel: "claude-sonnet",
						EffectiveModel: "claude-3-7-sonnet",
						RouteMode:      "bridged",
						StatusCode:     http.StatusBadGateway,
						LatencyMs:      500,
						InputTokens:    120,
						OutputTokens:   60,
						PricingStatus:  "unpriced",
					},
				},
			}); err != nil {
				t.Fatalf("encode telemetry: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "test-token")
	var out bytes.Buffer
	cmd := NewBenchmarkCommand(client, &out)

	if err := cmd.TargetSummary(context.Background(), "run-22", nil, "severity", "csv"); err != nil {
		t.Fatalf("TargetSummary returned error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"run_id,target_id,provider_id,public_model,effective_model,status,verdict,public_gap,vendor_gap,suspicion_score,completion_rate,critical_protocol_failures,target_estimated_cost_usd,requests,exact_identity_events,legacy_identity_events,successes,failures,failure_rate,avg_latency_ms,total_tokens,telemetry_cost_usd,route_mode_counts,pricing_status_counts,reason_codes,first_seen_at,last_seen_at",
		"run-22,target-3,provider-c,claude-sonnet,claude-3-7-sonnet,completed,highly_suspect,24.0,18.0,81.0,0.83,2,0.120000,1,0,1,0,1,1.0000,500.00,180,0.000000,bridged=1,unpriced=1,public_gap_high;protocol_failure,2026-04-22T05:06:07Z,2026-04-22T05:06:07Z",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected target summary csv output to contain %q, got:\n%s", want, output)
		}
	}
}

func TestSortBenchmarkTargetSummaryTargetsByProviderAndCost(t *testing.T) {
	t.Run("provider", func(t *testing.T) {
		targets := []benchmarkTargetSummaryTarget{
			{TargetID: "b-2", ProviderID: "provider-b", PublicModel: "model-2", EffectiveModel: "eff-2"},
			{TargetID: "a-2", ProviderID: "provider-a", PublicModel: "model-2", EffectiveModel: "eff-2"},
			{TargetID: "a-1", ProviderID: "provider-a", PublicModel: "model-1", EffectiveModel: "eff-1"},
		}
		sortBenchmarkTargetSummaryTargets(targets, "provider")
		got := []string{targets[0].TargetID, targets[1].TargetID, targets[2].TargetID}
		want := []string{"a-1", "a-2", "b-2"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("provider sort order = %#v, want %#v", got, want)
			}
		}
	})

	t.Run("cost", func(t *testing.T) {
		targets := []benchmarkTargetSummaryTarget{
			{TargetID: "low", ProviderID: "provider-a", PublicModel: "model-a", TelemetryCostUSD: 0.01},
			{TargetID: "high", ProviderID: "provider-b", PublicModel: "model-b", TelemetryCostUSD: 0.12},
			{TargetID: "mid", ProviderID: "provider-c", PublicModel: "model-c", TelemetryCostUSD: 0.05},
		}
		sortBenchmarkTargetSummaryTargets(targets, "cost")
		got := []string{targets[0].TargetID, targets[1].TargetID, targets[2].TargetID}
		want := []string{"high", "mid", "low"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("cost sort order = %#v, want %#v", got, want)
			}
		}
	})
}

func TestFindBenchmarkTargetIndex(t *testing.T) {
	targets := []cli.VerificationRunTarget{
		{TargetID: "public-only", ProviderID: "provider-a", PublicModel: "router-model"},
		{TargetID: "exact", ProviderID: "provider-a", PublicModel: "router-model", EffectiveModel: "claude-3-7-sonnet"},
		{TargetID: "effective-only", ProviderID: "provider-a", PublicModel: "other-model", EffectiveModel: "gpt-5"},
	}

	tests := []struct {
		name  string
		event cli.EventRecord
		want  int
	}{
		{
			name: "prefer exact requested and effective match",
			event: cli.EventRecord{
				Provider:       "provider-a",
				RequestedModel: "router-model",
				EffectiveModel: "claude-3-7-sonnet",
			},
			want: 1,
		},
		{
			name: "fall back to public model when effective model missing",
			event: cli.EventRecord{
				Provider:       "provider-a",
				RequestedModel: "router-model",
			},
			want: 0,
		},
		{
			name: "fall back to effective model only",
			event: cli.EventRecord{
				Provider:       "provider-a",
				RequestedModel: "unrelated-model",
				EffectiveModel: "gpt-5",
			},
			want: 2,
		},
		{
			name: "return -1 when provider or model do not match",
			event: cli.EventRecord{
				Provider:       "provider-b",
				RequestedModel: "router-model",
				EffectiveModel: "claude-3-7-sonnet",
			},
			want: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := findBenchmarkTargetIndex(targets, tc.event); got != tc.want {
				t.Fatalf("findBenchmarkTargetIndex() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSummarizeBenchmarkTargetsTracksMatchedAndUnmatchedEvents(t *testing.T) {
	detail := &cli.VerificationRunDetail{
		VerificationRunSummary: cli.VerificationRunSummary{
			RunID:        "run-match",
			Status:       "completed",
			SuiteVersion: "general_protocol_v1",
			Protocol:     "openai_chat_completions",
		},
		Targets: []cli.VerificationRunTarget{
			{
				TargetID:       "target-1",
				ProviderID:     "provider-a",
				PublicModel:    "router-model",
				EffectiveModel: "claude-3-7-sonnet",
				Status:         "completed",
				Verdict:        "normal",
			},
		},
	}
	telemetry := &cli.TelemetryResult{
		Total: 3,
		Events: []cli.EventRecord{
			{
				RequestID:         "req-match-1",
				Timestamp:         time.Date(2026, time.April, 22, 7, 0, 0, 0, time.UTC),
				Provider:          "provider-a",
				RequestedModel:    "router-model",
				EffectiveModel:    "claude-3-7-sonnet",
				BenchmarkTargetID: "target-1",
				RouteMode:         "bridged",
				StatusCode:        http.StatusOK,
				LatencyMs:         100,
				InputTokens:       10,
				OutputTokens:      5,
				PricingStatus:     "fixed",
				TotalCostUSD:      0.01,
			},
			{
				RequestID:      "req-match-2",
				Timestamp:      time.Date(2026, time.April, 22, 7, 1, 0, 0, time.UTC),
				Provider:       "provider-a",
				RequestedModel: "router-model",
				EffectiveModel: "claude-3-7-sonnet",
				RouteMode:      "bridged",
				StatusCode:     http.StatusBadGateway,
				LatencyMs:      300,
				InputTokens:    20,
				OutputTokens:   10,
				PricingStatus:  "fixed",
				TotalCostUSD:   0.02,
			},
			{
				RequestID:      "req-unmatched",
				Timestamp:      time.Date(2026, time.April, 22, 7, 2, 0, 0, time.UTC),
				Provider:       "provider-b",
				RequestedModel: "router-model",
				EffectiveModel: "gpt-4o",
				RouteMode:      "direct",
				StatusCode:     http.StatusOK,
				LatencyMs:      50,
				InputTokens:    5,
				OutputTokens:   5,
				PricingStatus:  "fixed",
				TotalCostUSD:   0.03,
			},
		},
	}

	report := summarizeBenchmarkTargets(detail, telemetry, "severity")
	if report.MatchedEvents != 2 || report.ExactMatchedEvents != 1 || report.LegacyMatchedEvents != 1 || report.UnmatchedEvents != 1 {
		t.Fatalf("matched/exact/legacy/unmatched = %d/%d/%d/%d, want 2/1/1/1", report.MatchedEvents, report.ExactMatchedEvents, report.LegacyMatchedEvents, report.UnmatchedEvents)
	}
	if report.TotalEvents != 3 || report.AvailableEvents != 3 || report.Truncated {
		t.Fatalf("unexpected pagination metadata: %+v", report)
	}
	target := report.Targets[0]
	if target.Requests != 2 || target.Successes != 1 || target.Failures != 1 {
		t.Fatalf("unexpected target request counts: %+v", target)
	}
	if target.ExactIdentityEvents != 1 || target.LegacyIdentityEvents != 1 {
		t.Fatalf("unexpected target identity counts: %+v", target)
	}
	if target.TotalTokens != 45 {
		t.Fatalf("target.TotalTokens = %d, want 45", target.TotalTokens)
	}
	if target.TelemetryCostUSD != 0.03 {
		t.Fatalf("target.TelemetryCostUSD = %f, want 0.03", target.TelemetryCostUSD)
	}
}
