package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/gateway/telemetry"
	"runtime"
)

type modelResponse struct {
	Object  string          `json:"object"`
	Data    []modelResponse `json:"data,omitempty"`
	ID      string          `json:"id,omitempty"`
	Created int64           `json:"created,omitempty"`
	OwnedBy string          `json:"owned_by,omitempty"`
}

func TestModelsHandlerReturnsOpenAIStyleModelObjects(t *testing.T) {
	generatedAt := time.Unix(1712345678, 0).UTC()
	d := &Daemon{
		startedAt: time.Unix(1712000000, 0).UTC(),
		snapshot: &snapshot.Snapshot{
			Meta: snapshot.SnapshotMeta{
				SnapshotID:      "snap-123",
				SchemaVersion:   snapshot.CurrentSchemaVersion,
				RevisionID:      "rev-123",
				GeneratedAt:     generatedAt,
				CompilerVersion: "test",
			},
			Providers: []snapshot.ProviderSnapshot{
				{
					ProviderID: "primary",
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-4o-mini", UpstreamModel: "provider-a-4o-mini"},
						{PublicModel: "gpt-5.4", UpstreamModel: "provider-a-5.4"},
					},
				},
				{
					ProviderID: "secondary",
					ModelTable: []snapshot.ModelMapping{
						{PublicModel: "gpt-5.4", UpstreamModel: "provider-b-5.4"},
					},
				},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	d.modelsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("modelsHandler() status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("modelsHandler() content-type = %q, want application/json", contentType)
	}

	var got modelResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Object != "list" {
		t.Fatalf("modelsHandler() object = %q, want %q", got.Object, "list")
	}

	want := []modelResponse{
		{
			ID:      "gpt-4o-mini",
			Object:  "model",
			Created: generatedAt.Unix(),
			OwnedBy: modelOwner,
		},
		{
			ID:      "gpt-5.4",
			Object:  "model",
			Created: generatedAt.Unix(),
			OwnedBy: modelOwner,
		},
	}
	if !reflect.DeepEqual(got.Data, want) {
		t.Fatalf("modelsHandler() data = %#v, want %#v", got.Data, want)
	}
}

func TestCreateHandlerProxiesLegacyAdminPaths(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Path", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	d := &Daemon{config: Config{AdminProxyURL: backend.URL}}
	handler := d.createHandler()

	for _, path := range []string{"/admin", "/admin/ops", "/api/admin/runtime/status", "/manifest.json", "/icon.svg", "/favicon.svg"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("X-Backend-Path"); got != path {
			t.Fatalf("%s proxied path = %q, want %q", path, got, path)
		}
	}
}

func TestSnapshotStateDrivesHealthAndStatusReadiness(t *testing.T) {
	startedAt := time.Unix(1712000000, 0).UTC()
	withSnapshot := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-456",
			SchemaVersion: snapshot.CurrentSchemaVersion,
			RevisionID:    "rev-456",
		},
	}

	cases := []struct {
		name           string
		snapshot       *snapshot.Snapshot
		wantHealth     string
		wantReadiness  gatewaycontrol.ReadinessState
		wantSnapshotID string
	}{
		{
			name:          "no snapshot loaded",
			wantHealth:    "starting",
			wantReadiness: gatewaycontrol.ReadinessStarting,
		},
		{
			name:           "snapshot loaded",
			snapshot:       withSnapshot,
			wantHealth:     "healthy",
			wantReadiness:  gatewaycontrol.ReadinessReady,
			wantSnapshotID: "snap-456",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{
				config:    Config{Listen: "127.0.0.1:18080"},
				startedAt: startedAt,
				snapshot:  tc.snapshot,
			}

			req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
			rec := httptest.NewRecorder()
			d.healthHandler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("healthHandler() status = %d, want %d", rec.Code, http.StatusOK)
			}

			var health struct {
				Status string `json:"status"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
				t.Fatalf("decode health response: %v", err)
			}
			if health.Status != tc.wantHealth {
				t.Fatalf("healthHandler() status = %q, want %q", health.Status, tc.wantHealth)
			}

			status := d.GetStatus()
			if status.Readiness != tc.wantReadiness {
				t.Fatalf("GetStatus() readiness = %v, want %v", status.Readiness, tc.wantReadiness)
			}
			if status.ActiveSnapshotID != tc.wantSnapshotID {
				t.Fatalf("GetStatus() active snapshot = %q, want %q", status.ActiveSnapshotID, tc.wantSnapshotID)
			}
		})
	}
}

func TestRunHealthProbeOnceUpdatesProviderHealth(t *testing.T) {
	var unhealthy atomic.Bool
	unhealthy.Store(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if unhealthy.Load() {
			http.Error(w, "upstream down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}
	snap := healthProbeSnapshot(upstream.URL)
	d.snapshot = snap
	d.runtime.ApplySnapshot(snap)

	d.runHealthProbeOnce(t.Context(), snap)

	status := d.GetStatus()
	health := status.ProviderHealth["health-provider"]
	if health.Healthy {
		t.Fatalf("provider should be unhealthy after failed probe: %#v", health)
	}
	if health.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1", health.ConsecutiveFailures)
	}
	if health.LastCheck.IsZero() {
		t.Fatalf("LastCheck should be set after failed probe")
	}
	if health.CooldownUntil.IsZero() {
		t.Fatalf("CooldownUntil should be set after failed probe")
	}

	unhealthy.Store(false)
	d.runHealthProbeOnce(t.Context(), snap)

	status = d.GetStatus()
	health = status.ProviderHealth["health-provider"]
	if !health.Healthy {
		t.Fatalf("provider should recover after successful probe: %#v", health)
	}
	if health.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0 after recovery", health.ConsecutiveFailures)
	}
	if health.LastSuccess.IsZero() {
		t.Fatalf("LastSuccess should be set after successful probe")
	}
}

func TestApplySnapshotStartsHealthProbeLoop(t *testing.T) {
	requests := make(chan struct{}, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/probe" {
			http.NotFound(w, r)
			return
		}
		select {
		case requests <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	d, err := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatalf("NewDaemon() error = %v", err)
	}
	d.runCtx, d.runCancel = context.WithCancel(context.Background())
	defer func() {
		if d.runCancel != nil {
			d.runCancel()
		}
		d.stopHealthProbes()
	}()

	snap := healthProbeSnapshot(upstream.URL)
	snap.RoutingPolicy.Health.IntervalSec = 1
	snap.RoutingPolicy.Health.Path = "/probe"

	if err := d.ApplySnapshot(snap); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}

	select {
	case <-requests:
	case <-time.After(2 * time.Second):
		t.Fatal("expected health probe loop to hit upstream")
	}
}

func healthProbeSnapshot(baseURL string) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:      "snap-health",
			SchemaVersion:   snapshot.CurrentSchemaVersion,
			RevisionID:      "rev-health",
			GeneratedAt:     time.Now().UTC(),
			CompilerVersion: "test",
		},
		Ingress: snapshot.IngressConfig{
			Listen: "127.0.0.1:18080",
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "health-provider",
				BaseURL:    baseURL,
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-health", UpstreamModel: "gpt-health"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
		RoutingPolicy: snapshot.RoutingPolicy{
			Health: snapshot.HealthConfig{
				Enabled:     true,
				IntervalSec: 60,
				TimeoutMs:   1000,
				Path:        "/healthz",
			},
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:                1,
				CooldownSec:              30,
				PassthroughAfterSec:      5,
				QuotaRecoveryIntervalMin: 1,
			},
		},
	}
}

func TestHealthHandlerWithSnapshot(t *testing.T) {
	generatedAt := time.Unix(1712345678, 0).UTC()
	d := &Daemon{
		startedAt: time.Unix(1712000000, 0).UTC(),
		snapshot: &snapshot.Snapshot{
			Meta: snapshot.SnapshotMeta{
				SnapshotID:      "snap-123",
				SchemaVersion:   snapshot.CurrentSchemaVersion,
				RevisionID:      "rev-123",
				GeneratedAt:     generatedAt,
				CompilerVersion: "test",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	rec := httptest.NewRecorder()

	d.healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthHandler() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Fatalf("status = %v, want healthy", resp["status"])
	}
	if resp["snapshot_id"] != "snap-123" {
		t.Fatalf("snapshot_id = %v, want snap-123", resp["snapshot_id"])
	}
	if resp["revision_id"] != "rev-123" {
		t.Fatalf("revision_id = %v, want rev-123", resp["revision_id"])
	}
}

func TestApplySnapshotValid(t *testing.T) {
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})

	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-test",
			SchemaVersion: snapshot.CurrentSchemaVersion,
			RevisionID:    "rev-test",
			GeneratedAt:   time.Now().UTC(),
		},
		Ingress: snapshot.IngressConfig{
			Listen: "127.0.0.1:18080",
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "test-provider",
				BaseURL:    "https://api.example.com",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4"},
				},
			},
		},
	}

	if err := d.ApplySnapshot(snap); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}

	if d.snapshot == nil {
		t.Fatal("ApplySnapshot() did not set snapshot")
	}
	if d.snapshot.Meta.SnapshotID != "snap-test" {
		t.Fatalf("SnapshotID = %q, want %q", d.snapshot.Meta.SnapshotID, "snap-test")
	}
}

func TestShutdownWithHTTPServer(t *testing.T) {
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	d.httpServer = &http.Server{Addr: "127.0.0.1:0"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestShutdownWithTelemetryClient(t *testing.T) {
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	d.telClient = telemetry.NewClient(nil, telemetry.DefaultClientConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestGetStatusWithRuntime(t *testing.T) {
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	d.snapshot = &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:    "snap-test",
			SchemaVersion: snapshot.CurrentSchemaVersion,
		},
	}

	status := d.GetStatus()
	if status.Readiness != gatewaycontrol.ReadinessReady {
		t.Fatalf("Readiness = %v, want %v", status.Readiness, gatewaycontrol.ReadinessReady)
	}
	if status.ActiveSnapshotID != "snap-test" {
		t.Fatalf("ActiveSnapshotID = %q, want %q", status.ActiveSnapshotID, "snap-test")
	}
}

func TestDefaultSocketPathWindows(t *testing.T) {
	// Test Windows path (name only, no socket extension)
	if runtime.GOOS == "windows" {
		path := defaultSocketPath("test")
		if path != "test" {
			t.Fatalf("defaultSocketPath() on Windows = %q, want %q", path, "test")
		}
	}
}

func TestHealthHandlerWithDetailTrue(t *testing.T) {
	generatedAt := time.Unix(1712345678, 0).UTC()
	d, _ := NewDaemon(Config{Listen: "127.0.0.1:18080"})
	d.startedAt = time.Unix(1712000000, 0).UTC()
	d.snapshot = &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:      "snap-123",
			SchemaVersion:   snapshot.CurrentSchemaVersion,
			RevisionID:      "rev-123",
			GeneratedAt:     generatedAt,
			CompilerVersion: "test",
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "test-provider",
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled: true,
				},
			},
		},
	}
	d.runtime.ApplySnapshot(d.snapshot)

	req := httptest.NewRequest(http.MethodGet, "/-/health?detail=true", nil)
	rec := httptest.NewRecorder()

	d.healthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthHandler() status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Fatalf("status = %v, want healthy", resp["status"])
	}

	providers, ok := resp["providers"].([]interface{})
	if !ok {
		t.Fatalf("providers not found or not an array")
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}

	provider := providers[0].(map[string]interface{})
	if provider["provider_id"] != "test-provider" {
		t.Fatalf("provider_id = %v, want test-provider", provider["provider_id"])
	}
	if provider["enabled"] != true {
		t.Fatalf("enabled = %v, want true", provider["enabled"])
	}
	// healthy should exist when runtime has health data
	if _, ok := provider["healthy"]; !ok {
		t.Fatalf("healthy field missing in provider response")
	}
}
