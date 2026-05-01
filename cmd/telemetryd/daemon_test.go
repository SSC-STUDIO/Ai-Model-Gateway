package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/query"
)

func TestNewDaemonTelemetryd(t *testing.T) {
	dataDir := t.TempDir()
	d, err := NewDaemon(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}
	if d == nil {
		t.Fatal("NewDaemon() returned nil")
	}
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Fatal("NewDaemon() did not create data directory")
	}
}

func TestNewDaemonFailsOnInvalidDataDirTelemetryd(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := NewDaemon(Config{DataDir: filePath})
	if err == nil {
		t.Fatal("NewDaemon() should fail when data dir cannot be created")
	}
}

func TestDefaultSocketPathTelemetryd(t *testing.T) {
	path := defaultSocketPath("test-socket")
	if path == "" {
		t.Fatal("defaultSocketPath() returned empty string")
	}
}

func TestLoadConfigDefaultsTelemetryd(t *testing.T) {
	cfg := loadConfig("", "", "", "")
	if cfg.DataDir != "data/telemetry" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "data/telemetry")
	}
	if cfg.RetentionDays != 30 {
		t.Fatalf("RetentionDays = %d, want 30", cfg.RetentionDays)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoadConfigFromFileTelemetryd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := Config{
		DataDir:       "data/custom",
		RetentionDays: 60,
		LogLevel:      "debug",
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	loaded := loadConfig(configPath, "", "", "")
	if loaded.DataDir != "data/custom" {
		t.Fatalf("DataDir = %q, want %q", loaded.DataDir, "data/custom")
	}
	if loaded.RetentionDays != 60 {
		t.Fatalf("RetentionDays = %d, want 60", loaded.RetentionDays)
	}
}

func TestLoadConfigFromInvalidFileTelemetryd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(configPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg := loadConfig(configPath, "", "", "")
	if cfg.DataDir != "data/telemetry" {
		t.Fatalf("DataDir should default, got %q", cfg.DataDir)
	}
}

func TestLoadConfigSetsPaths(t *testing.T) {
	cfg := loadConfig("", "ingest.sock", "query.sock", "data/test")
	if cfg.IngestSocket != "ingest.sock" {
		t.Fatalf("IngestSocket = %q, want %q", cfg.IngestSocket, "ingest.sock")
	}
	if cfg.QuerySocket != "query.sock" {
		t.Fatalf("QuerySocket = %q, want %q", cfg.QuerySocket, "query.sock")
	}
	if cfg.DataDir != "data/test" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "data/test")
	}
	if cfg.EventLogPath != filepath.Join("data/test", "events.db") {
		t.Fatalf("EventLogPath = %q, want %q", cfg.EventLogPath, filepath.Join("data/test", "events.db"))
	}
}

func TestAppendEventsNilEventLog(t *testing.T) {
	d := &Daemon{}
	_, _, err := d.AppendEvents([]telemetryingest.Event{{EventID: "test"}})
	if err == nil {
		t.Fatal("AppendEvents() with nil event log should error")
	}
}

func TestGetEventCountNilEventLog(t *testing.T) {
	d := &Daemon{}
	count := d.GetEventCount()
	if count != 0 {
		t.Fatalf("GetEventCount() = %d, want 0", count)
	}
}

func TestShutdownTelemetryd(t *testing.T) {
	dataDir := t.TempDir()
	d, err := NewDaemon(Config{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestShutdownWithNilResources(t *testing.T) {
	d := &Daemon{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() with nil resources error = %v", err)
	}
}

func TestAppendEvents(t *testing.T) {
	dataDir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dataDir, "events.db"))
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}
	defer eventLog.Close()

	d := &Daemon{eventLog: eventLog}

	events := []telemetryingest.Event{
		newTelemetryEvent("evt-1"),
		newTelemetryEvent("evt-2"),
	}

	accepted, _, err := d.AppendEvents(events)
	if err != nil {
		t.Fatalf("AppendEvents() error = %v", err)
	}
	if accepted != 2 {
		t.Fatalf("accepted = %d, want 2", accepted)
	}
}

func TestDaemonWithEventLog(t *testing.T) {
	dataDir := t.TempDir()
	eventLogPath := filepath.Join(dataDir, "events.db")

	eventLog, err := eventlog.New(eventLogPath)
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}
	defer eventLog.Close()

	d := &Daemon{
		config:    Config{DataDir: dataDir},
		eventLog:  eventLog,
		startedAt: time.Now(),
	}

	// Test GetEventCount
	count := d.GetEventCount()
	if count != 0 {
		t.Fatalf("GetEventCount() = %d, want 0", count)
	}

	// Test AppendEvents
	events := []telemetryingest.Event{
		newTelemetryEvent("evt-1"),
		newTelemetryEvent("evt-2"),
	}
	accepted, _, err := d.AppendEvents(events)
	if err != nil {
		t.Fatalf("AppendEvents() error = %v", err)
	}
	if accepted != 2 {
		t.Fatalf("accepted = %d, want 2", accepted)
	}

	// Verify count
	count = d.GetEventCount()
	if count != 2 {
		t.Fatalf("GetEventCount() = %d, want 2", count)
	}

}

func TestShutdownWithEventLogAndQueryStore(t *testing.T) {
	dataDir := t.TempDir()
	eventLog, err := eventlog.New(filepath.Join(dataDir, "events.db"))
	if err != nil {
		t.Fatalf("eventlog.New() error = %v", err)
	}

	queryStore, err := query.NewStore(filepath.Join(dataDir, "query.db"))
	if err != nil {
		t.Fatalf("query.NewStore() error = %v", err)
	}

	d := &Daemon{
		config:     Config{DataDir: dataDir},
		eventLog:   eventLog,
		queryStore: queryStore,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
