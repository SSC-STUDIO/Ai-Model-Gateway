package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMigrateTelemetrySnapshotWritesEventsQueryAndCheckpoint(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	createLegacyDB(t, source)
	insertLegacyRequest(t, source, 1, "req-1", "2026-04-24T10:00:00Z", "gpt-4o", "openai", 200, 123, 10, 2, 5)
	insertLegacyRequest(t, source, 2, "req-2", "2026-04-24T10:01:00Z", "gpt-4o-mini", "", 500, 50, 0, 0, 0)

	dest := filepath.Join(dir, "dest")
	checkpoint := filepath.Join(dir, "checkpoint.json")
	reportPath := filepath.Join(dir, "report.json")
	var stdout bytes.Buffer
	code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-mode", "snapshot", "-checkpoint", checkpoint, "-report", reportPath}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output:\n%s", code, stdout.String())
	}

	rep := readReport(t, reportPath)
	if rep.RowsRead != 2 || rep.EventsAccepted != 2 || rep.Projected != 2 {
		t.Fatalf("unexpected counts: rows=%d accepted=%d projected=%d", rep.RowsRead, rep.EventsAccepted, rep.Projected)
	}
	if rep.NewCheckpoint != 2 {
		t.Fatalf("checkpoint in report = %d, want 2", rep.NewCheckpoint)
	}
	if rep.BlankFields.ProviderID != 1 {
		t.Fatalf("blank provider count = %d, want 1", rep.BlankFields.ProviderID)
	}
	if rep.Checksum == "" {
		t.Fatal("report checksum is blank")
	}

	var cp checkpointFile
	data, err := os.ReadFile(checkpoint)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatalf("parse checkpoint: %v", err)
	}
	if cp.LastSourceID != 2 || cp.SourceDBFingerprint != rep.SourceDBFingerprint {
		t.Fatalf("checkpoint = %+v, report fingerprint=%s", cp, rep.SourceDBFingerprint)
	}

	eventDB, err := sql.Open("sqlite", filepath.Join(dest, "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer eventDB.Close()
	var imported int
	var eventID string
	err = eventDB.QueryRow(`SELECT event_id, imported FROM events WHERE event_id LIKE 'legacy-request-%-1'`).Scan(&eventID, &imported)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}
	if !strings.HasPrefix(eventID, "legacy-request-"+rep.SourceDBFingerprint+"-") || imported != 1 {
		t.Fatalf("event_id/imported = %q/%d", eventID, imported)
	}

	queryDB, err := sql.Open("sqlite", filepath.Join(dest, "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queryDB.Close()
	var factCount, requestTotal int
	if err := queryDB.QueryRow(`SELECT COUNT(*) FROM request_facts`).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if err := queryDB.QueryRow(`SELECT COALESCE(SUM(requests), 0) FROM agg_buckets`).Scan(&requestTotal); err != nil {
		t.Fatal(err)
	}
	if factCount != 2 || requestTotal != 2 {
		t.Fatalf("query projection counts facts=%d aggregate=%d, want 2/2", factCount, requestTotal)
	}
}

func TestMigrateTelemetryIncrementalUsesCheckpoint(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	createLegacyDB(t, source)
	insertLegacyRequest(t, source, 1, "req-1", "2026-04-24T10:00:00Z", "gpt-4o", "openai", 200, 10, 0, 0, 0)
	insertLegacyRequest(t, source, 2, "req-2", "2026-04-24T10:01:00Z", "gpt-4o", "openai", 200, 10, 0, 0, 0)

	fingerprint, err := fileFingerprint(source)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := filepath.Join(dir, "checkpoint.json")
	if err := saveCheckpoint(checkpoint, checkpointFile{SourceDBFingerprint: fingerprint, LastSourceID: 1, UpdatedAt: time.Now().UTC(), Mode: modeSnapshot}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "dest")
	var stdout bytes.Buffer
	code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-mode", "incremental", "-checkpoint", checkpoint}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output:\n%s", code, stdout.String())
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("parse stdout report: %v", err)
	}
	if rep.RowsRead != 1 || rep.NewCheckpoint != 2 {
		t.Fatalf("incremental report rows/checkpoint = %d/%d, want 1/2", rep.RowsRead, rep.NewCheckpoint)
	}
}

func TestMigrateTelemetryMapsCurrentLegacySchema(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy-current.db")
	createCurrentLegacyDB(t, source)
	insertCurrentLegacyRequest(t, source, 11, "req-current", "2026-04-24T10:00:00Z", "claude-opus-4-6", "kimi-for-coding", "kimi-official", 200, 321, 1000, 200, 50)

	dest := filepath.Join(dir, "dest")
	var stdout bytes.Buffer
	code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output:\n%s", code, stdout.String())
	}

	queryDB, err := sql.Open("sqlite", filepath.Join(dest, "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queryDB.Close()
	var provider, requested, effective string
	var prompt, cached, completion int64
	err = queryDB.QueryRow(`
		SELECT provider_id, requested_model, effective_model, prompt_tokens, cached_prompt_tokens, completion_tokens
		FROM request_facts
		WHERE event_id LIKE 'legacy-request-%-11'`,
	).Scan(&provider, &requested, &effective, &prompt, &cached, &completion)
	if err != nil {
		t.Fatal(err)
	}
	if provider != "kimi-official" || requested != "claude-opus-4-6" || effective != "kimi-for-coding" {
		t.Fatalf("mapped identity = provider %q requested %q effective %q", provider, requested, effective)
	}
	if prompt != 1000 || cached != 200 || completion != 50 {
		t.Fatalf("mapped tokens = %d/%d/%d, want 1000/200/50", prompt, cached, completion)
	}
}

func TestMigrateTelemetryUsageAdjustments(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	createLegacyDB(t, source)
	adjustmentsPath := filepath.Join(dir, "adjustments.json")
	if err := os.WriteFile(adjustmentsPath, []byte(`{
  "adjustments": [
    {
      "id": "kimi-k2.6-codex",
      "timestamp": "2026-04-25T00:00:00Z",
      "provider_id": "kimi-official",
      "requested_model": "kimi-k2.6",
      "effective_model": "kimi-for-coding",
      "prompt_tokens": 900000,
      "cached_prompt_tokens": 50000,
      "completion_tokens": 50000,
      "input_per_1m": 0.16,
      "cached_input_per_1m": 0.16,
      "output_per_1m": 4.00,
      "source_id": "official-kimi-2026-04-25"
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "dest")
	var stdout bytes.Buffer
	code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-usage-adjustments", adjustmentsPath}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output:\n%s", code, stdout.String())
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if rep.AdjustmentsRead != 1 || rep.EventsAccepted != 1 || rep.Projected != 1 {
		t.Fatalf("adjustment counts = read %d accepted %d projected %d", rep.AdjustmentsRead, rep.EventsAccepted, rep.Projected)
	}

	queryDB, err := sql.Open("sqlite", filepath.Join(dest, "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queryDB.Close()
	var requested, effective, pricingStatus string
	var totalCost float64
	if err := queryDB.QueryRow(`
		SELECT requested_model, effective_model, pricing_status, pricing_total_cost_usd
		FROM request_facts
		WHERE event_id LIKE 'usage-adjustment-%-kimi-k2.6-codex'`,
	).Scan(&requested, &effective, &pricingStatus, &totalCost); err != nil {
		t.Fatal(err)
	}
	if requested != "kimi-k2.6" || effective != "kimi-for-coding" || pricingStatus != "fixed" {
		t.Fatalf("adjustment identity/status = %q/%q/%q", requested, effective, pricingStatus)
	}
	if totalCost <= 0.34 || totalCost >= 0.36 {
		t.Fatalf("adjustment total cost = %.6f, want about 0.352", totalCost)
	}
}

func TestMigrateTelemetryRerunIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	createLegacyDB(t, source)
	insertLegacyRequest(t, source, 1, "req-1", "2026-04-24T10:00:00Z", "gpt-4o", "openai", 200, 10, 3, 1, 2)

	dest := filepath.Join(dir, "dest")
	checkpoint := filepath.Join(dir, "checkpoint.json")
	var stdout bytes.Buffer
	if code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-checkpoint", checkpoint}, &stdout, &stdout); code != 0 {
		t.Fatalf("first run exit = %d, output:\n%s", code, stdout.String())
	}
	if err := os.Remove(checkpoint); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-checkpoint", checkpoint}, &stdout, &stdout); code != 0 {
		t.Fatalf("rerun exit = %d, output:\n%s", code, stdout.String())
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("parse rerun report: %v", err)
	}
	if rep.EventsDuplicate != 1 || rep.EventsAccepted != 0 {
		t.Fatalf("rerun duplicate/accepted = %d/%d, want 1/0", rep.EventsDuplicate, rep.EventsAccepted)
	}

	queryDB, err := sql.Open("sqlite", filepath.Join(dest, "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queryDB.Close()
	var facts, aggregateRequests int
	if err := queryDB.QueryRow(`SELECT COUNT(*) FROM request_facts`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := queryDB.QueryRow(`SELECT COALESCE(SUM(requests), 0) FROM agg_buckets`).Scan(&aggregateRequests); err != nil {
		t.Fatal(err)
	}
	if facts != 1 || aggregateRequests != 1 {
		t.Fatalf("idempotent projection facts/aggregate=%d/%d, want 1/1", facts, aggregateRequests)
	}
}

func TestMigrateTelemetryDryRunDoesNotWriteDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "legacy.db")
	createLegacyDB(t, source)
	insertLegacyRequest(t, source, 1, "req-1", "2026-04-24T10:00:00Z", "gpt-4o", "openai", 200, 10, 0, 0, 0)
	dest := filepath.Join(dir, "dest")

	var stdout bytes.Buffer
	code := run([]string{"migrate-telemetry", "-source", source, "-dest-data-dir", dest, "-dry-run"}, &stdout, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output:\n%s", code, stdout.String())
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination exists after dry-run: %v", err)
	}
}

func createLegacyDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  request_id TEXT NOT NULL,
  path TEXT NOT NULL,
  requested_model TEXT,
  model TEXT,
  route_mode TEXT,
  upstream TEXT,
  status_code INTEGER NOT NULL,
  attempts INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  success INTEGER NOT NULL,
  error_message TEXT,
  prompt_tokens INTEGER NOT NULL,
  cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL,
  total_tokens INTEGER NOT NULL
)`)
	if err != nil {
		t.Fatal(err)
	}
}

func createCurrentLegacyDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  timestamp TEXT NOT NULL,
  request_id TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT '',
  requested_model TEXT NOT NULL DEFAULT '',
  effective_model TEXT NOT NULL DEFAULT '',
  model TEXT,
  route_mode TEXT NOT NULL DEFAULT '',
  provider TEXT,
  status_code INTEGER NOT NULL,
  duration_ms INTEGER NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 1,
  input_tokens INTEGER NOT NULL DEFAULT 0,
  cached_prompt_tokens INTEGER NOT NULL DEFAULT 0,
  output_tokens INTEGER NOT NULL DEFAULT 0,
  stream INTEGER NOT NULL DEFAULT 0,
  error_message TEXT
)`)
	if err != nil {
		t.Fatal(err)
	}
}

func insertCurrentLegacyRequest(t *testing.T, path string, id int64, requestID, timestamp, requestedModel, effectiveModel, provider string, statusCode int, durationMs int64, inputTokens, cachedPromptTokens, outputTokens int64) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
INSERT INTO requests (
  id, timestamp, request_id, path, requested_model, effective_model, model, route_mode,
  provider, status_code, duration_ms, attempts, input_tokens, cached_prompt_tokens,
  output_tokens, stream, error_message
) VALUES (?, ?, ?, '/v1/messages', ?, ?, ?, 'anthropic_buffered_stream', ?, ?, ?, 1, ?, ?, ?, 1, '')`,
		id, timestamp, requestID, requestedModel, effectiveModel, effectiveModel, provider, statusCode, durationMs, inputTokens, cachedPromptTokens, outputTokens)
	if err != nil {
		t.Fatal(err)
	}
}

func insertLegacyRequest(t *testing.T, path string, id int64, requestID, timestamp, model, upstream string, statusCode int, durationMs int64, promptTokens, cachedPromptTokens, completionTokens int64) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
INSERT INTO requests (
  id, timestamp, request_id, path, requested_model, model, route_mode, upstream,
  status_code, attempts, duration_ms, success, error_message,
  prompt_tokens, cached_prompt_tokens, completion_tokens, total_tokens
) VALUES (?, ?, ?, '/v1/chat/completions', ?, ?, 'direct', ?, ?, 1, ?, ?, '', ?, ?, ?, ?)`,
		id, timestamp, requestID, model, model, upstream, statusCode, durationMs, boolInt(statusCode >= 200 && statusCode < 400), promptTokens, cachedPromptTokens, completionTokens, promptTokens+cachedPromptTokens+completionTokens)
	if err != nil {
		t.Fatal(err)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func readReport(t *testing.T, path string) report {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}
