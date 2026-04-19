package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(10, 5)
	if l == nil {
		t.Fatal("expected non-nil Limiter")
	}
	if l.rps != 10 {
		t.Fatalf("expected rps=10, got %v", l.rps)
	}
	if l.burst != 5 {
		t.Fatalf("expected burst=5, got %v", l.burst)
	}
}

func TestAllow_WithinLimit(t *testing.T) {
	l := NewLimiter(100, 10)

	for i := 0; i < 10; i++ {
		if !l.Allow("key1") {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	l := NewLimiter(100, 3)

	// Consume all burst tokens.
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}

	// Next request should be denied.
	if l.Allow("key1") {
		t.Fatal("expected request to be denied after burst exhausted")
	}
}

func TestAllow_RefillOverTime(t *testing.T) {
	l := NewLimiter(10, 1)

	// Consume the single burst token.
	if !l.Allow("key1") {
		t.Fatal("expected first request to be allowed")
	}
	if l.Allow("key1") {
		t.Fatal("expected second request to be denied")
	}

	// Wait for token refill.
	time.Sleep(150 * time.Millisecond)

	if !l.Allow("key1") {
		t.Fatal("expected request to be allowed after refill")
	}
}

func TestAllow_IndependentKeys(t *testing.T) {
	l := NewLimiter(100, 2)

	// Exhaust burst for key1.
	for i := 0; i < 2; i++ {
		if !l.Allow("key1") {
			t.Fatalf("expected key1 request %d to be allowed", i+1)
		}
	}
	if l.Allow("key1") {
		t.Fatal("expected key1 request to be denied")
	}

	// key2 should still have its own burst.
	for i := 0; i < 2; i++ {
		if !l.Allow("key2") {
			t.Fatalf("expected key2 request %d to be allowed", i+1)
		}
	}
	if l.Allow("key2") {
		t.Fatal("expected key2 request to be denied")
	}
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Bearer abc123", "abc123"},
		{"Bearer  abc123", "abc123"},
		{"bearer abc123", ""}, // case sensitive
		{"Token abc123", ""},  // wrong scheme
		{"", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
	}

	for _, tt := range tests {
		result := extractAPIKey(tt.input)
		if result != tt.expected {
			t.Errorf("extractAPIKey(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMiddleware_AllowsRequest(t *testing.T) {
	l := NewLimiter(100, 10)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer testkey")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "ok") {
		t.Fatalf("expected body 'ok', got %q", rr.Body.String())
	}
}

func TestMiddleware_RateLimited(t *testing.T) {
	l := NewLimiter(100, 2)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust burst.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer limitedkey")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status %d on request %d, got %d", http.StatusOK, i+1, rr.Code)
		}
	}

	// Next request should be rate limited.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer limitedkey")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "rate limit exceeded") {
		t.Fatalf("expected rate limit error body, got %q", rr.Body.String())
	}
}

func TestMiddleware_NoAuthHeader(t *testing.T) {
	l := NewLimiter(100, 0) // zero burst so any keyed request would be denied

	called := false
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected downstream handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestMiddleware_InvalidAuthFormat(t *testing.T) {
	l := NewLimiter(100, 0) // zero burst

	called := false
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected downstream handler to be called for invalid auth format")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}
