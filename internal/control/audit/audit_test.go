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
