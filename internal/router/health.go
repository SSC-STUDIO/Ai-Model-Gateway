package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"ai-model-gateway/internal/config"
)

// HealthChecker defines the interface for health checking upstreams.
// This abstraction allows for testable health checking logic.
type HealthChecker interface {
	// Check performs a health check on the given upstream and returns the result.
	Check(ctx context.Context, upstream config.Upstream, cfg config.HealthConfig) HealthResult
}

// HealthResult represents the outcome of a health check.
type HealthResult struct {
	// Healthy indicates whether the upstream is healthy.
	Healthy bool
	// Latency is the response time of the health check.
	Latency time.Duration
	// Error contains any error that occurred during the check.
	Error error
	// StatusCode is the HTTP status code received (if applicable).
	StatusCode int
}

// HTTPHealthChecker implements HealthChecker using HTTP probes.
type HTTPHealthChecker struct {
	client *http.Client
}

// NewHTTPHealthChecker creates a new HTTP-based health checker.
func NewHTTPHealthChecker(client *http.Client) *HTTPHealthChecker {
	if client == nil {
		client = &http.Client{
			Timeout: 2 * time.Second,
		}
	}
	return &HTTPHealthChecker{client: client}
}

// Check performs an HTTP health check against the upstream.
func (h *HTTPHealthChecker) Check(ctx context.Context, upstream config.Upstream, cfg config.HealthConfig) HealthResult {
	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := joinURL(upstream.BaseURL, cfg.Path)
	if cfg.Path == "" {
		url = joinURL(upstream.BaseURL, "/v1/models")
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return HealthResult{
			Healthy: false,
			Error:   err,
		}
	}

	// Set authorization header if API key is available
	if upstream.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	}

	// Set custom headers
	for key, value := range upstream.Headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := h.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return HealthResult{
			Healthy: false,
			Latency: latency,
			Error:   err,
		}
	}
	defer resp.Body.Close()

	// Drain response body to allow connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		return HealthResult{
			Healthy:    false,
			Latency:    latency,
			StatusCode: resp.StatusCode,
			Error:      fmt.Errorf("health check failed with status %d", resp.StatusCode),
		}
	}

	return HealthResult{
		Healthy:    true,
		Latency:    latency,
		StatusCode: resp.StatusCode,
	}
}

// MockHealthChecker is a testable implementation of HealthChecker.
type MockHealthChecker struct {
	Results map[string]HealthResult
}

// NewMockHealthChecker creates a new mock health checker.
func NewMockHealthChecker() *MockHealthChecker {
	return &MockHealthChecker{
		Results: make(map[string]HealthResult),
	}
}

// SetResult configures the result for a specific upstream.
func (m *MockHealthChecker) SetResult(upstreamName string, result HealthResult) {
	if m.Results == nil {
		m.Results = make(map[string]HealthResult)
	}
	m.Results[upstreamName] = result
}

// Check returns the configured result for the upstream.
func (m *MockHealthChecker) Check(ctx context.Context, upstream config.Upstream, cfg config.HealthConfig) HealthResult {
	if result, ok := m.Results[upstream.Name]; ok {
		return result
	}
	return HealthResult{Healthy: true}
}
