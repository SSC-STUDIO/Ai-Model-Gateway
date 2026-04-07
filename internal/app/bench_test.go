package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-model-gateway/internal/core"
)

// BenchmarkPipeline_Success benchmarks the full pipeline with a mock upstream.
func BenchmarkPipeline_Success(b *testing.B) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "resp-1", "object": "chat.completion"})
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "bench", BaseURL: upstream.URL, APIKey: "sk-bench", Models: []string{"gpt-4o"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 0,
		Health:     core.HealthCheckConfig{Enabled: false},
	}

	resolver := NewModelResolver(core.CompatConfig{})
	selector := NewRouteSelector(routing, providers)
	transport := NewUpstreamTransport()
	inspector := NewResponseInspector(routing)
	sink := &mockSink{}

	pl := NewPipeline(PipelineParams{
		Resolver:  resolver,
		Selector:  selector,
		Transport: transport,
		Inspector: inspector,
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      sink,
		Cfg:       routing,
	})

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"bench"}]}`)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := &core.GatewayRequest{
			ID:      "bench",
			Model:   "gpt-4o",
			Method:  "POST",
			Path:    "/v1/chat/completions",
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Body:    body,
			Ctx:     ctx,
		}
		resp, err := pl.Handle(ctx, req)
		if err != nil {
			b.Fatalf("pipeline error: %v", err)
		}
		if resp.StatusCode != 200 {
			b.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	}
}

// BenchmarkModelResolver benchmarks model resolution with bridge rules.
func BenchmarkModelResolver(b *testing.B) {
	resolver := NewModelResolver(core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "claude-*", To: "gpt-4o"},
				{From: "gemini-*", To: "gpt-4o-mini"},
			},
			ExcludeUserAgents: []string{"internal-bot/*"},
		},
		Fallback: core.FallbackConfig{
			Enabled: true,
			Models:  map[string]string{"gpt-4o": "gpt-4o-mini"},
		},
	})

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resolver.Resolve(ctx, "claude-3-opus", "Mozilla/5.0")
	}
}

// BenchmarkRouteSelector benchmarks route selection with multiple providers.
func BenchmarkRouteSelector(b *testing.B) {
	tr := true
	providers := []core.Provider{
		{Name: "a", BaseURL: "https://a.com", Models: []string{"gpt-4o", "gpt-4o-mini"}, Weight: 3, Enabled: &tr},
		{Name: "b", BaseURL: "https://b.com", Models: []string{"gpt-4o"}, Weight: 2, Enabled: &tr},
		{Name: "c", BaseURL: "https://c.com", Models: []string{"gpt-4o", "claude-3"}, Weight: 1, Enabled: &tr},
	}
	selector := NewRouteSelector(core.RoutingConfig{
		Strategy: core.StrategyHealthWeightedRR,
		StickySessions: core.StickySessionConfig{Enabled: true, TTLSec: 300},
	}, providers)

	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		selector.Select(ctx, "gpt-4o", "session-123")
	}
}

// BenchmarkCompatAdapter_RewriteModel benchmarks JSON model field rewriting.
func BenchmarkCompatAdapter_RewriteModel(b *testing.B) {
	adapter := NewCompatAdapter(core.CompatConfig{
		Bridge: core.BridgeConfig{Enabled: true},
	})

	body := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hello world this is a benchmark test"}],"max_tokens":100,"temperature":0.7}`)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := &core.GatewayRequest{
			OriginalModel: "gpt-4",
			Model:         "gpt-4o",
			Body:          append([]byte(nil), body...),
		}
		adapter.AdaptRequest(ctx, req)
	}
}

// BenchmarkAnthropicToChatRequest benchmarks Anthropic→Chat protocol conversion.
func BenchmarkAnthropicToChatRequest(b *testing.B) {
	body := []byte(`{"model":"claude-3","system":"You are helpful.","max_tokens":100,"messages":[{"role":"user","content":"Hello, how are you doing today?"}]}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, err := AnthropicToChatRequest(body, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResponsesToChatRequest benchmarks Responses→Chat protocol conversion.
func BenchmarkResponsesToChatRequest(b *testing.B) {
	body := []byte(`{"model":"gpt-4o","instructions":"Be concise.","input":"What is the capital of France?","max_output_tokens":50}`)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, err := ResponsesToChatRequest(body, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkResponseInspector benchmarks response inspection.
func BenchmarkResponseInspector(b *testing.B) {
	inspector := NewResponseInspector(core.RoutingConfig{
		Retry: core.RetryPolicyConfig{
			StatusCodes:     []int{429, 503},
			StatusCodeMin:   func() *int { v := 500; return &v }(),
			MessageKeywords: []string{"rate limit", "overloaded"},
		},
	})

	ctx := context.Background()
	req := &core.GatewayRequest{Model: "gpt-4o"}
	resp := &core.GatewayResponse{
		StatusCode: 200,
		Body:       []byte(`{"id":"resp","choices":[{"message":{"content":"ok"}}]}`),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		inspector.Inspect(ctx, req, resp)
	}
}
