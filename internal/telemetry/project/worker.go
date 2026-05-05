// Package project provides the projection worker for telemetryd.
// The projector reads events from the event log and updates the query store.
package project

import (
	"context"
	"encoding/json"
	"time"

	"ai-model-gateway/internal/infra/logger"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/query"
)

// Projector reads events from the event log and projects them into the query store.
type Projector struct {
	eventLog   *eventlog.EventLog
	queryStore *query.Store
	interval   time.Duration
}

const projectionCheckpointName = "gateway_attempt_completed_projection"

// DrainResult describes a finite projection pass.
type DrainResult struct {
	Projected int64
	LastID    int64
}

// NewProjector creates a new projector.
func NewProjector(eventLog *eventlog.EventLog, queryStore *query.Store) *Projector {
	return &Projector{
		eventLog:   eventLog,
		queryStore: queryStore,
		interval:   5 * time.Second,
	}
}

// Drain projects all currently available events into the query store.
func (p *Projector) Drain(ctx context.Context) (DrainResult, error) {
	var result DrainResult
	if p.eventLog == nil || p.queryStore == nil {
		return result, nil
	}

	lastID, err := p.queryStore.LoadProjectionCheckpoint(ctx, projectionCheckpointName)
	if err != nil {
		return result, err
	}
	result.LastID = lastID

	for {
		count, maxID, err := p.projectNewEvents(ctx, lastID)
		if err != nil {
			return result, err
		}
		result.Projected += count
		result.LastID = maxID
		if maxID <= lastID {
			return result, nil
		}
		lastID = maxID
	}
}

// Run runs the projection worker.
func (p *Projector) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	var lastID int64 = 0
	if p.queryStore != nil {
		checkpoint, err := p.queryStore.LoadProjectionCheckpoint(ctx, projectionCheckpointName)
		if err != nil {
			logger.Warn("load checkpoint error", "error", err)
		} else {
			lastID = checkpoint
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, maxID, err := p.projectNewEvents(ctx, lastID)
			if err != nil {
				logger.Error("projection error", "error", err)
				continue
			}
			if maxID > lastID {
				lastID = maxID
			}
			if count > 0 {
				logger.Info("projected events", "count", count, "last_id", lastID)
			}
		}
	}
}

// projectNewEvents projects new events since the last ID.
func (p *Projector) projectNewEvents(ctx context.Context, lastID int64) (count int64, maxID int64, err error) {
	if p.eventLog == nil || p.queryStore == nil {
		return 0, lastID, nil
	}

	db := p.eventLog.GetDB()

	// Query new events
	rows, err := db.QueryContext(ctx, `
		SELECT id, event_id, event_type, emitted_at, payload
		FROM events
		WHERE id > ?
		ORDER BY id ASC
		LIMIT 1000
	`, lastID)
	if err != nil {
		return 0, lastID, err
	}
	defer rows.Close()

	maxID = lastID

	facts := make([]query.ProjectionFact, 0, 256)

	for rows.Next() {
		var id int64
		var eventID, eventType, emittedAt, payloadStr string
		if err := rows.Scan(&id, &eventID, &eventType, &emittedAt, &payloadStr); err != nil {
			continue
		}

		maxID = id

		// Only handle gateway.attempt.completed events
		if eventType != "gateway.attempt.completed" {
			continue
		}

		// Parse payload
		var payload eventPayload
		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			logger.Warn("parse payload error", "error", err)
			continue
		}

		fact := query.ProjectionFact{
			EventID:                  eventID,
			RequestID:                payload.RequestID,
			Timestamp:                emittedAt,
			Bucket:                   truncateToMinute(emittedAt),
			Path:                     payload.Path,
			RequestedModel:           payload.RequestedModel,
			EffectiveModel:           payload.EffectiveModel,
			ProviderID:               payload.ProviderID,
			RouteMode:                payload.RouteMode,
			StatusCode:               payload.StatusCode,
			LatencyMs:                payload.LatencyMs,
			Attempts:                 payload.Attempts,
			PromptTokens:             payload.PromptTokens,
			CachedPromptTokens:       payload.CachedPromptTokens,
			CompletionTokens:         payload.CompletionTokens,
			PricingStatus:            normalizeProjectedPricingStatus(payload.PricingStatus, payload.PromptTokens, payload.CachedPromptTokens, payload.CompletionTokens),
			PricingSourceID:          payload.PricingSourceID,
			PricingCurrency:          payload.PricingCurrency,
			PricingFXRateToUSD:       payload.PricingFXRateToUSD,
			PricingInputPer1M:        payload.PricingInputPer1M,
			PricingCachedInputPer1M:  payload.PricingCachedInputPer1M,
			PricingOutputPer1M:       payload.PricingOutputPer1M,
			PricingPromptCost:        payload.PricingPromptCost,
			PricingCompletionCost:    payload.PricingCompletionCost,
			PricingTotalCost:         payload.PricingTotalCost,
			PricingPromptCostUSD:     payload.PricingPromptCostUSD,
			PricingCompletionCostUSD: payload.PricingCompletionCostUSD,
			PricingTotalCostUSD:      payload.PricingTotalCostUSD,
			SyntheticKind:            payload.SyntheticKind,
			BenchmarkRunID:           payload.BenchmarkRunID,
			BenchmarkTargetID:        payload.BenchmarkTargetID,
			BenchmarkCaseID:          payload.BenchmarkCaseID,
			Stream:                   payload.Stream,
			ErrorMessage:             payload.Error,
		}
		facts = append(facts, fact)
	}

	if err := rows.Err(); err != nil {
		return 0, lastID, err
	}

	if maxID == lastID {
		return 0, lastID, nil
	}

	count, err = p.queryStore.ApplyProjectionBatch(ctx, projectionCheckpointName, maxID, facts)
	if err != nil {
		return 0, lastID, err
	}

	return count, maxID, nil
}

// truncateToMinute truncates a timestamp string to minute granularity.
func truncateToMinute(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
		if err != nil {
			return ts
		}
	}
	return t.Truncate(time.Minute).UTC().Format(time.RFC3339)
}

// Internal types for projection

type eventPayload struct {
	RequestID                string  `json:"request_id"`
	Timestamp                string  `json:"timestamp"`
	Path                     string  `json:"path"`
	RequestedModel           string  `json:"requested_model"`
	EffectiveModel           string  `json:"effective_model"`
	ProviderID               string  `json:"provider_id"`
	RouteMode                string  `json:"route_mode"`
	StatusCode               int     `json:"status_code"`
	LatencyMs                int64   `json:"latency_ms"`
	Attempts                 int     `json:"attempts"`
	PromptTokens             int64   `json:"prompt_tokens"`
	CachedPromptTokens       int64   `json:"cached_prompt_tokens"`
	CompletionTokens         int64   `json:"completion_tokens"`
	PricingStatus            string  `json:"pricing_status"`
	PricingSourceID          string  `json:"pricing_source_id"`
	PricingCurrency          string  `json:"pricing_currency"`
	PricingFXRateToUSD       float64 `json:"pricing_fx_rate_to_usd"`
	PricingInputPer1M        float64 `json:"pricing_input_per_1m"`
	PricingCachedInputPer1M  float64 `json:"pricing_cached_input_per_1m"`
	PricingOutputPer1M       float64 `json:"pricing_output_per_1m"`
	PricingPromptCost        float64 `json:"pricing_prompt_cost"`
	PricingCompletionCost    float64 `json:"pricing_completion_cost"`
	PricingTotalCost         float64 `json:"pricing_total_cost"`
	PricingPromptCostUSD     float64 `json:"pricing_prompt_cost_usd"`
	PricingCompletionCostUSD float64 `json:"pricing_completion_cost_usd"`
	PricingTotalCostUSD      float64 `json:"pricing_total_cost_usd"`
	SyntheticKind            string  `json:"synthetic_kind"`
	BenchmarkRunID           string  `json:"benchmark_run_id"`
	BenchmarkTargetID        string  `json:"benchmark_target_id"`
	BenchmarkCaseID          string  `json:"benchmark_case_id"`
	Stream                   bool    `json:"stream"`
	Error                    string  `json:"error"`
}

func normalizeProjectedPricingStatus(status string, promptTokens, cachedPromptTokens, completionTokens int64) string {
	switch status {
	case query.PricingStatusFixed, query.PricingStatusEstimatedLegacy, query.PricingStatusUnpriced:
		return status
	}
	if promptTokens > 0 || cachedPromptTokens > 0 || completionTokens > 0 {
		return query.PricingStatusEstimatedLegacy
	}
	return query.PricingStatusUnpriced
}
