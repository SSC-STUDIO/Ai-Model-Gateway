package main

import (
	"testing"

	"ai-model-gateway/internal/contracts/telemetryquery"
)

func TestNewQueryRPCServer(t *testing.T) {
	d := &Daemon{}
	server := NewQueryRPCServer(d)
	if server == nil {
		t.Fatal("NewQueryRPCServer() returned nil")
	}
}

func TestTelemetryQueryRPCGetOverviewNilStore(t *testing.T) {
	rpc := &TelemetryQueryRPC{daemon: &Daemon{}}

	req := telemetryquery.OverviewRequest{}
	var resp telemetryquery.OverviewResponse

	err := rpc.GetOverview(req, &resp)
	if err == nil {
		t.Fatal("GetOverview() with nil store should error")
	}
}

func TestTelemetryQueryRPCGetTelemetryNilStore(t *testing.T) {
	rpc := &TelemetryQueryRPC{daemon: &Daemon{}}

	req := telemetryquery.TelemetryRequest{}
	var resp telemetryquery.TelemetryResponse

	err := rpc.GetTelemetry(req, &resp)
	if err == nil {
		t.Fatal("GetTelemetry() with nil store should error")
	}
}

func TestTelemetryQueryRPCGetTimeSeriesNilStore(t *testing.T) {
	rpc := &TelemetryQueryRPC{daemon: &Daemon{}}

	req := telemetryquery.TimeSeriesRequest{}
	var resp telemetryquery.TimeSeriesResponse

	err := rpc.GetTimeSeries(req, &resp)
	if err == nil {
		t.Fatal("GetTimeSeries() with nil store should error")
	}
}

func TestTelemetryQueryRPCGetModelBenchmarkNilStore(t *testing.T) {
	rpc := &TelemetryQueryRPC{daemon: &Daemon{}}

	req := telemetryquery.BenchmarkRequest{}
	var resp telemetryquery.BenchmarkResponse

	err := rpc.GetModelBenchmark(req, &resp)
	if err == nil {
		t.Fatal("GetModelBenchmark() with nil store should error")
	}
}

func TestTelemetryQueryRPCPing(t *testing.T) {
	rpc := &TelemetryQueryRPC{daemon: &Daemon{}}

	req := telemetryquery.PingRequest{}
	var resp telemetryquery.PingResponse

	if err := rpc.Ping(req, &resp); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if resp.Version != Version {
		t.Fatalf("Version = %q, want %q", resp.Version, Version)
	}
	if !resp.Healthy {
		t.Fatal("Healthy should be true")
	}
}

func TestConnAdapterQuery(t *testing.T) {
	// connAdapter is also defined in query_rpc.go
	_ = &connAdapter{}
}
