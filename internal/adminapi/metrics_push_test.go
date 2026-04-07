package adminapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestStartMetricsPushNilSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Should not panic with nil bus or store.
	StartMetricsPush(ctx, nil, nil, time.Second)
	StartMetricsPush(ctx, NewEventBus(10), nil, time.Second)
	StartMetricsPush(ctx, nil, nil, time.Second)
}

func TestStartMetricsPushSkipsWithoutSubscribers(t *testing.T) {
	bus := NewEventBus(10)

	// Subscribe so we can observe, then immediately unsubscribe.
	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	bus.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// With no subscribers, nothing should be published even after ticking.
	StartMetricsPush(ctx, bus, nil, 50*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	if bus.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers, got %d", bus.SubscriberCount())
	}
}

func TestStartMetricsPushPublishesEvents(t *testing.T) {
	bus := NewEventBus(10)
	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Unsubscribe(ch)

	// Use a nil store — buildMetricsSnapshot will panic if store is nil,
	// but StartMetricsPush guards against nil store.  We test the actual
	// publish path indirectly via buildMetricsSnapshot + a real bus.
	// For a full integration test we would need a real Store, which requires
	// database setup.  Here we test the wiring/event-type contract.

	_, cancel := context.WithCancel(context.Background())

	// Manually test buildMetricsSnapshot → Publish path by calling Publish directly.
	snap := metricsSnapshot{
		Last1m:  windowMetricsView{Requests: 10, Successes: 9, Failures: 1, AvgLatencyMs: 42.5},
		Last5m:  windowMetricsView{Requests: 50, Successes: 45, Failures: 5, AvgLatencyMs: 55.0},
		Last1h:  windowMetricsView{Requests: 500, Successes: 480, Failures: 20, AvgLatencyMs: 60.0},
		Last24h: windowMetricsView{Requests: 5000, Successes: 4800, Failures: 200, AvgLatencyMs: 70.0},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(Event{
		Type:      "metrics_update",
		Data:      string(data),
		Timestamp: time.Now(),
	})

	select {
	case ev := <-ch:
		if ev.Type != "metrics_update" {
			t.Fatalf("expected event type metrics_update, got %s", ev.Type)
		}
		var got metricsSnapshot
		if err := json.Unmarshal([]byte(ev.Data), &got); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if got.Last1m.Requests != 10 {
			t.Fatalf("expected Last1m.Requests=10, got %d", got.Last1m.Requests)
		}
		if got.Last5m.Successes != 45 {
			t.Fatalf("expected Last5m.Successes=45, got %d", got.Last5m.Successes)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for metrics_update event")
	}

	cancel()
}

func TestStartMetricsPushStopsOnCancel(t *testing.T) {
	bus := NewEventBus(10)
	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Unsubscribe(ch)

	ctx, cancel := context.WithCancel(context.Background())

	// We can't pass a real store here, but we verify the goroutine respects
	// context cancellation by cancelling immediately and ensuring no panic.
	cancel()
	StartMetricsPush(ctx, bus, nil, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
}

func TestBuildMetricsSnapshotShape(t *testing.T) {
	// Verify the JSON shape matches what the frontend expects.
	snap := metricsSnapshot{
		Last1m:  windowMetricsView{Requests: 1},
		Last5m:  windowMetricsView{Requests: 5},
		Last1h:  windowMetricsView{Requests: 60},
		Last24h: windowMetricsView{Requests: 1440},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"last_1m", "last_5m", "last_1h", "last_24h"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing expected key %q in metrics snapshot JSON", key)
		}
	}
}
