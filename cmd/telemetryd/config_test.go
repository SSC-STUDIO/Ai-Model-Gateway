package main

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigAllowsDirectFlagOverrides(t *testing.T) {
	cfg := loadConfig("", "telemetry-ingest.sock", "telemetry-query.sock", "data/telemetryd")

	if cfg.IngestSocket != "telemetry-ingest.sock" {
		t.Fatalf("IngestSocket = %q, want %q", cfg.IngestSocket, "telemetry-ingest.sock")
	}
	if cfg.QuerySocket != "telemetry-query.sock" {
		t.Fatalf("QuerySocket = %q, want %q", cfg.QuerySocket, "telemetry-query.sock")
	}
	if cfg.DataDir != "data/telemetryd" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "data/telemetryd")
	}
	if got, want := filepath.ToSlash(cfg.EventLogPath), "data/telemetryd/events.db"; got != want {
		t.Fatalf("EventLogPath = %q, want %q", got, want)
	}
	if got, want := filepath.ToSlash(cfg.QueryStorePath), "data/telemetryd/query.db"; got != want {
		t.Fatalf("QueryStorePath = %q, want %q", got, want)
	}
}
