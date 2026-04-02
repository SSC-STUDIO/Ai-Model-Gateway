package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitMiddlewareAllowsSingleAPIRequest(t *testing.T) {
	limiter := NewRateLimiter(DefaultRateLimitConfig())
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for first API request, got %d", recorder.Code)
	}
}

func TestRateLimitMiddlewareAllowsSingleLoginRequest(t *testing.T) {
	limiter := NewRateLimiter(DefaultRateLimitConfig())
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "192.0.2.11:1234"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for first login request, got %d", recorder.Code)
	}
}
