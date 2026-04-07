package adminapi

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"ai-model-gateway/internal/infra/telemetrydb"
)

// metricsSnapshot mirrors the overview response shape so the frontend can
// treat SSE metrics_update payloads identically to /overview poll results.
type metricsSnapshot struct {
	Last1m  windowMetricsView `json:"last_1m"`
	Last5m  windowMetricsView `json:"last_5m"`
	Last1h  windowMetricsView `json:"last_1h"`
	Last24h windowMetricsView `json:"last_24h"`
}

type windowMetricsView struct {
	Requests     int     `json:"requests"`
	Successes    int     `json:"successes"`
	Failures     int     `json:"failures"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// StartMetricsPush launches a goroutine that periodically publishes
// telemetry overview data to the EventBus as "metrics_update" events.
// It stops when ctx is cancelled. No-op if bus or store is nil.
func StartMetricsPush(ctx context.Context, bus *EventBus, store *telemetrydb.Store, interval time.Duration) {
	if bus == nil || store == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if bus.SubscriberCount() == 0 {
					continue
				}
				snap := buildMetricsSnapshot(store)
				data, err := json.Marshal(snap)
				if err != nil {
					log.Printf("[metrics-push] marshal error: %v", err)
					continue
				}
				bus.Publish(Event{
					Type:      "metrics_update",
					Data:      string(data),
					Timestamp: time.Now(),
				})
			}
		}
	}()
}

func buildMetricsSnapshot(store *telemetrydb.Store) metricsSnapshot {
	query := func(window time.Duration) windowMetricsView {
		reqs, succ, fail, avg := store.QueryWindowMetrics(window)
		return windowMetricsView{
			Requests:     reqs,
			Successes:    succ,
			Failures:     fail,
			AvgLatencyMs: avg,
		}
	}
	return metricsSnapshot{
		Last1m:  query(time.Minute),
		Last5m:  query(5 * time.Minute),
		Last1h:  query(time.Hour),
		Last24h: query(24 * time.Hour),
	}
}
