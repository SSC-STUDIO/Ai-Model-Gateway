// Package telemetrydb implements the core.TelemetrySink interface backed by
// SQLite with WAL journal mode, bounded retention, pre-aggregated time-series
// buckets, and a small TTL query cache.
package telemetrydb

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/core"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	writeBatchSize     = 256  // Increased from 64 for higher throughput
	writeFlushInterval = 100 * time.Millisecond  // Faster flush for lower latency
	writeQueueSize     = 1024
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store is a SQLite-backed TelemetrySink.
type Store struct {
	db  *sql.DB
	cfg core.TelemetryConfig

	// Prepared statements
	insertStmt *sql.Stmt
	stmtCache  map[string]*sql.Stmt
	stmtMu     sync.RWMutex

	// Async writer
	writeCh   chan *core.RequestRecord
	flushCh   chan chan struct{}
	writerWg  sync.WaitGroup
	closeOnce sync.Once

	// Retention goroutine stop
	stopCh chan struct{}

	// TTL cache
	cacheMu      sync.Mutex
	cacheVersion atomic.Uint64
	queryCache   map[string]cachedResult
}

type cachedResult struct {
	version uint64
	expires time.Time
	value   interface{}
}

var sqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// New opens (or creates) the SQLite database at cfg.SQLitePath, applies
// WAL pragmas, creates the schema, and starts the background writer.
func New(cfg core.TelemetryConfig) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
		return nil, fmt.Errorf("create telemetry dir: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open telemetry db: %w", err)
	}

	s := &Store{
		db:         db,
		cfg:        cfg,
		writeCh:    make(chan *core.RequestRecord, writeQueueSize),
		flushCh:    make(chan chan struct{}, 4),
		stopCh:     make(chan struct{}),
		queryCache: make(map[string]cachedResult),
		stmtCache:  make(map[string]*sql.Stmt),
	}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.prepareStatements(); err != nil {
		_ = db.Close()
		return nil, err
	}

	s.writerWg.Add(1)
	go s.writerLoop()

	// Start background retention cleanup (runs every hour).
	go s.retentionLoop()

	return s, nil
}

// Record enqueues a telemetry record for async persistence.
func (s *Store) Record(ctx context.Context, rec *core.RequestRecord) error {
	select {
	case s.writeCh <- rec:
		s.cacheVersion.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Queue full — drop to avoid blocking the hot path.
		return nil
	}
}

// Flush blocks until all enqueued writes have been persisted.
func (s *Store) Flush() {
	done := make(chan struct{})
	s.flushCh <- done
	<-done
}

// Close flushes pending writes and closes the database.
func (s *Store) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.stopCh)  // stop retention goroutine
		close(s.writeCh) // stop writer goroutine
		s.writerWg.Wait()
		if s.insertStmt != nil {
			_ = s.insertStmt.Close()
		}
		// Close all cached prepared statements
		s.stmtMu.Lock()
		for _, stmt := range s.stmtCache {
			_ = stmt.Close()
		}
		s.stmtCache = nil
		s.stmtMu.Unlock()
		closeErr = s.db.Close()
	})
	return closeErr
}

// ---------------------------------------------------------------------------
// Schema & Pragmas
// ---------------------------------------------------------------------------

func (s *Store) initSchema() error {
	pragmas := `
PRAGMA journal_mode = WAL;
PRAGMA synchronous  = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size   = -2000;
`
	if _, err := s.db.Exec(pragmas); err != nil {
		return fmt.Errorf("set pragmas: %w", err)
	}

	schema := `
CREATE TABLE IF NOT EXISTS requests (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp     TEXT    NOT NULL,
  request_id    TEXT    NOT NULL,
  path          TEXT    NOT NULL DEFAULT '',
  requested_model TEXT  NOT NULL DEFAULT '',
  effective_model TEXT  NOT NULL DEFAULT '',
  model         TEXT,
  route_mode    TEXT    NOT NULL DEFAULT '',
  provider      TEXT,
  status_code   INTEGER NOT NULL,
  latency_ms    INTEGER NOT NULL,
  attempts      INTEGER NOT NULL DEFAULT 1,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  stream        INTEGER NOT NULL DEFAULT 0,
  error_message TEXT
);
CREATE INDEX IF NOT EXISTS idx_requests_ts ON requests(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests(provider);

CREATE TABLE IF NOT EXISTS agg_minutes (
  bucket        TEXT    NOT NULL,
  model         TEXT    NOT NULL DEFAULT '',
  provider      TEXT    NOT NULL DEFAULT '',
  requests      INTEGER NOT NULL DEFAULT 0,
  successes     INTEGER NOT NULL DEFAULT 0,
  failures      INTEGER NOT NULL DEFAULT 0,
  input_tokens  INTEGER NOT NULL DEFAULT 0,
  cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  total_latency INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket, model, provider)
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	if err := s.ensureColumn("requests", "path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "requested_model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "effective_model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "route_mode", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "attempts", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "cached_prompt_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("agg_minutes", "cached_prompt_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func (s *Store) prepareStatements() error {
	var err error
	s.insertStmt, err = s.db.Prepare(`
INSERT INTO requests (timestamp, request_id, path, requested_model, effective_model, model, route_mode,
                      provider, status_code, latency_ms, attempts, input_tokens, cached_prompt_tokens,
                      output_tokens, stream, error_message)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	return nil
}

// getStmt returns a cached prepared statement or creates a new one
func (s *Store) getStmt(query string) (*sql.Stmt, error) {
	// Fast path: read from cache
	s.stmtMu.RLock()
	if stmt, ok := s.stmtCache[query]; ok {
		s.stmtMu.RUnlock()
		return stmt, nil
	}
	s.stmtMu.RUnlock()

	// Slow path: prepare and cache
	s.stmtMu.Lock()
	defer s.stmtMu.Unlock()

	// Double-check after acquiring write lock
	if stmt, ok := s.stmtCache[query]; ok {
		return stmt, nil
	}

	stmt, err := s.db.Prepare(query)
	if err != nil {
		return nil, err
	}

	s.stmtCache[query] = stmt
	return stmt, nil
}

// ---------------------------------------------------------------------------
// Background writer — batches inserts for lower write-amplification
// ---------------------------------------------------------------------------

func (s *Store) writerLoop() {
	defer s.writerWg.Done()

	batch := make([]*core.RequestRecord, 0, writeBatchSize)
	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.writeBatch(batch); err != nil {
			log.Printf("[telemetry] write batch error: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case rec, ok := <-s.writeCh:
			if !ok {
				flushBatch()
				return
			}
			batch = append(batch, rec)
			if len(batch) >= writeBatchSize {
				flushBatch()
			}

		case <-ticker.C:
			flushBatch()

		case done := <-s.flushCh:
			// Drain remaining records from channel.
			draining := true
			for draining {
				select {
				case rec, ok := <-s.writeCh:
					if !ok {
						draining = false
					} else {
						batch = append(batch, rec)
					}
				default:
					draining = false
				}
			}
			flushBatch()
			close(done)
		}
	}
}

func (s *Store) writeBatch(batch []*core.RequestRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt := tx.Stmt(s.insertStmt)

	aggUpdates := make(map[string]*aggKey)

	for _, rec := range batch {
		ts := rec.Timestamp.UTC().Format(time.RFC3339Nano)
		errMsg := ""
		if rec.Error != "" {
			errMsg = rec.Error
		}
		streamInt := 0
		if rec.Stream {
			streamInt = 1
		}
		effectiveModel := rec.EffectiveModel
		if effectiveModel == "" {
			effectiveModel = rec.Model
		}
		legacyModel := rec.Model
		if legacyModel == "" {
			legacyModel = effectiveModel
		}
		attempts := rec.Attempts
		if attempts <= 0 {
			attempts = 1
		}
		if _, err := stmt.Exec(
			ts, rec.RequestID, rec.Path, rec.RequestedModel, effectiveModel, legacyModel, rec.RouteMode,
			rec.Provider,
			rec.StatusCode, rec.Latency.Milliseconds(),
			attempts,
			rec.InputTokens, rec.CachedPromptTokens, rec.OutputTokens,
			streamInt, errMsg,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert record: %w", err)
		}

		// Accumulate pre-aggregation data.
		bucket := rec.Timestamp.UTC().Truncate(time.Duration(s.cfg.AggregationSec) * time.Second).Format(time.RFC3339)
		key := bucket + "|" + legacyModel + "|" + rec.Provider
		agg, ok := aggUpdates[key]
		if !ok {
			agg = &aggKey{bucket: bucket, model: legacyModel, provider: rec.Provider}
			aggUpdates[key] = agg
		}
		agg.requests++
		if rec.StatusCode >= 200 && rec.StatusCode < 400 {
			agg.successes++
		} else {
			agg.failures++
		}
		agg.inputTokens += rec.InputTokens
		agg.cachedPromptTokens += rec.CachedPromptTokens
		agg.outputTokens += rec.OutputTokens
		agg.totalLatency += rec.Latency.Milliseconds()
	}

	// Upsert aggregation rows.
	for _, agg := range aggUpdates {
		if _, err := tx.Exec(`
INSERT INTO agg_minutes (bucket, model, provider, requests, successes, failures,
                         input_tokens, cached_prompt_tokens, output_tokens, total_latency)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(bucket, model, provider) DO UPDATE SET
  requests      = requests + excluded.requests,
  successes     = successes + excluded.successes,
  failures      = failures + excluded.failures,
  input_tokens  = input_tokens + excluded.input_tokens,
  cached_prompt_tokens = cached_prompt_tokens + excluded.cached_prompt_tokens,
  output_tokens = output_tokens + excluded.output_tokens,
  total_latency = total_latency + excluded.total_latency`,
			agg.bucket, agg.model, agg.provider,
			agg.requests, agg.successes, agg.failures,
			agg.inputTokens, agg.cachedPromptTokens, agg.outputTokens, agg.totalLatency,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert agg: %w", err)
		}
	}

	return tx.Commit()
}

type aggKey struct {
	bucket             string
	model              string
	provider           string
	requests           int
	successes          int
	failures           int
	inputTokens        int64
	cachedPromptTokens int64
	outputTokens       int64
	totalLatency       int64
}

// RecentRequest is a lightweight view used by admin query endpoints.
type RecentRequest struct {
	Timestamp          time.Time
	RequestID          string
	Path               string
	RequestedModel     string
	EffectiveModel     string
	Model              string
	RouteMode          string
	Attempts           int
	Upstream           string
	StatusCode         int
	LatencyMs          int64
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	Stream             bool
	Error              string
}

// RecentError is a lightweight view of recent failed/error requests.
type RecentError struct {
	Timestamp      time.Time
	RequestID      string
	Path           string
	RequestedModel string
	EffectiveModel string
	Model          string
	RouteMode      string
	Attempts       int
	Upstream       string
	StatusCode     int
	LatencyMs      int64
	Message        string
}

// TimeSeriesBucket is a coarse bucket used for charting request trends.
type TimeSeriesBucket struct {
	Bucket             string
	Requests           int
	Successes          int
	Failures           int
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	AvgLatencyMs       float64
}

// GroupSummary is an aggregated row grouped by model or upstream.
type GroupSummary struct {
	GroupValue         string
	Requests           int
	Successes          int
	Failures           int
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	AvgLatencyMs       float64
}

// ModelRouteUsage aggregates usage by requested/effective model pair.
type ModelRouteUsage struct {
	RequestedModel     string
	EffectiveModel     string
	Requests           int
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	TotalTokens        int64
}

// ModelBenchmark aggregates benchmark metrics for a model.
type ModelBenchmark struct {
	Model              string
	Requests           int
	Successes          int
	Failures           int
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	AvgLatencyMs       float64
	P50LatencyMs       float64
	P95LatencyMs       float64
	P99LatencyMs       float64
	MaxLatencyMs       int64
	SuccessRate        float64
	EstimatedCostUSD   float64
}

// ---------------------------------------------------------------------------
// Retention cleanup — deletes rows older than retention_days
// ---------------------------------------------------------------------------

func (s *Store) retentionLoop() {
	// Delay the initial prune slightly so a fast Close() in tests
	// can signal stopCh before we touch the DB.
	select {
	case <-s.stopCh:
		return
	case <-time.After(500 * time.Millisecond):
		s.pruneOldData()
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.pruneOldData()
		}
	}
}

func (s *Store) pruneOldData() {
	// Check if store is shutting down before touching the DB.
	select {
	case <-s.stopCh:
		return
	default:
	}
	cutoff := time.Now().AddDate(0, 0, -s.cfg.RetentionDays).UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(`DELETE FROM requests WHERE timestamp < ?`, cutoff); err != nil {
		log.Printf("[telemetry] prune requests: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM agg_minutes WHERE bucket < ?`, cutoff); err != nil {
		log.Printf("[telemetry] prune agg_minutes: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Query helpers (used by admin API — simple TTL cache)
// ---------------------------------------------------------------------------

// QueryWindowMetrics returns aggregated metrics for the given time window.
func (s *Store) QueryWindowMetrics(window time.Duration) (requests, successes, failures int, avgLatencyMs float64) {
	if window <= 0 {
		window = 5 * time.Minute
	}
	cacheKey := fmt.Sprintf("window:%v", window)
	if cached, ok := s.getCached(cacheKey); ok {
		v := cached.([4]interface{})
		return v[0].(int), v[1].(int), v[2].(int), v[3].(float64)
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339)
	row := s.db.QueryRow(`
SELECT
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_minutes
WHERE bucket >= ?`, cutoff)

	_ = row.Scan(&requests, &successes, &failures, &avgLatencyMs)
	s.setCached(cacheKey, [4]interface{}{requests, successes, failures, avgLatencyMs})
	return
}

// QueryRecentRequests returns the most recent request rows ordered by timestamp.
func (s *Store) QueryRecentRequests(limit int) []RecentRequest {
	if limit <= 0 {
		limit = 50
	}
	cacheKey := fmt.Sprintf("recent_requests:%d", limit)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]RecentRequest)
	}

	rows, err := s.db.Query(`
SELECT timestamp, request_id, path, requested_model, effective_model, model, route_mode,
       provider, status_code, latency_ms, attempts, input_tokens, cached_prompt_tokens,
       output_tokens, stream, error_message
FROM requests
ORDER BY timestamp DESC
LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make([]RecentRequest, 0, limit)
	for rows.Next() {
		var (
			timestamp string
			streamInt int
			record    RecentRequest
		)
		if err := rows.Scan(
			&timestamp,
			&record.RequestID,
			&record.Path,
			&record.RequestedModel,
			&record.EffectiveModel,
			&record.Model,
			&record.RouteMode,
			&record.Upstream,
			&record.StatusCode,
			&record.LatencyMs,
			&record.Attempts,
			&record.InputTokens,
			&record.CachedPromptTokens,
			&record.OutputTokens,
			&streamInt,
			&record.Error,
		); err != nil {
			continue
		}
		if record.Attempts <= 0 {
			record.Attempts = 1
		}
		if record.EffectiveModel == "" {
			record.EffectiveModel = record.Model
		}
		record.Timestamp = parseTimestamp(timestamp)
		record.Stream = streamInt == 1
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return result
	}
	s.setCached(cacheKey, result)
	return result
}

// QueryRecentErrors returns recent rows that represent failures or explicit errors.
func (s *Store) QueryRecentErrors(limit int) []RecentError {
	if limit <= 0 {
		limit = 50
	}
	cacheKey := fmt.Sprintf("recent_errors:%d", limit)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]RecentError)
	}

	rows, err := s.db.Query(`
SELECT timestamp, request_id, path, requested_model, effective_model, model, route_mode,
       provider, status_code, latency_ms, attempts, error_message
FROM requests
WHERE status_code >= 400 OR error_message != ''
ORDER BY timestamp DESC
LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make([]RecentError, 0, limit)
	for rows.Next() {
		var (
			timestamp string
			record    RecentError
		)
		if err := rows.Scan(
			&timestamp,
			&record.RequestID,
			&record.Path,
			&record.RequestedModel,
			&record.EffectiveModel,
			&record.Model,
			&record.RouteMode,
			&record.Upstream,
			&record.StatusCode,
			&record.LatencyMs,
			&record.Attempts,
			&record.Message,
		); err != nil {
			continue
		}
		if record.Attempts <= 0 {
			record.Attempts = 1
		}
		if record.EffectiveModel == "" {
			record.EffectiveModel = record.Model
		}
		record.Timestamp = parseTimestamp(timestamp)
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return result
	}
	s.setCached(cacheKey, result)
	return result
}

// QueryTimeSeriesBuckets returns aggregated rows grouped into the requested bucket size.
func (s *Store) QueryTimeSeriesBuckets(window time.Duration, bucketSize time.Duration) []TimeSeriesBucket {
	if window <= 0 {
		window = 24 * time.Hour
	}

	baseBucketSec := s.cfg.AggregationSec
	if baseBucketSec <= 0 {
		baseBucketSec = 60
	}
	baseBucket := time.Duration(baseBucketSec) * time.Second
	if bucketSize <= 0 {
		bucketSize = baseBucket
	}
	if bucketSize < baseBucket {
		bucketSize = baseBucket
	}
	groupFactor := int(bucketSize / baseBucket)
	if bucketSize%baseBucket != 0 {
		groupFactor++
	}
	groupSec := groupFactor * baseBucketSec

	cacheKey := fmt.Sprintf("timeseries:%v:%d", window, groupSec)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]TimeSeriesBucket)
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`
SELECT
  strftime('%Y-%m-%dT%H:%M:%SZ',
           datetime((CAST(strftime('%s', bucket) AS INTEGER) / ?) * ?, 'unixepoch')) AS grouped_bucket,
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_minutes
WHERE bucket >= ?
GROUP BY grouped_bucket
ORDER BY grouped_bucket ASC`, groupSec, groupSec, cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []TimeSeriesBucket
	for rows.Next() {
		var bucket TimeSeriesBucket
		if err := rows.Scan(
			&bucket.Bucket,
			&bucket.Requests,
			&bucket.Successes,
			&bucket.Failures,
			&bucket.InputTokens,
			&bucket.CachedPromptTokens,
			&bucket.OutputTokens,
			&bucket.AvgLatencyMs,
		); err != nil {
			continue
		}
		result = append(result, bucket)
	}
	if err := rows.Err(); err != nil {
		return result
	}
	s.setCached(cacheKey, result)
	return result
}

// QueryModelSummaries returns grouped aggregates for models in the given window.
func (s *Store) QueryModelSummaries(window time.Duration, limit int) []GroupSummary {
	return s.queryGroupedSummaries(window, limit, "model")
}

// QueryUpstreamSummaries returns grouped aggregates for upstream providers in the given window.
func (s *Store) QueryUpstreamSummaries(window time.Duration, limit int) []GroupSummary {
	return s.queryGroupedSummaries(window, limit, "provider")
}

// QueryModelRouteUsage returns aggregated usage grouped by requested/effective model pair.
// When window <= 0, it uses the full retained dataset.
func (s *Store) QueryModelRouteUsage(window time.Duration, limit int) []ModelRouteUsage {
	if limit <= 0 {
		limit = 200
	}

	cacheKey := fmt.Sprintf("model_route_usage:%v:%d", window, limit)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]ModelRouteUsage)
	}

	query := `
SELECT
  COALESCE(requested_model, ''),
  COALESCE(NULLIF(effective_model, ''), COALESCE(model, '')),
  COALESCE(COUNT(*), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(input_tokens + output_tokens), 0)
FROM requests
WHERE (COALESCE(requested_model, '') != '' OR COALESCE(NULLIF(effective_model, ''), COALESCE(model, '')) != '')`
	args := make([]interface{}, 0, 2)
	if window > 0 {
		query += "\n  AND timestamp >= ?"
		args = append(args, time.Now().Add(-window).UTC().Format(time.RFC3339))
	}
	query += `
GROUP BY
  COALESCE(requested_model, ''),
  COALESCE(NULLIF(effective_model, ''), COALESCE(model, ''))
ORDER BY
  COALESCE(SUM(input_tokens + output_tokens), 0) DESC,
  COALESCE(NULLIF(effective_model, ''), COALESCE(model, '')) ASC,
  COALESCE(requested_model, '') ASC
LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make([]ModelRouteUsage, 0, limit)
	for rows.Next() {
		var row ModelRouteUsage
		if err := rows.Scan(
			&row.RequestedModel,
			&row.EffectiveModel,
			&row.Requests,
			&row.InputTokens,
			&row.CachedPromptTokens,
			&row.OutputTokens,
			&row.TotalTokens,
		); err != nil {
			continue
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return result
	}
	s.setCached(cacheKey, result)
	return result
}

// QueryModelBenchmark returns detailed benchmark metrics for models in the given time window.
// Supports filtering by specific models if provided.
func (s *Store) QueryModelBenchmark(window time.Duration, models []string) []ModelBenchmark {
	if window <= 0 {
		window = 24 * time.Hour
	}

	cacheKey := fmt.Sprintf("benchmark:%v:%v", window, models)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]ModelBenchmark)
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339)

	// Build the base query
	query := `
SELECT
  model,
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_minutes
WHERE bucket >= ?
  AND model != ''`

	args := []interface{}{cutoff}

	// Add model filter if specified
	if len(models) > 0 {
		placeholders := make([]string, len(models))
		for i, m := range models {
			placeholders[i] = "?"
			args = append(args, m)
		}
		query += " AND model IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += `
GROUP BY model
ORDER BY COALESCE(SUM(requests), 0) DESC, model ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []ModelBenchmark
	for rows.Next() {
		var row ModelBenchmark
		if err := rows.Scan(
			&row.Model,
			&row.Requests,
			&row.Successes,
			&row.Failures,
			&row.InputTokens,
			&row.CachedPromptTokens,
			&row.OutputTokens,
			&row.AvgLatencyMs,
		); err != nil {
			continue
		}
		if row.Requests > 0 {
			row.SuccessRate = float64(row.Successes) / float64(row.Requests) * 100
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return result
	}

	// Calculate percentiles for each model
	for i := range result {
		s.calculatePercentiles(&result[i], cutoff)
	}

	s.setCached(cacheKey, result)
	return result
}

// calculatePercentiles calculates p50, p95, p99 and max latency for a model.
func (s *Store) calculatePercentiles(bm *ModelBenchmark, cutoff string) {
	// Get all latency values for this model
	rows, err := s.db.Query(`
		SELECT latency_ms FROM requests
		WHERE timestamp >= ? AND model = ? AND latency_ms > 0
		ORDER BY latency_ms ASC`, cutoff, bm.Model)
	if err != nil {
		return
	}
	defer rows.Close()

	var latencies []int64
	var maxLatency int64
	for rows.Next() {
		var lat int64
		if err := rows.Scan(&lat); err != nil {
			continue
		}
		latencies = append(latencies, lat)
		if lat > maxLatency {
			maxLatency = lat
		}
	}

	if len(latencies) == 0 {
		return
	}

	bm.MaxLatencyMs = maxLatency
	n := len(latencies)

	// P50 (median)
	if n%2 == 0 {
		bm.P50LatencyMs = float64(latencies[n/2-1]+latencies[n/2]) / 2
	} else {
		bm.P50LatencyMs = float64(latencies[n/2])
	}

	// P95
	p95Idx := int(float64(n) * 0.95)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	bm.P95LatencyMs = float64(latencies[p95Idx])

	// P99
	p99Idx := int(float64(n) * 0.99)
	if p99Idx >= n {
		p99Idx = n - 1
	}
	bm.P99LatencyMs = float64(latencies[p99Idx])
}

func (s *Store) queryGroupedSummaries(window time.Duration, limit int, groupColumn string) []GroupSummary {
	if window <= 0 {
		window = 24 * time.Hour
	}
	if limit <= 0 {
		limit = 20
	}

	column := "model"
	query := `
SELECT
  model,
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_minutes
WHERE bucket >= ?
  AND model != ''
GROUP BY model
ORDER BY COALESCE(SUM(requests), 0) DESC, model ASC
LIMIT ?`
	switch groupColumn {
	case "provider":
		column = "provider"
		query = `
SELECT
  provider,
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_minutes
WHERE bucket >= ?
  AND provider != ''
GROUP BY provider
ORDER BY COALESCE(SUM(requests), 0) DESC, provider ASC
LIMIT ?`
	case "model":
		column = "model"
	default:
		return nil
	}

	cacheKey := fmt.Sprintf("grouped:%s:%v:%d", column, window, limit)
	if cached, ok := s.getCached(cacheKey); ok {
		return cached.([]GroupSummary)
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339)
	rows, err := s.db.Query(query, cutoff, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make([]GroupSummary, 0, limit)
	for rows.Next() {
		var row GroupSummary
		if err := rows.Scan(
			&row.GroupValue,
			&row.Requests,
			&row.Successes,
			&row.Failures,
			&row.InputTokens,
			&row.CachedPromptTokens,
			&row.OutputTokens,
			&row.AvgLatencyMs,
		); err != nil {
			continue
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return result
	}
	s.setCached(cacheKey, result)
	return result
}

func (s *Store) getCached(cacheKey string) (interface{}, bool) {
	ttl := time.Duration(s.cfg.CacheTTLSec) * time.Second
	if ttl <= 0 {
		return nil, false
	}

	now := time.Now()
	version := s.cacheVersion.Load()
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	c, ok := s.queryCache[cacheKey]
	if !ok || c.version != version || now.After(c.expires) {
		return nil, false
	}
	return c.value, true
}

func (s *Store) setCached(cacheKey string, value interface{}) {
	ttl := time.Duration(s.cfg.CacheTTLSec) * time.Second
	if ttl <= 0 {
		return
	}

	s.cacheMu.Lock()
	s.queryCache[cacheKey] = cachedResult{
		version: s.cacheVersion.Load(),
		expires: time.Now().Add(ttl),
		value:   value,
	}
	s.cacheMu.Unlock()
}

func (s *Store) ensureColumn(table string, column string, columnType string) error {
	if !validSQLIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !validSQLIdentifier(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}
	if !validColumnType(columnType) {
		return fmt.Errorf("invalid column type: %q", columnType)
	}

	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()

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
			return fmt.Errorf("scan table info %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table info %s: %w", table, err)
	}

	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %q %s`, table, column, columnType)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func validSQLIdentifier(value string) bool {
	return sqlIdentifierPattern.MatchString(value)
}

func validColumnType(value string) bool {
	switch value {
	case "TEXT",
		"TEXT NOT NULL DEFAULT ''",
		"INTEGER NOT NULL DEFAULT 0",
		"INTEGER NOT NULL DEFAULT 1":
		return true
	default:
		return false
	}
}

func parseTimestamp(value string) time.Time {
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}
