package query

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/telemetryquery"
	telemetrycore "ai-model-gateway/internal/telemetry"
)

const (
	defaultWindowHours               = 24
	defaultTelemetryLimit            = 100
	maxTelemetryLimit                = 500
	defaultBucketMinutes             = 5
	maxBucketMinutes                 = 7 * 24 * 60
	defaultOverviewWindow            = 5 * time.Minute
	projectedModelExpression         = "COALESCE(NULLIF(effective_model, ''), NULLIF(requested_model, ''), '')"
	projectedPricingModelExpression  = "COALESCE(NULLIF(requested_model, ''), NULLIF(effective_model, ''), '')"
	nonSyntheticFilterExpression     = "COALESCE(synthetic_kind, '') = ''"
	projectedPricingStatusExpression = `CASE
		WHEN COALESCE(NULLIF(pricing_status, ''), '') != '' THEN pricing_status
		WHEN prompt_tokens > 0 OR cached_prompt_tokens > 0 OR completion_tokens > 0 THEN 'estimated_legacy'
		ELSE 'unpriced'
	END`
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
  WHERE ` + nonSyntheticFilterExpression + `
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

	where, args := telemetryWhere(req, windowHours, time.Now())
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
  COALESCE(pricing_status, ''),
  COALESCE(pricing_total_cost_usd, 0),
  COALESCE(synthetic_kind, ''),
  COALESCE(benchmark_run_id, ''),
  COALESCE(benchmark_target_id, ''),
  COALESCE(benchmark_case_id, ''),
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
			&event.PricingStatus,
			&event.PricingTotalCostUSD,
			&event.SyntheticKind,
			&event.BenchmarkRunID,
			&event.BenchmarkTargetID,
			&event.BenchmarkCaseID,
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

// QueryTelemetryDistributions returns full-window model and upstream distributions.
func (s *Store) QueryTelemetryDistributions(req telemetryquery.TelemetryRequest) ([]telemetryquery.TelemetryDistributionItem, []telemetryquery.TelemetryDistributionItem, int, error) {
	windowHours := normalizeWindowHours(req.WindowHours)
	where, args := telemetryWhere(req, windowHours, time.Now())

	models, err := s.queryTelemetryDistribution(
		`COALESCE(NULLIF(`+projectedModelExpression+`, ''), 'unknown')`,
		where,
		args,
	)
	if err != nil {
		return nil, nil, windowHours, err
	}

	upstreams, err := s.queryTelemetryDistribution(
		`COALESCE(NULLIF(provider_id, ''), 'unknown')`,
		where,
		args,
	)
	if err != nil {
		return nil, nil, windowHours, err
	}

	return models, upstreams, windowHours, nil
}

func (s *Store) queryTelemetryDistribution(groupExpr string, where []string, args []interface{}) ([]telemetryquery.TelemetryDistributionItem, error) {
	whereSQL := strings.Join(where, " AND ")
	rows, err := s.db.Query(`
SELECT
  `+groupExpr+` AS value,
  COUNT(*),
  COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 400 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status_code < 200 OR status_code >= 400 THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  CASE WHEN COUNT(*) > 0 THEN CAST(SUM(latency_ms) AS REAL) / COUNT(*) ELSE 0 END
FROM request_facts
WHERE `+whereSQL+`
GROUP BY value
ORDER BY COUNT(*) DESC, value ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]telemetryquery.TelemetryDistributionItem, 0, 32)
	for rows.Next() {
		var item telemetryquery.TelemetryDistributionItem
		if err := rows.Scan(
			&item.Value,
			&item.Requests,
			&item.Successes,
			&item.Failures,
			&item.InputTokens,
			&item.CachedPromptTokens,
			&item.OutputTokens,
			&item.AvgLatencyMs,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func telemetryWhere(req telemetryquery.TelemetryRequest, windowHours int, now time.Time) ([]string, []interface{}) {
	where := []string{"timestamp >= ?"}
	args := []interface{}{now.Add(-time.Duration(windowHours) * time.Hour).UTC().Format(time.RFC3339Nano)}
	where, args = appendSyntheticTelemetryFilters(where, args, req.Filters)

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

	return where, args
}

func appendSyntheticTelemetryFilters(where []string, args []interface{}, filters telemetryquery.TelemetryFilters) ([]string, []interface{}) {
	syntheticKind := strings.TrimSpace(filters.SyntheticKind)
	benchmarkRunID := strings.TrimSpace(filters.BenchmarkRunID)
	benchmarkTargetID := strings.TrimSpace(filters.BenchmarkTargetID)
	benchmarkCaseID := strings.TrimSpace(filters.BenchmarkCaseID)

	switch {
	case syntheticKind != "":
		where = append(where, "COALESCE(synthetic_kind, '') = ?")
		args = append(args, syntheticKind)
	case benchmarkRunID != "" || benchmarkTargetID != "" || benchmarkCaseID != "":
		where = append(where, "COALESCE(synthetic_kind, '') != ''")
	default:
		where = append(where, nonSyntheticFilterExpression)
	}

	if benchmarkRunID != "" {
		where = append(where, "COALESCE(benchmark_run_id, '') = ?")
		args = append(args, benchmarkRunID)
	}
	if benchmarkTargetID != "" {
		where = append(where, "COALESCE(benchmark_target_id, '') = ?")
		args = append(args, benchmarkTargetID)
	}
	if benchmarkCaseID != "" {
		where = append(where, "COALESCE(benchmark_case_id, '') = ?")
		args = append(args, benchmarkCaseID)
	}
	return where, args
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
	group := normalizeBenchmarkGroup(req.Group)
	groupExpr := projectedModelExpression
	groupNotEmpty := projectedModelExpression + " != ''"
	if group == "upstream" {
		groupExpr = "COALESCE(provider_id, '')"
		groupNotEmpty = "COALESCE(provider_id, '') != ''"
	}

	where := []string{
		"timestamp >= ?",
		"timestamp <= ?",
		groupNotEmpty,
		nonSyntheticFilterExpression,
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
  `+groupExpr+` AS benchmark_group,
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
GROUP BY benchmark_group
ORDER BY COUNT(*) DESC, benchmark_group ASC`, args...)
	if err != nil {
		return nil, windowHours, err
	}
	defer rows.Close()

	benchmarks := make([]telemetryquery.ModelBenchmark, 0, 32)
	for rows.Next() {
		var benchmark telemetryquery.ModelBenchmark
		var label string
		if err := rows.Scan(
			&label,
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
		benchmark.Label = label
		if group == "upstream" {
			benchmark.Upstream = label
			benchmark.Model = label
		} else {
			benchmark.Model = label
		}
		s.populateLatencyPercentiles(&benchmark, start, end, group)
		benchmarks = append(benchmarks, benchmark)
	}
	if err := rows.Err(); err != nil {
		return nil, windowHours, err
	}

	costs, err := s.queryBenchmarkCostBreakdown(start, end, models, group)
	if err != nil {
		return nil, windowHours, err
	}
	for i := range benchmarks {
		key := benchmarks[i].Model
		if group == "upstream" {
			key = benchmarks[i].Upstream
		}
		benchmarks[i].ExactCostUSD = costs[key][0]
		benchmarks[i].EstimatedLegacyCostUSD = costs[key][1]
		benchmarks[i].EstimatedCostUSD = benchmarks[i].ExactCostUSD + benchmarks[i].EstimatedLegacyCostUSD
	}

	return benchmarks, windowHours, nil
}

func (s *Store) populateLatencyPercentiles(benchmark *telemetryquery.ModelBenchmark, start, end time.Time, group string) {
	var (
		count      int64
		maxLatency sql.NullInt64
	)
	groupExpr := projectedModelExpression
	groupValue := benchmark.Model
	if group == "upstream" {
		groupExpr = "COALESCE(provider_id, '')"
		groupValue = benchmark.Upstream
	}
	err := s.db.QueryRow(`
SELECT COUNT(*), MAX(latency_ms)
FROM request_facts
WHERE timestamp >= ?
  AND timestamp <= ?
  AND `+groupExpr+` = ?
  AND `+nonSyntheticFilterExpression+`
  AND latency_ms > 0`,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
		groupValue,
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
  AND `+groupExpr+` = ?
  AND `+nonSyntheticFilterExpression+`
  AND latency_ms > 0
ORDER BY latency_ms ASC
LIMIT 1 OFFSET ?`,
			start.UTC().Format(time.RFC3339Nano),
			end.UTC().Format(time.RFC3339Nano),
			groupValue,
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

func normalizeBenchmarkGroup(group string) string {
	switch strings.ToLower(strings.TrimSpace(group)) {
	case "upstream", "provider", "providers":
		return "upstream"
	default:
		return "model"
	}
}

func (s *Store) queryBenchmarkCosts(start, end time.Time, models []string) (map[string]float64, error) {
	where := []string{
		"timestamp >= ?",
		"timestamp <= ?",
		projectedModelExpression + " != ''",
		nonSyntheticFilterExpression,
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
  COALESCE(SUM(CASE WHEN `+projectedPricingStatusExpression+` = '`+PricingStatusFixed+`' THEN pricing_total_cost_usd ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN `+projectedPricingStatusExpression+` = '`+PricingStatusEstimatedLegacy+`' THEN pricing_total_cost_usd ELSE 0 END), 0)
FROM request_facts
WHERE `+strings.Join(where, " AND ")+`
GROUP BY model`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	costs := make(map[string]float64, 32)
	for rows.Next() {
		var (
			model            string
			exactCostUSD     float64
			estimatedCostUSD float64
		)
		if err := rows.Scan(&model, &exactCostUSD, &estimatedCostUSD); err != nil {
			return nil, err
		}
		costs[model] = exactCostUSD + estimatedCostUSD
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return costs, nil
}

func (s *Store) queryBenchmarkCostBreakdown(start, end time.Time, models []string, group string) (map[string][2]float64, error) {
	groupExpr := projectedPricingModelExpression
	groupNotEmpty := projectedModelExpression + " != ''"
	if group == "upstream" {
		groupExpr = "COALESCE(provider_id, '')"
		groupNotEmpty = "COALESCE(provider_id, '') != ''"
	}
	where := []string{
		"timestamp >= ?",
		"timestamp <= ?",
		groupNotEmpty,
		nonSyntheticFilterExpression,
	}
	args := []interface{}{
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	}
	if len(models) > 0 {
		filterExpr := projectedPricingModelExpression
		if group == "upstream" {
			filterExpr = projectedModelExpression
		}
		where = append(where, filterExpr+" IN ("+placeholders(len(models))+")")
		args = append(args, stringArgs(models)...)
	}

	rows, err := s.db.Query(`
SELECT
  `+groupExpr+` AS benchmark_group,
  COALESCE(provider_id, ''),
  COALESCE(requested_model, ''),
  COALESCE(effective_model, ''),
  `+projectedPricingStatusExpression+` AS pricing_status,
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(pricing_total_cost_usd), 0)
FROM request_facts
WHERE `+strings.Join(where, " AND ")+`
GROUP BY benchmark_group, provider_id, requested_model, effective_model, pricing_status`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][2]float64, 32)
	for rows.Next() {
		var (
			key                string
			provider           string
			requestedModel     string
			effectiveModel     string
			pricingStatus      string
			inputTokens        int64
			cachedPromptTokens int64
			outputTokens       int64
			totalCostUSD       float64
		)
		if err := rows.Scan(&key, &provider, &requestedModel, &effectiveModel, &pricingStatus, &inputTokens, &cachedPromptTokens, &outputTokens, &totalCostUSD); err != nil {
			return nil, err
		}
		current := result[key]
		switch normalizePricingStatus(pricingStatus, inputTokens, cachedPromptTokens, outputTokens) {
		case PricingStatusFixed:
			current[0] += totalCostUSD
		case PricingStatusEstimatedLegacy:
			if totalCostUSD == 0 {
				_, _, _, _, _, totalCostUSD, _ = estimateLegacyPricing(requestedModel, effectiveModel, provider, inputTokens, cachedPromptTokens, outputTokens)
			}
			current[1] += totalCostUSD
		}
		result[key] = current
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
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

// QueryPricingEconomics returns pricing economics for the specified time window.
func (s *Store) QueryPricingEconomics(windowHours int) telemetryquery.PricingEconomics {
	if windowHours <= 0 {
		windowHours = defaultWindowHours
	}

	rows, err := s.db.Query(`
SELECT
  `+projectedPricingModelExpression+` AS model,
  COALESCE(provider_id, ''),
  COALESCE(requested_model, ''),
  COALESCE(effective_model, ''),
  `+projectedPricingStatusExpression+` AS pricing_status,
  COALESCE(pricing_source_id, ''),
  COALESCE(pricing_currency, ''),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0),
  COALESCE(SUM(pricing_prompt_cost), 0),
  COALESCE(SUM(pricing_completion_cost), 0),
  COALESCE(SUM(pricing_total_cost), 0),
  COALESCE(SUM(pricing_prompt_cost_usd), 0),
  COALESCE(SUM(pricing_completion_cost_usd), 0),
  COALESCE(SUM(pricing_total_cost_usd), 0),
  COALESCE(MAX(pricing_input_per_1m), 0),
  COALESCE(MAX(pricing_cached_input_per_1m), 0)
FROM request_facts
WHERE timestamp >= ?
  AND `+projectedPricingModelExpression+` != ''
  AND `+nonSyntheticFilterExpression+`
GROUP BY model, provider_id, requested_model, effective_model, pricing_status, pricing_source_id, pricing_currency`,
		time.Now().Add(-time.Duration(windowHours)*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return telemetryquery.PricingEconomics{}
	}
	defer rows.Close()

	models := make([]telemetryquery.PricingModelSummary, 0, 32)
	currencyTotals := make(map[string]*telemetryquery.PricingCurrencySummary)
	currencyModels := make(map[string]map[string]struct{})
	exactModels := make(map[string]struct{})
	estimatedModels := make(map[string]struct{})
	unpricedModels := make(map[string]struct{})
	pricedModels := make(map[string]struct{})
	var summary telemetryquery.PricingSummary
	summary.Currency = "USD"

	for rows.Next() {
		var (
			model              string
			provider           string
			requestedModel     string
			effectiveModel     string
			pricingStatus      string
			pricingSourceID    string
			pricingCurrency    string
			inputTokens        int64
			cachedPromptTokens int64
			outputTokens       int64
			promptCost         float64
			completionCost     float64
			totalCost          float64
			promptCostUSD      float64
			completionCostUSD  float64
			totalCostUSD       float64
			inputPer1M         float64
			cachedInputPer1M   float64
		)
		if err := rows.Scan(
			&model,
			&provider,
			&requestedModel,
			&effectiveModel,
			&pricingStatus,
			&pricingSourceID,
			&pricingCurrency,
			&inputTokens,
			&cachedPromptTokens,
			&outputTokens,
			&promptCost,
			&completionCost,
			&totalCost,
			&promptCostUSD,
			&completionCostUSD,
			&totalCostUSD,
			&inputPer1M,
			&cachedInputPer1M,
		); err != nil {
			continue
		}
		pricingStatus = normalizePricingStatus(pricingStatus, inputTokens, cachedPromptTokens, outputTokens)
		currency := strings.ToUpper(strings.TrimSpace(pricingCurrency))
		if currency == "" {
			currency = "USD"
		}
		modelKey := strings.TrimSpace(requestedModel)
		if modelKey == "" {
			modelKey = strings.TrimSpace(effectiveModel)
		}
		if modelKey == "" {
			modelKey = strings.TrimSpace(model)
		}
		if pricingStatus == PricingStatusEstimatedLegacy && totalCostUSD == 0 {
			var ok bool
			promptCost, completionCost, totalCost, promptCostUSD, completionCostUSD, totalCostUSD, ok = estimateLegacyPricing(requestedModel, effectiveModel, provider, inputTokens, cachedPromptTokens, outputTokens)
			if ok {
				pricingSourceID = "estimated_legacy_bootstrap"
				if promptCost > 0 || completionCost > 0 || totalCost > 0 {
					currency, ok = estimateLegacyCurrency(requestedModel, effectiveModel, provider)
					if !ok {
						currency = "USD"
					}
				}
			}
		}
		switch pricingStatus {
		case PricingStatusFixed:
			summary.ExactTotalUsd += totalCostUSD
			summary.ExactRequests++
			exactModels[modelKey] = struct{}{}
			pricedModels[modelKey] = struct{}{}
		case PricingStatusEstimatedLegacy:
			summary.EstimatedTotalUsd += totalCostUSD
			summary.EstimatedRequests++
			estimatedModels[modelKey] = struct{}{}
			pricedModels[modelKey] = struct{}{}
		default:
			unpricedModels[modelKey] = struct{}{}
		}
		summary.CachedPromptTokens += cachedPromptTokens
		cacheSavings := float64(cachedPromptTokens) * (inputPer1M - cachedInputPer1M) / 1_000_000
		cacheSavingsUSD := float64(cachedPromptTokens) * ((inputPer1M - cachedInputPer1M) * fxRateForSummary(totalCost, totalCostUSD)) / 1_000_000
		if pricingStatus == PricingStatusFixed {
			summary.CacheSavings += maxFloat(cacheSavings, 0)
			summary.CacheSavingsUsd += maxFloat(cacheSavingsUSD, 0)
			acc := currencyTotals[currency]
			if acc == nil {
				acc = &telemetryquery.PricingCurrencySummary{Currency: currency}
				currencyTotals[currency] = acc
			}
			if currencyModels[currency] == nil {
				currencyModels[currency] = make(map[string]struct{})
			}
			currencyModels[currency][modelKey] = struct{}{}
			acc.Prompt += promptCost
			acc.Completion += completionCost
			acc.Total += totalCost
			acc.CacheSavings += maxFloat(cacheSavings, 0)
		}

		models = append(models, telemetryquery.PricingModelSummary{
			DisplayModel:    modelKey,
			RequestedModel:  requestedModel,
			EffectiveModel:  effectiveModel,
			Upstream:        provider,
			PricingModel:    modelKey,
			PricingStatus:   pricingStatus,
			PricingSourceID: pricingSourceID,
			Usage: telemetryquery.PricingUsage{
				PromptTokens:       int(inputTokens),
				CachedPromptTokens: int(cachedPromptTokens),
				CompletionTokens:   int(outputTokens),
				TotalTokens:        int(inputTokens + outputTokens),
			},
			Cost: telemetryquery.PricingCost{
				Currency:      currency,
				Prompt:        promptCost,
				Completion:    completionCost,
				Total:         totalCost,
				PromptUsd:     promptCostUSD,
				CompletionUsd: completionCostUSD,
				TotalUsd:      totalCostUSD,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return telemetryquery.PricingEconomics{}
	}

	if len(models) == 0 {
		return telemetryquery.PricingEconomics{}
	}

	summary.ExactModels = len(exactModels)
	summary.EstimatedModels = len(estimatedModels)
	summary.UnpricedModels = len(unpricedModels)
	summary.PricedModels = len(pricedModels)
	summary.TotalUsd = summary.ExactTotalUsd + summary.EstimatedTotalUsd
	for _, model := range models {
		summary.PromptUsd += model.Cost.PromptUsd
		summary.CompletionUsd += model.Cost.CompletionUsd
	}
	summary.Total = summary.TotalUsd
	summary.Prompt = summary.PromptUsd
	summary.Completion = summary.CompletionUsd
	if len(currencyTotals) > 0 {
		summary.TotalsByCurrency = make([]telemetryquery.PricingCurrencySummary, 0, len(currencyTotals))
		for _, total := range currencyTotals {
			total.PricedModels = len(currencyModels[total.Currency])
			summary.TotalsByCurrency = append(summary.TotalsByCurrency, *total)
		}
		sortPricingCurrencySummaries(summary.TotalsByCurrency)
		summary.Currency = summary.TotalsByCurrency[0].Currency
		summary.Prompt = summary.TotalsByCurrency[0].Prompt
		summary.Completion = summary.TotalsByCurrency[0].Completion
		summary.Total = summary.TotalsByCurrency[0].Total
	}
	sortPricingModels(models)

	return telemetryquery.PricingEconomics{
		Summary: summary,
		Models:  models,
	}
}

func sortPricingModels(models []telemetryquery.PricingModelSummary) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost.TotalUsd == models[j].Cost.TotalUsd {
			return models[i].DisplayModel < models[j].DisplayModel
		}
		return models[i].Cost.TotalUsd > models[j].Cost.TotalUsd
	})
}

func sortPricingCurrencySummaries(items []telemetryquery.PricingCurrencySummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Total == items[j].Total {
			return items[i].Currency < items[j].Currency
		}
		return items[i].Total > items[j].Total
	})
}

func fxRateForSummary(totalNative, totalUSD float64) float64 {
	if totalNative <= 0 || totalUSD <= 0 {
		return 0
	}
	return totalUSD / totalNative
}

func maxFloat(value, fallback float64) float64 {
	if value < fallback {
		return fallback
	}
	return value
}

func estimateLegacyPricing(requestedModel, effectiveModel, provider string, promptTokens, cachedPromptTokens, completionTokens int64) (float64, float64, float64, float64, float64, float64, bool) {
	pricing, ok := resolveBootstrapPricing(requestedModel, effectiveModel, provider)
	if !ok {
		return 0, 0, 0, 0, 0, 0, false
	}
	usage := telemetrycore.Usage{
		PromptTokens:       int(promptTokens),
		CachedPromptTokens: int(cachedPromptTokens),
		CompletionTokens:   int(completionTokens),
		TotalTokens:        int(promptTokens + completionTokens),
	}
	prompt, completion, total := calculateLegacyNativeCost(usage, pricing)
	promptUSD, completionUSD, totalUSD := calculateLegacyUSDCost(usage, pricing)
	return prompt, completion, total, promptUSD, completionUSD, totalUSD, true
}

func estimateLegacyCurrency(requestedModel, effectiveModel, provider string) (string, bool) {
	pricing, ok := resolveBootstrapPricing(requestedModel, effectiveModel, provider)
	if !ok {
		return "", false
	}
	return pricing.Currency, true
}

func resolveBootstrapPricing(requestedModel, effectiveModel, provider string) (telemetrycore.Pricing, bool) {
	_, pricing, ok := telemetrycore.ResolvePricing(telemetrycore.BootstrapPricingSnapshot().Catalog, requestedModel, effectiveModel, provider)
	return pricing, ok
}

func calculateLegacyNativeCost(usage telemetrycore.Usage, price telemetrycore.Pricing) (float64, float64, float64) {
	cachedTokens := clampLegacyCachedPromptTokens(usage)
	uncachedTokens := usage.PromptTokens - cachedTokens
	prompt := (float64(uncachedTokens) / 1_000_000) * price.InputPer1M
	if cachedTokens > 0 {
		cachedRate := price.CachedInputPer1M
		if cachedRate <= 0 {
			cachedRate = price.InputPer1M
		}
		prompt += (float64(cachedTokens) / 1_000_000) * cachedRate
	}
	completion := (float64(usage.CompletionTokens) / 1_000_000) * price.OutputPer1M
	return prompt, completion, prompt + completion
}

func calculateLegacyUSDCost(usage telemetrycore.Usage, price telemetrycore.Pricing) (float64, float64, float64) {
	cachedTokens := clampLegacyCachedPromptTokens(usage)
	uncachedTokens := usage.PromptTokens - cachedTokens
	prompt := (float64(uncachedTokens) / 1_000_000) * price.InputPer1MUsd
	if cachedTokens > 0 {
		cachedRate := price.CachedInputPer1MUsd
		if cachedRate <= 0 {
			cachedRate = price.InputPer1MUsd
		}
		prompt += (float64(cachedTokens) / 1_000_000) * cachedRate
	}
	completion := (float64(usage.CompletionTokens) / 1_000_000) * price.OutputPer1MUsd
	return prompt, completion, prompt + completion
}

func clampLegacyCachedPromptTokens(usage telemetrycore.Usage) int {
	cached := usage.CachedPromptTokens
	if cached < 0 {
		return 0
	}
	if cached > usage.PromptTokens {
		return usage.PromptTokens
	}
	return cached
}
