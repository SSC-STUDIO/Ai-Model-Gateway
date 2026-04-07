package adminapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event represents a server-sent event published through the EventBus.
type Event struct {
	Type      string    `json:"type"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// EventBus fans out events to SSE subscribers.
type EventBus struct {
	mu       sync.Mutex
	subs     map[chan Event]struct{}
	maxSubs  int
}

// NewEventBus creates an EventBus with up to maxSubs concurrent clients.
func NewEventBus(maxSubs int) *EventBus {
	if maxSubs <= 0 {
		maxSubs = 50
	}
	return &EventBus{
		subs:    make(map[chan Event]struct{}),
		maxSubs: maxSubs,
	}
}

// Subscribe returns a channel that receives published events.
// Returns an error if the subscriber limit has been reached.
func (b *EventBus) Subscribe() (chan Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.subs) >= b.maxSubs {
		return nil, fmt.Errorf("max event subscribers reached (%d)", b.maxSubs)
	}

	ch := make(chan Event, 64)
	b.subs[ch] = struct{}{}
	return ch, nil
}

// Unsubscribe removes and closes a subscriber channel.
func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}

// Publish sends an event to all current subscribers.
// Slow consumers that have a full buffer are skipped (non-blocking send).
func (b *EventBus) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *EventBus) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// ---------------------------------------------------------------------------
// SSE handler
// ---------------------------------------------------------------------------

// eventsStreamHandler serves GET /api/admin/events as an SSE endpoint.
func eventsStreamHandler(bus *EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, err := bus.Subscribe()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer bus.Unsubscribe(ch)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
					return
				}
				flusher.Flush()

			case <-ticker.C:
				if _, err := fmt.Fprintf(w, ":keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()

			case <-ctx.Done():
				return
			}
		}
	}
}
