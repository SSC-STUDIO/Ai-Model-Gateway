package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/audit"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/updater"
	"ai-model-gateway/internal/version"
)

// --- stub for ConfigQuery (secretsStatusHandler needs GetCurrentConfigView) ---

type stubConfigQueryForSecrets struct {
	view *publish.CurrentConfigView
	err  error
}

func (s *stubConfigQueryForSecrets) GetCurrentConfigView() (*publish.CurrentConfigView, error) {
	return s.view, s.err
}

func (s *stubConfigQueryForSecrets) GetHistory(limit int) ([]publish.RevisionInfo, error) {
	return nil, nil
}

// --- non-pinger telemetry stub (does not implement telemetryPinger) ---

type stubNonPingerTelemetryQuerier struct{}

func (s *stubNonPingerTelemetryQuerier) GetOverview(_ telemetryquery.OverviewRequest) (*telemetryquery.OverviewResponse, error) {
	return &telemetryquery.OverviewResponse{}, nil
}
func (s *stubNonPingerTelemetryQuerier) GetTelemetry(_ telemetryquery.TelemetryRequest) (*telemetryquery.TelemetryResponse, error) {
	return &telemetryquery.TelemetryResponse{}, nil
}
func (s *stubNonPingerTelemetryQuerier) GetTimeSeries(_ telemetryquery.TimeSeriesRequest) (*telemetryquery.TimeSeriesResponse, error) {
	return &telemetryquery.TimeSeriesResponse{}, nil
}
func (s *stubNonPingerTelemetryQuerier) GetModelBenchmark(_ telemetryquery.BenchmarkRequest) (*telemetryquery.BenchmarkResponse, error) {
	return &telemetryquery.BenchmarkResponse{}, nil
}

func TestDiffConfigsRedactsSecretsInsideArrays(t *testing.T) {
	before := map[string]any{
		"providers": []any{
			map[string]any{
				"name":     "openai",
				"base_url": "https://old.example.test",
				"api_key":  "old-secret",
			},
		},
	}
	after := map[string]any{
		"providers": []any{
			map[string]any{
				"name":     "openai",
				"base_url": "https://new.example.test",
				"api_key":  "new-secret",
			},
		},
	}

	changes := DiffConfigs(before, after)
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, leaked := range []string{"old-secret", "new-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("diff leaked secret %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, "[redacted]") {
		t.Fatalf("diff did not include redaction marker: %s", body)
	}
	if !strings.Contains(body, "https://new.example.test") {
		t.Fatalf("diff redacted non-secret field: %s", body)
	}
}

func TestDiffConfigsRedactsHeaderCredentials(t *testing.T) {
	before := map[string]any{
		"providers": []any{
			map[string]any{
				"name": "custom",
				"headers": map[string]any{
					"Authorization": "Bearer old-token",
					"X-API-Key":     "old-key",
					"X-Custom":      "old-value",
				},
			},
		},
	}
	after := map[string]any{
		"providers": []any{
			map[string]any{
				"name": "custom",
				"headers": map[string]any{
					"Authorization": "Bearer new-token",
					"X-API-Key":     "new-key",
					"X-Custom":      "new-value",
				},
			},
		},
	}

	changes := DiffConfigs(before, after)
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)

	// Credential headers should be redacted.
	for _, leaked := range []string{"Bearer old-token", "Bearer new-token", "old-key", "new-key"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("diff leaked header credential %q: %s", leaked, body)
		}
	}
	// Non-credential header should NOT be redacted.
	if !strings.Contains(body, "new-value") {
		t.Fatalf("diff redacted non-credential header: %s", body)
	}
}

func TestIsSecretPathRecognizesCredentialHeaders(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"providers[0].headers.authorization", true},
		{"providers[0].headers.Authorization", true},
		{"providers[0].headers.cookie", true},
		{"providers[0].headers.x-api-key", true},
		{"providers[0].headers.X-API-Key", true},
		{"providers[0].headers.api-key", true},
		{"providers[0].headers.x-auth-token", true},
		{"providers[0].headers.x-token", true},
		{"providers[0].headers.x-custom", false},
		{"providers[0].headers.content-type", false},
		{"providers[0].api_key", true},
		{"admin.bootstrap_token", true},
	}
	for _, tt := range tests {
		got := isSecretPath(tt.path)
		if got != tt.expected {
			t.Errorf("isSecretPath(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

// --- stub for AuditLog ---

type stubAuditLog struct {
	events []audit.Event
}

func (s *stubAuditLog) Record(_ context.Context, event audit.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *stubAuditLog) List(_ context.Context, _ audit.Query) ([]audit.Event, error) {
	return append([]audit.Event(nil), s.events...), nil
}

type errorAuditLog struct{}

func (e *errorAuditLog) Record(_ context.Context, _ audit.Event) error {
	return context.DeadlineExceeded
}

func (e *errorAuditLog) List(_ context.Context, _ audit.Query) ([]audit.Event, error) {
	return nil, context.DeadlineExceeded
}

// --- stub for ProbeRunner ---

type stubProbeRunner struct {
	result *ProbeResult
	err    error
}

func (s *stubProbeRunner) ProbeProvider(_ context.Context, _ ProbeRequest) (*ProbeResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func (s *stubProbeRunner) ProbeModel(_ context.Context, _ ProbeRequest) (*ProbeResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

// --- stub for ConfigTools ---

type stubConfigTools struct {
	preview *ConfigPreviewResponse
	diff    *ConfigDiffResponse
	err     error
}

// --- stub for UpdateManager ---

type stubUpdateManager struct {
	status *updater.Status
	err    error

	checkCalled    bool
	fetchCalled    bool
	applyCalled    bool
	rollbackCalled bool

	lastForce      bool
	lastBundleDir  string
	lastDownload   bool
	lastDryRun     bool
	lastApplyForce bool
}

func (s *stubUpdateManager) Status() (*updater.Status, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubUpdateManager) Check(_ context.Context) (*updater.Status, error) {
	s.checkCalled = true
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubUpdateManager) Fetch(_ context.Context, force bool) (*updater.Status, error) {
	s.fetchCalled = true
	s.lastForce = force
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubUpdateManager) Apply(_ context.Context, bundleDir string, download bool, dryRun bool, force bool) (*updater.Status, error) {
	s.applyCalled = true
	s.lastBundleDir = bundleDir
	s.lastDownload = download
	s.lastDryRun = dryRun
	s.lastApplyForce = force
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubUpdateManager) Rollback() (*updater.Status, error) {
	s.rollbackCalled = true
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func (s *stubConfigTools) PreviewConfig(_ context.Context, _ ConfigPreviewRequest) (*ConfigPreviewResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.preview, nil
}

func (s *stubConfigTools) DiffConfig(_ context.Context, _ ConfigDiffRequest) (*ConfigDiffResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.diff, nil
}

// --- Helper: buildStatusPayload tests ---

func TestBuildStatusPayload_NoGateway_NoTelemetry(t *testing.T) {
	start := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	deps := Deps{Version: "test-v", StartedAt: start}
	resp := buildStatusPayload(deps)

	if resp["version"] != "test-v" {
		t.Fatalf("version = %v, want test-v", resp["version"])
	}
	if resp["product_version"] != version.ProductVersion {
		t.Fatalf("product_version mismatch")
	}
	if resp["rpc_contract_version"] != version.RPCContractVersion {
		t.Fatalf("rpc_contract_version mismatch")
	}
	if resp["gateway_status"] != "disconnected" {
		t.Fatalf("gateway_status = %v, want disconnected", resp["gateway_status"])
	}
	if resp["telemetry_status"] != "disconnected" {
		t.Fatalf("telemetry_status = %v, want disconnected", resp["telemetry_status"])
	}
}

func TestBuildStatusPayload_GatewayConnected(t *testing.T) {
	deps := Deps{
		StartedAt: time.Now(),
		GatewayRPC: &stubGatewayController{
			status: &gatewaycontrol.GetStatusResponse{
				Readiness:        gatewaycontrol.ReadinessReady,
				Listener:         ":18080",
				ActiveSnapshotID: "snap_1",
				ActiveRequests:   5,
				ProviderHealth:   map[string]gatewaycontrol.ProviderHealth{"openai": {Healthy: true}, "anthropic": {Healthy: false}},
			},
		},
	}
	resp := buildStatusPayload(deps)
	if resp["gateway_status"] != "connected" {
		t.Fatalf("gateway_status = %v, want connected", resp["gateway_status"])
	}
	if resp["gateway_readiness"] != "ready" {
		t.Fatalf("gateway_readiness = %v, want ready", resp["gateway_readiness"])
	}
	if resp["healthy_provider_count"] != 1 {
		t.Fatalf("healthy_provider_count = %v, want 1", resp["healthy_provider_count"])
	}
	if resp["unhealthy_provider_count"] != 1 {
		t.Fatalf("unhealthy_provider_count = %v, want 1", resp["unhealthy_provider_count"])
	}
}

func TestBuildStatusPayload_GatewayError(t *testing.T) {
	deps := Deps{
		StartedAt:  time.Now(),
		GatewayRPC: &stubGatewayController{err: context.DeadlineExceeded},
	}
	resp := buildStatusPayload(deps)
	if resp["gateway_status"] != "error" {
		t.Fatalf("gateway_status = %v, want error", resp["gateway_status"])
	}
}

func TestBuildStatusPayload_TelemetryConnected(t *testing.T) {
	deps := Deps{
		StartedAt:    time.Now(),
		TelemetryRPC: &stubTelemetryQuerier{ping: &telemetryquery.PingResponse{Version: "t1", EventCount: 42, Healthy: true}},
	}
	resp := buildStatusPayload(deps)
	if resp["telemetry_status"] != "connected" {
		t.Fatalf("telemetry_status = %v, want connected", resp["telemetry_status"])
	}
}
