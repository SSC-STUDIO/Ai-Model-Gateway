package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-model-gateway/internal/cli"
)

func newJSONServer(t *testing.T, payload map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatalf("encode payload: %v", err)
		}
	}))
}

func TestHealthCommandTreatsConnectedReadyAsHealthy(t *testing.T) {
	server := newJSONServer(t, map[string]interface{}{
		"version":          "1.2.3",
		"gateway_status":   "connected",
		"telemetry_status": "connected",
		"gateway": map[string]interface{}{
			"readiness": "ready",
		},
	})
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "token")
	var output bytes.Buffer
	cmd := NewHealthCommand(client, &output)

	if err := cmd.Check(context.Background(), "text"); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}

	text := output.String()
	if !strings.Contains(text, "Status: HEALTHY") {
		t.Fatalf("expected HEALTHY output, got %q", text)
	}
	if !strings.Contains(text, "Readiness: ready") {
		t.Fatalf("expected readiness output, got %q", text)
	}
}

func TestHealthCommandRejectsConnectedButNotReady(t *testing.T) {
	server := newJSONServer(t, map[string]interface{}{
		"version":          "1.2.3",
		"gateway_status":   "connected",
		"telemetry_status": "connected",
		"gateway": map[string]interface{}{
			"readiness": "starting",
		},
	})
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "token")
	var output bytes.Buffer
	cmd := NewHealthCommand(client, &output)

	if err := cmd.Check(context.Background(), "text"); err == nil {
		t.Fatal("expected unhealthy error")
	}
}

func TestHealthCommandQuickCheckUsesGatewayListener(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/-/health" {
			t.Fatalf("unexpected quick health path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/status" {
			t.Fatalf("unexpected control path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"gateway_status": "connected",
			"gateway": map[string]interface{}{
				"listener": strings.TrimPrefix(gateway.URL, "http://"),
			},
		})
	}))
	defer control.Close()

	client := cli.NewControlPlaneClient(control.URL, "token")
	var output bytes.Buffer
	cmd := NewHealthCommand(client, &output)

	if err := cmd.QuickCheck(context.Background()); err != nil {
		t.Fatalf("QuickCheck returned error: %v", err)
	}
	if strings.TrimSpace(output.String()) != "OK" {
		t.Fatalf("unexpected quick check output: %q", output.String())
	}
}

func TestGatewayHealthURLUsesControlPlaneHostForWildcardOrLoopbackListeners(t *testing.T) {
	tests := []struct {
		name     string
		listener string
		want     string
	}{
		{
			name:     "wildcard host",
			listener: ":19090",
			want:     "http://example.com:19090/-/health",
		},
		{
			name:     "loopback host",
			listener: "127.0.0.1:19090",
			want:     "http://example.com:19090/-/health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := gatewayHealthURL(tt.listener, "https://example.com:18081")
			if err != nil {
				t.Fatalf("gatewayHealthURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("gatewayHealthURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
