package main

import "testing"

func TestLoadConfigAllowsDirectFlagOverrides(t *testing.T) {
	cfg := loadConfig("", "127.0.0.1:19080", "gateway-control.sock", "telemetry-ingest.sock", "data/gatewayd")

	if cfg.Listen != "127.0.0.1:19080" {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:19080")
	}
	if cfg.ControlSocket != "gateway-control.sock" {
		t.Fatalf("ControlSocket = %q, want %q", cfg.ControlSocket, "gateway-control.sock")
	}
	if cfg.TelemetrySocket != "telemetry-ingest.sock" {
		t.Fatalf("TelemetrySocket = %q, want %q", cfg.TelemetrySocket, "telemetry-ingest.sock")
	}
	if cfg.DataDir != "data/gatewayd" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "data/gatewayd")
	}
}
