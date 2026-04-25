// Package query provides the query store for telemetryd.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store is the query store for telemetry data.
type Store struct {
	db   *sql.DB
	path string
}

// ProjectionFact is a projected request fact ready to be persisted.
type ProjectionFact struct {
	EventID                  string
	RequestID                string
	Timestamp                string
	Bucket                   string
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
	CompletionTokens         int64
	PricingStatus            string
	PricingSourceID          string
	PricingCurrency          string
	PricingFXRateToUSD       float64
	PricingInputPer1M        float64
	PricingCachedInputPer1M  float64
	PricingOutputPer1M       float64
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

const (
	PricingStatusFixed           = "fixed"
	PricingStatusEstimatedLegacy = "estimated_legacy"
	PricingStatusUnpriced        = "unpriced"
)

type projectionAggregate struct {
	bucket             string
	model              string
	provider           string
	requests           int64
	successes          int64
	failures           int64
	inputTokens        int64
	cachedPromptTokens int64
	outputTokens       int64
	totalLatency       int64
}

// NewStore creates a new query store.
func NewStore(path string) (*Store, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create query store directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open query store: %w", err)
	}

	// Set pragmas
	pragmas := `
	PRAGMA journal_mode = WAL;
	PRAGMA synchronous = NORMAL;
	PRAGMA busy_timeout = 5000;
	`
	if _, err := db.Exec(pragmas); err != nil {
		db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	// Create schema
	schema := `
	-- Request facts (one row per completed attempt)
	CREATE TABLE IF NOT EXISTS request_facts (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id        TEXT    NOT NULL UNIQUE,
		request_id      TEXT    NOT NULL,
		timestamp       TEXT    NOT NULL,
		path            TEXT    NOT NULL DEFAULT '',
		requested_model TEXT    NOT NULL DEFAULT '',
		effective_model TEXT    NOT NULL DEFAULT '',
		provider_id     TEXT    NOT NULL DEFAULT '',
		route_mode      TEXT    NOT NULL DEFAULT '',
		status_code     INTEGER NOT NULL,
		latency_ms      INTEGER NOT NULL,
		attempts        INTEGER NOT NULL DEFAULT 1,
		prompt_tokens   INTEGER NOT NULL DEFAULT 0,
		cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		synthetic_kind TEXT NOT NULL DEFAULT '',
		benchmark_run_id TEXT NOT NULL DEFAULT '',
		benchmark_target_id TEXT NOT NULL DEFAULT '',
		benchmark_case_id TEXT NOT NULL DEFAULT '',
		stream          INTEGER NOT NULL DEFAULT 0,
		error_message   TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_request_facts_timestamp ON request_facts(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_request_facts_provider ON request_facts(provider_id);
	CREATE INDEX IF NOT EXISTS idx_request_facts_model ON request_facts(effective_model);
	CREATE INDEX IF NOT EXISTS idx_request_facts_benchmark_target ON request_facts(benchmark_target_id);

	-- Time bucket aggregates
	CREATE TABLE IF NOT EXISTS agg_buckets (
		bucket          TEXT    NOT NULL,
		model           TEXT    NOT NULL DEFAULT '',
		provider        TEXT    NOT NULL DEFAULT '',
		requests        INTEGER NOT NULL DEFAULT 0,
		successes       INTEGER NOT NULL DEFAULT 0,
		failures        INTEGER NOT NULL DEFAULT 0,
		input_tokens    INTEGER NOT NULL DEFAULT 0,
		cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens   INTEGER NOT NULL DEFAULT 0,
		total_latency   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (bucket, model, provider)
	);
	CREATE INDEX IF NOT EXISTS idx_agg_buckets_bucket ON agg_buckets(bucket DESC);

	-- Projection checkpoints
	CREATE TABLE IF NOT EXISTS projection_checkpoints (
		projection_name TEXT    PRIMARY KEY,
		last_event_id   INTEGER NOT NULL DEFAULT 0,
		updated_at      TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE request_facts ADD COLUMN pricing_status TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN pricing_source_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN pricing_currency TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN pricing_fx_rate_to_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_input_per_1m REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_cached_input_per_1m REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_output_per_1m REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_prompt_cost REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_completion_cost REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_total_cost REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_prompt_cost_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_completion_cost_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN pricing_total_cost_usd REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE request_facts ADD COLUMN synthetic_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN benchmark_run_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN benchmark_target_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE request_facts ADD COLUMN benchmark_case_id TEXT NOT NULL DEFAULT ''`,
	} {
		if err := execCompatibleDDL(db, stmt); err != nil {
			db.Close()
			return nil, err
		}
	}

	return &Store{
		db:   db,
		path: path,
	}, nil
}

// Close closes the query store.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetDB returns the underlying database for queries.
func (s *Store) GetDB() *sql.DB {
	return s.db
}

// LoadProjectionCheckpoint returns the persisted last projected event ID for a projection.
func (s *Store) LoadProjectionCheckpoint(ctx context.Context, projectionName string) (int64, error) {
	var lastEventID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT last_event_id
		FROM projection_checkpoints
		WHERE projection_name = ?
	`, projectionName).Scan(&lastEventID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("load projection checkpoint %q: %w", projectionName, err)
	}
	return lastEventID, nil
}

// ApplyProjectionBatch atomically persists facts, derived aggregates, and the checkpoint.
func (s *Store) ApplyProjectionBatch(ctx context.Context, projectionName string, lastEventID int64, facts []ProjectionFact) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin projection batch: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO request_facts
			(event_id, request_id, timestamp, path, requested_model, effective_model,
			 provider_id, route_mode, status_code, latency_ms, attempts,
			 prompt_tokens, cached_prompt_tokens, completion_tokens, pricing_status,
			 pricing_source_id, pricing_currency, pricing_fx_rate_to_usd, pricing_input_per_1m,
			 pricing_cached_input_per_1m, pricing_output_per_1m, pricing_prompt_cost,
			 pricing_completion_cost, pricing_total_cost, pricing_prompt_cost_usd,
			 pricing_completion_cost_usd, pricing_total_cost_usd, synthetic_kind, benchmark_run_id, benchmark_target_id, benchmark_case_id, stream, error_message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, fmt.Errorf("prepare fact insert: %w", err)
	}
	defer stmt.Close()

	aggUpdates := make(map[string]*projectionAggregate, len(facts))
	var insertedCount int64

	for _, fact := range facts {
		streamInt := 0
		if fact.Stream {
			streamInt = 1
		}

		result, err := stmt.ExecContext(ctx,
			fact.EventID,
			fact.RequestID,
			fact.Timestamp,
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
			fact.CompletionTokens,
			normalizePricingStatus(fact.PricingStatus, fact.PromptTokens, fact.CachedPromptTokens, fact.CompletionTokens),
			fact.PricingSourceID,
			fact.PricingCurrency,
			fact.PricingFXRateToUSD,
			fact.PricingInputPer1M,
			fact.PricingCachedInputPer1M,
			fact.PricingOutputPer1M,
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
			return 0, fmt.Errorf("insert request fact %q: %w", fact.EventID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("request fact rows affected %q: %w", fact.EventID, err)
		}
		if rowsAffected == 0 {
			continue
		}

		insertedCount += rowsAffected
		if fact.SyntheticKind != "" {
			continue
		}
		key := fact.Bucket + "|" + fact.EffectiveModel + "|" + fact.ProviderID
		agg := aggUpdates[key]
		if agg == nil {
			agg = &projectionAggregate{
				bucket:   fact.Bucket,
				model:    fact.EffectiveModel,
				provider: fact.ProviderID,
			}
			aggUpdates[key] = agg
		}
		agg.requests++
		if fact.StatusCode >= 200 && fact.StatusCode < 400 {
			agg.successes++
		} else {
			agg.failures++
		}
		agg.inputTokens += fact.PromptTokens
		agg.cachedPromptTokens += fact.CachedPromptTokens
		agg.outputTokens += fact.CompletionTokens
		agg.totalLatency += fact.LatencyMs
	}

	for _, agg := range aggUpdates {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO agg_buckets (bucket, model, provider, requests, successes, failures,
			                         input_tokens, cached_prompt_tokens, output_tokens, total_latency)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(bucket, model, provider) DO UPDATE SET
				requests = requests + excluded.requests,
				successes = successes + excluded.successes,
				failures = failures + excluded.failures,
				input_tokens = input_tokens + excluded.input_tokens,
				cached_prompt_tokens = cached_prompt_tokens + excluded.cached_prompt_tokens,
				output_tokens = output_tokens + excluded.output_tokens,
				total_latency = total_latency + excluded.total_latency`,
			agg.bucket,
			agg.model,
			agg.provider,
			agg.requests,
			agg.successes,
			agg.failures,
			agg.inputTokens,
			agg.cachedPromptTokens,
			agg.outputTokens,
			agg.totalLatency,
		)
		if err != nil {
			return 0, fmt.Errorf("upsert aggregate bucket %q/%q/%q: %w", agg.bucket, agg.model, agg.provider, err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO projection_checkpoints (projection_name, last_event_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(projection_name) DO UPDATE SET
			last_event_id = CASE
				WHEN excluded.last_event_id > projection_checkpoints.last_event_id THEN excluded.last_event_id
				ELSE projection_checkpoints.last_event_id
			END,
			updated_at = CURRENT_TIMESTAMP
	`, projectionName, lastEventID)
	if err != nil {
		return 0, fmt.Errorf("upsert projection checkpoint %q: %w", projectionName, err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit projection batch: %w", err)
	}

	return insertedCount, nil
}

func execCompatibleDDL(db *sql.DB, stmt string) error {
	if _, err := db.Exec(stmt); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("apply schema update %q: %w", stmt, err)
	}
	return nil
}

func normalizePricingStatus(status string, promptTokens, cachedPromptTokens, completionTokens int64) string {
	switch status {
	case PricingStatusFixed, PricingStatusEstimatedLegacy, PricingStatusUnpriced:
		return status
	}
	if promptTokens > 0 || cachedPromptTokens > 0 || completionTokens > 0 {
		return PricingStatusEstimatedLegacy
	}
	return PricingStatusUnpriced
}
