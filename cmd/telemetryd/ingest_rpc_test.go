package main

import (
	"path/filepath"
	"testing"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
)

func TestNewIngestRPCServer(t *testing.T) {
	d := &Daemon{}
	server := NewIngestRPCServer(d)
	if server == nil {
		t.Fatal("NewIngestRPCServer() returned nil")
	}
}

func TestTelemetryIngestRPCAppendBatchEmptyEventsWithEventLog(t *testing.T) {
	dataDir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dataDir, "events.db"))
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}
	defer eventLog.Close()

	rpc := &TelemetryIngestRPC{daemon: &Daemon{eventLog: eventLog}}

	req := telemetryingest.AppendBatchRequest{
		BatchID: "batch-1",
		Events:  []telemetryingest.Event{},
	}
	var resp telemetryingest.AppendBatchResponse

	if err := rpc.AppendBatch(req, &resp); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if resp.Accepted != 0 {
		t.Fatalf("Accepted = %d, want 0", resp.Accepted)
	}
	if resp.Dropped != 0 {
		t.Fatalf("Dropped = %d, want 0", resp.Dropped)
	}
	if resp.HighWatermark == "" {
		t.Fatal("HighWatermark should be set")
	}
}

func TestTelemetryIngestRPCAppendBatchNilEventLog(t *testing.T) {
	rpc := &TelemetryIngestRPC{daemon: &Daemon{}}

	req := telemetryingest.AppendBatchRequest{
		BatchID: "batch-1",
		Events:  []telemetryingest.Event{{EventID: "evt-1"}},
	}
	var resp telemetryingest.AppendBatchResponse

	if err := rpc.AppendBatch(req, &resp); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if resp.Accepted != 0 {
		t.Fatalf("Accepted = %d, want 0", resp.Accepted)
	}
	if resp.Dropped != 1 {
		t.Fatalf("Dropped = %d, want 1", resp.Dropped)
	}
	if resp.Error == "" {
		t.Fatal("Error should be set")
	}
}

func TestTelemetryIngestRPCAppendBatchSuccess(t *testing.T) {
	dataDir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dataDir, "events.db"))
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}
	defer eventLog.Close()

	rpc := &TelemetryIngestRPC{daemon: &Daemon{eventLog: eventLog}}

	req := telemetryingest.AppendBatchRequest{
		BatchID: "batch-1",
		Events: []telemetryingest.Event{
			newTelemetryEvent("evt-1"),
			newTelemetryEvent("evt-2"),
		},
	}
	var resp telemetryingest.AppendBatchResponse

	if err := rpc.AppendBatch(req, &resp); err != nil {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if resp.Accepted != 2 {
		t.Fatalf("Accepted = %d, want 2", resp.Accepted)
	}
	if resp.Dropped != 0 {
		t.Fatalf("Dropped = %d, want 0", resp.Dropped)
	}
	if resp.HighWatermark == "" {
		t.Fatal("HighWatermark should be set")
	}
}

func TestTelemetryIngestRPCPing(t *testing.T) {
	rpc := &TelemetryIngestRPC{daemon: &Daemon{}}

	req := telemetryingest.PingRequest{}
	var resp telemetryingest.PingResponse

	if err := rpc.Ping(req, &resp); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if resp.Version != Version {
		t.Fatalf("Version = %q, want %q", resp.Version, Version)
	}
	if !resp.Healthy {
		t.Fatal("Healthy should be true")
	}
	if resp.EventCount != 0 {
		t.Fatalf("EventCount = %d, want 0", resp.EventCount)
	}
}

func TestTelemetryIngestRPCPingWithEvents(t *testing.T) {
	dataDir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dataDir, "events.db"))
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}
	defer eventLog.Close()

	accepted, dropped, err := eventLog.Append([]telemetryingest.Event{
		newTelemetryEvent("evt-1"),
		newTelemetryEvent("evt-2"),
	})
	if err != nil {
		t.Fatalf("eventLog.Append() error = %v", err)
	}
	if accepted != 2 || dropped != 0 {
		t.Fatalf("eventLog.Append() accepted=%d dropped=%d, want accepted=2 dropped=0", accepted, dropped)
	}

	rpc := &TelemetryIngestRPC{daemon: &Daemon{eventLog: eventLog}}

	req := telemetryingest.PingRequest{}
	var resp telemetryingest.PingResponse

	if err := rpc.Ping(req, &resp); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if resp.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", resp.EventCount)
	}
}

func TestGenerateHighWatermark(t *testing.T) {
	hw := generateHighWatermark()
	if hw == "" {
		t.Fatal("generateHighWatermark() returned empty string")
	}
	if len(hw) < 3 {
		t.Fatal("generateHighWatermark() returned too short string")
	}
}

func TestConnAdapterTelemetryd(t *testing.T) {
	// connAdapter is defined in ingest_rpc.go, test that it exists
	// and implements the right interface (via compilation)
	_ = &connAdapter{}
}
