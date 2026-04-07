package configloader

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func writeTestConfig(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.v2.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	return path
}

const minimalConfig = `
server:
  listen: ":18080"
admin:
  enabled: false
providers:
  - name: test
    base_url: https://api.test.com
    api_key: sk-test
    models: [gpt-4o]
`

func TestWatcher_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, minimalConfig)

	w, err := NewWatcher(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher error: %v", err)
	}
	defer w.Stop()

	cfg := w.Config()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Server.Listen != ":18080" {
		t.Errorf("expected listen :18080, got %s", cfg.Server.Listen)
	}
}

func TestWatcher_DetectsChange(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, minimalConfig)

	w, err := NewWatcher(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher error: %v", err)
	}
	defer w.Stop()

	changed := make(chan *core.Config, 1)
	w.OnChange(func(cfg *core.Config) {
		changed <- cfg
	})

	w.Start()

	// Modify the config file.
	newConfig := `
server:
  listen: ":19090"
admin:
  enabled: false
providers:
  - name: test
    base_url: https://api.test.com
    api_key: sk-test
    models: [gpt-4o]
`
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(path, []byte(newConfig), 0o644)

	select {
	case cfg := <-changed:
		if cfg.Server.Listen != ":19090" {
			t.Errorf("expected listen :19090, got %s", cfg.Server.Listen)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for config change notification")
	}

	// Verify Config() also returns the new config.
	if w.Config().Server.Listen != ":19090" {
		t.Errorf("Config() should return updated config")
	}
}

func TestWatcher_IgnoresSameContent(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, minimalConfig)

	w, err := NewWatcher(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher error: %v", err)
	}
	defer w.Stop()

	callCount := 0
	w.OnChange(func(cfg *core.Config) {
		callCount++
	})

	w.Start()

	// Rewrite file with same content.
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(path, []byte(minimalConfig), 0o644)
	time.Sleep(200 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("expected 0 change callbacks for same content, got %d", callCount)
	}
}

func TestWatcher_InvalidConfigKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, minimalConfig)

	w, err := NewWatcher(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewWatcher error: %v", err)
	}
	defer w.Stop()

	w.Start()

	// Write invalid config.
	time.Sleep(100 * time.Millisecond)
	os.WriteFile(path, []byte("not: valid: yaml: {{"), 0o644)
	time.Sleep(200 * time.Millisecond)

	// Should still have the original config.
	cfg := w.Config()
	if cfg.Server.Listen != ":18080" {
		t.Errorf("expected old config preserved, got listen=%s", cfg.Server.Listen)
	}
}
