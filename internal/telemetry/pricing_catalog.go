package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/config"
)

type PricingCatalog struct {
	cfg   atomic.Value
	state atomic.Value
}

type pricingPageParser struct {
	URL   string
	Parse func(string) map[string]Pricing
}

var (
	pricingCardPattern = regexp.MustCompile(`(?is)<h2 class="text-h4">([^<]+)</h2>.*?<h3[^>]*>Price</h3>(.*?)</div></div></div>`)
	priceValuePattern  = regexp.MustCompile(`(?is)(Input|Cached input|Output):<br/>\$([0-9]+(?:\.[0-9]+)?) / 1M tokens`)
	priceTablePattern  = regexp.MustCompile(`(?is)Price per million tokens.*?<table><tbody>(.*?)</tbody></table>`)
	priceRowPattern    = regexp.MustCompile(`(?is)<tr[^>]*>\s*<td[^>]*><p><b>(.*?)</b></p></td>\s*<td[^>]*><p>(.*?)</p></td>\s*<td[^>]*><p>(.*?)</p></td>\s*<td[^>]*><p>(.*?)</p></td>`)
	tagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
)

func NewPricingCatalog(cfg config.PricingConfig) *PricingCatalog {
	catalog := &PricingCatalog{}
	catalog.cfg.Store(cfg)
	state := BootstrapPricingSnapshot()
	if cached, err := loadPricingCatalogCache(cfg.CachePath); err == nil {
		state = mergePricingSnapshots(state, cached)
	} else if cfg.CachePath != "" {
		state.LastError = err.Error()
	}
	catalog.state.Store(state)
	return catalog
}

func (c *PricingCatalog) Start(ctx context.Context) {
	if c == nil {
		return
	}

	go func() {
		for {
			_ = c.refresh(ctx)

			interval := time.Duration(c.currentConfig().RefreshIntervalHours) * time.Hour
			if interval <= 0 {
				interval = 12 * time.Hour
			}

			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (c *PricingCatalog) Snapshot() PricingCatalogSnapshot {
	if c == nil {
		return BootstrapPricingSnapshot()
	}
	current := c.state.Load().(PricingCatalogSnapshot)
	current.Catalog = clonePricingCatalog(current.Catalog)
	return current
}

func (c *PricingCatalog) UpdateConfig(cfg config.PricingConfig) {
	if c == nil {
		return
	}

	previous := c.currentConfig()
	c.cfg.Store(cfg)

	if strings.TrimSpace(previous.CachePath) == strings.TrimSpace(cfg.CachePath) {
		return
	}

	current := c.Snapshot()
	switch {
	case strings.TrimSpace(cfg.CachePath) == "":
		current.LastError = ""
	case true:
		cached, err := loadPricingCatalogCache(cfg.CachePath)
		if err != nil {
			current.LastError = err.Error()
			break
		}
		current = mergePricingSnapshots(current, cached)
		current.LastError = ""
	}
	c.state.Store(current)
}

func (c *PricingCatalog) refresh(parent context.Context) error {
	if c == nil {
		return nil
	}

	cfg := c.currentConfig()
	timeout := time.Duration(cfg.RequestTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	current := c.Snapshot()
	current.LastAttemptAt = time.Now().UTC()

	fetched, err := fetchOfficialPricingCatalog(ctx)
	if err != nil {
		current.LastError = err.Error()
		c.state.Store(current)
		return err
	}

	fetched.LastAttemptAt = current.LastAttemptAt
	fetched.UpdatedAt = time.Now().UTC()
	if fetched.SourceURL == "" {
		fetched.SourceURL = OfficialPricingURL
	}
	fetched.Catalog = mergePricingCatalogs(BootstrapPricingCatalog(), fetched.Catalog)
	c.state.Store(fetched)

	if err := savePricingCatalogCache(cfg.CachePath, fetched); err != nil {
		current = fetched
		current.LastError = fmt.Sprintf("save pricing cache: %v", err)
		c.state.Store(current)
		return err
	}

	return nil
}

func (c *PricingCatalog) currentConfig() config.PricingConfig {
	if c == nil {
		return config.PricingConfig{}
	}
	value := c.cfg.Load()
	if value == nil {
		return config.PricingConfig{}
	}
	return value.(config.PricingConfig)
}

func fetchOfficialPricingCatalog(ctx context.Context) (PricingCatalogSnapshot, error) {
	pages := []pricingPageParser{
		{URL: OfficialPricingURL, Parse: parseAPIPricingPage},
		{URL: "https://openai.com/index/introducing-gpt-5-2/", Parse: parseGPT52PricingPage},
	}

	client := &http.Client{}
	catalog := make(map[string]Pricing)
	var errs []string

	for _, page := range pages {
		body, err := fetchPricingPage(ctx, client, page.URL)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", page.URL, err))
			continue
		}
		for key, value := range page.Parse(body) {
			catalog[key] = value
		}
	}

	if len(catalog) == 0 {
		return PricingCatalogSnapshot{}, fmt.Errorf("fetch official pricing: %s", strings.Join(errs, "; "))
	}

	return PricingCatalogSnapshot{
		SourceURL: OfficialPricingURL,
		Catalog:   catalog,
	}, nil
}

func fetchPricingPage(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := ioReadAllString(resp)
	if err != nil {
		return "", err
	}
	return body, nil
}

func parseAPIPricingPage(body string) map[string]Pricing {
	result := make(map[string]Pricing)

	for _, match := range pricingCardPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		models := canonicalModelNames(cleanPricingText(match[1]))
		if len(models) == 0 {
			continue
		}

		price := Pricing{}
		for _, valueMatch := range priceValuePattern.FindAllStringSubmatch(match[2], -1) {
			if len(valueMatch) < 3 {
				continue
			}
			value, err := strconv.ParseFloat(valueMatch[2], 64)
			if err != nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(valueMatch[1])) {
			case "input":
				price.InputPer1MUsd = value
			case "cached input":
				price.CachedInputPer1MUsd = value
			case "output":
				price.OutputPer1MUsd = value
			}
		}

		if price.InputPer1MUsd == 0 && price.OutputPer1MUsd == 0 {
			continue
		}
		for _, model := range models {
			result[model] = price
		}
	}

	return result
}

func parseGPT52PricingPage(body string) map[string]Pricing {
	result := make(map[string]Pricing)
	section := priceTablePattern.FindStringSubmatch(body)
	if len(section) < 2 {
		return result
	}

	for _, row := range priceRowPattern.FindAllStringSubmatch(section[1], -1) {
		if len(row) < 5 {
			continue
		}
		modelCell := cleanPricingText(row[1])
		input := parseDollarCell(row[2])
		cachedInput := parseDollarCell(row[3])
		output := parseDollarCell(row[4])
		if input == 0 && output == 0 {
			continue
		}

		price := Pricing{
			InputPer1MUsd:       input,
			CachedInputPer1MUsd: cachedInput,
			OutputPer1MUsd:      output,
		}
		for _, model := range splitModelAliases(modelCell) {
			result[model] = price
		}
	}

	return result
}

func mergePricingSnapshots(base PricingCatalogSnapshot, overlay PricingCatalogSnapshot) PricingCatalogSnapshot {
	merged := base
	merged.Catalog = mergePricingCatalogs(base.Catalog, overlay.Catalog)
	if overlay.SourceURL != "" {
		merged.SourceURL = overlay.SourceURL
	}
	if !overlay.UpdatedAt.IsZero() {
		merged.UpdatedAt = overlay.UpdatedAt
	}
	if !overlay.LastAttemptAt.IsZero() {
		merged.LastAttemptAt = overlay.LastAttemptAt
	}
	if overlay.LastError != "" {
		merged.LastError = overlay.LastError
	}
	return merged
}

func mergePricingCatalogs(base map[string]Pricing, overlay map[string]Pricing) map[string]Pricing {
	merged := clonePricingCatalog(base)
	for key, value := range overlay {
		merged[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return merged
}

func loadPricingCatalogCache(path string) (PricingCatalogSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return PricingCatalogSnapshot{}, fmt.Errorf("pricing cache path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PricingCatalogSnapshot{}, err
	}
	var snapshot PricingCatalogSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return PricingCatalogSnapshot{}, err
	}
	snapshot.Catalog = clonePricingCatalog(snapshot.Catalog)
	return snapshot, nil
}

func savePricingCatalogCache(path string, snapshot PricingCatalogSnapshot) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func canonicalModelNames(value string) []string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "gpt-5.4":
		return []string{"gpt-5.4"}
	case "gpt-5 mini":
		return []string{"gpt-5-mini"}
	case "gpt-5 nano":
		return []string{"gpt-5-nano"}
	default:
		return nil
	}
}

func splitModelAliases(value string) []string {
	value = strings.ReplaceAll(value, "\n", " ")
	parts := strings.Split(value, "/")
	aliases := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		aliases = append(aliases, part)
	}
	return aliases
}

func parseDollarCell(value string) float64 {
	value = cleanPricingText(value)
	value = strings.TrimPrefix(value, "$")
	if value == "" || value == "-" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func cleanPricingText(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return value
}

func ioReadAllString(resp *http.Response) (string, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
