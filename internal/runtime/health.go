package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HealthChecker performs HTTP health checks against the gateway.
type HealthChecker struct {
	endpoint string
	timeout  time.Duration
	client   *http.Client
}

// HealthResult represents the health check response.
type HealthResult struct {
	Status          string   `json:"status"`
	RouterStrategy  string   `json:"router_strategy,omitempty"`
	BridgeEnabled   bool     `json:"bridge_enabled,omitempty"`
	AvailableModels []string `json:"available_models,omitempty"`
	Upstreams       []string `json:"upstreams,omitempty"`
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(endpoint string, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		endpoint: endpoint,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
}

// Check performs the health check and returns the result.
func (h *HealthChecker) Check() (*HealthResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Try to parse JSON
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result HealthResult
	if err := json.Unmarshal(body, &result); err != nil {
		// Non-JSON response, but status 200 means healthy
		return &HealthResult{Status: "healthy"}, nil
	}

	return &result, nil
}
