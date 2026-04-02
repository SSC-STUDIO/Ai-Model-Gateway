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
	"ai-model-gateway/internal/proxy"
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

func newTestRouter(t *testing.T, manager *router.Manager, stats *telemetry.Store, pricingCatalog *telemetry.PricingCatalog) http.Handler {
	t.Helper()

	originalProxyHandlerFactory := newProxyHandler
	originalSSRFChecker := ssrfChecker

	newProxyHandler = func(manager *router.Manager, stats *telemetry.Store) *proxy.Handler {
		return proxy.NewHandlerWithSSRFChecker(manager, stats, nil)
	}
	ssrfChecker = nil

	t.Cleanup(func() {
		newProxyHandler = originalProxyHandlerFactory
		ssrfChecker = originalSSRFChecker
	})

	return NewRouter(manager, stats, pricingCatalog)
}

func TestHealthIncludesRequestID(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "health_weighted_rr"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
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

func TestModelsEndpointReturnsOpenAICompatibleModelObjects(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "kimi", BaseURL: "https://api.moonshot.cn", Models: []string{"kimi-k2.5"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Object != "list" {
		t.Fatalf("expected list object, got %q", payload.Object)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("expected one model, got %d", len(payload.Data))
	}
	model := payload.Data[0]
	if model.ID != "kimi-k2.5" {
		t.Fatalf("expected kimi-k2.5 model id, got %q", model.ID)
	}
	if model.Object != "model" {
		t.Fatalf("expected model object type, got %q", model.Object)
	}
	if model.Created <= 0 {
		t.Fatalf("expected created timestamp, got %d", model.Created)
	}
	if model.OwnedBy != "ai-model-gateway" {
		t.Fatalf("expected owned_by ai-model-gateway, got %q", model.OwnedBy)
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
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

func TestMessagesRouteProxiesAnthropicHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-anthropic" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "prompt-caching-2024-07-31" {
			t.Fatalf("expected anthropic-beta header to pass through, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "claude-opus-4-6" {
			t.Fatalf("expected anthropic model to stay opus, got %#v", payload["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_123","type":"message","model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":12,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "anthropic", BaseURL: upstream.URL, APIKey: "sk-anthropic", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":64,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Set(observability.RequestIDHeader, "req-messages")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "anthropic" {
		t.Fatalf("expected upstream header anthropic, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(body), `"type":"message"`) {
		t.Fatalf("expected anthropic message payload, got %q", string(body))
	}
}

func TestMessagesCountTokensRouteProxiesAnthropicHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-anthropic" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "claude-sonnet-4-6" {
			t.Fatalf("expected compat probe model rewrite to sonnet, got %#v", payload["model"])
		}
		if payload["max_tokens"] != float64(1) {
			t.Fatalf("expected compat probe max_tokens=1, got %#v", payload["max_tokens"])
		}
		if payload["stream"] != false {
			t.Fatalf("expected compat probe stream=false, got %#v", payload["stream"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_count","type":"message","model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":7,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "anthropic", BaseURL: upstream.URL, APIKey: "sk-anthropic", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus-4-6","system":"test","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(observability.RequestIDHeader, "req-count-tokens")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "anthropic" {
		t.Fatalf("expected upstream header anthropic, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.TrimSpace(string(body)) != `{"input_tokens":7}` {
		t.Fatalf("expected token count response, got %q", string(body))
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), stats, telemetry.NewPricingCatalog(cfg.Pricing))
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
	runtime, ok := body["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("expected runtime object")
	}
	if runtime["router_strategy"] != "round_robin" {
		t.Fatalf("expected runtime router strategy round_robin, got %#v", runtime["router_strategy"])
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != `Bearer realm="aigw-admin"` {
		t.Fatalf("expected bearer challenge, got %q", got)
	}
}

func TestAdminAcceptsQueryToken(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/admin?token=secret", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected query token to be accepted with 200, got %d", recorder.Result().StatusCode)
	}
}

func TestAdminConfigViewRoundTripsEnabledFlag(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: false, AuthToken: "secret", Language: config.AdminLanguageEnglish},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Weight: 1},
		},
	}
	cfg.Normalize()

	view := renderConfigView(cfg)
	if view.Admin.Enabled != nil && *view.Admin.Enabled {
		t.Fatal("expected rendered admin.enabled to stay false")
	}

	updated := cfg
	updated.Admin = applyAdminConfig(cfg.Admin, view.Admin)
	if updated.Admin.Enabled {
		t.Fatal("expected applied admin.enabled to stay false")
	}
}

func TestAdminAcceptsCookieToken(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: adminAuthCookie, Value: "secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected cookie auth 200, got %d: %s", recorder.Result().StatusCode, string(body))
	}
}

func TestAdminCookieSettingsRouteSucceedsSameOrigin(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.Host = "127.0.0.1:18080"
	req.AddCookie(&http.Cookie{Name: adminAuthCookie, Value: "secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected cookie settings 200, got %d: %s", recorder.Result().StatusCode, string(body))
	}
}

func TestAdminCookieDataRouteSucceeds(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Admin:  config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/-/admin/data", nil)
	req.AddCookie(&http.Cookie{Name: adminAuthCookie, Value: "secret"})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected cookie data 200, got %d: %s", recorder.Result().StatusCode, string(body))
	}
}

func TestAdminOverviewRouteTrimsDuplicateOverviewNavigation(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
	if !strings.Contains(text, `Runtime Posture`) ||
		!strings.Contains(text, `surface-card`) ||
		!strings.Contains(text, `data-topnav-target="performance"`) ||
		!strings.Contains(text, `data-topnav-target="economics"`) ||
		!strings.Contains(text, `href="/admin/settings"`) {
		t.Fatalf("expected streamlined overview markers, got %q", text)
	}
	if strings.Contains(text, `id="overviewPulse"`) ||
		strings.Contains(text, `id="overviewQuickNav"`) ||
		strings.Contains(text, `data-overview-target="runtime-card"`) ||
		strings.Contains(text, `Jump to Surface`) ||
		strings.Contains(text, `id="openSettings"`) ||
		strings.Contains(text, `Request Load`) ||
		strings.Contains(text, `Pricing Coverage`) ||
		strings.Contains(text, `id="bridgeState"`) ||
		strings.Contains(text, `id="runtimeMeta"`) ||
		strings.Contains(text, `id="overviewAlerts"`) ||
		strings.Contains(text, `id="heroStats"`) ||
		strings.Contains(text, `data-topnav-target="runtime-card"`) ||
		strings.Contains(text, `Health Watch`) ||
		strings.Contains(text, `Error Pressure`) {
		t.Fatalf("expected duplicate overview navigation removed, got %q", text)
	}
}

func TestAdminSettingsAndFaviconRoutes(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
		!strings.Contains(settingsText, `id="retryModePresetBounded"`) ||
		!strings.Contains(settingsText, `id="retryModePresetInfinite"`) ||
		!strings.Contains(settingsText, `id="cfgMaxRetriesHint"`) ||
		!strings.Contains(settingsText, `id="cfgPassthroughHint"`) ||
		!strings.Contains(settingsText, `id="settingsShell"`) ||
		!strings.Contains(settingsText, `id="settingsNav"`) ||
		!strings.Contains(settingsText, `id="providerClassFilter"`) ||
		!strings.Contains(settingsText, `id="cfgHealthMeta"`) ||
		!strings.Contains(settingsText, `id="cfgBridgeMeta"`) ||
		!strings.Contains(settingsText, `id="cfgRouterMeta"`) ||
		!strings.Contains(settingsText, `id="cfgInterceptMeta"`) ||
		!strings.Contains(settingsText, `id="cfgUpstreamsMeta"`) ||
		!strings.Contains(settingsText, `Configuration Center`) ||
		!strings.Contains(settingsText, `Runtime Routing, Health, Providers.`) ||
		!strings.Contains(settingsText, `Sections`) ||
		!strings.Contains(settingsText, `Free First`) ||
		!strings.Contains(settingsText, `Quota-Limited`) ||
		!strings.Contains(settingsText, `href="#cfg-upstreams"`) ||
		!strings.Contains(settingsText, `data-nav-target="cfg-upstreams"`) ||
		!strings.Contains(settingsText, `id="navMetaProviders"`) ||
		!strings.Contains(settingsText, `settings-rail-actions`) ||
		!strings.Contains(settingsText, `id="saveConfig"`) ||
		!strings.Contains(settingsText, `id="rollbackConfig"`) ||
		!strings.Contains(settingsText, `provider-summary-strip`) ||
		!strings.Contains(settingsText, `Status`) ||
		!strings.Contains(settingsText, `Models`) ||
		!strings.Contains(settingsText, `Auth`) ||
		!strings.Contains(settingsText, `Probe`) ||
		!strings.Contains(settingsText, `Provider Class`) ||
		!strings.Contains(settingsText, `.page-settings .hero`) ||
		!strings.Contains(settingsText, `grid-template-columns: 1fr;`) ||
		!strings.Contains(settingsText, `display: none;`) {
		t.Fatalf("expected upgraded settings controls, got %q", settingsText)
	}
	if strings.Contains(settingsText, `id="settingsBridgeRuleCount"`) ||
		strings.Contains(settingsText, `id="settingsDraftState"`) ||
		strings.Contains(settingsText, `id="settingsVisibleSections"`) ||
		strings.Contains(settingsText, `id="settingsIssueCount"`) ||
		strings.Contains(settingsText, `id="settingsProviderRoster"`) ||
		strings.Contains(settingsText, `id="settingsBridgeRoster"`) ||
		strings.Contains(settingsText, `id="settingsDiagnostics"`) ||
		strings.Contains(settingsText, `Per-provider probe`) ||
		strings.Contains(settingsText, `Diff and rollback ready`) ||
		strings.Contains(settingsText, `Runtime config surface`) ||
		strings.Contains(settingsText, `Config Directory`) ||
		strings.Contains(settingsText, `Surface Controls`) ||
		strings.Contains(settingsText, `Config History`) ||
		strings.Contains(settingsText, `Search config sections, fields, providers...`) ||
		strings.Contains(settingsText, `先搜索区块，再按 provider class 过滤免费或额度上游；保存、导出和回滚仍在底部固定操作区。`) ||
		strings.Contains(settingsText, `保存配置前会自动归档旧版本，可选择具体版本回滚`) ||
		strings.Contains(settingsText, `class="config-footer"`) ||
		strings.Contains(settingsText, `编辑 health、bridge、重试、拦截和上游服务商配置，支持导出当前配置与回滚上一个版本。`) ||
		strings.Contains(settingsText, `<a href="#cfg-health">Health</a>`) ||
		strings.Contains(settingsText, `<a href="#cfg-bridge">Bridge</a>`) ||
		strings.Contains(settingsText, `<a href="#cfg-router">Router</a>`) ||
		strings.Contains(settingsText, `<a href="#cfg-upstreams">Providers</a>`) ||
		strings.Contains(settingsText, `<a href="#cfg-history">History</a>`) ||
		strings.Contains(settingsText, `在一个页面里维护探活、桥接、重试、拦截和上游服务商。先做 probe，再保存；先看 diff，再回滚。`) {
		t.Fatalf("expected duplicate settings summary surfaces to be removed, got %q", settingsText)
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

func TestAdminRoutesRenderChineseLocale(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageChinese},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	overviewReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	overviewReq.Header.Set("Authorization", "Bearer secret")
	overviewRecorder := httptest.NewRecorder()
	handler.ServeHTTP(overviewRecorder, overviewReq)
	if overviewRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected overview 200, got %d", overviewRecorder.Result().StatusCode)
	}
	overviewBody, err := io.ReadAll(overviewRecorder.Result().Body)
	if err != nil {
		t.Fatalf("read overview body: %v", err)
	}
	overviewText := string(overviewBody)
	if !strings.Contains(overviewText, `AI 模型网关管理台`) ||
		!strings.Contains(overviewText, `运维、成本、吞吐。`) ||
		!strings.Contains(overviewText, `let currentLocale = "zh";`) {
		t.Fatalf("expected chinese overview bootstrap markers, got %q", overviewText)
	}

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
	if !strings.Contains(settingsText, `配置中心`) ||
		!strings.Contains(settingsText, `运行路由、探活、服务商。`) ||
		!strings.Contains(settingsText, `AI 模型网关设置`) ||
		!strings.Contains(settingsText, `let currentLocale = "zh";`) ||
		!strings.Contains(settingsText, `id="cfgAdminLanguage"`) {
		t.Fatalf("expected chinese settings bootstrap markers, got %q", settingsText)
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

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
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
	if strings.Contains(payload.BodyPreview, "sk-demo") {
		t.Fatalf("expected body preview to redact secrets, got %q", payload.BodyPreview)
	}
}

func TestAdminCookieMutationRequiresSameOrigin(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	cookie := &http.Cookie{Name: adminAuthCookie, Value: "secret"}

	t.Run("save same origin with origin succeeds", func(t *testing.T) {
		payload := `{"health":{"enabled":true,"interval_sec":10,"timeout_ms":2000,"path":"/v1/models"},"bridge":{"enabled":false,"exclude_user_agents":[],"rules":[]},"router":{"strategy":"round_robin","max_retries":1,"retry_backoff_ms":1000,"retry_backoff_max_ms":5000,"failure_threshold":3,"cooldown_sec":30,"failure_passthrough_after_sec":300},"proxy":{"retry":{"infinite_on_error":false,"status_codes":[408,429],"status_code_min":500,"message_keywords":[]},"intercepts":[]},"upstreams":[{"name":"cookie-provider","base_url":"https://cookie.example.com","api_key":"sk-cookie","models":["gpt-5.2"],"weight":1,"timeout_ms":30000,"enabled":true,"headers":{}}]}`
		req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewBufferString(payload))
		req.Host = "127.0.0.1:18080"
		req.Header.Set("Origin", "http://127.0.0.1:18080")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected save 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})

	t.Run("save cross origin denied", func(t *testing.T) {
		payload := `{"health":{"enabled":true,"interval_sec":10,"timeout_ms":2000,"path":"/v1/models"},"bridge":{"enabled":false,"exclude_user_agents":[],"rules":[]},"router":{"strategy":"round_robin","max_retries":1,"retry_backoff_ms":1000,"retry_backoff_max_ms":5000,"failure_threshold":3,"cooldown_sec":30,"failure_passthrough_after_sec":300},"proxy":{"retry":{"infinite_on_error":false,"status_codes":[408,429],"status_code_min":500,"message_keywords":[]},"intercepts":[]},"upstreams":[{"name":"cookie-provider","base_url":"https://cookie.example.com","api_key":"sk-cookie","models":["gpt-5.2"],"weight":1,"timeout_ms":30000,"enabled":true,"headers":{}}]}`
		req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewBufferString(payload))
		req.Host = "127.0.0.1:18080"
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusForbidden {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected save 403, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})

	t.Run("rollback same origin referer succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/-/admin/config/rollback", nil)
		req.Host = "127.0.0.1:18080"
		req.Header.Set("Referer", "http://127.0.0.1:18080/admin/settings")
		req.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected rollback 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})

	t.Run("rollback without origin and referer denied", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/-/admin/config/rollback", nil)
		req.Host = "127.0.0.1:18080"
		req.AddCookie(cookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusForbidden {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected rollback 403, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})
}

func TestAdminBearerMutationIgnoresOriginChecks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"gpt-5.4"}]}`)
	}))
	defer upstream.Close()

	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Health: config.HealthConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	t.Run("save without origin succeeds", func(t *testing.T) {
		payload := `{"health":{"enabled":true,"interval_sec":10,"timeout_ms":2000,"path":"/v1/models"},"bridge":{"enabled":false,"exclude_user_agents":[],"rules":[]},"router":{"strategy":"round_robin","max_retries":1,"retry_backoff_ms":1000,"retry_backoff_max_ms":5000,"failure_threshold":3,"cooldown_sec":30,"failure_passthrough_after_sec":300},"proxy":{"retry":{"infinite_on_error":false,"status_codes":[408,429],"status_code_min":500,"message_keywords":[]},"intercepts":[]},"upstreams":[{"name":"bearer-provider","base_url":"https://bearer.example.com","api_key":"sk-bearer","models":["gpt-5.2"],"weight":1,"timeout_ms":30000,"enabled":true,"headers":{}}]}`
		req := httptest.NewRequest(http.MethodPut, "/-/admin/config", bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected save 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})

	t.Run("rollback cross origin still succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/-/admin/config/rollback", nil)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Origin", "https://evil.example")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected rollback 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})

	t.Run("probe cross origin still succeeds", func(t *testing.T) {
		body := bytes.NewBufferString(`{"upstream":{"name":"probe","base_url":"` + upstream.URL + `","api_key":"sk-demo","timeout_ms":2000,"enabled":true}}`)
		req := httptest.NewRequest(http.MethodPost, "/-/admin/upstreams/test", body)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Result().StatusCode != http.StatusOK {
			data, _ := io.ReadAll(recorder.Result().Body)
			t.Fatalf("expected probe 200, got %d: %s", recorder.Result().StatusCode, string(data))
		}
	})
}

func TestAdminConfigPutRoundTripsEnabledFlag(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Listen: ":8080",
		Router: config.RouterConfig{Strategy: config.RouterStrategyRoundRobin},
		Admin:  config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
		Health: config.HealthConfig{Enabled: true, IntervalSec: 10, TimeoutMs: 2000, Path: "/v1/models"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodPut, "/-/admin/config", strings.NewReader(`{"admin":{"enabled":false,"language":"en"},"health":{"enabled":true,"interval_sec":10,"timeout_ms":2000,"path":"/v1/models"},"bridge":{"enabled":false,"exclude_user_agents":[],"rules":[]},"router":{"strategy":"round_robin","max_retries":2,"retry_backoff_ms":250,"retry_backoff_max_ms":2000,"failure_threshold":2,"cooldown_sec":15,"failure_passthrough_after_sec":0},"proxy":{"retry":{"infinite_on_error":false,"status_codes":[408,429,500,502,503,504],"message_keywords":["timeout","temporarily unavailable"]},"intercepts":[]},"upstreams":[{"name":"alpha","base_url":"https://alpha.example.com","api_key":"","provider_class":"quota_limited","models":["gpt-5.2"],"weight":1,"timeout_ms":30000,"same_upstream_retries":0,"enabled":true,"headers":{}}]}`))
	req.Host = "127.0.0.1:18080"
	req.Header.Set("Origin", "http://127.0.0.1:18080")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: adminAuthCookie, Value: "secret"})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected config PUT 200, got %d: %s", recorder.Result().StatusCode, string(body))
	}

	var body AdminConfigView
	if err := json.NewDecoder(recorder.Result().Body).Decode(&body); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if body.Admin.Enabled != nil && *body.Admin.Enabled {
		t.Fatal("expected admin.enabled to remain false after PUT")
	}
	if store.Get().Admin.Enabled {
		t.Fatal("expected stored admin.enabled to remain false after PUT")
	}
}

func TestAdminConfigPutRejectsUnknownLanguageWithFullSupportedSet(t *testing.T) {
	configPath := t.TempDir() + "/config.yaml"
	cfg := config.Config{
		Listen: ":8080",
		Router: config.RouterConfig{Strategy: config.RouterStrategyRoundRobin},
		Admin:  config.AdminConfig{Enabled: true, AuthToken: "secret", Language: config.AdminLanguageEnglish},
		Health: config.HealthConfig{Enabled: true, IntervalSec: 10, TimeoutMs: 2000, Path: "/v1/models"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()
	if err := config.SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	store := state.NewConfigStoreWithPath(cfg, configPath)
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodPut, "/-/admin/config", strings.NewReader(`{"admin":{"enabled":true,"language":"it"},"health":{"enabled":true,"interval_sec":10,"timeout_ms":2000,"path":"/v1/models"},"bridge":{"enabled":false,"exclude_user_agents":[],"rules":[]},"router":{"strategy":"round_robin","max_retries":2,"retry_backoff_ms":250,"retry_backoff_max_ms":2000,"failure_threshold":2,"cooldown_sec":15,"failure_passthrough_after_sec":0},"proxy":{"retry":{"infinite_on_error":false,"status_codes":[408,429,500,502,503,504],"message_keywords":["timeout","temporarily unavailable"]},"intercepts":[]},"upstreams":[{"name":"alpha","base_url":"https://alpha.example.com","api_key":"","provider_class":"quota_limited","models":["gpt-5.2"],"weight":1,"timeout_ms":30000,"same_upstream_retries":0,"enabled":true,"headers":{}}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)
	if recorder.Result().StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected config PUT 400, got %d: %s", recorder.Result().StatusCode, string(body))
	}
	body, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read error body: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != config.AdminLanguageValidationMessage() {
		t.Fatalf("unexpected validation error %q", got)
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

	payload := map[string]any{
		"admin": map[string]any{
			"enabled":  true,
			"language": "en",
		},
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
				"infinite_on_error": true,
				"status_codes":      []int{408, 429},
				"status_code_min":   500,
				"message_keywords":  []string{"rate limit"},
			},
			"intercepts": []any{},
		},
		"upstreams": []map[string]any{
			{
				"name":                  "provider-a",
				"base_url":              "https://provider-a.example.com",
				"api_key":               "sk-new",
				"provider_class":        "free",
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
	if updated.Upstreams[0].ProviderClassNormalized() != config.UpstreamClassFree {
		t.Fatalf("expected upstream class free, got %q", updated.Upstreams[0].ProviderClassNormalized())
	}
	if got := updated.Upstreams[0].Headers["X-Org"]; got != "demo" {
		t.Fatalf("expected header X-Org=demo, got %q", got)
	}
	if updated.Upstreams[0].SameUpstreamRetries != 2 {
		t.Fatalf("expected same upstream retries 2, got %d", updated.Upstreams[0].SameUpstreamRetries)
	}
	if !updated.Admin.Enabled {
		t.Fatalf("expected admin to remain enabled")
	}
	if updated.Admin.Language != config.AdminLanguageEnglish {
		t.Fatalf("expected admin language en, got %q", updated.Admin.Language)
	}
	if updated.Router.Strategy != "round_robin" {
		t.Fatalf("expected router strategy round_robin, got %q", updated.Router.Strategy)
	}
	if !updated.Proxy.Retry.InfiniteOnError {
		t.Fatalf("expected infinite retry mode to be enabled")
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
	if !strings.Contains(text, "infinite_on_error: true") {
		t.Fatalf("expected saved config to persist infinite retry mode, got %q", text)
	}
	if !strings.Contains(text, "language: en") {
		t.Fatalf("expected saved config to persist admin language, got %q", text)
	}
	if !strings.Contains(text, "enabled: true") {
		t.Fatalf("expected saved config to persist admin enabled state, got %q", text)
	}
	if !strings.Contains(text, "/healthz") || !strings.Contains(text, "gpt-5.2-codex") {
		t.Fatalf("expected saved config to contain updated health/bridge settings, got %q", text)
	}
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("expected backup config to be created: %v", err)
	}
}

func TestAdminConfigReturnsRedactedSecrets(t *testing.T) {
	cfg := config.Config{
		Admin: config.AdminConfig{Enabled: true, AuthToken: "secret"},
		Upstreams: []config.Upstream{
			{
				Name:    "alpha",
				BaseURL: "https://alpha.example.com",
				APIKey:  "sk-visible",
				Models:  []string{"gpt-5.2"},
				Weight:  1,
				Headers: map[string]string{
					"Authorization":       "Bearer sk-header",
					"Proxy-Authorization": "Bearer sk-proxy",
					"X-API-Key":           "sk-x-api",
					"X-Org":               "demo",
				},
			},
		},
	}
	cfg.Normalize()

	handler := newTestRouter(t, router.NewManager(state.NewConfigStore(cfg)), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))
	req := httptest.NewRequest(http.MethodGet, "/-/admin/config", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Result().StatusCode != http.StatusOK {
		body, _ := io.ReadAll(recorder.Result().Body)
		t.Fatalf("expected 200, got %d: %s", recorder.Result().StatusCode, string(body))
	}

	data, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read config body: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "sk-visible") || strings.Contains(text, "sk-header") || strings.Contains(text, "sk-proxy") || strings.Contains(text, "sk-x-api") {
		t.Fatalf("expected config view to redact secrets, got %q", text)
	}
	if !strings.Contains(text, "\"enabled\":true") {
		t.Fatalf("expected config view to include admin enabled state, got %q", text)
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, `"X-Org":"demo"`) {
		t.Fatalf("expected config view to keep non-sensitive fields and redact secrets, got %q", text)
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
	if !strings.Contains(text, "bridge:") || !strings.Contains(text, "upstreams:") {
		t.Fatalf("expected export yaml body, got %q", text)
	}
	if strings.Contains(text, "sk-export") || strings.Contains(text, "auth_token: secret") {
		t.Fatalf("expected export yaml to redact secrets, got %q", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("expected export yaml to contain redaction marker, got %q", text)
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
	responseData, err := io.ReadAll(rollbackRecorder.Result().Body)
	if err != nil {
		t.Fatalf("read rollback response: %v", err)
	}
	responseText := string(responseData)
	if strings.Contains(responseText, "sk-old") || strings.Contains(responseText, "secret") {
		t.Fatalf("expected rollback response body to redact secrets, got %q", responseText)
	}
	if !strings.Contains(responseText, "[REDACTED]") {
		t.Fatalf("expected rollback response body to include redaction marker, got %q", responseText)
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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

	data, err := io.ReadAll(recorder.Result().Body)
	if err != nil {
		t.Fatalf("read history payload: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "sk-provider-a") || strings.Contains(text, "sk-provider-b") {
		t.Fatalf("expected history metadata response without secrets, got %q", text)
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
		if strings.Contains(line.Text, "sk-") {
			t.Fatalf("expected diff lines to redact secrets, got %#v", diff.Lines)
		}
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
	handler := newTestRouter(t, router.NewManager(store), newTestStore(t), telemetry.NewPricingCatalog(cfg.Pricing))

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
