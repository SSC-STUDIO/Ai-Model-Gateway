package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-model-gateway/internal/config"
)

func TestHTTPHealthChecker_Check_Success(t *testing.T) {
	// Create a test server that returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected path /v1/models, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
	}
	healthCfg := config.HealthConfig{
		TimeoutMs: 2000,
		Path:      "/v1/models",
	}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if !result.Healthy {
		t.Errorf("expected healthy=true, got false")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
}

func TestHTTPHealthChecker_Check_WithAPIKey(t *testing.T) {
	expectedToken := "test-api-key"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		expectedHeader := "Bearer " + expectedToken
		if authHeader != expectedHeader {
			t.Errorf("expected Authorization header %s, got %s", expectedHeader, authHeader)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  expectedToken,
	}
	healthCfg := config.HealthConfig{TimeoutMs: 2000}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if !result.Healthy {
		t.Error("expected healthy upstream")
	}
}

func TestHTTPHealthChecker_Check_WithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Error("expected custom header to be set")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	}
	healthCfg := config.HealthConfig{TimeoutMs: 2000}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if !result.Healthy {
		t.Error("expected healthy upstream")
	}
}

func TestHTTPHealthChecker_Check_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
	}
	healthCfg := config.HealthConfig{TimeoutMs: 2000}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if result.Healthy {
		t.Error("expected unhealthy for 503 response")
	}
	if result.Error == nil {
		t.Error("expected error for 503 response")
	}
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, result.StatusCode)
	}
}

func TestHTTPHealthChecker_Check_ConnectionError(t *testing.T) {
	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: "http://localhost:1", // Invalid port, should fail
	}
	healthCfg := config.HealthConfig{
		TimeoutMs: 100,
	}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if result.Healthy {
		t.Error("expected unhealthy for connection error")
	}
	if result.Error == nil {
		t.Error("expected error for connection failure")
	}
}

func TestHTTPHealthChecker_Check_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
	}
	healthCfg := config.HealthConfig{
		TimeoutMs: 50, // Very short timeout
	}

	result := checker.Check(context.Background(), upstream, healthCfg)

	if result.Healthy {
		t.Error("expected unhealthy for timeout")
	}
	if result.Error == nil {
		t.Error("expected error for timeout")
	}
}

func TestHTTPHealthChecker_DefaultTimeout(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPHealthChecker(nil)
	upstream := config.Upstream{
		Name:    "test",
		BaseURL: server.URL,
	}
	// Don't set timeout - should use default
	healthCfg := config.HealthConfig{}

	result := checker.Check(context.Background(), upstream, healthCfg)

	// Should succeed with default 2 second timeout
	if !result.Healthy {
		t.Errorf("expected healthy with default timeout, got error: %v", result.Error)
	}
}

func TestMockHealthChecker(t *testing.T) {
	checker := NewMockHealthChecker()

	// Set up results
	checker.SetResult("healthy", HealthResult{
		Healthy:    true,
		Latency:    10 * time.Millisecond,
		StatusCode: 200,
	})
	checker.SetResult("unhealthy", HealthResult{
		Healthy:    false,
		Latency:    0,
		StatusCode: 503,
		Error:      errors.New("service unavailable"),
	})

	// Test healthy upstream
	healthyUpstream := config.Upstream{Name: "healthy"}
	result := checker.Check(context.Background(), healthyUpstream, config.HealthConfig{})
	if !result.Healthy {
		t.Error("expected healthy result")
	}
	if result.Latency != 10*time.Millisecond {
		t.Errorf("expected latency 10ms, got %v", result.Latency)
	}

	// Test unhealthy upstream
	unhealthyUpstream := config.Upstream{Name: "unhealthy"}
	result = checker.Check(context.Background(), unhealthyUpstream, config.HealthConfig{})
	if result.Healthy {
		t.Error("expected unhealthy result")
	}
	if result.Error == nil {
		t.Error("expected error")
	}

	// Test unknown upstream defaults to healthy
	unknownUpstream := config.Upstream{Name: "unknown"}
	result = checker.Check(context.Background(), unknownUpstream, config.HealthConfig{})
	if !result.Healthy {
		t.Error("expected healthy for unknown upstream")
	}
}

func TestMockHealthChecker_DefaultResult(t *testing.T) {
	checker := NewMockHealthChecker()

	upstream := config.Upstream{Name: "test"}
	result := checker.Check(context.Background(), upstream, config.HealthConfig{})

	if !result.Healthy {
		t.Error("expected default healthy result")
	}
}

func TestHealthResult_Struct(t *testing.T) {
	testErr := errors.New("test error")
	result := HealthResult{
		Healthy:    false,
		Latency:    100 * time.Millisecond,
		Error:      testErr,
		StatusCode: 500,
	}

	if result.Healthy {
		t.Error("expected Healthy=false")
	}
	if result.Latency != 100*time.Millisecond {
		t.Errorf("expected Latency=100ms, got %v", result.Latency)
	}
	if result.Error != testErr {
		t.Error("expected matching error")
	}
	if result.StatusCode != 500 {
		t.Errorf("expected StatusCode=500, got %d", result.StatusCode)
	}
}
