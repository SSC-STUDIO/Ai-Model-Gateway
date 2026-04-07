package runtime

import (
	"ai-model-gateway/internal/telemetry"
	v2pricing "ai-model-gateway/internal/infra/pricing"
)

// BuildPricingEconomicsHook creates an admin hook that combines telemetry route usage
// with the live pricing catalog into a v1-compatible economics snapshot.
func BuildPricingEconomicsHook(store TelemetryStore, catalog PricingCatalog) func() (interface{}, error) {
	return func() (interface{}, error) {
		snapshot := telemetry.Snapshot{}
		if store != nil {
			rows := store.QueryModelRouteUsage(0, 500)
			snapshot.ByModelRoute = make([]telemetry.ModelRouteUsage, 0, len(rows))
			for _, row := range rows {
				totalTokens := row.TotalTokens
				if totalTokens == 0 {
					totalTokens = row.InputTokens + row.OutputTokens
				}
				snapshot.ByModelRoute = append(snapshot.ByModelRoute, telemetry.ModelRouteUsage{
					RequestedModel: row.RequestedModel,
					Model:          row.EffectiveModel,
					Usage: telemetry.Usage{
						PromptTokens:       clampInt64ToInt(row.InputTokens),
						CachedPromptTokens: clampInt64ToInt(row.CachedPromptTokens),
						CompletionTokens:   clampInt64ToInt(row.OutputTokens),
						TotalTokens:        clampInt64ToInt(totalTokens),
					},
				})
			}
		}

		catalogSnapshot := telemetry.BootstrapPricingSnapshot()
		if catalog != nil {
			catalogSnapshot = toLegacyPricingSnapshot(catalog.Snapshot())
		}
		return telemetry.BuildPricingSnapshot(snapshot, catalogSnapshot), nil
	}
}

func toLegacyPricingSnapshot(snapshot v2pricing.Snapshot) telemetry.PricingCatalogSnapshot {
	catalog := make(map[string]telemetry.Pricing, len(snapshot.Catalog))
	for key, value := range snapshot.Catalog {
		catalog[key] = telemetry.Pricing{
			InputPer1MUsd:       value.InputPer1MUsd,
			CachedInputPer1MUsd: value.CachedInputPer1MUsd,
			OutputPer1MUsd:      value.OutputPer1MUsd,
		}
	}
	return telemetry.PricingCatalogSnapshot{
		SourceURL:     snapshot.SourceURL,
		UpdatedAt:     snapshot.UpdatedAt,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastError:     snapshot.LastError,
		Catalog:       catalog,
	}
}

func clampInt64ToInt(value int64) int {
	if value <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if value > int64(maxInt) {
		return maxInt
	}
	return int(value)
}
