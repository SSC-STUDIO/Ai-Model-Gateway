package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"ai-model-gateway/internal/telemetry"
)

// CostEntry represents a single cost record.
type CostEntry struct {
	Model            string    `json:"model"`
	ProviderID       string    `json:"provider_id"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalCost        float64   `json:"total_cost"`
	Currency         string    `json:"currency"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
}

// CostQuery provides cost aggregation queries.
type CostQuery struct {
	db *sql.DB
}

// NewCostQuery creates a new CostQuery.
func NewCostQuery(db *sql.DB) *CostQuery {
	return &CostQuery{db: db}
}

// costRow holds raw data from the database before pricing is applied.
type costRow struct {
	model              string
	providerID         string
	promptTokens       int64
	cachedPromptTokens int64
	completionTokens   int64
}

// ByModel returns cost aggregated by model for the given time range.
func (q *CostQuery) ByModel(ctx context.Context, start, end time.Time) ([]CostEntry, error) {
	rows, err := q.db.QueryContext(ctx, `
SELECT
  `+projectedModelExpression+` AS model,
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0)
FROM request_facts
WHERE timestamp >= ? AND timestamp <= ?
  AND `+projectedModelExpression+` != ''
GROUP BY model
ORDER BY model ASC`,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("cost by model query: %w", err)
	}
	defer rows.Close()

	rawRows := make([]costRow, 0, 32)
	for rows.Next() {
		var r costRow
		if err := rows.Scan(&r.model, &r.promptTokens, &r.cachedPromptTokens, &r.completionTokens); err != nil {
			return nil, fmt.Errorf("cost by model scan: %w", err)
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost by model iterate: %w", err)
	}

	return buildCostEntries(rawRows, start, end, "model"), nil
}

// ByProvider returns cost aggregated by provider for the given time range.
func (q *CostQuery) ByProvider(ctx context.Context, start, end time.Time) ([]CostEntry, error) {
	rows, err := q.db.QueryContext(ctx, `
SELECT
  `+projectedModelExpression+` AS model,
  COALESCE(provider_id, ''),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0)
FROM request_facts
WHERE timestamp >= ? AND timestamp <= ?
  AND `+projectedModelExpression+` != ''
GROUP BY model, provider_id
ORDER BY provider_id ASC, model ASC`,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("cost by provider query: %w", err)
	}
	defer rows.Close()

	rawRows := make([]costRow, 0, 32)
	for rows.Next() {
		var r costRow
		if err := rows.Scan(&r.model, &r.providerID, &r.promptTokens, &r.cachedPromptTokens, &r.completionTokens); err != nil {
			return nil, fmt.Errorf("cost by provider scan: %w", err)
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost by provider iterate: %w", err)
	}

	return buildCostEntries(rawRows, start, end, "provider"), nil
}

// ByTimeRange returns cost aggregated by time buckets for the given range.
func (q *CostQuery) ByTimeRange(ctx context.Context, start, end time.Time, bucketSec int) ([]CostEntry, error) {
	if bucketSec <= 0 {
		bucketSec = 300 // default 5 minutes
	}

	rows, err := q.db.QueryContext(ctx, `
SELECT
  strftime('%Y-%m-%dT%H:%M:%SZ',
           datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch')) AS grouped_bucket,
  `+projectedModelExpression+` AS model,
  COALESCE(provider_id, ''),
  COALESCE(SUM(prompt_tokens), 0),
  COALESCE(SUM(cached_prompt_tokens), 0),
  COALESCE(SUM(completion_tokens), 0)
FROM request_facts
WHERE timestamp >= ? AND timestamp <= ?
  AND `+projectedModelExpression+` != ''
GROUP BY grouped_bucket, model, provider_id
ORDER BY grouped_bucket ASC, model ASC`,
		bucketSec, bucketSec,
		start.UTC().Format(time.RFC3339Nano),
		end.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, fmt.Errorf("cost by time range query: %w", err)
	}
	defer rows.Close()

	type bucketRow struct {
		bucket string
		raw    costRow
	}
	var bucketRows []bucketRow
	for rows.Next() {
		var br bucketRow
		if err := rows.Scan(
			&br.bucket,
			&br.raw.model,
			&br.raw.providerID,
			&br.raw.promptTokens,
			&br.raw.cachedPromptTokens,
			&br.raw.completionTokens,
		); err != nil {
			return nil, fmt.Errorf("cost by time range scan: %w", err)
		}
		bucketRows = append(bucketRows, br)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost by time range iterate: %w", err)
	}

	if len(bucketRows) == 0 {
		return nil, nil
	}

	// Group by bucket, then compute pricing per bucket.
	bucketGroups := make(map[string][]costRow, len(bucketRows))
	orderedBuckets := make([]string, 0, len(bucketRows))
	for _, br := range bucketRows {
		if _, exists := bucketGroups[br.bucket]; !exists {
			orderedBuckets = append(orderedBuckets, br.bucket)
		}
		bucketGroups[br.bucket] = append(bucketGroups[br.bucket], br.raw)
	}

	var entries []CostEntry
	for _, bucket := range orderedBuckets {
		bucketStart := parseStoredTimestamp(bucket)
		bucketEnd := bucketStart.Add(time.Duration(bucketSec) * time.Second)
		if bucketEnd.After(end) {
			bucketEnd = end
		}
		bucketEntries := buildCostEntries(bucketGroups[bucket], bucketStart, bucketEnd, "time")
		entries = append(entries, bucketEntries...)
	}

	return entries, nil
}

// buildCostEntries converts raw token counts into CostEntry values using the
// pricing catalog. Each costRow becomes its own CostEntry so callers can
// further aggregate as needed.
func buildCostEntries(rawRows []costRow, periodStart, periodEnd time.Time, mode string) []CostEntry {
	if len(rawRows) == 0 {
		return nil
	}

	// Build routes for pricing snapshot.
	routes := make([]telemetry.ModelRouteUsage, 0, len(rawRows))
	for _, r := range rawRows {
		routes = append(routes, telemetry.ModelRouteUsage{
			Model:    r.model,
			Upstream: r.providerID,
			Usage: telemetry.Usage{
				PromptTokens:       int(r.promptTokens),
				CachedPromptTokens: int(r.cachedPromptTokens),
				CompletionTokens:   int(r.completionTokens),
				TotalTokens:        int(r.promptTokens + r.completionTokens),
			},
		})
	}

	pricing := telemetry.BuildPricingSnapshot(
		telemetry.Snapshot{ByModelRoute: routes},
		telemetry.BootstrapPricingSnapshot(),
	)

	// Build a lookup from model+provider to cost.
	type modelProvider struct {
		model    string
		provider string
	}
	costMap := make(map[modelProvider]float64, len(pricing.Models))
	currencyMap := make(map[modelProvider]string, len(pricing.Models))
	for _, item := range pricing.Models {
		model := strings.TrimSpace(item.EffectiveModel)
		if model == "" {
			model = strings.TrimSpace(item.DisplayModel)
		}
		if model == "" {
			continue
		}
		mp := modelProvider{model: model, provider: strings.TrimSpace(item.Upstream)}
		costMap[mp] += item.Cost.Total
		if cur := item.Cost.Currency; cur != "" {
			currencyMap[mp] = cur
		}
	}

	entries := make([]CostEntry, 0, len(rawRows))
	for _, r := range rawRows {
		mp := modelProvider{model: r.model, provider: r.providerID}
		cost := costMap[mp]
		currency := currencyMap[mp]
		if currency == "" {
			currency = "USD"
		}

		entry := CostEntry{
			Model:            r.model,
			ProviderID:       r.providerID,
			PromptTokens:     r.promptTokens,
			CachedTokens:     r.cachedPromptTokens,
			CompletionTokens: r.completionTokens,
			TotalCost:        cost,
			Currency:         currency,
			PeriodStart:      periodStart,
			PeriodEnd:        periodEnd,
		}

		// For model-level aggregation (ByModel), collapse provider.
		if mode == "model" {
			entry.ProviderID = ""
		}

		entries = append(entries, entry)
	}

	// For model-level aggregation, merge entries with the same model.
	if mode == "model" {
		merged := make(map[string]*CostEntry, len(entries))
		var order []string
		for i := range entries {
			e := &entries[i]
			if existing, ok := merged[e.Model]; ok {
				existing.PromptTokens += e.PromptTokens
				existing.CachedTokens += e.CachedTokens
				existing.CompletionTokens += e.CompletionTokens
				existing.TotalCost += e.TotalCost
			} else {
				clone := *e
				merged[e.Model] = &clone
				order = append(order, e.Model)
			}
		}
		result := make([]CostEntry, 0, len(order))
		for _, model := range order {
			result = append(result, *merged[model])
		}
		return result
	}

	return entries
}
