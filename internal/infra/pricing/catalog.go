package pricing

import (
	"context"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/telemetry"
)

// Catalog wraps the pricing catalog behind gateway types.
type Catalog struct {
	inner *telemetry.PricingCatalog
}

// Price is a model price entry.
type Price struct {
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

// Snapshot is the pricing catalog state used by admin/economics endpoints.
type Snapshot struct {
	SourceURL      string                      `json:"source_url,omitempty"`
	UpdatedAt      time.Time                   `json:"updated_at,omitempty"`
	LastAttemptAt  time.Time                   `json:"last_attempt_at,omitempty"`
	LastError      string                      `json:"last_error,omitempty"`
	Catalog        map[string]Price            `json:"catalog"`
	Sources        []SourceState               `json:"sources,omitempty"`
	FX             FXSnapshot                  `json:"fx,omitempty"`
	SourceCatalogs map[string]map[string]Price `json:"source_catalogs,omitempty"`
}

// SourceState describes the runtime refresh state of one official source.
type SourceState struct {
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

// FXSnapshot describes the runtime FX normalization status.
type FXSnapshot struct {
	Enabled       bool               `json:"enabled"`
	SourceURL     string             `json:"source_url,omitempty"`
	BaseCurrency  string             `json:"base_currency,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	LastAttemptAt time.Time          `json:"last_attempt_at,omitempty"`
	LastError     string             `json:"last_error,omitempty"`
	RatesToUSD    map[string]float64 `json:"rates_to_usd,omitempty"`
}

// NewCatalog creates a pricing catalog.
func NewCatalog(cfg core.PricingConfig) *Catalog {
	return &Catalog{
		inner: telemetry.NewPricingCatalog(cfg),
	}
}

// Start begins the background refresh loop.
func (c *Catalog) Start(ctx context.Context) {
	if c == nil || c.inner == nil {
		return
	}
	c.inner.Start(ctx)
}

// RefreshNow triggers an immediate refresh.
func (c *Catalog) RefreshNow(ctx context.Context) error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.RefreshNow(ctx)
}

// UpdateConfig updates the refresh/cache config.
func (c *Catalog) UpdateConfig(cfg core.PricingConfig) {
	if c == nil || c.inner == nil {
		return
	}
	c.inner.UpdateConfig(cfg)
}

// Snapshot returns the current catalog snapshot.
func (c *Catalog) Snapshot() Snapshot {
	if c == nil || c.inner == nil {
		return fromLegacySnapshot(telemetry.BootstrapPricingSnapshot())
	}
	return fromLegacySnapshot(c.inner.Snapshot())
}

func fromLegacySnapshot(snapshot telemetry.PricingCatalogSnapshot) Snapshot {
	catalog := make(map[string]Price, len(snapshot.Catalog))
	for key, value := range snapshot.Catalog {
		catalog[key] = Price{
			Currency:            value.Currency,
			InputPer1M:          value.InputPer1M,
			CachedInputPer1M:    value.CachedInputPer1M,
			OutputPer1M:         value.OutputPer1M,
			InputPer1MUsd:       value.InputPer1MUsd,
			CachedInputPer1MUsd: value.CachedInputPer1MUsd,
			OutputPer1MUsd:      value.OutputPer1MUsd,
			Source:              value.Source,
			SourceID:            value.SourceID,
			FXRateToUSD:         value.FXRateToUSD,
		}
	}

	sourceCatalogs := make(map[string]map[string]Price, len(snapshot.SourceCatalogs))
	for sourceID, sourceCatalog := range snapshot.SourceCatalogs {
		sourceCatalogs[sourceID] = make(map[string]Price, len(sourceCatalog))
		for key, value := range sourceCatalog {
			sourceCatalogs[sourceID][key] = Price{
				Currency:            value.Currency,
				InputPer1M:          value.InputPer1M,
				CachedInputPer1M:    value.CachedInputPer1M,
				OutputPer1M:         value.OutputPer1M,
				InputPer1MUsd:       value.InputPer1MUsd,
				CachedInputPer1MUsd: value.CachedInputPer1MUsd,
				OutputPer1MUsd:      value.OutputPer1MUsd,
				Source:              value.Source,
				SourceID:            value.SourceID,
				FXRateToUSD:         value.FXRateToUSD,
			}
		}
	}

	sources := make([]SourceState, 0, len(snapshot.Sources))
	for _, state := range snapshot.Sources {
		sources = append(sources, SourceState{
			ID:            state.ID,
			Vendor:        state.Vendor,
			URL:           state.URL,
			Enabled:       state.Enabled,
			Status:        state.Status,
			UpdatedAt:     state.UpdatedAt,
			LastAttemptAt: state.LastAttemptAt,
			LastError:     state.LastError,
			ModelCount:    state.ModelCount,
		})
	}

	return Snapshot{
		SourceURL:     snapshot.SourceURL,
		UpdatedAt:     snapshot.UpdatedAt,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastError:     snapshot.LastError,
		Catalog:       catalog,
		Sources:       sources,
		FX: FXSnapshot{
			Enabled:       snapshot.FX.Enabled,
			SourceURL:     snapshot.FX.SourceURL,
			BaseCurrency:  snapshot.FX.BaseCurrency,
			UpdatedAt:     snapshot.FX.UpdatedAt,
			LastAttemptAt: snapshot.FX.LastAttemptAt,
			LastError:     snapshot.FX.LastError,
			RatesToUSD:    cloneRates(snapshot.FX.RatesToUSD),
		},
		SourceCatalogs: sourceCatalogs,
	}
}

func cloneRates(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return nil
	}
	cloned := make(map[string]float64, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}
