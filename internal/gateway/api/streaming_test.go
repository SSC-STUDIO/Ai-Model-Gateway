package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

func TestIsSSE(t *testing.T) {
	tests := []struct {
		name     string
		ct       string
		expected bool
	}{
		{"empty content type", "", false},
		{"text/event-stream exact", "text/event-stream", true},
		{"text/event-stream with charset", "text/event-stream; charset=utf-8", true},
		{"text/event-stream with whitespace", "  text/event-stream  ", true},
		{"application/json", "application/json", false},
		{"text/plain", "text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{"Content-Type": []string{tt.ct}},
			}
			got := isSSE(resp)
			if got != tt.expected {
				t.Errorf("isSSE(%q) = %v, want %v", tt.ct, got, tt.expected)
			}
		})
	}
}

func TestResolveBridgeModel(t *testing.T) {
	snap := &snapshot.Snapshot{
		CompatPolicy: snapshot.CompatPolicy{
			Bridge: snapshot.BridgePolicy{
				Enabled: true,
				Rules: []snapshot.BridgeRule{
					{From: "gpt-*", To: "claude-*"},
					{From: "text-*", To: ""},
				},
				ExcludeUserAgents: []string{"test-exclude"},
			},
		},
	}

	tests := []struct {
		name      string
		snap      *snapshot.Snapshot
		model     string
		userAgent string
		expected  string
	}{
		{"nil snapshot", nil, "gpt-4", "", "gpt-4"},
		{"bridge disabled", &snapshot.Snapshot{}, "gpt-4", "", "gpt-4"},
		{"empty model", snap, "", "", ""},
		{"match wildcard", snap, "gpt-4", "", "claude-*"},
		{"no match", snap, "unknown", "", "unknown"},
		{"excluded user agent", snap, "gpt-4", "test-exclude", "gpt-4"},
		{"different excluded user agent", snap, "gpt-4", "allowed-agent", "claude-*"},
		{"empty exclude", snap, "text-davinci", "", "text-davinci"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBridgeModel(tt.snap, tt.model, tt.userAgent)
			if got != tt.expected {
				t.Errorf("resolveBridgeModel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWildcardMatch(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		value    string
		expected bool
	}{
		{"empty pattern", "", "anything", false},
		{"exact match", "hello", "hello", true},
		{"case insensitive", "Hello", "hello", true},
		{"wildcard star pattern", "gpt-*", "gpt-4", true},
		{"wildcard star no match", "gpt-*", "claude-3", false},
		{"wildcard prefix", "gpt*", "gpt-4-turbo", true},
		{"trailing whitespace", "  hello  ", "hello", true},
		{"value with whitespace", "hello", "  hello  ", true},
		{"partial string no star", "gpt", "gpt-4", false},
		{"empty value", "gpt-*", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wildcardMatch(tt.pattern, tt.value)
			if got != tt.expected {
				t.Errorf("wildcardMatch(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.expected)
			}
		})
	}
}

func TestCopyStreamingBody_ClientDisconnect(t *testing.T) {
	// Create a pipe that will be closed from the writer side to simulate client disconnect.
	pr, pw := io.Pipe()

	// Write some data then close the writer side to signal EOF.
	go func() {
		_, _ = io.WriteString(pw, "data: chunk1\n\n")
		pw.Close()
	}()

	rec := httptest.NewRecorder()

	// ResponseRecorder does not implement http.Flusher, so pass nil
	// to exercise the non-flusher path.
	promptTokens, cachedTokens, completionTokens := copyStreamingBody(rec, pr, nil)
	if promptTokens != 0 || cachedTokens != 0 || completionTokens != 0 {
		t.Fatalf("expected all zero tokens for no-usage SSE, got prompt=%d cached=%d completion=%d",
			promptTokens, cachedTokens, completionTokens)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "chunk1") {
		t.Fatalf("expected body to contain 'chunk1', got %q", body)
	}
}

func TestCopyStreamingBody_NilBody(t *testing.T) {
	rec := httptest.NewRecorder()
	pt, cpt, ct := copyStreamingBody(rec, nil, nil)
	if pt != 0 || cpt != 0 || ct != 0 {
		t.Fatalf("expected zero tokens for nil body, got prompt=%d cached=%d completion=%d", pt, cpt, ct)
	}
}

func TestCopyStreamingBody_ClosedBodyReturnsZeroTokens(t *testing.T) {
	// A body that immediately returns EOF should yield zero tokens.
	body := io.NopCloser(strings.NewReader(""))
	rec := httptest.NewRecorder()

	pt, cpt, ct := copyStreamingBody(rec, body, nil)
	if pt != 0 || cpt != 0 || ct != 0 {
		t.Fatalf("expected zero tokens for empty body, got prompt=%d cached=%d completion=%d", pt, cpt, ct)
	}
}

func TestHandleStreamResponse_UsageExtraction(t *testing.T) {
	// Simulate SSE response with usage data at the end.
	sseData := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":2}}}\n\n"

	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, sseData)
		pw.Close()
	}()

	rec := httptest.NewRecorder()

	// Test copyStreamingBody directly to verify usage extraction (without flusher).
	promptTokens, cachedTokens, completionTokens := copyStreamingBody(rec, pr, nil)

	if promptTokens != 5 {
		t.Errorf("expected promptTokens=5, got %d", promptTokens)
	}
	if cachedTokens != 2 {
		t.Errorf("expected cachedTokens=2, got %d", cachedTokens)
	}
	if completionTokens != 7 {
		t.Errorf("expected completionTokens=7, got %d", completionTokens)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "hello") {
		t.Errorf("expected body to contain hello, got %q", body)
	}
}

func TestHandleStreamResponse_WritesHeadersAndBody(t *testing.T) {
	body := io.NopCloser(strings.NewReader("data: test\n\n"))
	rec := httptest.NewRecorder()

	pt, cpt, ct := handleStreamResponse(rec, http.StatusOK, "", body)
	if pt != 0 || cpt != 0 || ct != 0 {
		t.Fatalf("expected zero tokens, got prompt=%d cached=%d completion=%d", pt, cpt, ct)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %q", cc)
	}
	if conn := rec.Header().Get("Connection"); conn != "keep-alive" {
		t.Fatalf("expected Connection keep-alive, got %q", conn)
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "test") {
		t.Fatalf("expected body to contain 'test', got %q", bodyStr)
	}
}

func TestHandleStreamResponse_CustomContentType(t *testing.T) {
	body := io.NopCloser(strings.NewReader(""))
	rec := httptest.NewRecorder()

	handleStreamResponse(rec, http.StatusOK, "text/plain", body)

	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("expected Content-Type text/plain, got %q", ct)
	}
}

func TestStartStreamRetrySession_ServesHeartbeat(t *testing.T) {
	previousInterval := streamRetryHeartbeatInterval
	streamRetryHeartbeatInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		streamRetryHeartbeatInterval = previousInterval
	})

	rec := httptest.NewRecorder()
	session := startStreamRetrySession(rec)
	if session == nil {
		t.Fatal("expected non-nil session")
	}

	// Let at least one keep-alive tick fire before stopping.
	time.Sleep(15 * time.Millisecond)
	session.Stop()

	body := rec.Body.String()
	if !strings.Contains(body, ": aigw waiting for upstream") {
		t.Fatalf("expected initial heartbeat comment in body, got %q", body)
	}
	if !strings.Contains(body, ": aigw keep-alive") {
		t.Fatalf("expected keep-alive heartbeat after tick, got body=%q", body)
	}
}

func TestStartStreamRetrySession_StopStopsHeartbeat(t *testing.T) {
	previousInterval := streamRetryHeartbeatInterval
	streamRetryHeartbeatInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		streamRetryHeartbeatInterval = previousInterval
	})

	rec := httptest.NewRecorder()
	session := startStreamRetrySession(rec)
	if session == nil {
		t.Fatal("expected non-nil session")
	}

	session.Stop()

	// After stop, no more heartbeats should be sent.
	body := rec.Body.String()
	heartbeatCount := strings.Count(body, ": aigw keep-alive")
	if heartbeatCount > 1 {
		t.Fatalf("expected at most 1 keep-alive after stop, got %d heartbeats in body=%q", heartbeatCount, body)
	}
}

func TestStreamRetrySession_StopNil(t *testing.T) {
	// Calling Stop on a nil session should not panic.
	var s *streamRetrySession
	s.Stop()
}

func TestStartNonStreamKeepAlive(t *testing.T) {
	previousInterval := nonStreamKeepAliveInterval
	nonStreamKeepAliveInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		nonStreamKeepAliveInterval = previousInterval
	})

	rec := httptest.NewRecorder()
	session := startNonStreamKeepAlive(rec)
	if session == nil {
		t.Fatal("expected non-nil session")
	}

	// Wait for at least one heartbeat.
	time.Sleep(30 * time.Millisecond)

	session.Stop()

	body := rec.Body.String()
	if !strings.Contains(body, " ") {
		t.Fatal("expected space heartbeat in body")
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty body after keep-alive session")
	}
}

func TestNonStreamKeepAlive_StopNil(t *testing.T) {
	var s *nonStreamKeepAliveSession
	s.Stop()
}

func TestCancelOnClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := io.NopCloser(strings.NewReader("hello"))
	wrapped := cancelOnClose(body, cancel)
	if wrapped == nil {
		t.Fatal("expected non-nil wrapped body")
	}

	// Closing the wrapped body should invoke the cancel func.
	err := wrapped.Close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}

	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled after close")
	}
}

func TestCancelOnClose_NilBody(t *testing.T) {
	wrapped := cancelOnClose(nil, nil)
	if wrapped != nil {
		t.Fatal("expected nil for nil body")
	}
}
