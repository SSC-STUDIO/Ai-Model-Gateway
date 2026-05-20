package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordsAndRedactsAuditEvents(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	err = store.Record(context.Background(), Event{
		Time:     time.Date(2026, 4, 25, 1, 2, 3, 0, time.UTC),
		Actor:    "operator",
		Role:     "admin",
		Action:   "config.publish",
		Resource: "rev_1",
		Success:  true,
		Details: map[string]any{
			"api_key": "plain-secret",
			"nested":  map[string]any{"token": "also-secret", "safe": "ok"},
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	events, err := store.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Details["api_key"] != "[redacted]" {
		t.Fatalf("api_key detail = %#v, want redacted", events[0].Details["api_key"])
	}
	nested := events[0].Details["nested"].(map[string]any)
	if nested["token"] != "[redacted]" || nested["safe"] != "ok" {
		t.Fatalf("nested details = %#v", nested)
	}
}

func mustRecord(t *testing.T, store *Store, event Event) {
	t.Helper()
	if err := store.Record(context.Background(), event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}

func TestNewStoreEmptyPath(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("NewStore('') error = nil, want error")
	}
	if _, err := NewStore("  "); err == nil {
		t.Fatal("NewStore('  ') error = nil, want error")
	}
}

func TestNewStoreCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "audit.jsonl")
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if store.Path() != path {
		t.Errorf("Path() = %q, want %q", store.Path(), path)
	}
}

func TestPathNilStore(t *testing.T) {
	var s *Store
	if s.Path() != "" {
		t.Errorf("nil Store.Path() = %q, want empty", s.Path())
	}
}

func TestRecordNilStore(t *testing.T) {
	var s *Store
	// Should be a no-op, not panic
	if err := s.Record(context.Background(), Event{Action: "test"}); err != nil {
		t.Fatalf("nil Store.Record() error = %v, want nil", err)
	}
}

func TestRecordCancelledContext(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Record(ctx, Event{Action: "test"}); err == nil {
		t.Fatal("Record(cancelled ctx) error = nil, want error")
	}
}

func TestRecordGeneratesIDAndTime(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Record with no ID and zero time
	if err := store.Record(context.Background(), Event{Action: "test.auto"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	events, err := store.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].ID == "" {
		t.Error("auto-generated ID is empty")
	}
	if events[0].Time.IsZero() {
		t.Error("auto-generated Time is zero")
	}
}

func TestRecordTrimsFields(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(context.Background(), Event{
		Action:   "  test.trim  ",
		Resource: "  res  ",
		Actor:    "  actor  ",
		Role:     "  role  ",
		Error:    "  err  ",
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	events, _ := store.List(context.Background(), Query{Limit: 1})
	if events[0].Action != "test.trim" {
		t.Errorf("Action = %q, want trimmed", events[0].Action)
	}
	if events[0].Resource != "res" {
		t.Errorf("Resource = %q, want trimmed", events[0].Resource)
	}
	if events[0].Actor != "actor" {
		t.Errorf("Actor = %q, want trimmed", events[0].Actor)
	}
	if events[0].Role != "role" {
		t.Errorf("Role = %q, want trimmed", events[0].Role)
	}
	if events[0].Error != "err" {
		t.Errorf("Error = %q, want trimmed", events[0].Error)
	}
}

func TestListNilStore(t *testing.T) {
	var s *Store
	events, err := s.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("nil Store.List() error = %v", err)
	}
	if events != nil {
		t.Errorf("nil Store.List() = %v, want nil", events)
	}
}

func TestListCancelledContext(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// Write a couple events first
	for i := 0; i < 3; i++ {
		mustRecord(t, store, Event{Action: "test"})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.List(ctx, Query{Limit: 10})
	if err == nil {
		t.Fatal("List(cancelled ctx) error = nil, want error")
	}
}

func TestListNonExistentFile(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// File doesn't exist yet since nothing was recorded
	events, err := store.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d, want 0", len(events))
	}
}

func TestListActionFilter(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, store, Event{Action: "config.publish", Time: time.Now()})
	mustRecord(t, store, Event{Action: "config.rollback", Time: time.Now()})
	mustRecord(t, store, Event{Action: "config.publish", Time: time.Now()})

	events, err := store.List(context.Background(), Query{Limit: 100, Action: "config.publish"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2 for action filter", len(events))
	}
	for _, e := range events {
		if e.Action != "config.publish" {
			t.Errorf("action = %q, want config.publish", e.Action)
		}
	}
}

func TestListSinceFilter(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	oldTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mustRecord(t, store, Event{Action: "old", Time: oldTime})
	mustRecord(t, store, Event{Action: "new", Time: newTime})

	events, err := store.List(context.Background(), Query{
		Limit: 100,
		Since: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1 for since filter", len(events))
	}
	if events[0].Action != "new" {
		t.Errorf("action = %q, want new", events[0].Action)
	}
}

func TestListLimitEnforcement(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		mustRecord(t, store, Event{Action: "test", Time: time.Now().Add(time.Duration(i) * time.Second)})
	}

	events, err := store.List(context.Background(), Query{Limit: 3})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 3 {
		t.Errorf("len(events) = %d, want 3 (limit)", len(events))
	}
}

func TestListDefaultLimit(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, store, Event{Action: "test"})

	// Limit=0 should default to 100
	events, err := store.List(context.Background(), Query{Limit: 0})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 1 {
		t.Errorf("len(events) = %d, want 1", len(events))
	}
}

func TestListMaxLimit(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		mustRecord(t, store, Event{Action: "test", Time: time.Now().Add(time.Duration(i) * time.Second)})
	}

	// Limit > 1000 should be capped to 1000
	events, err := store.List(context.Background(), Query{Limit: 9999})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(events) != 10 {
		t.Errorf("len(events) = %d, want 10", len(events))
	}
}

func TestListReverseChronological(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mustRecord(t, store, Event{Action: "first", Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	mustRecord(t, store, Event{Action: "second", Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)})

	events, err := store.List(context.Background(), Query{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if events[0].Action != "second" {
		t.Errorf("first event action = %q, want second (reverse chronological)", events[0].Action)
	}
}

func TestRedactMapNilAndEmpty(t *testing.T) {
	if RedactMap(nil) != nil {
		t.Error("RedactMap(nil) != nil")
	}
	if RedactMap(map[string]any{}) != nil {
		t.Error("RedactMap(empty) != nil")
	}
}

func TestRedactMapSensitiveKeys(t *testing.T) {
	input := map[string]any{
		"api_key":       "my-key",
		"apikey":        "my-key",
		"token":         "my-token",
		"password":      "my-pw",
		"authorization": "Bearer xyz",
		"cookie":        "session=abc",
		"signing_key":   "hmac-key",
		"safe_field":    "visible",
		"empty_secret":  "",
		"nil_secret":    nil,
	}
	out := RedactMap(input)
	if out["safe_field"] != "visible" {
		t.Errorf("safe_field = %v, want visible", out["safe_field"])
	}
	for _, key := range []string{"api_key", "apikey", "token", "password", "authorization", "cookie", "signing_key"} {
		if out[key] != "[redacted]" {
			t.Errorf("%s = %v, want [redacted]", key, out[key])
		}
	}
	if out["empty_secret"] != "" {
		t.Errorf("empty_secret = %v, want empty string", out["empty_secret"])
	}
	if out["nil_secret"] != "" {
		t.Errorf("nil_secret = %v, want empty string", out["nil_secret"])
	}
}

func TestRedactMapStringMap(t *testing.T) {
	input := map[string]any{
		"headers": map[string]string{
			"authorization": "Bearer secret",
			"content-type":  "application/json",
		},
	}
	out := RedactMap(input)
	headers, ok := out["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers type = %T, want map[string]any", out["headers"])
	}
	if headers["authorization"] != "[redacted]" {
		t.Errorf("authorization = %v, want [redacted]", headers["authorization"])
	}
	if headers["content-type"] != "application/json" {
		t.Errorf("content-type = %v, want application/json", headers["content-type"])
	}
}

func TestRedactMapSliceValues(t *testing.T) {
	input := map[string]any{
		"items": []any{
			map[string]any{"api_key": "secret", "name": "alice"},
			map[string]any{"name": "bob"},
		},
	}
	out := RedactMap(input)
	items, ok := out["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T, want []any", out["items"])
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] type = %T, want map[string]any", items[0])
	}
	if first["api_key"] != "[redacted]" {
		t.Errorf("items[0].api_key = %v, want [redacted]", first["api_key"])
	}
	if first["name"] != "alice" {
		t.Errorf("items[0].name = %v, want alice", first["name"])
	}
}
