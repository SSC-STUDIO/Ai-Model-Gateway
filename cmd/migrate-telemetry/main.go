package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/telemetry/eventlog"
	"ai-model-gateway/internal/telemetry/project"
	"ai-model-gateway/internal/telemetry/query"
	_ "modernc.org/sqlite"
)

const (
	modeSnapshot    = "snapshot"
	modeIncremental = "incremental"
	modeFinal       = "final"
)

type config struct {
	source          string
	destDataDir     string
	mode            string
	checkpoint      string
	report          string
	adjustmentsPath string
	dryRun          bool
}

type checkpointFile struct {
	SourceDBFingerprint string    `json:"source_db_fingerprint"`
	LastSourceID        int64     `json:"last_source_id"`
	UpdatedAt           time.Time `json:"updated_at"`
	Mode                string    `json:"mode"`
}

type report struct {
	Source              string     `json:"source"`
	DestDataDir         string     `json:"dest_data_dir"`
	Mode                string     `json:"mode"`
	DryRun              bool       `json:"dry_run"`
	StartedAt           time.Time  `json:"started_at"`
	FinishedAt          time.Time  `json:"finished_at"`
	SourceDBFingerprint string     `json:"source_db_fingerprint"`
	SourceHighWatermark int64      `json:"source_high_watermark"`
	PreviousCheckpoint  int64      `json:"previous_checkpoint"`
	NewCheckpoint       int64      `json:"new_checkpoint"`
	RowsRead            int64      `json:"rows_read"`
	AdjustmentsRead     int64      `json:"adjustments_read"`
	EventsPrepared      int64      `json:"events_prepared"`
	EventsAccepted      int        `json:"events_accepted"`
	EventsDuplicate     int64      `json:"events_duplicate"`
	EventsDropped       int        `json:"events_dropped"`
	Projected           int64      `json:"projected"`
	BlankFields         blankStats `json:"blank_fields"`
	Checksum            string     `json:"checksum"`
	Warnings            []string   `json:"warnings,omitempty"`
}

type blankStats struct {
	Timestamp      int64 `json:"timestamp"`
	RequestID      int64 `json:"request_id"`
	Path           int64 `json:"path"`
	RequestedModel int64 `json:"requested_model"`
	EffectiveModel int64 `json:"effective_model"`
	ProviderID     int64 `json:"provider_id"`
	RouteMode      int64 `json:"route_mode"`
}

type legacyRequest struct {
	ID                       int64
	Timestamp                time.Time
	RequestID                string
	Path                     string
	RequestedModel           string
	EffectiveModel           string
	ProviderID               string
	RouteMode                string
	StatusCode               int
	Attempts                 int
	DurationMs               int64
	Error                    string
	PromptTokens             int64
	CachedPromptTokens       int64
	CompletionTokens         int64
	PricingStatus            string
	PricingSourceID          string
	PricingCurrency          string
	PricingInputPer1M        float64
	PricingCachedInputPer1M  float64
	PricingOutputPer1M       float64
	PricingPromptCostUSD     float64
	PricingCompletionCostUSD float64
	PricingTotalCostUSD      float64
	SyntheticKind            string
}

type usageAdjustmentFile struct {
	Adjustments []usageAdjustment `json:"adjustments"`
}

type usageAdjustment struct {
	ID                 string  `json:"id"`
	Timestamp          string  `json:"timestamp"`
	ProviderID         string  `json:"provider_id"`
	RequestedModel     string  `json:"requested_model"`
	EffectiveModel     string  `json:"effective_model"`
	RouteMode          string  `json:"route_mode"`
	Path               string  `json:"path"`
	PromptTokens       int64   `json:"prompt_tokens"`
	CachedPromptTokens int64   `json:"cached_prompt_tokens"`
	CompletionTokens   int64   `json:"completion_tokens"`
	InputPer1M         float64 `json:"input_per_1m"`
	CachedInputPer1M   float64 `json:"cached_input_per_1m"`
	OutputPer1M        float64 `json:"output_per_1m"`
	SourceID           string  `json:"source_id"`
}

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := migrate(context.Background(), cfg, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("migrate-telemetry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := config{}
	fs.StringVar(&cfg.source, "source", "", "legacy telemetry SQLite database")
	fs.StringVar(&cfg.destDataDir, "dest-data-dir", "", "destination telemetry data directory")
	fs.StringVar(&cfg.mode, "mode", modeSnapshot, "migration mode: snapshot, incremental, or final")
	fs.StringVar(&cfg.checkpoint, "checkpoint", "", "checkpoint JSON path")
	fs.StringVar(&cfg.report, "report", "", "report JSON path")
	fs.StringVar(&cfg.adjustmentsPath, "usage-adjustments", "", "optional JSON file with synthetic usage adjustments")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "read and report without writing destination databases or checkpoint")
	if err := fs.Parse(args[1:]); err != nil {
		return cfg, err
	}
	if cfg.source == "" {
		return cfg, errors.New("-source is required")
	}
	if cfg.destDataDir == "" {
		return cfg, errors.New("-dest-data-dir is required")
	}
	switch cfg.mode {
	case modeSnapshot, modeIncremental, modeFinal:
	default:
		return cfg, fmt.Errorf("invalid -mode %q", cfg.mode)
	}
	if cfg.checkpoint == "" {
		cfg.checkpoint = filepath.Join(cfg.destDataDir, "migrate-telemetry.checkpoint.json")
	}
	return cfg, nil
}

func migrate(ctx context.Context, cfg config, stdout io.Writer) error {
	startedAt := time.Now().UTC()
	sourcePath, err := filepath.Abs(cfg.source)
	if err != nil {
		return err
	}
	destDir, err := filepath.Abs(cfg.destDataDir)
	if err != nil {
		return err
	}
	fingerprint, err := fileFingerprint(sourcePath)
	if err != nil {
		return fmt.Errorf("fingerprint source: %w", err)
	}

	rep := report{
		Source:              sourcePath,
		DestDataDir:         destDir,
		Mode:                cfg.mode,
		DryRun:              cfg.dryRun,
		StartedAt:           startedAt,
		SourceDBFingerprint: fingerprint,
		Checksum:            "",
	}
	checksum := sha256.New()

	cp, err := loadCheckpoint(cfg.checkpoint)
	if err != nil {
		return err
	}
	if cp.SourceDBFingerprint != "" && cp.SourceDBFingerprint != fingerprint {
		rep.Warnings = append(rep.Warnings, "checkpoint fingerprint differs from source database")
		fingerprint = cp.SourceDBFingerprint
		rep.SourceDBFingerprint = fingerprint
	}
	rep.PreviousCheckpoint = cp.LastSourceID
	startAfter := cp.LastSourceID
	if cfg.mode == modeSnapshot && cp.LastSourceID == 0 {
		startAfter = 0
	}

	db, err := openSource(sourcePath)
	if err != nil {
		return err
	}
	defer db.Close()

	columns, err := requestColumns(db)
	if err != nil {
		return err
	}
	for _, required := range []string{"id", "timestamp", "request_id", "status_code"} {
		if !columns[required] {
			return fmt.Errorf("legacy requests table is missing required column %q", required)
		}
	}
	addMissingColumnWarnings(&rep, columns)

	highWatermark, err := sourceHighWatermark(ctx, db)
	if err != nil {
		return err
	}
	rep.SourceHighWatermark = highWatermark

	rows, err := readLegacyRequests(ctx, db, columns, startAfter, highWatermark)
	if err != nil {
		return err
	}
	rep.RowsRead = int64(len(rows))

	events := make([]telemetryingest.Event, 0, len(rows))
	for _, row := range rows {
		updateBlankStats(&rep.BlankFields, row)
		event := legacyEvent(fingerprint, row)
		writeChecksum(checksum, event)
		events = append(events, event)
	}
	adjustments, err := readUsageAdjustments(cfg.adjustmentsPath)
	if err != nil {
		return err
	}
	rep.AdjustmentsRead = int64(len(adjustments))
	for _, adjustment := range adjustments {
		event := adjustmentEvent(fingerprint, adjustment)
		writeChecksum(checksum, event)
		events = append(events, event)
	}
	rep.EventsPrepared = int64(len(events))
	rep.Checksum = hex.EncodeToString(checksum.Sum(nil))
	if len(rows) > 0 {
		rep.NewCheckpoint = rows[len(rows)-1].ID
	} else {
		rep.NewCheckpoint = rep.PreviousCheckpoint
	}

	if !cfg.dryRun {
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("create destination data dir: %w", err)
		}
		eventLog, err := eventlog.New(filepath.Join(destDir, "events.db"))
		if err != nil {
			return fmt.Errorf("open destination event log: %w", err)
		}
		defer eventLog.Close()
		queryStore, err := query.NewStore(filepath.Join(destDir, "query.db"))
		if err != nil {
			return fmt.Errorf("open destination query store: %w", err)
		}
		defer queryStore.Close()

		duplicates, err := countExistingEvents(ctx, eventLog.GetDB(), events)
		if err != nil {
			return err
		}
		rep.EventsDuplicate = duplicates
		accepted, dropped, err := eventLog.Append(events)
		if err != nil {
			return err
		}
		rep.EventsAccepted = accepted
		rep.EventsDropped = dropped

		drain, err := project.NewProjector(eventLog, queryStore).Drain(ctx)
		if err != nil {
			return fmt.Errorf("project imported events: %w", err)
		}
		rep.Projected = drain.Projected

		if rep.NewCheckpoint > rep.PreviousCheckpoint {
			if err := saveCheckpoint(cfg.checkpoint, checkpointFile{
				SourceDBFingerprint: fingerprint,
				LastSourceID:        rep.NewCheckpoint,
				UpdatedAt:           time.Now().UTC(),
				Mode:                cfg.mode,
			}); err != nil {
				return err
			}
		}
	}
	if cfg.mode == modeFinal && rep.NewCheckpoint < rep.SourceHighWatermark {
		rep.Warnings = append(rep.Warnings, "final migration did not reach source high-watermark")
	}
	rep.FinishedAt = time.Now().UTC()

	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if cfg.report != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.report), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(cfg.report, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}

func fileFingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

func openSource(path string) (*sql.DB, error) {
	sourceURI := url.URL{Scheme: "file", RawQuery: "mode=ro"}
	if filepath.IsAbs(path) && filepath.VolumeName(path) != "" {
		sourceURI.Path = filepath.ToSlash(path)
	} else {
		sourceURI.Path = path
	}
	db, err := sql.Open("sqlite", sourceURI.String())
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open source: %w", err)
	}
	return db, nil
}

func requestColumns(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(requests)`)
	if err != nil {
		return nil, fmt.Errorf("inspect requests table: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, errors.New("legacy requests table not found")
	}
	return columns, nil
}

func addMissingColumnWarnings(rep *report, columns map[string]bool) {
	for label, candidates := range map[string][]string{
		"path":              {"path"},
		"requested_model":   {"requested_model"},
		"effective_model":   {"effective_model", "model"},
		"provider":          {"provider", "upstream"},
		"route_mode":        {"route_mode"},
		"attempts":          {"attempts"},
		"duration_ms":       {"duration_ms"},
		"error_message":     {"error_message"},
		"prompt_tokens":     {"prompt_tokens", "input_tokens"},
		"completion_tokens": {"completion_tokens", "output_tokens"},
	} {
		if firstExistingColumn(columns, candidates...) == "" {
			rep.Warnings = append(rep.Warnings, "source requests table missing optional column "+label)
		}
	}
	sort.Strings(rep.Warnings)
}

func sourceHighWatermark(ctx context.Context, db *sql.DB) (int64, error) {
	var highWatermark sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(id) FROM requests`).Scan(&highWatermark); err != nil {
		return 0, err
	}
	if !highWatermark.Valid {
		return 0, nil
	}
	return highWatermark.Int64, nil
}

func readLegacyRequests(ctx context.Context, db *sql.DB, columns map[string]bool, afterID int64, highWatermark int64) ([]legacyRequest, error) {
	selects := []string{
		"id",
		textExprAny(columns, "timestamp"),
		textExprAny(columns, "request_id"),
		textExprAny(columns, "path"),
		textExprAny(columns, "requested_model"),
		textExprAny(columns, "effective_model", "model"),
		textExprAny(columns, "provider", "upstream"),
		textExprAny(columns, "route_mode"),
		intExprAny(columns, "status_code"),
		intExprAny(columns, "attempts"),
		intExprAny(columns, "duration_ms"),
		textExprAny(columns, "error_message"),
		intExprAny(columns, "prompt_tokens", "input_tokens"),
		intExprAny(columns, "cached_prompt_tokens"),
		intExprAny(columns, "completion_tokens", "output_tokens"),
	}
	rows, err := db.QueryContext(ctx, `SELECT `+strings.Join(selects, ", ")+` FROM requests WHERE id > ? AND id <= ? ORDER BY id ASC`, afterID, highWatermark)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []legacyRequest
	for rows.Next() {
		var r legacyRequest
		var timestamp string
		if err := rows.Scan(&r.ID, &timestamp, &r.RequestID, &r.Path, &r.RequestedModel, &r.EffectiveModel, &r.ProviderID, &r.RouteMode, &r.StatusCode, &r.Attempts, &r.DurationMs, &r.Error, &r.PromptTokens, &r.CachedPromptTokens, &r.CompletionTokens); err != nil {
			return nil, err
		}
		r.Timestamp = parseLegacyTime(timestamp)
		if r.Attempts <= 0 {
			r.Attempts = 1
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func firstExistingColumn(columns map[string]bool, names ...string) string {
	for _, name := range names {
		if columns[name] {
			return name
		}
	}
	return ""
}

func textExprAny(columns map[string]bool, names ...string) string {
	if name := firstExistingColumn(columns, names...); name != "" {
		return "COALESCE(" + name + ", '')"
	}
	return "''"
}

func intExprAny(columns map[string]bool, names ...string) string {
	if name := firstExistingColumn(columns, names...); name != "" {
		return "COALESCE(" + name + ", 0)"
	}
	return "0"
}

func parseLegacyTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Unix(0, 0).UTC()
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Unix(0, 0).UTC()
}

func legacyEvent(fingerprint string, row legacyRequest) telemetryingest.Event {
	return telemetryingest.Event{
		EventID:        fmt.Sprintf("legacy-request-%s-%d", fingerprint, row.ID),
		EventType:      "gateway.attempt.completed",
		SchemaVersion:  1,
		SourceService:  "migrate-telemetry",
		SourceInstance: fingerprint,
		EmittedAt:      row.Timestamp,
		Imported:       true,
		Payload: telemetryingest.EventPayload{
			RequestID:                row.RequestID,
			Timestamp:                row.Timestamp,
			Path:                     row.Path,
			RequestedModel:           row.RequestedModel,
			EffectiveModel:           row.EffectiveModel,
			ProviderID:               row.ProviderID,
			RouteMode:                row.RouteMode,
			StatusCode:               row.StatusCode,
			Latency:                  time.Duration(row.DurationMs) * time.Millisecond,
			Attempts:                 row.Attempts,
			PromptTokens:             row.PromptTokens,
			CachedPromptTokens:       row.CachedPromptTokens,
			CompletionTokens:         row.CompletionTokens,
			PricingStatus:            row.PricingStatus,
			PricingSourceID:          row.PricingSourceID,
			PricingCurrency:          row.PricingCurrency,
			PricingInputPer1M:        row.PricingInputPer1M,
			PricingCachedInputPer1M:  row.PricingCachedInputPer1M,
			PricingOutputPer1M:       row.PricingOutputPer1M,
			PricingPromptCostUSD:     row.PricingPromptCostUSD,
			PricingCompletionCostUSD: row.PricingCompletionCostUSD,
			PricingTotalCostUSD:      row.PricingTotalCostUSD,
			SyntheticKind:            row.SyntheticKind,
			Error:                    row.Error,
		},
	}
}

func readUsageAdjustments(path string) ([]usageAdjustment, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read usage adjustments: %w", err)
	}
	var file usageAdjustmentFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse usage adjustments: %w", err)
	}
	for i, item := range file.Adjustments {
		if strings.TrimSpace(item.ID) == "" {
			return nil, fmt.Errorf("usage adjustment %d missing id", i)
		}
		if item.PromptTokens < 0 || item.CachedPromptTokens < 0 || item.CompletionTokens < 0 {
			return nil, fmt.Errorf("usage adjustment %q has negative token count", item.ID)
		}
		if item.PromptTokens+item.CachedPromptTokens+item.CompletionTokens <= 0 {
			return nil, fmt.Errorf("usage adjustment %q has no tokens", item.ID)
		}
		if strings.TrimSpace(item.ProviderID) == "" {
			return nil, fmt.Errorf("usage adjustment %q missing provider_id", item.ID)
		}
		if strings.TrimSpace(item.EffectiveModel) == "" {
			return nil, fmt.Errorf("usage adjustment %q missing effective_model", item.ID)
		}
		if item.InputPer1M < 0 || item.CachedInputPer1M < 0 || item.OutputPer1M < 0 {
			return nil, fmt.Errorf("usage adjustment %q has negative pricing", item.ID)
		}
	}
	return file.Adjustments, nil
}

func adjustmentEvent(fingerprint string, adjustment usageAdjustment) telemetryingest.Event {
	timestamp := parseLegacyTime(adjustment.Timestamp)
	if timestamp.Equal(time.Unix(0, 0).UTC()) {
		timestamp = time.Now().UTC()
	}
	path := strings.TrimSpace(adjustment.Path)
	if path == "" {
		path = "/synthetic/usage-adjustment"
	}
	routeMode := strings.TrimSpace(adjustment.RouteMode)
	if routeMode == "" {
		routeMode = "usage_adjustment"
	}
	sourceID := strings.TrimSpace(adjustment.SourceID)
	if sourceID == "" {
		sourceID = "manual-latest-pricing"
	}
	promptCostUSD := float64(adjustment.PromptTokens) / 1_000_000 * adjustment.InputPer1M
	cachedPromptCostUSD := float64(adjustment.CachedPromptTokens) / 1_000_000 * adjustment.CachedInputPer1M
	completionCostUSD := float64(adjustment.CompletionTokens) / 1_000_000 * adjustment.OutputPer1M
	return telemetryingest.Event{
		EventID:        fmt.Sprintf("usage-adjustment-%s-%s", fingerprint, normalizeEventIDPart(adjustment.ID)),
		EventType:      "gateway.attempt.completed",
		SchemaVersion:  1,
		SourceService:  "migrate-telemetry",
		SourceInstance: fingerprint,
		EmittedAt:      timestamp,
		Imported:       true,
		Payload: telemetryingest.EventPayload{
			RequestID:                "usage-adjustment-" + adjustment.ID,
			Timestamp:                timestamp,
			Path:                     path,
			RequestedModel:           strings.TrimSpace(adjustment.RequestedModel),
			EffectiveModel:           strings.TrimSpace(adjustment.EffectiveModel),
			ProviderID:               strings.TrimSpace(adjustment.ProviderID),
			RouteMode:                routeMode,
			StatusCode:               200,
			Latency:                  0,
			Attempts:                 1,
			PromptTokens:             adjustment.PromptTokens,
			CachedPromptTokens:       adjustment.CachedPromptTokens,
			CompletionTokens:         adjustment.CompletionTokens,
			PricingStatus:            "fixed",
			PricingSourceID:          sourceID,
			PricingCurrency:          "USD",
			PricingInputPer1M:        adjustment.InputPer1M,
			PricingCachedInputPer1M:  adjustment.CachedInputPer1M,
			PricingOutputPer1M:       adjustment.OutputPer1M,
			PricingPromptCost:        promptCostUSD + cachedPromptCostUSD,
			PricingCompletionCost:    completionCostUSD,
			PricingTotalCost:         promptCostUSD + cachedPromptCostUSD + completionCostUSD,
			PricingPromptCostUSD:     promptCostUSD + cachedPromptCostUSD,
			PricingCompletionCostUSD: completionCostUSD,
			PricingTotalCostUSD:      promptCostUSD + cachedPromptCostUSD + completionCostUSD,
			SyntheticKind:            "usage_adjustment",
		},
	}
}

func normalizeEventIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	normalized := strings.Trim(b.String(), "-")
	if normalized == "" {
		return "adjustment"
	}
	return normalized
}

func writeChecksum(h hash.Hash, event telemetryingest.Event) {
	payload, _ := json.Marshal(event.Payload)
	_, _ = fmt.Fprintf(h, "%s\n%s\n%d\n%s\n", event.EventID, event.EmittedAt.UTC().Format(time.RFC3339Nano), event.SchemaVersion, payload)
}

func updateBlankStats(stats *blankStats, row legacyRequest) {
	if row.Timestamp.Equal(time.Unix(0, 0).UTC()) {
		stats.Timestamp++
	}
	if strings.TrimSpace(row.RequestID) == "" {
		stats.RequestID++
	}
	if strings.TrimSpace(row.Path) == "" {
		stats.Path++
	}
	if strings.TrimSpace(row.RequestedModel) == "" {
		stats.RequestedModel++
	}
	if strings.TrimSpace(row.EffectiveModel) == "" {
		stats.EffectiveModel++
	}
	if strings.TrimSpace(row.ProviderID) == "" {
		stats.ProviderID++
	}
	if strings.TrimSpace(row.RouteMode) == "" {
		stats.RouteMode++
	}
}

func countExistingEvents(ctx context.Context, db *sql.DB, events []telemetryingest.Event) (int64, error) {
	var count int64
	for _, event := range events {
		var exists int
		err := db.QueryRowContext(ctx, `SELECT 1 FROM events WHERE event_id = ?`, event.EventID).Scan(&exists)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return 0, err
		}
		count++
	}
	return count, nil
}

func loadCheckpoint(path string) (checkpointFile, error) {
	var cp checkpointFile
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cp, nil
	}
	if err != nil {
		return cp, err
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		return cp, fmt.Errorf("read checkpoint: %w", err)
	}
	return cp, nil
}

func saveCheckpoint(path string, cp checkpointFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
