package pricing

import (
	"context"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestNewCatalog(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "/tmp/pricing.json",
		RefreshIntervalHours: 24,
	}
	catalog := NewCatalog(cfg)
	if catalog == nil {
		t.Fatal("NewCatalog returned nil")
	}
}

func TestNewCatalogEmptyConfig(t *testing.T) {
	catalog := NewCatalog(core.PricingConfig{})
	if catalog == nil {
		t.Fatal("NewCatalog returned nil for empty config")
	}
}

func TestCatalogSnapshot(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "/tmp/pricing.json",
		RefreshIntervalHours: 24,
	}
	catalog := NewCatalog(cfg)

	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Snapshot catalog should not be nil")
	}
}

func TestCatalogStartNil(t *testing.T) {
	var catalog *Catalog
	catalog.Start(context.Background())
}

func TestCatalogUpdateConfigNil(t *testing.T) {
	var catalog *Catalog
	catalog.UpdateConfig(core.PricingConfig{})
}

func TestCatalogSnapshotNil(t *testing.T) {
	var catalog *Catalog
	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Snapshot from nil catalog should return bootstrap snapshot")
	}
}

func TestPriceFields(t *testing.T) {
	price := Price{
		Currency:            "USD",
		InputPer1M:          1.0,
		CachedInputPer1M:    0.5,
		OutputPer1M:         2.0,
		InputPer1MUsd:       1.0,
		CachedInputPer1MUsd: 0.5,
		OutputPer1MUsd:      2.0,
		Source:              "test",
	}

	if price.Currency != "USD" {
		t.Errorf("expected USD, got %s", price.Currency)
	}
	if price.InputPer1M != 1.0 {
		t.Errorf("expected 1.0, got %f", price.InputPer1M)
	}
}

func TestSnapshotFields(t *testing.T) {
	now := time.Now()
	snapshot := Snapshot{
		SourceURL:     "https://example.com/pricing.json",
		UpdatedAt:     now,
		LastAttemptAt: now,
		LastError:     "",
		Catalog:       make(map[string]Price),
	}

	if snapshot.SourceURL != "https://example.com/pricing.json" {
		t.Errorf("unexpected source URL: %s", snapshot.SourceURL)
	}
}

func TestCatalogStartAndSnapshot(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "/tmp/pricing.json",
		RefreshIntervalHours: 24,
		RequestTimeoutMs:     5000,
	}

	catalog := NewCatalog(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	catalog.Start(ctx)

	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Catalog should not be nil after start")
	}
}

func TestCatalogUpdateConfig(t *testing.T) {
	cfg1 := core.PricingConfig{
		CachePath:            "/tmp/pricing1.json",
		RefreshIntervalHours: 12,
	}
	catalog := NewCatalog(cfg1)

	cfg2 := core.PricingConfig{
		CachePath:            "/tmp/pricing2.json",
		RefreshIntervalHours: 24,
	}
	catalog.UpdateConfig(cfg2)

	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Catalog should still be valid after config update")
	}
}

func TestCatalogWithManualPrices(t *testing.T) {
	enabled := true
	cfg := core.PricingConfig{
		CachePath:            "/tmp/pricing.json",
		RefreshIntervalHours: 24,
		ManualPrices: []core.PricingManualPrice{
			{
				Model:       "gpt-4",
				InputPer1M:  30.0,
				OutputPer1M: 60.0,
				Enabled:     &enabled,
			},
		},
	}

	catalog := NewCatalog(cfg)
	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Catalog should not be nil with manual prices")
	}
}
