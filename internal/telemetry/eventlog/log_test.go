package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}

	db := el.GetDB()
	if db == nil {
		t.Error("GetDB() returned nil")
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if err != nil {
		t.Errorf("failed to query events table: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 events, got %d", count)
	}
}

func TestNewCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "deep", "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed with nested path: %v", err)
	}
	defer el.Close()

	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("nested directory was not created")
	}
}

func TestAppend(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	now := time.Now().UTC()
	events := []telemetryingest.Event{
		{
			EventID:        "evt-001",
			EventType:      "gateway.attempt.completed",
			SchemaVersion:  1,
			SourceService:  "gatewayd",
			SourceInstance: "instance-1",
			EmittedAt:      now,
			Imported:       false,
			Payload: telemetryingest.EventPayload{
				RequestID:          "req-001",
				Timestamp:          now,
				Path:               "/v1/chat/completions",
				RequestedModel:     "gpt-4",
				EffectiveModel:     "gpt-4-turbo",
				ProviderID:         "openai",
				RouteMode:          "direct",
				StatusCode:         200,
				Latency:            1500 * time.Millisecond,
				Attempts:           1,
				PromptTokens:       100,
				CachedPromptTokens: 0,
				CompletionTokens:   50,
				Stream:             true,
				Error:              "",
			},
		},
		{
			EventID:        "evt-002",
			EventType:      "gateway.attempt.completed",
			SchemaVersion:  1,
			SourceService:  "gatewayd",
			SourceInstance: "instance-1",
			EmittedAt:      now.Add(time.Second),
			Imported:       true,
			Payload: telemetryingest.EventPayload{
				RequestID:          "req-002",
				Timestamp:          now.Add(time.Second),
				Path:               "/v1/completions",
				RequestedModel:     "gpt-3.5-turbo",
				EffectiveModel:     "gpt-3.5-turbo",
				ProviderID:         "openai",
				RouteMode:          "bridged",
				StatusCode:         500,
				Latency:            3000 * time.Millisecond,
				Attempts:           3,
				PromptTokens:       200,
				CachedPromptTokens: 100,
				CompletionTokens:   0,
				Stream:             false,
				Error:              "upstream timeout",
			},
		},
	}

	accepted, dropped, err := el.Append(events)
	if err != nil {
		t.Fatalf("Append() failed: %v", err)
	}
	if accepted != 2 {
		t.Errorf("expected 2 accepted, got %d", accepted)
	}
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}

	var count int
	err = el.GetDB().QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 events in database, got %d", count)
	}

	var eventID, eventType string
	var imported int
	err = el.GetDB().QueryRow("SELECT event_id, event_type, imported FROM events WHERE event_id = ?", "evt-001").Scan(
		&eventID, &eventType, &imported)
	if err != nil {
		t.Fatalf("failed to query first event: %v", err)
	}
	if eventID != "evt-001" {
		t.Errorf("expected event_id 'evt-001', got %q", eventID)
	}
	if eventType != "gateway.attempt.completed" {
		t.Errorf("expected event_type 'gateway.attempt.completed', got %q", eventType)
	}
	if imported != 0 {
		t.Errorf("expected imported 0, got %d", imported)
	}

	err = el.GetDB().QueryRow("SELECT imported FROM events WHERE event_id = ?", "evt-002").Scan(&imported)
	if err != nil {
		t.Fatalf("failed to query second event: %v", err)
	}
	if imported != 1 {
		t.Errorf("expected imported 1 for second event, got %d", imported)
	}
}

func TestAppendEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	accepted, dropped, err := el.Append([]telemetryingest.Event{})
	if err != nil {
		t.Fatalf("Append() with empty slice failed: %v", err)
	}
	if accepted != 0 {
		t.Errorf("expected 0 accepted, got %d", accepted)
	}
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}
}

func TestAppendDuplicateID(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	now := time.Now().UTC()
	event := telemetryingest.Event{
		EventID:        "evt-dup",
		EventType:      "gateway.attempt.completed",
		SchemaVersion:  1,
		SourceService:  "gatewayd",
		SourceInstance: "instance-1",
		EmittedAt:      now,
		Payload: telemetryingest.EventPayload{
			RequestID:  "req-001",
			Timestamp:  now,
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			Latency:    100 * time.Millisecond,
		},
	}

	accepted, dropped, err := el.Append([]telemetryingest.Event{event})
	if err != nil {
		t.Fatalf("first Append() failed: %v", err)
	}
	if accepted != 1 || dropped != 0 {
		t.Fatalf("first append: expected 1 accepted, 0 dropped; got %d accepted, %d dropped", accepted, dropped)
	}

	accepted, dropped, err = el.Append([]telemetryingest.Event{event})
	if err != nil {
		t.Fatalf("second Append() failed: %v", err)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped for duplicate, got %d", dropped)
	}
	if accepted != 0 {
		t.Errorf("expected 0 accepted for duplicate, got %d", accepted)
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if err := el.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}
}

func TestGetDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	db := el.GetDB()
	if db == nil {
		t.Error("GetDB() returned nil")
		return
	}

	var result int
	err = db.QueryRow("SELECT 1").Scan(&result)
	if err != nil {
		t.Errorf("failed to query via GetDB(): %v", err)
	}
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestSerializePayload(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
	payload := telemetryingest.EventPayload{
		RequestID:          "req-test",
		Timestamp:          now,
		Path:               "/v1/chat/completions",
		RequestedModel:     "gpt-4",
		EffectiveModel:     "gpt-4-turbo",
		ProviderID:         "openai",
		RouteMode:          "direct",
		StatusCode:         200,
		Latency:            2500 * time.Millisecond,
		Attempts:           2,
		PromptTokens:       150,
		CachedPromptTokens: 50,
		CompletionTokens:   75,
		Stream:             true,
		Error:              "test error",
	}

	result, err := serializePayload(payload)
	if err != nil {
		t.Fatalf("serializePayload() failed: %v", err)
	}

	tests := []struct {
		needle string
		name   string
	}{
		{`"request_id":"req-test"`, "request_id"},
		{`"path":"/v1/chat/completions"`, "path"},
		{`"requested_model":"gpt-4"`, "requested_model"},
		{`"effective_model":"gpt-4-turbo"`, "effective_model"},
		{`"provider_id":"openai"`, "provider_id"},
		{`"status_code":200`, "status_code"},
		{`"latency_ms":2500`, "latency_ms"},
		{`"stream":true`, "stream"},
	}

	for _, tc := range tests {
		if !contains(result, tc.needle) {
			t.Errorf("missing or incorrect %s in serialized payload", tc.name)
		}
	}
}

func TestSerializePayloadPricingFields(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	payload := telemetryingest.EventPayload{
		RequestID:              "req-pricing",
		Timestamp:              now,
		Path:                   "/v1/chat/completions",
		StatusCode:             200,
		Latency:                100 * time.Millisecond,
		PricingStatus:          "priced",
		PricingSourceID:        "openai-default",
		PricingCurrency:        "USD",
		PricingFXRateToUSD:     1.0,
		PricingInputPer1M:      2.5,
		PricingCachedInputPer1M: 1.25,
		PricingOutputPer1M:     10.0,
		PricingPromptCost:      0.00025,
		PricingCompletionCost:  0.001,
		PricingTotalCost:       0.00125,
		PricingPromptCostUSD:   0.00025,
		PricingCompletionCostUSD: 0.001,
		PricingTotalCostUSD:    0.00125,
	}

	result, err := serializePayload(payload)
	if err != nil {
		t.Fatalf("serializePayload() failed: %v", err)
	}

	pricingChecks := []string{
		`"pricing_status":"priced"`,
		`"pricing_source_id":"openai-default"`,
		`"pricing_currency":"USD"`,
		`"pricing_fx_rate_to_usd":1`,
		`"pricing_input_per_1m":2.5`,
		`"pricing_cached_input_per_1m":1.25`,
		`"pricing_output_per_1m":10`,
		`"pricing_prompt_cost":0.00025`,
		`"pricing_completion_cost":0.001`,
		`"pricing_total_cost":0.00125`,
		`"pricing_prompt_cost_usd":0.00025`,
		`"pricing_completion_cost_usd":0.001`,
		`"pricing_total_cost_usd":0.00125`,
	}
	for _, needle := range pricingChecks {
		if !contains(result, needle) {
			t.Errorf("missing %s in serialized payload", needle)
		}
	}
}

func TestSerializePayloadBenchmarkFields(t *testing.T) {
	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	payload := telemetryingest.EventPayload{
		RequestID:         "req-bench",
		Timestamp:         now,
		Path:              "/v1/chat/completions",
		StatusCode:        200,
		Latency:           50 * time.Millisecond,
		SyntheticKind:     "benchmark",
		BenchmarkRunID:    "run-001",
		BenchmarkTargetID: "target-001",
		BenchmarkCaseID:   "case-001",
	}

	result, err := serializePayload(payload)
	if err != nil {
		t.Fatalf("serializePayload() failed: %v", err)
	}

	benchChecks := []string{
		`"synthetic_kind":"benchmark"`,
		`"benchmark_run_id":"run-001"`,
		`"benchmark_target_id":"target-001"`,
		`"benchmark_case_id":"case-001"`,
	}
	for _, needle := range benchChecks {
		if !contains(result, needle) {
			t.Errorf("missing %s in serialized payload", needle)
		}
	}
}

func TestNewMkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()
	blocked := filepath.Join(tmpDir, "blocked.db")
	// Create a file where the directory should be, so MkdirAll fails
	if err := os.WriteFile(blocked, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	dbPath := filepath.Join(blocked, "sub", "test.db")

	_, err := New(dbPath)
	if err == nil {
		t.Fatal("expected New() to fail when directory path is blocked by a file")
	}
}

func TestAppendClosedDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	el.Close()

	events := []telemetryingest.Event{
		{
			EventID:       "evt-closed",
			EventType:     "gateway.attempt.completed",
			SchemaVersion: 1,
			SourceService: "gw",
			EmittedAt:     time.Now().UTC(),
			Payload: telemetryingest.EventPayload{
				RequestID: "req-closed",
				Timestamp: time.Now().UTC(),
				Path:      "/v1/test",
			},
		},
	}

	_, _, err = el.Append(events)
	if err == nil {
		t.Error("expected Append on closed DB to fail")
	}
}

func TestAppendDuplicateAndAccept(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	now := time.Now().UTC()
	event := telemetryingest.Event{
		EventID:       "evt-mix",
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gw",
		SourceInstance: "i1",
		EmittedAt:     now,
		Payload: telemetryingest.EventPayload{
			RequestID: "req-mix",
			Timestamp: now,
			Path:      "/v1/test",
			StatusCode: 200,
			Latency:   100 * time.Millisecond,
		},
	}

	// First append: accepted
	accepted, dropped, err := el.Append([]telemetryingest.Event{event})
	if err != nil {
		t.Fatalf("first Append() failed: %v", err)
	}
	if accepted != 1 || dropped != 0 {
		t.Errorf("first: expected 1/0, got %d/%d", accepted, dropped)
	}

	// Mix of duplicate (dropped) and new (accepted)
	newEvent := event
	newEvent.EventID = "evt-mix-2"
	newEvent.Payload.RequestID = "req-mix-2"

	accepted, dropped, err = el.Append([]telemetryingest.Event{event, newEvent})
	if err != nil {
		t.Fatalf("second Append() failed: %v", err)
	}
	if accepted != 1 {
		t.Errorf("expected 1 accepted, got %d", accepted)
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped, got %d", dropped)
	}
}

func TestAppendWithAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	now := time.Now().UTC()
	events := []telemetryingest.Event{
		{
			EventID:        "evt-full",
			EventType:      "gateway.attempt.completed",
			SchemaVersion:  1,
			SourceService:  "gatewayd",
			SourceInstance: "inst-1",
			EmittedAt:      now,
			Imported:       false,
			Payload: telemetryingest.EventPayload{
				RequestID:              "req-full",
				Timestamp:              now,
				Path:                   "/v1/chat/completions",
				RequestedModel:         "gpt-4",
				EffectiveModel:         "gpt-4-turbo",
				ProviderID:             "openai",
				RouteMode:              "direct",
				StatusCode:             200,
				Latency:                500 * time.Millisecond,
				Attempts:               1,
				PromptTokens:           100,
				CachedPromptTokens:     50,
				CompletionTokens:       200,
				PricingStatus:          "priced",
				PricingSourceID:        "openai-default",
				PricingCurrency:        "USD",
				PricingFXRateToUSD:     1.0,
				PricingInputPer1M:      2.5,
				PricingCachedInputPer1M: 1.25,
				PricingOutputPer1M:     10.0,
				PricingPromptCost:      0.00025,
				PricingCompletionCost:  0.002,
				PricingTotalCost:       0.00225,
				PricingPromptCostUSD:   0.00025,
				PricingCompletionCostUSD: 0.002,
				PricingTotalCostUSD:    0.00225,
				SyntheticKind:          "benchmark",
				BenchmarkRunID:         "run-1",
				BenchmarkTargetID:      "target-1",
				BenchmarkCaseID:        "case-1",
				Stream:                 true,
			},
		},
	}

	accepted, dropped, err := el.Append(events)
	if err != nil {
		t.Fatalf("Append() failed: %v", err)
	}
	if accepted != 1 {
		t.Errorf("expected 1 accepted, got %d", accepted)
	}
	if dropped != 0 {
		t.Errorf("expected 0 dropped, got %d", dropped)
	}

	var payload string
	err = el.GetDB().QueryRow("SELECT payload FROM events WHERE event_id = ?", "evt-full").Scan(&payload)
	if err != nil {
		t.Fatalf("failed to query payload: %v", err)
	}
	// Verify pricing and benchmark fields are persisted
	for _, needle := range []string{
		`"pricing_status":"priced"`,
		`"pricing_total_cost_usd":0.00225`,
		`"synthetic_kind":"benchmark"`,
		`"benchmark_run_id":"run-1"`,
	} {
		if !contains(payload, needle) {
			t.Errorf("missing %s in persisted payload", needle)
		}
	}
}

func TestAppendConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	el, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer el.Close()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			now := time.Now().UTC()
			events := []telemetryingest.Event{
				{
					EventID:        fmt.Sprintf("evt-conc-%d", idx),
					EventType:      "gateway.attempt.completed",
					SchemaVersion:  1,
					SourceService:  "gatewayd",
					SourceInstance: "inst-1",
					EmittedAt:      now,
					Payload: telemetryingest.EventPayload{
						RequestID:  fmt.Sprintf("req-conc-%d", idx),
						Timestamp:  now,
						Path:       "/v1/chat/completions",
						StatusCode: 200,
						Latency:    100 * time.Millisecond,
					},
				},
			}
			accepted, dropped, err := el.Append(events)
			if err != nil {
				t.Errorf("concurrent Append() failed: %v", err)
			}
			if accepted != 1 {
				t.Errorf("expected 1 accepted, got %d", accepted)
			}
			if dropped != 0 {
				t.Errorf("expected 0 dropped, got %d", dropped)
			}
		}(i)
	}
	wg.Wait()

	var count int
	err = el.GetDB().QueryRow("SELECT COUNT(*) FROM events").Scan(&count)
	if err != nil {
		t.Fatalf("query count failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 events, got %d", count)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
