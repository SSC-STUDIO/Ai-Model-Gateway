package commands

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"ai-model-gateway/internal/cli"
)

type BenchmarkCommand struct {
	client *cli.ControlPlaneClient
	output io.Writer
}

func NewBenchmarkCommand(client *cli.ControlPlaneClient, output io.Writer) *BenchmarkCommand {
	return &BenchmarkCommand{client: client, output: output}
}

func (c *BenchmarkCommand) ImportBaseline(ctx context.Context, kind, filePath, sourceName, sourceURL, format string) error {
	if kind == "" || filePath == "" {
		return fmt.Errorf("kind and file path are required")
	}
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read baseline file: %w", err)
	}
	if sourceName == "" {
		sourceName = filePath
	}
	result, err := c.client.ImportVerificationBaseline(ctx, &cli.VerificationBaselineImportRequest{
		Kind:       kind,
		SourceName: sourceName,
		SourceURL:  sourceURL,
		FileName:   filePath,
		Contents:   string(contents),
	})
	if err != nil {
		return fmt.Errorf("import verification baseline: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(c.output, "Imported baseline %s (%s) rows=%d\n", result.SnapshotID, result.Kind, result.RowCount)
	return nil
}

func (c *BenchmarkCommand) ListBaselines(ctx context.Context, format string) error {
	result, err := c.client.ListVerificationBaselines(ctx)
	if err != nil {
		return fmt.Errorf("list verification baselines: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SNAPSHOT\tKIND\tROWS\tSOURCE")
	for _, snapshot := range result.Snapshots {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", snapshot.SnapshotID, snapshot.Kind, snapshot.RowCount, snapshot.SourceName)
	}
	return w.Flush()
}

func (c *BenchmarkCommand) ListRuns(ctx context.Context, limit int, format string) error {
	result, err := c.client.ListVerificationRuns(ctx, limit)
	if err != nil {
		return fmt.Errorf("list verification runs: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RUN ID\tSTATUS\tSUITE\tPROTOCOL\tTARGETS")
	for _, run := range result.Runs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d/%d\n", run.RunID, run.Status, run.SuiteVersion, run.Protocol, run.CompletedTargets, run.TargetCount)
	}
	return w.Flush()
}

func (c *BenchmarkCommand) Run(ctx context.Context, req *cli.VerificationRunRequest, format string) error {
	result, err := c.client.StartVerificationRun(ctx, req)
	if err != nil {
		return fmt.Errorf("start verification benchmark run: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(c.output, "Started run %s status=%s targets=%d\n", result.RunID, result.Status, result.TargetCount)
	return nil
}

func (c *BenchmarkCommand) Show(ctx context.Context, runID, format string) error {
	result, err := c.client.GetVerificationRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get verification benchmark run: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(c.output, "Run %s status=%s suite=%s protocol=%s\n", result.RunID, result.Status, result.SuiteVersion, result.Protocol)
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tVERDICT\tPUBLIC GAP\tVENDOR GAP\tSUSPICION\tSTATUS")
	for _, target := range result.Targets {
		fmt.Fprintf(w, "%s\t%s\t%s\t%.1f\t%.1f\t%.1f\t%s\n",
			target.ProviderID,
			target.PublicModel,
			target.Verdict,
			target.PublicGap,
			target.VendorGap,
			target.SuspicionScore,
			target.Status,
		)
	}
	return w.Flush()
}

func (c *BenchmarkCommand) Telemetry(ctx context.Context, runID string, query *cli.VerificationRunTelemetryQuery, format string) error {
	result, err := c.client.GetVerificationRunTelemetry(ctx, runID, query)
	if err != nil {
		return fmt.Errorf("get verification benchmark telemetry: %w", err)
	}
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if format == "csv" {
		return c.writeTelemetryCSV(result)
	}

	fmt.Fprintf(
		c.output,
		"Run %s telemetry fetched=%d available=%d truncated=%t\n",
		runID,
		len(result.Events),
		result.Total,
		result.Total > len(result.Events),
	)
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tCASE\tPROVIDER\tMODEL\tSTATUS\tLATENCY\tTOKENS\tCOST\tROUTE\tERROR")
	for _, event := range result.Events {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%d\t%dms\t%d\t$%.6f\t%s\t%s\n",
			event.Timestamp.Format("15:04:05"),
			coalesceString(event.BenchmarkCaseID, "-"),
			coalesceString(event.Provider, "-"),
			coalesceString(telemetryModel(event), "-"),
			event.StatusCode,
			event.LatencyMs,
			telemetryTotalTokens(event),
			event.TotalCostUSD,
			coalesceString(event.RouteMode, "-"),
			coalesceString(event.Error, "-"),
		)
	}
	return w.Flush()
}

type benchmarkTelemetrySummary struct {
	RunID           string                           `json:"run_id"`
	TotalEvents     int                              `json:"total_events"`
	AvailableEvents int                              `json:"available_events"`
	Truncated       bool                             `json:"truncated"`
	TotalGroups     int                              `json:"total_groups"`
	SuccessCount    int                              `json:"success_count"`
	FailureCount    int                              `json:"failure_count"`
	TotalTokens     int64                            `json:"total_tokens"`
	TotalCostUSD    float64                          `json:"total_cost_usd"`
	Groups          []benchmarkTelemetrySummaryGroup `json:"groups"`
}

type benchmarkTelemetrySummaryGroup struct {
	CaseID        string    `json:"case_id,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	Model         string    `json:"model,omitempty"`
	RouteMode     string    `json:"route_mode,omitempty"`
	PricingStatus string    `json:"pricing_status,omitempty"`
	Requests      int       `json:"requests"`
	Successes     int       `json:"successes"`
	Failures      int       `json:"failures"`
	AvgLatencyMs  float64   `json:"avg_latency_ms"`
	TotalTokens   int64     `json:"total_tokens"`
	AvgTokens     float64   `json:"avg_tokens"`
	TotalCostUSD  float64   `json:"total_cost_usd"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

func (c *BenchmarkCommand) TelemetrySummary(ctx context.Context, runID string, query *cli.VerificationRunTelemetryQuery, format string) error {
	result, err := c.client.GetVerificationRunTelemetry(ctx, runID, query)
	if err != nil {
		return fmt.Errorf("get verification benchmark telemetry: %w", err)
	}
	summary := summarizeBenchmarkTelemetry(runID, result)
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(summary)
	}
	if format == "csv" {
		return writeBenchmarkTelemetrySummaryCSV(c.output, summary)
	}

	fmt.Fprintf(
		c.output,
		"Run %s telemetry summary fetched=%d available=%d truncated=%t groups=%d success=%d failed=%d total_tokens=%d total_cost=$%.6f\n",
		summary.RunID,
		summary.TotalEvents,
		summary.AvailableEvents,
		summary.Truncated,
		summary.TotalGroups,
		summary.SuccessCount,
		summary.FailureCount,
		summary.TotalTokens,
		summary.TotalCostUSD,
	)
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CASE\tPROVIDER\tMODEL\tROUTE\tPRICING\tREQS\tOK\tERR\tAVG LATENCY\tTOKENS\tAVG TOKENS\tCOST")
	for _, group := range summary.Groups {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%.1fms\t%d\t%.1f\t$%.6f\n",
			coalesceString(group.CaseID, "-"),
			coalesceString(group.Provider, "-"),
			coalesceString(group.Model, "-"),
			coalesceString(group.RouteMode, "-"),
			coalesceString(group.PricingStatus, "-"),
			group.Requests,
			group.Successes,
			group.Failures,
			group.AvgLatencyMs,
			group.TotalTokens,
			group.AvgTokens,
			group.TotalCostUSD,
		)
	}
	return w.Flush()
}

type benchmarkTargetSummaryReport struct {
	RunID               string                         `json:"run_id"`
	Status              string                         `json:"status"`
	SuiteVersion        string                         `json:"suite_version"`
	Protocol            string                         `json:"protocol"`
	SortMode            string                         `json:"sort_mode"`
	TotalTargets        int                            `json:"total_targets"`
	TotalEvents         int                            `json:"total_events"`
	AvailableEvents     int                            `json:"available_events"`
	Truncated           bool                           `json:"truncated"`
	MatchedEvents       int                            `json:"matched_events"`
	ExactMatchedEvents  int                            `json:"exact_matched_events"`
	LegacyMatchedEvents int                            `json:"legacy_matched_events"`
	UnmatchedEvents     int                            `json:"unmatched_events"`
	TotalTokens         int64                          `json:"total_tokens"`
	TotalCostUSD        float64                        `json:"total_cost_usd"`
	Targets             []benchmarkTargetSummaryTarget `json:"targets"`
}

type benchmarkTargetSummaryTarget struct {
	TargetID                 string         `json:"target_id"`
	ProviderID               string         `json:"provider_id"`
	PublicModel              string         `json:"public_model"`
	EffectiveModel           string         `json:"effective_model,omitempty"`
	Status                   string         `json:"status"`
	Verdict                  string         `json:"verdict,omitempty"`
	PublicGap                float64        `json:"public_gap,omitempty"`
	VendorGap                float64        `json:"vendor_gap,omitempty"`
	SuspicionScore           float64        `json:"suspicion_score,omitempty"`
	CompletionRate           float64        `json:"completion_rate,omitempty"`
	CriticalProtocolFailures int            `json:"critical_protocol_failures,omitempty"`
	ReasonCodes              []string       `json:"reason_codes,omitempty"`
	TargetEstimatedCostUSD   float64        `json:"target_estimated_cost_usd,omitempty"`
	Requests                 int            `json:"requests"`
	ExactIdentityEvents      int            `json:"exact_identity_events"`
	LegacyIdentityEvents     int            `json:"legacy_identity_events"`
	Successes                int            `json:"successes"`
	Failures                 int            `json:"failures"`
	FailureRate              float64        `json:"failure_rate,omitempty"`
	AvgLatencyMs             float64        `json:"avg_latency_ms"`
	TotalTokens              int64          `json:"total_tokens"`
	TelemetryCostUSD         float64        `json:"telemetry_cost_usd"`
	RouteModeCounts          map[string]int `json:"route_mode_counts,omitempty"`
	PricingStatusCounts      map[string]int `json:"pricing_status_counts,omitempty"`
	FirstSeenAt              time.Time      `json:"first_seen_at,omitempty"`
	LastSeenAt               time.Time      `json:"last_seen_at,omitempty"`
}

func (c *BenchmarkCommand) TargetSummary(ctx context.Context, runID string, query *cli.VerificationRunTelemetryQuery, sortMode, format string) error {
	detail, err := c.client.GetVerificationRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get verification benchmark run: %w", err)
	}
	telemetry, err := c.client.GetVerificationRunTelemetry(ctx, runID, query)
	if err != nil {
		return fmt.Errorf("get verification benchmark telemetry: %w", err)
	}
	report := summarizeBenchmarkTargets(detail, telemetry, sortMode)
	if format == "json" {
		encoder := json.NewEncoder(c.output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if format == "csv" {
		return writeBenchmarkTargetSummaryCSV(c.output, report)
	}

	fmt.Fprintf(
		c.output,
		"Run %s target summary targets=%d fetched=%d available=%d truncated=%t matched=%d exact=%d legacy=%d unmatched=%d total_tokens=%d total_cost=$%.6f\n",
		report.RunID,
		report.TotalTargets,
		report.TotalEvents,
		report.AvailableEvents,
		report.Truncated,
		report.MatchedEvents,
		report.ExactMatchedEvents,
		report.LegacyMatchedEvents,
		report.UnmatchedEvents,
		report.TotalTokens,
		report.TotalCostUSD,
	)
	w := tabwriter.NewWriter(c.output, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tPUBLIC MODEL\tEFFECTIVE\tVERDICT\tPUBLIC GAP\tVENDOR GAP\tSUSPICION\tCRIT\tEXACT\tLEGACY\tFAIL RATE\tREQS\tOK\tERR\tAVG LATENCY\tTOKENS\tTELE COST\tTARGET COST\tROUTES\tPRICING\tREASONS")
	for _, target := range report.Targets {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%.1f\t%.1f\t%.1f\t%d\t%d\t%d\t%.1f%%\t%d\t%d\t%d\t%.1fms\t%d\t$%.6f\t$%.6f\t%s\t%s\t%s\n",
			coalesceString(target.ProviderID, "-"),
			coalesceString(target.PublicModel, "-"),
			coalesceString(target.EffectiveModel, "-"),
			coalesceString(target.Verdict, "-"),
			target.PublicGap,
			target.VendorGap,
			target.SuspicionScore,
			target.CriticalProtocolFailures,
			target.ExactIdentityEvents,
			target.LegacyIdentityEvents,
			target.FailureRate*100,
			target.Requests,
			target.Successes,
			target.Failures,
			target.AvgLatencyMs,
			target.TotalTokens,
			target.TelemetryCostUSD,
			target.TargetEstimatedCostUSD,
			coalesceString(formatCountMap(target.RouteModeCounts), "-"),
			coalesceString(formatCountMap(target.PricingStatusCounts), "-"),
			coalesceString(formatReasonCodes(target.ReasonCodes), "-"),
		)
	}
	return w.Flush()
}

func (c *BenchmarkCommand) writeTelemetryCSV(result *cli.TelemetryResult) error {
	writer := csv.NewWriter(c.output)
	if err := writer.Write([]string{
		"timestamp",
		"event_id",
		"request_id",
		"benchmark_run_id",
		"benchmark_target_id",
		"benchmark_case_id",
		"provider",
		"requested_model",
		"effective_model",
		"path",
		"route_mode",
		"status_code",
		"latency_ms",
		"input_tokens",
		"cached_prompt_tokens",
		"output_tokens",
		"total_tokens",
		"pricing_status",
		"total_cost_usd",
		"synthetic_kind",
		"error",
	}); err != nil {
		return fmt.Errorf("write telemetry csv header: %w", err)
	}
	for _, event := range result.Events {
		if err := writer.Write([]string{
			event.Timestamp.Format(time.RFC3339Nano),
			event.EventID,
			event.RequestID,
			event.BenchmarkRunID,
			event.BenchmarkTargetID,
			event.BenchmarkCaseID,
			event.Provider,
			event.RequestedModel,
			event.EffectiveModel,
			event.Path,
			event.RouteMode,
			fmt.Sprintf("%d", event.StatusCode),
			fmt.Sprintf("%d", event.LatencyMs),
			fmt.Sprintf("%d", event.InputTokens),
			fmt.Sprintf("%d", event.CachedPromptTokens),
			fmt.Sprintf("%d", event.OutputTokens),
			fmt.Sprintf("%d", telemetryTotalTokens(event)),
			event.PricingStatus,
			fmt.Sprintf("%.6f", event.TotalCostUSD),
			event.SyntheticKind,
			event.Error,
		}); err != nil {
			return fmt.Errorf("write telemetry csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush telemetry csv: %w", err)
	}
	return nil
}

func summarizeBenchmarkTelemetry(runID string, result *cli.TelemetryResult) *benchmarkTelemetrySummary {
	summary := &benchmarkTelemetrySummary{
		RunID:           runID,
		TotalEvents:     len(result.Events),
		AvailableEvents: result.Total,
		Truncated:       result.Total > len(result.Events),
	}
	type accumulator struct {
		benchmarkTelemetrySummaryGroup
		totalLatency int64
	}
	groups := make(map[string]*accumulator)
	for _, event := range result.Events {
		tokens := telemetryTotalTokens(event)
		summary.TotalTokens += tokens
		summary.TotalCostUSD += event.TotalCostUSD
		if event.StatusCode >= 200 && event.StatusCode < 400 {
			summary.SuccessCount++
		} else {
			summary.FailureCount++
		}

		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s",
			event.BenchmarkCaseID,
			event.Provider,
			telemetryModel(event),
			event.RouteMode,
			event.PricingStatus,
		)
		group, ok := groups[key]
		if !ok {
			group = &accumulator{
				benchmarkTelemetrySummaryGroup: benchmarkTelemetrySummaryGroup{
					CaseID:        event.BenchmarkCaseID,
					Provider:      event.Provider,
					Model:         telemetryModel(event),
					RouteMode:     event.RouteMode,
					PricingStatus: event.PricingStatus,
					FirstSeenAt:   event.Timestamp,
					LastSeenAt:    event.Timestamp,
				},
			}
			groups[key] = group
		}
		group.Requests++
		if event.StatusCode >= 200 && event.StatusCode < 400 {
			group.Successes++
		} else {
			group.Failures++
		}
		group.totalLatency += event.LatencyMs
		group.TotalTokens += tokens
		group.TotalCostUSD += event.TotalCostUSD
		if event.Timestamp.Before(group.FirstSeenAt) {
			group.FirstSeenAt = event.Timestamp
		}
		if event.Timestamp.After(group.LastSeenAt) {
			group.LastSeenAt = event.Timestamp
		}
	}

	summary.TotalGroups = len(groups)
	summary.Groups = make([]benchmarkTelemetrySummaryGroup, 0, len(groups))
	for _, group := range groups {
		if group.Requests > 0 {
			group.AvgLatencyMs = float64(group.totalLatency) / float64(group.Requests)
			group.AvgTokens = float64(group.TotalTokens) / float64(group.Requests)
		}
		summary.Groups = append(summary.Groups, group.benchmarkTelemetrySummaryGroup)
	}
	sort.Slice(summary.Groups, func(i, j int) bool {
		left := summary.Groups[i]
		right := summary.Groups[j]
		switch {
		case left.CaseID != right.CaseID:
			return left.CaseID < right.CaseID
		case left.Provider != right.Provider:
			return left.Provider < right.Provider
		case left.Model != right.Model:
			return left.Model < right.Model
		case left.RouteMode != right.RouteMode:
			return left.RouteMode < right.RouteMode
		default:
			return left.PricingStatus < right.PricingStatus
		}
	})
	return summary
}

func writeBenchmarkTelemetrySummaryCSV(output io.Writer, summary *benchmarkTelemetrySummary) error {
	writer := csv.NewWriter(output)
	if err := writer.Write([]string{
		"run_id",
		"case_id",
		"provider",
		"model",
		"route_mode",
		"pricing_status",
		"requests",
		"successes",
		"failures",
		"avg_latency_ms",
		"total_tokens",
		"avg_tokens",
		"total_cost_usd",
		"first_seen_at",
		"last_seen_at",
	}); err != nil {
		return fmt.Errorf("write telemetry summary csv header: %w", err)
	}
	for _, group := range summary.Groups {
		if err := writer.Write([]string{
			summary.RunID,
			group.CaseID,
			group.Provider,
			group.Model,
			group.RouteMode,
			group.PricingStatus,
			fmt.Sprintf("%d", group.Requests),
			fmt.Sprintf("%d", group.Successes),
			fmt.Sprintf("%d", group.Failures),
			fmt.Sprintf("%.2f", group.AvgLatencyMs),
			fmt.Sprintf("%d", group.TotalTokens),
			fmt.Sprintf("%.2f", group.AvgTokens),
			fmt.Sprintf("%.6f", group.TotalCostUSD),
			group.FirstSeenAt.Format(time.RFC3339Nano),
			group.LastSeenAt.Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("write telemetry summary csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush telemetry summary csv: %w", err)
	}
	return nil
}

func summarizeBenchmarkTargets(detail *cli.VerificationRunDetail, telemetry *cli.TelemetryResult, sortMode string) *benchmarkTargetSummaryReport {
	report := &benchmarkTargetSummaryReport{
		RunID:           detail.RunID,
		Status:          detail.Status,
		SuiteVersion:    detail.SuiteVersion,
		Protocol:        detail.Protocol,
		SortMode:        normalizeBenchmarkTargetSortMode(sortMode),
		TotalTargets:    len(detail.Targets),
		TotalEvents:     len(telemetry.Events),
		AvailableEvents: telemetry.Total,
		Truncated:       telemetry.Total > len(telemetry.Events),
		Targets:         make([]benchmarkTargetSummaryTarget, 0, len(detail.Targets)),
	}
	type accumulator struct {
		target       *benchmarkTargetSummaryTarget
		totalLatency int64
	}
	accumulators := make([]accumulator, 0, len(detail.Targets))
	targetIndexByID := make(map[string]int, len(detail.Targets))
	for _, target := range detail.Targets {
		report.Targets = append(report.Targets, benchmarkTargetSummaryTarget{
			TargetID:                 target.TargetID,
			ProviderID:               target.ProviderID,
			PublicModel:              target.PublicModel,
			EffectiveModel:           target.EffectiveModel,
			Status:                   target.Status,
			Verdict:                  target.Verdict,
			PublicGap:                target.PublicGap,
			VendorGap:                target.VendorGap,
			SuspicionScore:           target.SuspicionScore,
			CompletionRate:           target.CompletionRate,
			CriticalProtocolFailures: target.CriticalProtocolFailures,
			ReasonCodes:              append([]string(nil), target.ReasonCodes...),
			TargetEstimatedCostUSD:   target.EstimatedCostUSD,
			RouteModeCounts:          map[string]int{},
			PricingStatusCounts:      map[string]int{},
		})
	}
	for i := range report.Targets {
		accumulators = append(accumulators, accumulator{target: &report.Targets[i]})
		if targetID := strings.TrimSpace(report.Targets[i].TargetID); targetID != "" {
			targetIndexByID[targetID] = i
		}
	}

	for _, event := range telemetry.Events {
		tokens := telemetryTotalTokens(event)
		report.TotalTokens += tokens
		report.TotalCostUSD += event.TotalCostUSD

		targetIndex, identitySource := matchBenchmarkTargetEvent(targetIndexByID, detail.Targets, event)
		if targetIndex < 0 {
			report.UnmatchedEvents++
			continue
		}
		report.MatchedEvents++
		switch identitySource {
		case benchmarkTargetIdentityExact:
			report.ExactMatchedEvents++
		case benchmarkTargetIdentityLegacy:
			report.LegacyMatchedEvents++
		}

		acc := &accumulators[targetIndex]
		target := acc.target
		target.Requests++
		switch identitySource {
		case benchmarkTargetIdentityExact:
			target.ExactIdentityEvents++
		case benchmarkTargetIdentityLegacy:
			target.LegacyIdentityEvents++
		}
		if event.StatusCode >= 200 && event.StatusCode < 400 {
			target.Successes++
		} else {
			target.Failures++
		}
		acc.totalLatency += event.LatencyMs
		target.TotalTokens += tokens
		target.TelemetryCostUSD += event.TotalCostUSD
		if routeMode := strings.TrimSpace(event.RouteMode); routeMode != "" {
			target.RouteModeCounts[routeMode]++
		}
		if pricingStatus := strings.TrimSpace(event.PricingStatus); pricingStatus != "" {
			target.PricingStatusCounts[pricingStatus]++
		}
		if target.FirstSeenAt.IsZero() || event.Timestamp.Before(target.FirstSeenAt) {
			target.FirstSeenAt = event.Timestamp
		}
		if target.LastSeenAt.IsZero() || event.Timestamp.After(target.LastSeenAt) {
			target.LastSeenAt = event.Timestamp
		}
	}

	for i := range report.Targets {
		target := &report.Targets[i]
		if target.Requests > 0 {
			target.AvgLatencyMs = float64(accumulators[i].totalLatency) / float64(target.Requests)
			target.FailureRate = float64(target.Failures) / float64(target.Requests)
		}
	}
	sortBenchmarkTargetSummaryTargets(report.Targets, report.SortMode)
	return report
}

func writeBenchmarkTargetSummaryCSV(output io.Writer, report *benchmarkTargetSummaryReport) error {
	writer := csv.NewWriter(output)
	if err := writer.Write([]string{
		"run_id",
		"target_id",
		"provider_id",
		"public_model",
		"effective_model",
		"status",
		"verdict",
		"public_gap",
		"vendor_gap",
		"suspicion_score",
		"completion_rate",
		"critical_protocol_failures",
		"target_estimated_cost_usd",
		"requests",
		"exact_identity_events",
		"legacy_identity_events",
		"successes",
		"failures",
		"failure_rate",
		"avg_latency_ms",
		"total_tokens",
		"telemetry_cost_usd",
		"route_mode_counts",
		"pricing_status_counts",
		"reason_codes",
		"first_seen_at",
		"last_seen_at",
	}); err != nil {
		return fmt.Errorf("write target summary csv header: %w", err)
	}
	for _, target := range report.Targets {
		if err := writer.Write([]string{
			report.RunID,
			target.TargetID,
			target.ProviderID,
			target.PublicModel,
			target.EffectiveModel,
			target.Status,
			target.Verdict,
			fmt.Sprintf("%.1f", target.PublicGap),
			fmt.Sprintf("%.1f", target.VendorGap),
			fmt.Sprintf("%.1f", target.SuspicionScore),
			fmt.Sprintf("%.2f", target.CompletionRate),
			fmt.Sprintf("%d", target.CriticalProtocolFailures),
			fmt.Sprintf("%.6f", target.TargetEstimatedCostUSD),
			fmt.Sprintf("%d", target.Requests),
			fmt.Sprintf("%d", target.ExactIdentityEvents),
			fmt.Sprintf("%d", target.LegacyIdentityEvents),
			fmt.Sprintf("%d", target.Successes),
			fmt.Sprintf("%d", target.Failures),
			fmt.Sprintf("%.4f", target.FailureRate),
			fmt.Sprintf("%.2f", target.AvgLatencyMs),
			fmt.Sprintf("%d", target.TotalTokens),
			fmt.Sprintf("%.6f", target.TelemetryCostUSD),
			formatCountMap(target.RouteModeCounts),
			formatCountMap(target.PricingStatusCounts),
			strings.Join(target.ReasonCodes, ";"),
			formatOptionalTime(target.FirstSeenAt),
			formatOptionalTime(target.LastSeenAt),
		}); err != nil {
			return fmt.Errorf("write target summary csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush target summary csv: %w", err)
	}
	return nil
}

func normalizeBenchmarkTargetSortMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "severity":
		return "severity"
	case "provider":
		return "provider"
	case "latency":
		return "latency"
	case "cost":
		return "cost"
	default:
		return "severity"
	}
}

func sortBenchmarkTargetSummaryTargets(targets []benchmarkTargetSummaryTarget, sortMode string) {
	sortMode = normalizeBenchmarkTargetSortMode(sortMode)
	sort.Slice(targets, func(i, j int) bool {
		left := targets[i]
		right := targets[j]
		switch sortMode {
		case "provider":
			return compareBenchmarkTargetIdentity(left, right)
		case "latency":
			if left.AvgLatencyMs != right.AvgLatencyMs {
				return left.AvgLatencyMs > right.AvgLatencyMs
			}
		case "cost":
			if left.TelemetryCostUSD != right.TelemetryCostUSD {
				return left.TelemetryCostUSD > right.TelemetryCostUSD
			}
		default:
			if benchmarkVerdictSeverity(left.Verdict) != benchmarkVerdictSeverity(right.Verdict) {
				return benchmarkVerdictSeverity(left.Verdict) > benchmarkVerdictSeverity(right.Verdict)
			}
			if left.CriticalProtocolFailures != right.CriticalProtocolFailures {
				return left.CriticalProtocolFailures > right.CriticalProtocolFailures
			}
			if left.SuspicionScore != right.SuspicionScore {
				return left.SuspicionScore > right.SuspicionScore
			}
			if left.FailureRate != right.FailureRate {
				return left.FailureRate > right.FailureRate
			}
			if left.PublicGap != right.PublicGap {
				return left.PublicGap > right.PublicGap
			}
			if left.VendorGap != right.VendorGap {
				return left.VendorGap > right.VendorGap
			}
			if left.AvgLatencyMs != right.AvgLatencyMs {
				return left.AvgLatencyMs > right.AvgLatencyMs
			}
			if left.TelemetryCostUSD != right.TelemetryCostUSD {
				return left.TelemetryCostUSD > right.TelemetryCostUSD
			}
		}
		return compareBenchmarkTargetIdentity(left, right)
	})
}

func benchmarkVerdictSeverity(verdict string) int {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "highly_suspect", "high_suspect":
		return 3
	case "suspect":
		return 2
	case "incomplete":
		return 1
	default:
		return 0
	}
}

func compareBenchmarkTargetIdentity(left, right benchmarkTargetSummaryTarget) bool {
	switch {
	case left.ProviderID != right.ProviderID:
		return left.ProviderID < right.ProviderID
	case left.PublicModel != right.PublicModel:
		return left.PublicModel < right.PublicModel
	case left.EffectiveModel != right.EffectiveModel:
		return left.EffectiveModel < right.EffectiveModel
	default:
		return left.TargetID < right.TargetID
	}
}

const (
	benchmarkTargetIdentityExact   = "exact"
	benchmarkTargetIdentityLegacy  = "legacy"
	benchmarkTargetIdentityMissing = ""
)

func matchBenchmarkTargetEvent(targetIndexByID map[string]int, targets []cli.VerificationRunTarget, event cli.EventRecord) (int, string) {
	if targetID := strings.TrimSpace(event.BenchmarkTargetID); targetID != "" {
		if index, ok := targetIndexByID[targetID]; ok {
			return index, benchmarkTargetIdentityExact
		}
		return -1, benchmarkTargetIdentityMissing
	}
	index := findBenchmarkTargetIndex(targets, event)
	if index >= 0 {
		return index, benchmarkTargetIdentityLegacy
	}
	return -1, benchmarkTargetIdentityMissing
}

func findBenchmarkTargetIndex(targets []cli.VerificationRunTarget, event cli.EventRecord) int {
	provider := strings.TrimSpace(event.Provider)
	requestedModel := strings.TrimSpace(event.RequestedModel)
	effectiveModel := strings.TrimSpace(telemetryModel(event))

	for i, target := range targets {
		if provider == "" || target.ProviderID != provider {
			continue
		}
		if requestedModel != "" && effectiveModel != "" && target.PublicModel == requestedModel && target.EffectiveModel == effectiveModel {
			return i
		}
	}
	for i, target := range targets {
		if provider == "" || target.ProviderID != provider {
			continue
		}
		if requestedModel != "" && target.PublicModel == requestedModel {
			if target.EffectiveModel == "" || effectiveModel == "" {
				return i
			}
		}
	}
	for i, target := range targets {
		if provider == "" || target.ProviderID != provider {
			continue
		}
		if effectiveModel != "" && target.EffectiveModel == effectiveModel {
			return i
		}
	}
	for i, target := range targets {
		if provider == "" || target.ProviderID != provider {
			continue
		}
		if requestedModel != "" && target.PublicModel == requestedModel {
			return i
		}
	}
	return -1
}

func formatCountMap(values map[string]int) string {
	if len(values) == 0 {
		return ""
	}
	type pair struct {
		key   string
		count int
	}
	pairs := make([]pair, 0, len(values))
	for key, count := range values {
		pairs = append(pairs, pair{key: key, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].key < pairs[j].key
	})
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, fmt.Sprintf("%s=%d", pair.key, pair.count))
	}
	return strings.Join(parts, ",")
}

func formatReasonCodes(values []string) string {
	if len(values) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	sort.Strings(filtered)
	return strings.Join(filtered, ",")
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func telemetryModel(event cli.EventRecord) string {
	model := event.EffectiveModel
	if model == "" {
		model = event.RequestedModel
	}
	if model == "" {
		model = event.Model
	}
	return model
}

func telemetryTotalTokens(event cli.EventRecord) int64 {
	return event.InputTokens + event.CachedPromptTokens + event.OutputTokens
}

func coalesceString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
