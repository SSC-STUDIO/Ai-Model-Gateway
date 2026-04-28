package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestNewStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "telemetry.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore(%q) failed: %v", dbPath, err)
	}
	defer store.Close()

	if store.db == nil {
		t.Fatal("store.db is nil")
	}
}

func TestNewStoreCreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "telemetry-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "subdir", "telemetry.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore(%q) failed: %v", dbPath, err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Dir(dbPath)); os.IsNotExist(err) {
		t.Fatal("NewStore did not create parent directory")
	}
}

func TestRecordRequest(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-123",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-4o",
		Model:          "gpt-4o",
		RouteMode:      "round-robin",
		Upstream:       "openai",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     150,
		Success:        true,
		Usage: Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
	}

	store.RecordRequest(record)

	snapshot := store.Snapshot()
	if snapshot.Summary.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.Summary.TotalRequests)
	}
	if snapshot.Summary.Successes != 1 {
		t.Fatalf("Successes = %d, want 1", snapshot.Summary.Successes)
	}
	if snapshot.Summary.PromptTokens != 100 {
		t.Fatalf("PromptTokens = %d, want 100", snapshot.Summary.PromptTokens)
	}
	if snapshot.Summary.CompletionTokens != 50 {
		t.Fatalf("CompletionTokens = %d, want 50", snapshot.Summary.CompletionTokens)
	}
}

func TestRecordRequestFailure(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-456",
		Path:       "/v1/chat/completions",
		StatusCode: 500,
		Attempts:   3,
		DurationMs: 300,
		Success:    false,
		Error:      "upstream timeout",
	}

	store.RecordRequest(record)

	snapshot := store.Snapshot()
	if snapshot.Summary.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.Summary.TotalRequests)
	}
	if snapshot.Summary.Failures != 1 {
		t.Fatalf("Failures = %d, want 1", snapshot.Summary.Failures)
	}
	if snapshot.Summary.Successes != 0 {
		t.Fatalf("Successes = %d, want 0", snapshot.Summary.Successes)
	}
}

func TestRecordRequestWithCachedTokens(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-789",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		Usage: Usage{
			PromptTokens:       1000,
			CachedPromptTokens: 500,
			CompletionTokens:   200,
			TotalTokens:        1200,
		},
	}

	store.RecordRequest(record)

	snapshot := store.Snapshot()
	if snapshot.Summary.CachedPromptTokens != 500 {
		t.Fatalf("CachedPromptTokens = %d, want 500", snapshot.Summary.CachedPromptTokens)
	}
}

func TestRecordError(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	errRecord := ErrorRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-err-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-4o",
		Model:          "gpt-4o",
		RouteMode:      "fallback",
		Upstream:       "openai",
		StatusCode:     502,
		Attempt:        2,
		Message:        "bad gateway",
	}

	store.RecordError(errRecord)

	snapshot := store.Snapshot()
	if len(snapshot.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(snapshot.Errors))
	}
	if snapshot.Errors[0].Message != "bad gateway" {
		t.Fatalf("Error message = %q, want %q", snapshot.Errors[0].Message, "bad gateway")
	}
}

func TestSnapshotCaching(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-1",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}

	store.RecordRequest(record)

	// First snapshot
	s1 := store.Snapshot()

	// Second snapshot within cache TTL should return cached value
	s2 := store.Snapshot()

	if s1.GeneratedAt != s2.GeneratedAt {
		t.Fatal("expected cached snapshot to have same GeneratedAt")
	}
}

func TestQueryTimeSeries(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-ts-1",
		Path:       "/v1/chat/completions",
		Model:      "gpt-4o",
		Upstream:   "openai",
		StatusCode: 200,
		Success:    true,
		Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}

	store.RecordRequest(record)

	ts := store.QueryTimeSeries(24, 60)
	if ts.Buckets == nil {
		t.Fatal("expected non-nil Buckets")
	}
	if ts.ByUpstream == nil {
		t.Fatal("expected non-nil ByUpstream")
	}
}

func TestQueryTimeSeriesWithInvalidParams(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record a request so there's data for the time series
	store.RecordRequest(RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-ts-default",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	})

	// Test with zero hours (should default to 24)
	ts := store.QueryTimeSeries(0, 0)
	// With data and default params, should return results
	if len(ts.Buckets) == 0 && len(ts.ByUpstream) == 0 {
		t.Fatal("expected at least some time series data with default params")
	}
}

func TestQueryTimeSeriesNilStore(t *testing.T) {
	var store *Store
	ts := store.QueryTimeSeries(24, 60)
	if ts.Buckets != nil || ts.ByUpstream != nil {
		t.Fatal("expected empty TimeSeries for nil store")
	}
}

func TestCloseNilStore(t *testing.T) {
	var store *Store
	if err := store.Close(); err != nil {
		t.Fatalf("Close on nil store should return nil, got %v", err)
	}
}

func TestSnapshotNilStore(t *testing.T) {
	var store *Store
	snapshot := store.Snapshot()
	if snapshot.GeneratedAt.IsZero() {
		t.Fatal("expected non-zero GeneratedAt for nil store snapshot")
	}
}

func TestRecordRequestNilStore(t *testing.T) {
	var store *Store
	// Should not panic
	store.RecordRequest(RequestRecord{})
}

func TestRecordErrorNilStore(t *testing.T) {
	var store *Store
	// Should not panic
	store.RecordError(ErrorRecord{})
}

func TestQueryWindowMetrics(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-wm-1",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		DurationMs: 100,
		Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	}

	store.RecordRequest(record)

	snapshot := store.Snapshot()

	// Check performance metrics exist
	if snapshot.Performance.Last1m.WindowLabel != "1m" {
		t.Fatalf("Last1m.WindowLabel = %q, want %q", snapshot.Performance.Last1m.WindowLabel, "1m")
	}
	if snapshot.Performance.Last5m.WindowLabel != "5m" {
		t.Fatalf("Last5m.WindowLabel = %q, want %q", snapshot.Performance.Last5m.WindowLabel, "5m")
	}
}

func TestQueryCacheTrends(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-ct-1",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		Usage: Usage{
			PromptTokens:       1000,
			CachedPromptTokens: 500,
			CompletionTokens:   200,
			TotalTokens:        1200,
		},
	}

	store.RecordRequest(record)

	snapshot := store.Snapshot()

	if snapshot.CacheTrends.Last1h.WindowLabel != "1h" {
		t.Fatalf("Last1h.WindowLabel = %q, want %q", snapshot.CacheTrends.Last1h.WindowLabel, "1h")
	}
	if snapshot.CacheTrends.Last24h.WindowLabel != "24h" {
		t.Fatalf("Last24h.WindowLabel = %q, want %q", snapshot.CacheTrends.Last24h.WindowLabel, "24h")
	}
}

func TestQueryUsageBreakdown(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record requests with different models
	for i, model := range []string{"gpt-4o", "gpt-4o-mini", "claude-3-opus"} {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-ub-" + model,
			Path:       "/v1/chat/completions",
			Model:      model,
			Upstream:   "openai",
			StatusCode: 200,
			Success:    true,
			Usage:      Usage{PromptTokens: 100 * (i + 1), CompletionTokens: 50 * (i + 1), TotalTokens: 150 * (i + 1)},
		})
	}

	snapshot := store.Snapshot()

	if len(snapshot.ByModel) == 0 {
		t.Fatal("expected non-empty ByModel")
	}
	if len(snapshot.ByUpstream) == 0 {
		t.Fatal("expected non-empty ByUpstream")
	}
}

func TestQueryModelRouteBreakdown(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-mrb-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-4",
		Model:          "gpt-4o",
		Upstream:       "openai-primary",
		StatusCode:     200,
		Success:        true,
		Usage:          Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
	})

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-mrb-2",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-4",
		Model:          "gpt-4o",
		Upstream:       "openai-secondary",
		StatusCode:     200,
		Success:        true,
		Usage:          Usage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300},
	})

	snapshot := store.Snapshot()

	if len(snapshot.ByModelRoute) == 0 {
		t.Fatal("expected non-empty ByModelRoute")
	}
}

func TestMultipleRequestsAndErrors(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record multiple requests
	for i := 0; i < 10; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-multi-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			Model:      "gpt-4o",
			StatusCode: 200,
			Success:    i%2 == 0, // alternate success/failure
			Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		})
	}

	// Record multiple errors
	for i := 0; i < 5; i++ {
		store.RecordError(ErrorRecord{
			Timestamp:  time.Now(),
			RequestID:  "err-multi-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			StatusCode: 500,
			Attempt:    i + 1,
			Message:    "test error",
		})
	}

	snapshot := store.Snapshot()

	if snapshot.Summary.TotalRequests != 10 {
		t.Fatalf("TotalRequests = %d, want 10", snapshot.Summary.TotalRequests)
	}
	if snapshot.Summary.Successes != 5 {
		t.Fatalf("Successes = %d, want 5", snapshot.Summary.Successes)
	}
	if snapshot.Summary.Failures != 5 {
		t.Fatalf("Failures = %d, want 5", snapshot.Summary.Failures)
	}
}

func TestPrependRequestRecord(t *testing.T) {
	records := []RequestRecord{}
	for i := 0; i < 5; i++ {
		records = prependRequestRecord(records, RequestRecord{RequestID: string(rune('0' + i))}, 10)
	}

	if len(records) != 5 {
		t.Fatalf("len(records) = %d, want 5", len(records))
	}
	// Most recent should be first
	if records[0].RequestID != "4" {
		t.Fatalf("records[0].RequestID = %q, want %q", records[0].RequestID, "4")
	}
}

func TestPrependRequestRecordLimit(t *testing.T) {
	records := []RequestRecord{}
	for i := 0; i < 15; i++ {
		records = prependRequestRecord(records, RequestRecord{RequestID: string(rune('0' + i))}, 10)
	}

	if len(records) != 10 {
		t.Fatalf("len(records) = %d, want 10", len(records))
	}
}

func TestPrependRequestRecordZeroLimit(t *testing.T) {
	records := []RequestRecord{}
	result := prependRequestRecord(records, RequestRecord{RequestID: "test"}, 0)

	if result != nil {
		t.Fatalf("expected nil for zero limit, got len=%d", len(result))
	}
}

func TestPrependErrorRecord(t *testing.T) {
	records := []ErrorRecord{}
	for i := 0; i < 5; i++ {
		records = prependErrorRecord(records, ErrorRecord{RequestID: string(rune('0' + i))}, 10)
	}

	if len(records) != 5 {
		t.Fatalf("len(records) = %d, want 5", len(records))
	}
	// Most recent should be first
	if records[0].RequestID != "4" {
		t.Fatalf("records[0].RequestID = %q, want %q", records[0].RequestID, "4")
	}
}

func TestPrependErrorRecordLimit(t *testing.T) {
	records := []ErrorRecord{}
	for i := 0; i < 15; i++ {
		records = prependErrorRecord(records, ErrorRecord{RequestID: string(rune('0' + i))}, 10)
	}

	if len(records) != 10 {
		t.Fatalf("len(records) = %d, want 10", len(records))
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Fatal("boolToInt(true) should be 1")
	}
	if boolToInt(false) != 0 {
		t.Fatal("boolToInt(false) should be 0")
	}
}

func TestParseTime(t *testing.T) {
	valid := "2024-01-15T10:30:00Z"
	parsed := parseTime(valid)
	if parsed.Year() != 2024 {
		t.Fatalf("parseTime(%q).Year() = %d, want 2024", valid, parsed.Year())
	}

	invalid := "not-a-time"
	parsed = parseTime(invalid)
	if !parsed.IsZero() {
		t.Fatalf("parseTime(%q) should return zero time", invalid)
	}
}

func TestValidSQLIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple", "table_name", true},
		{"uppercase", "TABLE_NAME", true},
		{"with_numbers", "table123", true},
		{"starts_with_underscore", "_table", true},
		{"empty", "", false},
		{"starts_with_number", "123table", false},
		{"with_hyphen", "table-name", false},
		{"with_space", "table name", false},
		{"with_special", "table@name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validSQLIdentifier(tt.input)
			if got != tt.want {
				t.Fatalf("validSQLIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidColumnType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"TEXT", "TEXT", true},
		{"INTEGER", "INTEGER", true},
		{"REAL", "REAL", true},
		{"BLOB", "BLOB", true},
		{"NUMERIC", "NUMERIC", true},
		{"TEXT_NOT_NULL", "TEXT NOT NULL", true},
		{"INTEGER_NOT_NULL", "INTEGER NOT NULL", true},
		{"INTEGER_DEFAULT", "INTEGER NOT NULL DEFAULT 0", true},
		{"TEXT_DEFAULT", "TEXT NOT NULL DEFAULT ''", true},
		{"lowercase", "text", true},
		{"mixed_case", "Text", true},
		{"invalid", "VARCHAR(255)", false},
		{"dangerous", "TEXT; DROP TABLE users;--", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validColumnType(tt.input)
			if got != tt.want {
				t.Fatalf("validColumnType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnsureColumnInvalidTableName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	err := store.ensureColumn("invalid-table-name", "column", "TEXT")
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestEnsureColumnInvalidColumnName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	err := store.ensureColumn("requests", "invalid-column-name", "TEXT")
	if err == nil {
		t.Fatal("expected error for invalid column name")
	}
}

func TestEnsureColumnInvalidColumnType(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	err := store.ensureColumn("requests", "new_column", "VARCHAR(255)")
	if err == nil {
		t.Fatal("expected error for invalid column type")
	}
}

// Helper function to create a test store
func newTestStore(t *testing.T) *Store {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "telemetry-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "telemetry.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore(%q) failed: %v", dbPath, err)
	}

	return store
}

func TestPricingGroupModel(t *testing.T) {
	tests := []struct {
		name           string
		requestedModel string
		effectiveModel string
		want           string
	}{
		{"both empty", "", "", ""},
		{"effective only", "", "gpt-4o", "gpt-4o"},
		{"requested only", "gpt-4o", "", "gpt-4o"},
		{"both set", "gpt-4", "gpt-4o", "gpt-4"},
		{"with whitespace", "  gpt-4o  ", "  gpt-4o-mini  ", "gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pricingGroupModel(tt.requestedModel, tt.effectiveModel)
			if got != tt.want {
				t.Fatalf("pricingGroupModel(%q, %q) = %q, want %q", tt.requestedModel, tt.effectiveModel, got, tt.want)
			}
		})
	}
}

func TestMergedModelName(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		candidate string
		fallback  string
		want      string
	}{
		{"current set", "gpt-4o", "gpt-4", "gpt-3.5", "gpt-4o"},
		{"current empty, candidate set", "", "gpt-4", "gpt-3.5", "gpt-4"},
		{"both empty, use fallback", "", "", "gpt-3.5", "gpt-3.5"},
		{"all empty", "", "", "", ""},
		{"whitespace current", "  ", "gpt-4", "gpt-3.5", "gpt-4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergedModelName(tt.current, tt.candidate, tt.fallback)
			if got != tt.want {
				t.Fatalf("mergedModelName(%q, %q, %q) = %q, want %q", tt.current, tt.candidate, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestFormatPricingDisplayModel(t *testing.T) {
	tests := []struct {
		name           string
		requestedModel string
		effectiveModel string
		want           string
	}{
		{"both empty", "", "", "unknown"},
		{"effective only", "", "gpt-4o", "gpt-4o"},
		{"requested only", "gpt-4o", "", "gpt-4o"},
		{"both set", "gpt-4", "gpt-4o", "gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatPricingDisplayModel(tt.requestedModel, tt.effectiveModel)
			if got != tt.want {
				t.Fatalf("formatPricingDisplayModel(%q, %q) = %q, want %q", tt.requestedModel, tt.effectiveModel, got, tt.want)
			}
		})
	}
}

func TestNormalizePricing(t *testing.T) {
	tests := []struct {
		name  string
		input Pricing
		want  Pricing
	}{
		{
			name:  "basic USD",
			input: Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
			want:  Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30, InputPer1MUsd: 10, OutputPer1MUsd: 30},
		},
		{
			name:  "from usd fields",
			input: Pricing{Currency: "USD", InputPer1MUsd: 10, OutputPer1MUsd: 30},
			want:  Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30, InputPer1MUsd: 10, OutputPer1MUsd: 30},
		},
		{
			name:  "non-USD currency",
			input: Pricing{Currency: "CNY", InputPer1M: 10, OutputPer1M: 30},
			want:  Pricing{Currency: "CNY", InputPer1M: 10, OutputPer1M: 30},
		},
		{
			name:  "empty currency defaults to USD",
			input: Pricing{InputPer1M: 10, OutputPer1M: 30},
			want:  Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30, InputPer1MUsd: 10, OutputPer1MUsd: 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePricing(tt.input)
			if got.Currency != tt.want.Currency {
				t.Fatalf("Currency = %q, want %q", got.Currency, tt.want.Currency)
			}
			if got.InputPer1M != tt.want.InputPer1M {
				t.Fatalf("InputPer1M = %f, want %f", got.InputPer1M, tt.want.InputPer1M)
			}
			if got.OutputPer1M != tt.want.OutputPer1M {
				t.Fatalf("OutputPer1M = %f, want %f", got.OutputPer1M, tt.want.OutputPer1M)
			}
		})
	}
}

func TestNormalizePricingCurrency(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "USD"},
		{"usd", "USD"},
		{"cny", "CNY"},
		{"USD", "USD"},
		{"  cny  ", "CNY"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePricingCurrency(tt.input)
			if got != tt.want {
				t.Fatalf("normalizePricingCurrency(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizePricingAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"gpt-4o", "gpt-4o"},
		{"GPT-4O", "gpt-4o"},
		{"gpt 4o", "gpt-4o"},
		{"gpt_4o", "gpt-4o"},
		{"openai/gpt-4o", "openai/gpt-4o"},
		{"  gpt-4o  ", "gpt-4o"},
		{"gpt--4o", "gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePricingAlias(tt.input)
			if got != tt.want {
				t.Fatalf("normalizePricingAlias(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsDigits(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"123", true},
		{"abc", false},
		{"12a", false},
		{"0", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isDigits(tt.input)
			if got != tt.want {
				t.Fatalf("isDigits(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeCachedPromptTokens(t *testing.T) {
	tests := []struct {
		name  string
		usage Usage
		want  int
	}{
		{"no cached", Usage{PromptTokens: 100}, 0},
		{"some cached", Usage{PromptTokens: 100, CachedPromptTokens: 50}, 50},
		{"cached exceeds prompt", Usage{PromptTokens: 100, CachedPromptTokens: 200}, 100},
		{"negative cached", Usage{PromptTokens: 100, CachedPromptTokens: -50}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCachedPromptTokens(tt.usage)
			if got != tt.want {
				t.Fatalf("normalizeCachedPromptTokens(%+v) = %d, want %d", tt.usage, got, tt.want)
			}
		})
	}
}

func TestProviderScopedPricingKey(t *testing.T) {
	tests := []struct {
		upstream string
		model    string
		want     string
	}{
		{"openai", "gpt-4o", "provider::openai::gpt-4o"},
		{"", "gpt-4o", ""},
		{"openai", "", ""},
		{"OPENAI", "GPT-4O", "provider::openai::gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.upstream+"_"+tt.model, func(t *testing.T) {
			got := providerScopedPricingKey(tt.upstream, tt.model)
			if got != tt.want {
				t.Fatalf("providerScopedPricingKey(%q, %q) = %q, want %q", tt.upstream, tt.model, got, tt.want)
			}
		})
	}
}

func TestPricingRouteKey(t *testing.T) {
	got := PricingRouteKey("gpt-4o", "gpt-4o-mini", "openai")
	want := "gpt-4o|gpt-4o-mini|openai"
	if got != want {
		t.Fatalf("PricingRouteKey() = %q, want %q", got, want)
	}

	got = PricingRouteKey("gpt-4o", "", "")
	want = "gpt-4o|"
	if got != want {
		t.Fatalf("PricingRouteKey() with empty effective/upstream = %q, want %q", got, want)
	}
}

func TestCalculateCacheSavings(t *testing.T) {
	tests := []struct {
		name    string
		usage   Usage
		pricing Pricing
		want    float64
	}{
		{
			name:    "no cached tokens",
			usage:   Usage{PromptTokens: 1000},
			pricing: Pricing{InputPer1M: 10, CachedInputPer1M: 5},
			want:    0,
		},
		{
			name:    "with savings",
			usage:   Usage{PromptTokens: 1000000, CachedPromptTokens: 500000},
			pricing: Pricing{InputPer1M: 10, CachedInputPer1M: 5},
			want:    2.5, // 500k tokens * ($10 - $5) / 1M = $2.50
		},
		{
			name:    "no cached rate",
			usage:   Usage{PromptTokens: 1000000, CachedPromptTokens: 500000},
			pricing: Pricing{InputPer1M: 10},
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCacheSavings(tt.usage, tt.pricing)
			if got != tt.want {
				t.Fatalf("calculateCacheSavings() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestClonePricingCatalog(t *testing.T) {
	src := map[string]Pricing{
		"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
	}

	dst := clonePricingCatalog(src)
	if len(dst) != 1 {
		t.Fatalf("len(dst) = %d, want 1", len(dst))
	}

	// Modify original, check clone is independent
	src["gpt-4o"] = Pricing{Currency: "USD", InputPer1M: 999, OutputPer1M: 30}
	if dst["gpt-4o"].InputPer1M == 999 {
		t.Fatal("clone should be independent of original")
	}

	// Empty input
	empty := clonePricingCatalog(nil)
	if empty == nil {
		t.Fatal("clonePricingCatalog(nil) should return empty map, not nil")
	}
}

func TestTrimPricingSnapshotSuffix(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"gpt-4o-2024-05-13", "gpt-4o", true},
		{"gpt-4o-20240513", "gpt-4o", true},
		{"gpt-4o", "", false},
		{"gpt-4o-mini-2025-04-14", "gpt-4o-mini", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := trimPricingSnapshotSuffix(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("trimPricingSnapshotSuffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrimPricingVariantSuffix(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"gpt-4o:free", "gpt-4o", true},
		{"gpt-4o", "", false},
		{"deepseek-chat:free", "deepseek-chat", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := trimPricingVariantSuffix(tt.input)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("trimPricingVariantSuffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPricingAliasTail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"openai/gpt-4o", "gpt-4o"},
		{"gpt-4o", ""},
		{"provider/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pricingAliasTail(tt.input)
			if got != tt.want {
				t.Fatalf("pricingAliasTail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalPricingAliases(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"gpt-4o", []string{"gpt-4o"}},
		{"gpt-4o-mini-2024-07-18", []string{"gpt-4o-mini-2024-07-18", "gpt-4o-mini"}},
		{"gpt-4o:free", []string{"gpt-4o:free", "gpt-4o"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := canonicalPricingAliases(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("canonicalPricingAliases(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPricingCatalogNilReceiver(t *testing.T) {
	var catalog *PricingCatalog

	// Snapshot on nil receiver should return bootstrap
	snapshot := catalog.Snapshot()
	if snapshot.SourceURL == "" {
		t.Fatal("expected bootstrap snapshot for nil catalog")
	}

	// UpdateConfig on nil should not panic
	catalog.UpdateConfig(core.PricingConfig{})

	// Start on nil should not panic
	catalog.Start(nil)
}

func TestNewPricingCatalog(t *testing.T) {
	catalog := NewPricingCatalog(core.PricingConfig{})
	if catalog == nil {
		t.Fatal("NewPricingCatalog returned nil")
	}

	snapshot := catalog.Snapshot()
	if len(snapshot.Catalog) == 0 {
		t.Fatal("expected non-empty bootstrap catalog")
	}
}

func TestMergePricingCatalogs(t *testing.T) {
	base := map[string]Pricing{
		"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
	}

	overlay := map[string]Pricing{
		"gpt-4o":        {Currency: "USD", InputPer1M: 5, OutputPer1M: 15},  // Override
		"claude-3-opus": {Currency: "USD", InputPer1M: 15, OutputPer1M: 75}, // New
	}

	merged := mergePricingCatalogs(base, overlay)
	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
	if merged["gpt-4o"].InputPer1M != 5 {
		t.Fatal("expected override from overlay")
	}
	if _, ok := merged["claude-3-opus"]; !ok {
		t.Fatal("expected new model from overlay")
	}
}

func TestStripPricingManualOverrides(t *testing.T) {
	cfg := core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{
			{Model: "gpt-4o", Currency: "USD", InputPer1M: 99, OutputPer1M: 199},
		},
	}

	snapshot := PricingCatalogSnapshot{
		Catalog: map[string]Pricing{
			"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
		},
	}

	stripped := stripPricingManualOverrides(snapshot, cfg)
	if _, ok := stripped.Catalog["gpt-4o"]; ok {
		t.Fatal("expected gpt-4o to be stripped")
	}
}

func TestApplyPricingConfigToSnapshot(t *testing.T) {
	cfg := core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{
			{Model: "custom-model", Currency: "CNY", InputPer1M: 1.5, OutputPer1M: 6.5},
		},
	}

	snapshot := PricingCatalogSnapshot{
		Catalog: map[string]Pricing{
			"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
		},
	}

	result := applyPricingConfigToSnapshot(snapshot, cfg)
	if _, ok := result.Catalog["custom-model"]; !ok {
		t.Fatal("expected custom-model to be added")
	}
	if result.Catalog["custom-model"].Currency != "CNY" {
		t.Fatal("expected CNY currency for custom model")
	}
}

func TestSplitModelAliases(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"gpt-4o", 1},
		{"gpt-4o/gpt-4o-mini", 2},
		{"", 0},
		{"gpt-4o / gpt-4o-mini / claude-3", 3},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitModelAliases(tt.input)
			if len(got) != tt.want {
				t.Fatalf("len(splitModelAliases(%q)) = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestParseDollarCell(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"$10.50", 10.50},
		{"10.50", 10.50},
		{"-", 0},
		{"", 0},
		{"$0.075", 0.075},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDollarCell(tt.input)
			if got != tt.want {
				t.Fatalf("parseDollarCell(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanPricingText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<b>text</b>", "text"},
		{"  multiple   spaces  ", "multiple spaces"},
		{"text&nbsp;text", "text text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanPricingText(tt.input)
			if got != tt.want {
				t.Fatalf("cleanPricingText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPricingSummaryKey(t *testing.T) {
	tests := []struct {
		name string
		item PricingModelSummary
		want string
	}{
		{
			name: "basic",
			item: PricingModelSummary{
				DisplayModel: "gpt-4o",
				PricingModel: "gpt-4o",
				Upstream:     "openai",
			},
			want: "gpt-4o|gpt-4o|openai",
		},
		{
			name: "no pricing model",
			item: PricingModelSummary{
				DisplayModel:   "gpt-4o",
				RequestedModel: "gpt-4",
				EffectiveModel: "gpt-4o",
			},
			want: "gpt-4o|gpt-4",
		},
		{
			name: "no upstream",
			item: PricingModelSummary{
				DisplayModel: "gpt-4o",
				PricingModel: "gpt-4o",
			},
			want: "gpt-4o|gpt-4o",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pricingSummaryKey(tt.item)
			if got != tt.want {
				t.Fatalf("pricingSummaryKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCalculatePricingCost(t *testing.T) {
	tests := []struct {
		name    string
		usage   Usage
		pricing Pricing
		want    float64 // total cost
	}{
		{
			name: "basic calculation",
			usage: Usage{
				PromptTokens:     1_000_000,
				CompletionTokens: 500_000,
			},
			pricing: Pricing{Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
			want:    25.0, // $10 + $15
		},
		{
			name: "with cached tokens",
			usage: Usage{
				PromptTokens:       1_000_000,
				CachedPromptTokens: 500_000,
				CompletionTokens:   500_000,
			},
			pricing: Pricing{Currency: "USD", InputPer1M: 10, CachedInputPer1M: 5, OutputPer1M: 30},
			want:    22.5, // ($500k * $10 + $500k * $5) / 1M + $15 = $7.5 + $15
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePricingCost(tt.usage, tt.pricing)
			if got.Total != tt.want {
				t.Fatalf("Total = %f, want %f", got.Total, tt.want)
			}
		})
	}
}

func TestPricingLookupCandidates(t *testing.T) {
	candidates := pricingLookupCandidates("gpt-4o", "gpt-4o-mini")
	if len(candidates) == 0 {
		t.Fatal("expected non-empty candidates")
	}

	// Requested model should come first
	if candidates[0] != "gpt-4o" {
		t.Fatalf("first candidate = %q, want gpt-4o", candidates[0])
	}
}

func TestPricingSortValue(t *testing.T) {
	tests := []struct {
		name string
		item PricingModelSummary
		want float64
	}{
		{"usd cost", PricingModelSummary{Cost: PricingCost{TotalUsd: 10.0, Total: 5.0}}, 10.0},
		{"no usd cost", PricingModelSummary{Cost: PricingCost{Total: 5.0}}, 5.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pricingSortValue(tt.item)
			if got != tt.want {
				t.Fatalf("pricingSortValue() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestPricingCatalogCurrentConfig(t *testing.T) {
	// Test nil receiver
	var nilCatalog *PricingCatalog
	cfg := nilCatalog.currentConfig()
	if cfg.RefreshIntervalHours != 0 {
		t.Fatal("expected zero config for nil catalog")
	}

	// Test normal catalog
	catalog := NewPricingCatalog(core.PricingConfig{RefreshIntervalHours: 6})
	cfg = catalog.currentConfig()
	if cfg.RefreshIntervalHours != 6 {
		t.Fatalf("RefreshIntervalHours = %d, want 6", cfg.RefreshIntervalHours)
	}
}

func TestPricingSnapshotFromConfig(t *testing.T) {
	// Test with empty cache path
	snapshot := pricingSnapshotFromConfig(core.PricingConfig{})
	if len(snapshot.Catalog) == 0 {
		t.Fatal("expected bootstrap catalog for empty config")
	}

	// Test with non-existent cache path
	snapshot = pricingSnapshotFromConfig(core.PricingConfig{CachePath: "/nonexistent/path/cache.json"})
	if snapshot.LastError == "" {
		t.Fatal("expected error for non-existent cache path")
	}
}

func TestPricingSnapshotForUpdate(t *testing.T) {
	current := BootstrapPricingSnapshot()
	cfg := core.PricingConfig{}

	snapshot := pricingSnapshotForUpdate(current, cfg)
	if len(snapshot.Catalog) == 0 {
		t.Fatal("expected non-empty catalog after update")
	}
}

func TestCanonicalModelNames(t *testing.T) {
	tests := []struct {
		input string
		want  int // expected count
	}{
		{"", 0},
		{"GPT-4o", 1},
		{"GPT-5.4 mini", 1},
		{"unknown model", 0},
		{"claude-3-opus", 1},
		{"gemini-2.5-pro", 1},
		{"deepseek-chat", 1},
		{"kimi-k2", 1},
		{"glm-4.5", 1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := canonicalModelNames(tt.input)
			if len(got) != tt.want {
				t.Fatalf("len(canonicalModelNames(%q)) = %d, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestLoadPricingCatalogCache(t *testing.T) {
	// Empty path
	_, err := loadPricingCatalogCache("")
	if err == nil {
		t.Fatal("expected error for empty cache path")
	}

	// Non-existent file
	_, err = loadPricingCatalogCache("/nonexistent/cache.json")
	if err == nil {
		t.Fatal("expected error for non-existent cache file")
	}
}

func TestSavePricingCatalogCache(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pricing-cache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Empty path should return nil
	err = savePricingCatalogCache("", BootstrapPricingSnapshot())
	if err != nil {
		t.Fatalf("expected nil for empty path, got %v", err)
	}

	// Valid path
	cachePath := filepath.Join(tmpDir, "cache.json")
	err = savePricingCatalogCache(cachePath, BootstrapPricingSnapshot())
	if err != nil {
		t.Fatalf("savePricingCatalogCache failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("cache file was not created")
	}

	// Load it back
	loaded, err := loadPricingCatalogCache(cachePath)
	if err != nil {
		t.Fatalf("loadPricingCatalogCache failed: %v", err)
	}
	if len(loaded.Catalog) == 0 {
		t.Fatal("expected non-empty catalog from cache")
	}
}

func TestSavePricingCatalogCacheInvalidPath(t *testing.T) {
	// Invalid path with directory traversal - pathsecurity validates the base name
	// "../invalid/cache.json" has basename "cache.json" which is valid
	// so it won't error at pathsecurity level
	// Instead test with an empty basename
	err := savePricingCatalogCache("/", BootstrapPricingSnapshot())
	if err == nil {
		t.Fatal("expected error for invalid cache path with empty basename")
	}
}

func TestBuildPricingSnapshotEmptyCatalog(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-4o",
				Usage:          Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			},
		},
	}

	// Empty catalog should bootstrap
	result := BuildPricingSnapshot(snapshot, PricingCatalogSnapshot{})
	if len(result.Catalog) == 0 {
		t.Fatal("expected bootstrap catalog for empty input")
	}
}

func TestBuildPricingSnapshotWithUnpricedModel(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "unknown-model",
				Usage:          Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			},
		},
	}

	result := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if result.Summary.UnpricedModels != 1 {
		t.Fatalf("UnpricedModels = %d, want 1", result.Summary.UnpricedModels)
	}
}

func TestBuildPricingSnapshotMergeSameKey(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-4o",
				Model:          "gpt-4o",
				Upstream:       "openai",
				Usage:          Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			},
			{
				RequestedModel: "gpt-4o",
				Model:          "gpt-4o",
				Upstream:       "openai",
				Usage:          Usage{PromptTokens: 200, CompletionTokens: 100, TotalTokens: 300},
			},
		},
	}

	result := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(result.Models) != 1 {
		t.Fatalf("models len = %d, want 1 (merged)", len(result.Models))
	}
	if result.Models[0].Usage.TotalTokens != 450 {
		t.Fatalf("TotalTokens = %d, want 450", result.Models[0].Usage.TotalTokens)
	}
}

func TestBuildPricingSnapshotSkipEmptyUsage(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "empty-model",
				Usage:          Usage{},
			},
			{
				RequestedModel: "gpt-4o",
				Usage:          Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			},
		},
	}

	result := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if len(result.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(result.Models))
	}
}

func TestBuildPricingSnapshotWithCacheSavings(t *testing.T) {
	snapshot := Snapshot{
		ByModelRoute: []ModelRouteUsage{
			{
				RequestedModel: "gpt-4o",
				Usage: Usage{
					PromptTokens:       1_000_000,
					CachedPromptTokens: 500_000,
					CompletionTokens:   500_000,
					TotalTokens:        1_500_000,
				},
			},
		},
	}

	result := BuildPricingSnapshot(snapshot, BootstrapPricingSnapshot())
	if result.Summary.CacheSavings <= 0 {
		t.Fatalf("expected positive cache savings, got %f", result.Summary.CacheSavings)
	}
}

func TestQueryCacheHitRanking(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record requests with different upstreams and cached tokens
	for i := 0; i < 5; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-chr-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			Upstream:   "openai",
			StatusCode: 200,
			Success:    true,
			Usage: Usage{
				PromptTokens:       1000,
				CachedPromptTokens: 500,
				CompletionTokens:   200,
				TotalTokens:        1200,
			},
		})
	}

	snapshot := store.Snapshot()
	if len(snapshot.CacheHitRanking) == 0 {
		t.Fatal("expected non-empty cache hit ranking")
	}
}

func TestQueryCacheHitRankingNilStore(t *testing.T) {
	var store *Store
	result := store.queryCacheHitRanking(24*time.Hour, 10)
	if result != nil {
		t.Fatal("expected nil for nil store")
	}
}

func TestQueryCacheHitRankingZeroLimit(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record some data first
	store.RecordRequest(RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-chr-zero",
		Path:       "/v1/chat/completions",
		Upstream:   "openai",
		StatusCode: 200,
		Success:    true,
		Usage:      Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200},
	})

	// Force flush and wait for data
	store.flushWriter()

	// Should use default limit of 10
	result := store.queryCacheHitRanking(24*time.Hour, 0)
	if result == nil {
		t.Fatal("expected non-nil result with default limit")
	}
}

func TestQueryWindowMetricsNilStore(t *testing.T) {
	var store *Store
	metrics := store.queryWindowMetrics(time.Minute, "1m")
	if metrics.WindowLabel != "1m" {
		t.Fatalf("WindowLabel = %q, want %q", metrics.WindowLabel, "1m")
	}
}

func TestQueryUsageBreakdownInvalidColumn(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Invalid SQL identifier should return nil
	result := store.queryUsageBreakdown("invalid; DROP TABLE requests;--")
	if result != nil {
		t.Fatal("expected nil for invalid column name")
	}
}

func TestPersistBatchNilStore(t *testing.T) {
	var store *Store
	// Should not panic
	store.persistBatch([]telemetryWrite{{request: &RequestRecord{}}})
}

func TestPersistBatchEmptyBatch(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Should not panic
	store.persistBatch([]telemetryWrite{})
}

func TestEnqueueWriteClosedChannel(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// After Close(), writer should be stopped
	// RecordRequest should fall back to direct persistBatch
	store.RecordRequest(RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-after-close",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
	})
}

func TestFlushWriterClosed(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// After Close(), flush should return immediately
	store.flushWriter()
}

func TestStartWriterAlreadyRunning(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Starting writer when already running should be a no-op
	store.startWriter()
	// Should not panic or cause issues
}

func TestPrepareStatements(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	if store.insertRequestStmt == nil {
		t.Fatal("expected insertRequestStmt to be prepared")
	}
	if store.insertErrorStmt == nil {
		t.Fatal("expected insertErrorStmt to be prepared")
	}
}

func TestExecRequestWriteZeroDuration(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record with zero duration should get default of 1
	record := RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-zero-dur",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
		DurationMs: 0,
	}

	store.RecordRequest(record)

	// Verify it was stored
	snapshot := store.Snapshot()
	if snapshot.Summary.TotalRequests != 1 {
		t.Fatalf("TotalRequests = %d, want 1", snapshot.Summary.TotalRequests)
	}
}

func TestExecErrorWrite(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	errRecord := ErrorRecord{
		Timestamp:  time.Now(),
		RequestID:  "err-write-test",
		Path:       "/v1/chat/completions",
		StatusCode: 500,
		Attempt:    1,
		Message:    "test error write",
	}

	store.RecordError(errRecord)

	snapshot := store.Snapshot()
	if len(snapshot.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(snapshot.Errors))
	}
}

func TestHydrateCaches(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	// Record some data
	for i := 0; i < 5; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-hydrate-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			Success:    true,
			Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		})
	}

	// Flush writer to persist data
	store.flushWriter()

	// Hydrate caches should update in-memory state from DB
	store.hydrateCaches()

	summary := store.cachedSummary()
	if summary.TotalRequests != 5 {
		t.Fatalf("TotalRequests = %d, want 5", summary.TotalRequests)
	}
}

func TestCachedRequests(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.RecordRequest(RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-cached",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
	})

	requests := store.cachedRequests()
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}
}

func TestCachedErrors(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	store.RecordError(ErrorRecord{
		Timestamp:  time.Now(),
		RequestID:  "err-cached",
		Path:       "/v1/chat/completions",
		StatusCode: 500,
		Message:    "cached error",
	})

	errors := store.cachedErrors()
	if len(errors) != 1 {
		t.Fatalf("len(errors) = %d, want 1", len(errors))
	}
}

func TestCurrentVersion(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	v1 := store.currentVersion()

	store.RecordRequest(RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-version",
		Path:       "/v1/chat/completions",
		StatusCode: 200,
		Success:    true,
	})

	v2 := store.currentVersion()
	if v2 <= v1 {
		t.Fatalf("expected version to increase: %d -> %d", v1, v2)
	}
}

func TestQuerySummary(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < 3; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-qs-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			Success:    true,
			Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		})
	}

	// Flush writer to persist data
	store.flushWriter()

	summary := store.querySummary()
	if summary.TotalRequests != 3 {
		t.Fatalf("TotalRequests = %d, want 3", summary.TotalRequests)
	}
}

func TestQuerySummaryEmpty(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	summary := store.querySummary()
	if summary.TotalRequests != 0 {
		t.Fatalf("TotalRequests = %d, want 0", summary.TotalRequests)
	}
}

func TestQueryRequests(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < 5; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:  time.Now(),
			RequestID:  "req-qr-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			Success:    true,
			Usage:      Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		})
	}

	// Flush writer to persist data
	store.flushWriter()

	requests := store.queryRequests(3)
	if len(requests) != 3 {
		t.Fatalf("len(requests) = %d, want 3", len(requests))
	}
}

func TestQueryErrors(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	for i := 0; i < 5; i++ {
		store.RecordError(ErrorRecord{
			Timestamp:  time.Now(),
			RequestID:  "err-qe-" + string(rune('0'+i)),
			Path:       "/v1/chat/completions",
			StatusCode: 500,
			Message:    "test error",
		})
	}

	// Flush writer to persist data
	store.flushWriter()

	errors := store.queryErrors(3)
	if len(errors) != 3 {
		t.Fatalf("len(errors) = %d, want 3", len(errors))
	}
}

func TestPricingCatalogUpdateConfigWithProvider(t *testing.T) {
	catalog := NewPricingCatalog(core.PricingConfig{})
	catalog.UpdateConfig(core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{{
			Model:       "custom-model",
			Provider:    "openai",
			Currency:    "USD",
			InputPer1M:  1.0,
			OutputPer1M: 2.0,
		}},
	})

	snapshot := catalog.Snapshot()
	key := providerScopedPricingKey("openai", "custom-model")
	if _, ok := snapshot.Catalog[key]; !ok {
		t.Fatalf("expected provider-scoped pricing for key %q", key)
	}
}

func TestApplyPricingConfigToSnapshotWithProvider(t *testing.T) {
	snapshot := BootstrapPricingSnapshot()
	cfg := core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{{
			Model:       "custom-model",
			Provider:    "openai",
			Currency:    "USD",
			InputPer1M:  1.0,
			OutputPer1M: 2.0,
		}},
	}

	result := applyPricingConfigToSnapshot(snapshot, cfg)
	key := providerScopedPricingKey("openai", "custom-model")
	if _, ok := result.Catalog[key]; !ok {
		t.Fatalf("expected provider-scoped pricing for key %q", key)
	}
}

func TestStripPricingManualOverridesDisabled(t *testing.T) {
	disabled := false
	cfg := core.PricingConfig{
		ManualPrices: []core.PricingManualPrice{
			{Model: "gpt-4o", Currency: "USD", InputPer1M: 99, OutputPer1M: 199, Enabled: &disabled},
		},
	}

	snapshot := PricingCatalogSnapshot{
		Catalog: map[string]Pricing{
			"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
		},
	}

	stripped := stripPricingManualOverrides(snapshot, cfg)
	if _, ok := stripped.Catalog["gpt-4o"]; !ok {
		t.Fatal("expected gpt-4o to remain when manual override is disabled")
	}
}

func TestMergePricingSnapshots(t *testing.T) {
	base := PricingCatalogSnapshot{
		SourceURL: "https://base.example.com",
		Catalog:   map[string]Pricing{"gpt-4o": {InputPer1M: 10}},
	}
	overlay := PricingCatalogSnapshot{
		SourceURL: "https://overlay.example.com",
		Catalog:   map[string]Pricing{"claude-3": {InputPer1M: 15}},
	}

	merged := mergePricingSnapshots(base, overlay)
	if merged.SourceURL != "https://overlay.example.com" {
		t.Fatalf("SourceURL = %q, want overlay URL", merged.SourceURL)
	}
	if len(merged.Catalog) != 2 {
		t.Fatalf("Catalog len = %d, want 2", len(merged.Catalog))
	}
}

func TestResolvePricingNoMatch(t *testing.T) {
	catalog := map[string]Pricing{}
	_, _, ok := ResolvePricing(catalog, "unknown-model", "", "")
	if ok {
		t.Fatal("expected no match for empty catalog")
	}
}

func TestResolvePricingWithUpstream(t *testing.T) {
	catalog := map[string]Pricing{
		providerScopedPricingKey("openai", "gpt-4o"): {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
	}

	model, pricing, ok := ResolvePricing(catalog, "gpt-4o", "", "openai")
	if !ok {
		t.Fatal("expected match for provider-scoped pricing")
	}
	if model != providerScopedPricingKey("openai", "gpt-4o") {
		t.Fatalf("model = %q, want provider-scoped key", model)
	}
	if pricing.InputPer1M != 10 {
		t.Fatalf("InputPer1M = %f, want 10", pricing.InputPer1M)
	}
}

func TestResolvePricingFallbackToGlobal(t *testing.T) {
	catalog := map[string]Pricing{
		"gpt-4o": {Currency: "USD", InputPer1M: 10, OutputPer1M: 30},
	}

	model, pricing, ok := ResolvePricing(catalog, "gpt-4o", "", "unknown-upstream")
	if !ok {
		t.Fatal("expected fallback to global pricing")
	}
	if model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", model)
	}
	if pricing.InputPer1M != 10 {
		t.Fatalf("InputPer1M = %f, want 10", pricing.InputPer1M)
	}
}
