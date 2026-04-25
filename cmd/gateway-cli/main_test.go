package main

import "testing"

func TestParseBenchmarkTelemetryArgsDefaults(t *testing.T) {
	runID, query, err := parseBenchmarkTelemetryArgs([]string{"run-1"})
	if err != nil {
		t.Fatalf("parseBenchmarkTelemetryArgs returned error: %v", err)
	}
	if runID != "run-1" {
		t.Fatalf("expected run id run-1, got %q", runID)
	}
	if query.WindowHours != 24 {
		t.Fatalf("expected default window hours 24, got %d", query.WindowHours)
	}
	if query.Limit != 200 {
		t.Fatalf("expected default limit 200, got %d", query.Limit)
	}
	if query.Offset != 0 {
		t.Fatalf("expected default offset 0, got %d", query.Offset)
	}
	if query.CaseID != "" {
		t.Fatalf("expected empty case id, got %q", query.CaseID)
	}
	if len(query.Providers) != 0 {
		t.Fatalf("expected no providers, got %v", query.Providers)
	}
	if len(query.Models) != 0 {
		t.Fatalf("expected no models, got %v", query.Models)
	}
	if query.TargetID != "" {
		t.Fatalf("expected no target id, got %q", query.TargetID)
	}
}

func TestParseBenchmarkTelemetryArgsWithFilters(t *testing.T) {
	runID, query, err := parseBenchmarkTelemetryArgs([]string{
		"run-2",
		"--case", "json_schema",
		"--target", "target-2",
		"--provider", "provider-a",
		"--model", "gpt-4o",
		"--hours", "12",
		"--limit", "50",
		"--offset", "10",
	})
	if err != nil {
		t.Fatalf("parseBenchmarkTelemetryArgs returned error: %v", err)
	}
	if runID != "run-2" {
		t.Fatalf("expected run id run-2, got %q", runID)
	}
	if query.CaseID != "json_schema" {
		t.Fatalf("expected case id json_schema, got %q", query.CaseID)
	}
	if query.TargetID != "target-2" {
		t.Fatalf("expected target id target-2, got %q", query.TargetID)
	}
	if query.WindowHours != 12 {
		t.Fatalf("expected 12 window hours, got %d", query.WindowHours)
	}
	if query.Limit != 50 {
		t.Fatalf("expected limit 50, got %d", query.Limit)
	}
	if query.Offset != 10 {
		t.Fatalf("expected offset 10, got %d", query.Offset)
	}
	if len(query.Providers) != 1 || query.Providers[0] != "provider-a" {
		t.Fatalf("unexpected providers: %v", query.Providers)
	}
	if len(query.Models) != 1 || query.Models[0] != "gpt-4o" {
		t.Fatalf("unexpected models: %v", query.Models)
	}
}

func TestParseBenchmarkTelemetryArgsRequiresRunID(t *testing.T) {
	if _, _, err := parseBenchmarkTelemetryArgs(nil); err == nil {
		t.Fatal("expected error for missing run id")
	}
}

func TestParseBenchmarkTargetSummaryArgsDefaults(t *testing.T) {
	runID, query, sortMode, err := parseBenchmarkTargetSummaryArgs([]string{"run-3"})
	if err != nil {
		t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
	}
	if runID != "run-3" {
		t.Fatalf("expected run id run-3, got %q", runID)
	}
	if sortMode != "severity" {
		t.Fatalf("expected default sort severity, got %q", sortMode)
	}
	if query.WindowHours != 24 || query.Limit != 200 || query.Offset != 0 {
		t.Fatalf("unexpected default query values: %+v", query)
	}
}

func TestParseBenchmarkTargetSummaryArgsWithSort(t *testing.T) {
	runID, query, sortMode, err := parseBenchmarkTargetSummaryArgs([]string{
		"run-4",
		"--target", "target-4",
		"--provider", "provider-z",
		"--model", "gpt-5",
		"--sort", "latency",
		"--hours", "6",
		"--limit", "20",
		"--offset", "5",
	})
	if err != nil {
		t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
	}
	if runID != "run-4" {
		t.Fatalf("expected run id run-4, got %q", runID)
	}
	if sortMode != "latency" {
		t.Fatalf("expected sort mode latency, got %q", sortMode)
	}
	if query.TargetID != "target-4" {
		t.Fatalf("expected target id target-4, got %q", query.TargetID)
	}
	if query.WindowHours != 6 || query.Limit != 20 || query.Offset != 5 {
		t.Fatalf("unexpected query values: %+v", query)
	}
	if len(query.Providers) != 1 || query.Providers[0] != "provider-z" {
		t.Fatalf("unexpected providers: %v", query.Providers)
	}
	if len(query.Models) != 1 || query.Models[0] != "gpt-5" {
		t.Fatalf("unexpected models: %v", query.Models)
	}
}

func TestParseBenchmarkTargetSummaryArgsSupportsProviderAndCostSort(t *testing.T) {
	for _, sortMode := range []string{"provider", "cost"} {
		t.Run(sortMode, func(t *testing.T) {
			runID, _, gotSort, err := parseBenchmarkTargetSummaryArgs([]string{"run-sort", "--sort", sortMode})
			if err != nil {
				t.Fatalf("parseBenchmarkTargetSummaryArgs returned error: %v", err)
			}
			if runID != "run-sort" {
				t.Fatalf("expected run id run-sort, got %q", runID)
			}
			if gotSort != sortMode {
				t.Fatalf("expected sort mode %q, got %q", sortMode, gotSort)
			}
		})
	}
}

func TestParseBenchmarkTargetSummaryArgsRejectsUnknownSort(t *testing.T) {
	if _, _, _, err := parseBenchmarkTargetSummaryArgs([]string{"run-5", "--sort", "weird"}); err == nil {
		t.Fatal("expected error for unsupported sort mode")
	}
}
