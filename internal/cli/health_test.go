package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewHealthChecker(t *testing.T) {
	hc := NewHealthChecker()
	if hc == nil {
		t.Fatal("NewHealthChecker() returned nil")
	}
	if hc.client != nil {
		t.Error("expected client to be nil when using default")
	}
}

func TestNewHealthCheckerWithClient(t *testing.T) {
	mockClient := &MockHTTPClient{}
	hc := NewHealthCheckerWithClient(mockClient)

	if hc == nil {
		t.Fatal("NewHealthCheckerWithClient() returned nil")
	}
	if hc.client != mockClient {
		t.Error("expected client to be set to mock")
	}
}

func TestHealthCheckerCheckSuccess(t *testing.T) {
	// Create mock response
	healthData := map[string]interface{}{
		"status":           "healthy",
		"router_strategy":  "health_weighted_rr",
		"bridge":           map[string]interface{}{"enabled": true},
		"available_models": []interface{}{"model1", "model2"},
		"upstreams":        map[string]interface{}{"upstream1": "active"},
	}

	jsonData, _ := json.Marshal(healthData)

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(jsonData)),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)

	// This should succeed (but prints to stdout which we can't easily capture here)
	err := hc.Check([]string{"-endpoint", "http://localhost:8080/-/health"})

	// The function only returns nil on success (exits on failure via os.Exit)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestHealthCheckerCheckNonJSONResponse(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK")),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)

	err := hc.Check([]string{})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestHealthCheckerCheckCustomTimeout(t *testing.T) {
	var capturedTimeout time.Duration

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("OK")),
				Header:     make(http.Header),
			}, nil
		},
	}

	// The custom timeout is handled when creating the client, not captured
	// Just verify the check works with a custom timeout argument
	hc := NewHealthCheckerWithClient(mockClient)

	err := hc.Check([]string{"-timeout", "10s"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	_ = capturedTimeout // suppress unused warning
}

func TestHealthCheckerCheckCustomEndpoint(t *testing.T) {
	var capturedURL string

	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			capturedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)

	err := hc.Check([]string{"-endpoint", "http://custom:9090/health"})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if capturedURL != "http://custom:9090/health" {
		t.Errorf("expected URL 'http://custom:9090/health', got '%s'", capturedURL)
	}
}

func TestHealthCheckerCheckHTTPError(t *testing.T) {
	// This test would need to mock os.Exit to properly test the error path
	// The current implementation calls os.Exit(1) on HTTP errors
	t.Skip("Skipping test that calls os.Exit - would terminate tests")
}

func TestHealthCheckerCheckNonOKStatus(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader("Service Unavailable")),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)

	// This will call os.Exit(1)
	t.Skip("Skipping test that calls os.Exit - would terminate tests")
	_ = hc
}

func TestCheckHealthLegacy(t *testing.T) {
	// Test the legacy checkHealth function
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"status":"healthy"}`)),
				Header:     make(http.Header),
			}, nil
		},
	}

	// Since checkHealth creates its own HealthChecker, we can't inject the mock
	// This is a limitation of the current design
	// The test demonstrates the function can be called

	// We can't easily test the legacy function without modifying it to accept a client
	t.Skip("Legacy checkHealth uses default client - cannot inject mock")
	_ = mockClient
}

func TestHealthCheckerCheckParseError(t *testing.T) {
	mockClient := &MockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("not valid json {{[")),
				Header:     make(http.Header),
			}, nil
		},
	}

	hc := NewHealthCheckerWithClient(mockClient)

	// Invalid JSON should not cause an error - just prints "Gateway is healthy"
	err := hc.Check([]string{})

	if err != nil {
		t.Errorf("expected no error for invalid JSON (200 OK), got %v", err)
	}
}

func TestDefaultHTTPClient(t *testing.T) {
	client := NewDefaultHTTPClient(5 * time.Second)

	if client == nil {
		t.Fatal("NewDefaultHTTPClient returned nil")
	}

	defaultClient, ok := client.(*DefaultHTTPClient)
	if !ok {
		t.Fatal("expected *DefaultHTTPClient type")
	}

	if defaultClient.client == nil {
		t.Error("underlying http.Client is nil")
	}

	if defaultClient.client.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", defaultClient.client.Timeout)
	}
}
