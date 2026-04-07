package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

func TestRun_HotReloadUpdatesLiveRoutingState(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	listenAddr := reserveListenAddr(t)

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:    listenAddr,
		TelemetryPath: filepath.Join(dir, "telemetry.db"),
		PricingPath:   filepath.Join(dir, "pricing.json"),
		HealthEnabled: false,
		ProvidersYAML: `
  - name: provider-a
    base_url: "https://provider-a.example.com"
    provider_class: quota_limited
    models: ["alpha-model"]
    weight: 1
    timeout_ms: 1000
    enabled: true
`,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, configPath)
	}()

	waitForGatewayReady(t, listenAddr)

	modelsBody := doGatewayRequest(t, listenAddr, http.MethodGet, "/v1/models", nil)
	if !strings.Contains(modelsBody, "alpha-model") {
		t.Fatalf("expected initial models to include alpha-model, got %s", modelsBody)
	}

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:    listenAddr,
		TelemetryPath: filepath.Join(dir, "telemetry.db"),
		PricingPath:   filepath.Join(dir, "pricing.json"),
		HealthEnabled: false,
		ProvidersYAML: `
  - name: provider-b
    base_url: "https://provider-b.example.com"
    provider_class: free
    models: ["beta-model"]
    weight: 1
    timeout_ms: 1000
    enabled: true
`,
	})

	waitForCondition(t, 8*time.Second, func() bool {
		body := doGatewayRequest(t, listenAddr, http.MethodGet, "/v1/models", nil)
		return strings.Contains(body, "beta-model") && !strings.Contains(body, "alpha-model")
	}, "expected /v1/models to reflect reloaded providers")

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run() to stop")
	}
}

func TestRun_HotReloadUpdatesPipelineRetryState(t *testing.T) {
	previousTransportFactory := newRunTransport
	newRunTransport = func() core.UpstreamTransport {
		return newUpstreamTransport(nil)
	}
	t.Cleanup(func() {
		newRunTransport = previousTransportFactory
	})

	var (
		mu       sync.Mutex
		attempts = make(map[string]int)
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		key := string(body)
		mu.Lock()
		attempts[key]++
		attempt := attempts[key]
		mu.Unlock()
		if attempt < 3 {
			writeJSON(t, w, http.StatusInternalServerError, `{"error":"temporary failure"}`)
			return
		}
		writeJSON(t, w, http.StatusOK, `{"id":"chatcmpl-retried","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	listenAddr := reserveListenAddr(t)

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:    listenAddr,
		TelemetryPath: filepath.Join(dir, "telemetry.db"),
		PricingPath:   filepath.Join(dir, "pricing.json"),
		HealthEnabled: false,
		MaxRetries:    1,
		FailureThreshold: 100000,
		ProvidersYAML: fmt.Sprintf(`
  - name: retry-provider
    base_url: %q
    provider_class: quota_limited
    models: ["gpt-4o"]
    weight: 1
    same_retries: 10
    timeout_ms: 1000
    enabled: true
`, upstream.URL),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, configPath)
	}()

	waitForGatewayReady(t, listenAddr)

	status, _ := doGatewayRequestWithStatus(t, listenAddr, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"phase-1"}]}`))
	if status != http.StatusInternalServerError {
		t.Fatalf("expected first request with max_retries=1 to fail before a third attempt, got %d", status)
	}

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:    listenAddr,
		TelemetryPath: filepath.Join(dir, "telemetry.db"),
		PricingPath:   filepath.Join(dir, "pricing.json"),
		HealthEnabled: false,
		MaxRetries:    2,
		FailureThreshold: 100000,
		ProvidersYAML: fmt.Sprintf(`
  - name: retry-provider
    base_url: %q
    provider_class: quota_limited
    models: ["gpt-4o"]
    weight: 1
    same_retries: 10
    timeout_ms: 1000
    enabled: true
`, upstream.URL),
	})

	var (
		lastStatus   int
		lastAttempts int
	)
	deadline := time.Now().Add(8 * time.Second)
	succeeded := false
	for time.Now().Before(deadline) {
		nonce := time.Now().UnixNano()
		payload := fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"phase-%d"}]}`, nonce)
		status, body := doGatewayRequestWithStatus(t, listenAddr, http.MethodPost, "/v1/chat/completions", []byte(payload))
		mu.Lock()
		lastAttempts = attempts[payload]
		mu.Unlock()
		lastStatus = status
		if status == http.StatusOK && strings.Contains(body, `"chat.completion"`) {
			succeeded = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !succeeded {
		t.Fatalf("expected max_retries hot reload to increase live request retries (last status=%d, last upstream attempts=%d)", lastStatus, lastAttempts)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run() to stop")
	}
}

func TestRun_SameRetriesReuseProviderBeforeSwitching(t *testing.T) {
	previousTransportFactory := newRunTransport
	newRunTransport = func() core.UpstreamTransport {
		return newUpstreamTransport(nil)
	}
	t.Cleanup(func() {
		newRunTransport = previousTransportFactory
	})

	var providerAHits atomic.Int64
	providerA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerAHits.Add(1)
		writeJSON(t, w, http.StatusInternalServerError, `{"error":"provider-a failure"}`)
	}))
	defer providerA.Close()

	var providerBHits atomic.Int64
	providerB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerBHits.Add(1)
		writeJSON(t, w, http.StatusOK, `{"id":"provider-b","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer providerB.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	listenAddr := reserveListenAddr(t)

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:       listenAddr,
		TelemetryPath:    filepath.Join(dir, "telemetry.db"),
		PricingPath:      filepath.Join(dir, "pricing.json"),
		HealthEnabled:    false,
		MaxRetries:       2,
		FailureThreshold: 100000,
		ProvidersYAML: fmt.Sprintf(`
  - name: provider-a
    base_url: %q
    provider_class: quota_limited
    models: ["gpt-4o"]
    weight: 1
    same_retries: 1
    timeout_ms: 1000
    enabled: true
  - name: provider-b
    base_url: %q
    provider_class: quota_limited
    models: ["gpt-4o"]
    weight: 1
    timeout_ms: 1000
    enabled: true
`, providerA.URL, providerB.URL),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, configPath)
	}()

	waitForGatewayReady(t, listenAddr)

	status, body := doGatewayRequestWithStatus(t, listenAddr, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"same-retries"}]}`))
	if status != http.StatusOK {
		t.Fatalf("expected successful failover after same_retries budget, got %d: %s", status, body)
	}
	if providerAHits.Load() != 2 {
		t.Fatalf("expected provider-a to be retried twice before failover, got %d hits", providerAHits.Load())
	}
	if providerBHits.Load() != 1 {
		t.Fatalf("expected provider-b to receive exactly one failover attempt, got %d hits", providerBHits.Load())
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run() to stop")
	}
}

func TestRun_StartupHealthProbeBlocksUnhealthyProviderBeforeFirstRequest(t *testing.T) {
	var healthHits atomic.Int64
	var chatHits atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			healthHits.Add(1)
			writeJSON(t, w, http.StatusInternalServerError, `{"error":"probe failed"}`)
		case "/v1/chat/completions":
			chatHits.Add(1)
			writeJSON(t, w, http.StatusOK, `{"id":"unexpected-success"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	listenAddr := reserveListenAddr(t)

	writeV2Config(t, configPath, gatewayTestConfig{
		ListenAddr:        listenAddr,
		TelemetryPath:     filepath.Join(dir, "telemetry.db"),
		PricingPath:       filepath.Join(dir, "pricing.json"),
		HealthEnabled:     true,
		HealthIntervalSec: 60,
		FailureThreshold:  1,
		CooldownSec:       600,
		ProvidersYAML: fmt.Sprintf(`
  - name: unhealthy-provider
    base_url: %q
    provider_class: quota_limited
    models: ["gpt-4o"]
    weight: 1
    timeout_ms: 1000
    enabled: true
`, provider.URL),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, configPath)
	}()

	waitForGatewayReady(t, listenAddr)
	waitForCondition(t, 2*time.Second, func() bool {
		return healthHits.Load() > 0
	}, "expected an immediate startup health probe")

	status, body := doGatewayRequestWithStatus(t, listenAddr, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("expected first request to be blocked after failed startup probe, got %d: %s", status, body)
	}
	if chatHits.Load() != 0 {
		t.Fatalf("expected unhealthy provider not to receive chat requests, got %d", chatHits.Load())
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run() to stop")
	}
}

type gatewayTestConfig struct {
	ListenAddr        string
	TelemetryPath     string
	PricingPath       string
	HealthEnabled     bool
	HealthIntervalSec int
	MaxRetries        int
	FailureThreshold  int
	CooldownSec       int
	ProvidersYAML     string
}

func writeV2Config(t *testing.T, path string, cfg gatewayTestConfig) {
	t.Helper()

	healthInterval := cfg.HealthIntervalSec
	if healthInterval == 0 {
		healthInterval = 60
	}
	failureThreshold := cfg.FailureThreshold
	if failureThreshold == 0 {
		failureThreshold = 1
	}
	maxRetries := cfg.MaxRetries
	cooldownSec := cfg.CooldownSec
	if cooldownSec == 0 {
		cooldownSec = 600
	}

	content := fmt.Sprintf(`server:
  listen: %q
  read_timeout_ms: 5000
  write_timeout_ms: 5000
  idle_timeout_ms: 5000
  max_body_bytes: 1048576
admin:
  enabled: false
  language: en
routing:
  strategy: health_weighted_rr
  max_retries: %d
  retry_backoff:
    initial_ms: 1
    max_ms: 1
  health:
    enabled: %t
    interval_sec: %d
    timeout_ms: 500
    path: /v1/models
  sticky_sessions:
    enabled: false
    ttl_sec: 60
  failure_policy:
    threshold: %d
    cooldown_sec: %d
    passthrough_after_sec: 600
    quota_recovery_interval_min: 60
  retry:
    infinite_on_error: false
    status_codes: [429, 500, 502, 503, 504]
    message_keywords: ["quota"]
  intercepts: []
providers:%s
telemetry:
  sqlite_path: %q
  retention_days: 1
  aggregation_sec: 60
  cache_ttl_sec: 1
pricing:
  cache_path: %q
  refresh_interval_hours: 24
  request_timeout_ms: 1000
compat:
  bridge:
    enabled: false
    rules: []
    exclude_user_agents: []
  fallback:
    enabled: false
    detect_repetition: false
    models: {}
`, cfg.ListenAddr, maxRetries, cfg.HealthEnabled, healthInterval, failureThreshold, cooldownSec, cfg.ProvidersYAML, cfg.TelemetryPath, cfg.PricingPath)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func reserveListenAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve listen addr: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close reserved listener: %v", err)
	}
	return addr
}

func waitForGatewayReady(t *testing.T, listenAddr string) {
	t.Helper()

	waitForCondition(t, 3*time.Second, func() bool {
		resp, err := http.Get("http://" + listenAddr + "/-/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, "expected gateway health endpoint to come up")
}

func waitForCondition(t *testing.T, timeout time.Duration, check func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(message)
}

func doGatewayRequest(t *testing.T, listenAddr string, method string, path string, body []byte) string {
	t.Helper()

	_, respBody := doGatewayRequestWithStatus(t, listenAddr, method, path, body)
	return respBody
}

func doGatewayRequestWithStatus(t *testing.T, listenAddr string, method string, path string, body []byte) (int, string) {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, "http://"+listenAddr+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, string(data)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
