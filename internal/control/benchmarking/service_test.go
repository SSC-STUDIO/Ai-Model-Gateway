package benchmarking

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/core"
)

func TestImportBaselineJSONAndList(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, nil, nil)
	snapshot, err := service.ImportBaseline(context.Background(), ImportBaselineRequest{
		Kind:       BaselineKindPublicStandard,
		SourceName: "test-json",
		FileName:   "baseline.json",
		Contents:   `[{"canonical_model_id":"model-a","metric_name":"reasoning","score":91,"scale_max":100}]`,
	})
	if err != nil {
		t.Fatalf("ImportBaseline() error = %v", err)
	}
	if snapshot.RowCount != 1 {
		t.Fatalf("snapshot.RowCount = %d, want 1", snapshot.RowCount)
	}

	items, err := service.ListBaselineSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListBaselineSnapshots() error = %v", err)
	}
	if len(items) != 1 || items[0].SnapshotID != snapshot.SnapshotID {
		t.Fatalf("ListBaselineSnapshots() = %#v, want snapshot %s", items, snapshot.SnapshotID)
	}
}

func TestStartRunCompletesWithFakeGatewayRunner(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "provider-a", Models: []string{"model-a"}, BaseURL: "https://provider-a.example"},
			{Name: "judge-provider", Models: []string{"judge-model"}, BaseURL: "https://judge.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Enabled:      true,
			DefaultSuite: core.BenchmarkSuiteGeneralProtocolV1,
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "judge-provider",
				PublicModel: "judge-model",
				TimeoutMs:   5000,
			},
			Limits: core.BenchmarkLimitsConfig{
				MaxParallelRuns:  1,
				MaxParallelCases: 2,
				PerCaseTimeoutMs: 5000,
			},
			VerdictThresholds: core.BenchmarkVerdictThresholds{
				NormalMaxGap:                8,
				SuspectMaxGap:               20,
				HighSuspectProtocolFailures: 2,
			},
			Aliases: []core.BenchmarkAliasConfig{
				{
					CanonicalModelID: "model-a-canonical",
					Provider:         "provider-a",
					Models:           []string{"model-a", "model-a-upstream"},
				},
			},
		},
	}
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	baseline, err := service.ImportBaseline(context.Background(), ImportBaselineRequest{
		Kind:       BaselineKindPublicStandard,
		SourceName: "public-baseline",
		FileName:   "baseline.json",
		Contents: `[
		  {"canonical_model_id":"model-a-canonical","metric_name":"reasoning","score":90,"scale_max":100},
		  {"canonical_model_id":"model-a-canonical","metric_name":"coding_proxy","score":90,"scale_max":100},
		  {"canonical_model_id":"model-a-canonical","metric_name":"instruction","score":100,"scale_max":100},
		  {"canonical_model_id":"model-a-canonical","metric_name":"tool_json","score":100,"scale_max":100},
		  {"canonical_model_id":"model-a-canonical","metric_name":"stream_protocol","score":100,"scale_max":100}
		]`,
	})
	if err != nil {
		t.Fatalf("ImportBaseline() error = %v", err)
	}

	run, err := service.StartRun(context.Background(), StartRunRequest{
		ProviderID:       "provider-a",
		PublicModel:      "model-a",
		PublicSnapshotID: baseline.SnapshotID,
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	var final *RunDetail
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		final, err = service.GetRun(context.Background(), run.RunID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if final != nil && final.Status != RunStatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final == nil {
		t.Fatal("expected run detail")
	}
	if final.Status != RunStatusCompleted {
		t.Fatalf("final.Status = %s, want %s", final.Status, RunStatusCompleted)
	}
	if len(final.Targets) != 1 {
		t.Fatalf("len(final.Targets) = %d, want 1", len(final.Targets))
	}
	target := final.Targets[0]
	if target.Status != TargetStatusCompleted {
		t.Fatalf("target.Status = %s, want %s", target.Status, TargetStatusCompleted)
	}
	if target.Verdict != VerdictNormal {
		t.Fatalf("target.Verdict = %s, want %s", target.Verdict, VerdictNormal)
	}
	if target.EffectiveModel != "model-a-upstream" {
		t.Fatalf("target.EffectiveModel = %q, want model-a-upstream", target.EffectiveModel)
	}
	if len(target.Cases) != 7 {
		t.Fatalf("len(target.Cases) = %d, want 7", len(target.Cases))
	}
}

func TestStartRunRejectsMissingBaselineSnapshot(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "provider-a", Models: []string{"model-a"}, BaseURL: "https://provider-a.example"},
			{Name: "judge-provider", Models: []string{"judge-model"}, BaseURL: "https://judge.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Enabled:      true,
			DefaultSuite: core.BenchmarkSuiteGeneralProtocolV1,
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "judge-provider",
				PublicModel: "judge-model",
			},
		},
	}
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	_, err = service.StartRun(context.Background(), StartRunRequest{
		ProviderID:       "provider-a",
		PublicModel:      "model-a",
		PublicSnapshotID: "missing",
	})
	if err == nil || err.Error() != "baseline snapshot not found: missing" {
		t.Fatalf("StartRun() error = %v, want missing snapshot error", err)
	}
}

func TestStartRunCompletesWithoutBaselineSelection(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := benchmarkTestConfig()
	cfg.Benchmarking.Aliases = nil
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	run, err := service.StartRun(context.Background(), StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	final := awaitRun(t, service, run.RunID)
	if final.Status != RunStatusCompleted {
		t.Fatalf("final.Status = %s, want %s", final.Status, RunStatusCompleted)
	}
	if len(final.Targets) != 1 {
		t.Fatalf("len(final.Targets) = %d, want 1", len(final.Targets))
	}
	target := final.Targets[0]
	if target.Status != TargetStatusCompleted {
		t.Fatalf("target.Status = %s, want %s", target.Status, TargetStatusCompleted)
	}
	if target.Verdict != VerdictNormal {
		t.Fatalf("target.Verdict = %s, want %s", target.Verdict, VerdictNormal)
	}
	if target.CanonicalModelID != "model-a-upstream" {
		t.Fatalf("target.CanonicalModelID = %q, want model-a-upstream", target.CanonicalModelID)
	}
	if target.PublicGap != 0 || target.VendorGap != 0 {
		t.Fatalf("target gaps = public %.2f vendor %.2f, want zero gaps without baselines", target.PublicGap, target.VendorGap)
	}
	if containsString(target.ReasonCodes, "no_baseline_rows_for_target") {
		t.Fatalf("target.ReasonCodes = %#v, did not expect no_baseline_rows_for_target", target.ReasonCodes)
	}
}

func TestImportBaselineRejectsMalformedCSVNumericFields(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, nil, nil)
	_, err = service.ImportBaseline(context.Background(), ImportBaselineRequest{
		Kind:       BaselineKindPublicStandard,
		SourceName: "test-csv",
		FileName:   "baseline.csv",
		Contents: strings.Join([]string{
			"canonical_model_id,metric_name,score,scale_max",
			"model-a,overall,not-a-number,100",
		}, "\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "parse baseline csv score") {
		t.Fatalf("ImportBaseline() error = %v, want csv score parse error", err)
	}
}

func TestStartRunSkipsDisabledProvidersForAllActive(t *testing.T) {
	disabled := false
	cfg := benchmarkTestConfig()
	cfg.Providers = []core.Provider{
		{Name: "provider-a", Models: []string{"model-a"}, BaseURL: "https://provider-a.example"},
		{Name: "provider-disabled", Models: []string{"model-b"}, BaseURL: "https://provider-b.example", Enabled: &disabled},
		{Name: "judge-provider", Models: []string{"judge-model"}, BaseURL: "https://judge.example"},
	}
	cfg.Normalize()

	targets, err := expandTargets(cfg, StartRunRequest{AllActive: true}, core.BenchmarkSuiteGeneralProtocolV1)
	if err != nil {
		t.Fatalf("expandTargets() error = %v", err)
	}
	for _, target := range targets {
		if target.ProviderID == "provider-disabled" {
			t.Fatalf("expandTargets() included disabled provider: %#v", targets)
		}
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2 active providers", len(targets))
	}
}

func TestStartRunMarksTargetIncompleteWhenBaselineRowsMissing(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := benchmarkTestConfig()
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	baseline, err := service.ImportBaseline(context.Background(), ImportBaselineRequest{
		Kind:       BaselineKindPublicStandard,
		SourceName: "public-baseline",
		FileName:   "baseline.json",
		Contents:   `[{"canonical_model_id":"other-model","metric_name":"overall","score":90,"scale_max":100}]`,
	})
	if err != nil {
		t.Fatalf("ImportBaseline() error = %v", err)
	}

	run, err := service.StartRun(context.Background(), StartRunRequest{
		ProviderID:       "provider-a",
		PublicModel:      "model-a",
		PublicSnapshotID: baseline.SnapshotID,
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	final := awaitRun(t, service, run.RunID)
	if final.Status != RunStatusIncomplete {
		t.Fatalf("final.Status = %s, want %s", final.Status, RunStatusIncomplete)
	}
	target := final.Targets[0]
	if target.Verdict != VerdictIncomplete {
		t.Fatalf("target.Verdict = %s, want %s", target.Verdict, VerdictIncomplete)
	}
	if !containsString(target.ReasonCodes, "no_baseline_rows_for_target") {
		t.Fatalf("target.ReasonCodes = %#v, want no_baseline_rows_for_target", target.ReasonCodes)
	}
}

func TestStartRunRejectsSelfJudgeRouteAtExecution(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "provider-a", Models: []string{"model-a"}, BaseURL: "https://provider-a.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Enabled:      true,
			DefaultSuite: core.BenchmarkSuiteGeneralProtocolV1,
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "provider-a",
				PublicModel: "model-a",
			},
			Aliases: []core.BenchmarkAliasConfig{
				{
					CanonicalModelID: "model-a-canonical",
					Provider:         "provider-a",
					Models:           []string{"model-a", "model-a-upstream"},
				},
			},
		},
	}
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	baseline := importBenchmarkBaseline(t, service, "model-a-canonical")

	run, err := service.StartRun(context.Background(), StartRunRequest{
		ProviderID:       "provider-a",
		PublicModel:      "model-a",
		PublicSnapshotID: baseline.SnapshotID,
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	final := awaitRun(t, service, run.RunID)
	if final.Status != RunStatusIncomplete {
		t.Fatalf("final.Status = %s, want %s", final.Status, RunStatusIncomplete)
	}
	target := final.Targets[0]
	if target.Error == "" || !strings.Contains(target.Error, "judge route cannot be the same") {
		t.Fatalf("target.Error = %q, want self-judge rejection", target.Error)
	}
	if !containsString(target.ReasonCodes, "judge_unavailable") {
		t.Fatalf("target.ReasonCodes = %#v, want judge_unavailable", target.ReasonCodes)
	}
}

func TestVerdictForTargetThresholdBoundaries(t *testing.T) {
	thresholds := core.BenchmarkVerdictThresholds{
		NormalMaxGap:                8,
		SuspectMaxGap:               20,
		HighSuspectProtocolFailures: 2,
	}

	tests := []struct {
		name             string
		target           RunTargetDetail
		publicFound      bool
		vendorFound      bool
		baselineSelected bool
		wantVerdict      string
		wantReasons      []string
	}{
		{
			name: "normal at threshold",
			target: RunTargetDetail{
				PublicGap:      8,
				CompletionRate: 100,
			},
			publicFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictNormal,
		},
		{
			name: "suspect when gap exceeds normal threshold",
			target: RunTargetDetail{
				PublicGap:      8.1,
				CompletionRate: 100,
			},
			publicFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictSuspect,
			wantReasons:      []string{"public_gap_above_normal"},
		},
		{
			name: "suspect when one critical protocol failure",
			target: RunTargetDetail{
				VendorGap:                2,
				CompletionRate:           100,
				CriticalProtocolFailures: 1,
			},
			vendorFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictSuspect,
		},
		{
			name: "highly suspect when gap exceeds suspect threshold",
			target: RunTargetDetail{
				PublicGap:      20.1,
				CompletionRate: 100,
			},
			publicFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictHighSuspect,
			wantReasons:      []string{"public_gap_above_normal", "public_gap_above_suspect"},
		},
		{
			name: "highly suspect when critical protocol failures reach threshold",
			target: RunTargetDetail{
				VendorGap:                1,
				CompletionRate:           100,
				CriticalProtocolFailures: 2,
			},
			vendorFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictHighSuspect,
		},
		{
			name: "incomplete when completion rate below eighty",
			target: RunTargetDetail{
				PublicGap:      40,
				CompletionRate: 79.9,
			},
			publicFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictIncomplete,
			wantReasons:      []string{"completion_rate_below_80"},
		},
		{
			name: "incomplete when both baselines missing",
			target: RunTargetDetail{
				CompletionRate: 100,
			},
			baselineSelected: true,
			wantVerdict:      VerdictIncomplete,
			wantReasons:      []string{"no_baseline_rows_for_target"},
		},
		{
			name: "normal when no baseline was selected",
			target: RunTargetDetail{
				CompletionRate: 100,
			},
			wantVerdict: VerdictNormal,
		},
		{
			name: "vendor baseline alone can still drive verdict",
			target: RunTargetDetail{
				VendorGap:      12,
				CompletionRate: 100,
			},
			vendorFound:      true,
			baselineSelected: true,
			wantVerdict:      VerdictSuspect,
			wantReasons:      []string{"vendor_gap_above_normal"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdict, _, reasons := verdictForTarget(tc.target, thresholds, tc.publicFound, tc.vendorFound, tc.baselineSelected)
			if verdict != tc.wantVerdict {
				t.Fatalf("verdict = %s, want %s", verdict, tc.wantVerdict)
			}
			for _, want := range tc.wantReasons {
				if !containsString(reasons, want) {
					t.Fatalf("reasons = %#v, want to contain %q", reasons, want)
				}
			}
		})
	}
}

func TestExecuteTargetAggregatesCaseTokensAndCostWithoutJudgePollution(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "benchmark.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := benchmarkTestConfig()
	cfg.Normalize()

	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	publicBaseline := importBenchmarkBaseline(t, service, "model-a-canonical")
	suite := generalProtocolSuite()

	target := service.executeTarget(context.Background(), cfg, suite, RunTargetDetail{
		TargetID:        "target-agg",
		RunID:           "run-agg",
		ProviderID:      "provider-a",
		PublicModel:     "model-a",
		Protocol:        ProtocolOpenAIChat,
		SuiteVersion:    suite.Name,
		ProtocolAdapter: ProtocolOpenAIChat,
		StartedAt:       time.Now().UTC(),
	}, RunSummary{
		RunID:            "run-agg",
		Status:           RunStatusRunning,
		SuiteVersion:     suite.Name,
		Protocol:         ProtocolOpenAIChat,
		PublicSnapshotID: publicBaseline.SnapshotID,
		StartedAt:        time.Now().UTC(),
	})

	if target.Status != TargetStatusCompleted {
		t.Fatalf("target.Status = %s, want %s", target.Status, TargetStatusCompleted)
	}
	if target.Verdict != VerdictNormal {
		t.Fatalf("target.Verdict = %s, want %s", target.Verdict, VerdictNormal)
	}
	if target.PromptTokens != 68 {
		t.Fatalf("target.PromptTokens = %d, want 68", target.PromptTokens)
	}
	if target.CachedPromptTokens != 0 {
		t.Fatalf("target.CachedPromptTokens = %d, want 0", target.CachedPromptTokens)
	}
	if target.CompletionTokens != 33 {
		t.Fatalf("target.CompletionTokens = %d, want 33", target.CompletionTokens)
	}
	if math.Abs(target.OverallScore-95.75) > 1e-9 {
		t.Fatalf("target.OverallScore = %.4f, want 95.75", target.OverallScore)
	}
	if math.Abs(target.EstimatedCostUSD-0.007) > 1e-9 {
		t.Fatalf("target.EstimatedCostUSD = %f, want 0.007", target.EstimatedCostUSD)
	}
	if target.CompletionRate != 100 {
		t.Fatalf("target.CompletionRate = %f, want 100", target.CompletionRate)
	}
	if target.CriticalProtocolFailures != 0 {
		t.Fatalf("target.CriticalProtocolFailures = %d, want 0", target.CriticalProtocolFailures)
	}
	for _, item := range target.Cases {
		if item.ProviderID == "judge-provider" {
			t.Fatalf("judge provider leaked into target cases: %#v", target.Cases)
		}
	}
}

func TestExpandTargetsTreatsAutoProtocolAsProviderDefault(t *testing.T) {
	cfg := benchmarkTestConfig()
	cfg.Providers[0].ProtocolAdapter = core.ProtocolAdapterAnthropicMessages
	cfg.Normalize()

	targets, err := expandTargets(cfg, StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
		Protocol:    "auto",
	}, cfg.Benchmarking.DefaultSuite)
	if err != nil {
		t.Fatalf("expandTargets() error = %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %#v, want one target", targets)
	}
	if targets[0].Protocol != ProtocolAnthropicMessage {
		t.Fatalf("protocol = %q, want provider default anthropic_messages", targets[0].Protocol)
	}
}

func benchmarkTestConfig() *core.Config {
	return &core.Config{
		Providers: []core.Provider{
			{Name: "provider-a", Models: []string{"model-a"}, BaseURL: "https://provider-a.example"},
			{Name: "judge-provider", Models: []string{"judge-model"}, BaseURL: "https://judge.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Enabled:      true,
			DefaultSuite: core.BenchmarkSuiteGeneralProtocolV1,
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "judge-provider",
				PublicModel: "judge-model",
				TimeoutMs:   5000,
			},
			Limits: core.BenchmarkLimitsConfig{
				MaxParallelRuns:  1,
				MaxParallelCases: 2,
				PerCaseTimeoutMs: 5000,
			},
			VerdictThresholds: core.BenchmarkVerdictThresholds{
				NormalMaxGap:                8,
				SuspectMaxGap:               20,
				HighSuspectProtocolFailures: 2,
			},
			Aliases: []core.BenchmarkAliasConfig{
				{
					CanonicalModelID: "model-a-canonical",
					Provider:         "provider-a",
					Models:           []string{"model-a", "model-a-upstream"},
				},
			},
		},
	}
}

func importBenchmarkBaseline(t *testing.T, service *Service, canonicalModelID string) *BaselineSnapshot {
	t.Helper()
	baseline, err := service.ImportBaseline(context.Background(), ImportBaselineRequest{
		Kind:       BaselineKindPublicStandard,
		SourceName: "public-baseline",
		FileName:   "baseline.json",
		Contents: `[
		  {"canonical_model_id":"` + canonicalModelID + `","metric_name":"reasoning","score":90,"scale_max":100},
		  {"canonical_model_id":"` + canonicalModelID + `","metric_name":"coding_proxy","score":90,"scale_max":100},
		  {"canonical_model_id":"` + canonicalModelID + `","metric_name":"instruction","score":100,"scale_max":100},
		  {"canonical_model_id":"` + canonicalModelID + `","metric_name":"tool_json","score":100,"scale_max":100},
		  {"canonical_model_id":"` + canonicalModelID + `","metric_name":"stream_protocol","score":100,"scale_max":100}
		]`,
	})
	if err != nil {
		t.Fatalf("ImportBaseline() error = %v", err)
	}
	return baseline
}

func awaitRun(t *testing.T, service *Service, runID string) *RunDetail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := service.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if run != nil && run.Status != RunStatusRunning {
			return run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s did not complete before deadline", runID)
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type staticConfigSource struct {
	cfg *core.Config
}

func (s staticConfigSource) GetCurrentConfig() (*core.Config, error) {
	return s.cfg, nil
}

type fakeGatewayRunner struct{}

func (fakeGatewayRunner) RunBenchmarkCase(req gatewaycontrol.RunBenchmarkCaseRequest) (*gatewaycontrol.RunBenchmarkCaseResponse, error) {
	if req.ProviderID == "judge-provider" {
		return &gatewaycontrol.RunBenchmarkCaseResponse{
			StatusCode:   200,
			ResponseBody: []byte(`{"choices":[{"message":{"content":"{\"score\":90,\"reason\":\"judge ok\"}"}}]}`),
		}, nil
	}
	switch req.CaseID {
	case "reasoning_exact":
		return openAIResponse(`{"choices":[{"message":{"content":"17"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`), nil
	case "reasoning_judge":
		return openAIResponse(`{"choices":[{"message":{"content":"Because ordering lets each comparison discard half the remaining search space."}}],"usage":{"prompt_tokens":12,"completion_tokens":12}}`), nil
	case "coding_proxy":
		return openAIResponse(`{"choices":[{"message":{"content":"def fib(n):\n    a, b = 0, 1\n    for _ in range(n):\n        a, b = b, a + b\n    return a"}}],"usage":{"prompt_tokens":18,"completion_tokens":22}}`), nil
	case "instruction_exact":
		return openAIResponse(`{"choices":[{"message":{"content":"alpha beta gamma"}}],"usage":{"prompt_tokens":9,"completion_tokens":3}}`), nil
	case "json_schema":
		return openAIResponse(`{"choices":[{"message":{"content":"{\"animal\":\"cat\",\"count\":2}"}}],"usage":{"prompt_tokens":11,"completion_tokens":6}}`), nil
	case "tool_call":
		return openAIResponse(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":4}}`), nil
	case "stream_protocol":
		return &gatewaycontrol.RunBenchmarkCaseResponse{
			StatusCode:          200,
			ResponseBody:        []byte("data: {\"id\":\"1\"}\n\ndata: [DONE]\n\n"),
			PromptTokens:        8,
			CompletionTokens:    3,
			ProviderID:          "provider-a",
			EffectiveModel:      "model-a-upstream",
			RouteMode:           "direct",
			PricingTotalCostUSD: 0.001,
		}, nil
	default:
		return openAIResponse(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`), nil
	}
}

func openAIResponse(body string) *gatewaycontrol.RunBenchmarkCaseResponse {
	return &gatewaycontrol.RunBenchmarkCaseResponse{
		StatusCode:          200,
		ResponseBody:        []byte(body),
		PromptTokens:        10,
		CachedPromptTokens:  0,
		CompletionTokens:    5,
		ProviderID:          "provider-a",
		EffectiveModel:      "model-a-upstream",
		RouteMode:           "direct",
		PricingTotalCostUSD: 0.001,
	}
}
