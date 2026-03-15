package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestHandlerRetriesAndAddsObservabilityHeaders(t *testing.T) {
	var badCalls atomic.Int32
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"bad upstream"}`)
	}))
	defer badServer.Close()

	var goodCalls atomic.Int32
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls.Add(1)
		if got := r.Header.Get(observability.RequestIDHeader); got != "req-123" {
			t.Fatalf("expected request id forwarded, got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-good" {
			t.Fatalf("expected upstream auth header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion"}`)
	}))
	defer goodServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 2},
		Upstreams: []config.Upstream{
			{Name: "bad", BaseURL: badServer.URL, APIKey: "sk-bad", Models: []string{"gpt-4o-mini"}, Weight: 1},
			{Name: "good", BaseURL: goodServer.URL, APIKey: "sk-good", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-123"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "good" {
		t.Fatalf("expected upstream header good, got %q", got)
	}
	if got := resp.Header.Get(observability.AttemptsHeader); got != "2" {
		t.Fatalf("expected attempts header 2, got %q", got)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-4o-mini" {
		t.Fatalf("expected model header, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-123" {
		t.Fatalf("expected response request id, got %q", got)
	}
	if badCalls.Load() != 1 {
		t.Fatalf("expected bad upstream called once, got %d", badCalls.Load())
	}
	if goodCalls.Load() != 1 {
		t.Fatalf("expected good upstream called once, got %d", goodCalls.Load())
	}
}

func TestHandlerInterceptRuleForcesRetry(t *testing.T) {
	var badCalls atomic.Int32
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":{"message":"kuba meltdown"}}`)
	}))
	defer badServer.Close()

	var goodCalls atomic.Int32
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion"}`)
	}))
	defer goodServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Proxy: config.ProxyPolicyConfig{
			Intercepts: []config.ResponseInterceptRule{
				{
					Name:            "kuba-meltdown",
					Action:          "retry",
					MessageKeywords: []string{"kuba meltdown"},
				},
			},
		},
		Upstreams: []config.Upstream{
			{Name: "bad", BaseURL: badServer.URL, Models: []string{"gpt-4o-mini"}, Weight: 1},
			{Name: "good", BaseURL: goodServer.URL, Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "good" {
		t.Fatalf("expected upstream header good, got %q", got)
	}
	if got := resp.Header.Get(observability.AttemptsHeader); got != "2" {
		t.Fatalf("expected attempts header 2, got %q", got)
	}
	if badCalls.Load() != 1 {
		t.Fatalf("expected bad upstream called once, got %d", badCalls.Load())
	}
	if goodCalls.Load() != 1 {
		t.Fatalf("expected good upstream called once, got %d", goodCalls.Load())
	}
}

func TestHandlerAllowsOptionalModelEndpoints(t *testing.T) {
	var calls atomic.Int32
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":123,"data":[{"url":"https://example.com/image.png"}]}`)
	}))
	defer imageServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "images", BaseURL: imageServer.URL, Models: []string{"gpt-image-1"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"prompt":"a red bird"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-img"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ImageGenerations(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "images" {
		t.Fatalf("expected upstream header images, got %q", got)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "" {
		t.Fatalf("expected empty model header, got %q", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected single upstream call, got %d", calls.Load())
	}
}

func TestHandlerExtractsModelFromMultipartRequests(t *testing.T) {
	var calls atomic.Int32
	audioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("expected multipart content type, got %q", r.Header.Get("Content-Type"))
		}
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hello world"}`)
	}))
	defer audioServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "audio", BaseURL: audioServer.URL, Models: []string{"gpt-4o-mini-transcribe"}, Weight: 1},
		},
	}
	cfg.Normalize()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "gpt-4o-mini-transcribe"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(fileWriter, "RIFF....WAVE"); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-audio"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler.AudioTranscriptions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "audio" {
		t.Fatalf("expected upstream header audio, got %q", got)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-4o-mini-transcribe" {
		t.Fatalf("expected model header, got %q", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected single upstream call, got %d", calls.Load())
	}
}

func TestHandlerPassthroughsRetryableUpstreamBodyAfterWindow(t *testing.T) {
	rateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"too many requests","type":"rate_limit"}}`)
	}))
	defer rateLimitServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:                   "round_robin",
			MaxRetries:                 1,
			FailureThreshold:           100,
			CooldownSec:                60,
			FailurePassthroughAfterSec: 1,
		},
		Upstreams: []config.Upstream{
			{Name: "solo", BaseURL: rateLimitServer.URL, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := router.NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestFailure("solo", time.Millisecond, http.StatusTooManyRequests, nil, true, "status")
	time.Sleep(1100 * time.Millisecond)

	handler := NewHandler(manager, newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-pass"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected passthrough 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "solo" {
		t.Fatalf("expected upstream header solo, got %q", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	errorMap, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected upstream error payload")
	}
	if errorMap["message"] != "too many requests" {
		t.Fatalf("expected passthrough message, got %#v", errorMap["message"])
	}
}

func TestHandlerRecordsFinal503WithLastUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"late"}`)
	}))
	defer upstream.Close()

	store := newTestStore(t)
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "codex-openai", BaseURL: upstream.URL, Models: []string{"gpt-5.2"}, Weight: 1, TimeoutMs: 10},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2","input":"hi"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-final-503"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "codex-openai" {
		t.Fatalf("expected upstream header codex-openai, got %q", got)
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	if snapshot.Requests[0].Upstream != "codex-openai" {
		t.Fatalf("expected recorded upstream codex-openai, got %q", snapshot.Requests[0].Upstream)
	}
}

func TestResponsesCompatFallbackToChatCompletions(t *testing.T) {
	var responsesCalls atomic.Int32
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `{"error":{"message":"not implemented"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"claude-opus-4-6","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "gm12331", BaseURL: upstream.URL, Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"claude-opus-4-6","input":"ping"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-compat"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"object":"response"`) {
		t.Fatalf("expected responses object, got %q", text)
	}
	if !strings.Contains(text, `"output_text":"pong"`) {
		t.Fatalf("expected output_text pong, got %q", text)
	}
	if responsesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected responses and chat calls once, got responses=%d chat=%d", responsesCalls.Load(), chatCalls.Load())
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	if snapshot.Requests[0].RouteMode != "responses_compat" {
		t.Fatalf("expected route mode responses_compat, got %q", snapshot.Requests[0].RouteMode)
	}
}

func TestResponsesCompatStreamEmitsCompletedEvent(t *testing.T) {
	var responsesCalls atomic.Int32
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `{"error":{"message":"not implemented"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-2","object":"chat.completion","created":1700000000,"model":"claude-opus-4-6","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "gm12331", BaseURL: upstream.URL, Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"claude-opus-4-6","input":"ping","stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-compat-stream"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "response.completed") {
		t.Fatalf("expected response.completed event, got %q", text)
	}
	if !strings.Contains(text, "pong") {
		t.Fatalf("expected output pong, got %q", text)
	}
	if responsesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected responses and chat calls once, got responses=%d chat=%d", responsesCalls.Load(), chatCalls.Load())
	}
}

func TestHandlerPassesThroughResponsesEventStreamWithoutRetry(t *testing.T) {
	var firstCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"id\":\"resp_1\"}\n\n")
	}))
	defer first.Close()

	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"id\":\"resp_2\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"id\":\"resp_2\"}\n\n")
	}))
	defer second.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 2},
		Upstreams: []config.Upstream{
			{Name: "first", BaseURL: first.URL, Models: []string{"gpt-5.4"}, Weight: 1},
			{Name: "second", BaseURL: second.URL, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hi","stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-sse-retry"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "first" {
		t.Fatalf("expected upstream header first, got %q", got)
	}
	if got := resp.Header.Get(observability.AttemptsHeader); got != "1" {
		t.Fatalf("expected attempts header 1, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), "response.created") {
		t.Fatalf("expected created event in response body, got %q", string(body))
	}
	if firstCalls.Load() != 1 {
		t.Fatalf("expected first upstream called once, got %d", firstCalls.Load())
	}
	if secondCalls.Load() != 0 {
		t.Fatalf("expected second upstream not called, got %d", secondCalls.Load())
	}
}

func TestHandlerBridgesModelFromConfig(t *testing.T) {
	var receivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		model, _ := payload["model"].(string)
		receivedModel = model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_bridge","object":"response"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "*", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2","input":"hi"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-bridge"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedModel != "gpt-5.4" {
		t.Fatalf("expected upstream model gpt-5.4, got %q", receivedModel)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-5.4" {
		t.Fatalf("expected effective model header gpt-5.4, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "gpt-5.2" {
		t.Fatalf("expected requested model header gpt-5.2, got %q", got)
	}
}

func TestHandlerFallsBackToRequestedModelWhenBridgeUpstreamFails(t *testing.T) {
	var bridgeCalls atomic.Int32
	bridgeUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bridgeCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode bridge body: %v", err)
		}
		if got, _ := payload["model"].(string); got != "gpt-5.4" {
			t.Fatalf("expected bridged model gpt-5.4, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"message":"Upstream request failed","type":"upstream"}}`)
	}))
	defer bridgeUpstream.Close()

	var fallbackCalls atomic.Int32
	fallbackUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode fallback body: %v", err)
		}
		if got, _ := payload["model"].(string); got != "gpt-5.2" {
			t.Fatalf("expected fallback model gpt-5.2, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_fallback","object":"response","usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}}`)
	}))
	defer fallbackUpstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 2},
		Upstreams: []config.Upstream{
			{Name: "kuba", BaseURL: bridgeUpstream.URL, Models: []string{"gpt-5.4"}, Weight: 1},
			{Name: "codex-openai", BaseURL: fallbackUpstream.URL, Models: []string{"gpt-5.2"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2","input":"hi"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-bridge-fallback"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if bridgeCalls.Load() != 1 {
		t.Fatalf("expected bridge upstream called once, got %d", bridgeCalls.Load())
	}
	if fallbackCalls.Load() != 1 {
		t.Fatalf("expected fallback upstream called once, got %d", fallbackCalls.Load())
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "codex-openai" {
		t.Fatalf("expected fallback upstream header codex-openai, got %q", got)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-5.2" {
		t.Fatalf("expected final model header gpt-5.2, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "" {
		t.Fatalf("expected empty requested model header after fallback, got %q", got)
	}
	if got := resp.Header.Get(observability.AttemptsHeader); got != "2" {
		t.Fatalf("expected attempts header 2, got %q", got)
	}
	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	if got := snapshot.Requests[0].RequestedModel; got != "gpt-5.2" {
		t.Fatalf("expected requested model gpt-5.2, got %q", got)
	}
	if got := snapshot.Requests[0].RouteMode; got != "bridge_fallback" {
		t.Fatalf("expected route mode bridge_fallback, got %q", got)
	}
}

func TestHandlerBridgesMultipartModelFromConfig(t *testing.T) {
	var receivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		receivedModel = r.FormValue("model")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "*", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "audio", BaseURL: upstream.URL, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "gpt-5.2-codex"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(fileWriter, "RIFF....WAVE"); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-bridge-multipart"))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	recorder := httptest.NewRecorder()
	handler.AudioTranscriptions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedModel != "gpt-5.4" {
		t.Fatalf("expected upstream model gpt-5.4, got %q", receivedModel)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "gpt-5.2-codex" {
		t.Fatalf("expected requested model header, got %q", got)
	}
}

func TestHandlerSkipsBridgeForExcludedUserAgent(t *testing.T) {
	var receivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		model, _ := payload["model"].(string)
		receivedModel = model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_codex","object":"response"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled:           true,
			ExcludeUserAgents: []string{"*Codex Desktop*"},
			Rules: []config.ModelBridgeRule{
				{From: "*", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "codex", BaseURL: upstream.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2-codex","input":"hi"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-codex"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Codex Desktop/0.112.0-alpha.3")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedModel != "gpt-5.2-codex" {
		t.Fatalf("expected upstream model to stay gpt-5.2-codex, got %q", receivedModel)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-5.2-codex" {
		t.Fatalf("expected effective model header gpt-5.2-codex, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "" {
		t.Fatalf("expected empty requested model header when bridge is skipped, got %q", got)
	}
}

func TestHandlerResponsesCompactSkipsBridgeRouting(t *testing.T) {
	var receivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		model, _ := payload["model"].(string)
		receivedModel = model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_compact","object":"response.compaction"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "*", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "codex", BaseURL: upstream.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.2-codex","input":"hi"}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-compact"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ResponsesCompact(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedModel != "gpt-5.2-codex" {
		t.Fatalf("expected compact upstream model gpt-5.2-codex, got %q", receivedModel)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-5.2-codex" {
		t.Fatalf("expected effective model header gpt-5.2-codex, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "" {
		t.Fatalf("expected requested model header to be omitted when unchanged, got %q", got)
	}
}

func TestHandlerResponsesReuseStickyUpstreamFromPreviousResponseID(t *testing.T) {
	var alphaCalls atomic.Int32
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alphaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_alpha","object":"response"}`)
	}))
	defer alpha.Close()

	var betaCalls atomic.Int32
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		betaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_beta","object":"response"}`)
	}))
	defer beta.Close()

	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:   "round_robin",
			MaxRetries: 1,
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1800,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: alpha.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
			{Name: "beta", BaseURL: beta.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))

	first := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2-codex","input":"hi"}`))
	first = first.WithContext(observability.WithRequestID(first.Context(), "req-sticky-1"))
	first.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	handler.Responses(firstRecorder, first)
	if firstRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected first response 200, got %d", firstRecorder.Result().StatusCode)
	}

	second := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2-codex","input":"follow up","previous_response_id":"resp_alpha"}`))
	second = second.WithContext(observability.WithRequestID(second.Context(), "req-sticky-2"))
	second.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	handler.Responses(secondRecorder, second)
	if secondRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected second response 200, got %d", secondRecorder.Result().StatusCode)
	}

	if alphaCalls.Load() != 2 {
		t.Fatalf("expected alpha to handle both sticky requests, got %d calls", alphaCalls.Load())
	}
	if betaCalls.Load() != 0 {
		t.Fatalf("expected beta to be skipped by sticky routing, got %d calls", betaCalls.Load())
	}
}

func TestHandlerResponsesCompactUsesStickyResponseID(t *testing.T) {
	var alphaCalls atomic.Int32
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alphaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/responses":
			_, _ = io.WriteString(w, `{"id":"resp_alpha","object":"response"}`)
		case "/v1/responses/compact":
			_, _ = io.WriteString(w, `{"id":"cmp_alpha","object":"response.compaction"}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer alpha.Close()

	var betaCalls atomic.Int32
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		betaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/responses":
			_, _ = io.WriteString(w, `{"id":"resp_beta","object":"response"}`)
		case "/v1/responses/compact":
			_, _ = io.WriteString(w, `{"id":"cmp_beta","object":"response.compaction"}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer beta.Close()

	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:   "round_robin",
			MaxRetries: 1,
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1800,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: alpha.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
			{Name: "beta", BaseURL: beta.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))

	first := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.2-codex","input":"hi"}`))
	first = first.WithContext(observability.WithRequestID(first.Context(), "req-compact-sticky-1"))
	first.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	handler.Responses(firstRecorder, first)
	if firstRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected first response 200, got %d", firstRecorder.Result().StatusCode)
	}

	compact := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5.2-codex","response_id":"resp_alpha","input":"checkpoint"}`))
	compact = compact.WithContext(observability.WithRequestID(compact.Context(), "req-compact-sticky-2"))
	compact.Header.Set("Content-Type", "application/json")
	compactRecorder := httptest.NewRecorder()
	handler.ResponsesCompact(compactRecorder, compact)
	if compactRecorder.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected compact response 200, got %d", compactRecorder.Result().StatusCode)
	}

	if alphaCalls.Load() != 2 {
		t.Fatalf("expected alpha to serve response and compact request, got %d calls", alphaCalls.Load())
	}
	if betaCalls.Load() != 0 {
		t.Fatalf("expected beta to be skipped by compact sticky routing, got %d calls", betaCalls.Load())
	}
}

func TestHandlerRetriesSameUpstreamWhenConfigured(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if call < 3 {
			_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\"}\n\n")
			return
		}
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 5, RetryBackoffMs: 1, RetryBackoffMaxMs: 1},
		Upstreams: []config.Upstream{
			{Name: "codex-openai", BaseURL: upstream.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1, SameUpstreamRetries: 2},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.2-codex","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-same-upstream"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected same upstream to be retried 3 times, got %d", calls.Load())
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "codex-openai" {
		t.Fatalf("expected codex-openai upstream header, got %q", got)
	}
	if got := resp.Header.Get(observability.AttemptsHeader); got != "3" {
		t.Fatalf("expected attempts header 3, got %q", got)
	}
}

func TestHandlerAllowsChatCompletionsStreamDoneMarker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "xiavier", BaseURL: upstream.URL, Models: []string{"claude-sonnet-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"Reply with pong"}],"stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-chat-stream"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "xiavier" {
		t.Fatalf("expected xiavier upstream header, got %q", got)
	}
}

func TestHandlerDoKeepsStreamingBodyReadableUntilClose(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(25 * time.Millisecond)
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()

	cfg := config.Config{}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp, _, err := handler.do(req, []byte(`{"model":"gpt-5.4","stream":true}`), "application/json", config.Upstream{
		Name:      "stream",
		BaseURL:   upstream.URL,
		Models:    []string{"gpt-5.4"},
		TimeoutMs: 1000,
	}, "req-do-stream")
	if err != nil {
		t.Fatalf("do request: %v", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read streaming body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "response.created") || !strings.Contains(text, "response.completed") {
		t.Fatalf("expected full stream body, got %q", text)
	}
}

func TestInspectEventStreamResponseRejectsIncompleteChatCompletionsStream(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\"}\n\n")),
	}

	cfg := config.Config{}
	cfg.Normalize()

	assessment, _, err := inspectEventStreamResponse(resp, "/v1/chat/completions", cfg.Proxy)
	if err != nil {
		t.Fatalf("inspect stream: %v", err)
	}
	if !assessment.ErrorBody || !assessment.Retryable {
		t.Fatalf("expected incomplete chat stream to be retryable failure, got %+v", assessment)
	}
	if assessment.Message != "stream disconnected before completion: stream closed before [DONE]" {
		t.Fatalf("unexpected assessment message %q", assessment.Message)
	}
}

func TestExtractUsageSupportsResponsesAPIFields(t *testing.T) {
	usage := extractUsage([]byte(`{"usage":{"input_tokens":24,"input_tokens_details":{"cached_tokens":10},"output_tokens":7,"total_tokens":31}}`))
	if usage.PromptTokens != 24 {
		t.Fatalf("expected prompt tokens 24, got %d", usage.PromptTokens)
	}
	if usage.CachedPromptTokens != 10 {
		t.Fatalf("expected cached prompt tokens 10, got %d", usage.CachedPromptTokens)
	}
	if usage.CompletionTokens != 7 {
		t.Fatalf("expected completion tokens 7, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 31 {
		t.Fatalf("expected total tokens 31, got %d", usage.TotalTokens)
	}
}

func TestExtractUsageSupportsChatCompletionsFields(t *testing.T) {
	usage := extractUsage([]byte(`{"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`))
	if usage.PromptTokens != 11 {
		t.Fatalf("expected prompt tokens 11, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 5 {
		t.Fatalf("expected completion tokens 5, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 16 {
		t.Fatalf("expected total tokens 16, got %d", usage.TotalTokens)
	}
}

func TestExtractUsageSupportsResponsesSSECompletedEvent(t *testing.T) {
	body := []byte(`event: response.created
data: {"type":"response.created","response":{"usage":null}}

event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":23,"input_tokens_details":{"cached_tokens":8},"output_tokens":7,"total_tokens":30}}}
`)

	usage := extractUsage(body)
	if usage.PromptTokens != 23 {
		t.Fatalf("expected prompt tokens 23, got %d", usage.PromptTokens)
	}
	if usage.CachedPromptTokens != 8 {
		t.Fatalf("expected cached prompt tokens 8, got %d", usage.CachedPromptTokens)
	}
	if usage.CompletionTokens != 7 {
		t.Fatalf("expected completion tokens 7, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 30 {
		t.Fatalf("expected total tokens 30, got %d", usage.TotalTokens)
	}
}

func TestResponsesCompactSkipsBridgeRewrite(t *testing.T) {
	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "gpt-5.2-codex", To: "gpt-5.4"},
			},
		},
	}
	cfg.Normalize()

	body := []byte(`{"model":"gpt-5.2-codex","input":[{"role":"user","content":"compact me"}]}`)
	resolved, err := resolveModel(body, "application/json", "Codex Desktop/0.112.0-alpha.3", cfg, forwardOptions{
		ModelRequired:    true,
		SkipModelRewrite: true,
	})
	if err != nil {
		t.Fatalf("resolve model: %v", err)
	}
	if resolved.Requested != "gpt-5.2-codex" {
		t.Fatalf("expected requested model gpt-5.2-codex, got %q", resolved.Requested)
	}
	if resolved.Effective != "gpt-5.2-codex" {
		t.Fatalf("expected effective model to skip bridge rewrite, got %q", resolved.Effective)
	}
	if string(resolved.Body) != string(body) {
		t.Fatalf("expected request body to remain unchanged")
	}
}

func TestRetryBackoffDelayCapsAtThirtySeconds(t *testing.T) {
	if got := retryBackoffDelay(3000, 30000, 1); got != 3*time.Second {
		t.Fatalf("expected first retry delay 3s, got %v", got)
	}
	if got := retryBackoffDelay(3000, 30000, 2); got != 6*time.Second {
		t.Fatalf("expected second retry delay 6s, got %v", got)
	}
	if got := retryBackoffDelay(3000, 30000, 4); got != 24*time.Second {
		t.Fatalf("expected fourth retry delay 24s, got %v", got)
	}
	if got := retryBackoffDelay(3000, 30000, 5); got != 30*time.Second {
		t.Fatalf("expected fifth retry delay capped at 30s, got %v", got)
	}
}

func TestIsRetryableMessageMatchesStreamDisconnect(t *testing.T) {
	message := "stream disconnected before completion: stream closed before response.completed"
	cfg := config.Config{}
	cfg.Normalize()
	if !isRetryableMessage(message, cfg.Proxy.Retry) {
		t.Fatalf("expected stream disconnect message to be retryable")
	}
}

func TestBuildRequestDebugSummaryForCodexResponses(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-5.2-codex",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}],
		"previous_response_id":"resp_1234567890",
		"tools":[{"type":"file_search"},{"type":"code_interpreter"}],
		"reasoning":{"effort":"medium"},
		"text":{"format":{"type":"text"}},
		"store":true,
		"stream":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Codex Desktop/0.112.0-alpha.3")

	summary := buildRequestDebugSummary(req, []byte(`{
		"model":"gpt-5.2-codex",
		"input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_text","text":"world"}]}],
		"previous_response_id":"resp_1234567890",
		"tools":[{"type":"file_search"},{"type":"code_interpreter"}],
		"reasoning":{"effort":"medium"},
		"text":{"format":{"type":"text"}},
		"store":true,
		"stream":true
	}`), "gpt-5.2-codex", "gpt-5.2-codex")

	if summary.Path != "/v1/responses" {
		t.Fatalf("expected path /v1/responses, got %q", summary.Path)
	}
	if summary.RequestedModel != "gpt-5.2-codex" {
		t.Fatalf("expected requested model, got %q", summary.RequestedModel)
	}
	if !summary.HasPreviousResponse {
		t.Fatalf("expected previous_response_id to be detected")
	}
	if summary.ToolCount != 2 {
		t.Fatalf("expected tool count 2, got %d", summary.ToolCount)
	}
	if summary.InputItemCount != 1 {
		t.Fatalf("expected input item count 1, got %d", summary.InputItemCount)
	}
	if summary.InputTextChars != len("helloworld") {
		t.Fatalf("expected input text chars %d, got %d", len("helloworld"), summary.InputTextChars)
	}
	if !summary.HasReasoning {
		t.Fatalf("expected reasoning to be detected")
	}
	if !summary.HasTextConfig {
		t.Fatalf("expected text config to be detected")
	}
	if !summary.HasStore {
		t.Fatalf("expected store to be detected")
	}
	if !summary.Stream {
		t.Fatalf("expected stream=true")
	}
}

func TestResponseBodyPreviewCompactsWhitespace(t *testing.T) {
	preview := responseBodyPreview([]byte("{\n  \"error\": {\n    \"message\": \"Upstream request failed\"\n  }\n}"))
	if strings.Contains(preview, "\n") {
		t.Fatalf("expected preview without newlines, got %q", preview)
	}
	if !strings.Contains(preview, "Upstream request failed") {
		t.Fatalf("expected preview to contain error message, got %q", preview)
	}
}
