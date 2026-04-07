package pricing

import (
	"context"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/telemetry"
)

// Catalog wraps the pricing catalog behind v2-facing types.
type Catalog struct {
	inner *telemetry.PricingCatalog
}

// Price is a model price entry.
type Price struct {
	InputPer1MUsd       float64 `json:"input_per_1m_usd"`
	CachedInputPer1MUsd float64 `json:"cached_input_per_1m_usd,omitempty"`
	OutputPer1MUsd      float64 `json:"output_per_1m_usd"`
}

// Snapshot is the pricing catalog state used by admin/economics endpoints.
type Snapshot struct {
	SourceURL     string           `json:"source_url,omitempty"`
	UpdatedAt     time.Time        `json:"updated_at,omitempty"`
	LastAttemptAt time.Time        `json:"last_attempt_at,omitempty"`
	LastError     string           `json:"last_error,omitempty"`
	Catalog       map[string]Price `json:"catalog"`
}

// NewCatalog creates a v2 pricing catalog.
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
			InputPer1MUsd:       value.InputPer1MUsd,
			CachedInputPer1MUsd: value.CachedInputPer1MUsd,
			OutputPer1MUsd:      value.OutputPer1MUsd,
		}
	}

	return Snapshot{
		SourceURL:     snapshot.SourceURL,
		UpdatedAt:     snapshot.UpdatedAt,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastError:     snapshot.LastError,
		Catalog:       catalog,
	}
}
