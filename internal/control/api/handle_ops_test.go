package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/contracts/telemetryquery"
	"ai-model-gateway/internal/control/audit"
	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
)

// --- shared test deps ---

func baseHandlerDeps() Deps {
	return Deps{
		Version:   "test-v",
		StartedAt: time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC),
		AuditLog:  &stubAuditLog{},
		Runtime: RuntimeConfig{
			BundleVersion:   "1.0.0",
			ConfigPath:      "/etc/aigw/config.yaml",
			Listen:          ":18080",
			GatewaySocket:   "/tmp/gateway.sock",
			TelemetrySocket: "/tmp/telemetry.sock",
		},
	}
}

// ---------------------------------------------------------------------------
// runtimeStatusHandler
// ---------------------------------------------------------------------------

func TestRuntimeStatusHandler(t *testing.T) {
	deps := baseHandlerDeps()
	handler := runtimeStatusHandler(deps)

	req := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json err: %v", err)
	}
	if body["version"] != "test-v" {
		t.Errorf("version = %v, want test-v", body["version"])
	}
	if _, ok := body["runtime"]; !ok {
		t.Errorf("response missing runtime field")
	}
	runtime, ok := body["runtime"].(map[string]any)
	if !ok {
		t.Fatal("runtime is not a map")
	}
	if runtime["bundle_version"] != "1.0.0" {
		t.Errorf("bundle_version = %v, want 1.0.0", runtime["bundle_version"])
	}
}

// ---------------------------------------------------------------------------
// runtimePreflightHandler
// ---------------------------------------------------------------------------

func TestRuntimePreflightHandler(t *testing.T) {
	t.Run("GET returns 405", func(t *testing.T) {
		deps := baseHandlerDeps()
		handler := runtimePreflightHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/ops/preflight", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("POST with nil gateway shows not ok", func(t *testing.T) {
		deps := baseHandlerDeps()
		handler := runtimePreflightHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/ops/preflight", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if body["ok"] != false {
			t.Errorf("ok = %v, want false", body["ok"])
		}
	})

	t.Run("POST with ready gateway shows ok true", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.GatewayRPC = &stubGatewayController{
			status: &gatewaycontrol.GetStatusResponse{
				Readiness:        gatewaycontrol.ReadinessReady,
				Listener:         ":18080",
				ActiveSnapshotID: "snap_1",
				ActiveRequests:   3,
				ProviderHealth:   map[string]gatewaycontrol.ProviderHealth{},
			},
		}
		deps.TelemetryRPC = &stubTelemetryQuerier{
			ping: &telemetryquery.PingResponse{
				Version:    "t1",
				EventCount: 42,
				Healthy:    true,
			},
		}
		handler := runtimePreflightHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/ops/preflight", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if body["ok"] != true {
			t.Errorf("ok = %v, want true", body["ok"])
		}
		checks, ok := body["checks"].([]any)
		if !ok || len(checks) == 0 {
			t.Fatal("missing or empty checks")
		}
	})

	t.Run("POST with gateway but not ready", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.GatewayRPC = &stubGatewayController{
			status: &gatewaycontrol.GetStatusResponse{
				Readiness:        gatewaycontrol.ReadinessStarting,
				Listener:         ":18080",
				ActiveSnapshotID: "",
				ActiveRequests:   0,
				ProviderHealth:   map[string]gatewaycontrol.ProviderHealth{},
			},
		}
		handler := runtimePreflightHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/ops/preflight", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if body["ok"] != false {
			t.Errorf("ok = %v, want false", body["ok"])
		}
	})
}

// ---------------------------------------------------------------------------
// auditHandler
// ---------------------------------------------------------------------------

func TestAuditHandler(t *testing.T) {
	t.Run("POST returns 405", func(t *testing.T) {
		handler := auditHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/audit", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("GET with nil audit log returns empty", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.AuditLog = nil
		handler := auditHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/audit", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		events, _ := body["events"].([]any)
		if len(events) != 0 {
			t.Errorf("len(events) = %d, want 0", len(events))
		}
		if body["count"] != float64(0) {
			t.Errorf("count = %v, want 0", body["count"])
		}
	})

	t.Run("GET with stub audit log returns events", func(t *testing.T) {
		log := &stubAuditLog{}
		deps := baseHandlerDeps()
		deps.AuditLog = log
		_ = log.Record(nil, audit.Event{Action: "test"})

		handler := auditHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/audit?action=test&limit=5&since=2026-01-01T00:00:00Z", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if body["count"] == nil {
			t.Error("count is nil")
		}
	})

	t.Run("GET with error audit log returns 500", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.AuditLog = &errorAuditLog{}
		handler := auditHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/audit", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// configPreviewHandler
// ---------------------------------------------------------------------------

func TestConfigPreviewHandler(t *testing.T) {
	t.Run("GET returns 405", func(t *testing.T) {
		handler := configPreviewHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/config/preview", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("POST with nil ConfigTools returns 503", func(t *testing.T) {
		handler := configPreviewHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/config/preview", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST with invalid JSON returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{}
		handler := configPreviewHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/preview", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with preview error returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{err: errors.New("preview failed")}
		handler := configPreviewHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/preview", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with valid preview", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{
			preview: &ConfigPreviewResponse{
				Valid:         true,
				RevisionID:    "rev_1",
				ProviderCount: 2,
				EnabledRoutes: []string{"/v1/chat"},
			},
		}
		handler := configPreviewHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/preview", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body ConfigPreviewResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if !body.Valid {
			t.Errorf("Valid = false, want true")
		}
	})
}

// ---------------------------------------------------------------------------
// configDiffHandler
// ---------------------------------------------------------------------------

func TestConfigDiffHandler(t *testing.T) {
	t.Run("GET returns 405", func(t *testing.T) {
		handler := configDiffHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/config/diff", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("POST with nil ConfigTools returns 503", func(t *testing.T) {
		handler := configDiffHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/config/diff", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST with invalid JSON returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{}
		handler := configDiffHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/diff", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with diff error returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{err: errors.New("diff failed")}
		handler := configDiffHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/diff", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with valid diff", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigTools = &stubConfigTools{
			diff: &ConfigDiffResponse{
				FromRevisionID: "rev_0",
				ToRevisionID:   "rev_1",
				Changes:        []DiffChange{{Path: "providers[0].name", Kind: "changed"}},
			},
		}
		handler := configDiffHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/config/diff", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body ConfigDiffResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if len(body.Changes) != 1 {
			t.Errorf("len(Changes) = %d, want 1", len(body.Changes))
		}
	})
}

// ---------------------------------------------------------------------------
// handleProbe / probeProviderHandler / probeModelHandler
// ---------------------------------------------------------------------------

func TestProbeProviderHandler(t *testing.T) {
	t.Run("GET returns 405", func(t *testing.T) {
		handler := probeProviderHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/probe/provider", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("POST with nil ProbeRunner returns 503", func(t *testing.T) {
		handler := probeProviderHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/probe/provider", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST with invalid JSON returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ProbeRunner = &stubProbeRunner{}
		handler := probeProviderHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/probe/provider", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with probe error returns 400", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ProbeRunner = &stubProbeRunner{err: errors.New("probe failed")}
		handler := probeProviderHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/probe/provider", strings.NewReader(`{"provider_id":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with successful probe", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ProbeRunner = &stubProbeRunner{
			result: &ProbeResult{
				Diagnostic: true,
				ProviderID: "test",
				Healthy:    true,
				StatusCode: 200,
				LatencyMs:  150,
				ProbedAt:   time.Now(),
			},
		}
		handler := probeProviderHandler(deps)
		req := httptest.NewRequest(http.MethodPost, "/probe/provider", strings.NewReader(`{"provider_id":"test","model":"gpt-4"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: body=%s", rec.Code, rec.Body.String())
		}
		var body ProbeResult
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if !body.Healthy {
			t.Errorf("Healthy = false, want true")
		}
	})
}

func TestProbeModelHandler(t *testing.T) {
	deps := baseHandlerDeps()
	deps.ProbeRunner = &stubProbeRunner{
		result: &ProbeResult{
			Diagnostic: true,
			Model:      "gpt-4",
			Healthy:    true,
			StatusCode: 200,
		},
	}
	handler := probeModelHandler(deps)
	req := httptest.NewRequest(http.MethodPost, "/probe/model", strings.NewReader(`{"provider_id":"test","model":"gpt-4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body ProbeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json err: %v", err)
	}
	if !body.Healthy {
		t.Errorf("Healthy = false, want true")
	}
}

// ---------------------------------------------------------------------------
// diagnosticsHandler
// ---------------------------------------------------------------------------

func TestDiagnosticsHandler(t *testing.T) {
	t.Run("POST returns 405", func(t *testing.T) {
		handler := diagnosticsHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/ops/diagnostics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("GET returns diagnostics payload", func(t *testing.T) {
		handler := diagnosticsHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/ops/diagnostics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if _, ok := body["generated_at"]; !ok {
			t.Error("response missing generated_at")
		}
		if body["redacted"] != true {
			t.Errorf("redacted = %v, want true", body["redacted"])
		}
		if _, ok := body["status"]; !ok {
			t.Error("response missing status")
		}
		if _, ok := body["runtime"]; !ok {
			t.Error("response missing runtime")
		}
		if _, ok := body["audit_tail"]; !ok {
			t.Error("response missing audit_tail")
		}
	})
}

// ---------------------------------------------------------------------------
// secretsStatusHandler
// ---------------------------------------------------------------------------

func TestSecretsStatusHandler(t *testing.T) {
	t.Run("POST returns 405", func(t *testing.T) {
		handler := secretsStatusHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/ops/secrets", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("GET with nil ConfigQuery returns 503", func(t *testing.T) {
		handler := secretsStatusHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/ops/secrets", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("GET with config query returns secrets status", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.ConfigQuery = &stubConfigQueryForSecrets{
			view: &publish.CurrentConfigView{
				Config: &core.Config{
					Admin: core.AdminConfig{
						BootstrapToken: "tok",
					},
				},
			},
		}
		handler := secretsStatusHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/ops/secrets", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("json err: %v", err)
		}
		if body["redacted"] != true {
			t.Errorf("redacted = %v, want true", body["redacted"])
		}
		if _, ok := body["items"]; !ok {
			t.Error("response missing items")
		}
		if _, ok := body["count"]; !ok {
			t.Error("response missing count")
		}
		if _, ok := body["ok"]; !ok {
			t.Error("response missing ok")
		}
	})
}

// ---------------------------------------------------------------------------
// clientErrorHandler
// ---------------------------------------------------------------------------

func TestClientErrorHandler(t *testing.T) {
	t.Run("GET returns 405", func(t *testing.T) {
		handler := clientErrorHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodGet, "/ops/client-error", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("POST with invalid JSON returns 400", func(t *testing.T) {
		handler := clientErrorHandler(baseHandlerDeps())
		req := httptest.NewRequest(http.MethodPost, "/ops/client-error", strings.NewReader(`{invalid`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("POST with valid error payload returns 204", func(t *testing.T) {
		deps := baseHandlerDeps()
		handler := clientErrorHandler(deps)
		body := `{"message":"test error","stack":"stack trace","source":"ui","url":"/admin/logs"}`
		req := httptest.NewRequest(http.MethodPost, "/ops/client-error", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// replayUnavailableHandler
// ---------------------------------------------------------------------------

func TestReplayUnavailableHandler(t *testing.T) {
	t.Run("GET returns 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/replay", nil)
		rec := httptest.NewRecorder()
		replayUnavailableHandler(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})

	t.Run("POST returns 503", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/replay", nil)
		rec := httptest.NewRecorder()
		replayUnavailableHandler(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// metricsHandler
// ---------------------------------------------------------------------------

func TestMetricsHandler(t *testing.T) {
	t.Run("GET returns prometheus-style text", func(t *testing.T) {
		deps := baseHandlerDeps()
		handler := metricsHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/ops/metrics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/plain") {
			t.Errorf("Content-Type = %q, want text/plain", ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "aigw_control_up 1") {
			t.Errorf("body missing aigw_control_up: %s", body)
		}
		if !strings.Contains(body, "aigw_gateway_connected") {
			t.Errorf("body missing aigw_gateway_connected: %s", body)
		}
		if !strings.Contains(body, "aigw_telemetry_connected") {
			t.Errorf("body missing aigw_telemetry_connected: %s", body)
		}
	})

	t.Run("GET with connected gateway shows active_requests", func(t *testing.T) {
		deps := baseHandlerDeps()
		deps.GatewayRPC = &stubGatewayController{
			status: &gatewaycontrol.GetStatusResponse{
				Readiness:      gatewaycontrol.ReadinessReady,
				ActiveRequests: 5,
				ProviderHealth: map[string]gatewaycontrol.ProviderHealth{},
			},
		}
		handler := metricsHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/ops/metrics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "aigw_gateway_connected 1") {
			t.Errorf("expected gateway connected 1, got: %s", body)
		}
		if !strings.Contains(body, "aigw_active_requests 5") {
			t.Errorf("expected aigw_active_requests 5, got: %s", body)
		}
	})

	t.Run("GET with disconnected gateway shows 0", func(t *testing.T) {
		deps := baseHandlerDeps()
		// No GatewayRPC = disconnected
		handler := metricsHandler(deps)
		req := httptest.NewRequest(http.MethodGet, "/ops/metrics", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		body := rec.Body.String()
		if !strings.Contains(body, "aigw_gateway_connected 0") {
			t.Errorf("expected gateway connected 0, got: %s", body)
		}
	})
}
