package configloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_MinimalValid(t *testing.T) {
	yaml := `
providers:
  - name: test
    base_url: https://example.com
    api_key: sk-test
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "test" {
		t.Errorf("expected provider name 'test', got %q", cfg.Providers[0].Name)
	}
	// Defaults applied
	if cfg.Server.Listen != ":18080" {
		t.Errorf("expected default listen :18080, got %s", cfg.Server.Listen)
	}
	if cfg.Routing.Strategy != "health_weighted_rr" {
		t.Errorf("expected default strategy, got %s", cfg.Routing.Strategy)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	_, err := Parse([]byte(`{{{`))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParse_NoProviders(t *testing.T) {
	_, err := Parse([]byte(`server: {listen: ":9090"}`))
	if err == nil {
		t.Error("expected validation error for no providers")
	}
}

func TestParse_ProviderMissingName(t *testing.T) {
	yaml := `
providers:
  - base_url: https://example.com
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Error("expected validation error for missing provider name")
	}
}

func TestLoadFromReader_Valid(t *testing.T) {
	yaml := `
providers:
  - name: r
    base_url: https://example.com
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Providers[0].Name != "r" {
		t.Errorf("expected provider name 'r', got %q", cfg.Providers[0].Name)
	}
}

func TestParse_FullConfig(t *testing.T) {
	yaml := `
server:
  listen: ":9090"
  read_timeout_ms: 5000
admin:
  enabled: false
  language: en
routing:
  strategy: round_robin
  max_retries: 3
  retry_backoff:
    initial_ms: 1000
    max_ms: 10000
  health:
    enabled: true
    interval_sec: 30
  sticky_sessions:
    enabled: false
    ttl_sec: 900
  failure_policy:
    threshold: 10
    cooldown_sec: 30
providers:
  - name: openai
    base_url: https://api.openai.com
    api_key: sk-test
    provider_class: quota_limited
    models:
      - gpt-4o
    weight: 2
    timeout_ms: 60000
    same_retries: 1
    enabled: true
telemetry:
  sqlite_path: data/test.db
  retention_days: 7
compat:
  bridge:
    enabled: true
    rules:
      - from: "gpt-4"
        to: "gpt-4o"
  fallback:
    enabled: true
    models:
      gpt-4o: gpt-4o-mini
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Server.Listen)
	}
	if cfg.Routing.Strategy != "round_robin" {
		t.Errorf("expected strategy round_robin, got %s", cfg.Routing.Strategy)
	}
	if cfg.Routing.MaxRetries != 3 {
		t.Errorf("expected max_retries 3, got %d", cfg.Routing.MaxRetries)
	}
	if !cfg.Compat.Bridge.Enabled {
		t.Error("expected bridge enabled")
	}
	if len(cfg.Compat.Bridge.Rules) != 1 {
		t.Errorf("expected 1 bridge rule, got %d", len(cfg.Compat.Bridge.Rules))
	}
	if !cfg.Compat.Fallback.Enabled {
		t.Error("expected fallback enabled")
	}
	if cfg.Compat.Fallback.Models["gpt-4o"] != "gpt-4o-mini" {
		t.Errorf("expected fallback gpt-4o -> gpt-4o-mini")
	}
	if cfg.Telemetry.RetentionDays != 7 {
		t.Errorf("expected retention_days 7, got %d", cfg.Telemetry.RetentionDays)
	}
}

func TestLoadFromFile_ResolvesRelativePricingCachePath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := `
providers:
  - name: local
    base_url: https://example.com
telemetry:
  sqlite_path: data/telemetry.db
pricing:
  cache_path: cache/pricing.json
`
	if err := os.WriteFile(configPath, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error: %v", err)
	}

	wantTelemetry := filepath.Join(dir, "data", "telemetry.db")
	if cfg.Telemetry.SQLitePath != wantTelemetry {
		t.Fatalf("expected telemetry.sqlite_path %q, got %q", wantTelemetry, cfg.Telemetry.SQLitePath)
	}

	wantPricing := filepath.Join(dir, "cache", "pricing.json")
	if cfg.Pricing.CachePath != wantPricing {
		t.Fatalf("expected pricing.cache_path %q, got %q", wantPricing, cfg.Pricing.CachePath)
	}
}

func TestLoadFromFile_ExampleConfigUsesCutoverSafePaths(t *testing.T) {
	configPath := filepath.Join("..", "..", "..", "configs", "config.example.yaml")

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile(example config) error: %v", err)
	}

	baseDir := filepath.Clean(filepath.Join("..", "..", "..", "configs"))
	wantTelemetry := filepath.Join(baseDir, "data", "telemetry.db")
	if cfg.Telemetry.SQLitePath != wantTelemetry {
		t.Fatalf("expected example telemetry path %q, got %q", wantTelemetry, cfg.Telemetry.SQLitePath)
	}

	wantPricing := filepath.Join(baseDir, "data", "pricing-cache.json")
	if cfg.Pricing.CachePath != wantPricing {
		t.Fatalf("expected example pricing path %q, got %q", wantPricing, cfg.Pricing.CachePath)
	}
}
