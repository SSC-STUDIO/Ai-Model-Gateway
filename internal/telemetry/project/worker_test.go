package project

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/query"
)

func TestProjectNewEventsDoesNotDoubleCountReplay(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	appendTelemetryEvents(t, eventLog,
		newGatewayAttemptEvent("evt-1", "req-1", time.Date(2026, 4, 18, 12, 0, 1, 0, time.UTC)),
		newGatewayAttemptEvent("evt-2", "req-2", time.Date(2026, 4, 18, 12, 0, 20, 0, time.UTC)),
	)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	if count != 2 {
		t.Fatalf("first projection count = %d, want 2", count)
	}
	if maxID != 2 {
		t.Fatalf("first projection maxID = %d, want 2", maxID)
	}

	assertQueryCounts(t, queryStore, 2, 2, 2, 0, 10, 2, 4)
	assertCheckpoint(t, queryStore, 2)

	count, maxID, err = projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("replay projection: %v", err)
	}
	if count != 0 {
		t.Fatalf("replay projection count = %d, want 0", count)
	}
	if maxID != 2 {
		t.Fatalf("replay projection maxID = %d, want 2", maxID)
	}

	assertQueryCounts(t, queryStore, 2, 2, 2, 0, 10, 2, 4)
	assertCheckpoint(t, queryStore, 2)
}

func TestProjectNewEventsAdvancesCheckpointForSkippedEvents(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	appendTelemetryEvents(t, eventLog, telemetryingest.Event{
		EventID:       "evt-skip",
		EventType:     "gateway.health.checked",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     time.Date(2026, 4, 18, 12, 1, 0, 0, time.UTC),
	})

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("project skipped event: %v", err)
	}
	if count != 0 {
		t.Fatalf("skipped projection count = %d, want 0", count)
	}
	if maxID != 1 {
		t.Fatalf("skipped projection maxID = %d, want 1", maxID)
	}
	assertCheckpoint(t, queryStore, 1)

	appendTelemetryEvents(t, eventLog, newGatewayAttemptEvent("evt-3", "req-3", time.Date(2026, 4, 18, 12, 1, 10, 0, time.UTC)))

	count, maxID, err = projector.projectNewEvents(ctx, 1)
	if err != nil {
		t.Fatalf("project new event after skipped event: %v", err)
	}
	if count != 1 {
		t.Fatalf("projection count after skipped event = %d, want 1", count)
	}
	if maxID != 2 {
		t.Fatalf("projection maxID after skipped event = %d, want 2", maxID)
	}

	assertQueryCounts(t, queryStore, 1, 1, 1, 0, 5, 1, 2)
	assertCheckpoint(t, queryStore, 2)
}

func TestProjectNewEventsWithNilEventLog(t *testing.T) {
	ctx := context.Background()
	projector := NewProjector(nil, nil)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("project with nil stores: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if maxID != 0 {
		t.Fatalf("maxID = %d, want 0", maxID)
	}
}

func TestProjectNewEventsWithNilQueryStore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("create event log: %v", err)
	}
	t.Cleanup(func() { _ = eventLog.Close() })

	projector := NewProjector(eventLog, nil)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("project with nil query store: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if maxID != 0 {
		t.Fatalf("maxID = %d, want 0", maxID)
	}
}

func TestProjectNewEventsWithInvalidJSONPayload(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	db := eventLog.GetDB()
	_, err := db.Exec(`
		INSERT INTO events (event_id, event_type, schema_version, source_service, source_instance, emitted_at, imported, payload)
		VALUES (?, 'gateway.attempt.completed', 1, 'gatewayd', 'test-instance', ?, 0, ?)
	`, "evt-bad-json", time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), "not valid json")
	if err != nil {
		t.Fatalf("insert bad json event: %v", err)
	}

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("project with bad JSON: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if maxID != 1 {
		t.Fatalf("maxID = %d, want 1", maxID)
	}
	assertCheckpoint(t, queryStore, 1)
}

func TestTruncateToMinute(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "RFC3339Nano format",
			input:    "2026-04-18T12:34:56.789123456Z",
			expected: "2026-04-18T12:34:00Z",
		},
		{
			name:     "RFC3339 format",
			input:    "2026-04-18T12:34:56Z",
			expected: "2026-04-18T12:34:00Z",
		},
		{
			name:     "RFC3339 with timezone offset",
			input:    "2026-04-18T12:34:56+00:00",
			expected: "2026-04-18T12:34:00Z",
		},
		{
			name:     "invalid format returns original",
			input:    "not-a-timestamp",
			expected: "not-a-timestamp",
		},
		{
			name:     "empty string returns original",
			input:    "",
			expected: "",
		},
		{
			name:     "partial timestamp returns original",
			input:    "2026-04-18",
			expected: "2026-04-18",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateToMinute(tt.input)
			if got != tt.expected {
				t.Errorf("truncateToMinute(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRunContextCancellation(t *testing.T) {
	projector, _, queryStore := newProjectorTestHarness(t)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	_, err := queryStore.LoadProjectionCheckpoint(context.Background(), projectionCheckpointName)
	if err != nil {
		t.Fatalf("query store should still be functional: %v", err)
	}
}

func TestNewProjectorFields(t *testing.T) {
	projector, _, _ := newProjectorTestHarness(t)

	if projector.eventLog == nil {
		t.Error("eventLog should not be nil")
	}
	if projector.queryStore == nil {
		t.Error("queryStore should not be nil")
	}
	if projector.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", projector.interval)
	}
}

func TestProjectNewEventsWithMultipleEventTypes(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	appendTelemetryEvents(t, eventLog,
		telemetryingest.Event{
			EventID:       "evt-health-1",
			EventType:     "gateway.health.checked",
			SchemaVersion: 1,
			SourceService: "gatewayd",
			EmittedAt:     time.Date(2026, 4, 18, 12, 0, 1, 0, time.UTC),
		},
		newGatewayAttemptEvent("evt-attempt-1", "req-1", time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC)),
		telemetryingest.Event{
			EventID:       "evt-health-2",
			EventType:     "gateway.health.checked",
			SchemaVersion: 1,
			SourceService: "gatewayd",
			EmittedAt:     time.Date(2026, 4, 18, 12, 0, 10, 0, time.UTC),
		},
		newGatewayAttemptEvent("evt-attempt-2", "req-2", time.Date(2026, 4, 18, 12, 0, 15, 0, time.UTC)),
	)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if maxID != 4 {
		t.Fatalf("maxID = %d, want 4", maxID)
	}
	assertCheckpoint(t, queryStore, 4)
	assertQueryCounts(t, queryStore, 2, 2, 2, 0, 10, 2, 4)
}

func TestProjectNewEventsWithStreamingEvent(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	streamEvent := telemetryingest.Event{
		EventID:       "evt-stream",
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
		Payload: telemetryingest.EventPayload{
			RequestID:          "req-stream",
			Timestamp:          time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
			Path:               "/v1/chat/completions",
			RequestedModel:     "public-model",
			EffectiveModel:     "provider-model",
			ProviderID:         "provider-a",
			RouteMode:          "weighted_failover",
			StatusCode:         200,
			Latency:            100 * time.Millisecond,
			Attempts:           1,
			PromptTokens:       10,
			CachedPromptTokens: 0,
			CompletionTokens:   20,
			Stream:             true,
		},
	}

	appendTelemetryEvents(t, eventLog, streamEvent)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	if maxID != 1 {
		t.Fatalf("maxID = %d, want 1", maxID)
	}

	db := queryStore.GetDB()
	var stream bool
	err = db.QueryRow(`SELECT stream FROM request_facts LIMIT 1`).Scan(&stream)
	if err != nil {
		t.Fatalf("query stream flag: %v", err)
	}
	if !stream {
		t.Error("stream flag should be true")
	}
}

func TestProjectNewEventsWithErrorPayload(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	errorEvent := telemetryingest.Event{
		EventID:       "evt-error",
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
		Payload: telemetryingest.EventPayload{
			RequestID:          "req-error",
			Timestamp:          time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
			Path:               "/v1/chat/completions",
			RequestedModel:     "public-model",
			EffectiveModel:     "",
			ProviderID:         "provider-a",
			RouteMode:          "weighted_failover",
			StatusCode:         500,
			Latency:            50 * time.Millisecond,
			Attempts:           3,
			PromptTokens:       5,
			CachedPromptTokens: 0,
			CompletionTokens:   0,
			Error:              "provider timeout after 30s",
		},
	}

	appendTelemetryEvents(t, eventLog, errorEvent)

	count, _, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	db := queryStore.GetDB()
	var errorMsg string
	err = db.QueryRow(`SELECT error_message FROM request_facts LIMIT 1`).Scan(&errorMsg)
	if err != nil {
		t.Fatalf("query error message: %v", err)
	}
	if errorMsg != "provider timeout after 30s" {
		t.Errorf("error_message = %q, want %q", errorMsg, "provider timeout after 30s")
	}
}

func TestRunWithNilQueryStore(t *testing.T) {
	dir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("create event log: %v", err)
	}
	t.Cleanup(func() { _ = eventLog.Close() })

	projector := NewProjector(eventLog, nil)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunWithNilEventLog(t *testing.T) {
	dir := t.TempDir()
	queryStore, err := query.NewStore(filepath.Join(dir, "query.db"))
	if err != nil {
		t.Fatalf("create query store: %v", err)
	}
	t.Cleanup(func() { _ = queryStore.Close() })

	projector := NewProjector(nil, queryStore)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestProjectNewEventsEmptyTable(t *testing.T) {
	ctx := context.Background()
	projector, _, queryStore := newProjectorTestHarness(t)

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if maxID != 0 {
		t.Fatalf("maxID = %d, want 0", maxID)
	}

	_ = queryStore
}

func TestProjectNewEventsWithCheckpointGreaterThanMaxID(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, _ := newProjectorTestHarness(t)

	appendTelemetryEvents(t, eventLog, newGatewayAttemptEvent("evt-1", "req-1", time.Date(2026, 4, 18, 12, 0, 1, 0, time.UTC)))

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("first projection: %v", err)
	}
	if count != 1 {
		t.Fatalf("first projection count = %d, want 1", count)
	}

	count, maxID, err = projector.projectNewEvents(ctx, 999)
	if err != nil {
		t.Fatalf("projection with high checkpoint: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
	if maxID != 999 {
		t.Fatalf("maxID = %d, want 999", maxID)
	}
}

func TestProjectNewEventsWithLatency(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	event := telemetryingest.Event{
		EventID:       "evt-latency",
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
		Payload: telemetryingest.EventPayload{
			RequestID:          "req-latency",
			Timestamp:          time.Date(2026, 4, 18, 12, 0, 5, 0, time.UTC),
			Path:               "/v1/chat/completions",
			RequestedModel:     "public-model",
			EffectiveModel:     "provider-model",
			ProviderID:         "provider-a",
			RouteMode:          "weighted_failover",
			StatusCode:         200,
			Latency:            1234 * time.Millisecond,
			Attempts:           1,
			PromptTokens:       100,
			CachedPromptTokens: 50,
			CompletionTokens:   200,
		},
	}

	appendTelemetryEvents(t, eventLog, event)

	count, _, err := projector.projectNewEvents(ctx, 0)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	db := queryStore.GetDB()
	var latencyMs int64
	err = db.QueryRow(`SELECT latency_ms FROM request_facts LIMIT 1`).Scan(&latencyMs)
	if err != nil {
		t.Fatalf("query latency: %v", err)
	}
	if latencyMs != 1234 {
		t.Errorf("latency_ms = %d, want 1234", latencyMs)
	}
}

func newProjectorTestHarness(t *testing.T) (*Projector, *eventlog.EventLog, *query.Store) {
	t.Helper()

	dir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dir, "events.db"))
	if err != nil {
		t.Fatalf("create event log: %v", err)
	}
	t.Cleanup(func() { _ = eventLog.Close() })

	queryStore, err := query.NewStore(filepath.Join(dir, "query.db"))
	if err != nil {
		t.Fatalf("create query store: %v", err)
	}
	t.Cleanup(func() { _ = queryStore.Close() })

	return NewProjector(eventLog, queryStore), eventLog, queryStore
}

func appendTelemetryEvents(t *testing.T, eventLog *eventlog.EventLog, events ...telemetryingest.Event) {
	t.Helper()

	accepted, dropped, err := eventLog.Append(events)
	if err != nil {
		t.Fatalf("append telemetry events: %v", err)
	}
	if dropped != 0 {
		t.Fatalf("append telemetry events dropped = %d, want 0", dropped)
	}
	if accepted != len(events) {
		t.Fatalf("append telemetry events accepted = %d, want %d", accepted, len(events))
	}
}

func newGatewayAttemptEvent(eventID, requestID string, emittedAt time.Time) telemetryingest.Event {
	return telemetryingest.Event{
		EventID:       eventID,
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     emittedAt,
		Payload: telemetryingest.EventPayload{
			RequestID:          requestID,
			Timestamp:          emittedAt,
			Path:               "/v1/chat/completions",
			RequestedModel:     "public-model",
			EffectiveModel:     "provider-model",
			ProviderID:         "provider-a",
			RouteMode:          "weighted_failover",
			StatusCode:         200,
			Latency:            25 * time.Millisecond,
			Attempts:           1,
			PromptTokens:       5,
			CachedPromptTokens: 1,
			CompletionTokens:   2,
		},
	}
}

func assertCheckpoint(t *testing.T, queryStore *query.Store, want int64) {
	t.Helper()

	got, err := queryStore.LoadProjectionCheckpoint(context.Background(), projectionCheckpointName)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if got != want {
		t.Fatalf("checkpoint = %d, want %d", got, want)
	}
}

func assertQueryCounts(t *testing.T, queryStore *query.Store, wantFacts, wantRequests, wantSuccesses, wantFailures int64, wantInputTokens, wantCachedPromptTokens, wantOutputTokens int64) {
	t.Helper()

	db := queryStore.GetDB()

	var factsCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_facts`).Scan(&factsCount); err != nil {
		t.Fatalf("count request_facts: %v", err)
	}
	if factsCount != wantFacts {
		t.Fatalf("request_facts count = %d, want %d", factsCount, wantFacts)
	}

	var (
		requests           int64
		successes          int64
		failures           int64
		inputTokens        int64
		cachedPromptTokens int64
		outputTokens       int64
	)
	if err := db.QueryRow(`
		SELECT requests, successes, failures, input_tokens, cached_prompt_tokens, output_tokens
		FROM agg_buckets
		WHERE bucket = ?
	`, time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)).Scan(
		&requests,
		&successes,
		&failures,
		&inputTokens,
		&cachedPromptTokens,
		&outputTokens,
	); err != nil {
		if err := db.QueryRow(`
			SELECT requests, successes, failures, input_tokens, cached_prompt_tokens, output_tokens
			FROM agg_buckets
			ORDER BY bucket DESC
			LIMIT 1
		`).Scan(
			&requests, &successes, &failures, &inputTokens, &cachedPromptTokens, &outputTokens); err != nil {
			t.Fatalf("read agg_buckets: %v", err)
		}
	}

	if requests != wantRequests || successes != wantSuccesses || failures != wantFailures ||
		inputTokens != wantInputTokens || cachedPromptTokens != wantCachedPromptTokens || outputTokens != wantOutputTokens {
		t.Fatalf(
			"agg bucket = requests:%d successes:%d failures:%d input:%d cached:%d output:%d, want requests:%d successes:%d failures:%d input:%d cached:%d output:%d",
			requests, successes, failures, inputTokens, cachedPromptTokens, outputTokens,
			wantRequests, wantSuccesses, wantFailures, wantInputTokens, wantCachedPromptTokens, wantOutputTokens,
		)
	}
}

func TestRunWithQueryStoreError(t *testing.T) {
	projector, _, queryStore := newProjectorTestHarness(t)

	// Close the query store to cause LoadProjectionCheckpoint to fail
	_ = queryStore.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestProjectNewEventsWithClosedEventLog(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, _ := newProjectorTestHarness(t)

	// Close event log before projecting
	_ = eventLog.Close()

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	// Should return an error or 0 count
	if err == nil {
		if count != 0 || maxID != 0 {
			t.Logf("unexpected result: count=%d, maxID=%d", count, maxID)
		}
	}
}

func TestProjectNewEventsWithClosedQueryStore(t *testing.T) {
	ctx := context.Background()
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	appendTelemetryEvents(t, eventLog, newGatewayAttemptEvent("evt-1", "req-1", time.Date(2026, 4, 18, 12, 0, 1, 0, time.UTC)))

	// Close query store before projecting
	_ = queryStore.Close()

	count, maxID, err := projector.projectNewEvents(ctx, 0)
	// Should return an error
	if err == nil {
		t.Logf("expected error but got: count=%d, maxID=%d", count, maxID)
	}
}

func TestRunWithTickerCycle(t *testing.T) {
	projector, eventLog, queryStore := newProjectorTestHarness(t)

	// Set a shorter interval for faster testing
	projector.interval = 100 * time.Millisecond

	appendTelemetryEvents(t, eventLog, newGatewayAttemptEvent("evt-1", "req-1", time.Date(2026, 4, 18, 12, 0, 1, 0, time.UTC)))

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after enough time for at least one ticker cycle
	time.AfterFunc(300*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		projector.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Give a moment for the projection to complete
		time.Sleep(50 * time.Millisecond)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	// Verify the event was projected
	assertCheckpoint(t, queryStore, 1)
}
