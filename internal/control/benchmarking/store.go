package benchmarking

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists immutable benchmark baselines and verification run results.
type Store struct {
	db   *sql.DB
	path string
}

// NewStore opens or creates the benchmark store.
func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create benchmark store directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open benchmark store: %w", err)
	}
	pragmas := `
	PRAGMA journal_mode = WAL;
	PRAGMA synchronous = NORMAL;
	PRAGMA busy_timeout = 5000;
	`
	if _, err := db.Exec(pragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("set benchmark store pragmas: %w", err)
	}
	schema := `
	CREATE TABLE IF NOT EXISTS baseline_snapshots (
		snapshot_id  TEXT PRIMARY KEY,
		kind         TEXT NOT NULL,
		source_name  TEXT NOT NULL,
		source_url   TEXT NOT NULL DEFAULT '',
		captured_at  TEXT NOT NULL,
		imported_at  TEXT NOT NULL,
		row_count    INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS baseline_rows (
		id                 INTEGER PRIMARY KEY AUTOINCREMENT,
		snapshot_id        TEXT NOT NULL,
		canonical_model_id TEXT NOT NULL,
		source_model_name  TEXT NOT NULL DEFAULT '',
		family             TEXT NOT NULL DEFAULT '',
		metric_name        TEXT NOT NULL,
		score              REAL NOT NULL DEFAULT 0,
		scale_max          REAL NOT NULL DEFAULT 100,
		metadata_json      TEXT NOT NULL DEFAULT '',
		created_at         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_baseline_rows_snapshot_model ON baseline_rows(snapshot_id, canonical_model_id);
	CREATE INDEX IF NOT EXISTS idx_baseline_rows_snapshot_metric ON baseline_rows(snapshot_id, metric_name);

	CREATE TABLE IF NOT EXISTS benchmark_runs (
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
	CREATE INDEX IF NOT EXISTS idx_benchmark_runs_started_at ON benchmark_runs(started_at DESC);

	CREATE TABLE IF NOT EXISTS benchmark_targets (
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
		overall_score                REAL NOT NULL DEFAULT 0,
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
	CREATE INDEX IF NOT EXISTS idx_benchmark_targets_run_id ON benchmark_targets(run_id, started_at DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create benchmark store schema: %w", err)
	}
	if err := execBenchmarkCompatibleDDL(db, `ALTER TABLE benchmark_targets ADD COLUMN overall_score REAL NOT NULL DEFAULT 0`); err != nil {
		db.Close()
		return nil, err
	}
	if err := backfillBenchmarkOverallScores(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Close closes the store.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// InsertBaselineSnapshot inserts one immutable baseline snapshot and all rows.
func (s *Store) InsertBaselineSnapshot(ctx context.Context, snapshot BaselineSnapshot, rows []BaselineMetricRow) (*BaselineSnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin baseline snapshot insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if snapshot.ImportedAt.IsZero() {
		snapshot.ImportedAt = time.Now().UTC()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = snapshot.ImportedAt
	}
	snapshot.RowCount = len(rows)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO baseline_snapshots (snapshot_id, kind, source_name, source_url, captured_at, imported_at, row_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snapshot.SnapshotID,
		snapshot.Kind,
		snapshot.SourceName,
		snapshot.SourceURL,
		snapshot.CapturedAt.UTC().Format(time.RFC3339Nano),
		snapshot.ImportedAt.UTC().Format(time.RFC3339Nano),
		snapshot.RowCount,
	); err != nil {
		return nil, fmt.Errorf("insert baseline snapshot: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO baseline_rows
			(snapshot_id, canonical_model_id, source_model_name, family, metric_name, score, scale_max, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare baseline row insert: %w", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		metadataJSON, err := json.Marshal(row.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshal baseline row metadata: %w", err)
		}
		if _, err := stmt.ExecContext(ctx,
			snapshot.SnapshotID,
			row.CanonicalModelID,
			row.SourceModelName,
			row.Family,
			row.MetricName,
			row.Score,
			row.ScaleMax,
			string(metadataJSON),
		); err != nil {
			return nil, fmt.Errorf("insert baseline row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit baseline snapshot insert: %w", err)
	}
	return &snapshot, nil
}

// ListBaselineSnapshots returns all imported snapshots, newest first.
func (s *Store) ListBaselineSnapshots(ctx context.Context) ([]BaselineSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT snapshot_id, kind, source_name, source_url, captured_at, imported_at, row_count
		FROM baseline_snapshots
		ORDER BY imported_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list baseline snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]BaselineSnapshot, 0, 16)
	for rows.Next() {
		var (
			snapshot               BaselineSnapshot
			capturedAt, importedAt string
		)
		if err := rows.Scan(
			&snapshot.SnapshotID,
			&snapshot.Kind,
			&snapshot.SourceName,
			&snapshot.SourceURL,
			&capturedAt,
			&importedAt,
			&snapshot.RowCount,
		); err != nil {
			return nil, fmt.Errorf("scan baseline snapshot: %w", err)
		}
		snapshot.CapturedAt = parseTime(capturedAt)
		snapshot.ImportedAt = parseTime(importedAt)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate baseline snapshots: %w", err)
	}
	return snapshots, nil
}

// GetBaselineSnapshot returns one baseline snapshot by ID.
func (s *Store) GetBaselineSnapshot(ctx context.Context, snapshotID string) (*BaselineSnapshot, error) {
	var (
		snapshot               BaselineSnapshot
		capturedAt, importedAt string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT snapshot_id, kind, source_name, source_url, captured_at, imported_at, row_count
		FROM baseline_snapshots
		WHERE snapshot_id = ?`, snapshotID).Scan(
		&snapshot.SnapshotID,
		&snapshot.Kind,
		&snapshot.SourceName,
		&snapshot.SourceURL,
		&capturedAt,
		&importedAt,
		&snapshot.RowCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load baseline snapshot: %w", err)
	}
	snapshot.CapturedAt = parseTime(capturedAt)
	snapshot.ImportedAt = parseTime(importedAt)
	return &snapshot, nil
}

// ListBaselineRows returns all metrics for one snapshot/model pair.
func (s *Store) ListBaselineRows(ctx context.Context, snapshotID, canonicalModelID string) ([]BaselineMetricRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT canonical_model_id, source_model_name, family, metric_name, score, scale_max, metadata_json
		FROM baseline_rows
		WHERE snapshot_id = ? AND canonical_model_id = ?
		ORDER BY metric_name ASC`, snapshotID, canonicalModelID)
	if err != nil {
		return nil, fmt.Errorf("list baseline rows: %w", err)
	}
	defer rows.Close()

	items := make([]BaselineMetricRow, 0, 16)
	for rows.Next() {
		var (
			item         BaselineMetricRow
			metadataJSON string
		)
		if err := rows.Scan(
			&item.CanonicalModelID,
			&item.SourceModelName,
			&item.Family,
			&item.MetricName,
			&item.Score,
			&item.ScaleMax,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan baseline row: %w", err)
		}
		item.SnapshotID = snapshotID
		if metadataJSON != "" {
			_ = json.Unmarshal([]byte(metadataJSON), &item.Metadata)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate baseline rows: %w", err)
	}
	return items, nil
}

// CreateRun inserts a new benchmark run and its targets.
func (s *Store) CreateRun(ctx context.Context, run RunSummary, targets []RunTargetDetail) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin benchmark run insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO benchmark_runs
			(run_id, status, suite_version, protocol, public_snapshot_id, vendor_snapshot_id, started_at, completed_at, target_count, completed_targets, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID,
		run.Status,
		run.SuiteVersion,
		run.Protocol,
		run.PublicSnapshotID,
		run.VendorSnapshotID,
		run.StartedAt.UTC().Format(time.RFC3339Nano),
		formatOptionalTime(run.CompletedAt),
		run.TargetCount,
		run.CompletedTargets,
		run.Error,
	); err != nil {
		return fmt.Errorf("insert benchmark run: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO benchmark_targets
			(target_id, run_id, status, provider_id, public_model, effective_model, canonical_model_id, protocol,
			 protocol_adapter, suite_version, judge_model, public_snapshot_id, vendor_snapshot_id, verdict,
			 suspicion_score, public_gap, vendor_gap, overall_score, completion_rate, critical_protocol_failures, prompt_tokens,
			 cached_prompt_tokens, completion_tokens, estimated_cost_usd, reason_codes_json, dimension_scores_json,
			 cases_json, started_at, completed_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare benchmark target insert: %w", err)
	}
	defer stmt.Close()

	for _, target := range targets {
		reasonCodesJSON, _ := json.Marshal(target.ReasonCodes)
		dimensionScoresJSON, _ := json.Marshal(target.DimensionScores)
		casesJSON, _ := json.Marshal(target.Cases)
		if _, err := stmt.ExecContext(ctx,
			target.TargetID,
			target.RunID,
			target.Status,
			target.ProviderID,
			target.PublicModel,
			target.EffectiveModel,
			target.CanonicalModelID,
			target.Protocol,
			target.ProtocolAdapter,
			target.SuiteVersion,
			target.JudgeModel,
			target.PublicSnapshotID,
			target.VendorSnapshotID,
			target.Verdict,
			target.SuspicionScore,
			target.PublicGap,
			target.VendorGap,
			target.OverallScore,
			target.CompletionRate,
			target.CriticalProtocolFailures,
			target.PromptTokens,
			target.CachedPromptTokens,
			target.CompletionTokens,
			target.EstimatedCostUSD,
			string(reasonCodesJSON),
			string(dimensionScoresJSON),
			string(casesJSON),
			target.StartedAt.UTC().Format(time.RFC3339Nano),
			formatOptionalTime(target.CompletedAt),
			target.Error,
		); err != nil {
			return fmt.Errorf("insert benchmark target: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit benchmark run insert: %w", err)
	}
	return nil
}

// UpdateRunStatus updates the overall run status.
func (s *Store) UpdateRunStatus(ctx context.Context, runID, status, errMessage string, completedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE benchmark_runs
		SET status = ?, error = ?, completed_at = ?, completed_targets = (
			SELECT COUNT(*) FROM benchmark_targets WHERE run_id = ? AND status IN (?, ?, ?)
		)
		WHERE run_id = ?`,
		status,
		errMessage,
		formatOptionalTime(completedAt),
		runID,
		TargetStatusCompleted,
		TargetStatusFailed,
		TargetStatusIncomplete,
		runID,
	)
	if err != nil {
		return fmt.Errorf("update benchmark run status: %w", err)
	}
	return nil
}

// UpdateTarget updates one benchmark target result.
func (s *Store) UpdateTarget(ctx context.Context, target RunTargetDetail) error {
	reasonCodesJSON, _ := json.Marshal(target.ReasonCodes)
	dimensionScoresJSON, _ := json.Marshal(target.DimensionScores)
	casesJSON, _ := json.Marshal(target.Cases)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin benchmark target update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE benchmark_targets
		SET status = ?, effective_model = ?, canonical_model_id = ?, protocol = ?, protocol_adapter = ?, judge_model = ?,
			public_snapshot_id = ?, vendor_snapshot_id = ?, verdict = ?, suspicion_score = ?, public_gap = ?, vendor_gap = ?,
			overall_score = ?, completion_rate = ?, critical_protocol_failures = ?, prompt_tokens = ?, cached_prompt_tokens = ?, completion_tokens = ?,
			estimated_cost_usd = ?, reason_codes_json = ?, dimension_scores_json = ?, cases_json = ?, completed_at = ?, error = ?
		WHERE target_id = ?`,
		target.Status,
		target.EffectiveModel,
		target.CanonicalModelID,
		target.Protocol,
		target.ProtocolAdapter,
		target.JudgeModel,
		target.PublicSnapshotID,
		target.VendorSnapshotID,
		target.Verdict,
		target.SuspicionScore,
		target.PublicGap,
		target.VendorGap,
		target.OverallScore,
		target.CompletionRate,
		target.CriticalProtocolFailures,
		target.PromptTokens,
		target.CachedPromptTokens,
		target.CompletionTokens,
		target.EstimatedCostUSD,
		string(reasonCodesJSON),
		string(dimensionScoresJSON),
		string(casesJSON),
		formatOptionalTime(target.CompletedAt),
		target.Error,
		target.TargetID,
	); err != nil {
		return fmt.Errorf("update benchmark target: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE benchmark_runs
		SET completed_targets = (
			SELECT COUNT(*) FROM benchmark_targets WHERE run_id = ? AND status IN (?, ?, ?)
		)
		WHERE run_id = ?`,
		target.RunID,
		TargetStatusCompleted,
		TargetStatusFailed,
		TargetStatusIncomplete,
		target.RunID,
	); err != nil {
		return fmt.Errorf("update benchmark run completed target count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit benchmark target update: %w", err)
	}
	return nil
}

// ListRuns returns recent run summaries.
func (s *Store) ListRuns(ctx context.Context, limit int) ([]RunSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, status, suite_version, protocol, public_snapshot_id, vendor_snapshot_id,
		       started_at, completed_at, target_count, completed_targets, error
		FROM benchmark_runs
		ORDER BY started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list benchmark runs: %w", err)
	}
	defer rows.Close()

	runs := make([]RunSummary, 0, limit)
	for rows.Next() {
		var (
			run                    RunSummary
			startedAt, completedAt string
		)
		if err := rows.Scan(
			&run.RunID,
			&run.Status,
			&run.SuiteVersion,
			&run.Protocol,
			&run.PublicSnapshotID,
			&run.VendorSnapshotID,
			&startedAt,
			&completedAt,
			&run.TargetCount,
			&run.CompletedTargets,
			&run.Error,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark run: %w", err)
		}
		run.StartedAt = parseTime(startedAt)
		run.CompletedAt = parseTime(completedAt)
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark runs: %w", err)
	}
	return runs, nil
}

// GetRun returns one run with all target details.
func (s *Store) GetRun(ctx context.Context, runID string) (*RunDetail, error) {
	var (
		run                    RunDetail
		startedAt, completedAt string
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT run_id, status, suite_version, protocol, public_snapshot_id, vendor_snapshot_id,
		       started_at, completed_at, target_count, completed_targets, error
		FROM benchmark_runs
		WHERE run_id = ?`, runID).Scan(
		&run.RunID,
		&run.Status,
		&run.SuiteVersion,
		&run.Protocol,
		&run.PublicSnapshotID,
		&run.VendorSnapshotID,
		&startedAt,
		&completedAt,
		&run.TargetCount,
		&run.CompletedTargets,
		&run.Error,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load benchmark run: %w", err)
	}
	run.StartedAt = parseTime(startedAt)
	run.CompletedAt = parseTime(completedAt)

	rows, err := s.db.QueryContext(ctx, `
		SELECT target_id, run_id, status, provider_id, public_model, effective_model, canonical_model_id, protocol,
		       protocol_adapter, suite_version, judge_model, public_snapshot_id, vendor_snapshot_id, verdict,
		       suspicion_score, public_gap, vendor_gap, overall_score, completion_rate, critical_protocol_failures,
		       prompt_tokens, cached_prompt_tokens, completion_tokens, estimated_cost_usd,
		       reason_codes_json, dimension_scores_json, cases_json, started_at, completed_at, error
		FROM benchmark_targets
		WHERE run_id = ?
		ORDER BY started_at ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("load benchmark targets: %w", err)
	}
	defer rows.Close()

	run.Targets = make([]RunTargetDetail, 0, run.TargetCount)
	for rows.Next() {
		var (
			target              RunTargetDetail
			reasonCodesJSON     string
			dimensionScoresJSON string
			casesJSON           string
			targetStartedAt     string
			targetCompletedAt   string
		)
		if err := rows.Scan(
			&target.TargetID,
			&target.RunID,
			&target.Status,
			&target.ProviderID,
			&target.PublicModel,
			&target.EffectiveModel,
			&target.CanonicalModelID,
			&target.Protocol,
			&target.ProtocolAdapter,
			&target.SuiteVersion,
			&target.JudgeModel,
			&target.PublicSnapshotID,
			&target.VendorSnapshotID,
			&target.Verdict,
			&target.SuspicionScore,
			&target.PublicGap,
			&target.VendorGap,
			&target.OverallScore,
			&target.CompletionRate,
			&target.CriticalProtocolFailures,
			&target.PromptTokens,
			&target.CachedPromptTokens,
			&target.CompletionTokens,
			&target.EstimatedCostUSD,
			&reasonCodesJSON,
			&dimensionScoresJSON,
			&casesJSON,
			&targetStartedAt,
			&targetCompletedAt,
			&target.Error,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark target: %w", err)
		}
		target.StartedAt = parseTime(targetStartedAt)
		target.CompletedAt = parseTime(targetCompletedAt)
		if reasonCodesJSON != "" {
			_ = json.Unmarshal([]byte(reasonCodesJSON), &target.ReasonCodes)
		}
		if dimensionScoresJSON != "" {
			_ = json.Unmarshal([]byte(dimensionScoresJSON), &target.DimensionScores)
		}
		if casesJSON != "" {
			_ = json.Unmarshal([]byte(casesJSON), &target.Cases)
		}
		run.Targets = append(run.Targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark targets: %w", err)
	}
	return &run, nil
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts
	}
	return time.Time{}
}

func formatOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func execBenchmarkCompatibleDDL(db *sql.DB, stmt string) error {
	if _, err := db.Exec(stmt); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("apply benchmark schema update %q: %w", stmt, err)
	}
	return nil
}

func backfillBenchmarkOverallScores(db *sql.DB) error {
	rows, err := db.Query(`
		SELECT target_id, suite_version, dimension_scores_json
		FROM benchmark_targets
		WHERE overall_score = 0
		  AND TRIM(COALESCE(dimension_scores_json, '')) NOT IN ('', '{}', 'null')`)
	if err != nil {
		return fmt.Errorf("query benchmark overall score backfill candidates: %w", err)
	}
	defer rows.Close()

	type update struct {
		targetID string
		score    float64
	}
	updates := make([]update, 0, 32)
	for rows.Next() {
		var targetID, suiteVersion, dimensionScoresJSON string
		if err := rows.Scan(&targetID, &suiteVersion, &dimensionScoresJSON); err != nil {
			return fmt.Errorf("scan benchmark overall score backfill candidate: %w", err)
		}
		dimensionScores := map[string]float64{}
		if err := json.Unmarshal([]byte(dimensionScoresJSON), &dimensionScores); err != nil || len(dimensionScores) == 0 {
			continue
		}
		suite, err := suiteByName(suiteVersion)
		if err != nil {
			suite = generalProtocolSuite()
		}
		score := weightedAverage(dimensionScores, suite.DimensionWeights)
		updates = append(updates, update{targetID: targetID, score: score})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate benchmark overall score backfill candidates: %w", err)
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin benchmark overall score backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		UPDATE benchmark_targets
		SET overall_score = ?
		WHERE target_id = ? AND overall_score = 0`)
	if err != nil {
		return fmt.Errorf("prepare benchmark overall score backfill: %w", err)
	}
	defer stmt.Close()

	for _, item := range updates {
		if _, err := stmt.Exec(item.score, item.targetID); err != nil {
			return fmt.Errorf("update benchmark target %s overall score: %w", item.targetID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit benchmark overall score backfill: %w", err)
	}
	return nil
}
