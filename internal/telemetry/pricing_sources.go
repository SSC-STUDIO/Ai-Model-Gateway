package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"ai-model-gateway/internal/core"
)

type pricingSourceFetcher func(ctx context.Context, client *http.Client, source core.PricingSourceConfig) (map[string]Pricing, error)

var (
	htmlTableRowPattern  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	htmlTableCellPattern = regexp.MustCompile(`(?is)<t[dh][^>]*>(.*?)</t[dh]>`)
	moneyValuePattern    = regexp.MustCompile(`(?i)(?:\$|us\$|usd|￥|¥|cny|rmb|元)\s*([0-9]+(?:\.[0-9]+)?)`)
)

var pricingSourceFetchers = map[string]pricingSourceFetcher{
	"openai":    fetchOpenAISourceCatalog,
	"anthropic": fetchSinglePageCatalog("USD", parseGenericPricingTables),
	"gemini":    fetchSinglePageCatalog("USD", parseGenericPricingTables),
	"moonshot":  fetchSinglePageCatalog("CNY", parseGenericPricingTables),
	"zhipu":     fetchSinglePageCatalog("CNY", parseGenericPricingTables),
	"minimax":   fetchSinglePageCatalog("CNY", parseGenericPricingTables),
	"deepseek":  fetchSinglePageCatalog("USD", parseGenericPricingTables),
	"xai":       fetchSinglePageCatalog("USD", parseGenericPricingTables),
	"step":      fetchSinglePageCatalog("CNY", parseGenericPricingTables),
	"xiaomi":    fetchSinglePageCatalog("CNY", parseGenericPricingTables),
}

func fetchPricingSourceCatalog(ctx context.Context, source core.PricingSourceConfig) (map[string]Pricing, error) {
	fetcher := pricingSourceFetchers[strings.ToLower(strings.TrimSpace(source.Vendor))]
	if fetcher == nil {
		return nil, fmt.Errorf("unsupported pricing vendor %q", source.Vendor)
	}
	return fetcher(ctx, defaultPricingHTTPClient, source)
}

func fetchOpenAISourceCatalog(ctx context.Context, client *http.Client, source core.PricingSourceConfig) (map[string]Pricing, error) {
	pages := []struct {
		URL   string
		Parse func(string) map[string]Pricing
	}{
		{URL: source.URL, Parse: parseAPIPricingPage},
		{URL: "https://openai.com/index/introducing-gpt-5-2/", Parse: parseGPT52PricingPage},
	}
	catalog := make(map[string]Pricing)
	var errs []string
	for _, page := range pages {
		body, err := fetchPricingPage(ctx, client, page.URL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", page.URL, err))
			continue
		}
		for key, value := range page.Parse(body) {
			catalog[key] = normalizePricing(value)
		}
	}
	if len(catalog) == 0 {
		return nil, fmt.Errorf("fetch official pricing: %s", strings.Join(errs, "; "))
	}
	return catalog, nil
}

func fetchSinglePageCatalog(defaultCurrency string, parser func(string, string) map[string]Pricing) pricingSourceFetcher {
	return func(ctx context.Context, client *http.Client, source core.PricingSourceConfig) (map[string]Pricing, error) {
		if strings.TrimSpace(source.URL) == "" {
			return nil, fmt.Errorf("pricing source %q missing URL", source.ID)
		}
		body, err := fetchPricingPage(ctx, client, source.URL)
		if err != nil {
			return nil, err
		}
		catalog := parser(body, defaultCurrency)
		if len(catalog) == 0 {
			return nil, fmt.Errorf("no pricing rows parsed from %s", source.URL)
		}
		return catalog, nil
	}
}

func parseGenericPricingTables(body string, defaultCurrency string) map[string]Pricing {
	result := make(map[string]Pricing)
	for _, row := range htmlTableRowPattern.FindAllStringSubmatch(body, -1) {
		if len(row) < 2 {
			continue
		}
		cellMatches := htmlTableCellPattern.FindAllStringSubmatch(row[1], -1)
		if len(cellMatches) < 2 {
			continue
		}
		cells := make([]string, 0, len(cellMatches))
		for _, cell := range cellMatches {
			if len(cell) < 2 {
				continue
			}
			cells = append(cells, cleanPricingText(cell[1]))
		}
		if len(cells) < 2 {
			continue
		}
		models := parseModelAliasesFromCell(cells[0])
		if len(models) == 0 {
			continue
		}
		price, ok := extractPriceFromCells(cells[1:], defaultCurrency)
		if !ok {
			continue
		}
		for _, model := range models {
			result[model] = normalizePricing(price)
		}
	}
	return result
}

func parseModelAliasesFromCell(value string) []string {
	if aliases := splitModelAliases(value); len(aliases) > 0 {
		return aliases
	}
	return canonicalModelNames(value)
}

func extractPriceFromCells(cells []string, defaultCurrency string) (Pricing, bool) {
	price := Pricing{Currency: defaultCurrency}
	ordered := make([]float64, 0, len(cells))
	for _, cell := range cells {
		if strings.TrimSpace(cell) == "" {
			continue
		}
		lower := strings.ToLower(cell)
		currency := inferCurrency(cell, defaultCurrency)
		values := extractMoneyValues(cell)
		if len(values) == 0 {
			continue
		}
		price.Currency = currency
		switch {
		case strings.Contains(lower, "cache"), strings.Contains(lower, "缓存"):
			if price.CachedInputPer1M == 0 {
				price.CachedInputPer1M = values[0]
			}
		case strings.Contains(lower, "output"), strings.Contains(lower, "completion"), strings.Contains(lower, "生成"):
			if price.OutputPer1M == 0 {
				price.OutputPer1M = values[0]
			}
		case strings.Contains(lower, "input"), strings.Contains(lower, "prompt"), strings.Contains(lower, "输入"):
			if price.InputPer1M == 0 {
				price.InputPer1M = values[0]
			}
		default:
			ordered = append(ordered, values...)
		}
	}
	if price.InputPer1M == 0 && len(ordered) > 0 {
		price.InputPer1M = ordered[0]
	}
	if price.OutputPer1M == 0 {
		switch len(ordered) {
		case 0:
		case 1:
			price.OutputPer1M = ordered[0]
		default:
			price.OutputPer1M = ordered[len(ordered)-1]
		}
	}
	if price.CachedInputPer1M == 0 && len(ordered) >= 3 {
		price.CachedInputPer1M = ordered[1]
	}
	if price.InputPer1M == 0 && price.OutputPer1M == 0 {
		return Pricing{}, false
	}
	if normalizePricingCurrency(price.Currency) == "USD" {
		price.InputPer1MUsd = price.InputPer1M
		price.CachedInputPer1MUsd = price.CachedInputPer1M
		price.OutputPer1MUsd = price.OutputPer1M
	}
	return normalizePricing(price), true
}

func inferCurrency(value string, fallback string) string {
	switch {
	case strings.Contains(value, "￥"), strings.Contains(value, "¥"), strings.Contains(strings.ToUpper(value), "CNY"), strings.Contains(value, "元"):
		return "CNY"
	case strings.Contains(value, "$"), strings.Contains(strings.ToUpper(value), "USD"):
		return "USD"
	default:
		return normalizePricingCurrency(fallback)
	}
}

func extractMoneyValues(value string) []float64 {
	matches := moneyValuePattern.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	values := make([]float64, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		parsed, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		values = append(values, parsed)
	}
	return values
}
