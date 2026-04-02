package telemetry

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStoreSnapshotCachesSummaryAndRecentRows(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     1234,
		Success:        true,
		Usage: Usage{
			PromptTokens:       10,
			CachedPromptTokens: 4,
			CompletionTokens:   6,
			TotalTokens:        16,
		},
	})
	store.RecordError(ErrorRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     503,
		Attempt:        1,
		Message:        "upstream failed",
	})

	snapshot := store.Snapshot()
	if snapshot.Summary.TotalRequests != 1 {
		t.Fatalf("expected 1 total request, got %d", snapshot.Summary.TotalRequests)
	}
	if snapshot.Summary.Successes != 1 || snapshot.Summary.Failures != 0 {
		t.Fatalf("unexpected success/failure summary: %+v", snapshot.Summary)
	}
	if snapshot.Summary.TotalTokens != 16 || snapshot.Summary.CachedPromptTokens != 4 {
		t.Fatalf("unexpected token summary: %+v", snapshot.Summary)
	}
	if len(snapshot.Requests) != 1 || snapshot.Requests[0].RequestID != "req-1" {
		t.Fatalf("expected recent request cache to return req-1, got %+v", snapshot.Requests)
	}
	if len(snapshot.Errors) != 1 || snapshot.Errors[0].Message != "upstream failed" {
		t.Fatalf("expected recent error cache to return the recorded error, got %+v", snapshot.Errors)
	}
}

func TestStoreHydratesCachesAndCapsRecentRowsOnReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	base := time.Now().Add(-time.Hour)
	for i := 0; i < recentRequestLimit+5; i++ {
		id := "req-" + strconv.Itoa(i)
		ts := base.Add(time.Duration(i) * time.Millisecond)
		store.RecordRequest(RequestRecord{
			Timestamp:      ts,
			RequestID:      id,
			Path:           "/v1/responses",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     200,
			Attempts:       1,
			DurationMs:     int64(100 + i),
			Success:        true,
			Usage: Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		})
		store.RecordError(ErrorRecord{
			Timestamp:      ts,
			RequestID:      id,
			Path:           "/v1/responses",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     503,
			Attempt:        1,
			Message:        "boom-" + strconv.Itoa(i),
		})
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		_ = reopened.Close()
	})

	snapshot := reopened.Snapshot()
	if snapshot.Summary.TotalRequests != recentRequestLimit+5 {
		t.Fatalf("expected %d total requests, got %d", recentRequestLimit+5, snapshot.Summary.TotalRequests)
	}
	if len(snapshot.Requests) != recentRequestLimit {
		t.Fatalf("expected %d cached recent requests, got %d", recentRequestLimit, len(snapshot.Requests))
	}
	if len(snapshot.Errors) != recentErrorLimit {
		t.Fatalf("expected %d cached recent errors, got %d", recentErrorLimit, len(snapshot.Errors))
	}
	if snapshot.Requests[0].RequestID != "req-204" {
		t.Fatalf("expected newest request req-204 first, got %q", snapshot.Requests[0].RequestID)
	}
	if snapshot.Errors[0].RequestID != "req-204" {
		t.Fatalf("expected newest error req-204 first, got %q", snapshot.Errors[0].RequestID)
	}
	if snapshot.Requests[len(snapshot.Requests)-1].RequestID != "req-5" {
		t.Fatalf("expected capped oldest request req-5 last, got %q", snapshot.Requests[len(snapshot.Requests)-1].RequestID)
	}
	if snapshot.Errors[len(snapshot.Errors)-1].RequestID != "req-5" {
		t.Fatalf("expected capped oldest error req-5 last, got %q", snapshot.Errors[len(snapshot.Errors)-1].RequestID)
	}
}

func TestStoreSnapshotReusesCacheUntilNewWritesArrive(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     120,
		Success:        true,
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 4,
			TotalTokens:      14,
		},
	})

	first := store.Snapshot()
	second := store.Snapshot()
	if !first.GeneratedAt.Equal(second.GeneratedAt) {
		t.Fatalf("expected second snapshot to reuse cached payload timestamp, got %s then %s", first.GeneratedAt, second.GeneratedAt)
	}

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-2",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     140,
		Success:        true,
		Usage: Usage{
			PromptTokens:     7,
			CompletionTokens: 3,
			TotalTokens:      10,
		},
	})

	third := store.Snapshot()
	if third.Summary.TotalRequests != 2 {
		t.Fatalf("expected snapshot to invalidate after new write, got %+v", third.Summary)
	}
	if !third.GeneratedAt.After(second.GeneratedAt) {
		t.Fatalf("expected rebuilt snapshot timestamp after cache invalidation, got %s then %s", second.GeneratedAt, third.GeneratedAt)
	}
}

func TestStoreSnapshotClockRollbackDoesNotExtendCacheExpiry(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-1",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     120,
		Success:        true,
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 4,
			TotalTokens:      14,
		},
	})

	_ = store.Snapshot()

	futureGeneratedAt := time.Now().Add(30 * time.Second)
	store.cacheMu.Lock()
	store.snapshotCache.expires = time.Now().Add(-time.Millisecond)
	store.snapshotCache.value.GeneratedAt = futureGeneratedAt
	store.cacheMu.Unlock()

	rebuilt := store.Snapshot()
	if !rebuilt.GeneratedAt.After(futureGeneratedAt) {
		t.Fatalf("expected generated_at to remain monotonic after rollback, got %s <= %s", rebuilt.GeneratedAt, futureGeneratedAt)
	}

	cacheExpiryUpperBound := time.Now().Add(snapshotCacheTTL + 500*time.Millisecond)
	store.cacheMu.Lock()
	expires := store.snapshotCache.expires
	store.cacheMu.Unlock()
	if expires.After(cacheExpiryUpperBound) {
		t.Fatalf("expected cache expiry to stay near wall clock, got %s > %s", expires, cacheExpiryUpperBound)
	}
}

func TestStoreQueryTimeSeriesCachesByWindowAndInvalidatesOnWrite(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	base := time.Now().Add(-5 * time.Minute)
	for i := 0; i < 2; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:      base.Add(time.Duration(i) * time.Minute),
			RequestID:      "req-" + strconv.Itoa(i),
			Path:           "/v1/responses",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     200,
			Attempts:       1,
			DurationMs:     100,
			Success:        true,
			Usage: Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		})
	}

	first := store.QueryTimeSeries(24, 60)
	second := store.QueryTimeSeries(24, 60)
	if len(first.Buckets) != len(second.Buckets) {
		t.Fatalf("expected cached timeseries to preserve bucket shape, got %d and %d", len(first.Buckets), len(second.Buckets))
	}
	cached := store.timeSeriesCache["24:60"]
	if cached.version != store.currentVersion() {
		t.Fatalf("expected timeseries cache version %d, got %d", store.currentVersion(), cached.version)
	}

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-3",
		Path:           "/v1/responses",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     90,
		Success:        true,
		Usage: Usage{
			PromptTokens:     2,
			CompletionTokens: 1,
			TotalTokens:      3,
		},
	})

	updated := store.QueryTimeSeries(24, 60)
	if len(updated.Buckets) == 0 {
		t.Fatalf("expected rebuilt timeseries buckets after new write")
	}
	if store.timeSeriesCache["24:60"].version != store.currentVersion() {
		t.Fatalf("expected timeseries cache to update to version %d", store.currentVersion())
	}
}

func TestStoreCloseFlushesPendingWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "telemetry.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	store.RecordRequest(RequestRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-close",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     200,
		Attempts:       1,
		DurationMs:     100,
		Success:        true,
		Usage: Usage{
			PromptTokens:     2,
			CompletionTokens: 1,
			TotalTokens:      3,
		},
	})
	store.RecordError(ErrorRecord{
		Timestamp:      time.Now(),
		RequestID:      "req-close",
		Path:           "/v1/chat/completions",
		RequestedModel: "gpt-5.4",
		Model:          "gpt-5.4",
		Upstream:       "alpha",
		StatusCode:     503,
		Attempt:        1,
		Message:        "boom-close",
	})

	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		_ = reopened.Close()
	})

	snapshot := reopened.Snapshot()
	if snapshot.Summary.TotalRequests != 1 {
		t.Fatalf("expected pending request to persist on close, got %+v", snapshot.Summary)
	}
	if len(snapshot.Errors) != 1 || snapshot.Errors[0].Message != "boom-close" {
		t.Fatalf("expected pending error to persist on close, got %+v", snapshot.Errors)
	}
}

func BenchmarkStoreSnapshotCached(b *testing.B) {
	store, err := NewStore(filepath.Join(b.TempDir(), "telemetry.db"))
	if err != nil {
		b.Fatalf("new store: %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 256; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:      base.Add(time.Duration(i) * time.Second),
			RequestID:      "req-" + strconv.Itoa(i),
			Path:           "/v1/chat/completions",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     200,
			Attempts:       1,
			DurationMs:     100,
			Success:        true,
			Usage: Usage{
				PromptTokens:       10,
				CachedPromptTokens: 4,
				CompletionTokens:   4,
				TotalTokens:        14,
			},
		})
	}

	_ = store.Snapshot()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Snapshot()
	}
}

func BenchmarkStoreRecordRequestHotPath(b *testing.B) {
	store, err := NewStore(filepath.Join(b.TempDir(), "telemetry.db"))
	if err != nil {
		b.Fatalf("new store: %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})

	base := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:      base.Add(time.Duration(i) * time.Millisecond),
			RequestID:      "req-" + strconv.Itoa(i),
			Path:           "/v1/chat/completions",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     200,
			Attempts:       1,
			DurationMs:     100,
			Success:        true,
			Usage: Usage{
				PromptTokens:       10,
				CachedPromptTokens: 4,
				CompletionTokens:   4,
				TotalTokens:        14,
			},
		})
	}
}

func BenchmarkStoreQueryTimeSeriesCached(b *testing.B) {
	store, err := NewStore(filepath.Join(b.TempDir(), "telemetry.db"))
	if err != nil {
		b.Fatalf("new store: %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})

	base := time.Now().Add(-6 * time.Hour)
	for i := 0; i < 512; i++ {
		store.RecordRequest(RequestRecord{
			Timestamp:      base.Add(time.Duration(i) * time.Minute),
			RequestID:      "req-" + strconv.Itoa(i),
			Path:           "/v1/responses",
			RequestedModel: "gpt-5.4",
			Model:          "gpt-5.4",
			Upstream:       "alpha",
			StatusCode:     200,
			Attempts:       1,
			DurationMs:     100,
			Success:        true,
			Usage: Usage{
				PromptTokens:     8,
				CompletionTokens: 4,
				TotalTokens:      12,
			},
		})
	}

	_ = store.QueryTimeSeries(24, 60)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.QueryTimeSeries(24, 60)
	}
}
