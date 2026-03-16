package telemetry

import (
	"math"
	"testing"
)

func TestBuildPricingSnapshotUsesEffectiveModelPricingForBridgeRoute(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-5.2",
				Model:          "gpt-5.4",
				Usage: Usage{
					PromptTokens:       1_000_000,
					CachedPromptTokens: 400_000,
					CompletionTokens:   1_000_000,
					TotalTokens:        2_000_000,
				},
			},
		},
	}

	pricing := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(pricing.Models) != 1 {
		t.Fatalf("expected 1 pricing model summary, got %d", len(pricing.Models))
	}
	if pricing.Models[0].DisplayModel != "gpt-5.4" {
		t.Fatalf("expected bridged display model to collapse to effective model, got %q", pricing.Models[0].DisplayModel)
	}
	if pricing.Models[0].PricingModel != "gpt-5.4" {
		t.Fatalf("expected pricing model gpt-5.4, got %q", pricing.Models[0].PricingModel)
	}
	if math.Abs(pricing.Models[0].Cost.TotalUsd-16.6) > 1e-9 {
		t.Fatalf("expected total usd 16.6 with effective-model cached pricing, got %v", pricing.Models[0].Cost.TotalUsd)
	}
	if pricing.Summary.CachedPromptTokens != 400000 {
		t.Fatalf("expected cached prompt tokens 400000, got %d", pricing.Summary.CachedPromptTokens)
	}
	if math.Abs(pricing.Summary.CacheSavingsUsd-0.9) > 1e-9 {
		t.Fatalf("expected cache savings usd 0.9, got %v", pricing.Summary.CacheSavingsUsd)
	}
	if _, ok := pricing.RouteCatalog[PricingRouteKey("gpt-5.2", "gpt-5.4")]; !ok {
		t.Fatalf("expected route catalog entry for bridged request")
	}
	if routePrice := pricing.RouteCatalog[PricingRouteKey("gpt-5.2", "gpt-5.4")]; routePrice.InputPer1MUsd != 2.5 || routePrice.OutputPer1MUsd != 15 {
		t.Fatalf("expected bridged route catalog to use gpt-5.4 pricing, got %+v", routePrice)
	}
}

func TestCalculatePricingCostUsesCachedPromptRate(t *testing.T) {
	cost := calculatePricingCost(Usage{
		PromptTokens:       1_000_000,
		CachedPromptTokens: 500_000,
		CompletionTokens:   100_000,
		TotalTokens:        1_100_000,
	}, Pricing{
		InputPer1MUsd:       2.50,
		CachedInputPer1MUsd: 0.25,
		OutputPer1MUsd:      15.00,
	})

	if math.Abs(cost.PromptUsd-1.375) > 1e-9 {
		t.Fatalf("expected prompt usd 1.375, got %v", cost.PromptUsd)
	}
	if math.Abs(cost.CompletionUsd-1.5) > 1e-9 {
		t.Fatalf("expected completion usd 1.5, got %v", cost.CompletionUsd)
	}
	if math.Abs(cost.TotalUsd-2.875) > 1e-9 {
		t.Fatalf("expected total usd 2.875, got %v", cost.TotalUsd)
	}
}

func TestBuildPricingSnapshotSkipsZeroUsageRows(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-5.1-codex-mini",
				Model:          "gpt-5.1-codex-mini",
				Usage:          Usage{},
			},
			{
				RequestedModel: "gpt-5.2",
				Model:          "gpt-5.2",
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		},
	}

	pricing := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(pricing.Models) != 1 {
		t.Fatalf("expected zero-usage rows to be skipped, got %d models", len(pricing.Models))
	}
	if pricing.Models[0].DisplayModel != "gpt-5.2" {
		t.Fatalf("expected remaining model to be gpt-5.2, got %q", pricing.Models[0].DisplayModel)
	}
}

func TestBuildPricingSnapshotPricesClaudeModel(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "claude-sonnet-4-6",
				Model:          "claude-sonnet-4-6",
				Usage: Usage{
					PromptTokens:     1_000_000,
					CompletionTokens: 1_000_000,
					TotalTokens:      2_000_000,
				},
			},
		},
	}

	pricing := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(pricing.Models) != 1 {
		t.Fatalf("expected 1 pricing model summary, got %d", len(pricing.Models))
	}
	if pricing.Models[0].PricingModel != "claude-sonnet-4-6" {
		t.Fatalf("expected claude pricing model, got %q", pricing.Models[0].PricingModel)
	}
	if math.Abs(pricing.Models[0].Cost.TotalUsd-18.0) > 1e-9 {
		t.Fatalf("expected claude total usd 18.0, got %v", pricing.Models[0].Cost.TotalUsd)
	}
	if pricing.Summary.PricedModels != 1 || pricing.Summary.UnpricedModels != 0 {
		t.Fatalf("expected claude model to be priced, got summary %+v", pricing.Summary)
	}
}

func TestBuildPricingSnapshotMergesLegacyDirectRows(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "",
				Model:          "gpt-5.4",
				Usage: Usage{
					PromptTokens:     100,
					CompletionTokens: 10,
					TotalTokens:      110,
				},
			},
			{
				RequestedModel: "gpt-5.4",
				Model:          "gpt-5.4",
				Usage: Usage{
					PromptTokens:       200,
					CachedPromptTokens: 50,
					CompletionTokens:   20,
					TotalTokens:        220,
				},
			},
		},
	}

	pricing := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(pricing.Models) != 1 {
		t.Fatalf("expected legacy direct rows to merge, got %d models", len(pricing.Models))
	}
	if pricing.Models[0].DisplayModel != "gpt-5.4" {
		t.Fatalf("expected merged display model gpt-5.4, got %q", pricing.Models[0].DisplayModel)
	}
	if pricing.Models[0].Usage.PromptTokens != 300 {
		t.Fatalf("expected merged prompt tokens 300, got %d", pricing.Models[0].Usage.PromptTokens)
	}
	if pricing.Models[0].Usage.CachedPromptTokens != 50 {
		t.Fatalf("expected merged cached prompt tokens 50, got %d", pricing.Models[0].Usage.CachedPromptTokens)
	}
	if pricing.Summary.PricedModels != 1 {
		t.Fatalf("expected priced model count 1, got %d", pricing.Summary.PricedModels)
	}
}

func TestBuildPricingSnapshotMergesBridgeRowsIntoEffectiveModel(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-5.2",
				Model:          "gpt-5.4",
				Usage: Usage{
					PromptTokens:     100,
					CompletionTokens: 20,
					TotalTokens:      120,
				},
			},
			{
				RequestedModel: "gpt-5.4",
				Model:          "gpt-5.4",
				Usage: Usage{
					PromptTokens:     200,
					CompletionTokens: 30,
					TotalTokens:      230,
				},
			},
		},
	}

	pricing := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(pricing.Models) != 1 {
		t.Fatalf("expected bridged and direct rows to merge into one effective model row, got %d", len(pricing.Models))
	}
	if pricing.Models[0].DisplayModel != "gpt-5.4" {
		t.Fatalf("expected merged display model gpt-5.4, got %q", pricing.Models[0].DisplayModel)
	}
	if pricing.Models[0].PricingModel != "gpt-5.4" {
		t.Fatalf("expected merged pricing model gpt-5.4, got %q", pricing.Models[0].PricingModel)
	}
	if pricing.Models[0].Usage.PromptTokens != 300 {
		t.Fatalf("expected merged prompt tokens 300, got %d", pricing.Models[0].Usage.PromptTokens)
	}
	if pricing.Models[0].Usage.CompletionTokens != 50 {
		t.Fatalf("expected merged completion tokens 50, got %d", pricing.Models[0].Usage.CompletionTokens)
	}
}

func TestResolvePricingUsesClaudeAlias(t *testing.T) {
	model, pricing, ok := ResolvePricing(BootstrapPricingCatalog(), "claude-opus-4-6-thinking", "")
	if !ok {
		t.Fatalf("expected claude thinking alias to resolve")
	}
	if model != "claude-opus-4-6" {
		t.Fatalf("expected alias to resolve to claude-opus-4-6, got %q", model)
	}
	if pricing.InputPer1MUsd != 15 || pricing.OutputPer1MUsd != 75 {
		t.Fatalf("unexpected claude opus pricing: %+v", pricing)
	}
}

func TestParseGPT52PricingPageExtractsOfficialRows(t *testing.T) {
	body := `
<h4><span>Price per million tokens</span></h4>
<table><tbody>
<tr><td><p><b>gpt-5.2 / <br/>gpt-5.2-chat-latest</b></p></td><td><p>$1.75</p></td><td><p>$0.175</p></td><td><p>$14</p></td></tr>
<tr><td><p><b>gpt-5.2-pro</b></p></td><td><p>$21</p></td><td><p>-</p></td><td><p>$168</p></td></tr>
</tbody></table>`

	catalog := parseGPT52PricingPage(body)
	if len(catalog) != 3 {
		t.Fatalf("expected 3 catalog entries, got %d", len(catalog))
	}

	got := catalog["gpt-5.2"]
	if got.InputPer1MUsd != 1.75 || got.CachedInputPer1MUsd != 0.175 || got.OutputPer1MUsd != 14 {
		t.Fatalf("unexpected gpt-5.2 pricing: %+v", got)
	}

	got = catalog["gpt-5.2-pro"]
	if got.InputPer1MUsd != 21 || got.OutputPer1MUsd != 168 {
		t.Fatalf("unexpected gpt-5.2-pro pricing: %+v", got)
	}
}
