package telemetrydb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/core"

	_ "modernc.org/sqlite"
)

func testConfig(t *testing.T) core.TelemetryConfig {
	t.Helper()
	dir := t.TempDir()
	return core.TelemetryConfig{
		SQLitePath:     filepath.Join(dir, "test.db"),
		RetentionDays:  7,
		AggregationSec: 60,
		CacheTTLSec:    1,
	}
}

func TestNew_CreatesDB(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(cfg.SQLitePath); err != nil {
		t.Errorf("expected db file to exist: %v", err)
	}
}

func TestRecord_And_Flush(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	rec := &core.RequestRecord{
		RequestID:          "req-1",
		Timestamp:          time.Now(),
		Model:              "gpt-4o",
		RequestedModel:     "gpt-4o",
		EffectiveModel:     "gpt-4o",
		Provider:           "openai",
		StatusCode:         200,
		Latency:            150 * time.Millisecond,
		InputTokens:        100,
		OutputTokens:       50,
		CachedPromptTokens: 25,
		Path:               "/v1/responses",
		RouteMode:          "direct",
		Attempts:           1,
		Stream:             false,
	}

	if err := s.Record(ctx, rec); err != nil {
		t.Fatalf("Record() error: %v", err)
	}

	s.Flush()

	// Verify row was inserted.
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM requests")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 request row, got %d", count)
	}

	// Verify aggregation row was created.
	row = s.db.QueryRow("SELECT COUNT(*) FROM agg_minutes")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query agg count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 agg row, got %d", count)
	}

	var (
		path, requested, effective, routeMode string
		attempts                              int
		cachedPromptTokens                    int64
	)
	row = s.db.QueryRow(`
SELECT path, requested_model, effective_model, route_mode, attempts, cached_prompt_tokens
FROM requests
LIMIT 1`)
	if err := row.Scan(&path, &requested, &effective, &routeMode, &attempts, &cachedPromptTokens); err != nil {
		t.Fatalf("query telemetry metadata columns: %v", err)
	}
	if path != "/v1/responses" || requested != "gpt-4o" || effective != "gpt-4o" || routeMode != "direct" {
		t.Fatalf("unexpected telemetry metadata values: path=%q requested=%q effective=%q route_mode=%q", path, requested, effective, routeMode)
	}
	if attempts != 1 || cachedPromptTokens != 25 {
		t.Fatalf("unexpected attempts/cached_prompt_tokens: attempts=%d cached_prompt_tokens=%d", attempts, cachedPromptTokens)
	}
}

func TestRecord_BatchMultiple(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 10; i++ {
		rec := &core.RequestRecord{
			RequestID:    "req-batch",
			Timestamp:    now,
			Model:        "gpt-4o",
			Provider:     "openai",
			StatusCode:   200,
			Latency:      100 * time.Millisecond,
			InputTokens:  10,
			OutputTokens: 5,
		}
		if err := s.Record(ctx, rec); err != nil {
			t.Fatalf("Record() error: %v", err)
		}
	}

	s.Flush()

	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM requests")
	_ = row.Scan(&count)
	if count != 10 {
		t.Errorf("expected 10 request rows, got %d", count)
	}

	// Aggregation should combine into 1 bucket (same minute).
	var aggCount int
	row = s.db.QueryRow("SELECT COUNT(*) FROM agg_minutes")
	_ = row.Scan(&aggCount)
	if aggCount != 1 {
		t.Errorf("expected 1 agg bucket, got %d", aggCount)
	}

	// Verify aggregated totals.
	var totalReqs int
	row = s.db.QueryRow("SELECT requests FROM agg_minutes LIMIT 1")
	_ = row.Scan(&totalReqs)
	if totalReqs != 10 {
		t.Errorf("expected agg requests=10, got %d", totalReqs)
	}
}

func TestQueryWindowMetrics(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	now := time.Now()

	// Insert 5 success + 2 failure.
	for i := 0; i < 5; i++ {
		_ = s.Record(ctx, &core.RequestRecord{
			RequestID: "ok", Timestamp: now, Model: "m", Provider: "p",
			StatusCode: 200, Latency: 100 * time.Millisecond, InputTokens: 10, OutputTokens: 5,
		})
	}
	for i := 0; i < 2; i++ {
		_ = s.Record(ctx, &core.RequestRecord{
			RequestID: "fail", Timestamp: now, Model: "m", Provider: "p",
			StatusCode: 500, Latency: 200 * time.Millisecond, Error: "server error",
		})
	}
	s.Flush()

	reqs, successes, failures, _ := s.QueryWindowMetrics(5 * time.Minute)
	if reqs != 7 {
		t.Errorf("expected 7 requests, got %d", reqs)
	}
	if successes != 5 {
		t.Errorf("expected 5 successes, got %d", successes)
	}
	if failures != 2 {
		t.Errorf("expected 2 failures, got %d", failures)
	}
}

func TestQueryRecentRequests(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	for i := 0; i < 3; i++ {
		_ = s.Record(ctx, &core.RequestRecord{
			RequestID:          fmt.Sprintf("req-recent-%d", i),
			Timestamp:          base.Add(time.Duration(i) * time.Second),
			Model:              "gpt-4o",
			RequestedModel:     "gpt-4o",
			EffectiveModel:     "gpt-4o",
			Provider:           "openai",
			StatusCode:         200,
			Latency:            time.Duration(100+i) * time.Millisecond,
			InputTokens:        10,
			OutputTokens:       5,
			CachedPromptTokens: int64(i),
			Path:               "/v1/chat/completions",
			RouteMode:          "direct",
			Attempts:           i + 1,
			Stream:             i%2 == 0,
		})
	}
	s.Flush()

	rows := s.QueryRecentRequests(2)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].RequestID != "req-recent-2" || rows[1].RequestID != "req-recent-1" {
		t.Fatalf("unexpected order: %+v", rows)
	}
	if !rows[0].Stream {
		t.Fatalf("expected stream flag to round-trip, got %+v", rows[0])
	}
	if rows[0].Path != "/v1/chat/completions" || rows[0].RequestedModel != "gpt-4o" || rows[0].EffectiveModel != "gpt-4o" {
		t.Fatalf("expected routing metadata to round-trip, got %+v", rows[0])
	}
	if rows[0].RouteMode != "direct" || rows[0].Attempts != 3 || rows[0].CachedPromptTokens != 2 {
		t.Fatalf("expected route mode/attempt/cached tokens to round-trip, got %+v", rows[0])
	}
}

func TestQueryRecentErrors(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-ok", Timestamp: base, Model: "m", Provider: "p",
		StatusCode: 200, Latency: 100 * time.Millisecond, Path: "/ok",
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-fail", Timestamp: base.Add(1 * time.Second), Model: "m", Provider: "p",
		StatusCode: 500, Latency: 200 * time.Millisecond, Error: "boom", Path: "/fail",
		RequestedModel: "m", EffectiveModel: "m", RouteMode: "direct", Attempts: 1,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-rate", Timestamp: base.Add(2 * time.Second), Model: "m", Provider: "p",
		StatusCode: 429, Latency: 150 * time.Millisecond, Path: "/rate",
		RequestedModel: "m", EffectiveModel: "m", RouteMode: "direct", Attempts: 2,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-msg", Timestamp: base.Add(3 * time.Second), Model: "m", Provider: "p",
		StatusCode: 200, Latency: 120 * time.Millisecond, Error: "transport timeout", Path: "/msg",
		RequestedModel: "m", EffectiveModel: "m-fallback", RouteMode: "bridge_fallback", Attempts: 3,
	})
	s.Flush()

	rows := s.QueryRecentErrors(10)
	if len(rows) != 3 {
		t.Fatalf("expected 3 error rows, got %d", len(rows))
	}
	if rows[0].RequestID != "req-msg" || rows[1].RequestID != "req-rate" || rows[2].RequestID != "req-fail" {
		t.Fatalf("unexpected error order: %+v", rows)
	}
	if rows[0].Path != "/msg" || rows[0].RouteMode != "bridge_fallback" || rows[0].Attempts != 3 {
		t.Fatalf("expected enriched metadata in recent error row, got %+v", rows[0])
	}
	if rows[0].EffectiveModel != "m-fallback" {
		t.Fatalf("expected effective model m-fallback, got %q", rows[0].EffectiveModel)
	}
}

func TestQueryTimeSeriesBuckets(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Minute)
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-b1-ok", Timestamp: base, Model: "m1", Provider: "p1",
		StatusCode: 200, Latency: 100 * time.Millisecond, InputTokens: 5, OutputTokens: 6,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-b1-fail", Timestamp: base, Model: "m1", Provider: "p1",
		StatusCode: 500, Latency: 300 * time.Millisecond, InputTokens: 1, OutputTokens: 1, Error: "boom",
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-b2-ok", Timestamp: base.Add(time.Minute), Model: "m2", Provider: "p2",
		StatusCode: 200, Latency: 200 * time.Millisecond, InputTokens: 2, OutputTokens: 3,
	})
	s.Flush()

	buckets := s.QueryTimeSeriesBuckets(10*time.Minute, time.Minute)
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d (%+v)", len(buckets), buckets)
	}
	if buckets[0].Bucket != base.Format(time.RFC3339) {
		t.Fatalf("expected first bucket %s, got %s", base.Format(time.RFC3339), buckets[0].Bucket)
	}
	if buckets[0].Requests != 2 || buckets[0].Successes != 1 || buckets[0].Failures != 1 {
		t.Fatalf("unexpected first bucket metrics: %+v", buckets[0])
	}
	if buckets[1].Requests != 1 || buckets[1].Successes != 1 || buckets[1].Failures != 0 {
		t.Fatalf("unexpected second bucket metrics: %+v", buckets[1])
	}
}

func TestQueryGroupedSummaries(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-1", Timestamp: ts, Model: "m1", Provider: "up-a",
		StatusCode: 200, Latency: 100 * time.Millisecond, InputTokens: 5, OutputTokens: 6,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-2", Timestamp: ts, Model: "m1", Provider: "up-b",
		StatusCode: 500, Latency: 300 * time.Millisecond, InputTokens: 1, OutputTokens: 1, Error: "boom",
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID: "req-3", Timestamp: ts, Model: "m2", Provider: "up-a",
		StatusCode: 200, Latency: 200 * time.Millisecond, InputTokens: 2, OutputTokens: 3,
	})
	s.Flush()

	byModel := s.QueryModelSummaries(time.Hour, 10)
	if len(byModel) != 2 {
		t.Fatalf("expected 2 model summaries, got %d", len(byModel))
	}
	if byModel[0].GroupValue != "m1" || byModel[0].Requests != 2 || byModel[0].Successes != 1 || byModel[0].Failures != 1 {
		t.Fatalf("unexpected m1 summary: %+v", byModel[0])
	}
	if byModel[0].InputTokens != 6 || byModel[0].OutputTokens != 7 {
		t.Fatalf("unexpected m1 token totals: %+v", byModel[0])
	}
	if byModel[0].AvgLatencyMs != 200 {
		t.Fatalf("expected m1 avg latency 200, got %f", byModel[0].AvgLatencyMs)
	}

	byUpstream := s.QueryUpstreamSummaries(time.Hour, 1)
	if len(byUpstream) != 1 {
		t.Fatalf("expected 1 upstream summary due limit, got %d", len(byUpstream))
	}
	if byUpstream[0].GroupValue != "up-a" || byUpstream[0].Requests != 2 || byUpstream[0].Successes != 2 || byUpstream[0].Failures != 0 {
		t.Fatalf("unexpected upstream summary: %+v", byUpstream[0])
	}
	if byUpstream[0].AvgLatencyMs != 150 {
		t.Fatalf("expected upstream avg latency 150, got %f", byUpstream[0].AvgLatencyMs)
	}
}

func TestQueryModelRouteUsage(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID:          "req-route-1",
		Timestamp:          ts,
		RequestedModel:     "gpt-5.2",
		EffectiveModel:     "gpt-5.4",
		Model:              "gpt-5.4",
		Provider:           "up-a",
		StatusCode:         200,
		Latency:            100 * time.Millisecond,
		InputTokens:        100,
		CachedPromptTokens: 25,
		OutputTokens:       40,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID:          "req-route-2",
		Timestamp:          ts.Add(2 * time.Second),
		RequestedModel:     "gpt-5.2",
		EffectiveModel:     "gpt-5.4",
		Model:              "gpt-5.4",
		Provider:           "up-b",
		StatusCode:         200,
		Latency:            120 * time.Millisecond,
		InputTokens:        50,
		CachedPromptTokens: 10,
		OutputTokens:       15,
	})
	_ = s.Record(ctx, &core.RequestRecord{
		RequestID:      "req-route-3",
		Timestamp:      ts.Add(4 * time.Second),
		RequestedModel: "claude-sonnet-4-6",
		EffectiveModel: "claude-sonnet-4-6",
		Model:          "claude-sonnet-4-6",
		Provider:       "up-c",
		StatusCode:     200,
		Latency:        90 * time.Millisecond,
		InputTokens:    30,
		OutputTokens:   20,
	})
	s.Flush()

	rows := s.QueryModelRouteUsage(time.Hour, 10)
	if len(rows) != 2 {
		t.Fatalf("expected 2 grouped rows, got %d", len(rows))
	}
	if rows[0].RequestedModel != "gpt-5.2" || rows[0].EffectiveModel != "gpt-5.4" {
		t.Fatalf("unexpected top grouped row: %+v", rows[0])
	}
	if rows[0].Requests != 2 || rows[0].InputTokens != 150 || rows[0].CachedPromptTokens != 35 || rows[0].OutputTokens != 55 || rows[0].TotalTokens != 205 {
		t.Fatalf("unexpected gpt route totals: %+v", rows[0])
	}
	if rows[1].RequestedModel != "claude-sonnet-4-6" || rows[1].EffectiveModel != "claude-sonnet-4-6" {
		t.Fatalf("unexpected second grouped row: %+v", rows[1])
	}
}

func TestClose_Idempotent(t *testing.T) {
	cfg := testConfig(t)
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close() error: %v", err)
	}
}

func TestNew_MigratesLegacySchema_AdditiveColumns(t *testing.T) {
	cfg := testConfig(t)

	db, err := sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	legacySchema := `
CREATE TABLE IF NOT EXISTS requests (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp     TEXT    NOT NULL,
  request_id    TEXT    NOT NULL,
  model         TEXT,
  provider      TEXT,
  status_code   INTEGER NOT NULL,
  latency_ms    INTEGER NOT NULL,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  stream        INTEGER NOT NULL DEFAULT 0,
  error_message TEXT
);
CREATE TABLE IF NOT EXISTS agg_minutes (
  bucket        TEXT    NOT NULL,
  model         TEXT    NOT NULL DEFAULT '',
  provider      TEXT    NOT NULL DEFAULT '',
  requests      INTEGER NOT NULL DEFAULT 0,
  successes     INTEGER NOT NULL DEFAULT 0,
  failures      INTEGER NOT NULL DEFAULT 0,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  total_latency INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket, model, provider)
);`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with legacy schema error: %v", err)
	}
	defer s.Close()

	assertHasColumn := func(table, column string) {
		t.Helper()
		if !validSQLIdentifier(table) {
			t.Fatalf("invalid table name: %q", table)
		}
		rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
		if err != nil {
			t.Fatalf("inspect table info %s: %v", table, err)
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			var (
				cid        int
				name       string
				kind       string
				notNull    int
				defaultVal sql.NullString
				primaryKey int
			)
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultVal, &primaryKey); err != nil {
				t.Fatalf("scan table info %s: %v", table, err)
			}
			if name == column {
				found = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate table info %s: %v", table, err)
		}
		if !found {
			t.Fatalf("expected column %s.%s to exist after migration", table, column)
		}
	}

	assertHasColumn("requests", "path")
	assertHasColumn("requests", "requested_model")
	assertHasColumn("requests", "effective_model")
	assertHasColumn("requests", "route_mode")
	assertHasColumn("requests", "attempts")
	assertHasColumn("requests", "cached_prompt_tokens")
	assertHasColumn("agg_minutes", "cached_prompt_tokens")

	if err := s.Record(context.Background(), &core.RequestRecord{
		RequestID:          "legacy-write",
		Timestamp:          time.Now().UTC(),
		Path:               "/v1/chat/completions",
		RequestedModel:     "gpt-4o",
		EffectiveModel:     "gpt-4o",
		Model:              "gpt-4o",
		RouteMode:          "direct",
		Provider:           "openai",
		StatusCode:         200,
		Latency:            123 * time.Millisecond,
		Attempts:           1,
		InputTokens:        7,
		CachedPromptTokens: 3,
		OutputTokens:       2,
	}); err != nil {
		t.Fatalf("record migrated schema row: %v", err)
	}
	s.Flush()

	rows := s.QueryRecentRequests(1)
	if len(rows) != 1 {
		t.Fatalf("expected 1 recent row, got %d", len(rows))
	}
	if rows[0].Path != "/v1/chat/completions" || rows[0].CachedPromptTokens != 3 {
		t.Fatalf("unexpected migrated row content: %+v", rows[0])
	}
}
