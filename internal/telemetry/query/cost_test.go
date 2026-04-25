package query

import (
	"testing"
	"time"
)

func TestBuildCostEntries(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)

	rows := []costRow{
		{model: "gpt-4o-mini", promptTokens: 1000, cachedPromptTokens: 200, completionTokens: 500},
		{model: "claude-3-sonnet", promptTokens: 2000, cachedPromptTokens: 0, completionTokens: 1000},
	}

	entries := buildCostEntries(rows, start, end, "model")

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify entries exist (order may vary)
	models := make(map[string]bool)
	for _, e := range entries {
		models[e.Model] = true
	}
	if !models["gpt-4o-mini"] {
		t.Error("expected gpt-4o-mini in results")
	}
	if !models["claude-3-sonnet"] {
		t.Error("expected claude-3-sonnet in results")
	}

	// Find and verify gpt-4o-mini entry
	for _, e := range entries {
		if e.Model == "gpt-4o-mini" {
			if e.PromptTokens != 1000 {
				t.Errorf("gpt-4o-mini: expected prompt tokens 1000, got %d", e.PromptTokens)
			}
			if e.CachedTokens != 200 {
				t.Errorf("gpt-4o-mini: expected cached tokens 200, got %d", e.CachedTokens)
			}
		}
	}
}

func TestBuildCostEntriesEmpty(t *testing.T) {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)

	entries := buildCostEntries(nil, start, end, "model")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for nil rows, got %d", len(entries))
	}

	entries = buildCostEntries([]costRow{}, start, end, "model")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty rows, got %d", len(entries))
	}
}

func TestCostEntryFields(t *testing.T) {
	entry := CostEntry{
		Model:            "gpt-4o",
		ProviderID:       "openai",
		PromptTokens:     1000,
		CachedTokens:     100,
		CompletionTokens: 500,
		TotalCost:        0.05,
		Currency:         "USD",
		PeriodStart:      time.Now(),
		PeriodEnd:        time.Now().Add(24 * time.Hour),
	}

	if entry.Model != "gpt-4o" {
		t.Errorf("unexpected model: %s", entry.Model)
	}
	if entry.ProviderID != "openai" {
		t.Errorf("unexpected provider: %s", entry.ProviderID)
	}
	if entry.Currency != "USD" {
		t.Errorf("unexpected currency: %s", entry.Currency)
	}
}
