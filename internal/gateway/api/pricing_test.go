package api

import (
	"testing"

	pricinginfra "ai-model-gateway/internal/infra/pricing"
	telemetrycore "ai-model-gateway/internal/telemetry"
)

type mockPricingResolver struct {
	snapshot pricinginfra.Snapshot
}

func (m *mockPricingResolver) Snapshot() pricinginfra.Snapshot {
	return m.snapshot
}

func TestResolveFixedPricing_CacheHit(t *testing.T) {
	result := resolveFixedPricing(nil, "gpt-4", "gpt-4", "openai", 100, 50, 200, true, 200)
	if result.Status != telemetrycore.PricingStatusFixed {
		t.Errorf("expected status fixed, got %s", result.Status)
	}
	if result.SourceID != "cache" {
		t.Errorf("expected sourceID cache, got %s", result.SourceID)
	}
	if result.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", result.Currency)
	}
}

func TestResolveFixedPricing_ErrorStatus(t *testing.T) {
	statusCodes := []int{100, 400, 500, 502}
	for _, code := range statusCodes {
		result := resolveFixedPricing(nil, "gpt-4", "gpt-4", "openai", 100, 50, 200, false, code)
		if result.Status != telemetrycore.PricingStatusUnpriced {
			t.Errorf("status code %d: expected status unpriced, got %s", code, result.Status)
		}
	}
}

func TestResolveFixedPricing_ZeroTokens(t *testing.T) {
	result := resolveFixedPricing(nil, "gpt-4", "gpt-4", "openai", 0, 0, 0, false, 200)
	if result.Status != telemetrycore.PricingStatusUnpriced {
		t.Errorf("expected status unpriced for zero tokens, got %s", result.Status)
	}
}

func TestResolveFixedPricing_NilResolver(t *testing.T) {
	result := resolveFixedPricing(nil, "gpt-4", "gpt-4", "openai", 100, 0, 50, false, 200)
	if result.Status != telemetrycore.PricingStatusUnpriced {
		t.Errorf("expected status unpriced for nil resolver, got %s", result.Status)
	}
}

func TestResolveFixedPricing_EmptyCatalog(t *testing.T) {
	resolver := &mockPricingResolver{snapshot: pricinginfra.Snapshot{}}
	result := resolveFixedPricing(resolver, "gpt-4", "gpt-4", "openai", 100, 0, 50, false, 200)
	if result.Status != telemetrycore.PricingStatusUnpriced {
		t.Errorf("expected status unpriced for empty catalog, got %s", result.Status)
	}
}

func TestResolveFixedPricing_ModelNotFound(t *testing.T) {
	resolver := &mockPricingResolver{
		snapshot: pricinginfra.Snapshot{
			Catalog: map[string]pricinginfra.Price{
				"other-model": {InputPer1M: 1, OutputPer1M: 2},
			},
		},
	}
	result := resolveFixedPricing(resolver, "gpt-4", "gpt-4", "openai", 100, 0, 50, false, 200)
	if result.Status != telemetrycore.PricingStatusUnpriced {
		t.Errorf("expected status unpriced for missing model, got %s", result.Status)
	}
}

func TestResolveFixedPricing_Success(t *testing.T) {
	resolver := &mockPricingResolver{
		snapshot: pricinginfra.Snapshot{
			Catalog: map[string]pricinginfra.Price{
				"gpt-4": {
					Currency:         "USD",
					InputPer1M:       30,
					CachedInputPer1M: 15,
					OutputPer1M:      60,
					Source:           "openai",
					SourceID:         "official",
					FXRateToUSD:      1,
				},
			},
		},
	}
	result := resolveFixedPricing(resolver, "gpt-4", "gpt-4", "openai", 1000, 500, 2000, false, 200)

	if result.Status != telemetrycore.PricingStatusFixed {
		t.Errorf("expected status fixed, got %s", result.Status)
	}
	if result.SourceID != "official" {
		t.Errorf("expected sourceID official, got %s", result.SourceID)
	}
	if result.Currency != "USD" {
		t.Errorf("expected currency USD, got %s", result.Currency)
	}
	if result.InputPer1M != 30 {
		t.Errorf("expected InputPer1M 30, got %f", result.InputPer1M)
	}
	if result.TotalCost <= 0 {
		t.Errorf("expected positive TotalCost, got %f", result.TotalCost)
	}
}

func TestCalculateNativeCost_NoCachedTokens(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 500_000,
	}
	price := telemetrycore.Pricing{
		InputPer1M:  10,
		OutputPer1M: 30,
	}

	promptCost, completionCost, totalCost := calculateNativeCost(usage, price)

	expectedPrompt := 10.0     // 1M tokens * $10/M
	expectedCompletion := 15.0 // 0.5M tokens * $30/M

	if promptCost != expectedPrompt {
		t.Errorf("expected promptCost %f, got %f", expectedPrompt, promptCost)
	}
	if completionCost != expectedCompletion {
		t.Errorf("expected completionCost %f, got %f", expectedCompletion, completionCost)
	}
	if totalCost != expectedPrompt+expectedCompletion {
		t.Errorf("expected totalCost %f, got %f", expectedPrompt+expectedCompletion, totalCost)
	}
}

func TestCalculateNativeCost_WithCachedTokens(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       1_000_000,
		CachedPromptTokens: 500_000,
		CompletionTokens:   500_000,
	}
	price := telemetrycore.Pricing{
		InputPer1M:       10,
		CachedInputPer1M: 5,
		OutputPer1M:      30,
	}

	promptCost, _, _ := calculateNativeCost(usage, price)

	// 500K uncached * $10/M + 500K cached * $5/M = 5 + 2.5 = 7.5
	expectedPrompt := 7.5
	if promptCost != expectedPrompt {
		t.Errorf("expected promptCost %f, got %f", expectedPrompt, promptCost)
	}
}

func TestCalculateNativeCost_ZeroCachedRate(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       1_000_000,
		CachedPromptTokens: 500_000,
	}
	price := telemetrycore.Pricing{
		InputPer1M:       10,
		CachedInputPer1M: 0, // Should fall back to InputPer1M
		OutputPer1M:      30,
	}

	promptCost, _, _ := calculateNativeCost(usage, price)

	// 500K uncached * $10/M + 500K cached * $10/M (fallback) = 5 + 5 = 10
	expectedPrompt := 10.0
	if promptCost != expectedPrompt {
		t.Errorf("expected promptCost %f (with fallback), got %f", expectedPrompt, promptCost)
	}
}

func TestClampCachedPromptTokens_Negative(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       100,
		CachedPromptTokens: -50,
	}
	result := clampCachedPromptTokens(usage)
	if result != 0 {
		t.Errorf("expected 0 for negative cached tokens, got %d", result)
	}
}

func TestClampCachedPromptTokens_ExceedsPrompt(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       100,
		CachedPromptTokens: 200,
	}
	result := clampCachedPromptTokens(usage)
	if result != 100 {
		t.Errorf("expected 100 (clamped to prompt tokens), got %d", result)
	}
}

func TestClampCachedPromptTokens_Valid(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       100,
		CachedPromptTokens: 50,
	}
	result := clampCachedPromptTokens(usage)
	if result != 50 {
		t.Errorf("expected 50, got %d", result)
	}
}

func TestClampCachedPromptTokens_Zero(t *testing.T) {
	usage := telemetrycore.Usage{
		PromptTokens:       100,
		CachedPromptTokens: 0,
	}
	result := clampCachedPromptTokens(usage)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}
