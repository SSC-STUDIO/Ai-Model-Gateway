package adminapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus(50)

	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	evt := Event{Type: "config_changed", Data: "test", Timestamp: time.Now()}
	bus.Publish(evt)

	select {
	case got := <-ch:
		if got.Type != "config_changed" {
			t.Fatalf("expected type config_changed, got %s", got.Type)
		}
		if got.Data != "test" {
			t.Fatalf("expected data test, got %s", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	bus.Unsubscribe(ch)
	if bus.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", bus.SubscriberCount())
	}
}

func TestEventBus_MaxSubscribers(t *testing.T) {
	bus := NewEventBus(50)
	bus.maxSubs = 2

	_, err1 := bus.Subscribe()
	if err1 != nil {
		t.Fatalf("subscribe 1: %v", err1)
	}
	_, err2 := bus.Subscribe()
	if err2 != nil {
		t.Fatalf("subscribe 2: %v", err2)
	}
	_, err3 := bus.Subscribe()
	if err3 == nil {
		t.Fatal("expected error when exceeding max subscribers")
	}
}

func TestEventBus_PublishSetsTimestamp(t *testing.T) {
	bus := NewEventBus(50)
	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	bus.Publish(Event{Type: "test", Data: "hello"})

	select {
	case evt := <-ch:
		if evt.Timestamp.IsZero() {
			t.Fatal("expected non-zero timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	bus.Unsubscribe(ch)
}

func TestEventsSSE_Unauthorized(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	d.EventBus = NewEventBus(50)

	r := setupRouter(t, d)
	req := httptest.NewRequest("GET", "/api/admin/events", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestEventsSSE_StreamsEvents(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	bus := NewEventBus(50)
	d.EventBus = bus

	r := setupRouter(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := authedRequest(t, "GET", "/api/admin/events", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Give the handler time to subscribe
	time.Sleep(50 * time.Millisecond)

	bus.Publish(Event{Type: "config_changed", Data: "test payload"})

	// Give the handler time to write
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := w.Body.String()
	if !strings.Contains(body, "event: config_changed") {
		t.Fatalf("expected SSE event line, got: %s", body)
	}
	if !strings.Contains(body, "data: ") {
		t.Fatalf("expected SSE data line, got: %s", body)
	}

	// Parse the data line to verify JSON
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var evt Event
			if err := json.Unmarshal([]byte(jsonData), &evt); err != nil {
				t.Fatalf("failed to parse event JSON: %v, raw: %s", err, jsonData)
			}
			if evt.Type != "config_changed" {
				t.Fatalf("expected config_changed, got %s", evt.Type)
			}
			if evt.Data != "test payload" {
				t.Fatalf("expected 'test payload', got %s", evt.Data)
			}
			break
		}
	}
}

func TestEventsSSE_Headers(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	bus := NewEventBus(50)
	d.EventBus = bus

	r := setupRouter(t, d)

	ctx, cancel := context.WithCancel(context.Background())
	req := authedRequest(t, "GET", "/api/admin/events", "")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %s", cc)
	}
}

func TestConfigSave_PublishesEvent(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	bus := NewEventBus(50)
	d.EventBus = bus
	d.ConfigSave = func(payload json.RawMessage) (interface{}, error) {
		return map[string]string{"status": "ok"}, nil
	}

	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	r := setupRouter(t, d)
	req := authedRequest(t, "PUT", "/api/admin/config", `{"admin":{"language":"en"}}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case evt := <-ch:
		if evt.Type != "config_changed" {
			t.Fatalf("expected config_changed event, got %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config_changed event from save")
	}
}

func TestConfigRollback_PublishesEvent(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	bus := NewEventBus(50)
	d.EventBus = bus
	d.ConfigRollback = func(versionID string) (interface{}, error) {
		return map[string]string{"status": "rolled back"}, nil
	}

	ch, err := bus.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	r := setupRouter(t, d)
	req := authedRequest(t, "POST", "/api/admin/config/rollback", `{"version_id":"v1"}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	select {
	case evt := <-ch:
		if evt.Type != "config_changed" {
			t.Fatalf("expected config_changed event, got %s", evt.Type)
		}
		if !strings.Contains(evt.Data, "v1") {
			t.Fatalf("expected rollback event to mention version, got %s", evt.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for config_changed event from rollback")
	}
}

func TestEventsSSE_NotMountedWithoutBus(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	// EventBus is nil — /events should not be mounted

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/events", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 404/405 without EventBus, got %d", w.Code)
	}
}
