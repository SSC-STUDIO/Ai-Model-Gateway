package telemetry

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/pathsecurity"
)

const pricingFXSourceURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

// defaultPricingHTTPClient is a shared HTTP client with timeout for pricing fetches.
var defaultPricingHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	},
}

type ecbEnvelope struct {
	Cube struct {
		Cube struct {
			Time  string `xml:"time,attr"`
			Rates []struct {
				Currency string  `xml:"currency,attr"`
				Rate     float64 `xml:"rate,attr"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	} `xml:"Cube"`
}

func clonePricingCatalogSnapshot(snapshot PricingCatalogSnapshot) PricingCatalogSnapshot {
	snapshot.Catalog = clonePricingCatalog(snapshot.Catalog)
	snapshot.SourceCatalogs = cloneSourceCatalogs(snapshot.SourceCatalogs)
	snapshot.Sources = clonePricingSourceStates(snapshot.Sources)
	snapshot.FX = clonePricingFXSnapshot(snapshot.FX)
	return snapshot
}

func cloneSourceCatalogs(sourceCatalogs map[string]map[string]Pricing) map[string]map[string]Pricing {
	if len(sourceCatalogs) == 0 {
		return nil
	}
	cloned := make(map[string]map[string]Pricing, len(sourceCatalogs))
	for key, catalog := range sourceCatalogs {
		cloned[key] = clonePricingCatalog(catalog)
	}
	return cloned
}

func mergeSourceCatalogs(base map[string]map[string]Pricing, overlay map[string]map[string]Pricing) map[string]map[string]Pricing {
	merged := cloneSourceCatalogs(base)
	if merged == nil {
		merged = make(map[string]map[string]Pricing)
	}
	for key, catalog := range overlay {
		merged[key] = clonePricingCatalog(catalog)
	}
	return merged
}

func clonePricingSourceStates(states []PricingSourceState) []PricingSourceState {
	if len(states) == 0 {
		return nil
	}
	cloned := make([]PricingSourceState, len(states))
	copy(cloned, states)
	return cloned
}

func clonePricingFXSnapshot(snapshot PricingFXSnapshot) PricingFXSnapshot {
	if len(snapshot.RatesToUSD) > 0 {
		rates := make(map[string]float64, len(snapshot.RatesToUSD))
		for key, value := range snapshot.RatesToUSD {
			rates[key] = value
		}
		snapshot.RatesToUSD = rates
	}
	return snapshot
}

func refreshPricingSnapshot(ctx context.Context, current PricingCatalogSnapshot, cfg core.PricingConfig, force bool) (PricingCatalogSnapshot, error) {
	now := time.Now().UTC()
	updated := mergePricingSnapshots(BootstrapPricingSnapshot(), current)
	updated.LastAttemptAt = now

	stateByID := make(map[string]PricingSourceState, len(updated.Sources))
	for _, state := range updated.Sources {
		stateByID[state.ID] = state
	}

	sourceCatalogs := cloneSourceCatalogs(updated.SourceCatalogs)
	if sourceCatalogs == nil {
		sourceCatalogs = make(map[string]map[string]Pricing)
	}

	var (
		errs       []string
		anySuccess bool
	)

	for _, source := range cfg.Sources {
		state := stateByID[source.ID]
		state.ID = source.ID
		state.Vendor = source.Vendor
		state.URL = source.URL
		state.Enabled = source.IsEnabled()

		if !source.IsEnabled() {
			state.Status = "disabled"
			state.LastError = ""
			state.ModelCount = len(sourceCatalogs[source.ID])
			stateByID[source.ID] = state
			continue
		}
		if !force && !shouldRefresh(state.LastAttemptAt, source.RefreshIntervalMinutes) {
			state.ModelCount = len(sourceCatalogs[source.ID])
			stateByID[source.ID] = state
			continue
		}

		state.LastAttemptAt = now
		sourceCtx, cancel := context.WithTimeout(ctx, core.MillisecondsToDuration(source.TimeoutMs, 15*time.Second))
		catalog, err := fetchPricingSourceCatalog(sourceCtx, source)
		cancel()
		if err != nil {
			state.Status = "error"
			state.LastError = err.Error()
			state.ModelCount = len(sourceCatalogs[source.ID])
			errs = append(errs, fmt.Sprintf("%s: %v", source.ID, err))
			stateByID[source.ID] = state
			continue
		}

		annotateSourceCatalog(catalog, source)
		sourceCatalogs[source.ID] = catalog
		state.Status = "ready"
		state.UpdatedAt = now
		state.LastError = ""
		state.ModelCount = len(catalog)
		stateByID[source.ID] = state
		anySuccess = true
	}

	fx := clonePricingFXSnapshot(updated.FX)
	fx.Enabled = cfg.FX.IsEnabled()
	if fx.Enabled && (force || shouldRefresh(fx.LastAttemptAt, cfg.FX.RefreshIntervalMinutes)) {
		fx.LastAttemptAt = now
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		liveFX, err := fetchPricingFX(fetchCtx)
		cancel()
		if err != nil {
			fx.LastError = err.Error()
			errs = append(errs, fmt.Sprintf("fx: %v", err))
		} else {
			liveFX.Enabled = true
			liveFX.LastAttemptAt = now
			liveFX.UpdatedAt = now
			fx = liveFX
			anySuccess = true
		}
	}

	updated.SourceCatalogs = sourceCatalogs
	updated.Sources = orderedSourceStates(cfg.Sources, stateByID)
	updated.FX = fx
	if anySuccess || updated.UpdatedAt.IsZero() {
		updated.UpdatedAt = now
	}
	if len(errs) > 0 {
		updated.LastError = strings.Join(errs, "; ")
	} else {
		updated.LastError = ""
	}

	updated = applyPricingConfigToSnapshot(updated, cfg)
	if len(errs) > 0 {
		return updated, fmt.Errorf("%s", updated.LastError)
	}
	return updated, nil
}

func orderedSourceStates(configured []core.PricingSourceConfig, stateByID map[string]PricingSourceState) []PricingSourceState {
	if len(configured) == 0 {
		return nil
	}
	states := make([]PricingSourceState, 0, len(configured))
	for _, source := range configured {
		state := stateByID[source.ID]
		state.ID = source.ID
		state.Vendor = source.Vendor
		state.URL = source.URL
		state.Enabled = source.IsEnabled()
		states = append(states, state)
	}
	return states
}

func hydrateSourceStates(cfg core.PricingConfig, existing []PricingSourceState, sourceCatalogs map[string]map[string]Pricing) []PricingSourceState {
	stateByID := make(map[string]PricingSourceState, len(existing))
	for _, state := range existing {
		stateByID[state.ID] = state
	}
	for _, source := range cfg.Sources {
		state := stateByID[source.ID]
		state.ID = source.ID
		state.Vendor = source.Vendor
		state.URL = source.URL
		state.Enabled = source.IsEnabled()
		state.ModelCount = len(sourceCatalogs[source.ID])
		if !source.IsEnabled() && state.Status == "" {
			state.Status = "disabled"
		}
		stateByID[source.ID] = state
	}
	return orderedSourceStates(cfg.Sources, stateByID)
}

func shouldRefresh(lastAttempt time.Time, intervalMinutes int) bool {
	if intervalMinutes <= 0 {
		intervalMinutes = 15
	}
	if lastAttempt.IsZero() {
		return true
	}
	return time.Since(lastAttempt) >= time.Duration(intervalMinutes)*time.Minute
}

func annotateSourceCatalog(catalog map[string]Pricing, source core.PricingSourceConfig) {
	for key, price := range catalog {
		price.Source = source.Vendor
		price.SourceID = source.ID
		catalog[key] = normalizePricing(price)
	}
}

func applyFXToCatalog(catalog map[string]Pricing, fx PricingFXSnapshot) map[string]Pricing {
	if len(catalog) == 0 {
		return nil
	}
	converted := make(map[string]Pricing, len(catalog))
	for key, price := range catalog {
		converted[key] = applyFXToPrice(price, fx)
	}
	return converted
}

func applyFXToPrice(price Pricing, fx PricingFXSnapshot) Pricing {
	price = normalizePricing(price)
	if !fx.Enabled {
		return price
	}
	currency := normalizePricingCurrency(price.Currency)
	switch {
	case currency == "USD":
		price.FXRateToUSD = 1
	case fx.RatesToUSD != nil:
		if rate := fx.RatesToUSD[currency]; rate > 0 {
			price.FXRateToUSD = rate
			price.InputPer1MUsd = price.InputPer1M * rate
			price.CachedInputPer1MUsd = price.CachedInputPer1M * rate
			price.OutputPer1MUsd = price.OutputPer1M * rate
		}
	}
	return normalizePricing(price)
}

func loadPricingFXCache(path string) (PricingFXSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return PricingFXSnapshot{}, fmt.Errorf("pricing fx cache path is empty")
	}
	if err := pathsecurity.ValidatePathComponent(filepath.Base(path)); err != nil {
		return PricingFXSnapshot{}, fmt.Errorf("invalid fx cache path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PricingFXSnapshot{}, err
	}
	var snapshot PricingFXSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return PricingFXSnapshot{}, err
	}
	return clonePricingFXSnapshot(snapshot), nil
}

func savePricingFXCache(path string, snapshot PricingFXSnapshot) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := pathsecurity.ValidatePathComponent(filepath.Base(path)); err != nil {
		return fmt.Errorf("invalid fx cache path: %w", err)
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

func fetchPricingFX(ctx context.Context) (PricingFXSnapshot, error) {
	body, err := fetchPricingPage(ctx, defaultPricingHTTPClient, pricingFXSourceURL)
	if err != nil {
		return PricingFXSnapshot{}, err
	}
	var envelope ecbEnvelope
	if err := xml.Unmarshal([]byte(body), &envelope); err != nil {
		return PricingFXSnapshot{}, err
	}

	rates := map[string]float64{
		"USD": 1,
	}
	usdPerEUR := 0.0
	for _, rate := range envelope.Cube.Cube.Rates {
		if strings.EqualFold(rate.Currency, "USD") && rate.Rate > 0 {
			usdPerEUR = rate.Rate
			break
		}
	}
	if usdPerEUR == 0 {
		return PricingFXSnapshot{}, fmt.Errorf("missing USD reference rate in ECB feed")
	}
	rates["EUR"] = usdPerEUR
	for _, rate := range envelope.Cube.Cube.Rates {
		currency := strings.ToUpper(strings.TrimSpace(rate.Currency))
		if currency == "" || rate.Rate <= 0 {
			continue
		}
		rates[currency] = usdPerEUR / rate.Rate
	}

	return PricingFXSnapshot{
		Enabled:      true,
		SourceURL:    pricingFXSourceURL,
		BaseCurrency: "EUR",
		RatesToUSD:   rates,
	}, nil
}
