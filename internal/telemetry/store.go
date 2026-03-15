package telemetry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Usage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CachedPromptTokens int `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
}

type RequestRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	RequestID      string    `json:"request_id"`
	Path           string    `json:"path"`
	RequestedModel string    `json:"requested_model,omitempty"`
	Model          string    `json:"model,omitempty"`
	RouteMode      string    `json:"route_mode,omitempty"`
	Upstream       string    `json:"upstream,omitempty"`
	StatusCode     int       `json:"status_code"`
	Attempts       int       `json:"attempts"`
	DurationMs     int64     `json:"duration_ms"`
	Success        bool      `json:"success"`
	Error          string    `json:"error,omitempty"`
	Usage          Usage     `json:"usage"`
}

type ErrorRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	RequestID      string    `json:"request_id"`
	Path           string    `json:"path"`
	RequestedModel string    `json:"requested_model,omitempty"`
	Model          string    `json:"model,omitempty"`
	RouteMode      string    `json:"route_mode,omitempty"`
	Upstream       string    `json:"upstream,omitempty"`
	StatusCode     int       `json:"status_code"`
	Attempt        int       `json:"attempt"`
	Message        string    `json:"message"`
}

type Summary struct {
	TotalRequests      int   `json:"total_requests"`
	Successes          int   `json:"successes"`
	Failures           int   `json:"failures"`
	PromptTokens       int64 `json:"prompt_tokens"`
	CachedPromptTokens int64 `json:"cached_prompt_tokens"`
	CompletionTokens   int64 `json:"completion_tokens"`
	TotalTokens        int64 `json:"total_tokens"`
}

type WindowMetrics struct {
	WindowLabel        string  `json:"window_label"`
	Requests           int     `json:"requests"`
	Successes          int     `json:"successes"`
	Failures           int     `json:"failures"`
	PromptTokens       int64   `json:"prompt_tokens"`
	CachedPromptTokens int64   `json:"cached_prompt_tokens"`
	CompletionTokens   int64   `json:"completion_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
	RPM                float64 `json:"rpm"`
	TPM                float64 `json:"tpm"`
	AvgLatencyMs       float64 `json:"avg_latency_ms"`
	SuccessRate        float64 `json:"success_rate"`
}

type ModelRouteUsage struct {
	RequestedModel string `json:"requested_model,omitempty"`
	Model          string `json:"model,omitempty"`
	Usage          Usage  `json:"usage"`
}

type Performance struct {
	Last1m WindowMetrics `json:"last_1m"`
	Last5m WindowMetrics `json:"last_5m"`
}

type CacheTrends struct {
	Last1h  WindowMetrics `json:"last_1h"`
	Last24h WindowMetrics `json:"last_24h"`
}

type CacheHitRankItem struct {
	Upstream string `json:"upstream"`
	Requests int    `json:"requests"`
	Usage    Usage  `json:"usage"`
}

type Snapshot struct {
	Summary         Summary            `json:"summary"`
	Performance     Performance        `json:"performance"`
	CacheTrends     CacheTrends        `json:"cache_trends"`
	CacheHitRanking []CacheHitRankItem `json:"cache_hit_ranking"`
	Requests        []RequestRecord    `json:"requests"`
	Errors          []ErrorRecord      `json:"errors"`
	ByModel         map[string]Usage   `json:"by_model"`
	ByUpstream      map[string]Usage   `json:"by_upstream"`
	ByModelRoute    []ModelRouteUsage  `json:"by_model_route"`
	GeneratedAt     time.Time          `json:"generated_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(sqlitePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err != nil {
		return nil, fmt.Errorf("create telemetry dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open telemetry db: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init() error {
	if _, err := s.db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
`); err != nil {
		return fmt.Errorf("configure telemetry pragmas: %w", err)
	}

	schema := `
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  request_id TEXT NOT NULL,
  path TEXT NOT NULL,
  requested_model TEXT,
  model TEXT,
  route_mode TEXT,
  upstream TEXT,
  status_code INTEGER NOT NULL,
  attempts INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  success INTEGER NOT NULL,
  error_message TEXT,
  prompt_tokens INTEGER NOT NULL,
  cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL,
  total_tokens INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS errors (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  request_id TEXT NOT NULL,
  path TEXT NOT NULL,
  requested_model TEXT,
  model TEXT,
  route_mode TEXT,
  upstream TEXT,
  status_code INTEGER NOT NULL,
  attempt INTEGER NOT NULL,
  message TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_errors_timestamp ON errors(timestamp DESC);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("init telemetry schema: %w", err)
	}
	if err := s.ensureColumn("requests", "requested_model", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "route_mode", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("requests", "cached_prompt_tokens", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("errors", "requested_model", "TEXT"); err != nil {
		return err
	}
	if err := s.ensureColumn("errors", "route_mode", "TEXT"); err != nil {
		return err
	}
	return nil
}

func (s *Store) RecordRequest(record RequestRecord) {
	if s == nil || s.db == nil {
		return
	}

	_, _ = s.db.Exec(
		`INSERT INTO requests (
			timestamp, request_id, path, requested_model, model, route_mode, upstream, status_code, attempts, duration_ms,
			success, error_message, prompt_tokens, cached_prompt_tokens, completion_tokens, total_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp.UTC().Format(time.RFC3339Nano),
		record.RequestID,
		record.Path,
		record.RequestedModel,
		record.Model,
		record.RouteMode,
		record.Upstream,
		record.StatusCode,
		record.Attempts,
		record.DurationMs,
		boolToInt(record.Success),
		record.Error,
		record.Usage.PromptTokens,
		record.Usage.CachedPromptTokens,
		record.Usage.CompletionTokens,
		record.Usage.TotalTokens,
	)
}

func (s *Store) RecordError(record ErrorRecord) {
	if s == nil || s.db == nil {
		return
	}

	_, _ = s.db.Exec(
		`INSERT INTO errors (
			timestamp, request_id, path, requested_model, model, route_mode, upstream, status_code, attempt, message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp.UTC().Format(time.RFC3339Nano),
		record.RequestID,
		record.Path,
		record.RequestedModel,
		record.Model,
		record.RouteMode,
		record.Upstream,
		record.StatusCode,
		record.Attempt,
		record.Message,
	)
}

func (s *Store) Snapshot() Snapshot {
	if s == nil || s.db == nil {
		return Snapshot{GeneratedAt: time.Now()}
	}

	return Snapshot{
		Summary:         s.querySummary(),
		Performance:     s.queryPerformance(),
		CacheTrends:     s.queryCacheTrends(),
		CacheHitRanking: s.queryCacheHitRanking(24*time.Hour, 10),
		Requests:        s.queryRequests(200),
		Errors:          s.queryErrors(200),
		ByModel:         s.queryUsageBreakdown("model"),
		ByUpstream:      s.queryUsageBreakdown("upstream"),
		ByModelRoute:    s.queryModelRouteBreakdown(),
		GeneratedAt:     time.Now(),
	}
}

func (s *Store) queryPerformance() Performance {
	return Performance{
		Last1m: s.queryWindowMetrics(time.Minute, "1m"),
		Last5m: s.queryWindowMetrics(5*time.Minute, "5m"),
	}
}

func (s *Store) queryCacheTrends() CacheTrends {
	return CacheTrends{
		Last1h:  s.queryWindowMetrics(time.Hour, "1h"),
		Last24h: s.queryWindowMetrics(24*time.Hour, "24h"),
	}
}

func (s *Store) querySummary() Summary {
	var summary Summary
	row := s.db.QueryRow(`
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(total_tokens), 0)
FROM requests`)
	_ = row.Scan(
		&summary.TotalRequests,
		&summary.Successes,
		&summary.Failures,
		&summary.PromptTokens,
		&summary.CachedPromptTokens,
		&summary.CompletionTokens,
		&summary.TotalTokens,
	)
	return summary
}

func (s *Store) queryWindowMetrics(window time.Duration, label string) WindowMetrics {
	metrics := WindowMetrics{WindowLabel: label}
	if s == nil || s.db == nil {
		return metrics
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339Nano)
	row := s.db.QueryRow(`
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(total_tokens), 0),
  COALESCE(AVG(duration_ms), 0)
FROM requests
WHERE timestamp >= ?`, cutoff)

	_ = row.Scan(
		&metrics.Requests,
		&metrics.Successes,
		&metrics.Failures,
		&metrics.PromptTokens,
		&metrics.CachedPromptTokens,
		&metrics.CompletionTokens,
		&metrics.TotalTokens,
		&metrics.AvgLatencyMs,
	)

	minutes := window.Minutes()
	if minutes > 0 {
		metrics.RPM = float64(metrics.Requests) / minutes
		metrics.TPM = float64(metrics.TotalTokens) / minutes
	}
	if metrics.Requests > 0 {
		metrics.SuccessRate = (float64(metrics.Successes) / float64(metrics.Requests)) * 100
	}
	return metrics
}

func (s *Store) queryCacheHitRanking(window time.Duration, limit int) []CacheHitRankItem {
	if s == nil || s.db == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}

	cutoff := time.Now().Add(-window).UTC().Format(time.RFC3339Nano)
	rows, err := s.db.Query(`
SELECT
  upstream,
  COUNT(*),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(total_tokens), 0)
FROM requests
WHERE timestamp >= ?
  AND upstream IS NOT NULL
  AND upstream != ''
GROUP BY upstream
ORDER BY
  CASE WHEN SUM(prompt_tokens) > 0 THEN CAST(SUM(cached_prompt_tokens) AS REAL) / SUM(prompt_tokens) ELSE 0 END DESC,
  COALESCE(SUM(prompt_tokens), 0) DESC
LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []CacheHitRankItem
	for rows.Next() {
		var item CacheHitRankItem
		if err := rows.Scan(
			&item.Upstream,
			&item.Requests,
			&item.Usage.PromptTokens,
			&item.Usage.CachedPromptTokens,
			&item.Usage.CompletionTokens,
			&item.Usage.TotalTokens,
		); err != nil {
			continue
		}
		result = append(result, item)
	}
	return result
}

func (s *Store) queryRequests(limit int) []RequestRecord {
	rows, err := s.db.Query(`
SELECT timestamp, request_id, path, model, upstream, status_code, attempts, duration_ms,
       requested_model, route_mode, success, error_message, prompt_tokens, cached_prompt_tokens, completion_tokens, total_tokens
FROM requests
ORDER BY timestamp DESC
LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var records []RequestRecord
	for rows.Next() {
		var timestamp string
		var success int
		record := RequestRecord{}
		if err := rows.Scan(
			&timestamp,
			&record.RequestID,
			&record.Path,
			&record.Model,
			&record.Upstream,
			&record.StatusCode,
			&record.Attempts,
			&record.DurationMs,
			&record.RequestedModel,
			&record.RouteMode,
			&success,
			&record.Error,
			&record.Usage.PromptTokens,
			&record.Usage.CachedPromptTokens,
			&record.Usage.CompletionTokens,
			&record.Usage.TotalTokens,
		); err != nil {
			continue
		}
		record.Success = success == 1
		record.Timestamp = parseTime(timestamp)
		records = append(records, record)
	}
	return records
}

func (s *Store) queryErrors(limit int) []ErrorRecord {
	rows, err := s.db.Query(`
SELECT timestamp, request_id, path, model, upstream, status_code, attempt, message, requested_model, route_mode
FROM errors
ORDER BY timestamp DESC
LIMIT ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var records []ErrorRecord
	for rows.Next() {
		var timestamp string
		record := ErrorRecord{}
		if err := rows.Scan(
			&timestamp,
			&record.RequestID,
			&record.Path,
			&record.Model,
			&record.Upstream,
			&record.StatusCode,
			&record.Attempt,
			&record.Message,
			&record.RequestedModel,
			&record.RouteMode,
		); err != nil {
			continue
		}
		record.Timestamp = parseTime(timestamp)
		records = append(records, record)
	}
	return records
}

func (s *Store) queryUsageBreakdown(column string) map[string]Usage {
	query := fmt.Sprintf(`
SELECT %s, COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(total_tokens), 0)
                 , COALESCE(SUM(cached_prompt_tokens), 0)
FROM requests
WHERE %s IS NOT NULL AND %s != ''
GROUP BY %s
ORDER BY COALESCE(SUM(total_tokens), 0) DESC`, column, column, column, column)
	rows, err := s.db.Query(query)
	if err != nil {
		return map[string]Usage{}
	}
	defer rows.Close()

	result := make(map[string]Usage)
	for rows.Next() {
		var key string
		var usage Usage
		if err := rows.Scan(&key, &usage.PromptTokens, &usage.CompletionTokens, &usage.TotalTokens, &usage.CachedPromptTokens); err != nil {
			continue
		}
		result[key] = usage
	}
	return result
}

func (s *Store) queryModelRouteBreakdown() []ModelRouteUsage {
	rows, err := s.db.Query(`
SELECT
  COALESCE(requested_model, ''),
  COALESCE(model, ''),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(total_tokens), 0)
FROM requests
WHERE COALESCE(model, '') != '' OR COALESCE(requested_model, '') != ''
GROUP BY COALESCE(requested_model, ''), COALESCE(model, '')
ORDER BY COALESCE(SUM(total_tokens), 0) DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []ModelRouteUsage
	for rows.Next() {
		item := ModelRouteUsage{}
		if err := rows.Scan(
			&item.RequestedModel,
			&item.Model,
			&item.Usage.PromptTokens,
			&item.Usage.CachedPromptTokens,
			&item.Usage.CompletionTokens,
			&item.Usage.TotalTokens,
		); err != nil {
			continue
		}
		result = append(result, item)
	}
	return result
}

// TimeSeriesBucket holds aggregated metrics for a single time bucket.
type TimeSeriesBucket struct {
	Timestamp        string  `json:"t"`
	Requests         int     `json:"requests"`
	Successes        int     `json:"successes"`
	Failures         int     `json:"failures"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CachedPrompt     int64   `json:"cached_prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
}

// TimeSeriesByUpstream holds per-upstream token usage for a single time bucket.
type TimeSeriesByUpstream struct {
	Timestamp string           `json:"t"`
	Upstreams map[string]int64 `json:"upstreams"`
}

// TimeSeries is the full response for the timeseries endpoint.
type TimeSeries struct {
	Buckets    []TimeSeriesBucket     `json:"buckets"`
	ByUpstream []TimeSeriesByUpstream `json:"by_upstream"`
}

// QueryTimeSeries returns aggregated metrics in fixed-width time buckets.
// hours controls how far back to look; bucketMinutes controls bucket width.
func (s *Store) QueryTimeSeries(hours int, bucketMinutes int) TimeSeries {
	if s == nil || s.db == nil {
		return TimeSeries{}
	}
	if hours <= 0 {
		hours = 24
	}
	if bucketMinutes <= 0 {
		bucketMinutes = 60
	}

	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339Nano)

	// --- overall buckets ---
	rows, err := s.db.Query(`
SELECT
  strftime('%Y-%m-%dT%H:', timestamp) || printf('%02d', (CAST(strftime('%M', timestamp) AS INTEGER) / ?) * ?) || ':00Z' AS bucket,
  COUNT(*),
  COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(total_tokens), 0),
  COALESCE(AVG(duration_ms), 0)
FROM requests
WHERE timestamp >= ?
GROUP BY bucket
ORDER BY bucket ASC`, bucketMinutes, bucketMinutes, cutoff)
	if err != nil {
		return TimeSeries{}
	}

	var buckets []TimeSeriesBucket
	for rows.Next() {
		var b TimeSeriesBucket
		if err := rows.Scan(&b.Timestamp, &b.Requests, &b.Successes, &b.Failures,
			&b.PromptTokens, &b.CachedPrompt, &b.CompletionTokens, &b.TotalTokens,
			&b.AvgLatencyMs); err != nil {
			continue
		}
		buckets = append(buckets, b)
	}
	rows.Close()

	// --- per-upstream token usage buckets ---
	rows2, err := s.db.Query(`
SELECT
  strftime('%Y-%m-%dT%H:', timestamp) || printf('%02d', (CAST(strftime('%M', timestamp) AS INTEGER) / ?) * ?) || ':00Z' AS bucket,
  upstream,
  COALESCE(SUM(total_tokens), 0)
FROM requests
WHERE timestamp >= ?
  AND upstream IS NOT NULL AND upstream != ''
GROUP BY bucket, upstream
ORDER BY bucket ASC`, bucketMinutes, bucketMinutes, cutoff)
	if err != nil {
		return TimeSeries{Buckets: buckets}
	}

	upstreamMap := make(map[string]map[string]int64)
	for rows2.Next() {
		var bucket, upstream string
		var tokens int64
		if err := rows2.Scan(&bucket, &upstream, &tokens); err != nil {
			continue
		}
		if upstreamMap[bucket] == nil {
			upstreamMap[bucket] = make(map[string]int64)
		}
		upstreamMap[bucket][upstream] = tokens
	}
	rows2.Close()

	// build ordered slice
	var byUpstream []TimeSeriesByUpstream
	for _, b := range buckets {
		entry := TimeSeriesByUpstream{Timestamp: b.Timestamp, Upstreams: upstreamMap[b.Timestamp]}
		if entry.Upstreams == nil {
			entry.Upstreams = make(map[string]int64)
		}
		byUpstream = append(byUpstream, entry)
	}

	return TimeSeries{Buckets: buckets, ByUpstream: byUpstream}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (s *Store) ensureColumn(table string, column string, columnType string) error {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("inspect telemetry table %s: %w", table, err)
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
			return fmt.Errorf("scan telemetry table info %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}

	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, columnType)); err != nil {
		return fmt.Errorf("add telemetry column %s.%s: %w", table, column, err)
	}
	return nil
}
