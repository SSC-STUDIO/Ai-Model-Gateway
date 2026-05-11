package pricing

import (
	"context"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/telemetry"
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

func TestCatalogRefreshNowNilGuard(t *testing.T) {
	var catalog *Catalog
	if err := catalog.RefreshNow(context.Background()); err != nil {
		t.Errorf("RefreshNow on nil catalog should return nil, got %v", err)
	}
}

func TestCatalogRefreshNow(t *testing.T) {
	cfg := core.PricingConfig{
		CachePath:            "/tmp/pricing-refresh.json",
		RefreshIntervalHours: 24,
		RequestTimeoutMs:     1000,
	}
	catalog := NewCatalog(cfg)
	// RefreshNow may fail because it tries to fetch remote data,
	// but it should not panic.
	_ = catalog.RefreshNow(context.Background())

	snapshot := catalog.Snapshot()
	if snapshot.Catalog == nil {
		t.Error("Catalog should not be nil after RefreshNow")
	}
}

func TestFromLegacySnapshotWithFullData(t *testing.T) {
	now := time.Now()
	input := telemetry.PricingCatalogSnapshot{
		SourceURL:     "https://example.com/pricing.json",
		UpdatedAt:     now,
		LastAttemptAt: now,
		LastError:     "some error",
		Catalog: map[string]telemetry.Pricing{
			"model-a": {
				Currency:            "USD",
				InputPer1M:          10.0,
				CachedInputPer1M:    5.0,
				OutputPer1M:         20.0,
				InputPer1MUsd:       10.0,
				CachedInputPer1MUsd: 5.0,
				OutputPer1MUsd:      20.0,
				Source:              "official",
				SourceID:            "openai",
				FXRateToUSD:         1.0,
			},
		},
		Sources: []telemetry.PricingSourceState{
			{
				ID:         "openai",
				Vendor:     "OpenAI",
				URL:        "https://openai.com/pricing",
				Enabled:    true,
				Status:     "ok",
				UpdatedAt:  now,
				ModelCount: 10,
			},
			{
				ID:         "anthropic",
				Vendor:     "Anthropic",
				Enabled:    false,
				Status:     "disabled",
				LastError:  "source disabled",
			},
		},
		FX: telemetry.PricingFXSnapshot{
			Enabled:      true,
			SourceURL:    "https://fx.example.com/rates",
			BaseCurrency: "USD",
			UpdatedAt:    now,
			RatesToUSD: map[string]float64{
				"EUR": 0.92,
				"GBP": 0.79,
				"JPY": 150.0,
			},
		},
		SourceCatalogs: map[string]map[string]telemetry.Pricing{
			"openai": {
				"gpt-4": {
					Currency:   "USD",
					InputPer1M: 30.0,
					OutputPer1M: 60.0,
					Source:     "openai",
					SourceID:   "openai",
				},
			},
			"anthropic": {
				"claude-opus-4-6[1m]": {
					Currency:   "USD",
					InputPer1M: 15.0,
					OutputPer1M: 75.0,
					Source:     "anthropic",
					SourceID:   "anthropic",
				},
			},
		},
	}

	snapshot := fromLegacySnapshot(input)

	// Basic fields
	if snapshot.SourceURL != "https://example.com/pricing.json" {
		t.Errorf("expected source URL, got %s", snapshot.SourceURL)
	}
	if snapshot.LastError != "some error" {
		t.Errorf("expected 'some error', got %s", snapshot.LastError)
	}
	if !snapshot.UpdatedAt.Equal(now) || !snapshot.LastAttemptAt.Equal(now) {
		t.Error("timestamps should match input")
	}

	// Catalog mapping
	price, ok := snapshot.Catalog["model-a"]
	if !ok {
		t.Fatal("expected model-a in catalog")
	}
	if price.Currency != "USD" || price.InputPer1M != 10.0 || price.OutputPer1M != 20.0 {
		t.Errorf("unexpected price fields: %+v", price)
	}
	if price.CachedInputPer1M != 5.0 || price.CachedInputPer1MUsd != 5.0 {
		t.Errorf("unexpected cached price: %+v", price)
	}
	if price.Source != "official" || price.SourceID != "openai" || price.FXRateToUSD != 1.0 {
		t.Errorf("unexpected price metadata: %+v", price)
	}

	// Sources
	if len(snapshot.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(snapshot.Sources))
	}
	if snapshot.Sources[0].ID != "openai" || snapshot.Sources[0].Vendor != "OpenAI" {
		t.Errorf("unexpected first source: %+v", snapshot.Sources[0])
	}
	if snapshot.Sources[0].ModelCount != 10 {
		t.Errorf("expected ModelCount 10, got %d", snapshot.Sources[0].ModelCount)
	}
	if snapshot.Sources[1].ID != "anthropic" || snapshot.Sources[1].Enabled {
		t.Errorf("unexpected second source: %+v", snapshot.Sources[1])
	}

	// FX
	if !snapshot.FX.Enabled || snapshot.FX.BaseCurrency != "USD" {
		t.Errorf("unexpected FX: %+v", snapshot.FX)
	}
	if len(snapshot.FX.RatesToUSD) != 3 {
		t.Fatalf("expected 3 FX rates, got %d", len(snapshot.FX.RatesToUSD))
	}
	if snapshot.FX.RatesToUSD["EUR"] != 0.92 || snapshot.FX.RatesToUSD["JPY"] != 150.0 {
		t.Errorf("unexpected FX rates: %+v", snapshot.FX.RatesToUSD)
	}

	// SourceCatalogs
	if len(snapshot.SourceCatalogs) != 2 {
		t.Fatalf("expected 2 source catalogs, got %d", len(snapshot.SourceCatalogs))
	}
	oaiPrice, ok := snapshot.SourceCatalogs["openai"]["gpt-4"]
	if !ok || oaiPrice.InputPer1M != 30.0 || oaiPrice.OutputPer1M != 60.0 {
		t.Errorf("unexpected openai source catalog: %+v", snapshot.SourceCatalogs["openai"])
	}
	antPrice, ok := snapshot.SourceCatalogs["anthropic"]["claude-opus-4-6[1m]"]
	if !ok || antPrice.InputPer1M != 15.0 || antPrice.OutputPer1M != 75.0 {
		t.Errorf("unexpected anthropic source catalog: %+v", snapshot.SourceCatalogs["anthropic"])
	}
}

func TestFromLegacySnapshotEmptyData(t *testing.T) {
	input := telemetry.PricingCatalogSnapshot{}
	snapshot := fromLegacySnapshot(input)

	if snapshot.Catalog == nil {
		t.Error("Catalog should be non-nil even for empty input")
	}
	if len(snapshot.Catalog) != 0 {
		t.Errorf("expected empty catalog, got %d entries", len(snapshot.Catalog))
	}
	if len(snapshot.Sources) != 0 {
		t.Errorf("expected empty sources, got %v", snapshot.Sources)
	}
	if snapshot.SourceCatalogs == nil {
		t.Error("SourceCatalogs should be non-nil even for empty input")
	}
	if snapshot.FX.RatesToUSD != nil {
		t.Errorf("expected nil rates for empty FX, got %v", snapshot.FX.RatesToUSD)
	}
}

func TestCloneRatesNonEmpty(t *testing.T) {
	src := map[string]float64{
		"EUR": 0.92,
		"GBP": 0.79,
	}
	cloned := cloneRates(src)
	if len(cloned) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cloned))
	}
	if cloned["EUR"] != 0.92 || cloned["GBP"] != 0.79 {
		t.Errorf("unexpected cloned values: %+v", cloned)
	}
	// Mutation independence
	cloned["EUR"] = 1.0
	if src["EUR"] != 0.92 {
		t.Error("modifying clone should not affect source")
	}
}

func TestCloneRatesNil(t *testing.T) {
	if result := cloneRates(nil); result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestCloneRatesEmpty(t *testing.T) {
	if result := cloneRates(map[string]float64{}); result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestSourceStateFields(t *testing.T) {
	now := time.Now()
	state := SourceState{
		ID:            "test-source",
		Vendor:        "TestVendor",
		URL:           "https://test.example.com",
		Enabled:       true,
		Status:        "ok",
		UpdatedAt:     now,
		LastAttemptAt: now,
		LastError:     "",
		ModelCount:    42,
	}
	if state.ID != "test-source" || state.Vendor != "TestVendor" {
		t.Errorf("unexpected source state: %+v", state)
	}
	if !state.Enabled || state.Status != "ok" || state.ModelCount != 42 {
		t.Errorf("unexpected source state values: %+v", state)
	}
}

func TestFXSnapshotFields(t *testing.T) {
	now := time.Now()
	fx := FXSnapshot{
		Enabled:       true,
		SourceURL:     "https://fx.example.com",
		BaseCurrency:  "USD",
		UpdatedAt:     now,
		LastAttemptAt: now,
		LastError:     "",
		RatesToUSD:    map[string]float64{"EUR": 0.92},
	}
	if !fx.Enabled || fx.BaseCurrency != "USD" {
		t.Errorf("unexpected FX snapshot: %+v", fx)
	}
	if fx.RatesToUSD["EUR"] != 0.92 {
		t.Errorf("unexpected rate: %f", fx.RatesToUSD["EUR"])
	}
}
