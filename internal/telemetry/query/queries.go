package query

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/telemetry"
)

const (
	defaultWindowHours       = 24
	defaultTelemetryLimit    = 100
	maxTelemetryLimit        = 500
	defaultBucketMinutes     = 5
	maxBucketMinutes         = 7 * 24 * 60
	defaultOverviewWindow    = 5 * time.Minute
	projectedModelExpression = "COALESCE(NULLIF(effective_model, ''), NULLIF(requested_model, ''), '')"
)

// QueryWindowMetrics returns aggregated metrics for a time window using agg_buckets.
func (s *Store) QueryWindowMetrics(window time.Duration) (telemetryquery.WindowMetrics, error) {
	if window == 0 {
		window = defaultOverviewWindow
	}

	queryText := `
SELECT
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_buckets`
	args := make([]interface{}, 0, 1)
	if window > 0 {
		queryText += "\nWHERE bucket >= ?"
		args = append(args, time.Now().Add(-window).UTC().Format(time.RFC3339Nano))
	}

	var metrics telemetryquery.WindowMetrics
	if err := s.db.QueryRow(queryText, args...).Scan(
		&metrics.Requests,
		&metrics.Successes,
		&metrics.Failures,
		&metrics.InputTokens,
		&metrics.CachedPromptTokens,
		&metrics.OutputTokens,
		&metrics.AvgLatencyMs,
	); err != nil {
		if err == sql.ErrNoRows {
			return telemetryquery.WindowMetrics{}, nil
		}
		return telemetryquery.WindowMetrics{}, err
	}

	return metrics, nil
}

// ListAvailableModels returns observed models based on projected request facts.
func (s *Store) ListAvailableModels() ([]string, error) {
	rows, err := s.db.Query(`
SELECT DISTINCT model
FROM (
  SELECT ` + projectedModelExpression + ` AS model
  FROM request_facts
)
WHERE model != ''
ORDER BY model ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := make([]string, 0, 32)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return models, nil
}

// QueryTelemetry returns recent request facts with pagination and filters applied.
func (s *Store) QueryTelemetry(req telemetryquery.TelemetryRequest) ([]telemetryquery.TelemetryEvent, int64, int, error) {
	windowHours := normalizeWindowHours(req.WindowHours)
	limit := normalizeLimit(req.Limit, defaultTelemetryLimit, maxTelemetryLimit)
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"timestamp >= ?"}
	args := []interface{}{time.Now().Add(-time.Duration(windowHours) * time.Hour).UTC().Format(time.RFC3339Nano)}

	if models := cleanStrings(req.Filters.Models); len(models) > 0 {
		modelPlaceholders := placeholders(len(models))
		where = append(where, fmt.Sprintf("(%s IN (%s) OR requested_model IN (%s))", projectedModelExpression, modelPlaceholders, modelPlaceholders))
		args = append(args, stringArgs(models)...)
		args = append(args, stringArgs(models)...)
	}

	if providers := cleanStrings(req.Filters.Providers); len(providers) > 0 {
		where = append(where, "provider_id IN ("+placeholders(len(providers))+")")
		args = append(args, stringArgs(providers)...)
	}

	if statusCodes := cleanStatusCodes(req.Filters.StatusCodes); len(statusCodes) > 0 {
		where = append(where, "status_code IN ("+placeholders(len(statusCodes))+")")
		args = append(args, intArgs(statusCodes)...)
	}

	if req.Filters.ErrorsOnly {
		where = append(where, "(status_code >= 400 OR COALESCE(error_message, '') != '')")
	}
	if req.Filters.MinLatencyMs > 0 {
		where = append(where, "latency_ms >= ?")
		args = append(args, req.Filters.MinLatencyMs)
	}
	if req.Filters.MaxLatencyMs > 0 {
		where = append(where, "latency_ms <= ?")
		args = append(args, req.Filters.MaxLatencyMs)
	}

	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM request_facts WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, windowHours, err
	}

	rows, err := s.db.Query(`
SELECT
  event_id,
  timestamp,
  request_id,
  path,
  requested_model,
  `+projectedModelExpression+` AS effective_model,
  provider_id,
  route_mode,
  status_code,
  latency_ms,
  attempts,
  prompt_tokens,
  cached_prompt_tokens,
  completion_tokens,
  stream,
  COALESCE(error_message, '')
FROM request_facts
WHERE `+whereSQL+`
ORDER BY timestamp DESC, id DESC
LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, windowHours, err
	}
	defer rows.Close()

	events := make([]telemetryquery.TelemetryEvent, 0, limit)
	for rows.Next() {
		var (
			ts        string
			streamInt int
			event     telemetryquery.TelemetryEvent
		)
		if err := rows.Scan(
			&event.EventID,
			&ts,
			&event.RequestID,
			&event.Path,
			&event.RequestedModel,
			&event.EffectiveModel,
			&event.Provider,
			&event.RouteMode,
			&event.StatusCode,
			&event.LatencyMs,
			&event.Attempts,
			&event.InputTokens,
			&event.CachedPromptTokens,
			&event.OutputTokens,
			&streamInt,
			&event.Error,
		); err != nil {
			return nil, 0, windowHours, err
		}
		event.Timestamp = parseStoredTimestamp(ts)
		event.Stream = streamInt == 1
		if event.Attempts < 0 {
			event.Attempts = 0
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, windowHours, err
	}

	return events, total, windowHours, nil
}

// QueryTimeSeries returns time-bucketed metrics derived from agg_buckets.
func (s *Store) QueryTimeSeries(req telemetryquery.TimeSeriesRequest) ([]telemetryquery.TimeBucket, int, int, error) {
	windowHours := normalizeWindowHours(req.WindowHours)
	bucketMinutes := normalizeLimit(req.BucketMinutes, defaultBucketMinutes, maxBucketMinutes)
	groupBy := strings.ToLower(strings.TrimSpace(req.GroupBy))

	var (
		queryText string
		args      = []interface{}{
			bucketMinutes * 60,
			bucketMinutes * 60,
			time.Now().Add(-time.Duration(windowHours) * time.Hour).UTC().Format(time.RFC3339Nano),
		}
	)

	switch groupBy {
	case "":
		queryText = `
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
FROM agg_buckets
WHERE bucket >= ?
GROUP BY grouped_bucket
ORDER BY grouped_bucket ASC`
	case "model", "provider":
		queryText = `
SELECT
  strftime('%Y-%m-%dT%H:%M:%SZ',
           datetime((CAST(strftime('%s', bucket) AS INTEGER) / ?) * ?, 'unixepoch')) AS grouped_bucket,
  ` + groupBy + `,
  COALESCE(SUM(requests), 0),
  COALESCE(SUM(successes), 0),
  COALESCE(SUM(failures), 0),
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  CASE WHEN SUM(requests) > 0 THEN CAST(SUM(total_latency) AS REAL) / SUM(requests) ELSE 0 END
FROM agg_buckets
WHERE bucket >= ?
GROUP BY grouped_bucket, ` + groupBy + `
ORDER BY grouped_bucket ASC, ` + groupBy + ` ASC`
	default:
		return nil, windowHours, bucketMinutes, fmt.Errorf("unsupported group_by %q", req.GroupBy)
	}

	rows, err := s.db.Query(queryText, args...)
	if err != nil {
		return nil, windowHours, bucketMinutes, err
	}
	defer rows.Close()

	buckets := make([]telemetryquery.TimeBucket, 0, 128)
	for rows.Next() {
		var bucket telemetryquery.TimeBucket
		if groupBy == "" {
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
				return nil, windowHours, bucketMinutes, err
			}
		} else {
			if err := rows.Scan(
				&bucket.Bucket,
				&bucket.GroupValue,
				&bucket.Requests,
				&bucket.Successes,
				&bucket.Failures,
				&bucket.InputTokens,
				&bucket.CachedPromptTokens,
				&bucket.OutputTokens,
				&bucket.AvgLatencyMs,
			); err != nil {
				return nil, windowHours, bucketMinutes, err
			}
		}
		buckets = append(buckets, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, windowHours, bucketMinutes, err
	}

	return buckets, windowHours, bucketMinutes, nil
}

// QueryModelBenchmark returns model benchmark metrics based on request_facts.
func (s *Store) QueryModelBenchmark(req telemetryquery.BenchmarkRequest) ([]telemetryquery.ModelBenchmark, int, error) {
	start, end, windowHours := resolveBenchmarkRange(req)
	models := cleanStrings(req.Models)

	where := []string{
		"timestamp >= ?",
		"timestamp <= ?",
		projectedModelExpression + " != ''",
	}
	args := []interface{}{
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	}
	if len(models) > 0 {
		where = append(where, projectedModelExpression+" IN ("+placeholders(len(models))+")")
		args = append(args, stringArgs(models)...)
	}

	rows, err := s.db.Query(`
SELECT
  `+projectedModelExpression+` AS model,
  COUNT(*),
  COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status_code < 200 OR status_code >= 400 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  CASE WHEN COUNT(*) > 0 THEN CAST(SUM(latency_ms) AS REAL) / COUNT(*) ELSE 0 END,
  COALESCE(MAX(latency_ms), 0)
FROM request_facts
WHERE `+strings.Join(where, " AND ")+`
GROUP BY model
ORDER BY COUNT(*) DESC, model ASC`, args...)
	if err != nil {
		return nil, windowHours, err
	}
	defer rows.Close()

	benchmarks := make([]telemetryquery.ModelBenchmark, 0, 32)
	for rows.Next() {
		var benchmark telemetryquery.ModelBenchmark
		if err := rows.Scan(
			&benchmark.Model,
			&benchmark.Requests,
			&benchmark.Successes,
			&benchmark.Failures,
			&benchmark.InputTokens,
			&benchmark.CachedPromptTokens,
			&benchmark.OutputTokens,
			&benchmark.AvgLatencyMs,
			&benchmark.MaxLatencyMs,
		); err != nil {
			return nil, windowHours, err
		}
		if benchmark.Requests > 0 {
			benchmark.SuccessRate = float64(benchmark.Successes) / float64(benchmark.Requests) * 100
		}
		s.populateLatencyPercentiles(&benchmark, start, end)
		benchmarks = append(benchmarks, benchmark)
	}
	if err := rows.Err(); err != nil {
		return nil, windowHours, err
	}

	costs, err := s.queryBenchmarkCosts(start, end, models)
	if err != nil {
		return nil, windowHours, err
	}
	for i := range benchmarks {
		benchmarks[i].EstimatedCostUSD = costs[benchmarks[i].Model]
	}

	return benchmarks, windowHours, nil
}

func (s *Store) populateLatencyPercentiles(benchmark *telemetryquery.ModelBenchmark, start, end time.Time) {
	var (
		count      int64
		maxLatency sql.NullInt64
	)
	err := s.db.QueryRow(`
SELECT COUNT(*), MAX(latency_ms)
FROM request_facts
WHERE timestamp >= ?
  AND timestamp <= ?
  AND `+projectedModelExpression+` = ?
  AND latency_ms > 0`,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
		benchmark.Model,
	).Scan(&count, &maxLatency)
	if err != nil || count == 0 {
		return
	}
	if maxLatency.Valid {
		benchmark.MaxLatencyMs = maxLatency.Int64
	}

	percentileAt := func(offset int64) (float64, error) {
		var value int64
		err := s.db.QueryRow(`
SELECT latency_ms
FROM request_facts
WHERE timestamp >= ?
  AND timestamp <= ?
  AND `+projectedModelExpression+` = ?
  AND latency_ms > 0
ORDER BY latency_ms ASC
LIMIT 1 OFFSET ?`,
			start.UTC().Format(time.RFC3339Nano),
			end.UTC().Format(time.RFC3339Nano),
			benchmark.Model,
			offset,
		).Scan(&value)
		return float64(value), err
	}

	if count%2 == 0 {
		lo, errLo := percentileAt(count/2 - 1)
		hi, errHi := percentileAt(count / 2)
		if errLo == nil && errHi == nil {
			benchmark.P50LatencyMs = (lo + hi) / 2
		}
	} else if value, err := percentileAt(count / 2); err == nil {
		benchmark.P50LatencyMs = value
	}

	p95Offset := int64(float64(count) * 0.95)
	if p95Offset >= count {
		p95Offset = count - 1
	}
	if value, err := percentileAt(p95Offset); err == nil {
		benchmark.P95LatencyMs = value
	}

	p99Offset := int64(float64(count) * 0.99)
	if p99Offset >= count {
		p99Offset = count - 1
	}
	if value, err := percentileAt(p99Offset); err == nil {
		benchmark.P99LatencyMs = value
	}
}

func (s *Store) queryBenchmarkCosts(start, end time.Time, models []string) (map[string]float64, error) {
	where := []string{
		"timestamp >= ?",
		"timestamp <= ?",
		projectedModelExpression + " != ''",
	}
	args := []interface{}{
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	}
	if len(models) > 0 {
		where = append(where, projectedModelExpression+" IN ("+placeholders(len(models))+")")
		args = append(args, stringArgs(models)...)
	}

	rows, err := s.db.Query(`
SELECT
  `+projectedModelExpression+` AS model,
  COALESCE(provider_id, ''),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0)
FROM request_facts
WHERE `+strings.Join(where, " AND ")+`
GROUP BY model, provider_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := make([]telemetry.ModelRouteUsage, 0, 32)
	for rows.Next() {
		var (
			model              string
			provider           string
			inputTokens        int64
			cachedPromptTokens int64
			outputTokens       int64
		)
		if err := rows.Scan(&model, &provider, &inputTokens, &cachedPromptTokens, &outputTokens); err != nil {
			return nil, err
		}
		routes = append(routes, telemetry.ModelRouteUsage{
			Model:    model,
			Upstream: provider,
			Usage: telemetry.Usage{
				PromptTokens:       int(inputTokens),
				CachedPromptTokens: int(cachedPromptTokens),
				CompletionTokens:   int(outputTokens),
				TotalTokens:        int(inputTokens + outputTokens),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return map[string]float64{}, nil
	}

	pricing := telemetry.BuildPricingSnapshot(
		telemetry.Snapshot{ByModelRoute: routes},
		telemetry.BootstrapPricingSnapshot(),
	)
	costs := make(map[string]float64, len(pricing.Models))
	for _, item := range pricing.Models {
		model := strings.TrimSpace(item.EffectiveModel)
		if model == "" {
			model = strings.TrimSpace(item.DisplayModel)
		}
		if model == "" {
			continue
		}
		costs[model] += item.Cost.TotalUsd
	}

	return costs, nil
}

func resolveBenchmarkRange(req telemetryquery.BenchmarkRequest) (time.Time, time.Time, int) {
	windowHours := normalizeWindowHours(req.WindowHours)
	end := time.Now().UTC()
	start := end.Add(-time.Duration(windowHours) * time.Hour)

	if req.StartTime != nil && req.EndTime != nil {
		startCandidate := req.StartTime.UTC()
		endCandidate := req.EndTime.UTC()
		if endCandidate.After(startCandidate) {
			start = startCandidate
			end = endCandidate
			windowHours = int((end.Sub(start) + time.Hour - time.Nanosecond) / time.Hour)
			if windowHours <= 0 {
				windowHours = 1
			}
		}
	}

	return start, end, windowHours
}

func normalizeWindowHours(hours int) int {
	return normalizeLimit(hours, defaultWindowHours, 365*24)
}

func normalizeLimit(value int, fallback int, max int) int {
	if value <= 0 {
		value = fallback
	}
	if max > 0 && value > max {
		value = max
	}
	return value
}

func cleanStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cleanStatusCodes(values []int) []int {
	if len(values) == 0 {
		return nil
	}

	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func stringArgs(values []string) []interface{} {
	args := make([]interface{}, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func intArgs(values []int) []interface{} {
	args := make([]interface{}, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func parseStoredTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}
