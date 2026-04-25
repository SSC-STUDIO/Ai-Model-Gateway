package telemetry

import (
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestPricingCatalogSnapshot(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "data/pricing-cache.json",
		RefreshIntervalHours: 12,
	}

	catalog := NewPricingCatalog(cfg)
	if catalog == nil {
		t.Fatal("expected non-nil catalog")
	}

	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("expected non-nil catalog in snapshot")
	}
}

func TestPricingCatalogUpdateConfig(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "data/pricing-cache.json",
		RefreshIntervalHours: 12,
	}

	catalog := NewPricingCatalog(cfg)

	newCfg := core.PricingConfig{
		CachePath:            "data/pricing-cache-new.json",
		RefreshIntervalHours: 24,
	}

	catalog.UpdateConfig(newCfg)

	current := catalog.currentConfig()
	if current.RefreshIntervalHours != 24 {
		t.Errorf("expected refresh interval 24, got %d", current.RefreshIntervalHours)
	}
}

func TestPricingCatalogNilSafety(t *testing.T) {
	var catalog *PricingCatalog

	// These should not panic
	catalog.Start(nil)
	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("expected bootstrap catalog for nil catalog")
	}
	catalog.UpdateConfig(core.PricingConfig{})
}

func TestPricingStruct(t *testing.T) {
	p := Pricing{
		InputPer1M:       1.5,
		CachedInputPer1M: 0.75,
		OutputPer1M:      6.0,
		Currency:         "USD",
	}

	if p.InputPer1M != 1.5 {
		t.Errorf("expected InputPer1M 1.5, got %f", p.InputPer1M)
	}
	if p.CachedInputPer1M != 0.75 {
		t.Errorf("expected CachedInputPer1M 0.75, got %f", p.CachedInputPer1M)
	}
	if p.OutputPer1M != 6.0 {
		t.Errorf("expected OutputPer1M 6.0, got %f", p.OutputPer1M)
	}
}

func TestPricingCatalogSnapshotFields(t *testing.T) {
	now := time.Now().UTC()
	snapshot := PricingCatalogSnapshot{
		SourceURL:     "https://example.com/pricing",
		UpdatedAt:     now,
		LastAttemptAt: now,
		LastError:     "",
		Catalog:       make(map[string]Pricing),
	}

	if snapshot.SourceURL != "https://example.com/pricing" {
		t.Errorf("unexpected source URL: %s", snapshot.SourceURL)
	}
	if len(snapshot.Catalog) != 0 {
		t.Error("expected empty catalog")
	}
}

func TestApplyPricingConfigToSnapshotSkipsDisabledAndRemovedSourceCatalogs(t *testing.T) {
	enabled := true
	disabled := false
	snapshot := PricingCatalogSnapshot{
		SourceCatalogs: map[string]map[string]Pricing{
			"enabled-source": {
				"enabled-test-model": {Currency: "USD", InputPer1M: 1, OutputPer1M: 2},
			},
			"disabled-source": {
				"disabled-test-model": {Currency: "USD", InputPer1M: 3, OutputPer1M: 4},
			},
			"removed-source": {
				"removed-test-model": {Currency: "USD", InputPer1M: 5, OutputPer1M: 6},
			},
		},
	}
	cfg := core.PricingConfig{
		Sources: []core.PricingSourceConfig{
			{ID: "enabled-source", Vendor: "openai", Enabled: &enabled},
			{ID: "disabled-source", Vendor: "anthropic", Enabled: &disabled},
		},
	}

	updated := applyPricingConfigToSnapshot(snapshot, cfg)
	if _, ok := updated.Catalog["enabled-test-model"]; !ok {
		t.Fatal("expected enabled source pricing to remain active")
	}
	if _, ok := updated.Catalog["disabled-test-model"]; ok {
		t.Fatal("disabled source pricing should not be merged into active catalog")
	}
	if _, ok := updated.Catalog["removed-test-model"]; ok {
		t.Fatal("removed source pricing should not be merged into active catalog")
	}
	if _, ok := updated.SourceCatalogs["removed-source"]; ok {
		t.Fatal("removed source catalog should be pruned from snapshot")
	}
	if _, ok := updated.SourceCatalogs["disabled-source"]; !ok {
		t.Fatal("disabled source catalog should remain cached for state tracking")
	}
}
