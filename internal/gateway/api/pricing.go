package api

import (
	"strings"

	pricinginfra "ai-model-gateway/internal/infra/pricing"
	telemetrycore "ai-model-gateway/internal/telemetry"
)

// PricingResolver exposes the live pricing snapshot used to fix request costs.
type PricingResolver interface {
	Snapshot() pricinginfra.Snapshot
}

// FixedPricing captures the immutable pricing values persisted for one request.
type FixedPricing struct {
	Status            string
	SourceID          string
	Currency          string
	FXRateToUSD       float64
	InputPer1M        float64
	CachedInputPer1M  float64
	OutputPer1M       float64
	PromptCost        float64
	CompletionCost    float64
	TotalCost         float64
	PromptCostUSD     float64
	CompletionCostUSD float64
	TotalCostUSD      float64
}

func resolveFixedPricing(
	resolver PricingResolver,
	requestedModel string,
	effectiveModel string,
	providerID string,
	promptTokens int64,
	cachedPromptTokens int64,
	completionTokens int64,
	cacheHit bool,
	statusCode int,
) FixedPricing {
	if cacheHit {
		return FixedPricing{
			Status:   telemetrycore.PricingStatusFixed,
			SourceID: "cache",
			Currency: "USD",
		}
	}
	if statusCode < 200 || statusCode >= 400 {
		return FixedPricing{Status: telemetrycore.PricingStatusUnpriced}
	}
	if promptTokens <= 0 && cachedPromptTokens <= 0 && completionTokens <= 0 {
		return FixedPricing{Status: telemetrycore.PricingStatusUnpriced}
	}
	if resolver == nil {
		return FixedPricing{Status: telemetrycore.PricingStatusUnpriced}
	}

	snapshot := resolver.Snapshot()
	if len(snapshot.Catalog) == 0 {
		return FixedPricing{Status: telemetrycore.PricingStatusUnpriced}
	}

	catalog := make(map[string]telemetrycore.Pricing, len(snapshot.Catalog))
	for key, price := range snapshot.Catalog {
		catalog[key] = telemetrycore.Pricing{
			Currency:            price.Currency,
			InputPer1M:          price.InputPer1M,
			CachedInputPer1M:    price.CachedInputPer1M,
			OutputPer1M:         price.OutputPer1M,
			InputPer1MUsd:       price.InputPer1MUsd,
			CachedInputPer1MUsd: price.CachedInputPer1MUsd,
			OutputPer1MUsd:      price.OutputPer1MUsd,
			Source:              price.Source,
			SourceID:            price.SourceID,
			FXRateToUSD:         price.FXRateToUSD,
		}
	}

	_, price, ok := telemetrycore.ResolvePricing(catalog, requestedModel, effectiveModel, providerID)
	if !ok {
		return FixedPricing{Status: telemetrycore.PricingStatusUnpriced}
	}

	usage := telemetrycore.Usage{
		PromptTokens:       int(promptTokens),
		CachedPromptTokens: int(cachedPromptTokens),
		CompletionTokens:   int(completionTokens),
		TotalTokens:        int(promptTokens + completionTokens),
	}
	promptCost, completionCost, totalCost := calculateNativeCost(usage, price)
	promptCostUSD, completionCostUSD, totalCostUSD := calculateUSDCost(usage, price)

	sourceID := strings.TrimSpace(price.SourceID)
	if sourceID == "" {
		sourceID = strings.TrimSpace(price.Source)
	}
	currency := strings.ToUpper(strings.TrimSpace(price.Currency))
	if currency == "" {
		currency = "USD"
	}
	fxRate := price.FXRateToUSD
	if currency == "USD" && fxRate == 0 {
		fxRate = 1
	}

	return FixedPricing{
		Status:            telemetrycore.PricingStatusFixed,
		SourceID:          sourceID,
		Currency:          currency,
		FXRateToUSD:       fxRate,
		InputPer1M:        price.InputPer1M,
		CachedInputPer1M:  price.CachedInputPer1M,
		OutputPer1M:       price.OutputPer1M,
		PromptCost:        promptCost,
		CompletionCost:    completionCost,
		TotalCost:         totalCost,
		PromptCostUSD:     promptCostUSD,
		CompletionCostUSD: completionCostUSD,
		TotalCostUSD:      totalCostUSD,
	}
}

func calculateNativeCost(usage telemetrycore.Usage, price telemetrycore.Pricing) (float64, float64, float64) {
	cachedTokens := clampCachedPromptTokens(usage)
	uncachedTokens := usage.PromptTokens - cachedTokens

	prompt := (float64(uncachedTokens) / 1_000_000) * price.InputPer1M
	if cachedTokens > 0 {
		cachedRate := price.CachedInputPer1M
		if cachedRate <= 0 {
			cachedRate = price.InputPer1M
		}
		prompt += (float64(cachedTokens) / 1_000_000) * cachedRate
	}
	completion := (float64(usage.CompletionTokens) / 1_000_000) * price.OutputPer1M
	return prompt, completion, prompt + completion
}

func calculateUSDCost(usage telemetrycore.Usage, price telemetrycore.Pricing) (float64, float64, float64) {
	cachedTokens := clampCachedPromptTokens(usage)
	uncachedTokens := usage.PromptTokens - cachedTokens

	prompt := (float64(uncachedTokens) / 1_000_000) * price.InputPer1MUsd
	if cachedTokens > 0 {
		cachedRate := price.CachedInputPer1MUsd
		if cachedRate <= 0 {
			cachedRate = price.InputPer1MUsd
		}
		prompt += (float64(cachedTokens) / 1_000_000) * cachedRate
	}
	completion := (float64(usage.CompletionTokens) / 1_000_000) * price.OutputPer1MUsd
	return prompt, completion, prompt + completion
}

func clampCachedPromptTokens(usage telemetrycore.Usage) int {
	cached := usage.CachedPromptTokens
	if cached < 0 {
		return 0
	}
	if cached > usage.PromptTokens {
		return usage.PromptTokens
	}
	return cached
}
