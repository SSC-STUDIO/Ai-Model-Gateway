package telemetry

import (
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestResolvePricingSupportsExpandedCatalog(t *testing.T) {
	catalog := mergePricingCatalogs(BootstrapPricingCatalog(), map[string]Pricing{
		providerScopedPricingKey("kimi-official", "glm-5.1"): priced("CNY", 3.2, 0.8, 12.8),
	})

	tests := []struct {
		name           string
		requestedModel string
		effectiveModel string
		upstream       string
		wantPricing    string
		wantCurrency   string
	}{
		{
			name:           "dated gpt 4.1 mini",
			requestedModel: "gpt-4.1-mini-2025-04-14",
			wantPricing:    "gpt-4.1-mini",
			wantCurrency:   "USD",
		},
		{
			name:           "provider prefixed gpt 4o mini",
			requestedModel: "openai/gpt-4o-mini-2024-07-18",
			wantPricing:    "gpt-4o-mini",
			wantCurrency:   "USD",
		},
		{
			name:           "deepseek variant suffix",
			requestedModel: "deepseek/deepseek-chat:free",
			wantPricing:    "deepseek-chat",
			wantCurrency:   "USD",
		},
		{
			name:           "provider scoped manual pricing",
			requestedModel: "glm-5.1",
			upstream:       "kimi-official",
			wantPricing:    providerScopedPricingKey("kimi-official", "glm-5.1"),
			wantCurrency:   "CNY",
		},
		{
			name:           "kimi preview alias",
			requestedModel: "moonshot/kimi-k2.5-preview",
			wantPricing:    "kimi-k2.5-preview",
			wantCurrency:   "CNY",
		},
		{
			name:           "dated o4 mini effective model",
			effectiveModel: "o4-mini-2025-04-16",
			wantPricing:    "o4-mini",
			wantCurrency:   "USD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, gotPricing, ok := ResolvePricing(catalog, tt.requestedModel, tt.effectiveModel, tt.upstream)
			if !ok {
				t.Fatalf("ResolvePricing(%q, %q, %q) returned no match", tt.requestedModel, tt.effectiveModel, tt.upstream)
			}
			if gotModel != tt.wantPricing {
				t.Fatalf("ResolvePricing(%q, %q, %q) model = %q, want %q", tt.requestedModel, tt.effectiveModel, tt.upstream, gotModel, tt.wantPricing)
			}
			if gotPricing.Currency != tt.wantCurrency {
				t.Fatalf("ResolvePricing(%q, %q, %q) currency = %q, want %q", tt.requestedModel, tt.effectiveModel, tt.upstream, gotPricing.Currency, tt.wantCurrency)
			}
			if gotPricing.InputPer1M < 0 || gotPricing.OutputPer1M < 0 {
				t.Fatalf("ResolvePricing(%q, %q, %q) returned invalid pricing: %+v", tt.requestedModel, tt.effectiveModel, tt.upstream, gotPricing)
			}
		})
	}
}

func TestBuildPricingSnapshotSupportsMultipleCurrencies(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-4o-mini-2024-07-18",
				Upstream:       "openai",
				Usage: Usage{
					PromptTokens:       600000,
					CachedPromptTokens: 100000,
					CompletionTokens:   200000,
					TotalTokens:        800000,
				},
			},
			{
				RequestedModel: "kimi-k2.5-preview",
				Upstream:       "kimi-official",
				Usage: Usage{
					PromptTokens:       300000,
					CachedPromptTokens: 50000,
					CompletionTokens:   120000,
					TotalTokens:        420000,
				},
			},
			{
				RequestedModel: "glm-5.1",
				Upstream:       "kimi-official",
				Usage: Usage{
					PromptTokens:     100000,
					CompletionTokens: 40000,
					TotalTokens:      140000,
				},
			},
		},
	}

	catalog := BootstrapPricingSnapshot()
	catalog.Catalog = mergePricingCatalogs(catalog.Catalog, map[string]Pricing{
		providerScopedPricingKey("kimi-official", "glm-5.1"): priced("CNY", 3.2, 0.8, 12.8),
	})

	result := BuildPricingSnapshot(snapshot, catalog)
	if result.Summary.PricedModels != 3 {
		t.Fatalf("priced models = %d, want 3", result.Summary.PricedModels)
	}
	if result.Summary.UnpricedModels != 0 {
		t.Fatalf("unpriced models = %d, want 0", result.Summary.UnpricedModels)
	}
	if len(result.Summary.TotalsByCurrency) != 2 {
		t.Fatalf("totals_by_currency len = %d, want 2", len(result.Summary.TotalsByCurrency))
	}
	if result.Summary.Total <= 0 {
		t.Fatalf("summary total = %f, want > 0", result.Summary.Total)
	}
	if result.Summary.TotalUsd <= 0 {
		t.Fatalf("summary total_usd = %f, want > 0", result.Summary.TotalUsd)
	}
	if len(result.RouteCatalog) != 3 {
		t.Fatalf("route catalog size = %d, want 3", len(result.RouteCatalog))
	}

	var hasUSD bool
	var hasCNY bool
	for _, total := range result.Summary.TotalsByCurrency {
		if total.Currency == "USD" && total.Total > 0 {
			hasUSD = true
		}
		if total.Currency == "CNY" && total.Total > 0 {
			hasCNY = true
		}
	}
	if !hasUSD || !hasCNY {
		t.Fatalf("expected USD and CNY totals, got %+v", result.Summary.TotalsByCurrency)
	}
	if result.Models[0].Cost.Currency == "" {
		t.Fatalf("expected model cost currency, got %+v", result.Models[0].Cost)
	}
}

func TestBuildPricingSnapshotKeepsPerUpstreamRowsSeparate(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-4o",
				Upstream:       "openai-primary",
				Usage: Usage{
					PromptTokens:     400000,
					CompletionTokens: 100000,
					TotalTokens:      500000,
				},
			},
			{
				RequestedModel: "gpt-4o",
				Upstream:       "openai-secondary",
				Usage: Usage{
					PromptTokens:     200000,
					CompletionTokens: 50000,
					TotalTokens:      250000,
				},
			},
		},
	}

	result := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(result.Models) != 2 {
		t.Fatalf("models len = %d, want 2: %+v", len(result.Models), result.Models)
	}
	if len(result.RouteCatalog) != 2 {
		t.Fatalf("route catalog len = %d, want 2", len(result.RouteCatalog))
	}

	byUpstream := make(map[string]PricingModelSummary, len(result.Models))
	for _, item := range result.Models {
		byUpstream[item.Upstream] = item
	}

	primary, ok := byUpstream["openai-primary"]
	if !ok {
		t.Fatalf("missing openai-primary row: %+v", result.Models)
	}
	if primary.Usage.TotalTokens != 500000 {
		t.Fatalf("openai-primary total_tokens = %d, want 500000", primary.Usage.TotalTokens)
	}

	secondary, ok := byUpstream["openai-secondary"]
	if !ok {
		t.Fatalf("missing openai-secondary row: %+v", result.Models)
	}
	if secondary.Usage.TotalTokens != 250000 {
		t.Fatalf("openai-secondary total_tokens = %d, want 250000", secondary.Usage.TotalTokens)
	}

	if result.Summary.PricedModels != 2 {
		t.Fatalf("priced models = %d, want 2", result.Summary.PricedModels)
	}
	if result.Summary.Total <= primary.Cost.Total || result.Summary.Total <= secondary.Cost.Total {
		t.Fatalf("summary total = %f, want combined total greater than each row", result.Summary.Total)
	}
}

func TestPricingCatalogUpdateConfigPreservesLiveCatalogWithoutCache(t *testing.T) {
	cfg := core.PricingConfig{}
	catalog := NewPricingCatalog(cfg)

	liveUpdatedAt := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	liveAttemptAt := liveUpdatedAt.Add(-time.Minute)
	live := PricingCatalogSnapshot{
		SourceURL:     "https://example.com/live",
		UpdatedAt:     liveUpdatedAt,
		LastAttemptAt: liveAttemptAt,
		Catalog: mergePricingCatalogs(BootstrapPricingCatalog(), map[string]Pricing{
			"custom-live-model": pricing(9, 0, 9),
		}),
	}
	catalog.state.Store(live)

	catalog.UpdateConfig(cfg)
	snapshot := catalog.Snapshot()

	got, ok := snapshot.Catalog["custom-live-model"]
	if !ok {
		t.Fatalf("expected live model pricing to survive config reload, got %+v", snapshot.Catalog)
	}
	if got.InputPer1M != 9 || got.OutputPer1M != 9 {
		t.Fatalf("unexpected live pricing after config reload: %+v", got)
	}
	if snapshot.SourceURL != live.SourceURL {
		t.Fatalf("source_url = %q, want %q", snapshot.SourceURL, live.SourceURL)
	}
	if !snapshot.UpdatedAt.Equal(liveUpdatedAt) {
		t.Fatalf("updated_at = %v, want %v", snapshot.UpdatedAt, liveUpdatedAt)
	}
	if !snapshot.LastAttemptAt.Equal(liveAttemptAt) {
		t.Fatalf("last_attempt_at = %v, want %v", snapshot.LastAttemptAt, liveAttemptAt)
	}
}

func TestPricingCatalogUpdateConfigAppliesNewManualOverridesToLiveCatalog(t *testing.T) {
	catalog := NewPricingCatalog(core.PricingConfig{})
	catalog.state.Store(PricingCatalogSnapshot{
		SourceURL: "https://example.com/live",
		Catalog: mergePricingCatalogs(BootstrapPricingCatalog(), map[string]Pricing{
			"custom-live-model": pricing(9, 0, 9),
		}),
	})

	catalog.UpdateConfig(core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{{
			Model:       "custom-live-model",
			Currency:    "CNY",
			InputPer1M:  1.5,
			OutputPer1M: 6.5,
		}},
	})

	snapshot := catalog.Snapshot()
	got, ok := snapshot.Catalog["custom-live-model"]
	if !ok {
		t.Fatalf("expected live model pricing to remain present after manual override")
	}
	if got.Currency != "CNY" || got.InputPer1M != 1.5 || got.OutputPer1M != 6.5 {
		t.Fatalf("unexpected manual override pricing: %+v", got)
	}
}

func TestPricingCatalogUpdateConfigClearsPreviousManualOverrides(t *testing.T) {
	manualCfg := core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{{
			Model:       "gpt-4o",
			Currency:    "USD",
			InputPer1M:  99,
			OutputPer1M: 199,
		}},
	}
	catalog := NewPricingCatalog(manualCfg)
	catalog.state.Store(applyPricingConfigToSnapshot(BootstrapPricingSnapshot(), manualCfg))

	catalog.UpdateConfig(core.PricingConfig{})

	snapshot := catalog.Snapshot()
	got, ok := snapshot.Catalog["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o pricing to remain available after removing manual override")
	}
	if got.InputPer1M == 99 || got.OutputPer1M == 199 {
		t.Fatalf("expected manual override to be cleared, got %+v", got)
	}

	bootstrap := BootstrapPricingSnapshot().Catalog["gpt-4o"]
	if got.InputPer1M != bootstrap.InputPer1M || got.OutputPer1M != bootstrap.OutputPer1M {
		t.Fatalf("expected bootstrap pricing after clearing manual override, got %+v want %+v", got, bootstrap)
	}
}

func TestCanonicalModelNamesNormalizesOpenAITitles(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "GPT-5.4 mini", want: "gpt-5.4-mini"},
		{title: "GPT-5.4 nano", want: "gpt-5.4-nano"},
		{title: "GPT-5.1 Codex mini", want: "gpt-5.1-codex-mini"},
		{title: "GPT-4o mini", want: "gpt-4o-mini"},
		{title: "o3 mini", want: "o3-mini"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := canonicalModelNames(tt.title)
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("canonicalModelNames(%q) = %#v, want [%q]", tt.title, got, tt.want)
			}
		})
	}
}
