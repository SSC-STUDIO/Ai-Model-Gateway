package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
)

func TestDaemonGetEventCountReadsPersistedEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")

	logWriter, err := eventlog.New(path)
	if err != nil {
		t.Fatalf("eventlog.New returned error: %v", err)
	}

	accepted, dropped, err := logWriter.Append([]telemetryingest.Event{
		newTelemetryEvent("evt-1"),
		newTelemetryEvent("evt-2"),
	})
	if err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if accepted != 2 || dropped != 0 {
		t.Fatalf("Append = accepted:%d dropped:%d, want accepted:2 dropped:0", accepted, dropped)
	}
	if err := logWriter.Close(); err != nil {
		t.Fatalf("Close writer returned error: %v", err)
	}

	logReader, err := eventlog.New(path)
	if err != nil {
		t.Fatalf("reopen event log returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := logReader.Close(); err != nil {
			t.Fatalf("Close reader returned error: %v", err)
		}
	})

	daemon := &Daemon{eventLog: logReader}
	if got := daemon.GetEventCount(); got != 2 {
		t.Fatalf("GetEventCount() = %d, want 2", got)
	}
}

func TestTelemetryIngestRPCFlushIsHonestAboutNotBeingImplemented(t *testing.T) {
	rpc := &TelemetryIngestRPC{daemon: &Daemon{}}

	var resp telemetryingest.FlushResponse
	if err := rpc.Flush(telemetryingest.FlushRequest{}, &resp); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if resp.Success {
		t.Fatalf("Flush success = true, want false")
	}
	if resp.FlushedCount != 0 {
		t.Fatalf("Flush flushed_count = %d, want 0", resp.FlushedCount)
	}
	if !strings.Contains(resp.Error, "not implemented") {
		t.Fatalf("Flush error = %q, want not implemented", resp.Error)
	}
}

func newTelemetryEvent(eventID string) telemetryingest.Event {
	now := time.Date(2026, 4, 18, 13, 45, 0, 0, time.UTC)
	return telemetryingest.Event{
		EventID:       eventID,
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     now,
		Payload: telemetryingest.EventPayload{
			RequestID:          "req-" + eventID,
			Timestamp:          now,
			Path:               "/v1/chat/completions",
			RequestedModel:     "public-model",
			EffectiveModel:     "provider-model",
			ProviderID:         "provider-a",
			RouteMode:          "weighted_failover",
			StatusCode:         200,
			Latency:            15 * time.Millisecond,
			Attempts:           1,
			PromptTokens:       1,
			CachedPromptTokens: 0,
			CompletionTokens:   1,
		},
	}
}
