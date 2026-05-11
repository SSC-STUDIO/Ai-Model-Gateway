package benchmarking

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestListRunsEmpty(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "listruns.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	runs, err := store.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("ListRuns() = %d runs, want 0", len(runs))
	}
}

func TestListRunsAfterStartRun(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "listruns2.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := benchmarkTestConfig()
	cfg.Normalize()
	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	_, err = service.StartRun(context.Background(), StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}

	runs, err := service.ListRuns(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("ListRuns() = 0 runs after StartRun, want > 0")
	}
}

func TestServiceListRunsNilService(t *testing.T) {
	var svc *Service
	runs, err := svc.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRuns(nil service) error = %v", err)
	}
	if runs != nil {
		t.Fatalf("ListRuns(nil service) = %v, want nil", runs)
	}
}

func TestServiceGetRunNilService(t *testing.T) {
	var svc *Service
	run, err := svc.GetRun(context.Background(), "any")
	if err != nil {
		t.Fatalf("GetRun(nil service) error = %v", err)
	}
	if run != nil {
		t.Fatalf("GetRun(nil service) = %v, want nil", run)
	}
}

func TestStartRunNilService(t *testing.T) {
	var svc *Service
	_, err := svc.StartRun(context.Background(), StartRunRequest{})
	if err == nil || err.Error() != "benchmark store not configured" {
		t.Fatalf("StartRun(nil service) error = %v, want benchmark store not configured", err)
	}
}

func TestStartRunNoGateway(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "nogateway.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, staticConfigSource{cfg: benchmarkTestConfig()}, nil)
	_, err = service.StartRun(context.Background(), StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
	})
	if err == nil || err.Error() != "gateway benchmark runner not connected" {
		t.Fatalf("StartRun(no gateway) error = %v, want gateway benchmark runner not connected", err)
	}
}

func TestStartRunNoConfigSource(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "noconfig.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	service := NewService(store, nil, fakeGatewayRunner{})
	_, err = service.StartRun(context.Background(), StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
	})
	if err == nil || err.Error() != "benchmark config source not configured" {
		t.Fatalf("StartRun(no config) error = %v, want benchmark config source not configured", err)
	}
}

func TestStartRunDisabled(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "disabled.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	cfg := benchmarkTestConfig()
	cfg.Benchmarking.Enabled = false
	service := NewService(store, staticConfigSource{cfg: cfg}, fakeGatewayRunner{})
	_, err = service.StartRun(context.Background(), StartRunRequest{
		ProviderID:  "provider-a",
		PublicModel: "model-a",
	})
	if err == nil || err.Error() != "benchmarking is disabled" {
		t.Fatalf("StartRun(disabled) error = %v, want benchmarking is disabled", err)
	}
}

func TestResolveJudgeProviderNilConfig(t *testing.T) {
	_, err := resolveJudgeProvider(nil, "model", "provider")
	if err == nil || err.Error() != "no config available" {
		t.Fatalf("resolveJudgeProvider(nil) error = %v, want no config available", err)
	}
}

func TestResolveJudgeProviderPreferredNotFound(t *testing.T) {
	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "p1", Models: []string{"m1"}, BaseURL: "https://p1.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "missing-provider",
				PublicModel: "judge-model",
			},
		},
	}
	cfg.Normalize()
	_, err := resolveJudgeProvider(cfg, "judge-model", "p1")
	if err == nil || !strings.Contains(err.Error(), "judge provider not found") {
		t.Fatalf("resolveJudgeProvider() error = %v, want judge provider not found", err)
	}
}

func TestResolveJudgeProviderPreferredModelMismatch(t *testing.T) {
	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "judge", Models: []string{"other-model"}, BaseURL: "https://judge.example"},
		},
		Benchmarking: core.BenchmarkingConfig{
			Judge: core.BenchmarkJudgeConfig{
				Provider:    "judge",
				PublicModel: "judge-model",
			},
		},
	}
	cfg.Normalize()
	_, err := resolveJudgeProvider(cfg, "judge-model", "p1")
	if err == nil || !strings.Contains(err.Error(), "does not serve model") {
		t.Fatalf("resolveJudgeProvider() error = %v, want does not serve model", err)
	}
}

func TestResolveJudgeProviderFallbackNotFound(t *testing.T) {
	cfg := &core.Config{
		Providers: []core.Provider{
			{Name: "p1", Models: []string{"m1", "judge-model"}, BaseURL: "https://p1.example"},
		},
		Benchmarking: core.BenchmarkingConfig{},
	}
	cfg.Normalize()
	_, err := resolveJudgeProvider(cfg, "judge-model", "p1")
	if err == nil || !strings.Contains(err.Error(), "no judge provider found") {
		t.Fatalf("resolveJudgeProvider() error = %v, want no judge provider found", err)
	}
}

func TestNormalizeProtocol(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"auto", ""},
		{"openai_chat_completions", ProtocolOpenAIChat},
		{"anthropic_messages", ProtocolAnthropicMessage},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		got := normalizeProtocol(tc.input)
		if got != tc.want {
			t.Fatalf("normalizeProtocol(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"a", "b", "a", "c", "b"})
	if len(got) != 3 {
		t.Fatalf("uniqueStrings() = %v, want 3 unique", got)
	}
	empty := uniqueStrings(nil)
	if len(empty) != 0 {
		t.Fatalf("uniqueStrings(nil) = %v, want empty", empty)
	}
}

func TestWeightedAverage(t *testing.T) {
	scores := map[string]float64{
		"a": 100,
		"b": 80,
	}
	weights := map[string]float64{
		"a": 50,
		"b": 50,
	}
	got := weightedAverage(scores, weights)
	if math.Abs(got-90) > 1e-9 {
		t.Fatalf("weightedAverage() = %f, want 90", got)
	}
}

func TestWeightedAverageZeroWeight(t *testing.T) {
	got := weightedAverage(map[string]float64{"a": 100}, map[string]float64{})
	if got != 0 {
		t.Fatalf("weightedAverage(empty weights) = %f, want 0", got)
	}
}

func TestMaxFloat(t *testing.T) {
	if maxFloat(1, 2) != 2 {
		t.Fatalf("maxFloat(1,2) != 2")
	}
	if maxFloat(5, 3) != 5 {
		t.Fatalf("maxFloat(5,3) != 5")
	}
}

func TestAbsFloat(t *testing.T) {
	if absFloat(-5) != 5 {
		t.Fatalf("absFloat(-5) != 5")
	}
	if absFloat(3) != 3 {
		t.Fatalf("absFloat(3) != 3")
	}
}

func TestExpandTargetsNilConfig(t *testing.T) {
	_, err := expandTargets(nil, StartRunRequest{}, "suite")
	if err == nil || err.Error() != "no config available" {
		t.Fatalf("expandTargets(nil) error = %v, want no config available", err)
	}
}

func TestExpandTargetsMissingProviderAndModel(t *testing.T) {
	cfg := benchmarkTestConfig()
	cfg.Normalize()
	_, err := expandTargets(cfg, StartRunRequest{}, "general_protocol_v1")
	if err == nil || !strings.Contains(err.Error(), "provider_id and public_model are required") {
		t.Fatalf("expandTargets(missing) error = %v, want required", err)
	}
}

func TestExpandTargetsProviderNotFound(t *testing.T) {
	cfg := benchmarkTestConfig()
	cfg.Normalize()
	_, err := expandTargets(cfg, StartRunRequest{ProviderID: "nonexistent", PublicModel: "model-a"}, "general_protocol_v1")
	if err == nil || !strings.Contains(err.Error(), "provider not found") {
		t.Fatalf("expandTargets() error = %v, want provider not found", err)
	}
}

func TestExpandTargetsModelNotAdvertised(t *testing.T) {
	cfg := benchmarkTestConfig()
	cfg.Normalize()
	_, err := expandTargets(cfg, StartRunRequest{ProviderID: "provider-a", PublicModel: "nonexistent-model"}, "general_protocol_v1")
	if err == nil || !strings.Contains(err.Error(), "does not advertise public model") {
		t.Fatalf("expandTargets() error = %v, want does not advertise", err)
	}
}

func TestValidateBaselineSelectionEmpty(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "validate.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()
	service := NewService(store, nil, nil)
	if err := service.validateBaselineSelection(context.Background(), "", BaselineKindPublicStandard); err != nil {
		t.Fatalf("validateBaselineSelection(empty) error = %v, want nil", err)
	}
}

func TestParseBaselineRowsEmpty(t *testing.T) {
	_, err := parseBaselineRows("file.json", "")
	if err == nil || !strings.Contains(err.Error(), "baseline contents are empty") {
		t.Fatalf("parseBaselineRows(empty) error = %v, want empty", err)
	}
}

func TestNewStoreMigratesOverallScoreColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	oldSchema := `
	CREATE TABLE benchmark_runs (
		run_id              TEXT PRIMARY KEY,
		status              TEXT NOT NULL,
		suite_version       TEXT NOT NULL,
		protocol            TEXT NOT NULL DEFAULT '',
		public_snapshot_id  TEXT NOT NULL DEFAULT '',
		vendor_snapshot_id  TEXT NOT NULL DEFAULT '',
		started_at          TEXT NOT NULL,
		completed_at        TEXT NOT NULL DEFAULT '',
		target_count        INTEGER NOT NULL DEFAULT 0,
		completed_targets   INTEGER NOT NULL DEFAULT 0,
		error               TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE benchmark_targets (
		target_id                    TEXT PRIMARY KEY,
		run_id                       TEXT NOT NULL,
		status                       TEXT NOT NULL,
		provider_id                  TEXT NOT NULL DEFAULT '',
		public_model                 TEXT NOT NULL DEFAULT '',
		effective_model              TEXT NOT NULL DEFAULT '',
		canonical_model_id           TEXT NOT NULL DEFAULT '',
		protocol                     TEXT NOT NULL DEFAULT '',
		protocol_adapter             TEXT NOT NULL DEFAULT '',
		suite_version                TEXT NOT NULL DEFAULT '',
		judge_model                  TEXT NOT NULL DEFAULT '',
		public_snapshot_id           TEXT NOT NULL DEFAULT '',
		vendor_snapshot_id           TEXT NOT NULL DEFAULT '',
		verdict                      TEXT NOT NULL DEFAULT '',
		suspicion_score              REAL NOT NULL DEFAULT 0,
		public_gap                   REAL NOT NULL DEFAULT 0,
		vendor_gap                   REAL NOT NULL DEFAULT 0,
		completion_rate              REAL NOT NULL DEFAULT 0,
		critical_protocol_failures   INTEGER NOT NULL DEFAULT 0,
		prompt_tokens                INTEGER NOT NULL DEFAULT 0,
		cached_prompt_tokens         INTEGER NOT NULL DEFAULT 0,
		completion_tokens            INTEGER NOT NULL DEFAULT 0,
		estimated_cost_usd           REAL NOT NULL DEFAULT 0,
		reason_codes_json            TEXT NOT NULL DEFAULT '[]',
		dimension_scores_json        TEXT NOT NULL DEFAULT '{}',
		cases_json                   TEXT NOT NULL DEFAULT '[]',
		started_at                   TEXT NOT NULL,
		completed_at                 TEXT NOT NULL DEFAULT '',
		error                        TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO benchmark_runs
		(run_id, status, suite_version, protocol, started_at, completed_at, target_count, completed_targets)
	VALUES ('run-old', 'completed', 'general_protocol_v1', 'openai_chat_completions', ?, ?, 1, 1);
	INSERT INTO benchmark_targets
		(target_id, run_id, status, provider_id, public_model, protocol, suite_version, verdict,
		 completion_rate, reason_codes_json, dimension_scores_json, cases_json, started_at, completed_at)
	VALUES ('target-old', 'run-old', 'completed', 'provider-a', 'model-a', 'openai_chat_completions',
		'general_protocol_v1', 'normal', 100, '[]',
		'{"reasoning":95,"coding_proxy":90,"instruction":100,"tool_json":100,"stream_protocol":100}', '[]', ?, ?);
	`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(oldSchema, now, now, now, now); err != nil {
		_ = db.Close()
		t.Fatalf("create old benchmark schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old sqlite: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	var overallScore float64
	if err := store.db.QueryRow(`SELECT overall_score FROM benchmark_targets WHERE target_id = 'target-old'`).Scan(&overallScore); err != nil {
		t.Fatalf("overall_score column was not added: %v", err)
	}
	if math.Abs(overallScore-95.75) > 1e-9 {
		t.Fatalf("overall_score backfill = %f, want 95.75", overallScore)
	}

	run, err := store.GetRun(context.Background(), "run-old")
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run == nil || len(run.Targets) != 1 {
		t.Fatalf("GetRun() = %#v, want one migrated target", run)
	}
	if math.Abs(run.Targets[0].OverallScore-95.75) > 1e-9 {
		t.Fatalf("migrated target OverallScore = %f, want 95.75", run.Targets[0].OverallScore)
	}
}
