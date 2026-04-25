package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestWithExecutionOptions_NilOptions(t *testing.T) {
	ctx := context.Background()
	result := WithExecutionOptions(ctx, nil)
	if result != ctx {
		t.Error("WithExecutionOptions with nil opts should return original context")
	}
}

func TestWithExecutionOptions_WithValue(t *testing.T) {
	ctx := context.Background()
	opts := &ExecutionOptions{RequestID: "test-123"}
	result := WithExecutionOptions(ctx, opts)

	retrieved := result.Value(executionOptionsKey{})
	if retrieved == nil {
		t.Fatal("expected options to be stored in context")
	}

	retrievedOpts, ok := retrieved.(*ExecutionOptions)
	if !ok {
		t.Fatalf("expected *ExecutionOptions, got %T", retrieved)
	}
	if retrievedOpts.RequestID != "test-123" {
		t.Errorf("expected RequestID test-123, got %s", retrievedOpts.RequestID)
	}
}

func TestExecutionResult_Snapshot_NilReceiver(t *testing.T) {
	var r *ExecutionResult
	snapshot := r.Snapshot()
	if snapshot.StatusCode != 0 {
		t.Error("expected zero-value snapshot for nil receiver")
	}
}

func TestExecutionResult_Snapshot_ThreadSafety(t *testing.T) {
	result := &ExecutionResult{
		StatusCode:          200,
		ContentType:         "application/json",
		Latency:             100 * time.Millisecond,
		PromptTokens:        1000,
		CachedPromptTokens:  500,
		CompletionTokens:    2000,
		ProviderID:          "provider-1",
		EffectiveModel:      "gpt-4",
		RouteMode:           "standard",
		PricingTotalCostUSD: 0.05,
		Error:               "",
	}

	var wg sync.WaitGroup
	const numReaders = 100

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot := result.Snapshot()
			if snapshot.StatusCode != 200 {
				t.Errorf("expected StatusCode 200, got %d", snapshot.StatusCode)
			}
		}()
	}

	wg.Wait()
}

func TestExecutionResult_Snapshot_CopyIndependence(t *testing.T) {
	result := &ExecutionResult{
		StatusCode:     200,
		EffectiveModel: "original-model",
	}

	snapshot := result.Snapshot()

	// Modify original
	result.StatusCode = 404
	result.EffectiveModel = "modified-model"

	// Snapshot should retain original values
	if snapshot.StatusCode != 200 {
		t.Errorf("expected snapshot StatusCode 200, got %d", snapshot.StatusCode)
	}
	if snapshot.EffectiveModel != "original-model" {
		t.Errorf("expected snapshot EffectiveModel original-model, got %s", snapshot.EffectiveModel)
	}
}

func TestCloneHeadersForRPC_NilHeaders(t *testing.T) {
	result := CloneHeadersForRPC(nil)
	if result != nil {
		t.Errorf("expected nil for nil headers, got %v", result)
	}
}

func TestCloneHeadersForRPC_EmptyHeaders(t *testing.T) {
	headers := http.Header{}
	result := CloneHeadersForRPC(headers)
	if result != nil {
		t.Errorf("expected nil for empty headers, got %v", result)
	}
}

func TestCloneHeadersForRPC_SingleValue(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")

	result := CloneHeadersForRPC(headers)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	values, ok := result["Content-Type"]
	if !ok {
		t.Fatal("expected Content-Type key in result")
	}
	if len(values) != 1 || values[0] != "application/json" {
		t.Errorf("expected [application/json], got %v", values)
	}
}

func TestCloneHeadersForRPC_MultipleValues(t *testing.T) {
	headers := http.Header{}
	headers.Add("Accept", "application/json")
	headers.Add("Accept", "text/plain")

	result := CloneHeadersForRPC(headers)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	values, ok := result["Accept"]
	if !ok {
		t.Fatal("expected Accept key in result")
	}
	if len(values) != 2 {
		t.Errorf("expected 2 values, got %d", len(values))
	}
	if values[0] != "application/json" || values[1] != "text/plain" {
		t.Errorf("expected [application/json, text/plain], got %v", values)
	}
}

func TestCloneHeadersForRPC_Independence(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer token123")

	result := CloneHeadersForRPC(headers)

	// Modify original
	headers.Set("Authorization", "Bearer newtoken")

	// Cloned result should be independent
	if result["Authorization"][0] != "Bearer token123" {
		t.Errorf("cloned headers should be independent from original")
	}
}
