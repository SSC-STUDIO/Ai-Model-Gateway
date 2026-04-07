package app

import (
	"context"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func testProviders() []core.Provider {
	tr := true
	return []core.Provider{
		{Name: "provider-a", BaseURL: "https://a.com", Models: []string{"gpt-4o", "gpt-4o-mini"}, Weight: 3, Enabled: &tr},
		{Name: "provider-b", BaseURL: "https://b.com", Models: []string{"gpt-4o"}, Weight: 1, Enabled: &tr},
		{Name: "provider-c", BaseURL: "https://c.com", Models: []string{"claude-3"}, Weight: 1, Enabled: &tr},
	}
}

func testRoutingCfg() core.RoutingConfig {
	cfg := core.RoutingConfig{}
	cfg.Strategy = core.StrategyHealthWeightedRR
	cfg.MaxRetries = 2
	cfg.StickySessions = core.StickySessionConfig{Enabled: true, TTLSec: 300}
	cfg.FailurePolicy = core.FailurePolicyConfig{Threshold: 3, CooldownSec: 60}
	return cfg
}

func TestSelector_Select_BasicRouting(t *testing.T) {
	sel := NewRouteSelector(testRoutingCfg(), testProviders())
	ctx := context.Background()

	p, err := sel.Select(ctx, "gpt-4o", "")
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if p.Name != "provider-a" && p.Name != "provider-b" {
		t.Errorf("expected provider-a or provider-b, got %s", p.Name)
	}
}

func TestSelector_Select_ModelFilter(t *testing.T) {
	sel := NewRouteSelector(testRoutingCfg(), testProviders())
	ctx := context.Background()

	p, err := sel.Select(ctx, "claude-3", "")
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if p.Name != "provider-c" {
		t.Errorf("expected provider-c for claude-3, got %s", p.Name)
	}
}

func TestSelector_Select_NoProvider(t *testing.T) {
	sel := NewRouteSelector(testRoutingCfg(), testProviders())
	ctx := context.Background()

	_, err := sel.Select(ctx, "nonexistent-model", "")
	if err != core.ErrNoProvider {
		t.Errorf("expected ErrNoProvider, got %v", err)
	}
}

func TestSelector_Select_DisabledProvider(t *testing.T) {
	f := false
	providers := []core.Provider{
		{Name: "disabled", BaseURL: "https://d.com", Models: []string{"gpt-4o"}, Weight: 1, Enabled: &f},
	}
	sel := NewRouteSelector(testRoutingCfg(), providers)
	ctx := context.Background()

	_, err := sel.Select(ctx, "gpt-4o", "")
	if err != core.ErrNoProvider {
		t.Errorf("expected ErrNoProvider for disabled provider, got %v", err)
	}
}

func TestSelector_StickySession(t *testing.T) {
	sel := NewRouteSelector(testRoutingCfg(), testProviders())
	ctx := context.Background()

	// First call binds sticky key to a provider.
	p1, _ := sel.Select(ctx, "gpt-4o", "session-123")

	// Subsequent calls with same sticky key should return same provider.
	for i := 0; i < 5; i++ {
		p, _ := sel.Select(ctx, "gpt-4o", "session-123")
		if p.Name != p1.Name {
			t.Errorf("iteration %d: expected sticky provider %s, got %s", i, p1.Name, p.Name)
		}
	}
}

func TestSelector_ReportResult_CircuitBreaker(t *testing.T) {
	tr := true
	providers := []core.Provider{
		{Name: "only-one", BaseURL: "https://a.com", Models: []string{"m"}, Weight: 1, Enabled: &tr},
	}
	cfg := testRoutingCfg()
	cfg.FailurePolicy.Threshold = 2
	cfg.FailurePolicy.CooldownSec = 600 // long cooldown
	sel := NewRouteSelector(cfg, providers)

	// Report enough failures to trigger circuit breaker.
	for i := 0; i < 3; i++ {
		sel.ReportResult(&providers[0], 500, 100*time.Millisecond, nil)
	}

	// Should be cooled down → no provider available.
	_, err := sel.Select(context.Background(), "m", "")
	if err != core.ErrNoProvider {
		t.Errorf("expected ErrNoProvider after circuit break, got %v", err)
	}
}

func TestSelector_ListModels(t *testing.T) {
	sel := NewRouteSelector(testRoutingCfg(), testProviders())
	models := sel.ListModels()

	expected := map[string]bool{"gpt-4o": true, "gpt-4o-mini": true, "claude-3": true}
	if len(models) != len(expected) {
		t.Fatalf("expected %d models, got %d: %v", len(expected), len(models), models)
	}
	for _, m := range models {
		if !expected[m] {
			t.Errorf("unexpected model: %s", m)
		}
	}
}

func TestSelector_Select_PrefersFreeProvidersBeforeQuotaLimited(t *testing.T) {
	tr := true
	providers := []core.Provider{
		{Name: "quota-first", BaseURL: "https://quota.example.com", ProviderClass: core.ProviderClassQuotaLimited, Models: []string{"gpt-4o"}, Weight: 1, Enabled: &tr},
		{Name: "free-second", BaseURL: "https://free.example.com", ProviderClass: core.ProviderClassFree, Models: []string{"gpt-4o"}, Weight: 1, Enabled: &tr},
	}

	sel := NewRouteSelector(testRoutingCfg(), providers)

	p, err := sel.Select(context.Background(), "gpt-4o", "")
	if err != nil {
		t.Fatalf("Select() error: %v", err)
	}
	if p.Name != "free-second" {
		t.Fatalf("expected free provider to be preferred, got %s", p.Name)
	}
}

func TestSelector_Select_KeepsQuotaBlockedProviderBlockedWhenRecoveryIntervalIsNonPositive(t *testing.T) {
	for _, recoveryMin := range []int{0, -5} {
		t.Run(time.Duration(recoveryMin).String(), func(t *testing.T) {
			tr := true
			providers := []core.Provider{
				{Name: "quota-provider", BaseURL: "https://quota.example.com", ProviderClass: core.ProviderClassQuotaLimited, Models: []string{"gpt-4o"}, Weight: 1, Enabled: &tr},
			}
			cfg := testRoutingCfg()
			cfg.FailurePolicy.QuotaRecoveryIntervalMin = recoveryMin
			sel := NewRouteSelector(cfg, providers)

			sel.ReportResult(&providers[0], 429, 10*time.Millisecond, nil)

			_, err := sel.Select(context.Background(), "gpt-4o", "")
			if err != core.ErrNoProvider {
				t.Fatalf("expected ErrNoProvider while quota block is still active, got %v", err)
			}
		})
	}
}
