package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-model-gateway/internal/cli"
)

func TestReloadCommandReloadsConfigSource(t *testing.T) {
	var reloadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/admin/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"gateway_status": "connected",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/admin/config/reload":
			reloadCalls++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":     true,
				"revision_id": "rev-reloaded",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "token")
	var output bytes.Buffer
	cmd := NewReloadCommand(client, &output)

	if err := cmd.Reload(context.Background(), "text"); err != nil {
		t.Fatalf("Reload returned error: %v", err)
	}
	if reloadCalls != 1 {
		t.Fatalf("reloadCalls = %d, want 1", reloadCalls)
	}
}

func TestReloadCommandRequiresConnectedGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"gateway_status": "disconnected",
		})
	}))
	defer server.Close()

	client := cli.NewControlPlaneClient(server.URL, "token")
	var output bytes.Buffer
	cmd := NewReloadCommand(client, &output)

	if err := cmd.Reload(context.Background(), "text"); err == nil {
		t.Fatal("expected reload failure when gateway is disconnected")
	}
}
