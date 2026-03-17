package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/router"
	"ai-model-gateway/internal/state"
	"ai-model-gateway/internal/telemetry"
)

func newTestStore(t *testing.T) *telemetry.Store {
	t.Helper()
	store, err := telemetry.NewStore(t.TempDir() + "/telemetry.db")
	if err != nil {
		t.Fatalf("new telemetry store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestHealthIncludesRequestID(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "health_weighted_rr"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	req.Header.Set(observability.RequestIDHeader, "req-health")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-health" {
		t.Fatalf("expected request id header to round-trip, got %q", got)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got := body["request_id"]; got != "req-health" {
		t.Fatalf("expected request_id in body, got %#v", got)
	}
}

func TestResponsesResourceRouteProxiesWithoutBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/responses/resp_123" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_123","object":"response"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: upstream.URL, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set(observability.RequestIDHeader, "req-response-get")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "alpha" {
		t.Fatalf("expected upstream header alpha, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-response-get" {
		t.Fatalf("expected request id header, got %q", got)
	}
}

func TestResponsesCompactRouteProxiesPostBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/responses/compact" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "gpt-5.2-codex" {
			t.Fatalf("expected model to be forwarded, got %#v", payload["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cmp_123","object":"response.compaction","usage":{"input_tokens":12,"output_tokens":4,"total_tokens":16}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: upstream.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.2-codex","input":[{"role":"user","content":"checkpoint"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(observability.RequestIDHeader, "req-response-compact")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "alpha" {
		t.Fatalf("expected upstream header alpha, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-response-compact" {
		t.Fatalf("expected request id header, got %q", got)
	}
}

func TestFileContentRouteProxiesWithoutBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/files/file_123/content" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "file-bytes")
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "files", BaseURL: upstream.URL, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/v1/files/file_123/content", nil)
	req.Header.Set(observability.RequestIDHeader, "req-file-content")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "files" {
		t.Fatalf("expected upstream header files, got %q", got)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("expected octet-stream content type, got %q", got)
	}
}

func TestAdminDataIncludesTelemetry(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Admin:  config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()

	stats := newTestStore(t)
	stats.RecordRequest(telemetry.RequestRecord{
		Timestamp:  time.Now(),
		RequestID:  "req-admin",
		Path:       "/v1/chat/completions",
		Model:      "gpt-5.2",
		Upstream:   "alpha",
		StatusCode: 200,
		Attempts:   1,
		Success:    true,
		Usage: telemetry.Usage{
			PromptTokens:     12,
			CompletionTokens: 8,
			TotalTokens:      20,
		},
	})

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), stats, telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/-/admin/data", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	telemetryMap, ok := body["telemetry"].(map[string]any)
	if !ok {
		t.Fatalf("expected telemetry object")
	}
	summary, ok := telemetryMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary object")
	}
	if summary["total_tokens"] != float64(20) {
		t.Fatalf("expected total tokens 20, got %#v", summary["total_tokens"])
	}
	performance, ok := telemetryMap["performance"].(map[string]any)
	if !ok {
		t.Fatalf("expected performance object")
	}
	if _, ok := performance["last_1m"].(map[string]any); !ok {
		t.Fatalf("expected last_1m performance window")
	}
	pricingMap, ok := body["pricing"].(map[string]any)
	if !ok {
		t.Fatalf("expected pricing object")
	}
	pricingSummary, ok := pricingMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("expected pricing summary")
	}
	if pricingSummary["currency"] != "USD" {
		t.Fatalf("expected USD currency, got %#v", pricingSummary["currency"])
	}
}

func TestAdminRequiresAuth(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Result().StatusCode)
	}
}

func TestAdminOverviewRouteIncludesDenseOverviewControls(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected overview 200, got %d", recorder.Result().StatusCode)
	}
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read overview body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `id="overviewPulse"`) ||
		!strings.Contains(text, `id="overviewQuickNav"`) ||
		!strings.Contains(text, `id="overviewAlerts"`) ||
		!strings.Contains(text, `id="performanceMeta"`) ||
		!strings.Contains(text, `id="costMeta"`) ||
		!strings.Contains(text, `id="economicsMeta"`) ||
		!strings.Contains(text, `id="economicsTopline"`) ||
		!strings.Contains(text, `id="upstreamMeta"`) ||
		!strings.Contains(text, `id="upstreamTopline"`) ||
		!strings.Contains(text, `id="cacheMeta"`) ||
		!strings.Contains(text, `id="cacheTopline"`) ||
		!strings.Contains(text, `id="errorsMeta"`) ||
		!strings.Contains(text, `id="errorsTopline"`) ||
		!strings.Contains(text, `id="requestsMeta"`) ||
		!strings.Contains(text, `id="requestsTopline"`) ||
		!strings.Contains(text, `id="usageTopline"`) ||
		!strings.Contains(text, `data-topnav-target="performance"`) ||
		!strings.Contains(text, `data-overview-target="performance"`) ||
		!strings.Contains(text, `Jump to Surface`) ||
		!strings.Contains(text, `Request Load`) ||
		!strings.Contains(text, `Health Watch`) ||
		!strings.Contains(text, `Error Pressure`) ||
		!strings.Contains(text, `Pricing Coverage`) ||
		!strings.Contains(text, `Top Model 1`) ||
		!strings.Contains(text, `Cache Leader 1`) ||
		!strings.Contains(text, `Top Upstream 1`) ||
		!strings.Contains(text, `Degraded Routes`) ||
		!strings.Contains(text, `Dominant Upstream`) ||
		!strings.Contains(text, `Hottest Path`) ||
		!strings.Contains(text, `status-chip`) ||
		!strings.Contains(text, `surface-card`) ||
		!strings.Contains(text, `scroll-margin-top: 88px;`) ||
		!strings.Contains(text, `Performance`) ||
		!strings.Contains(text, `Economics`) ||
		!strings.Contains(text, `Latest traces and failures`) {
		t.Fatalf("expected overview dense ui markers, got %q", text)
	}
}

func TestAdminSettingsAndFaviconRoutes(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	settingsReq := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	settingsReq.Header.Set("Authorization", "Bearer secret")
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsReq)
	if settingsRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected settings 200, got %d", settingsRecorder.Result().StatusCode)
	}
	settingsBody, err := io.ReadAll(settingsRecorder.Result().Body)
	if err != nil {
		t.Fatalf("read settings body: %v", err)
	}
	settingsText := string(settingsBody)
	if !strings.Contains(settingsText, `id="cfgRouterStrategy"`) ||
		!strings.Contains(settingsText, `id="applyCodexBridgePreset"`) ||
		!strings.Contains(settingsText, `id="settingsShell"`) ||
		!strings.Contains(settingsText, `id="settingsNav"`) ||
		!strings.Contains(settingsText, `id="settingsBridgeRuleCount"`) ||
		!strings.Contains(settingsText, `id="settingsDraftState"`) ||
		!strings.Contains(settingsText, `id="settingsVisibleSections"`) ||
		!strings.Contains(settingsText, `id="settingsIssueCount"`) ||
		!strings.Contains(settingsText, `id="settingsProviderRoster"`) ||
		!strings.Contains(settingsText, `id="settingsBridgeRoster"`) ||
		!strings.Contains(settingsText, `id="settingsDiagnostics"`) ||
		!strings.Contains(settingsText, `id="cfgHealthMeta"`) ||
		!strings.Contains(settingsText, `id="cfgBridgeMeta"`) ||
		!strings.Contains(settingsText, `id="cfgRouterMeta"`) ||
		!strings.Contains(settingsText, `id="cfgInterceptMeta"`) ||
		!strings.Contains(settingsText, `id="cfgUpstreamsMeta"`) ||
		!strings.Contains(settingsText, `Configuration Center`) ||
		!strings.Contains(settingsText, `Runtime Routing, Health, Providers.`) ||
		!strings.Contains(settingsText, `Config Directory`) ||
		!strings.Contains(settingsText, `Per-provider probe`) ||
		!strings.Contains(settingsText, `Diff and rollback ready`) ||
		!strings.Contains(settingsText, `href="#cfg-upstreams"`) ||
		!strings.Contains(settingsText, `data-nav-target="cfg-upstreams"`) ||
		!strings.Contains(settingsText, `id="navMetaProviders"`) ||
		!strings.Contains(settingsText, `provider-summary-strip`) ||
		!strings.Contains(settingsText, `Status`) ||
		!strings.Contains(settingsText, `Models`) ||
		!strings.Contains(settingsText, `Auth`) ||
		!strings.Contains(settingsText, `Probe`) ||
		!strings.Contains(settingsText, `.page-settings .hero`) ||
		!strings.Contains(settingsText, `grid-template-columns: 1fr;`) ||
		!strings.Contains(settingsText, `.page-settings #heroStats`) ||
		!strings.Contains(settingsText, `display: none;`) {
		t.Fatalf("expected upgraded settings controls, got %q", settingsText)
	}

	faviconReq := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	faviconRecorder := httptest.NewRecorder()
	handler.ServeHTTP(faviconRecorder, faviconReq)
	if faviconRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected favicon 200, got %d", faviconRecorder.Result().StatusCode)
	}
	if got := faviconRecorder.Result().Header.Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
		t.Fatalf("expected svg content type, got %q", got)
	}
	body, _ := io.ReadAll(faviconRecorder.Result().Body)
	if !strings.Contains(string(body), "<svg") {
		t.Fatalf("expected svg body, got %q", string(body))
	}
}

func TestAdminUpstreamProbe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-demo" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Fatalf("unexpected custom header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"gpt-5.4"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Admin:  config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{Enabled: true, Path: "/v1/models", TimeoutMs: 2000},
	}
	cfg.Normalize()

	handler := NewRouter(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	body := bytes.NewBufferString(`{"upstream":{"name":"probe","base_url":"` + upstream.URL + `","api_key":"sk-demo","headers":{"X-Test":"yes"},"timeout_ms":2000,"enabled":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/-/admin/upstreams/test", body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected 200, got %d: %s", recorder.Result().StatusCode, string(data))
	}

	var payload struct {
		OK          bool   `json:"ok"`
		StatusCode  int    `json:"status_code"`
		TargetURL   string `json:"target_url"`
		BodyPreview string `json:"body_preview"`
	}
	if err := json.NewDecoder(recorder.Result().Body).Decode(&payload); err != nil {
		t.Fatalf("decode probe payload: %v", err)
	}
	if !payload.OK {
		t.Fatalf("expected probe ok, got false")
	}
	if payload.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", payload.StatusCode)
	}
	if !strings.HasSuffix(payload.TargetURL, "/v1/models") {
		t.Fatalf("unexpected target url %q", payload.TargetURL)
	}
	if !strings.Contains(payload.BodyPreview, `"object":"list"`) {
		t.Fatalf("unexpected body preview %q", payload.BodyPreview)
	}
}

func TestAdminConfigUpdatesUpstreams(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
		},
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
			ExcludeUserAgents: []string{"OpenAI-Python/*"},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-old", Models: []string{"gpt-5.2"}, Weight: 1, TimeoutMs: 30000},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	payload := map[string]any{
		"health": map[string]any{
			"enabled":      false,
			"interval_sec": 30,
			"timeout_ms":   5000,
			"path":         "/healthz",
		},
		"bridge": map[string]any{
			"enabled":             true,
			"exclude_user_agents": []string{"curl/*", "Claude-Code/*"},
			"rules": []map[string]any{
				{
					"from": "gpt-5.2",
					"to":   "gpt-5.4",
				},
				{
					"from": "gpt-5.2-codex",
					"to":   "gpt-5.4",
				},
			},
		},
		"router": map[string]any{
			"strategy":                      "round_robin",
			"max_retries":                   2,
			"retry_backoff_ms":              3000,
			"retry_backoff_max_ms":          30000,
			"failure_threshold":             20,
			"cooldown_sec":                  60,
			"failure_passthrough_after_sec": 600,
		},
		"proxy": map[string]any{
			"retry": map[string]any{
				"status_codes":     []int{408, 429},
				"status_code_min":  500,
				"message_keywords": []string{"rate limit"},
			},
			"intercepts": []any{},
		},
		"upstreams": []map[string]any{
			{
				"name":                  "provider-a",
				"base_url":              "https://provider-a.example.com",
				"api_key":               "sk-new",
				"models":                []string{"gpt-5.2", "gpt-5.2-codex"},
				"weight":                3,
				"timeout_ms":            45000,
				"same_upstream_retries": 2,
				"enabled":               true,
				"headers": map[string]string{
					"X-Org": "demo",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected 200, got %d: %s", recorder.Result().StatusCode, string(data))
	}

	updated := store.Get()
	if len(updated.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(updated.Upstreams))
	}
	if updated.Upstreams[0].Name != "provider-a" {
		t.Fatalf("expected updated upstream name, got %q", updated.Upstreams[0].Name)
	}
	if updated.Upstreams[0].APIKey != "sk-new" {
		t.Fatalf("expected updated api key, got %q", updated.Upstreams[0].APIKey)
	}
	if updated.Upstreams[0].BaseURL != "https://provider-a.example.com" {
		t.Fatalf("expected updated base url, got %q", updated.Upstreams[0].BaseURL)
	}
	if got := updated.Upstreams[0].Headers["X-Org"]; got != "demo" {
		t.Fatalf("expected header X-Org=demo, got %q", got)
	}
	if updated.Upstreams[0].SameUpstreamRetries != 2 {
		t.Fatalf("expected same upstream retries 2, got %d", updated.Upstreams[0].SameUpstreamRetries)
	}
	if updated.Router.Strategy != "round_robin" {
		t.Fatalf("expected router strategy round_robin, got %q", updated.Router.Strategy)
	}
	if updated.Health.Enabled {
		t.Fatalf("expected health to be disabled")
	}
	if updated.Health.Path != "/healthz" {
		t.Fatalf("expected health path to be updated, got %q", updated.Health.Path)
	}
	if !updated.Bridge.Enabled {
		t.Fatalf("expected bridge to stay enabled")
	}
	if len(updated.Bridge.Rules) != 2 {
		t.Fatalf("expected 2 bridge rules, got %d", len(updated.Bridge.Rules))
	}
	if got := updated.Bridge.ExcludeUserAgents[0]; got != "curl/*" {
		t.Fatalf("expected updated bridge exclude UA, got %q", got)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "provider-a") || !strings.Contains(text, "sk-new") {
		t.Fatalf("expected saved config to contain updated upstream, got %q", text)
	}
	if !strings.Contains(text, "/healthz") || !strings.Contains(text, "gpt-5.2-codex") {
		t.Fatalf("expected saved config to contain updated health/bridge settings, got %q", text)
	}
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("expected backup config to be created: %v", err)
	}
}

func TestAdminConfigExportReturnsYAML(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
		},
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-export", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	req := httptest.NewRequest(http.MethodGet, "/-/admin/config/export", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Result().StatusCode)
	}
	if got := recorder.Result().Header.Get("Content-Type"); !strings.Contains(got, "application/yaml") {
		t.Fatalf("expected yaml content type, got %q", got)
	}

	data, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read export body: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "bridge:") || !strings.Contains(text, "upstreams:") || !strings.Contains(text, "sk-export") {
		t.Fatalf("expected export yaml body, got %q", text)
	}
}

func TestAdminConfigRollbackRestoresPreviousVersion(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
		},
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-old", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	payload := map[string]any{
		"health": map[string]any{
			"enabled":      false,
			"interval_sec": 30,
			"timeout_ms":   5000,
			"path":         "/healthz",
		},
		"bridge": map[string]any{
			"enabled":             true,
			"exclude_user_agents": []string{"curl/*"},
			"rules": []map[string]any{
				{
					"from": "gpt-5.2-codex",
					"to":   "gpt-5.4",
				},
			},
		},
		"router": map[string]any{
			"max_retries":                   cfg.Router.MaxRetries,
			"retry_backoff_ms":              cfg.Router.RetryBackoffMs,
			"retry_backoff_max_ms":          cfg.Router.RetryBackoffMaxMs,
			"failure_threshold":             cfg.Router.FailureThreshold,
			"cooldown_sec":                  cfg.Router.CooldownSec,
			"failure_passthrough_after_sec": cfg.Router.FailurePassthroughAfterSec,
		},
		"proxy": map[string]any{
			"retry": map[string]any{
				"status_codes":     cfg.Proxy.Retry.StatusCodes,
				"status_code_min":  *cfg.Proxy.Retry.StatusCodeMin,
				"message_keywords": cfg.Proxy.Retry.MessageKeywords,
			},
			"intercepts": []any{},
		},
		"upstreams": []map[string]any{
			{
				"name":       "provider-a",
				"base_url":   "https://provider-a.example.com",
				"api_key":    "sk-new",
				"models":     []string{"gpt-5.2-codex"},
				"weight":     3,
				"timeout_ms": 45000,
				"enabled":    true,
				"headers":    map[string]string{"X-Org": "demo"},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	saveReq := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewReader(body))
	saveReq.Header.Set("Authorization", "Bearer secret")
	saveReq.Header.Set("Content-Type", "application/json")
	saveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(saveRecorder, saveReq)
	if saveRecorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(saveRecorder.Result().Body)
		t.Fatalf("expected save 200, got %d: %s", saveRecorder.Result().StatusCode, string(data))
	}

	rollbackReq := httptest.NewRequest(http.MethodPost, "/-/admin/config/rollback", nil)
	rollbackReq.Header.Set("Authorization", "Bearer secret")
	rollbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rollbackRecorder, rollbackReq)

	if rollbackRecorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(rollbackRecorder.Result().Body)
		t.Fatalf("expected rollback 200, got %d: %s", rollbackRecorder.Result().StatusCode, string(data))
	}

	restored := store.Get()
	if restored.Upstreams[0].Name != "alpha" {
		t.Fatalf("expected upstream to roll back to alpha, got %q", restored.Upstreams[0].Name)
	}
	if restored.Upstreams[0].APIKey != "sk-old" {
		t.Fatalf("expected api key to roll back, got %q", restored.Upstreams[0].APIKey)
	}
	if restored.Health.Path != "/v1/models" {
		t.Fatalf("expected health path to roll back, got %q", restored.Health.Path)
	}
	if len(restored.Bridge.Rules) != 1 || restored.Bridge.Rules[0].From != "gpt-5.2" {
		t.Fatalf("expected bridge rules to roll back, got %#v", restored.Bridge.Rules)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "alpha") || !strings.Contains(text, "sk-old") {
		t.Fatalf("expected active config to be restored, got %q", text)
	}

	backupData, err := os.ReadFile(configPath + ".bak")
	if err != nil {
		t.Fatalf("read backup config: %v", err)
	}
	backupText := string(backupData)
	if !strings.Contains(backupText, "provider-a") || !strings.Contains(backupText, "sk-new") {
		t.Fatalf("expected backup config to keep rolled-back version, got %q", backupText)
	}
}

func TestAdminConfigHistoryListsSavedVersions(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-old", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	for _, name := range []string{"provider-a", "provider-b"} {
		payload := map[string]any{
			"health": map[string]any{
				"enabled":      cfg.Health.Enabled,
				"interval_sec": cfg.Health.IntervalSec,
				"timeout_ms":   cfg.Health.TimeoutMs,
				"path":         cfg.Health.Path,
			},
			"bridge": map[string]any{
				"enabled":             cfg.Bridge.Enabled,
				"exclude_user_agents": cfg.Bridge.ExcludeUserAgents,
				"rules":               []map[string]any{},
			},
			"router": map[string]any{
				"max_retries":                   cfg.Router.MaxRetries,
				"retry_backoff_ms":              cfg.Router.RetryBackoffMs,
				"retry_backoff_max_ms":          cfg.Router.RetryBackoffMaxMs,
				"failure_threshold":             cfg.Router.FailureThreshold,
				"cooldown_sec":                  cfg.Router.CooldownSec,
				"failure_passthrough_after_sec": cfg.Router.FailurePassthroughAfterSec,
			},
			"proxy": map[string]any{
				"retry": map[string]any{
					"status_codes":     cfg.Proxy.Retry.StatusCodes,
					"status_code_min":  *cfg.Proxy.Retry.StatusCodeMin,
					"message_keywords": cfg.Proxy.Retry.MessageKeywords,
				},
				"intercepts": []any{},
			},
			"upstreams": []map[string]any{
				{
					"name":       name,
					"base_url":   "https://" + name + ".example.com",
					"api_key":    "sk-" + name,
					"models":     []string{"gpt-5.2"},
					"weight":     1,
					"timeout_ms": 30000,
					"enabled":    true,
					"headers":    map[string]string{},
				},
			},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected save 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
		time.Sleep(5 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodGet, "/-/admin/config/history", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Result().StatusCode)
	}

	var payload struct {
		Versions []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(recorder.Result().Body).Decode(&payload); err != nil {
		t.Fatalf("decode history payload: %v", err)
	}
	if len(payload.Versions) < 2 {
		t.Fatalf("expected at least 2 history versions, got %d", len(payload.Versions))
	}
	if payload.Versions[0].ID == "" || payload.Versions[0].Filename == "" {
		t.Fatalf("expected non-empty history metadata, got %#v", payload.Versions[0])
	}
}

func TestAdminConfigHistoryDiffReturnsLineChanges(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-alpha", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	payload := map[string]any{
		"health": map[string]any{
			"enabled":      cfg.Health.Enabled,
			"interval_sec": cfg.Health.IntervalSec,
			"timeout_ms":   cfg.Health.TimeoutMs,
			"path":         cfg.Health.Path,
		},
		"bridge": map[string]any{
			"enabled":             cfg.Bridge.Enabled,
			"exclude_user_agents": cfg.Bridge.ExcludeUserAgents,
			"rules":               []map[string]any{},
		},
		"router": map[string]any{
			"max_retries":                   cfg.Router.MaxRetries,
			"retry_backoff_ms":              cfg.Router.RetryBackoffMs,
			"retry_backoff_max_ms":          cfg.Router.RetryBackoffMaxMs,
			"failure_threshold":             cfg.Router.FailureThreshold,
			"cooldown_sec":                  cfg.Router.CooldownSec,
			"failure_passthrough_after_sec": cfg.Router.FailurePassthroughAfterSec,
		},
		"proxy": map[string]any{
			"retry": map[string]any{
				"status_codes":     cfg.Proxy.Retry.StatusCodes,
				"status_code_min":  *cfg.Proxy.Retry.StatusCodeMin,
				"message_keywords": cfg.Proxy.Retry.MessageKeywords,
			},
			"intercepts": []any{},
		},
		"upstreams": []map[string]any{
			{
				"name":       "provider-a",
				"base_url":   "https://provider-a.example.com",
				"api_key":    "sk-provider-a",
				"models":     []string{"gpt-5.2"},
				"weight":     1,
				"timeout_ms": 30000,
				"enabled":    true,
				"headers":    map[string]string{},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	saveReq := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewReader(body))
	saveReq.Header.Set("Authorization", "Bearer secret")
	saveReq.Header.Set("Content-Type", "application/json")
	saveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(saveRecorder, saveReq)
	if saveRecorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(saveRecorder.Result().Body)
		t.Fatalf("expected save 200, got %d: %s", saveRecorder.Result().StatusCode, string(data))
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/-/admin/config/history", nil)
	historyReq.Header.Set("Authorization", "Bearer secret")
	historyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(historyRecorder, historyReq)
	if historyRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected history 200, got %d", historyRecorder.Result().StatusCode)
	}

	var history struct {
		Versions []struct {
			ID string `json:"id"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(historyRecorder.Result().Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Versions) == 0 {
		t.Fatalf("expected at least 1 history version")
	}

	diffReq := httptest.NewRequest(http.MethodGet, "/-/admin/config/history/"+history.Versions[0].ID+"/diff", nil)
	diffReq.Header.Set("Authorization", "Bearer secret")
	diffRecorder := httptest.NewRecorder()
	handler.ServeHTTP(diffRecorder, diffReq)
	if diffRecorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(diffRecorder.Result().Body)
		t.Fatalf("expected diff 200, got %d: %s", diffRecorder.Result().StatusCode, string(data))
	}

	var diff struct {
		Summary struct {
			AddedLines   int `json:"added_lines"`
			RemovedLines int `json:"removed_lines"`
		} `json:"summary"`
		Lines []struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	if err := json.NewDecoder(diffRecorder.Result().Body).Decode(&diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary.AddedLines == 0 || diff.Summary.RemovedLines == 0 {
		t.Fatalf("expected diff summary to report adds and removes, got %#v", diff.Summary)
	}

	var foundAdded, foundRemoved bool
	for _, line := range diff.Lines {
		if line.Kind == "add" && strings.Contains(line.Text, "provider-a") {
			foundAdded = true
		}
		if line.Kind == "remove" && strings.Contains(line.Text, "alpha") {
			foundRemoved = true
		}
	}
	if !foundAdded || !foundRemoved {
		t.Fatalf("expected diff lines to include alpha->provider-a change, got %#v", diff.Lines)
	}
}

func TestAdminConfigRollbackSpecificVersion(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", APIKey: "sk-alpha", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := NewRouter(router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	saveVersion := func(name string) {
		payload := map[string]any{
			"health": map[string]any{
				"enabled":      cfg.Health.Enabled,
				"interval_sec": cfg.Health.IntervalSec,
				"timeout_ms":   cfg.Health.TimeoutMs,
				"path":         cfg.Health.Path,
			},
			"bridge": map[string]any{
				"enabled":             cfg.Bridge.Enabled,
				"exclude_user_agents": cfg.Bridge.ExcludeUserAgents,
				"rules":               []map[string]any{},
			},
			"router": map[string]any{
				"max_retries":                   cfg.Router.MaxRetries,
				"retry_backoff_ms":              cfg.Router.RetryBackoffMs,
				"retry_backoff_max_ms":          cfg.Router.RetryBackoffMaxMs,
				"failure_threshold":             cfg.Router.FailureThreshold,
				"cooldown_sec":                  cfg.Router.CooldownSec,
				"failure_passthrough_after_sec": cfg.Router.FailurePassthroughAfterSec,
			},
			"proxy": map[string]any{
				"retry": map[string]any{
					"status_codes":     cfg.Proxy.Retry.StatusCodes,
					"status_code_min":  *cfg.Proxy.Retry.StatusCodeMin,
					"message_keywords": cfg.Proxy.Retry.MessageKeywords,
				},
				"intercepts": []any{},
			},
			"upstreams": []map[string]any{
				{
					"name":       name,
					"base_url":   "https://" + name + ".example.com",
					"api_key":    "sk-" + name,
					"models":     []string{"gpt-5.2"},
					"weight":     1,
					"timeout_ms": 30000,
					"enabled":    true,
					"headers":    map[string]string{},
				},
			},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected save 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
		time.Sleep(5 * time.Millisecond)
	}

	saveVersion("provider-a")
	saveVersion("provider-b")

	historyReq := httptest.NewRequest(http.MethodGet, "/-/admin/config/history", nil)
	historyReq.Header.Set("Authorization", "Bearer secret")
	historyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(historyRecorder, historyReq)
	if historyRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected history 200, got %d", historyRecorder.Result().StatusCode)
	}

	var history struct {
		Versions []struct {
			ID string `json:"id"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(historyRecorder.Result().Body).Decode(&history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Versions) < 2 {
		t.Fatalf("expected at least 2 history versions, got %d", len(history.Versions))
	}

	targetVersionID := history.Versions[1].ID
	body, err := json.Marshal(map[string]any{"version_id": targetVersionID})
	if err != nil {
		t.Fatalf("marshal rollback body: %v", err)
	}
	rollbackReq := httptest.NewRequest(http.MethodPost, "/-/admin/config/rollback", bytes.NewReader(body))
	rollbackReq.Header.Set("Authorization", "Bearer secret")
	rollbackReq.Header.Set("Content-Type", "application/json")
	rollbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rollbackRecorder, rollbackReq)

	if rollbackRecorder.Result().StatusCode != http.StatusOK {
		data, _ := io.ReadAll(rollbackRecorder.Result().Body)
		t.Fatalf("expected rollback 200, got %d: %s", rollbackRecorder.Result().StatusCode, string(data))
	}

	restored := store.Get()
	if restored.Upstreams[0].Name != "alpha" {
		t.Fatalf("expected rollback to older alpha version, got %q", restored.Upstreams[0].Name)
	}
}
