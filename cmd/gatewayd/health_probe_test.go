package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

func TestEnabledProviders(t *testing.T) {
	providers := []snapshot.ProviderSnapshot{
		{ProviderID: "enabled-1", ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true}},
		{ProviderID: "disabled-1", ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: false}},
		{ProviderID: "enabled-2", ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true}},
	}

	result := enabledProviders(providers)
	if len(result) != 2 {
		t.Fatalf("enabledProviders() returned %d providers, want 2", len(result))
	}
	if result[0].ProviderID != "enabled-1" || result[1].ProviderID != "enabled-2" {
		t.Fatalf("enabledProviders() returned wrong providers: %v", result)
	}
}

func TestEnabledProvidersEmpty(t *testing.T) {
	result := enabledProviders(nil)
	if len(result) != 0 {
		t.Fatalf("enabledProviders(nil) returned %d providers, want 0", len(result))
	}
}

func TestResolveHealthProbeInterval(t *testing.T) {
	cases := []struct {
		name string
		snap *snapshot.Snapshot
		want time.Duration
	}{
		{
			name: "nil snapshot",
			snap: nil,
			want: 0,
		},
		{
			name: "health disabled",
			snap: &snapshot.Snapshot{
				RoutingPolicy: snapshot.RoutingPolicy{Health: snapshot.HealthConfig{Enabled: false}},
			},
			want: 0,
		},
		{
			name: "interval set",
			snap: &snapshot.Snapshot{
				RoutingPolicy: snapshot.RoutingPolicy{Health: snapshot.HealthConfig{Enabled: true, IntervalSec: 30}},
			},
			want: 30 * time.Second,
		},
		{
			name: "interval zero uses default",
			snap: &snapshot.Snapshot{
				RoutingPolicy: snapshot.RoutingPolicy{Health: snapshot.HealthConfig{Enabled: true, IntervalSec: 0}},
			},
			want: 10 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveHealthProbeInterval(tc.snap)
			if got != tc.want {
				t.Fatalf("resolveHealthProbeInterval() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyProviderHeaders(t *testing.T) {
	cases := []struct {
		name     string
		provider snapshot.ProviderSnapshot
		wantKey  string
		wantVal  string
	}{
		{
			name:     "bearer credentials",
			provider: snapshot.ProviderSnapshot{Credentials: snapshot.Credentials{Kind: "bearer", Value: "test-bearer"}},
			wantKey:  "Authorization",
			wantVal:  "Bearer test-bearer",
		},
		{
			name:     "api_key credentials default header",
			provider: snapshot.ProviderSnapshot{Credentials: snapshot.Credentials{Kind: "api_key", Value: "test-api-key"}},
			wantKey:  "x-api-key",
			wantVal:  "test-api-key",
		},
		{
			name:     "api_key credentials custom header",
			provider: snapshot.ProviderSnapshot{Credentials: snapshot.Credentials{Kind: "api_key", Value: "test-api-key", HeaderName: "X-Custom-Key"}},
			wantKey:  "X-Custom-Key",
			wantVal:  "test-api-key",
		},
		{
			name:     "additional headers",
			provider: snapshot.ProviderSnapshot{Headers: map[string]string{"X-Custom": "value", "  ": "should-skip"}},
			wantKey:  "X-Custom",
			wantVal:  "value",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(http.Header)
			applyProviderHeaders(headers, tc.provider)
			if got := headers.Get(tc.wantKey); got != tc.wantVal {
				t.Fatalf("header %q = %q, want %q", tc.wantKey, got, tc.wantVal)
			}
		})
	}
}

func TestErrHealthStatus(t *testing.T) {
	err := errHealthStatus(503)
	if err.Error() != "health check failed with status 503" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "health check failed with status 503")
	}
	if err.StatusCode() != 503 {
		t.Fatalf("StatusCode() = %d, want 503", err.StatusCode())
	}
}

func TestStopHealthProbesNilDaemon(t *testing.T) {
	// Should not panic with nil Daemon
	var d *Daemon
	d.stopHealthProbes()
}

func TestDetachHealthProbeLoopNilDaemon(t *testing.T) {
	var d *Daemon
	cancel, done := d.detachHealthProbeLoop()
	if cancel != nil || done != nil {
		t.Fatal("detachHealthProbeLoop() on nil Daemon should return nil values")
	}
}

func TestRestartHealthProbesNilDaemon(t *testing.T) {
	// Should not panic with nil daemon
	var d *Daemon
	d.restartHealthProbes(nil)
}

func TestRestartHealthProbesNilRuntime(t *testing.T) {
	d := &Daemon{}
	d.restartHealthProbes(&snapshot.Snapshot{})
}

func TestRunHealthProbeOnceDisabled(t *testing.T) {
	d := &Daemon{}
	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{Health: snapshot.HealthConfig{Enabled: false}},
	}
	// Should return early without error
	d.runHealthProbeOnce(context.Background(), snap)
}

func TestProbeProviderHealthCustomPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/custom-health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	provider := snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		BaseURL:    server.URL,
	}
	healthCfg := snapshot.HealthConfig{
		Path:      "/custom-health",
		TimeoutMs: 1000,
	}

	statusCode, latency, err := d.probeProviderHealth(context.Background(), provider, healthCfg)
	if err != nil {
		t.Fatalf("probeProviderHealth() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
	if latency == 0 {
		t.Fatal("latency should be > 0")
	}
}

func TestProbeProviderHealthDefaultPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	provider := snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		BaseURL:    server.URL,
	}
	healthCfg := snapshot.HealthConfig{Path: ""} // Empty path should default to /v1/models

	statusCode, _, err := d.probeProviderHealth(context.Background(), provider, healthCfg)
	if err != nil {
		t.Fatalf("probeProviderHealth() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusOK)
	}
}

func TestProbeProviderHealthWithBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("expected Bearer auth, got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	provider := snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		BaseURL:    server.URL,
		Credentials: snapshot.Credentials{
			Kind:  "bearer",
			Value: "test-token",
		},
	}
	healthCfg := snapshot.HealthConfig{Path: "/health"}

	d.probeProviderHealth(context.Background(), provider, healthCfg)
}

func TestProbeProviderHealthErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	provider := snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		BaseURL:    server.URL,
	}
	healthCfg := snapshot.HealthConfig{Path: "/health"}

	statusCode, _, err := d.probeProviderHealth(context.Background(), provider, healthCfg)
	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("statusCode = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	if err == nil {
		t.Fatal("expected error for 503 status")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("error should contain status code: %v", err)
	}
}

func TestNewHealthHTTPClient(t *testing.T) {
	client := newHealthHTTPClient()
	if client == nil {
		t.Fatal("newHealthHTTPClient() returned nil")
	}
}

func TestRunHealthProbeLoopExitsOnDisabled(t *testing.T) {
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{Health: snapshot.HealthConfig{Enabled: false}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Should exit quickly since health is disabled
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runHealthProbeLoop(ctx, snap)
	}()

	select {
	case <-done:
		// Good, exited quickly
	case <-time.After(100 * time.Millisecond):
		t.Fatal("runHealthProbeLoop should exit immediately when health is disabled")
	}
}
