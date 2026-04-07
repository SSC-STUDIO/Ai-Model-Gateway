package adminapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/auth"
	"ai-model-gateway/internal/infra/telemetrydb"

	"github.com/go-chi/chi/v5"
)

var (
	testToken               = makeTestSecret("bootstrap-token")
	testSigningKey          = makeTestSecret("signing-key")
	testBootstrapSecret     = makeTestSecret("bootstrap-hidden")
	testCookieSigningSecret = makeTestSecret("cookie-hidden")
)

var adminAssetPathPattern = regexp.MustCompile(`/admin/assets/[^"' >]+`)

func makeTestSecret(label string) string {
	if len(label) >= 34 {
		return label[:34]
	}
	return label + strings.Repeat("x", 34-len(label))
}

func setupTestDeps(t *testing.T) (Deps, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := telemetrydb.New(core.TelemetryConfig{
		SQLitePath:     filepath.Join(dir, "test.db"),
		RetentionDays:  7,
		AggregationSec: 60,
		CacheTTLSec:    1,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	tr := true
	providers := []core.Provider{
		{
			Name:             "test-a",
			BaseURL:          "https://a.com",
			AnthropicBaseURL: "https://anthropic.a.com",
			APIKey:           "sk-test-a",
			ProviderClass:    core.ProviderClassQuotaLimited,
			Models:           []string{"gpt-4o", "gpt-4o-mini"},
			Weight:           1,
			TimeoutMs:        45000,
			SameRetries:      2,
			Enabled:          &tr,
			Headers: map[string]string{
				"X-Test-Header": "enabled",
				"Authorization": "Bearer provider-secret",
			},
		},
		{Name: "test-b", BaseURL: "https://b.com", APIKey: "sk-test-b", Models: []string{"claude-3"}, Weight: 1, Enabled: &tr},
	}
	cfg := &core.Config{
		Server: core.ServerConfig{Listen: ":18080"},
		Admin: core.AdminConfig{
			Enabled:          true,
			BootstrapToken:   testBootstrapSecret,
			CookieSigningKey: testCookieSigningSecret,
			Language:         "en",
		},
		Providers: providers,
		Routing:   core.RoutingConfig{Strategy: "health_weighted_rr"},
		Telemetry: core.TelemetryConfig{RetentionDays: 7},
		Pricing: core.PricingConfig{
			CachePath:            "data/pricing-cache.json",
			RefreshIntervalHours: 12,
			RequestTimeoutMs:     15000,
		},
		Compat: core.CompatConfig{
			Bridge: core.BridgeConfig{
				Enabled: true,
				Rules: []core.BridgeRule{
					{From: "gpt-4.1", To: "gpt-4o"},
				},
			},
			Fallback: core.FallbackConfig{
				Enabled: true,
				Models: map[string]string{
					"gpt-4.1": "gpt-4o-mini",
				},
			},
		},
	}

	sel := &mockSelector{models: []string{"gpt-4o", "gpt-4o-mini", "claude-3"}}

	d := Deps{
		Auth:      auth.New(testToken, testSigningKey),
		Store:     store,
		Selector:  sel,
		GetConfig: func() *core.Config { return cfg },
	}
	cleanup := func() { store.Close() }
	return d, cleanup
}

type mockSelector struct {
	models []string
}

func (m *mockSelector) Select(_ context.Context, _ string, _ string) (*core.Provider, error) {
	return nil, core.ErrNoProvider
}
func (m *mockSelector) RememberSticky(_ string, _ string)                              {}
func (m *mockSelector) ReportResult(_ *core.Provider, _ int, _ time.Duration, _ error) {}
func (m *mockSelector) ListModels() []string                                           { return m.models }

func setupRouter(t *testing.T, d Deps) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	Mount(r, d)
	return r
}

func authedRequest(t *testing.T, method, path string, body string) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func cookieAuthedRequest(t *testing.T, method, path string, body string, cookie *http.Cookie) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:18080"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func loginCookie(t *testing.T, r chi.Router) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/admin/auth/login", strings.NewReader(`{"token":"`+testToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected login to return auth cookie")
	}
	return cookies[0]
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd to %s: %v", prev, err)
		}
	})
}

func firstAdminAssetPath(t *testing.T, body string) string {
	t.Helper()
	match := adminAssetPathPattern.FindString(body)
	if match == "" {
		t.Fatalf("expected admin index to reference /admin/assets/*, got body: %s", body)
	}
	return match
}

func TestLogin_Success(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	r := setupRouter(t, d)

	req := httptest.NewRequest("POST", "/api/admin/auth/login", strings.NewReader(`{"token":"`+testToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Should have Set-Cookie header.
	if cookie := w.Header().Get("Set-Cookie"); cookie == "" {
		t.Error("expected Set-Cookie header")
	}
}

func TestLogin_InvalidToken(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	r := setupRouter(t, d)

	req := httptest.NewRequest("POST", "/api/admin/auth/login", strings.NewReader(`{"token":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestProtectedEndpoint_Unauthorized(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	r := setupRouter(t, d)

	req := httptest.NewRequest("GET", "/api/admin/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestOverview_WithAuth(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	seedTelemetry(t, d.Store)

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/overview", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	// Should have windowed metrics.
	for _, key := range []string{"last_1m", "last_5m", "last_1h", "last_24h"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing %s in overview", key)
		}
	}
	if runtimeView, ok := result["runtime"].(map[string]interface{}); !ok {
		t.Fatalf("expected runtime view in overview, got %#v", result["runtime"])
	} else {
		if runtimeView["router_strategy"] != "health_weighted_rr" {
			t.Fatalf("expected router_strategy health_weighted_rr, got %#v", runtimeView["router_strategy"])
		}
		if runtimeView["bridge_enabled"] != true {
			t.Fatalf("expected bridge_enabled true, got %#v", runtimeView["bridge_enabled"])
		}
	}
	if models, ok := result["available_models"].([]interface{}); !ok || len(models) != 3 {
		t.Fatalf("expected available_models to expose 3 models, got %#v", result["available_models"])
	}
}

func TestData_Unauthorized(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	r := setupRouter(t, d)

	req := httptest.NewRequest("GET", "/api/admin/data", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestData_WithAuth(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	seedTelemetry(t, d.Store)

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/data?hours=1&limit=10", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"summary", "requests", "errors", "models", "upstreams"} {
		if _, ok := result[key]; !ok {
			t.Errorf("missing %s in data response", key)
		}
	}
	if _, ok := result["runtime"]; !ok {
		t.Fatalf("expected runtime in data response, got %#v", result)
	}
	if models, ok := result["available_models"].([]interface{}); !ok || len(models) != 3 {
		t.Fatalf("expected available_models to expose 3 models, got %#v", result["available_models"])
	}
	if h, ok := result["window_hours"].(float64); !ok || h != 1 {
		t.Errorf("expected window_hours=1, got %v", result["window_hours"])
	}
}

func TestData_WithPricingHook_AdditiveField(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	seedTelemetry(t, d.Store)
	d.PricingEconomics = func() (interface{}, error) {
		return map[string]interface{}{
			"catalog_version": "test-v1",
			"models":          []string{"gpt-4o"},
		}, nil
	}

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/data?hours=1&limit=10", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := result["pricing_economics"]; !ok {
		t.Fatalf("expected pricing_economics field when hook is provided, got %v", result)
	}
}

func TestData_WithPricingHookError_OmitsField(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	seedTelemetry(t, d.Store)
	d.PricingEconomics = func() (interface{}, error) {
		return nil, fmt.Errorf("pricing unavailable")
	}

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/data?hours=1&limit=10", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := result["pricing_economics"]; ok {
		t.Fatalf("expected pricing_economics field to be omitted when hook errors, got %v", result["pricing_economics"])
	}
}

func TestTimeseries_WithAuth(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()
	seedTelemetry(t, d.Store)

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/timeseries?hours=1&bucket=1", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if points, ok := result["points"].([]interface{}); !ok || len(points) == 0 {
		t.Errorf("expected non-empty points, got %v", result["points"])
	}
	if b, ok := result["bucket_minutes"].(float64); !ok || b != 1 {
		t.Errorf("expected bucket_minutes=1, got %v", result["bucket_minutes"])
	}
}

func TestModels_WithAuth(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/models", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	models, ok := result["models"].([]interface{})
	if !ok {
		t.Fatal("expected models array")
	}
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}
	if count, _ := result["model_count"].(float64); count != 3 {
		t.Errorf("expected model_count=3, got %v", result["model_count"])
	}
	if prov, _ := result["enabled_providers"].(float64); prov != 2 {
		t.Errorf("expected enabled_providers=2, got %v", result["enabled_providers"])
	}
}

func TestConfig_WithAuth_SanitizedKeys(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/config", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal config response: %v", err)
	}
	for _, key := range []string{"server", "admin", "routing", "telemetry", "pricing", "compat", "providers"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected top-level %q in config payload, got %#v", key, payload)
		}
	}
	body := w.Body.String()
	if strings.Contains(body, "api_key") {
		t.Error("config response should not contain api_key")
	}
	if strings.Contains(body, testBootstrapSecret) || strings.Contains(body, testCookieSigningSecret) {
		t.Fatalf("config response leaked admin secret values: %s", body)
	}

	providers, ok := payload["providers"].([]interface{})
	if !ok || len(providers) == 0 {
		t.Fatalf("expected non-empty providers array, got %#v", payload["providers"])
	}
	firstProvider, ok := providers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected provider object, got %#v", providers[0])
	}
	if firstProvider["name"] != "test-a" {
		t.Fatalf("expected first provider test-a, got %#v", firstProvider["name"])
	}
	if firstProvider["timeout_ms"] != float64(45000) {
		t.Fatalf("expected timeout_ms 45000, got %#v", firstProvider["timeout_ms"])
	}
	if firstProvider["same_retries"] != float64(2) {
		t.Fatalf("expected same_retries 2, got %#v", firstProvider["same_retries"])
	}
	headers, ok := firstProvider["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected provider headers map, got %#v", firstProvider["headers"])
	}
	if headers["X-Test-Header"] != "enabled" {
		t.Fatalf("expected non-sensitive header to remain visible, got %#v", headers)
	}
	if headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("expected sensitive provider header to be redacted, got %#v", headers["Authorization"])
	}
}

func TestConfigExport_WithAuth(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/config/export", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "yaml") {
		t.Fatalf("expected yaml content-type, got %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, "providers:") {
		t.Fatalf("expected providers in export payload, got %q", body)
	}
	if strings.Contains(body, testBootstrapSecret) ||
		strings.Contains(body, testCookieSigningSecret) ||
		strings.Contains(body, "sk-test-a") ||
		strings.Contains(body, "sk-test-b") {
		t.Fatalf("config export fallback leaked secret data: %q", body)
	}
}

func TestConfigSave_NotImplementedWithoutDeps(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "PUT", "/api/admin/config", `{"admin":{"language":"en"}}`)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConfigSave_CookieMutationRequiresSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	cookie := loginCookie(t, r)

	t.Run("cross origin denied", func(t *testing.T) {
		req := cookieAuthedRequest(t, "PUT", "/api/admin/config", `{"admin":{"language":"en"}}`, cookie)
		req.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("same origin allowed to reach handler", func(t *testing.T) {
		req := cookieAuthedRequest(t, "PUT", "/api/admin/config", `{"admin":{"language":"en"}}`, cookie)
		req.Header.Set("Origin", "http://127.0.0.1:18080")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code == http.StatusForbidden {
			t.Fatalf("expected non-403 for same-origin cookie request, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestConfigSave_BearerMutationBypassesSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "PUT", "/api/admin/config", `{"admin":{"language":"en"}}`)
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "127.0.0.1:18080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("expected non-403 for bearer mutation, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConfigHistory_NotImplementedWithoutDeps(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/config/history", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConfigHistoryDiff_NotImplementedWithoutDeps(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "GET", "/api/admin/config/history/v1/diff", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConfigRollback_CookieMutationRequiresSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	cookie := loginCookie(t, r)

	t.Run("cross origin denied", func(t *testing.T) {
		req := cookieAuthedRequest(t, "POST", "/api/admin/config/rollback", `{}`, cookie)
		req.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("same origin allowed to reach handler", func(t *testing.T) {
		req := cookieAuthedRequest(t, "POST", "/api/admin/config/rollback", `{}`, cookie)
		req.Header.Set("Origin", "http://127.0.0.1:18080")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Handler currently depends on runtime rollback plumbing. Same-origin should pass middleware.
		if w.Code == http.StatusForbidden {
			t.Fatalf("expected non-403 for same-origin cookie request, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestConfigRollback_BearerMutationBypassesSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := authedRequest(t, "POST", "/api/admin/config/rollback", `{}`)
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "127.0.0.1:18080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// With bearer auth, request should reach handler (currently 501 without rollback dependency).
	if w.Code == http.StatusForbidden {
		t.Fatalf("expected non-403 for bearer mutation, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpstreamsTest_CookieMutationRequiresSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	d.UpstreamTest = func(upstream core.Provider, health core.HealthCheckConfig) (interface{}, int, error) {
		return map[string]interface{}{
			"ok":         true,
			"target_url": upstream.BaseURL + health.Path,
			"checked_at": time.Now().Format(time.RFC3339),
		}, http.StatusOK, nil
	}

	r := setupRouter(t, d)
	cookie := loginCookie(t, r)
	payload := `{"upstream":{"name":"probe","base_url":"https://example.com","api_key":"sk-demo","timeout_ms":2000,"enabled":true}}`

	t.Run("cross origin denied", func(t *testing.T) {
		req := cookieAuthedRequest(t, "POST", "/api/admin/upstreams/test", payload, cookie)
		req.Header.Set("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("same origin succeeds", func(t *testing.T) {
		req := cookieAuthedRequest(t, "POST", "/api/admin/upstreams/test", payload, cookie)
		req.Header.Set("Origin", "http://127.0.0.1:18080")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestUpstreamsTest_BearerMutationBypassesSameOrigin(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	d.UpstreamTest = func(upstream core.Provider, health core.HealthCheckConfig) (interface{}, int, error) {
		return map[string]interface{}{
			"ok":         true,
			"target_url": upstream.BaseURL + health.Path,
			"checked_at": time.Now().Format(time.RFC3339),
		}, http.StatusOK, nil
	}

	r := setupRouter(t, d)
	payload := `{"upstream":{"name":"probe","base_url":"https://example.com","api_key":"sk-demo","timeout_ms":2000,"enabled":true}}`
	req := authedRequest(t, "POST", "/api/admin/upstreams/test", payload)
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "127.0.0.1:18080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpstreamsTest_FallbackProbeRejectsLoopback(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"}]}`))
	}))
	defer upstream.Close()

	r := setupRouter(t, d)
	payload := fmt.Sprintf(`{"upstream":{"name":"probe","base_url":"%s","api_key":"sk-demo","timeout_ms":2000,"enabled":true}}`, upstream.URL)
	req := authedRequest(t, "POST", "/api/admin/upstreams/test", payload)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for loopback SSRF rejection, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SSRF validation failed") {
		t.Fatalf("expected SSRF validation error, got %s", w.Body.String())
	}
}

func TestLogout(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := httptest.NewRequest("POST", "/api/admin/auth/logout", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAdminFrontendServesEmbeddedIndexWhenDistMissing(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(strings.ToLower(body), "<!doctype html") && !strings.Contains(strings.ToLower(body), "<html") {
		t.Fatal("expected HTML response for /admin")
	}
	if !strings.Contains(body, "/admin/assets/") {
		t.Fatalf("expected embedded admin index with asset references, got body: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "assets not found") {
		t.Fatalf("expected embedded admin index, got placeholder body: %s", body)
	}
}

func TestAdminFrontendServesEmbeddedAssetWhenDistMissing(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)

	indexReq := httptest.NewRequest("GET", "/admin", nil)
	indexRes := httptest.NewRecorder()
	r.ServeHTTP(indexRes, indexReq)
	if indexRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for /admin, got %d", indexRes.Code)
	}

	assetPath := firstAdminAssetPath(t, indexRes.Body.String())
	assetReq := httptest.NewRequest("GET", assetPath, nil)
	assetRes := httptest.NewRecorder()
	r.ServeHTTP(assetRes, assetReq)

	if assetRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d: %s", assetPath, assetRes.Code, assetRes.Body.String())
	}
	if assetRes.Body.Len() == 0 {
		t.Fatalf("expected non-empty body for %s", assetPath)
	}
	if strings.HasSuffix(assetPath, ".js") && !strings.Contains(assetRes.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("expected JavaScript content type for %s, got %q", assetPath, assetRes.Header().Get("Content-Type"))
	}
	if strings.HasSuffix(assetPath, ".css") && !strings.Contains(assetRes.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("expected CSS content type for %s, got %q", assetPath, assetRes.Header().Get("Content-Type"))
	}
}

func TestAdminFrontendMissingAssetReturnsNotFound(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	r := setupRouter(t, d)
	req := httptest.NewRequest("GET", "/admin/assets/does-not-exist.js", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminFrontendPrefersDiskDistWhenPresent(t *testing.T) {
	d, cleanup := setupTestDeps(t)
	defer cleanup()

	root := t.TempDir()
	withWorkingDir(t, root)

	distDir := filepath.Join(root, "web", "admin", "dist")
	if err := os.MkdirAll(filepath.Join(distDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir dist assets: %v", err)
	}
	indexBody := `<!DOCTYPE html><html><body>disk-admin<script src="/admin/assets/app.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte(indexBody), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "assets", "app.js"), []byte(`console.log("disk-admin");`), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	r := setupRouter(t, d)

	indexReq := httptest.NewRequest("GET", "/admin", nil)
	indexRes := httptest.NewRecorder()
	r.ServeHTTP(indexRes, indexReq)
	if indexRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for /admin, got %d", indexRes.Code)
	}
	if !strings.Contains(indexRes.Body.String(), "disk-admin") {
		t.Fatalf("expected disk-served index, got %s", indexRes.Body.String())
	}

	assetReq := httptest.NewRequest("GET", "/admin/assets/app.js", nil)
	assetRes := httptest.NewRecorder()
	r.ServeHTTP(assetRes, assetReq)
	if assetRes.Code != http.StatusOK {
		t.Fatalf("expected 200 for disk asset, got %d: %s", assetRes.Code, assetRes.Body.String())
	}
	if !strings.Contains(assetRes.Body.String(), "disk-admin") {
		t.Fatalf("expected disk-served asset, got %s", assetRes.Body.String())
	}
}

func TestMountNilAuth(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, Deps{Auth: nil})
	// Should not panic and no routes should be mounted.

	req := httptest.NewRequest("GET", "/api/admin/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404 when auth is nil, got %d", w.Code)
	}
}

// Ensure unused import doesn't cause issues.
var _ = os.DevNull

func seedTelemetry(t *testing.T, store *telemetrydb.Store) {
	t.Helper()
	now := time.Now().UTC()
	ctx := context.Background()
	records := []*core.RequestRecord{
		{
			RequestID: "req-1", Timestamp: now.Add(-2 * time.Minute), Model: "gpt-4o", Provider: "test-a",
			StatusCode: 200, Latency: 120 * time.Millisecond, InputTokens: 10, OutputTokens: 5,
		},
		{
			RequestID: "req-2", Timestamp: now.Add(-1 * time.Minute), Model: "gpt-4o-mini", Provider: "test-a",
			StatusCode: 500, Latency: 240 * time.Millisecond, InputTokens: 12, OutputTokens: 0, Error: "upstream timeout",
		},
		{
			RequestID: "req-3", Timestamp: now.Add(-30 * time.Second), Model: "claude-3", Provider: "test-b",
			StatusCode: 200, Latency: 80 * time.Millisecond, InputTokens: 8, OutputTokens: 6,
		},
	}
	for _, rec := range records {
		if err := store.Record(ctx, rec); err != nil {
			t.Fatalf("record telemetry: %v", err)
		}
	}
	store.Flush()
}
