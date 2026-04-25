package telemetryingest

import (
	"testing"
	"time"
)

// TestEventStruct tests the Event struct initialization.
func TestEventStruct(t *testing.T) {
	now := time.Now()
	event := Event{
		EventID:        "evt-001",
		EventType:      "gateway.attempt.completed",
		SchemaVersion:  1,
		SourceService:  "gatewayd",
		SourceInstance: "gateway-1",
		EmittedAt:      now,
		Imported:       false,
		Payload: EventPayload{
			RequestID:          "req-001",
			Timestamp:          now,
			Path:               "/v1/chat/completions",
			RequestedModel:     "gpt-4",
			EffectiveModel:     "gpt-4-turbo",
			ProviderID:         "openai",
			RouteMode:          "direct",
			StatusCode:         200,
			Latency:            150 * time.Millisecond,
			Attempts:           1,
			PromptTokens:       100,
			CachedPromptTokens: 50,
			CompletionTokens:   200,
			Stream:             true,
			Error:              "",
		},
	}

	if event.EventID != "evt-001" {
		t.Errorf("EventID = %q, want evt-001", event.EventID)
	}
	if event.EventType != "gateway.attempt.completed" {
		t.Errorf("EventType = %q, want gateway.attempt.completed", event.EventType)
	}
	if event.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", event.SchemaVersion)
	}
	if event.SourceService != "gatewayd" {
		t.Errorf("SourceService = %q, want gatewayd", event.SourceService)
	}
	if event.Imported {
		t.Error("Imported should be false")
	}
}

// TestEventPayload tests the EventPayload struct.
func TestEventPayload(t *testing.T) {
	payload := EventPayload{
		RequestID:          "req-002",
		Timestamp:          time.Now(),
		Path:               "/v1/completions",
		RequestedModel:     "claude-3",
		EffectiveModel:     "claude-3-opus",
		ProviderID:         "anthropic",
		RouteMode:          "bridge_fallback",
		StatusCode:         500,
		Latency:            2 * time.Second,
		Attempts:           3,
		PromptTokens:       500,
		CachedPromptTokens: 0,
		CompletionTokens:   0,
		Stream:             false,
		Error:              "upstream timeout",
	}

	if payload.RequestID != "req-002" {
		t.Errorf("RequestID = %q, want req-002", payload.RequestID)
	}
	if payload.Path != "/v1/completions" {
		t.Errorf("Path = %q, want /v1/completions", payload.Path)
	}
	if payload.RouteMode != "bridge_fallback" {
		t.Errorf("RouteMode = %q, want bridge_fallback", payload.RouteMode)
	}
	if payload.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", payload.StatusCode)
	}
	if payload.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", payload.Attempts)
	}
	if payload.Error != "upstream timeout" {
		t.Errorf("Error = %q, want upstream timeout", payload.Error)
	}
}

// TestAppendBatchRequest tests the batch request struct.
func TestAppendBatchRequest(t *testing.T) {
	req := AppendBatchRequest{
		Events: []Event{
			{EventID: "evt-001"},
			{EventID: "evt-002"},
		},
		BatchID:        "batch-001",
		SourceInstance: "gateway-1",
	}

	if len(req.Events) != 2 {
		t.Errorf("Events length = %d, want 2", len(req.Events))
	}
	if req.BatchID != "batch-001" {
		t.Errorf("BatchID = %q, want batch-001", req.BatchID)
	}
	if req.SourceInstance != "gateway-1" {
		t.Errorf("SourceInstance = %q, want gateway-1", req.SourceInstance)
	}
}

// TestAppendBatchResponse tests the batch response struct.
func TestAppendBatchResponse(t *testing.T) {
	resp := AppendBatchResponse{
		Accepted:      10,
		Dropped:       2,
		HighWatermark: "evt-010",
		Error:         "",
	}

	if resp.Accepted != 10 {
		t.Errorf("Accepted = %d, want 10", resp.Accepted)
	}
	if resp.Dropped != 2 {
		t.Errorf("Dropped = %d, want 2", resp.Dropped)
	}
	if resp.HighWatermark != "evt-010" {
		t.Errorf("HighWatermark = %q, want evt-010", resp.HighWatermark)
	}
}

// TestFlushRequest tests the flush request struct.
func TestFlushRequest(t *testing.T) {
	req := FlushRequest{
		Timeout: 5 * time.Second,
	}

	if req.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", req.Timeout)
	}
}

// TestFlushResponse tests the flush response struct.
func TestFlushResponse(t *testing.T) {
	resp := FlushResponse{
		Success:      true,
		FlushedCount: 100,
		Error:        "",
	}

	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.FlushedCount != 100 {
		t.Errorf("FlushedCount = %d, want 100", resp.FlushedCount)
	}
}

// TestPingRequest tests the ping request struct.
func TestPingRequest(t *testing.T) {
	now := time.Now()
	req := PingRequest{
		Timestamp: now,
	}

	if !req.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", req.Timestamp, now)
	}
}

// TestPingResponse tests the ping response struct.
func TestPingResponse(t *testing.T) {
	now := time.Now()
	resp := PingResponse{
		Version:    "1.0.0",
		ServerTime: now,
		EventCount: 1000,
		Healthy:    true,
	}

	if resp.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", resp.Version)
	}
	if resp.EventCount != 1000 {
		t.Errorf("EventCount = %d, want 1000", resp.EventCount)
	}
	if !resp.Healthy {
		t.Error("Healthy should be true")
	}
}

// TestTelemetryIngestRPCInterface ensures the interface is correctly defined.
func TestTelemetryIngestRPCInterface(t *testing.T) {
	var _ TelemetryIngestRPC = (*mockTelemetryIngest)(nil)
}

type mockTelemetryIngest struct{}

func (m *mockTelemetryIngest) AppendBatch(req AppendBatchRequest, resp *AppendBatchResponse) error {
	return nil
}

func (m *mockTelemetryIngest) Flush(req FlushRequest, resp *FlushResponse) error {
	return nil
}

func (m *mockTelemetryIngest) Ping(req PingRequest, resp *PingResponse) error {
	return nil
}
