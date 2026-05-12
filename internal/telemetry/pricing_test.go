package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			wantCurrency:   "USD",
		},
		{
			name:           "kimi k2.6 routed as coding model",
			requestedModel: "kimi-k2.6",
			effectiveModel: "kimi-for-coding",
			upstream:       "kimi-official",
			wantPricing:    "kimi-k2.6",
			wantCurrency:   "USD",
		},
		{
			name:           "requested model wins over bridge model",
			requestedModel: "gpt-5.5",
			effectiveModel: "gpt-4o-mini",
			wantPricing:    "gpt-5.5",
			wantCurrency:   "USD",
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

func TestShouldRefresh(t *testing.T) {
	tests := []struct {
		name     string
		last     time.Time
		interval int
		want     bool
	}{
		{"zero time", time.Time{}, 15, true},
		{"recent attempt", time.Now(), 15, false},
		{"old attempt", time.Now().Add(-30 * time.Minute), 15, true},
		{"zero interval defaults to 15", time.Now().Add(-20 * time.Minute), 0, true},
		{"zero interval recent", time.Now(), 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRefresh(tt.last, tt.interval)
			if got != tt.want {
				t.Fatalf("shouldRefresh(%v, %d) = %v, want %v", tt.last, tt.interval, got, tt.want)
			}
		})
	}
}

func TestCloneSourceCatalogs(t *testing.T) {
	// nil/empty
	if result := cloneSourceCatalogs(nil); result != nil {
		t.Fatalf("expected nil for nil input, got %v", result)
	}
	if result := cloneSourceCatalogs(map[string]map[string]Pricing{}); result != nil {
		t.Fatalf("expected nil for empty input, got %v", result)
	}

	// non-empty with mutation independence
	input := map[string]map[string]Pricing{
		"openai": {"gpt-4o": {InputPer1M: 10}},
	}
	cloned := cloneSourceCatalogs(input)
	input["openai"]["gpt-4o"] = Pricing{InputPer1M: 999}
	if cloned["openai"]["gpt-4o"].InputPer1M == 999 {
		t.Fatal("clone should be independent")
	}
}

func TestMergeSourceCatalogs(t *testing.T) {
	base := map[string]map[string]Pricing{
		"openai": {"gpt-4o": {InputPer1M: 10}},
	}
	overlay := map[string]map[string]Pricing{
		"anthropic": {"claude-3": {InputPer1M: 15}},
	}
	merged := mergeSourceCatalogs(base, overlay)
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
	// nil base
	merged2 := mergeSourceCatalogs(nil, overlay)
	if len(merged2) != 1 {
		t.Fatalf("len(merged2) = %d, want 1", len(merged2))
	}
}

func TestClonePricingSourceStates(t *testing.T) {
	if result := clonePricingSourceStates(nil); result != nil {
		t.Fatalf("expected nil for nil input")
	}
	if result := clonePricingSourceStates([]PricingSourceState{}); result != nil {
		t.Fatalf("expected nil for empty input")
	}
	states := []PricingSourceState{{ID: "s1", Vendor: "openai"}}
	cloned := clonePricingSourceStates(states)
	states[0].ID = "modified"
	if cloned[0].ID != "s1" {
		t.Fatal("clone should be independent")
	}
}

func TestClonePricingFXSnapshot(t *testing.T) {
	// empty
	snap := clonePricingFXSnapshot(PricingFXSnapshot{})
	if snap.RatesToUSD != nil {
		t.Fatal("expected nil rates for empty snapshot")
	}
	// non-empty with mutation independence
	snap = clonePricingFXSnapshot(PricingFXSnapshot{RatesToUSD: map[string]float64{"USD": 1, "EUR": 0.9}})
	snap.RatesToUSD["USD"] = 999
	original := PricingFXSnapshot{RatesToUSD: map[string]float64{"USD": 1, "EUR": 0.9}}
	clone := clonePricingFXSnapshot(original)
	if clone.RatesToUSD["USD"] != 1 {
		t.Fatal("clone should be independent")
	}
}

func TestLoadPricingFXCache(t *testing.T) {
	// empty path
	_, err := loadPricingFXCache("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}

	// valid round-trip
	tmpDir, err := os.MkdirTemp("", "fx-cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "fx-cache.json")
	original := PricingFXSnapshot{
		Enabled:      true,
		SourceURL:    "https://example.com",
		BaseCurrency: "EUR",
		RatesToUSD:   map[string]float64{"USD": 1, "EUR": 0.9, "JPY": 0.0067},
	}
	if err := savePricingFXCache(path, original); err != nil {
		t.Fatalf("savePricingFXCache: %v", err)
	}
	loaded, err := loadPricingFXCache(path)
	if err != nil {
		t.Fatalf("loadPricingFXCache: %v", err)
	}
	if loaded.BaseCurrency != "EUR" {
		t.Fatalf("BaseCurrency = %q, want EUR", loaded.BaseCurrency)
	}
	if len(loaded.RatesToUSD) != 3 {
		t.Fatalf("RatesToUSD len = %d, want 3", len(loaded.RatesToUSD))
	}

	// save with empty path should be no-op
	if err := savePricingFXCache("", original); err != nil {
		t.Fatalf("savePricingFXCache empty path should be nil, got %v", err)
	}

	// invalid JSON file
	invalidPath := filepath.Join(tmpDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("{bad json"), 0o600)
	_, err = loadPricingFXCache(invalidPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestApplyFXToPrice(t *testing.T) {
	fx := PricingFXSnapshot{
		Enabled:    true,
		RatesToUSD: map[string]float64{"USD": 1, "CNY": 0.14},
	}

	// USD price
	usdPrice := applyFXToPrice(Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30}, fx)
	if usdPrice.FXRateToUSD != 1 {
		t.Fatalf("USD FXRateToUSD = %f, want 1", usdPrice.FXRateToUSD)
	}

	// CNY price
	cnyPrice := applyFXToPrice(Pricing{Currency: "CNY", InputPer1M: 10, OutputPer1M: 30}, fx)
	if cnyPrice.FXRateToUSD != 0.14 {
		t.Fatalf("CNY FXRateToUSD = %f, want 0.14", cnyPrice.FXRateToUSD)
	}
	if cnyPrice.InputPer1MUsd < 1.39 || cnyPrice.InputPer1MUsd > 1.41 {
		t.Fatalf("CNY InputPer1MUsd = %f, want ~1.4", cnyPrice.InputPer1MUsd)
	}

	// FX disabled
	fxDisabled := PricingFXSnapshot{Enabled: false}
	disabledPrice := applyFXToPrice(Pricing{Currency: "CNY", InputPer1M: 10}, fxDisabled)
	if disabledPrice.FXRateToUSD != 0 {
		t.Fatalf("disabled FX should not set rate, got %f", disabledPrice.FXRateToUSD)
	}

	// CNY with no rates map
	noRates := applyFXToPrice(Pricing{Currency: "CNY", InputPer1M: 10}, PricingFXSnapshot{Enabled: true})
	if noRates.FXRateToUSD != 0 {
		t.Fatalf("no rates map should not set rate, got %f", noRates.FXRateToUSD)
	}
}

func TestApplyFXToCatalog(t *testing.T) {
	fx := PricingFXSnapshot{Enabled: true, RatesToUSD: map[string]float64{"USD": 1}}

	// empty
	if result := applyFXToCatalog(nil, fx); result != nil {
		t.Fatal("expected nil for empty catalog")
	}

	// non-empty
	catalog := map[string]Pricing{"gpt-4o": {Currency: "USD", InputPer1M: 10}}
	result := applyFXToCatalog(catalog, fx)
	if len(result) != 1 {
		t.Fatalf("len = %d, want 1", len(result))
	}
}

func TestAnnotateSourceCatalog(t *testing.T) {
	catalog := map[string]Pricing{
		"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
	}
	source := core.PricingSourceConfig{ID: "openai-src", Vendor: "OpenAI"}
	annotateSourceCatalog(catalog, source)
	if catalog["gpt-4o"].Source != "OpenAI" {
		t.Fatalf("Source = %q, want OpenAI", catalog["gpt-4o"].Source)
	}
	if catalog["gpt-4o"].SourceID != "openai-src" {
		t.Fatalf("SourceID = %q, want openai-src", catalog["gpt-4o"].SourceID)
	}
}

func TestFetchSinglePageCatalogWithMockServer(t *testing.T) {
	html := `<table><tr><th>Model</th><th>Input</th><th>Output</th></tr>
<tr><td>claude-sonnet-4</td><td>Input $3.00</td><td>Output $15.00</td></tr>
</table>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
	}))
	defer srv.Close()

	fetcher := fetchSinglePageCatalog("USD", parseGenericPricingTables)
	catalog, err := fetcher(context.Background(), srv.Client(), core.PricingSourceConfig{
		ID:  "anthropic",
		URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(catalog) == 0 {
		t.Fatal("expected at least one parsed model")
	}

	// Test with server returning no pricing rows
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<table><tr><td>no price</td></tr></table>"))
	}))
	defer srvEmpty.Close()

	_, err = fetcher(context.Background(), srvEmpty.Client(), core.PricingSourceConfig{
		ID:  "test",
		URL: srvEmpty.URL,
	})
	if err == nil {
		t.Fatal("expected error for no pricing rows")
	}

	// Test with server returning error status
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srvErr.Close()

	_, err = fetcher(context.Background(), srvErr.Client(), core.PricingSourceConfig{
		ID:  "test",
		URL: srvErr.URL,
	})
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestInferCurrency(t *testing.T) {
	tests := []struct {
		input    string
		fallback string
		want     string
	}{
		{"$10.00", "USD", "USD"},
		{"US$5.00", "USD", "USD"},
		{"￥3.00", "USD", "CNY"},
		{"¥3.00", "USD", "CNY"},
		{"10 CNY", "USD", "CNY"},
		{"10 元", "USD", "CNY"},
		{"unknown", "USD", "USD"},
		{"unknown", "cny", "CNY"},
	}
	for _, tt := range tests {
		t.Run(tt.input+"_"+tt.fallback, func(t *testing.T) {
			got := inferCurrency(tt.input, tt.fallback)
			if got != tt.want {
				t.Fatalf("inferCurrency(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestExtractMoneyValues(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"$10.50", 1},
		{"$10.50 and $20.00", 2},
		{"no money here", 0},
		{"", 0},
		{"US$3.14", 1},
		{"￥1.50", 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractMoneyValues(tt.input)
			if len(got) != tt.want {
				t.Fatalf("extractMoneyValues(%q) len = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestParseModelAliasesFromCell(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"gpt-4o", 1},
		{"", 0},
		{"GPT-4o / GPT-4o-mini", 2},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseModelAliasesFromCell(tt.input)
			if len(got) != tt.want {
				t.Fatalf("parseModelAliasesFromCell(%q) len = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestExtractPriceFromCells(t *testing.T) {
	tests := []struct {
		name   string
		cells  []string
		defCur string
		wantOk bool
	}{
		{"input+output", []string{"Input $10.00", "Output $30.00"}, "USD", true},
		{"cache+output", []string{"Cached input $5.00", "Output $30.00"}, "USD", true},
		{"empty cells", []string{}, "USD", false},
		{"no money", []string{"free", "n/a"}, "USD", false},
		{"unordered defaults", []string{"$10.00", "$30.00"}, "USD", true},
		{"three values", []string{"$10.00", "$5.00", "$30.00"}, "USD", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := extractPriceFromCells(tt.cells, tt.defCur)
			if ok != tt.wantOk {
				t.Fatalf("extractPriceFromCells ok = %v, want %v", ok, tt.wantOk)
			}
		})
	}
}

func TestParseGenericPricingTables(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		currency string
		wantKeys int
	}{
		{
			name:     "simple table with input and output",
			body:     `<table><tr><th>Model</th><th>Price</th></tr><tr><td>gpt-4o</td><td>Input $10.00 / 1M tokens</td><td>Output $30.00 / 1M tokens</td></tr></table>`,
			currency: "USD",
			wantKeys: 1,
		},
		{
			name:     "empty body",
			body:     "",
			currency: "USD",
			wantKeys: 0,
		},
		{
			name:     "no valid rows",
			body:     `<table><tr><td>no model name</td></tr></table>`,
			currency: "USD",
			wantKeys: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseGenericPricingTables(tt.body, tt.currency)
			if len(result) != tt.wantKeys {
				t.Fatalf("len = %d, want %d", len(result), tt.wantKeys)
			}
		})
	}
}

func TestFetchPricingSourceCatalogUnsupportedVendor(t *testing.T) {
	_, err := fetchPricingSourceCatalog(context.Background(), core.PricingSourceConfig{
		ID:     "test",
		Vendor: "unsupported_vendor",
		URL:    "https://example.com",
	})
	if err == nil {
		t.Fatal("expected error for unsupported vendor")
	}
}

func TestFetchSinglePageCatalogEmptyURL(t *testing.T) {
	fetcher := fetchSinglePageCatalog("USD", parseGenericPricingTables)
	_, err := fetcher(context.Background(), defaultPricingHTTPClient, core.PricingSourceConfig{
		ID:  "test",
		URL: "",
	})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestHydrateSourceStates(t *testing.T) {
	cfg := core.PricingConfig{
		Sources: []core.PricingSourceConfig{
			{ID: "openai", Vendor: "openai", URL: "https://example.com", Enabled: boolPtr(true)},
			{ID: "disabled-src", Vendor: "test", URL: "https://example.com", Enabled: boolPtr(false)},
		},
	}
	existing := []PricingSourceState{
		{ID: "openai", Status: "ready"},
	}
	sourceCatalogs := map[string]map[string]Pricing{
		"openai": {"gpt-4o": {InputPer1M: 10}},
	}

	states := hydrateSourceStates(cfg, existing, sourceCatalogs)
	if len(states) != 2 {
		t.Fatalf("len(states) = %d, want 2", len(states))
	}

	// Check that the disabled source gets status "disabled"
	for _, s := range states {
		if s.ID == "disabled-src" && s.Status != "disabled" {
			t.Fatalf("disabled source status = %q, want disabled", s.Status)
		}
		if s.ID == "openai" && s.ModelCount != 1 {
			t.Fatalf("openai ModelCount = %d, want 1", s.ModelCount)
		}
	}

	// Empty config
	states2 := hydrateSourceStates(core.PricingConfig{}, nil, nil)
	if states2 != nil {
		t.Fatalf("expected nil for empty config, got len=%d", len(states2))
	}
}

func TestClonePricingCatalogSnapshot(t *testing.T) {
	snapshot := PricingCatalogSnapshot{
		SourceURL: "https://example.com",
		Catalog:   map[string]Pricing{"gpt-4o": {InputPer1M: 10}},
		SourceCatalogs: map[string]map[string]Pricing{
			"openai": {"gpt-4o": {InputPer1M: 10}},
		},
		Sources: []PricingSourceState{{ID: "openai", Status: "ready"}},
		FX:      PricingFXSnapshot{Enabled: true, RatesToUSD: map[string]float64{"USD": 1}},
	}

	cloned := clonePricingCatalogSnapshot(snapshot)

	// Mutation independence check
	snapshot.Catalog["gpt-4o"] = Pricing{InputPer1M: 999}
	if cloned.Catalog["gpt-4o"].InputPer1M == 999 {
		t.Fatal("catalog clone should be independent")
	}
	snapshot.Sources[0].ID = "modified"
	if cloned.Sources[0].ID != "openai" {
		t.Fatal("sources clone should be independent")
	}
	snapshot.FX.RatesToUSD["USD"] = 999
	if cloned.FX.RatesToUSD["USD"] == 999 {
		t.Fatal("FX clone should be independent")
	}
}

func TestFetchPricingFXMockServer(t *testing.T) {
	// Test with invalid XML body
	_, err := fetchPricingFXFromBytes([]byte("not xml"))
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}

	// Test with missing USD rate
	xmlBody := `<?xml version="1.0"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
<Cube><Cube time="2026-01-01"><Cube currency="EUR" rate="1"/></Cube></Cube>
</gesmes:Envelope>`
	_, err = fetchPricingFXFromBytes([]byte(xmlBody))
	if err == nil {
		t.Fatal("expected error for missing USD rate")
	}
}

func TestSavePricingFXCacheInvalidPath(t *testing.T) {
	// Empty path returns nil (no-op)
	if err := savePricingFXCache("", PricingFXSnapshot{}); err != nil {
		t.Fatalf("expected nil for empty path, got %v", err)
	}
}

func TestFetchPricingFXFromBytesSuccess(t *testing.T) {
	xmlBody := `<?xml version="1.0"?>
<gesmes:Envelope xmlns:gesmes="http://www.gesmes.org/xml/2002-08-01" xmlns="http://www.ecb.int/vocabulary/2002-08-01/eurofxref">
<Cube><Cube time="2026-05-12">
  <Cube currency="USD" rate="1.08"/>
  <Cube currency="JPY" rate="161.5"/>
  <Cube currency="GBP" rate="0.86"/>
  <Cube currency="CNY" rate="7.8"/>
</Cube></Cube>
</gesmes:Envelope>`

	snap, err := fetchPricingFXFromBytes([]byte(xmlBody))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.BaseCurrency != "EUR" {
		t.Fatalf("BaseCurrency = %q, want EUR", snap.BaseCurrency)
	}
	if snap.RatesToUSD["USD"] != 1 {
		t.Fatalf("USD = %f, want 1", snap.RatesToUSD["USD"])
	}
	if snap.RatesToUSD["EUR"] != 1.08 {
		t.Fatalf("EUR = %f, want 1.08", snap.RatesToUSD["EUR"])
	}
	if snap.SourceURL != pricingFXSourceURL {
		t.Fatalf("SourceURL = %q, want %q", snap.SourceURL, pricingFXSourceURL)
	}
}

func TestParseAPIPricingPage(t *testing.T) {
	body := `<h2 class="text-h4">GPT-4o</h2><h3>Price</h3><div><div><div>
  Input:<br/>$2.50 / 1M tokens
  Cached input:<br/>$1.25 / 1M tokens
  Output:<br/>$10.00 / 1M tokens
</div></div></div>`

	result := parseAPIPricingPage(body)
	if len(result) == 0 {
		t.Fatal("expected at least one parsed model")
	}
	if price, ok := result["gpt-4o"]; ok {
		if price.InputPer1M != 2.50 {
			t.Fatalf("InputPer1M = %f, want 2.50", price.InputPer1M)
		}
		if price.OutputPer1M != 10.00 {
			t.Fatalf("OutputPer1M = %f, want 10.00", price.OutputPer1M)
		}
		if price.CachedInputPer1M != 1.25 {
			t.Fatalf("CachedInputPer1M = %f, want 1.25", price.CachedInputPer1M)
		}
	}

	// Empty body
	result2 := parseAPIPricingPage("")
	if len(result2) != 0 {
		t.Fatalf("expected empty result for empty body, got %d", len(result2))
	}

	// No price values
	body3 := `<h2 class="text-h4">Unknown Model</h2><h3>Price</h3><div><div><div>No pricing info</div></div></div>`
	result3 := parseAPIPricingPage(body3)
	if len(result3) != 0 {
		t.Fatalf("expected empty result for no prices, got %d", len(result3))
	}
}

func TestParseGPT52PricingPage(t *testing.T) {
	body := `Price per million tokens
<table><tbody>
<tr><td><p><b>GPT-5.2</b></p></td><td><p>$2.00</p></td><td><p>$0.50</p></td><td><p>$8.00</p></td></tr>
<tr><td><p><b>GPT-5.2 mini</b></p></td><td><p>$0.40</p></td><td><p>$0.10</p></td><td><p>$1.60</p></td></tr>
</tbody></table>`

	result := parseGPT52PricingPage(body)
	if len(result) < 2 {
		t.Fatalf("expected at least 2 models, got %d", len(result))
	}
	if price, ok := result["gpt-5.2"]; ok {
		if price.InputPer1M != 2.00 {
			t.Fatalf("InputPer1M = %f, want 2.00", price.InputPer1M)
		}
	} else {
		t.Fatal("expected gpt-5.2 in result")
	}

	// Empty body
	result2 := parseGPT52PricingPage("")
	if len(result2) != 0 {
		t.Fatalf("expected empty result for empty body, got %d", len(result2))
	}

	// No matching table
	result3 := parseGPT52PricingPage("<p>no table here</p>")
	if len(result3) != 0 {
		t.Fatalf("expected empty result for no table, got %d", len(result3))
	}
}

func boolPtr(b bool) *bool { return &b }

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
