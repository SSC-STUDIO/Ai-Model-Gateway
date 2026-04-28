package benchmarking

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"
)

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
