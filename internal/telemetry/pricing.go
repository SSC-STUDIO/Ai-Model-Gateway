package telemetry

import (
	"sort"
	"strings"
	"time"
)

const OfficialPricingURL = "https://openai.com/api/pricing/"

const (
	PricingStatusFixed           = "fixed"
	PricingStatusEstimatedLegacy = "estimated_legacy"
	PricingStatusUnpriced        = "unpriced"
)

type Pricing struct {
	Currency            string  `json:"currency,omitempty"`
	InputPer1M          float64 `json:"input_per_1m"`
	CachedInputPer1M    float64 `json:"cached_input_per_1m,omitempty"`
	OutputPer1M         float64 `json:"output_per_1m"`
	InputPer1MUsd       float64 `json:"input_per_1m_usd,omitempty"`
	CachedInputPer1MUsd float64 `json:"cached_input_per_1m_usd,omitempty"`
	OutputPer1MUsd      float64 `json:"output_per_1m_usd,omitempty"`
	Source              string  `json:"source,omitempty"`
	SourceID            string  `json:"source_id,omitempty"`
	FXRateToUSD         float64 `json:"fx_rate_to_usd,omitempty"`
}

type PricingCost struct {
	Currency      string  `json:"currency,omitempty"`
	Prompt        float64 `json:"prompt"`
	Completion    float64 `json:"completion"`
	Total         float64 `json:"total"`
	PromptUsd     float64 `json:"prompt_usd,omitempty"`
	CompletionUsd float64 `json:"completion_usd,omitempty"`
	TotalUsd      float64 `json:"total_usd,omitempty"`
}

type PricingModelSummary struct {
	DisplayModel    string      `json:"display_model"`
	RequestedModel  string      `json:"requested_model,omitempty"`
	EffectiveModel  string      `json:"effective_model,omitempty"`
	Upstream        string      `json:"upstream,omitempty"`
	PricingModel    string      `json:"pricing_model,omitempty"`
	PricingStatus   string      `json:"pricing_status,omitempty"`
	PricingSourceID string      `json:"pricing_source_id,omitempty"`
	Usage           Usage       `json:"usage"`
	Pricing         *Pricing    `json:"pricing,omitempty"`
	Cost            PricingCost `json:"cost"`
}

type PricingCurrencySummary struct {
	Currency     string  `json:"currency"`
	Prompt       float64 `json:"prompt"`
	Completion   float64 `json:"completion"`
	Total        float64 `json:"total"`
	CacheSavings float64 `json:"cache_savings"`
	PricedModels int     `json:"priced_models"`
}

type PricingSummary struct {
	Currency           string                   `json:"currency"`
	Prompt             float64                  `json:"prompt"`
	Completion         float64                  `json:"completion"`
	Total              float64                  `json:"total"`
	PromptUsd          float64                  `json:"prompt_usd,omitempty"`
	CompletionUsd      float64                  `json:"completion_usd,omitempty"`
	TotalUsd           float64                  `json:"total_usd,omitempty"`
	CachedPromptTokens int64                    `json:"cached_prompt_tokens"`
	CacheSavings       float64                  `json:"cache_savings"`
	CacheSavingsUsd    float64                  `json:"cache_savings_usd,omitempty"`
	PricedModels       int                      `json:"priced_models"`
	UnpricedModels     int                      `json:"unpriced_models"`
	ExactTotalUsd      float64                  `json:"exact_total_usd,omitempty"`
	EstimatedTotalUsd  float64                  `json:"estimated_total_usd,omitempty"`
	ExactRequests      int64                    `json:"exact_requests,omitempty"`
	EstimatedRequests  int64                    `json:"estimated_requests,omitempty"`
	ExactModels        int                      `json:"exact_models,omitempty"`
	EstimatedModels    int                      `json:"estimated_models,omitempty"`
	TotalsByCurrency   []PricingCurrencySummary `json:"totals_by_currency,omitempty"`
}

type PricingCatalogSnapshot struct {
	SourceURL      string                        `json:"source_url,omitempty"`
	UpdatedAt      time.Time                     `json:"updated_at,omitempty"`
	LastAttemptAt  time.Time                     `json:"last_attempt_at,omitempty"`
	LastError      string                        `json:"last_error,omitempty"`
	Catalog        map[string]Pricing            `json:"catalog"`
	Sources        []PricingSourceState          `json:"sources,omitempty"`
	FX             PricingFXSnapshot             `json:"fx,omitempty"`
	SourceCatalogs map[string]map[string]Pricing `json:"source_catalogs,omitempty"`
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
	Sources       []PricingSourceState  `json:"sources,omitempty"`
	FX            PricingFXSnapshot     `json:"fx,omitempty"`
}

type PricingSourceState struct {
	ID            string    `json:"id"`
	Vendor        string    `json:"vendor"`
	URL           string    `json:"url,omitempty"`
	Enabled       bool      `json:"enabled"`
	Status        string    `json:"status,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	ModelCount    int       `json:"model_count,omitempty"`
}

type PricingFXSnapshot struct {
	Enabled       bool               `json:"enabled"`
	SourceURL     string             `json:"source_url,omitempty"`
	BaseCurrency  string             `json:"base_currency,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	LastAttemptAt time.Time          `json:"last_attempt_at,omitempty"`
	LastError     string             `json:"last_error,omitempty"`
	RatesToUSD    map[string]float64 `json:"rates_to_usd,omitempty"`
}

type pricingCurrencyAccumulator struct {
	Currency     string
	Prompt       float64
	Completion   float64
	Total        float64
	CacheSavings float64
	PricedModels int
}

func BuildPricingSnapshot(snapshot Snapshot, catalog PricingCatalogSnapshot) PricingSnapshot {
	if len(catalog.Catalog) == 0 {
		catalog.Catalog = BootstrapPricingCatalog()
	}

	summary := PricingSummary{Currency: "USD"}
	modelsByKey := make(map[string]*PricingModelSummary, len(snapshot.ByModelRoute))
	routeCatalog := make(map[string]Pricing, len(snapshot.ByModelRoute))
	currencyTotals := make(map[string]*pricingCurrencyAccumulator)

	addCurrencyTotals := func(currency string, prompt float64, completion float64, total float64, cacheSavings float64) {
		currency = normalizePricingCurrency(currency)
		acc, ok := currencyTotals[currency]
		if !ok {
			acc = &pricingCurrencyAccumulator{Currency: currency}
			currencyTotals[currency] = acc
		}
		acc.Prompt += prompt
		acc.Completion += completion
		acc.Total += total
		acc.CacheSavings += cacheSavings
	}

	for _, route := range snapshot.ByModelRoute {
		if route.Usage.TotalTokens <= 0 && route.Usage.PromptTokens <= 0 && route.Usage.CompletionTokens <= 0 {
			continue
		}

		requestedModel := strings.TrimSpace(route.RequestedModel)
		effectiveModel := strings.TrimSpace(route.Model)
		upstream := strings.TrimSpace(route.Upstream)
		item := PricingModelSummary{
			DisplayModel:   formatPricingDisplayModel(requestedModel, effectiveModel),
			RequestedModel: requestedModel,
			EffectiveModel: effectiveModel,
			Upstream:       upstream,
			Usage:          route.Usage,
		}

		if pricingModel, pricing, ok := ResolvePricing(catalog.Catalog, item.RequestedModel, item.EffectiveModel, item.Upstream); ok {
			item.PricingModel = pricingModel
			item.Pricing = &pricing
			item.Cost = calculatePricingCost(route.Usage, pricing)
			routeCatalog[PricingRouteKey(item.RequestedModel, item.EffectiveModel, item.Upstream)] = pricing
			cacheSavings := calculateCacheSavings(route.Usage, pricing)
			addCurrencyTotals(item.Cost.Currency, item.Cost.Prompt, item.Cost.Completion, item.Cost.Total, cacheSavings)
			summary.CachedPromptTokens += int64(normalizeCachedPromptTokens(route.Usage))
		} else {
			item.PricingModel = pricingGroupModel(item.RequestedModel, item.EffectiveModel)
		}

		key := pricingSummaryKey(item)
		if existing, ok := modelsByKey[key]; ok {
			existing.Usage.PromptTokens += item.Usage.PromptTokens
			existing.Usage.CachedPromptTokens += item.Usage.CachedPromptTokens
			existing.Usage.CompletionTokens += item.Usage.CompletionTokens
			existing.Usage.TotalTokens += item.Usage.TotalTokens
			existing.Cost.Prompt += item.Cost.Prompt
			existing.Cost.Completion += item.Cost.Completion
			existing.Cost.Total += item.Cost.Total
			existing.Cost.PromptUsd += item.Cost.PromptUsd
			existing.Cost.CompletionUsd += item.Cost.CompletionUsd
			existing.Cost.TotalUsd += item.Cost.TotalUsd
			existing.RequestedModel = mergedModelName(existing.RequestedModel, item.RequestedModel, item.EffectiveModel)
			existing.EffectiveModel = mergedModelName(existing.EffectiveModel, item.EffectiveModel, item.EffectiveModel)
			existing.Upstream = mergedModelName(existing.Upstream, item.Upstream, "")
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
			if acc, ok := currencyTotals[normalizePricingCurrency(item.Cost.Currency)]; ok {
				acc.PricedModels++
			}
		} else {
			summary.UnpricedModels++
		}
	}

	sort.Slice(models, func(i, j int) bool {
		left := pricingSortValue(models[i])
		right := pricingSortValue(models[j])
		if left == right {
			return models[i].Usage.TotalTokens > models[j].Usage.TotalTokens
		}
		return left > right
	})

	if len(currencyTotals) > 0 {
		totals := make([]PricingCurrencySummary, 0, len(currencyTotals))
		for _, acc := range currencyTotals {
			totals = append(totals, PricingCurrencySummary{
				Currency:     acc.Currency,
				Prompt:       acc.Prompt,
				Completion:   acc.Completion,
				Total:        acc.Total,
				CacheSavings: acc.CacheSavings,
				PricedModels: acc.PricedModels,
			})
		}
		sort.Slice(totals, func(i, j int) bool {
			if totals[i].Total == totals[j].Total {
				return totals[i].Currency < totals[j].Currency
			}
			return totals[i].Total > totals[j].Total
		})
		summary.TotalsByCurrency = totals
		summary.Currency = totals[0].Currency
		summary.Prompt = totals[0].Prompt
		summary.Completion = totals[0].Completion
		summary.Total = totals[0].Total
		summary.CacheSavings = totals[0].CacheSavings
		if usd, ok := currencyTotals["USD"]; ok {
			summary.PromptUsd = usd.Prompt
			summary.CompletionUsd = usd.Completion
			summary.TotalUsd = usd.Total
			summary.CacheSavingsUsd = usd.CacheSavings
		}
	}

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
	pricingKey := strings.TrimSpace(strings.ToLower(item.PricingModel))
	if pricingKey == "" {
		pricingKey = pricingGroupModel(item.RequestedModel, item.EffectiveModel)
	}
	key := strings.TrimSpace(strings.ToLower(item.DisplayModel)) + "|" + pricingKey
	if upstream := strings.TrimSpace(strings.ToLower(item.Upstream)); upstream != "" {
		key += "|" + upstream
	}
	return key
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

func ResolvePricing(catalog map[string]Pricing, requestedModel string, effectiveModel string, upstream string) (string, Pricing, bool) {
	candidates := pricingLookupCandidates(requestedModel, effectiveModel)
	if normalizedUpstream := normalizePricingAlias(upstream); normalizedUpstream != "" {
		for _, candidate := range candidates {
			scopedKey := providerScopedPricingKey(normalizedUpstream, candidate)
			if pricing, ok := catalog[scopedKey]; ok {
				return scopedKey, normalizePricing(pricing), true
			}
		}
	}
	for _, candidate := range candidates {
		if pricing, ok := catalog[candidate]; ok {
			return candidate, normalizePricing(pricing), true
		}
	}
	return "", Pricing{}, false
}

func PricingRouteKey(requestedModel string, effectiveModel string, upstream string) string {
	key := strings.TrimSpace(strings.ToLower(requestedModel)) + "|" + strings.TrimSpace(strings.ToLower(effectiveModel))
	if normalizedUpstream := strings.TrimSpace(strings.ToLower(upstream)); normalizedUpstream != "" {
		key += "|" + normalizedUpstream
	}
	return key
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
		"gpt-4.1":                    pricing(2.00, 0.50, 8.00),
		"gpt-4.1-mini":               pricing(0.40, 0.10, 1.60),
		"gpt-4.1-nano":               pricing(0.10, 0.025, 0.40),
		"gpt-4o":                     pricing(2.50, 1.25, 10.00),
		"gpt-4o-mini":                pricing(0.15, 0.075, 0.60),
		"gpt-5":                      pricing(1.25, 0.125, 10.00),
		"gpt-5-mini":                 pricing(0.25, 0.025, 2.00),
		"gpt-5-nano":                 pricing(0.05, 0.005, 0.40),
		"gpt-5-pro":                  pricing(15.00, 0, 120.00),
		"gpt-5-codex":                pricing(1.25, 0.125, 10.00),
		"gpt-5.1":                    pricing(1.25, 0.125, 10.00),
		"gpt-5.1-chat-latest":        pricing(1.25, 0.125, 10.00),
		"gpt-5.1-codex":              pricing(1.25, 0.125, 10.00),
		"gpt-5.1-codex-max":          pricing(1.25, 0.125, 10.00),
		"gpt-5.1-codex-mini":         pricing(0.25, 0.025, 2.00),
		"gpt-5.2":                    pricing(1.75, 0.175, 14.00),
		"gpt-5.2-codex":              pricing(1.75, 0.175, 14.00),
		"gpt-5.2-pro":                pricing(21.00, 0, 168.00),
		"gpt-5.2-chat-latest":        pricing(1.75, 0.175, 14.00),
		"gpt-5.2-thinking":           pricing(1.75, 0.175, 14.00),
		"gpt-5.3-codex":              pricing(1.75, 0.175, 14.00),
		"gpt-5.4":                    pricing(2.50, 0.25, 15.00),
		"gpt-5.4-mini":               pricing(0.75, 0.075, 4.50),
		"gpt-5.4-nano":               pricing(0.20, 0.020, 1.25),
		"gpt-5.5":                    pricing(5.00, 0.50, 30.00),
		"o1":                         pricing(15.00, 7.50, 60.00),
		"o1-mini":                    pricing(1.10, 0.55, 4.40),
		"o1-preview":                 pricing(15.00, 7.50, 60.00),
		"o3":                         pricing(2.00, 0.50, 8.00),
		"o3-mini":                    pricing(1.10, 0.55, 4.40),
		"o3-pro":                     pricing(20.00, 0, 80.00),
		"o4-mini":                    pricing(1.10, 0.275, 4.40),
		"claude-haiku-4-5":           pricing(0.80, 0, 4.00),
		"claude-haiku-4-5-20251001":  pricing(0.80, 0, 4.00),
		"claude-sonnet-4-5":          pricing(3.00, 0, 15.00),
		"claude-sonnet-4-5-20250929": pricing(3.00, 0, 15.00),
		"claude-sonnet-4-6":          pricing(3.00, 0, 15.00),
		"claude-opus-4-5":            pricing(15.00, 0, 75.00),
		"claude-opus-4-5-20251101":   pricing(15.00, 0, 75.00),
		"claude-opus-4-6":            pricing(15.00, 0, 75.00),
		"gemini-2.5-pro":             pricing(1.25, 0.31, 10.00),
		"gemini-2.5-pro-preview":     pricing(1.25, 0.31, 10.00),
		"gemini-2.5-flash":           pricing(0.30, 0.075, 2.50),
		"gemini-2.5-flash-lite":      pricing(0.10, 0.025, 0.40),
		"gemini-2.0-flash":           pricing(0.10, 0.025, 0.40),
		"deepseek-chat":              pricing(0.28, 0.028, 0.42),
		"deepseek-reasoner":          pricing(0.28, 0.028, 0.42),
		"kimi-k2":                    priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2-turbo":              priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2-turbo-preview":      priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2-thinking":           priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2-0711-preview":       priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2-0905-preview":       priced("CNY", 4.00, 1.00, 16.00),
		"kimi-k2.5":                  pricing(0.60, 0.10, 3.00),
		"kimi-k2.5-preview":          pricing(0.60, 0.10, 3.00),
		"kimi-k2.6":                  pricing(0.95, 0.16, 4.00),
		"glm-4.5":                    priced("CNY", 0.80, 0, 2.00),
		"glm-4.5-air":                priced("CNY", 0.80, 0, 2.00),
		"glm-4.5-flash":              priced("CNY", 0, 0, 0),
		"glm-5":                      priced("CNY", 1.00, 0.20, 3.00),
		"glm-5.1":                    priced("CNY", 1.00, 0.20, 3.00),
		"glm-5-air":                  priced("CNY", 0.50, 0.10, 1.50),
		"glm-5-flash":                priced("CNY", 0, 0, 0),
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
	model = normalizePricingAlias(model)
	if model == "" {
		return nil
	}

	aliases := make([]string, 0, 8)
	seen := map[string]struct{}{}
	queue := []string{model}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == "" {
			continue
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		aliases = append(aliases, current)

		if segment := pricingAliasTail(current); segment != "" && segment != current {
			queue = append(queue, segment)
		}
		if trimmed, ok := trimPricingSnapshotSuffix(current); ok {
			queue = append(queue, trimmed)
		}
		if trimmed, ok := trimPricingVariantSuffix(current); ok {
			queue = append(queue, trimmed)
		}
		for _, suffix := range []string{"-chat-latest", "-thinking", "-preview", "-latest", "-exp"} {
			if strings.HasSuffix(current, suffix) {
				queue = append(queue, strings.TrimSuffix(current, suffix))
			}
		}
		if strings.HasPrefix(current, "claude-") {
			for _, suffix := range []string{"-high", "-medium", "-low", "-max"} {
				if strings.HasSuffix(current, suffix) {
					queue = append(queue, strings.TrimSuffix(current, suffix))
				}
			}
		}
	}

	return aliases
}

func pricing(input float64, cachedInput float64, output float64) Pricing {
	return priced("USD", input, cachedInput, output)
}

func priced(currency string, input float64, cachedInput float64, output float64) Pricing {
	currency = normalizePricingCurrency(currency)
	price := Pricing{
		Currency:         currency,
		InputPer1M:       input,
		CachedInputPer1M: cachedInput,
		OutputPer1M:      output,
	}
	if currency == "USD" {
		price.InputPer1MUsd = input
		price.CachedInputPer1MUsd = cachedInput
		price.OutputPer1MUsd = output
	}
	return price
}

func normalizePricing(pricing Pricing) Pricing {
	pricing.Currency = normalizePricingCurrency(pricing.Currency)
	if pricing.InputPer1M == 0 && pricing.InputPer1MUsd > 0 {
		pricing.InputPer1M = pricing.InputPer1MUsd
	}
	if pricing.CachedInputPer1M == 0 && pricing.CachedInputPer1MUsd > 0 {
		pricing.CachedInputPer1M = pricing.CachedInputPer1MUsd
	}
	if pricing.OutputPer1M == 0 && pricing.OutputPer1MUsd > 0 {
		pricing.OutputPer1M = pricing.OutputPer1MUsd
	}
	if pricing.Currency == "USD" {
		if pricing.InputPer1MUsd == 0 {
			pricing.InputPer1MUsd = pricing.InputPer1M
		}
		if pricing.CachedInputPer1MUsd == 0 && pricing.CachedInputPer1M > 0 {
			pricing.CachedInputPer1MUsd = pricing.CachedInputPer1M
		}
		if pricing.OutputPer1MUsd == 0 {
			pricing.OutputPer1MUsd = pricing.OutputPer1M
		}
	}
	return pricing
}

func normalizePricingCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return "USD"
	}
	return currency
}

func normalizePricingAlias(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	if model == "" {
		return ""
	}
	model = strings.NewReplacer("_", "-", " ", "-", "\\", "/").Replace(model)
	for strings.Contains(model, "--") {
		model = strings.ReplaceAll(model, "--", "-")
	}
	return strings.Trim(model, "-")
}

func pricingAliasTail(model string) string {
	idx := strings.LastIndex(model, "/")
	if idx < 0 || idx >= len(model)-1 {
		return ""
	}
	return model[idx+1:]
}

func trimPricingSnapshotSuffix(model string) (string, bool) {
	parts := strings.Split(model, "-")
	if len(parts) >= 4 {
		last := parts[len(parts)-3:]
		if len(last[0]) == 4 && len(last[1]) == 2 && len(last[2]) == 2 &&
			isDigits(last[0]) && isDigits(last[1]) && isDigits(last[2]) {
			return strings.Join(parts[:len(parts)-3], "-"), true
		}
	}
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if len(last) == 8 && isDigits(last) {
			return strings.Join(parts[:len(parts)-1], "-"), true
		}
	}
	return "", false
}

func trimPricingVariantSuffix(model string) (string, bool) {
	if idx := strings.LastIndex(model, ":"); idx > 0 {
		return model[:idx], true
	}
	return "", false
}

func providerScopedPricingKey(upstream string, model string) string {
	upstream = normalizePricingAlias(upstream)
	model = normalizePricingAlias(model)
	if upstream == "" || model == "" {
		return ""
	}
	return "provider::" + upstream + "::" + model
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func calculatePricingCost(usage Usage, pricing Pricing) PricingCost {
	pricing = normalizePricing(pricing)
	cachedPromptTokens := normalizeCachedPromptTokens(usage)
	uncachedPromptTokens := usage.PromptTokens - cachedPromptTokens

	prompt := (float64(uncachedPromptTokens) / 1_000_000) * pricing.InputPer1M
	if pricing.CachedInputPer1M > 0 && cachedPromptTokens > 0 {
		prompt += (float64(cachedPromptTokens) / 1_000_000) * pricing.CachedInputPer1M
	} else if cachedPromptTokens > 0 {
		prompt += (float64(cachedPromptTokens) / 1_000_000) * pricing.InputPer1M
	}
	completion := (float64(usage.CompletionTokens) / 1_000_000) * pricing.OutputPer1M
	cost := PricingCost{
		Currency:   pricing.Currency,
		Prompt:     prompt,
		Completion: completion,
		Total:      prompt + completion,
	}
	if pricing.Currency == "USD" {
		cost.PromptUsd = prompt
		cost.CompletionUsd = completion
		cost.TotalUsd = cost.Total
	}
	return cost
}

func calculateCacheSavings(usage Usage, pricing Pricing) float64 {
	pricing = normalizePricing(pricing)
	cachedPromptTokens := normalizeCachedPromptTokens(usage)
	if cachedPromptTokens == 0 {
		return 0
	}
	cachedRate := pricing.CachedInputPer1M
	if cachedRate <= 0 {
		return 0
	}
	return (float64(cachedPromptTokens) / 1_000_000) * (pricing.InputPer1M - cachedRate)
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

func pricingSortValue(item PricingModelSummary) float64 {
	if item.Cost.TotalUsd > 0 {
		return item.Cost.TotalUsd
	}
	return item.Cost.Total
}

func clonePricingCatalog(src map[string]Pricing) map[string]Pricing {
	if len(src) == 0 {
		return map[string]Pricing{}
	}
	dst := make(map[string]Pricing, len(src))
	for key, value := range src {
		dst[key] = normalizePricing(value)
	}
	return dst
}
