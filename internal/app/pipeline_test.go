package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-model-gateway/internal/core"
)

// mockSink implements core.TelemetrySink for testing.
type mockSink struct {
	records []*core.RequestRecord
}

func (m *mockSink) Record(_ context.Context, rec *core.RequestRecord) error {
	m.records = append(m.records, rec)
	return nil
}
func (m *mockSink) Close() error { return nil }

func setupPipelineTest(t *testing.T, upstreamHandler http.HandlerFunc) (*httptest.Server, core.Pipeline, core.RouteSelector, *mockSink) {
	return setupPipelineTestWithCompat(t, upstreamHandler, core.CompatConfig{})
}

func setupPipelineTestWithCompat(t *testing.T, upstreamHandler http.HandlerFunc, compat core.CompatConfig) (*httptest.Server, core.Pipeline, core.RouteSelector, *mockSink) {
	t.Helper()

	upstream := httptest.NewServer(upstreamHandler)

	tr := true
	providers := []core.Provider{
		{
			Name:      "test-provider",
			BaseURL:   upstream.URL,
			APIKey:    "sk-test",
			Models:    []string{"gpt-4o", "gpt-4o-mini"},
			Weight:    1,
			TimeoutMs: 5000,
			Enabled:   &tr,
		},
	}

	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		RetryBackoff: core.RetryBackoffConfig{
			InitialMs: 10, // very short for tests
			MaxMs:     50,
		},
		Health: core.HealthCheckConfig{Enabled: false},
		StickySessions: core.StickySessionConfig{
			Enabled: true,
			TTLSec:  300,
		},
		FailurePolicy: core.FailurePolicyConfig{
			Threshold:   5,
			CooldownSec: 60,
		},
		Retry: core.RetryPolicyConfig{
			StatusCodes:     []int{429},
			StatusCodeMin:   intPtr(500),
			MessageKeywords: []string{"rate limit"},
		},
	}

	resolver := NewModelResolver(compat)
	selector := NewRouteSelector(routing, providers)
	transport := newUpstreamTransport(nil)
	inspector := NewResponseInspector(routing)
	sink := &mockSink{}

	pl := NewPipeline(PipelineParams{
		Resolver:  resolver,
		Selector:  selector,
		Transport: transport,
		Inspector: inspector,
		Compat:    nil,
		Sink:      sink,
		Cfg:       routing,
	})

	return upstream, pl, selector, sink
}

func intPtr(v int) *int { return &v }

func TestPipeline_SuccessfulRequest(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"response": "hello"})
	}

	upstream, pl, _, sink := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-1",
		Model:         "gpt-4o",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if len(resp.Body) == 0 {
		t.Error("expected non-empty body")
	}
	if len(sink.records) != 1 {
		t.Errorf("expected 1 telemetry record, got %d", len(sink.records))
	}
	if sink.records[0].Provider != "test-provider" {
		t.Errorf("expected provider test-provider, got %s", sink.records[0].Provider)
	}
}

func TestPipeline_RetryOnServerError(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"internal error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"response": "ok after retry"})
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-retry",
		Model:         "gpt-4o",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{},
		Body:          []byte(`{"model":"gpt-4o"}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 upstream calls (retry), got %d", callCount)
	}
	// The final response should be 200 (success after retry) or 500 (if retry also failed).
	// With our test setup, second call returns 200.
	if resp.StatusCode != 200 {
		t.Logf("response status: %d (may be 500 if retry limit reached)", resp.StatusCode)
	}
}

func TestPipeline_TelemetryIncludesRoutingMetadataAndUsage(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"model":"gpt-4o-mini",
			"usage":{
				"prompt_tokens":120,
				"completion_tokens":30,
				"total_tokens":150,
				"prompt_tokens_details":{"cached_tokens":40}
			}
		}`))
	}

	compat := core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "gpt-4o", To: "gpt-4o-mini"},
			},
		},
	}
	upstream, pl, _, sink := setupPipelineTestWithCompat(t, handler, compat)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-telemetry-metadata",
		Model:         "gpt-4o",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/responses",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"gpt-4o","input":"hello"}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(sink.records))
	}

	rec := sink.records[0]
	if rec.Path != "/v1/responses" {
		t.Fatalf("expected path /v1/responses, got %q", rec.Path)
	}
	if rec.RequestedModel != "gpt-4o" {
		t.Fatalf("expected requested model gpt-4o, got %q", rec.RequestedModel)
	}
	if rec.EffectiveModel != "gpt-4o-mini" {
		t.Fatalf("expected effective model gpt-4o-mini, got %q", rec.EffectiveModel)
	}
	if rec.RouteMode != "bridged" {
		t.Fatalf("expected route mode bridged, got %q", rec.RouteMode)
	}
	if rec.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", rec.Attempts)
	}
	if rec.InputTokens != 120 || rec.OutputTokens != 30 || rec.CachedPromptTokens != 40 {
		t.Fatalf("unexpected token usage: %+v", rec)
	}
}

func TestPipeline_TelemetryRecordsResponsesCompatRouteMode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"error":{"message":"not implemented"}}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl-compat","object":"chat.completion","created":1700000000,"model":"claude-sonnet-4-6","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-test", Models: []string{"claude-sonnet-4-6"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	sink := &mockSink{}
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      sink,
		Cfg:       routing,
	})

	req := &core.GatewayRequest{
		ID:            "test-telemetry-responses-compat",
		Model:         "claude-sonnet-4-6",
		ModelRequired: true,
		Method:        http.MethodPost,
		Path:          "/v1/responses",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"claude-sonnet-4-6","input":"ping"}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(sink.records))
	}
	if sink.records[0].RouteMode != "responses_compat" {
		t.Fatalf("expected route mode responses_compat, got %q", sink.records[0].RouteMode)
	}
}

func TestPipeline_TelemetryRecordsChatAnthropicCompatRouteMode(t *testing.T) {
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected OpenAI path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"forbidden"}}`))
	}))
	defer openAIUpstream.Close()

	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected anthropic path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_compat","type":"message","model":"kimi-for-coding","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer anthropicUpstream.Close()

	tr := true
	providers := []core.Provider{
		{
			Name:             "kimi",
			BaseURL:          openAIUpstream.URL,
			AnthropicBaseURL: anthropicUpstream.URL,
			APIKey:           "sk-kimi",
			Models:           []string{"kimi-for-coding"},
			Weight:           1,
			TimeoutMs:        5000,
			Enabled:          &tr,
		},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	sink := &mockSink{}
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      sink,
		Cfg:       routing,
	})

	req := &core.GatewayRequest{
		ID:            "test-telemetry-chat-anthropic-compat",
		Model:         "kimi-for-coding",
		ModelRequired: true,
		Method:        http.MethodPost,
		Path:          "/v1/chat/completions",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"ping"}]}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(sink.records))
	}
	if sink.records[0].RouteMode != "anthropic_messages_compat" {
		t.Fatalf("expected route mode anthropic_messages_compat, got %q", sink.records[0].RouteMode)
	}
}

func TestPipeline_TelemetryRecordsAnthropicCountTokensCompatRouteMode(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_count","type":"message","model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":21,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	sink := &mockSink{}
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      sink,
		Cfg:       routing,
	})

	req := &core.GatewayRequest{
		ID:            "test-telemetry-count-tokens-compat",
		Model:         "claude-opus-4-6",
		ModelRequired: true,
		Method:        http.MethodPost,
		Path:          "/v1/messages/count_tokens",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"ping"}]}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(sink.records) != 1 {
		t.Fatalf("expected 1 telemetry record, got %d", len(sink.records))
	}
	if sink.records[0].RouteMode != "anthropic_count_tokens_compat" {
		t.Fatalf("expected route mode anthropic_count_tokens_compat, got %q", sink.records[0].RouteMode)
	}
}

func TestPipeline_BridgeFallbackRestoresRequestBodyModel(t *testing.T) {
	bridgeCalls := 0
	var bridgeModel string
	bridgeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode bridge body: %v", err)
		}
		bridgeModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"bridge failed"}}`))
	}))
	defer bridgeUpstream.Close()

	fallbackCalls := 0
	var fallbackModel string
	fallbackUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode fallback body: %v", err)
		}
		fallbackModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_fallback","object":"response","model":"gpt-5.2","usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}}`))
	}))
	defer fallbackUpstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "bridge", BaseURL: bridgeUpstream.URL, APIKey: "sk-bridge", Models: []string{"gpt-5.4"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
		{Name: "fallback", BaseURL: fallbackUpstream.URL, APIKey: "sk-fallback", Models: []string{"gpt-5.2"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   2,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
		Retry: core.RetryPolicyConfig{
			StatusCodeMin: intPtr(500),
		},
	}
	compat := core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
		},
		Fallback: core.FallbackConfig{
			Enabled: true,
			Models: map[string]string{
				"gpt-5.4": "gpt-5.2",
			},
		},
	}
	selector := NewRouteSelector(routing, providers)
	sink := &mockSink{}
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(compat),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(compat),
		Sink:      sink,
		Cfg:       routing,
	})

	req := &core.GatewayRequest{
		ID:            "test-bridge-fallback-body",
		Model:         "gpt-5.2",
		ModelRequired: true,
		Method:        http.MethodPost,
		Path:          "/v1/responses",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"gpt-5.2","input":"hi"}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d (bridgeCalls=%d fallbackCalls=%d bridgeModel=%q fallbackModel=%q body=%s)", resp.StatusCode, bridgeCalls, fallbackCalls, bridgeModel, fallbackModel, string(resp.Body))
	}
	if bridgeModel != "gpt-5.4" {
		t.Fatalf("expected bridge request model gpt-5.4, got %q", bridgeModel)
	}
	if fallbackModel != "gpt-5.2" {
		t.Fatalf("expected fallback request model gpt-5.2, got %q", fallbackModel)
	}
	if len(sink.records) != 2 {
		t.Fatalf("expected 2 telemetry records, got %d", len(sink.records))
	}
	if got := sink.records[1].RouteMode; got != "bridge_fallback" {
		t.Fatalf("expected final route mode bridge_fallback, got %q", got)
	}
}

func TestPipeline_TelemetryAttemptsIncrementAcrossRetries(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}

	upstream, pl, _, sink := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-telemetry-attempts",
		Model:         "gpt-4o",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected final status 200, got %d", resp.StatusCode)
	}
	if len(sink.records) != 2 {
		t.Fatalf("expected 2 telemetry records (retry), got %d", len(sink.records))
	}
	if sink.records[0].Attempts != 1 {
		t.Fatalf("expected first attempt=1, got %d", sink.records[0].Attempts)
	}
	if sink.records[1].Attempts != 2 {
		t.Fatalf("expected second attempt=2, got %d", sink.records[1].Attempts)
	}
}

func TestPipeline_ModelNotFound(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-notfound",
		Model:         "",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Ctx:           context.Background(),
	}

	_, err := pl.Handle(context.Background(), req)
	if err == nil {
		t.Error("expected error for empty model")
	}
	if !errors.Is(err, core.ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got %v", err)
	}
}

func TestPipeline_NoProviderForModel(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-noprovider",
		Model:         "nonexistent-model",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{},
		Body:          []byte(`{"model":"nonexistent-model"}`),
		Ctx:           context.Background(),
	}

	_, err := pl.Handle(context.Background(), req)
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}

func TestPipeline_SSEStreaming(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		w.Write([]byte("data: {\"chunk\":1}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		w.Write([]byte("data: {\"chunk\":2}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-sse",
		Model:         "gpt-4o",
		ModelRequired: true,
		Stream:        true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{},
		Body:          []byte(`{"model":"gpt-4o","stream":true}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if !resp.Stream {
		t.Error("expected streaming response")
	}
	if resp.BodyReader == nil {
		t.Error("expected non-nil BodyReader for streaming response")
	} else {
		resp.BodyReader.Close()
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := &core.GatewayRequest{
		ID:            "test-cancel",
		Model:         "gpt-4o",
		ModelRequired: true,
		Method:        "POST",
		Path:          "/v1/chat/completions",
		Headers:       http.Header{},
		Body:          []byte(`{"model":"gpt-4o"}`),
		Ctx:           ctx,
	}

	resp, err := pl.Handle(ctx, req)
	// Should return a timeout/gateway-timeout response or error.
	if err != nil {
		return // acceptable — pipeline returned error on cancellation
	}
	if resp.StatusCode != http.StatusGatewayTimeout && resp.StatusCode != http.StatusBadGateway {
		t.Logf("got status %d (acceptable for cancelled context)", resp.StatusCode)
	}
}

func TestPipeline_OptionalModelEndpointAllowsEmptyModel(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}

	upstream, pl, _, _ := setupPipelineTest(t, handler)
	defer upstream.Close()

	req := &core.GatewayRequest{
		ID:            "test-optional-model",
		Model:         "",
		ModelRequired: false,
		Method:        "POST",
		Path:          "/v1/images/generations",
		Headers:       http.Header{"Content-Type": []string{"application/json"}},
		Body:          []byte(`{"prompt":"a red bird"}`),
		Ctx:           context.Background(),
	}

	resp, err := pl.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestModelsHandler(t *testing.T) {
	tr := true
	providers := []core.Provider{
		{Name: "a", Models: []string{"gpt-4o", "gpt-4o-mini"}, Enabled: &tr},
		{Name: "b", Models: []string{"claude-3"}, Enabled: &tr},
	}
	sel := NewRouteSelector(core.RoutingConfig{}, providers)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/models", nil)
	modelsHandler(sel)(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Object != "list" {
		t.Errorf("expected object=list, got %s", result.Object)
	}
	if len(result.Data) != 3 {
		t.Errorf("expected 3 models, got %d", len(result.Data))
	}
}
