package telemetry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var sqlIdentifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func validSQLIdentifier(name string) bool {
	return sqlIdentifierRe.MatchString(name)
}

// validColumnType 验证 SQL 列类型是否在白名单中
func validColumnType(columnType string) bool {
	allowedTypes := []string{
		"TEXT",
		"INTEGER",
		"REAL",
		"BLOB",
		"NUMERIC",
		"TEXT NOT NULL",
		"INTEGER NOT NULL",
		"INTEGER NOT NULL DEFAULT 0",
		"TEXT NOT NULL DEFAULT ''",
	}

	// 清理输入
	columnType = strings.ToUpper(strings.TrimSpace(columnType))

	for _, allowed := range allowedTypes {
		if columnType == strings.ToUpper(allowed) {
			return true
		}
	}
	return false
}

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

	mu             sync.RWMutex
	summary        Summary
	recentRequests []RequestRecord
	recentErrors   []ErrorRecord
	dataVersion    uint64

	cacheMu         sync.Mutex
	snapshotCache   cachedSnapshot
	timeSeriesCache map[string]cachedTimeSeries
	dbWriteMu       sync.Mutex
	writeMu         sync.RWMutex
	writerCh        chan telemetryWriterMessage
	writerDone      chan struct{}
	closed          bool

	insertRequestStmt *sql.Stmt
	insertErrorStmt   *sql.Stmt
}

type cachedSnapshot struct {
	version uint64
	expires time.Time
	value   Snapshot
}

type cachedTimeSeries struct {
	version uint64
	expires time.Time
	value   TimeSeries
}

type telemetryWrite struct {
	request *RequestRecord
	errRec  *ErrorRecord
}

type telemetryWriterMessage struct {
	write *telemetryWrite
	flush chan struct{}
}

const (
	recentRequestLimit = 200
	recentErrorLimit   = 200
	snapshotCacheTTL   = 2 * time.Second
	timeSeriesCacheTTL = 2 * time.Second
	writeBatchSize     = 64
	writeFlushInterval = 200 * time.Millisecond
	writeQueueSize     = 1024
)

const (
	insertRequestSQL = `
INSERT INTO requests (
	timestamp, request_id, path, requested_model, model, route_mode, upstream, status_code, attempts, duration_ms,
	success, error_message, prompt_tokens, cached_prompt_tokens, completion_tokens, total_tokens
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	insertErrorSQL = `
INSERT INTO errors (
	timestamp, request_id, path, requested_model, model, route_mode, upstream, status_code, attempt, message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
)

func NewStore(sqlitePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err != nil {
		return nil, fmt.Errorf("create telemetry dir: %w", err)
	}

	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open telemetry db: %w", err)
	}

	store := &Store{
		db:              db,
		timeSeriesCache: make(map[string]cachedTimeSeries),
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.prepareStatements(); err != nil {
		_ = store.Close()
		return nil, err
	}
	store.hydrateCaches()
	store.startWriter()
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.stopWriter()
	if s.insertRequestStmt != nil {
		_ = s.insertRequestStmt.Close()
	}
	if s.insertErrorStmt != nil {
		_ = s.insertErrorStmt.Close()
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

	s.mu.Lock()
	s.summary.TotalRequests++
	if record.Success {
		s.summary.Successes++
	} else {
		s.summary.Failures++
	}
	s.summary.PromptTokens += int64(record.Usage.PromptTokens)
	s.summary.CachedPromptTokens += int64(record.Usage.CachedPromptTokens)
	s.summary.CompletionTokens += int64(record.Usage.CompletionTokens)
	s.summary.TotalTokens += int64(record.Usage.TotalTokens)
	s.recentRequests = prependRequestRecord(s.recentRequests, record, recentRequestLimit)
	s.dataVersion++
	s.mu.Unlock()
	s.enqueueWrite(telemetryWrite{request: &record})
}

func (s *Store) RecordError(record ErrorRecord) {
	if s == nil || s.db == nil {
		return
	}

	s.mu.Lock()
	s.recentErrors = prependErrorRecord(s.recentErrors, record, recentErrorLimit)
	s.dataVersion++
	s.mu.Unlock()
	s.enqueueWrite(telemetryWrite{errRec: &record})
}

func (s *Store) Snapshot() Snapshot {
	if s == nil || s.db == nil {
		return Snapshot{GeneratedAt: time.Now()}
	}

	version := s.currentVersion()
	now := time.Now()

	s.cacheMu.Lock()
	if now.Before(s.snapshotCache.expires) && s.snapshotCache.version == version {
		snapshot := s.snapshotCache.value
		s.cacheMu.Unlock()
		return snapshot
	}
	s.cacheMu.Unlock()

	s.flushWriter()
	snapshot := Snapshot{
		Summary:         s.cachedSummary(),
		Performance:     s.queryPerformance(),
		CacheTrends:     s.queryCacheTrends(),
		CacheHitRanking: s.queryCacheHitRanking(24*time.Hour, 10),
		Requests:        s.cachedRequests(),
		Errors:          s.cachedErrors(),
		ByModel:         s.queryUsageBreakdown("model"),
		ByUpstream:      s.queryUsageBreakdown("upstream"),
		ByModelRoute:    s.queryModelRouteBreakdown(),
		GeneratedAt:     now,
	}

	s.cacheMu.Lock()
	s.snapshotCache = cachedSnapshot{
		version: version,
		expires: now.Add(snapshotCacheTTL),
		value:   snapshot,
	}
	s.cacheMu.Unlock()

	return snapshot
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
	if err := rows.Err(); err != nil {
		return result
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
	if err := rows.Err(); err != nil {
		return records
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
	if err := rows.Err(); err != nil {
		return records
	}
	return records
}

func (s *Store) queryUsageBreakdown(column string) map[string]Usage {
	if !validSQLIdentifier(column) {
		return nil
	}
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
	if err := rows.Err(); err != nil {
		return result
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
	if err := rows.Err(); err != nil {
		return result
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

	version := s.currentVersion()
	now := time.Now()
	cacheKey := strconv.Itoa(hours) + ":" + strconv.Itoa(bucketMinutes)
	s.cacheMu.Lock()
	if cached, ok := s.timeSeriesCache[cacheKey]; ok && now.Before(cached.expires) && cached.version == version {
		s.cacheMu.Unlock()
		return cached.value
	}
	s.cacheMu.Unlock()

	s.flushWriter()
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return TimeSeries{}
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
	if err := rows2.Err(); err != nil {
		rows2.Close()
		return TimeSeries{Buckets: buckets}
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

	result := TimeSeries{Buckets: buckets, ByUpstream: byUpstream}
	s.cacheMu.Lock()
	s.timeSeriesCache[cacheKey] = cachedTimeSeries{
		version: version,
		expires: now.Add(timeSeriesCacheTTL),
		value:   result,
	}
	s.cacheMu.Unlock()
	return result
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
	// 严格验证 SQL 标识符，防止 SQL 注入
	if !validSQLIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if !validSQLIdentifier(column) {
		return fmt.Errorf("invalid column name: %q", column)
	}
	// 验证列类型（只允许白名单中的类型）
	if !validColumnType(columnType) {
		return fmt.Errorf("invalid column type: %q", columnType)
	}

	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		return fmt.Errorf("inspect telemetry table %q: %w", table, err)
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
			return fmt.Errorf("scan telemetry table info %q: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate telemetry table info %q: %w", table, err)
	}

	// 使用 %q 而不是 %s 来确保标识符被正确转义
	if _, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %q ADD COLUMN %q %s`, table, column, columnType)); err != nil {
		return fmt.Errorf("add telemetry column %q.%q: %w", table, column, err)
	}
	return nil
}

func (s *Store) startWriter() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed || s.writerCh != nil {
		return
	}
	s.writerCh = make(chan telemetryWriterMessage, writeQueueSize)
	s.writerDone = make(chan struct{})
	go s.runWriter(s.writerCh, s.writerDone)
}

func (s *Store) stopWriter() {
	s.writeMu.Lock()
	if s.closed {
		s.writeMu.Unlock()
		return
	}
	s.closed = true
	writerCh := s.writerCh
	writerDone := s.writerDone
	s.writerCh = nil
	s.writerDone = nil
	s.writeMu.Unlock()

	if writerCh != nil {
		close(writerCh)
	}
	if writerDone != nil {
		<-writerDone
	}
}

func (s *Store) enqueueWrite(write telemetryWrite) {
	s.writeMu.RLock()
	if s.closed || s.writerCh == nil {
		s.writeMu.RUnlock()
		s.persistBatch([]telemetryWrite{write})
		return
	}
	writerCh := s.writerCh
	select {
	case writerCh <- telemetryWriterMessage{write: &write}:
		s.writeMu.RUnlock()
		return
	default:
		s.writeMu.RUnlock()
		s.persistBatch([]telemetryWrite{write})
		return
	}
}

func (s *Store) flushWriter() {
	s.writeMu.RLock()
	if s.closed || s.writerCh == nil {
		s.writeMu.RUnlock()
		return
	}
	ack := make(chan struct{})
	select {
	case s.writerCh <- telemetryWriterMessage{flush: ack}:
		s.writeMu.RUnlock()
		<-ack
		return
	case <-time.After(5 * time.Second):
		s.writeMu.RUnlock()
		close(ack)
		return
	}
}

func (s *Store) runWriter(writerCh <-chan telemetryWriterMessage, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(writeFlushInterval)
	defer ticker.Stop()

	batch := make([]telemetryWrite, 0, writeBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.persistBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case msg, ok := <-writerCh:
			if !ok {
				flush()
				return
			}
			if msg.flush != nil {
				flush()
				close(msg.flush)
				continue
			}
			if msg.write == nil {
				continue
			}
			batch = append(batch, *msg.write)
			if len(batch) >= writeBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Store) persistBatch(batch []telemetryWrite) {
	if len(batch) == 0 || s == nil || s.db == nil {
		return
	}

	s.dbWriteMu.Lock()
	defer s.dbWriteMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return
	}

	requestStmt := tx.Stmt(s.insertRequestStmt)
	errorStmt := tx.Stmt(s.insertErrorStmt)
	commit := false
	defer func() {
		_ = requestStmt.Close()
		_ = errorStmt.Close()
		if commit {
			return
		}
		_ = tx.Rollback()
	}()

	for _, item := range batch {
		switch {
		case item.request != nil:
			if err := execRequestWrite(requestStmt, *item.request); err != nil {
				return
			}
		case item.errRec != nil:
			if err := execErrorWrite(errorStmt, *item.errRec); err != nil {
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return
	}
	commit = true
}

func execRequestWrite(stmt *sql.Stmt, record RequestRecord) error {
	_, err := stmt.Exec(
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
	return err
}

func execErrorWrite(stmt *sql.Stmt, record ErrorRecord) error {
	_, err := stmt.Exec(
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
	return err
}

func (s *Store) prepareStatements() error {
	requestStmt, err := s.db.Prepare(insertRequestSQL)
	if err != nil {
		return fmt.Errorf("prepare telemetry request insert: %w", err)
	}

	errorStmt, err := s.db.Prepare(insertErrorSQL)
	if err != nil {
		_ = requestStmt.Close()
		return fmt.Errorf("prepare telemetry error insert: %w", err)
	}

	s.insertRequestStmt = requestStmt
	s.insertErrorStmt = errorStmt
	return nil
}

func (s *Store) hydrateCaches() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.summary = s.querySummary()
	s.recentRequests = s.queryRequests(recentRequestLimit)
	s.recentErrors = s.queryErrors(recentErrorLimit)
}

func (s *Store) cachedSummary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.summary
}

func (s *Store) cachedRequests() []RequestRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RequestRecord(nil), s.recentRequests...)
}

func (s *Store) cachedErrors() []ErrorRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]ErrorRecord(nil), s.recentErrors...)
}

func (s *Store) currentVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dataVersion
}

func prependRequestRecord(records []RequestRecord, record RequestRecord, limit int) []RequestRecord {
	if limit <= 0 {
		return nil
	}
	if len(records) >= limit {
		records = records[:limit-1]
	}
	records = append(records, RequestRecord{})
	copy(records[1:], records[:len(records)-1])
	records[0] = record
	return records
}

func prependErrorRecord(records []ErrorRecord, record ErrorRecord, limit int) []ErrorRecord {
	if limit <= 0 {
		return nil
	}
	if len(records) >= limit {
		records = records[:limit-1]
	}
	records = append(records, ErrorRecord{})
	copy(records[1:], records[:len(records)-1])
	records[0] = record
	return records
}
