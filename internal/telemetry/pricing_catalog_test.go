package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ai-model-gateway/internal/config"
)

func TestPricingCatalogUpdateConfigLoadsNewCachePath(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "pricing-cache.json")
	snapshot := PricingCatalogSnapshot{
		SourceURL: "https://example.com/pricing",
		Catalog: map[string]Pricing{
			"custom-model": {
				InputPer1MUsd:  1.25,
				OutputPer1MUsd: 9.75,
			},
		},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal pricing snapshot: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatalf("write pricing cache: %v", err)
	}

	catalog := NewPricingCatalog(config.PricingConfig{})
	if _, ok := catalog.Snapshot().Catalog["custom-model"]; ok {
		t.Fatal("expected custom model to be absent before config update")
	}

	catalog.UpdateConfig(config.PricingConfig{
		CachePath:            cachePath,
		RefreshIntervalHours: 1,
		RequestTimeoutMs:     5000,
	})

	if got := catalog.currentConfig().CachePath; got != cachePath {
		t.Fatalf("expected cache path %q, got %q", cachePath, got)
	}
	if got := catalog.currentConfig().RefreshIntervalHours; got != 1 {
		t.Fatalf("expected refresh interval 1, got %d", got)
	}

	pricing, ok := catalog.Snapshot().Catalog["custom-model"]
	if !ok {
		t.Fatal("expected updated cache catalog to be loaded")
	}
	if pricing.InputPer1MUsd != 1.25 || pricing.OutputPer1MUsd != 9.75 {
		t.Fatalf("unexpected loaded pricing: %+v", pricing)
	}
	if got := catalog.Snapshot().LastError; got != "" {
		t.Fatalf("expected no cache load error, got %q", got)
	}
}
