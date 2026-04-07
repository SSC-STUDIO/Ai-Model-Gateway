package configstate

import (
	"os"
	"path/filepath"
	"testing"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"

	"gopkg.in/yaml.v3"
)

func TestStoreSaveCreatesHistoryAndKeepsV2Format(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.v2.yaml")

	initial := testConfig("alpha", ":18080")
	writeV2ConfigFile(t, path, initial)

	store, err := New(path, &initial)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	updated := testConfig("beta", ":19090")
	if err := store.Save(&updated); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := configloader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load saved v2 config: %v", err)
	}
	if len(loaded.Providers) != 1 || loaded.Providers[0].Name != "beta" {
		t.Fatalf("unexpected saved providers: %+v", loaded.Providers)
	}
	if loaded.Server.Listen != ":19090" {
		t.Fatalf("unexpected saved listen: %s", loaded.Server.Listen)
	}

	versions, err := store.ListVersions()
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected config history version after save")
	}

	currentBytes, err := store.ReadCurrentFile()
	if err != nil {
		t.Fatalf("read current file: %v", err)
	}
	if len(currentBytes) == 0 {
		t.Fatal("expected non-empty current config bytes")
	}
}

func TestStoreRollbackVersionRestoresPreviousConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.v2.yaml")

	initial := testConfig("alpha", ":18080")
	writeV2ConfigFile(t, path, initial)

	store, err := New(path, &initial)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	updated := testConfig("beta", ":19090")
	if err := store.Save(&updated); err != nil {
		t.Fatalf("save config: %v", err)
	}

	versions, err := store.ListVersions()
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected at least one version for rollback")
	}

	restored, err := store.RollbackVersion(versions[0].ID)
	if err != nil {
		t.Fatalf("rollback version: %v", err)
	}
	if len(restored.Providers) != 1 || restored.Providers[0].Name != "alpha" {
		t.Fatalf("unexpected restored providers: %+v", restored.Providers)
	}
	if restored.Server.Listen != ":18080" {
		t.Fatalf("unexpected restored listen: %s", restored.Server.Listen)
	}

	loaded, err := configloader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load rolled back config: %v", err)
	}
	if len(loaded.Providers) != 1 || loaded.Providers[0].Name != "alpha" {
		t.Fatalf("unexpected loaded providers after rollback: %+v", loaded.Providers)
	}
}

func testConfig(providerName string, listen string) core.Config {
	return core.Config{
		Server: core.ServerConfig{
			Listen: listen,
		},
		Admin: core.AdminConfig{
			Enabled: false,
		},
		Routing: core.RoutingConfig{},
		Providers: []core.Provider{
			{
				Name:          providerName,
				BaseURL:       "https://example.com",
				APIKey:        "sk-test",
				Models:        []string{"gpt-5.2"},
				Weight:        1,
				ProviderClass: core.ProviderClassQuotaLimited,
			},
		},
		Telemetry: core.TelemetryConfig{
			SQLitePath: filepath.Join("data", "telemetry.db"),
		},
		Pricing: core.PricingConfig{
			CachePath:            filepath.Join("data", "pricing-cache.json"),
			RefreshIntervalHours: 12,
			RequestTimeoutMs:     15000,
		},
		Compat: core.CompatConfig{
			Fallback: core.FallbackConfig{Models: map[string]string{}},
		},
	}
}

func writeV2ConfigFile(t *testing.T, path string, cfg core.Config) {
	t.Helper()
	cfg.Normalize()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}
