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

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/pathsecurity"
)

type PricingCatalog struct {
	cfg   atomic.Value
	state atomic.Value
}

var (
	pricingCardPattern = regexp.MustCompile(`(?is)<h2 class="text-h4">([^<]+)</h2>.*?<h3[^>]*>Price</h3>(.*?)</div></div></div>`)
	priceValuePattern  = regexp.MustCompile(`(?is)(Input|Cached input|Output):<br/>\$([0-9]+(?:\.[0-9]+)?) / 1M tokens`)
	priceTablePattern  = regexp.MustCompile(`(?is)Price per million tokens.*?<table><tbody>(.*?)</tbody></table>`)
	priceRowPattern    = regexp.MustCompile(`(?is)<tr[^>]*>\s*<td[^>]*><p><b>(.*?)</b></p></td>\s*<td[^>]*><p>(.*?)</p></td>\s*<td[^>]*><p>(.*?)</p></td>\s*<td[^>]*><p>(.*?)</p></td>`)
	tagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
)

func NewPricingCatalog(cfg core.PricingConfig) *PricingCatalog {
	catalog := &PricingCatalog{}
	catalog.cfg.Store(cfg)
	catalog.state.Store(pricingSnapshotFromConfig(cfg))
	return catalog
}

func (c *PricingCatalog) Start(ctx context.Context) {
	if c == nil {
		return
	}

	go func() {
		timer := time.NewTicker(time.Minute)
		defer timer.Stop()
		_ = c.RefreshNow(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_, _ = c.refresh(ctx, false)
			}
		}
	}()
}

func (c *PricingCatalog) RefreshNow(ctx context.Context) error {
	if c == nil {
		return nil
	}
	_, err := c.refresh(ctx, true)
	return err
}

func (c *PricingCatalog) Snapshot() PricingCatalogSnapshot {
	if c == nil {
		return BootstrapPricingSnapshot()
	}
	current := c.state.Load().(PricingCatalogSnapshot)
	return clonePricingCatalogSnapshot(current)
}

func (c *PricingCatalog) UpdateConfig(cfg core.PricingConfig) {
	if c == nil {
		return
	}
	current := stripPricingManualOverrides(c.Snapshot(), c.currentConfig())
	c.cfg.Store(cfg)
	c.state.Store(pricingSnapshotForUpdate(current, cfg))
}

func (c *PricingCatalog) refresh(parent context.Context, force bool) (PricingCatalogSnapshot, error) {
	if c == nil {
		return BootstrapPricingSnapshot(), nil
	}

	cfg := c.currentConfig()
	timeout := core.MillisecondsToDuration(cfg.RequestTimeoutMs, 15*time.Second)

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	current := c.Snapshot()
	updated, err := refreshPricingSnapshot(ctx, current, cfg, force)
	c.state.Store(updated)
	if saveErr := savePricingCatalogCache(cfg.CachePath, updated); saveErr != nil {
		updated.LastError = fmt.Sprintf("save pricing cache: %v", saveErr)
		c.state.Store(updated)
		if err == nil {
			err = saveErr
		}
	}
	if fxErr := savePricingFXCache(cfg.FX.CachePath, updated.FX); fxErr != nil && err == nil {
		err = fxErr
	}
	return updated, err
}

func (c *PricingCatalog) currentConfig() core.PricingConfig {
	if c == nil {
		return core.PricingConfig{}
	}
	value := c.cfg.Load()
	if value == nil {
		return core.PricingConfig{}
	}
	return value.(core.PricingConfig)
}

func fetchOfficialPricingCatalog(ctx context.Context) (PricingCatalogSnapshot, error) {
	catalog, err := fetchPricingSourceCatalog(ctx, core.PricingSourceConfig{
		ID:                     "openai",
		Vendor:                 "openai",
		URL:                    OfficialPricingURL,
		RefreshIntervalMinutes: 15,
		TimeoutMs:              15000,
	})
	if err != nil {
		return PricingCatalogSnapshot{}, err
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

		price := pricing(0, 0, 0)
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
				price.InputPer1M = value
				price.InputPer1MUsd = value
			case "cached input":
				price.CachedInputPer1M = value
				price.CachedInputPer1MUsd = value
			case "output":
				price.OutputPer1M = value
				price.OutputPer1MUsd = value
			}
		}

		if price.InputPer1M == 0 && price.OutputPer1M == 0 {
			continue
		}
		for _, model := range models {
			result[model] = normalizePricing(price)
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

		price := pricing(input, cachedInput, output)
		for _, model := range splitModelAliases(modelCell) {
			result[model] = price
		}
	}

	return result
}

func mergePricingSnapshots(base PricingCatalogSnapshot, overlay PricingCatalogSnapshot) PricingCatalogSnapshot {
	merged := base
	merged.Catalog = mergePricingCatalogs(base.Catalog, overlay.Catalog)
	merged.SourceCatalogs = mergeSourceCatalogs(base.SourceCatalogs, overlay.SourceCatalogs)
	merged.Sources = clonePricingSourceStates(overlay.Sources)
	if len(merged.Sources) == 0 {
		merged.Sources = clonePricingSourceStates(base.Sources)
	}
	if overlay.FX.Enabled || overlay.FX.SourceURL != "" || overlay.FX.LastError != "" || len(overlay.FX.RatesToUSD) > 0 {
		merged.FX = clonePricingFXSnapshot(overlay.FX)
	} else if merged.FX.RatesToUSD == nil {
		merged.FX = clonePricingFXSnapshot(base.FX)
	}
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
		normalizedKey := normalizePricingAlias(key)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "provider::") {
			normalizedKey = strings.ToLower(strings.TrimSpace(key))
		}
		if normalizedKey == "" {
			continue
		}
		merged[normalizedKey] = normalizePricing(value)
	}
	return merged
}

func loadPricingCatalogCache(path string) (PricingCatalogSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return PricingCatalogSnapshot{}, fmt.Errorf("pricing cache path is empty")
	}
	// SECURITY FIX: 验证路径安全
	if err := pathsecurity.ValidatePathComponent(filepath.Base(path)); err != nil {
		return PricingCatalogSnapshot{}, fmt.Errorf("invalid cache path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PricingCatalogSnapshot{}, err
	}
	var snapshot PricingCatalogSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return PricingCatalogSnapshot{}, err
	}
	return clonePricingCatalogSnapshot(snapshot), nil
}

func savePricingCatalogCache(path string, snapshot PricingCatalogSnapshot) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	// SECURITY FIX: 验证路径安全
	if err := pathsecurity.ValidatePathComponent(filepath.Base(path)); err != nil {
		return fmt.Errorf("invalid cache path: %w", err)
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
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return nil
	}
	value = strings.NewReplacer("—", "-", "–", "-", "_", " ").Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return nil
	}

	slug := strings.ReplaceAll(value, " ", "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")

	switch {
	case strings.HasPrefix(slug, "gpt-"),
		strings.HasPrefix(slug, "o1"),
		strings.HasPrefix(slug, "o3"),
		strings.HasPrefix(slug, "o4"),
		strings.HasPrefix(slug, "claude-"),
		strings.HasPrefix(slug, "gemini-"),
		strings.HasPrefix(slug, "deepseek-"),
		strings.HasPrefix(slug, "kimi-"),
		strings.HasPrefix(slug, "glm-"),
		strings.HasPrefix(slug, "grok-"),
		strings.HasPrefix(slug, "step-"),
		strings.HasPrefix(slug, "minimax-"),
		strings.HasPrefix(slug, "mimo-"):
		return []string{slug}
	default:
		return nil
	}
}

func pricingSnapshotFromConfig(cfg core.PricingConfig) PricingCatalogSnapshot {
	snapshot := BootstrapPricingSnapshot()
	if cached, err := loadPricingCatalogCache(cfg.CachePath); err == nil {
		snapshot = mergePricingSnapshots(snapshot, cached)
	} else if strings.TrimSpace(cfg.CachePath) != "" {
		snapshot.LastError = err.Error()
	}
	if fx, err := loadPricingFXCache(cfg.FX.CachePath); err == nil {
		snapshot.FX = clonePricingFXSnapshot(fx)
	} else if strings.TrimSpace(cfg.FX.CachePath) != "" && snapshot.LastError == "" {
		snapshot.LastError = err.Error()
	}
	return applyPricingConfigToSnapshot(snapshot, cfg)
}

func pricingSnapshotForUpdate(current PricingCatalogSnapshot, cfg core.PricingConfig) PricingCatalogSnapshot {
	snapshot := BootstrapPricingSnapshot()
	if cached, err := loadPricingCatalogCache(cfg.CachePath); err == nil {
		snapshot = mergePricingSnapshots(snapshot, cached)
	} else if strings.TrimSpace(cfg.CachePath) != "" {
		snapshot.LastError = err.Error()
	}
	if fx, err := loadPricingFXCache(cfg.FX.CachePath); err == nil {
		snapshot.FX = clonePricingFXSnapshot(fx)
	}
	snapshot = mergePricingSnapshots(snapshot, current)
	updated := applyPricingConfigToSnapshot(snapshot, cfg)
	if len(updated.SourceCatalogs) == 0 {
		updated.Catalog = preserveLiveCatalogEntries(updated.Catalog, current.Catalog)
	}
	return updated
}

func stripPricingManualOverrides(snapshot PricingCatalogSnapshot, cfg core.PricingConfig) PricingCatalogSnapshot {
	snapshot.Catalog = clonePricingCatalog(snapshot.Catalog)
	for _, manual := range cfg.ManualPrices {
		if !manual.IsEnabled() {
			continue
		}
		modelKey := normalizePricingAlias(manual.Model)
		if modelKey == "" {
			continue
		}
		delete(snapshot.Catalog, modelKey)
		if provider := strings.TrimSpace(manual.Provider); provider != "" {
			if scopedKey := providerScopedPricingKey(provider, modelKey); scopedKey != "" {
				delete(snapshot.Catalog, scopedKey)
			}
		}
	}
	return snapshot
}

func applyPricingConfigToSnapshot(snapshot PricingCatalogSnapshot, cfg core.PricingConfig) PricingCatalogSnapshot {
	sourceCatalogs := filterSourceCatalogsForConfig(snapshot.SourceCatalogs, cfg.Sources)
	combined := BootstrapPricingCatalog()
	for _, source := range cfg.Sources {
		if !source.IsEnabled() {
			continue
		}
		catalog := sourceCatalogs[source.ID]
		combined = mergePricingCatalogs(combined, applyFXToCatalog(catalog, snapshot.FX))
	}
	snapshot.Catalog = combined
	snapshot.SourceCatalogs = sourceCatalogs
	for _, manual := range cfg.ManualPrices {
		if !manual.IsEnabled() {
			continue
		}
		modelKey := normalizePricingAlias(manual.Model)
		if modelKey == "" {
			continue
		}
		key := modelKey
		if provider := strings.TrimSpace(manual.Provider); provider != "" {
			scopedKey := providerScopedPricingKey(provider, modelKey)
			if scopedKey != "" {
				key = scopedKey
			}
		}
		price := priced(manual.Currency, manual.InputPer1M, manual.CachedInputPer1M, manual.OutputPer1M)
		price.Source = strings.TrimSpace(manual.Source)
		if price.Source == "" {
			price.Source = "manual"
		}
		price.SourceID = "manual"
		if snapshot.FX.Enabled {
			price = applyFXToPrice(price, snapshot.FX)
		}
		snapshot.Catalog[key] = normalizePricing(price)
	}
	snapshot.Sources = hydrateSourceStates(cfg, snapshot.Sources, snapshot.SourceCatalogs)
	return snapshot
}

func filterSourceCatalogsForConfig(sourceCatalogs map[string]map[string]Pricing, configured []core.PricingSourceConfig) map[string]map[string]Pricing {
	if len(sourceCatalogs) == 0 || len(configured) == 0 {
		return nil
	}
	filtered := make(map[string]map[string]Pricing, len(configured))
	for _, source := range configured {
		catalog, ok := sourceCatalogs[source.ID]
		if !ok {
			continue
		}
		filtered[source.ID] = clonePricingCatalog(catalog)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func preserveLiveCatalogEntries(base map[string]Pricing, live map[string]Pricing) map[string]Pricing {
	if len(live) == 0 {
		return base
	}
	merged := clonePricingCatalog(base)
	for key, price := range live {
		normalizedKey := normalizePricingAlias(key)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "provider::") {
			normalizedKey = strings.ToLower(strings.TrimSpace(key))
		}
		if normalizedKey == "" {
			continue
		}
		if _, exists := merged[normalizedKey]; exists {
			continue
		}
		merged[normalizedKey] = normalizePricing(price)
	}
	return merged
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
