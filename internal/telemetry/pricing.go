package telemetry

import (
	"sort"
	"strings"
	"time"
)

const OfficialPricingURL = "https://openai.com/api/pricing/"

type Pricing struct {
	InputPer1MUsd       float64 `json:"input_per_1m_usd"`
	CachedInputPer1MUsd float64 `json:"cached_input_per_1m_usd,omitempty"`
	OutputPer1MUsd      float64 `json:"output_per_1m_usd"`
}

type PricingCost struct {
	PromptUsd     float64 `json:"prompt_usd"`
	CompletionUsd float64 `json:"completion_usd"`
	TotalUsd      float64 `json:"total_usd"`
}

type PricingModelSummary struct {
	DisplayModel   string      `json:"display_model"`
	RequestedModel string      `json:"requested_model,omitempty"`
	EffectiveModel string      `json:"effective_model,omitempty"`
	PricingModel   string      `json:"pricing_model,omitempty"`
	Usage          Usage       `json:"usage"`
	Pricing        *Pricing    `json:"pricing,omitempty"`
	Cost           PricingCost `json:"cost"`
}

type PricingSummary struct {
	Currency           string  `json:"currency"`
	PromptUsd          float64 `json:"prompt_usd"`
	CompletionUsd      float64 `json:"completion_usd"`
	TotalUsd           float64 `json:"total_usd"`
	CachedPromptTokens int64   `json:"cached_prompt_tokens"`
	CacheSavingsUsd    float64 `json:"cache_savings_usd"`
	PricedModels       int     `json:"priced_models"`
	UnpricedModels     int     `json:"unpriced_models"`
}

type PricingCatalogSnapshot struct {
	SourceURL     string             `json:"source_url,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	LastAttemptAt time.Time          `json:"last_attempt_at,omitempty"`
	LastError     string             `json:"last_error,omitempty"`
	Catalog       map[string]Pricing `json:"catalog"`
}

type PricingSnapshot struct {
	SourceURL     string                `json:"source_url,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at,omitempty"`
	LastAttemptAt time.Time             `json:"last_attempt_at,omitempty"`
	LastError     string                `json:"last_error,omitempty"`
	Summary       PricingSummary        `json:"summary"`
	Models        []PricingModelSummary `json:"models"`
	Catalog       map[string]Pricing    `json:"catalog"`
	RouteCatalog  map[string]Pricing    `json:"route_catalog"`
}

func BuildPricingSnapshot(snapshot Snapshot, catalog PricingCatalogSnapshot) PricingSnapshot {
	if len(catalog.Catalog) == 0 {
		catalog.Catalog = BootstrapPricingCatalog()
	}

	summary := PricingSummary{Currency: "USD"}
	modelsByKey := make(map[string]*PricingModelSummary, len(snapshot.ByModelRoute))
	routeCatalog := make(map[string]Pricing, len(snapshot.ByModelRoute))

	for _, route := range snapshot.ByModelRoute {
		if route.Usage.TotalTokens <= 0 && route.Usage.PromptTokens <= 0 && route.Usage.CompletionTokens <= 0 {
			continue
		}

		item := PricingModelSummary{
			DisplayModel:   formatPricingDisplayModel(route.RequestedModel, route.Model),
			RequestedModel: strings.TrimSpace(route.RequestedModel),
			EffectiveModel: strings.TrimSpace(route.Model),
			Usage:          route.Usage,
		}

		if pricingModel, pricing, ok := ResolvePricing(catalog.Catalog, item.RequestedModel, item.EffectiveModel); ok {
			item.PricingModel = pricingModel
			item.Pricing = &pricing
			item.Cost = calculatePricingCost(route.Usage, pricing)
			routeCatalog[PricingRouteKey(item.RequestedModel, item.EffectiveModel)] = pricing
			summary.PromptUsd += item.Cost.PromptUsd
			summary.CompletionUsd += item.Cost.CompletionUsd
			summary.TotalUsd += item.Cost.TotalUsd
			summary.CachedPromptTokens += int64(normalizeCachedPromptTokens(route.Usage))
			summary.CacheSavingsUsd += calculateCacheSavings(route.Usage, pricing)
		} else {
			item.PricingModel = pricingGroupModel(item.RequestedModel, item.EffectiveModel)
		}

		key := pricingSummaryKey(item)
		if existing, ok := modelsByKey[key]; ok {
			existing.Usage.PromptTokens += item.Usage.PromptTokens
			existing.Usage.CachedPromptTokens += item.Usage.CachedPromptTokens
			existing.Usage.CompletionTokens += item.Usage.CompletionTokens
			existing.Usage.TotalTokens += item.Usage.TotalTokens
			existing.Cost.PromptUsd += item.Cost.PromptUsd
			existing.Cost.CompletionUsd += item.Cost.CompletionUsd
			existing.Cost.TotalUsd += item.Cost.TotalUsd
			existing.RequestedModel = mergedModelName(existing.RequestedModel, item.RequestedModel, item.EffectiveModel)
			existing.EffectiveModel = mergedModelName(existing.EffectiveModel, item.EffectiveModel, item.EffectiveModel)
			continue
		}

		clone := item
		modelsByKey[key] = &clone
	}

	models := make([]PricingModelSummary, 0, len(modelsByKey))
	for _, item := range modelsByKey {
		models = append(models, *item)
		if item.Pricing != nil {
			summary.PricedModels++
		} else {
			summary.UnpricedModels++
		}
	}

	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost.TotalUsd == models[j].Cost.TotalUsd {
			return models[i].Usage.TotalTokens > models[j].Usage.TotalTokens
		}
		return models[i].Cost.TotalUsd > models[j].Cost.TotalUsd
	})

	return PricingSnapshot{
		SourceURL:     catalog.SourceURL,
		UpdatedAt:     catalog.UpdatedAt,
		LastAttemptAt: catalog.LastAttemptAt,
		LastError:     catalog.LastError,
		Summary:       summary,
		Models:        models,
		Catalog:       clonePricingCatalog(catalog.Catalog),
		RouteCatalog:  routeCatalog,
	}
}

func pricingSummaryKey(item PricingModelSummary) string {
	groupModel := pricingGroupModel(item.RequestedModel, item.EffectiveModel)
	return strings.TrimSpace(strings.ToLower(item.DisplayModel)) + "|" + strings.TrimSpace(strings.ToLower(groupModel))
}

func pricingGroupModel(requestedModel string, effectiveModel string) string {
	if pricingModel := strings.TrimSpace(strings.ToLower(effectiveModel)); pricingModel != "" {
		return pricingModel
	}
	if pricingModel := strings.TrimSpace(strings.ToLower(requestedModel)); pricingModel != "" {
		return pricingModel
	}
	return ""
}

func mergedModelName(current string, candidate string, fallback string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	if strings.TrimSpace(candidate) != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func ResolvePricing(catalog map[string]Pricing, requestedModel string, effectiveModel string) (string, Pricing, bool) {
	for _, candidate := range pricingLookupCandidates(requestedModel, effectiveModel) {
		if pricing, ok := catalog[candidate]; ok {
			return candidate, pricing, true
		}
	}
	return "", Pricing{}, false
}

func PricingRouteKey(requestedModel string, effectiveModel string) string {
	return strings.TrimSpace(strings.ToLower(requestedModel)) + "|" + strings.TrimSpace(strings.ToLower(effectiveModel))
}

func formatPricingDisplayModel(requestedModel string, effectiveModel string) string {
	requested := strings.TrimSpace(requestedModel)
	effective := strings.TrimSpace(effectiveModel)

	switch {
	case requested == "" && effective == "":
		return "unknown"
	case effective != "":
		return effective
	case requested == "":
		return effective
	default:
		return requested
	}
}

func BootstrapPricingCatalog() map[string]Pricing {
	return map[string]Pricing{
		"gpt-5.4":                    {InputPer1MUsd: 2.50, CachedInputPer1MUsd: 0.25, OutputPer1MUsd: 15.00},
		"gpt-5.2":                    {InputPer1MUsd: 1.75, CachedInputPer1MUsd: 0.175, OutputPer1MUsd: 14.00},
		"gpt-5.2-codex":              {InputPer1MUsd: 1.75, CachedInputPer1MUsd: 0.175, OutputPer1MUsd: 14.00},
		"gpt-5.2-pro":                {InputPer1MUsd: 21.00, OutputPer1MUsd: 168.00},
		"gpt-5.2-chat-latest":        {InputPer1MUsd: 1.75, CachedInputPer1MUsd: 0.175, OutputPer1MUsd: 14.00},
		"gpt-5.2-thinking":           {InputPer1MUsd: 1.75, CachedInputPer1MUsd: 0.175, OutputPer1MUsd: 14.00},
		"claude-haiku-4-5":           {InputPer1MUsd: 0.80, OutputPer1MUsd: 4.00},
		"claude-haiku-4-5-20251001":  {InputPer1MUsd: 0.80, OutputPer1MUsd: 4.00},
		"claude-sonnet-4-5":          {InputPer1MUsd: 3.00, OutputPer1MUsd: 15.00},
		"claude-sonnet-4-5-20250929": {InputPer1MUsd: 3.00, OutputPer1MUsd: 15.00},
		"claude-sonnet-4-6":          {InputPer1MUsd: 3.00, OutputPer1MUsd: 15.00},
		"claude-opus-4-5":            {InputPer1MUsd: 15.00, OutputPer1MUsd: 75.00},
		"claude-opus-4-5-20251101":   {InputPer1MUsd: 15.00, OutputPer1MUsd: 75.00},
		"claude-opus-4-6":            {InputPer1MUsd: 15.00, OutputPer1MUsd: 75.00},
	}
}

func BootstrapPricingSnapshot() PricingCatalogSnapshot {
	return PricingCatalogSnapshot{
		SourceURL: OfficialPricingURL,
		Catalog:   BootstrapPricingCatalog(),
	}
}

func pricingLookupCandidates(requestedModel string, effectiveModel string) []string {
	var candidates []string
	seen := make(map[string]struct{})

	add := func(model string) {
		for _, candidate := range canonicalPricingAliases(model) {
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}

	add(effectiveModel)
	add(requestedModel)
	return candidates
}

func canonicalPricingAliases(model string) []string {
	model = strings.TrimSpace(strings.ToLower(model))
	if model == "" {
		return nil
	}

	aliases := []string{model}
	switch {
	case strings.HasSuffix(model, "-chat-latest"):
		aliases = append(aliases, strings.TrimSuffix(model, "-chat-latest"))
	case strings.HasSuffix(model, "-thinking"):
		aliases = append(aliases, strings.TrimSuffix(model, "-thinking"))
	}

	switch model {
	case "gpt-5.2-thinking":
		aliases = append(aliases, "gpt-5.2")
	case "gpt-5.2-chat-latest":
		aliases = append(aliases, "gpt-5.2")
	case "claude-haiku-4-5-20251001":
		aliases = append(aliases, "claude-haiku-4-5")
	case "claude-sonnet-4-5-20250929":
		aliases = append(aliases, "claude-sonnet-4-5")
	case "claude-sonnet-4-6-thinking":
		aliases = append(aliases, "claude-sonnet-4-6")
	case "claude-opus-4-5-20251101":
		aliases = append(aliases, "claude-opus-4-5")
	case "claude-opus-4-6-thinking":
		aliases = append(aliases, "claude-opus-4-6")
	}

	return aliases
}

func calculatePricingCost(usage Usage, pricing Pricing) PricingCost {
	cachedPromptTokens := normalizeCachedPromptTokens(usage)
	uncachedPromptTokens := usage.PromptTokens - cachedPromptTokens

	promptUsd := (float64(uncachedPromptTokens) / 1_000_000) * pricing.InputPer1MUsd
	if pricing.CachedInputPer1MUsd > 0 && cachedPromptTokens > 0 {
		promptUsd += (float64(cachedPromptTokens) / 1_000_000) * pricing.CachedInputPer1MUsd
	} else if cachedPromptTokens > 0 {
		promptUsd += (float64(cachedPromptTokens) / 1_000_000) * pricing.InputPer1MUsd
	}
	completionUsd := (float64(usage.CompletionTokens) / 1_000_000) * pricing.OutputPer1MUsd
	return PricingCost{
		PromptUsd:     promptUsd,
		CompletionUsd: completionUsd,
		TotalUsd:      promptUsd + completionUsd,
	}
}

func calculateCacheSavings(usage Usage, pricing Pricing) float64 {
	cachedPromptTokens := normalizeCachedPromptTokens(usage)
	if cachedPromptTokens == 0 {
		return 0
	}
	cachedRate := pricing.CachedInputPer1MUsd
	if cachedRate <= 0 {
		return 0
	}
	return (float64(cachedPromptTokens) / 1_000_000) * (pricing.InputPer1MUsd - cachedRate)
}

func normalizeCachedPromptTokens(usage Usage) int {
	cachedPromptTokens := usage.CachedPromptTokens
	if cachedPromptTokens < 0 {
		cachedPromptTokens = 0
	}
	if cachedPromptTokens > usage.PromptTokens {
		cachedPromptTokens = usage.PromptTokens
	}
	return cachedPromptTokens
}

func clonePricingCatalog(src map[string]Pricing) map[string]Pricing {
	if len(src) == 0 {
		return map[string]Pricing{}
	}
	dst := make(map[string]Pricing, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
