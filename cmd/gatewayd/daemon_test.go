package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestNewDaemon(t *testing.T) {
	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}
	if d == nil {
		t.Fatal("NewDaemon() returned nil")
	}
	if d.runtime == nil {
		t.Fatal("NewDaemon() runtime is nil")
	}
}

func TestDefaultSocketPathGatewayd(t *testing.T) {
	path := defaultSocketPath("test-socket")
	if path == "" {
		t.Fatal("defaultSocketPath() returned empty string")
	}
}

func TestLoadConfigFromFileGatewayd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cfg := Config{
		Listen:          "127.0.0.1:28080",
		DataDir:         "data/custom",
		LogLevel:        "debug",
		ReadTimeoutSec:  10,
		WriteTimeoutSec: 20,
		IdleTimeoutSec:  30,
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	loaded := loadConfig(configPath, "", "", "", "")
	if loaded.Listen != "127.0.0.1:28080" {
		t.Fatalf("Listen = %q, want %q", loaded.Listen, "127.0.0.1:28080")
	}
	if loaded.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want %q", loaded.LogLevel, "debug")
	}
}

func TestLoadConfigFromInvalidFileGatewayd(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(configPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg := loadConfig(configPath, "", "", "", "")
	if cfg.Listen != "127.0.0.1:18080" {
		t.Fatalf("Listen should default to 127.0.0.1:18080, got %q", cfg.Listen)
	}
}

func TestLoadConfigDefaultsGatewayd(t *testing.T) {
	cfg := loadConfig("", "", "", "", "")
	if cfg.Listen != "127.0.0.1:18080" {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, "127.0.0.1:18080")
	}
	if cfg.DataDir != "data" {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, "data")
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestValidateSnapshot(t *testing.T) {
	cases := []struct {
		name    string
		snap    *snapshot.Snapshot
		wantErr bool
	}{
		{
			name: "missing snapshot_id",
			snap: &snapshot.Snapshot{
				Meta: snapshot.SnapshotMeta{
					SchemaVersion: snapshot.CurrentSchemaVersion,
				},
				Ingress:   snapshot.IngressConfig{Listen: ":8080"},
				Providers: []snapshot.ProviderSnapshot{{ProviderID: "test"}},
			},
			wantErr: true,
		},
		{
			name: "wrong schema version",
			snap: &snapshot.Snapshot{
				Meta: snapshot.SnapshotMeta{
					SnapshotID:    "snap-1",
					SchemaVersion: 999,
				},
				Ingress:   snapshot.IngressConfig{Listen: ":8080"},
				Providers: []snapshot.ProviderSnapshot{{ProviderID: "test"}},
			},
			wantErr: true,
		},
		{
			name: "missing ingress listen",
			snap: &snapshot.Snapshot{
				Meta: snapshot.SnapshotMeta{
					SnapshotID:    "snap-1",
					SchemaVersion: snapshot.CurrentSchemaVersion,
				},
				Ingress:   snapshot.IngressConfig{},
				Providers: []snapshot.ProviderSnapshot{{ProviderID: "test"}},
			},
			wantErr: true,
		},
		{
			name: "no providers",
			snap: &snapshot.Snapshot{
				Meta: snapshot.SnapshotMeta{
					SnapshotID:    "snap-1",
					SchemaVersion: snapshot.CurrentSchemaVersion,
				},
				Ingress:   snapshot.IngressConfig{Listen: ":8080"},
				Providers: []snapshot.ProviderSnapshot{},
			},
			wantErr: true,
		},
		{
			name: "valid snapshot",
			snap: &snapshot.Snapshot{
				Meta: snapshot.SnapshotMeta{
					SnapshotID:    "snap-1",
					SchemaVersion: snapshot.CurrentSchemaVersion,
				},
				Ingress:   snapshot.IngressConfig{Listen: ":8080"},
				Providers: []snapshot.ProviderSnapshot{{ProviderID: "test"}},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSnapshot(tc.snap)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestModelsHandlerNoSnapshot(t *testing.T) {
	d := &Daemon{
		config:    Config{Listen: "127.0.0.1:18080"},
		startedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	d.modelsHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("modelsHandler() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestChatCompletionsHandlerNoSnapshot(t *testing.T) {
	d := &Daemon{
		config:    Config{Listen: "127.0.0.1:18080"},
		startedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	d.chatCompletionsHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("chatCompletionsHandler() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestMessagesHandlerNoSnapshot(t *testing.T) {
	d := &Daemon{
		config:    Config{Listen: "127.0.0.1:18080"},
		startedAt: time.Now(),
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	rec := httptest.NewRecorder()
	d.messagesHandler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("messagesHandler() status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestShutdownGatewayd(t *testing.T) {
	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestGetStatusWithoutSnapshot(t *testing.T) {
	d := &Daemon{
		config:    Config{Listen: "127.0.0.1:18080"},
		startedAt: time.Now(),
	}

	status := d.GetStatus()
	if status.Readiness != gatewaycontrol.ReadinessStarting {
		t.Fatalf("Readiness = %v, want %v", status.Readiness, gatewaycontrol.ReadinessStarting)
	}
	if status.Listener != "127.0.0.1:18080" {
		t.Fatalf("Listener = %q, want %q", status.Listener, "127.0.0.1:18080")
	}
	if status.ActiveSnapshotID != "" {
		t.Fatalf("ActiveSnapshotID = %q, want empty", status.ActiveSnapshotID)
	}
}

func TestApplySnapshotInvalid(t *testing.T) {
	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}

	// Test with invalid snapshot
	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "",
			SchemaVersion: snapshot.CurrentSchemaVersion,
		},
	}

	err = d.ApplySnapshot(snap)
	if err == nil {
		t.Fatal("ApplySnapshot() with invalid snapshot should error")
	}
}
