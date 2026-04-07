package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/core"
)

func TestHealthHandler_ReturnsV1CompatibleRuntimeDetail(t *testing.T) {
	tr := true
	providers := []core.Provider{
		{Name: "zeta", BaseURL: "https://zeta.example.com", Models: []string{"z-model"}, Weight: 1, Enabled: &tr},
		{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"a-model"}, Weight: 2, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy: core.StrategyHealthWeightedRR,
		Health: core.HealthCheckConfig{
			Enabled:     true,
			IntervalSec: 10,
			TimeoutMs:   2000,
			Path:        "/v1/models",
		},
	}
	compat := core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "gpt-4", To: "gpt-4o"},
			},
		},
	}

	sel := newRouteSelector(routing, providers, nil)
	cfg := &core.Config{
		Routing: routing,
		Compat:  compat,
		Providers: []core.Provider{
			{Name: "zeta", BaseURL: "https://zeta.example.com", Models: []string{"z-model"}, Weight: 1, Enabled: &tr},
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"a-model"}, Weight: 2, Enabled: &tr},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	req.Header.Set(observability.RequestIDHeader, "req-health")
	rec := httptest.NewRecorder()

	healthHandler(func() *core.Config { return cfg }, sel).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(observability.RequestIDHeader); got != "req-health" {
		t.Fatalf("expected request ID header req-health, got %q", got)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %#v", body["status"])
	}
	if body["request_id"] != "req-health" {
		t.Fatalf("expected request_id req-health, got %#v", body["request_id"])
	}
	if body["router_strategy"] != core.StrategyHealthWeightedRR {
		t.Fatalf("expected router strategy %q, got %#v", core.StrategyHealthWeightedRR, body["router_strategy"])
	}

	bridge, ok := body["bridge"].(map[string]any)
	if !ok {
		t.Fatalf("expected bridge object, got %#v", body["bridge"])
	}
	if bridge["enabled"] != true {
		t.Fatalf("expected bridge.enabled true, got %#v", bridge["enabled"])
	}

	models, ok := body["available_models"].([]any)
	if !ok {
		t.Fatalf("expected available_models array, got %#v", body["available_models"])
	}
	if len(models) != 2 || models[0] != "a-model" || models[1] != "z-model" {
		t.Fatalf("expected sorted available models, got %#v", models)
	}

	upstreams, ok := body["upstreams"].(map[string]any)
	if !ok {
		t.Fatalf("expected upstreams object, got %#v", body["upstreams"])
	}
	alpha, ok := upstreams["alpha"].(map[string]any)
	if !ok {
		t.Fatalf("expected alpha upstream status, got %#v", upstreams["alpha"])
	}
	if alpha["healthy"] != true {
		t.Fatalf("expected alpha healthy=true, got %#v", alpha["healthy"])
	}
}
