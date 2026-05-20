package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewControlPlaneClient(t *testing.T) {
	client := NewControlPlaneClient("http://localhost:8080/", "test-token")
	if client.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL without trailing slash, got %q", client.baseURL)
	}
	if client.token != "test-token" {
		t.Errorf("expected token, got %q", client.token)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", client.httpClient.Timeout)
	}
}

func TestControlPlaneClient_SetHTTPClient(t *testing.T) {
	client := NewControlPlaneClient("http://localhost:8080", "token")
	customClient := &http.Client{Timeout: 10 * time.Second}
	client.SetHTTPClient(customClient)
	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestControlPlaneClient_GetConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/config" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ConfigResponse{
			Revision: &RevisionInfo{
				RevisionID: "rev-001",
				CreatedAt:  time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
				CreatedBy:  "admin",
				IsActive:   true,
			},
			Policy: PublisherPolicy{AutoPublish: false},
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	resp, err := client.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if resp.Revision == nil || resp.Revision.RevisionID != "rev-001" {
		t.Errorf("expected revision rev-001, got %v", resp.Revision)
	}
}

func TestControlPlaneClient_GetConfigHistory(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		expectedPath string
	}{
		{"default limit", 0, "/api/admin/config/history?limit=50"},
		{"custom limit", 10, "/api/admin/config/history?limit=10"},
		{"negative limit", -5, "/api/admin/config/history?limit=50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.String() != tt.expectedPath {
					t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
				}
				json.NewEncoder(w).Encode([]RevisionInfo{
					{RevisionID: "rev-001", IsActive: true},
				})
			}))
			defer server.Close()

			client := NewControlPlaneClient(server.URL, "token")
			history, err := client.GetConfigHistory(context.Background(), tt.limit)
			if err != nil {
				t.Fatalf("GetConfigHistory failed: %v", err)
			}
			if len(history) != 1 {
				t.Errorf("expected 1 revision, got %d", len(history))
			}
		})
	}
}

func TestControlPlaneClient_PublishConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/config/publish" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req struct {
			RevisionID string `json:"revision_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.RevisionID != "rev-001" {
			t.Errorf("expected revision rev-001, got %s", req.RevisionID)
		}
		json.NewEncoder(w).Encode(PublishResult{
			Success:    true,
			RevisionID: "rev-001",
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	result, err := client.PublishConfig(context.Background(), "rev-001")
	if err != nil {
		t.Fatalf("PublishConfig failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestControlPlaneClient_ReloadConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/config/reload" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		json.NewEncoder(w).Encode(PublishResult{
			Success:    true,
			RevisionID: "rev-reloaded",
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	result, err := client.ReloadConfig(context.Background())
	if err != nil {
		t.Fatalf("ReloadConfig failed: %v", err)
	}
	if !result.Success || result.RevisionID != "rev-reloaded" {
		t.Fatalf("ReloadConfig result = %#v, want success rev-reloaded", result)
	}
}

func TestControlPlaneClient_RollbackConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/config/rollback" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		json.NewEncoder(w).Encode(PublishResult{
			Success:    true,
			RevisionID: "rev-001",
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	result, err := client.RollbackConfig(context.Background(), "rev-001")
	if err != nil {
		t.Fatalf("RollbackConfig failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestControlPlaneClient_GetOverview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/overview" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(OverviewResponse{
			Windows: map[string]WindowData{
				"last_1h": {TotalRequests: 100},
			},
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	overview, err := client.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("GetOverview failed: %v", err)
	}
	if overview.Windows["last_1h"].TotalRequests != 100 {
		t.Errorf("expected 100 requests, got %d", overview.Windows["last_1h"].TotalRequests)
	}
}

func TestControlPlaneClient_GetTelemetry(t *testing.T) {
	tests := []struct {
		name         string
		query        *TelemetryQuery
		expectedPath string
	}{
		{
			name:         "nil query uses defaults",
			query:        nil,
			expectedPath: "/api/admin/telemetry?hours=24&limit=100",
		},
		{
			name:         "custom query",
			query:        &TelemetryQuery{WindowHours: 12, Limit: 50, Offset: 10},
			expectedPath: "/api/admin/telemetry?hours=12&limit=50&offset=10",
		},
		{
			name:         "negative values use defaults",
			query:        &TelemetryQuery{WindowHours: -1, Limit: -1},
			expectedPath: "/api/admin/telemetry?hours=24&limit=100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.String() != tt.expectedPath {
					t.Errorf("expected %s, got %s?%s", tt.expectedPath, r.URL.Path, r.URL.RawQuery)
				}
				json.NewEncoder(w).Encode(TelemetryResult{Total: 0})
			}))
			defer server.Close()

			client := NewControlPlaneClient(server.URL, "token")
			result, err := client.GetTelemetry(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("GetTelemetry failed: %v", err)
			}
			if result.Total != 0 {
				t.Errorf("expected total 0, got %d", result.Total)
			}
		})
	}
}

func TestControlPlaneClient_GetVerificationRunTelemetry(t *testing.T) {
	tests := []struct {
		name         string
		runID        string
		query        *VerificationRunTelemetryQuery
		expectedPath string
	}{
		{
			name:         "nil query uses defaults",
			runID:        "run-1",
			query:        nil,
			expectedPath: "/api/admin/benchmark/runs/run-1/telemetry?hours=24&limit=200",
		},
		{
			name:  "custom query",
			runID: "run-2",
			query: &VerificationRunTelemetryQuery{
				WindowHours: 12,
				Limit:       50,
				Offset:      10,
				Providers:   []string{"provider-a"},
				Models:      []string{"gpt-4o"},
				TargetID:    "target-2",
				CaseID:      "reasoning_exact",
			},
			expectedPath: "/api/admin/benchmark/runs/run-2/telemetry?case_id=reasoning_exact&hours=12&limit=50&models=gpt-4o&offset=10&providers=provider-a&target_id=target-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.String(); got != tt.expectedPath {
					t.Fatalf("expected %s, got %s", tt.expectedPath, got)
				}
				json.NewEncoder(w).Encode(TelemetryResult{
					Total: 1,
					Events: []EventRecord{
						{RequestID: "req-1", BenchmarkCaseID: "reasoning_exact"},
					},
				})
			}))
			defer server.Close()

			client := NewControlPlaneClient(server.URL, "token")
			result, err := client.GetVerificationRunTelemetry(context.Background(), tt.runID, tt.query)
			if err != nil {
				t.Fatalf("GetVerificationRunTelemetry failed: %v", err)
			}
			if result.Total != 1 || len(result.Events) != 1 {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}

	client := NewControlPlaneClient("http://example.com", "token")
	if _, err := client.GetVerificationRunTelemetry(context.Background(), "  ", nil); err == nil || !strings.Contains(err.Error(), "run id is required") {
		t.Fatalf("expected missing run id error, got %v", err)
	}
}

func TestControlPlaneClient_GetTimeSeries(t *testing.T) {
	tests := []struct {
		name  string
		query *TimeSeriesQuery
	}{
		{"nil query uses defaults", nil},
		{"with group_by", &TimeSeriesQuery{WindowHours: 12, BucketMinutes: 10, GroupBy: "model"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(TimeSeriesResult{})
			}))
			defer server.Close()

			client := NewControlPlaneClient(server.URL, "token")
			result, err := client.GetTimeSeries(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("GetTimeSeries failed: %v", err)
			}
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestControlPlaneClient_GetBenchmark(t *testing.T) {
	tests := []struct {
		name  string
		query *BenchmarkQuery
	}{
		{"nil query", nil},
		{"with models", &BenchmarkQuery{WindowHours: 24, Models: []string{"gpt-4", "claude"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(BenchmarkResult{})
			}))
			defer server.Close()

			client := NewControlPlaneClient(server.URL, "token")
			result, err := client.GetBenchmark(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("GetBenchmark failed: %v", err)
			}
			if result == nil {
				t.Error("expected non-nil result")
			}
		})
	}
}

func TestControlPlaneClient_GetStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/status" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(SystemStatus{
			Version: "1.1.1",
			Uptime:  "1h",
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	status, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Version != "1.1.1" {
		t.Errorf("expected version 1.1.1, got %s", status.Version)
	}
}

func TestControlPlaneClient_ListProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(SystemStatus{
			Version: "1.1.1",
			Gateway: &GatewayStatusResponse{
				ProviderHealth: map[string]ProviderHealth{
					"openai":    {Name: "openai", Healthy: true},
					"anthropic": {Name: "anthropic", Healthy: false},
				},
			},
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	providers, err := client.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders failed: %v", err)
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
	for _, p := range providers {
		if p.Name == "openai" && p.Status != "healthy" {
			t.Errorf("expected openai to be healthy, got %s", p.Status)
		}
		if p.Name == "anthropic" && p.Status != "unhealthy" {
			t.Errorf("expected anthropic to be unhealthy, got %s", p.Status)
		}
	}
}

func TestControlPlaneClient_ListProviders_NoGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(SystemStatus{Version: "1.1.1"})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	providers, err := client.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders failed: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers without gateway, got %d", len(providers))
	}
}

func TestControlPlaneClient_TestProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(SystemStatus{
			Version: "1.1.1",
			Gateway: &GatewayStatusResponse{
				ProviderHealth: map[string]ProviderHealth{
					"openai": {Name: "openai", Healthy: true, LatencyMs: 50},
				},
			},
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")

	status, err := client.TestProvider(context.Background(), "openai")
	if err != nil {
		t.Fatalf("TestProvider failed: %v", err)
	}
	if !status.Healthy || status.Status != "healthy" {
		t.Errorf("expected healthy status, got healthy=%v status=%s", status.Healthy, status.Status)
	}

	status, err = client.TestProvider(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("TestProvider failed: %v", err)
	}
	if status.Status != "unknown" {
		t.Errorf("expected unknown status, got %s", status.Status)
	}
}

func TestControlPlaneClient_doRequest_AuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Authorization header 'Bearer test-token', got %q", auth)
		}
		accept := r.Header.Get("Accept")
		if accept != "application/json" {
			t.Errorf("expected Accept header 'application/json', got %q", accept)
		}
		json.NewEncoder(w).Encode(SystemStatus{})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "test-token")
	_, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestControlPlaneClient_GetStatusNumericReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"version": "test",
			"gateway_status": "connected",
			"telemetry_status": "connected",
			"gateway": {
				"active_snapshot_id": "snap_1",
				"readiness": 2,
				"active_requests": 0,
				"listener": "127.0.0.1:18080",
				"provider_health": {}
			}
		}`))
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	status, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Gateway == nil {
		t.Fatal("expected gateway status")
	}
	if status.Gateway.Readiness != "ready" {
		t.Fatalf("readiness = %q, want ready", status.Gateway.Readiness)
	}
}

func TestControlPlaneClient_doRequest_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	_, err := client.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestControlPlaneClient_doRequest_ErrorResponseWithoutJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad gateway"))
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	_, err := client.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error on 502 response")
	}
}

func TestControlPlaneClient_doRequest_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	_, err := client.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestControlPlaneClient_doRequest_ConnectionError(t *testing.T) {
	client := NewControlPlaneClient("http://localhost:1", "token")
	_, err := client.GetStatus(context.Background())
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestControlPlaneClient_doRequest_WithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		json.NewEncoder(w).Encode(SystemStatus{})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.GetStatus(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestControlPlaneClient_doRequest_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Empty body - should not error when result is nil
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	// Call doRequest directly with nil result
	ctx := context.Background()
	var client2 *ControlPlaneClient = client
	err := client2.doRequest(ctx, http.MethodGet, "/api/admin/status", nil, nil)
	if err != nil {
		t.Fatalf("expected no error for empty response with nil result, got: %v", err)
	}
}

func TestControlPlaneClient_doRequest_NoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" {
			t.Errorf("expected no Authorization header when token is empty, got %q", auth)
		}
		json.NewEncoder(w).Encode(SystemStatus{})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "")
	_, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestControlPlaneClient_doRequest_PostWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", r.Header.Get("Content-Type"))
		}
		json.NewEncoder(w).Encode(PublishResult{})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	_, err := client.PublishConfig(context.Background(), "rev-001")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestControlPlaneClient_TestProvider_NoGatewayInStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(SystemStatus{
			Version: "1.1.1",
			// No Gateway field
		})
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, "token")
	status, err := client.TestProvider(context.Background(), "openai")
	if err != nil {
		t.Fatalf("TestProvider failed: %v", err)
	}
	if status.Status != "unknown" {
		t.Errorf("expected unknown status when gateway is nil, got %s", status.Status)
	}
}
