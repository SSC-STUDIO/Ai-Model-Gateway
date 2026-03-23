package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
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

type zeroReadCloser struct {
	remaining int64
}

func (r *zeroReadCloser) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	clear(p[:n])
	r.remaining -= int64(n)
	return n, nil
}

func (r *zeroReadCloser) Close() error {
	return nil
}

type errReadCloser struct {
	err error
}

func (r *errReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *errReadCloser) Close() error {
	return nil
}

func TestHandlerRejectsRequestBodyOverProxyLimit(t *testing.T) {
	cfg := config.Config{}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-too-large"))
	req.Header.Set("Content-Type", "application/json")
	req.Body = &zeroReadCloser{remaining: maxProxyRequestBodyBytes + 1}

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-too-large" {
		t.Fatalf("expected request id header, got %q", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	errorMap, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error payload, got %#v", payload["error"])
	}
	if errorMap["type"] != "request_too_large" {
		t.Fatalf("expected request_too_large, got %#v", errorMap["type"])
	}
	if errorMap["message"] != "request body exceeds 104857600 bytes" {
		t.Fatalf("unexpected error message %#v", errorMap["message"])
	}
}

func TestHandlerPreservesInvalidRequestErrorForOrdinaryBodyReadFailure(t *testing.T) {
	cfg := config.Config{}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-body-read-error"))
	req.Header.Set("Content-Type", "application/json")
	req.Body = &errReadCloser{err: errors.New("boom")}

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-body-read-error" {
		t.Fatalf("expected request id header, got %q", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	errorMap, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error payload, got %#v", payload["error"])
	}
	if errorMap["type"] != "invalid_request_error" {
		t.Fatalf("expected invalid_request_error, got %#v", errorMap["type"])
	}
	if errorMap["message"] != "read request body: boom" {
		t.Fatalf("unexpected error message %#v", errorMap["message"])
	}
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

func TestHandlerInfiniteRetryModeKeepsRecoveringSingleUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if attempt < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"temporary overload"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"chatcmpl-3","object":"chat.completion"}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:          "round_robin",
			MaxRetries:        0,
			RetryBackoffMs:    1,
			RetryBackoffMaxMs: 1,
			FailureThreshold:  10,
			CooldownSec:       0,
		},
		Proxy: config.ProxyPolicyConfig{
			Retry: config.RetryPolicyConfig{
				InfiniteOnError: true,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "solo", BaseURL: upstream.URL, Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-infinite"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "solo" {
		t.Fatalf("expected upstream header solo, got %q", got)
	}
	attempts, err := strconv.Atoi(resp.Header.Get(observability.AttemptsHeader))
	if err != nil {
		t.Fatalf("expected numeric attempts header, got %q", resp.Header.Get(observability.AttemptsHeader))
	}
	if attempts < 3 {
		t.Fatalf("expected attempts header to reflect at least 3 recovery loops, got %d", attempts)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected upstream to be retried until recovery, got %d calls", calls.Load())
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

func TestHandlerQuotaLimitedUpstreamGetsBlockedAfterQuotaError(t *testing.T) {
	var quotaCalls atomic.Int32
	quotaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		quotaCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"insufficient_quota","type":"insufficient_quota"}}`)
	}))
	defer quotaServer.Close()

	var fallbackCalls atomic.Int32
	fallbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-fallback","object":"chat.completion"}`)
	}))
	defer fallbackServer.Close()

	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:          "round_robin",
			MaxRetries:        2,
			RetryBackoffMs:    1,
			RetryBackoffMaxMs: 1,
		},
		Upstreams: []config.Upstream{
			{Name: "codex-openai", BaseURL: quotaServer.URL, ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.2-codex"}, Weight: 1},
			{Name: "codex-backup", BaseURL: fallbackServer.URL, ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := router.NewManager(state.NewConfigStore(cfg))
	handler := NewHandler(manager, newTestStore(t))

	makeRequest := func(requestID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.2-codex","messages":[{"role":"user","content":"hi"}]}`))
		req = req.WithContext(observability.WithRequestID(req.Context(), requestID))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ChatCompletions(recorder, req)
		return recorder
	}

	first := makeRequest("req-quota-block-1")
	if first.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected first request to recover via fallback, got %d", first.Result().StatusCode)
	}
	if got := first.Result().Header.Get(observability.UpstreamHeader); got != "codex-backup" {
		t.Fatalf("expected fallback upstream header codex-backup, got %q", got)
	}

	second := makeRequest("req-quota-block-2")
	if second.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected second request to use remaining quota upstream, got %d", second.Result().StatusCode)
	}
	if got := second.Result().Header.Get(observability.UpstreamHeader); got != "codex-backup" {
		t.Fatalf("expected blocked quota upstream to stay skipped, got %q", got)
	}

	if quotaCalls.Load() != 1 {
		t.Fatalf("expected quota-limited upstream to be blocked after first quota error, got %d calls", quotaCalls.Load())
	}
	if fallbackCalls.Load() != 2 {
		t.Fatalf("expected fallback upstream to serve both requests, got %d calls", fallbackCalls.Load())
	}

	status := manager.Snapshot()["codex-openai"]
	if !status.QuotaBlocked {
		t.Fatalf("expected codex-openai to be marked quota blocked")
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

func TestChatCompletionsAnthropicMessagesCompatFallback(t *testing.T) {
	var chatCalls atomic.Int32
	var messagesCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			chatCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"service temporarily unavailable"}}`)
		case "/v1/messages":
			messagesCalls.Add(1)
			if got := r.Header.Get("x-api-key"); got != "sk-anthropic" {
				t.Fatalf("expected x-api-key header, got %q", got)
			}
			if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
				t.Fatalf("expected anthropic-version header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_123","type":"message","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":12,"output_tokens":1}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "cc-claude", BaseURL: upstream.URL, APIKey: "sk-anthropic", Models: []string{"claude-sonnet-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-chat"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"object":"chat.completion"`) || !strings.Contains(text, `"content":"ok"`) {
		t.Fatalf("expected chat completion payload with ok, got %q", text)
	}
	if chatCalls.Load() != 1 || messagesCalls.Load() != 1 {
		t.Fatalf("expected chat and anthropic messages fallback once, got chat=%d messages=%d", chatCalls.Load(), messagesCalls.Load())
	}
}

func TestChatCompletionsAnthropicMessagesCompatFallbackStream(t *testing.T) {
	var messagesCalls atomic.Int32
	var anthropicRequestBody atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"service temporarily unavailable"}}`)
		case "/v1/messages":
			messagesCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			anthropicRequestBody.Store(string(body))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_456","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":13,"output_tokens":1}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"Reply with exactly ok"}],"stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-stream"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected chat completion chunk payload, got %q", text)
	}
	if !strings.Contains(text, `"content":"ok"`) {
		t.Fatalf("expected streamed content ok, got %q", text)
	}
	if !strings.Contains(text, `data: [DONE]`) {
		t.Fatalf("expected done marker, got %q", text)
	}
	if messagesCalls.Load() != 1 {
		t.Fatalf("expected one anthropic messages call, got %d", messagesCalls.Load())
	}
	rawBody, _ := anthropicRequestBody.Load().(string)
	if !strings.Contains(rawBody, `"stream":false`) {
		t.Fatalf("expected anthropic fallback request to disable stream, got %q", rawBody)
	}
}

func TestChatCompletionsAnthropicMessagesCompatFallbackPreservesImages(t *testing.T) {
	var messagesCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"service temporarily unavailable"}}`)
		case "/v1/messages":
			messagesCalls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode anthropic body: %v", err)
			}
			rawMessages, ok := payload["messages"].([]any)
			if !ok || len(rawMessages) != 1 {
				t.Fatalf("expected one anthropic user message, got %#v", payload["messages"])
			}
			message, _ := rawMessages[0].(map[string]any)
			content, ok := message["content"].([]any)
			if !ok || len(content) != 2 {
				t.Fatalf("expected text + image anthropic blocks, got %#v", message["content"])
			}
			image, _ := content[1].(map[string]any)
			source, _ := image["source"].(map[string]any)
			if source["type"] != "url" || source["url"] != "https://example.com/cat.png" {
				t.Fatalf("expected image source url preserved, got %#v", image["source"])
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_789","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":14,"output_tokens":2}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe"},
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}
			]}
		]
	}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-image"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.ChatCompletions(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if messagesCalls.Load() != 1 {
		t.Fatalf("expected one anthropic messages fallback call, got %d", messagesCalls.Load())
	}
}

func TestResponsesAnthropicMessagesCompatFallback(t *testing.T) {
	var responsesCalls atomic.Int32
	var messagesCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"service temporarily unavailable"}}`)
		case "/v1/messages":
			messagesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_456","type":"message","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":1}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "cc-claude", BaseURL: upstream.URL, APIKey: "sk-anthropic", Models: []string{"claude-sonnet-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"claude-sonnet-4-6","input":"Reply with exactly ok","stream":false}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-responses"))
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
	if !strings.Contains(text, `"object":"response"`) || !strings.Contains(text, `"output_text":"ok"`) {
		t.Fatalf("expected responses payload with ok, got %q", text)
	}
	if responsesCalls.Load() != 1 || messagesCalls.Load() != 1 {
		t.Fatalf("expected responses and anthropic messages fallback once, got responses=%d messages=%d", responsesCalls.Load(), messagesCalls.Load())
	}
}

func TestResponsesAnthropicMessagesCompatFallbackPreservesStructuredImageInput(t *testing.T) {
	var messagesCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"service temporarily unavailable"}}`)
		case "/v1/messages":
			messagesCalls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode anthropic body: %v", err)
			}
			rawMessages, ok := payload["messages"].([]any)
			if !ok || len(rawMessages) != 1 {
				t.Fatalf("expected one anthropic user message, got %#v", payload["messages"])
			}
			message, _ := rawMessages[0].(map[string]any)
			content, ok := message["content"].([]any)
			if !ok || len(content) != 2 {
				t.Fatalf("expected text + image anthropic blocks, got %#v", message["content"])
			}
			image, _ := content[1].(map[string]any)
			source, _ := image["source"].(map[string]any)
			if source["type"] != "url" || source["url"] != "https://example.com/cat.png" {
				t.Fatalf("expected image source preserved, got %#v", image["source"])
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_img","type":"message","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":1}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "cc-claude", BaseURL: upstream.URL, APIKey: "sk-anthropic", Models: []string{"claude-sonnet-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4-6",
		"input":[
			{
				"role":"user",
				"content":[
					{"type":"input_text","text":"describe"},
					{"type":"input_image","image_url":"https://example.com/cat.png"}
				]
			}
		]
	}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-responses-image"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if messagesCalls.Load() != 1 {
		t.Fatalf("expected one anthropic messages fallback call, got %d", messagesCalls.Load())
	}
}

func TestMessagesRoutePassthroughAnthropicStream(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\"}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-direct-messages"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: content_block_delta") || !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected anthropic stream body, got %q", text)
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	if !snapshot.Requests[0].Success {
		t.Fatalf("expected streamed anthropic request to be recorded as success")
	}
}

func TestMessagesRoutePassthroughAnthropicBridgeRewritesJSONModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := payload["model"]; got != "gpt-5.4" {
			t.Fatalf("expected bridged upstream model gpt-5.4, got %#v", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_bridge","type":"message","role":"assistant","model":"gpt-5.4","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":7,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-bridge", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"Reply with pong"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-direct-anthropic-bridge-json"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"model":"claude-opus-4-6"`) {
		t.Fatalf("expected rewritten anthropic model, got %q", text)
	}
	if strings.Contains(text, `"model":"gpt-5.4"`) {
		t.Fatalf("expected upstream model to be hidden, got %q", text)
	}
}

func TestMessagesRoutePassthroughAnthropicBridgeRewritesStreamModel(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := payload["model"]; got != "gpt-5.4" {
			t.Fatalf("expected bridged upstream model gpt-5.4, got %#v", got)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_bridge_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"gpt-5.4\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-bridge", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"Reply with pong"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-direct-anthropic-bridge-stream"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"model":"claude-opus-4-6"`) {
		t.Fatalf("expected rewritten anthropic stream model, got %q", text)
	}
	if strings.Contains(text, `"model":"gpt-5.4"`) {
		t.Fatalf("expected upstream model to be hidden in stream, got %q", text)
	}
	if !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected complete anthropic stream, got %q", text)
	}
}

func TestMessageCountTokensAnthropicCompatStripsAuthorizationAndRecordsTelemetry(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected caller authorization header to be stripped, got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}
		betas := strings.Join(r.Header.Values("anthropic-beta"), ",")
		if !strings.Contains(betas, "prompt-caching-2024-07-31") || !strings.Contains(betas, "fine-grained-tool-streaming-2025-05-14") {
			t.Fatalf("expected anthropic-beta headers to pass through, got %q", betas)
		}
		if got := r.Header.Get(observability.RequestIDHeader); got != "req-count-tokens-direct" {
			t.Fatalf("expected request id header, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "claude-sonnet-4-6" {
			t.Fatalf("expected compat probe model rewrite, got %#v", payload["model"])
		}
		if payload["max_tokens"] != float64(1) {
			t.Fatalf("expected compat probe max_tokens=1, got %#v", payload["max_tokens"])
		}
		if payload["stream"] != false {
			t.Fatalf("expected compat probe stream=false, got %#v", payload["stream"])
		}
		if payload["system"] != "Count carefully." {
			t.Fatalf("expected system prompt preserved, got %#v", payload["system"])
		}
		rawMessages, ok := payload["messages"].([]any)
		if !ok || len(rawMessages) != 1 {
			t.Fatalf("expected one forwarded message, got %#v", payload["messages"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_count","type":"message","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"x"}],"usage":{"input_tokens":21,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6-thinking"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus-4-6-thinking","system":"Count carefully.","messages":[{"role":"user","content":"ping"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-count-tokens-direct"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer user-secret")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Add("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Add("anthropic-beta", "fine-grained-tool-streaming-2025-05-14")

	recorder := httptest.NewRecorder()
	handler.MessageCountTokens(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "claude" {
		t.Fatalf("expected upstream header claude, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestIDHeader); got != "req-count-tokens-direct" {
		t.Fatalf("expected request id header, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if strings.TrimSpace(string(body)) != `{"input_tokens":21}` {
		t.Fatalf("expected token count response, got %q", string(body))
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	record := snapshot.Requests[0]
	if record.Path != "/v1/messages/count_tokens" {
		t.Fatalf("expected count_tokens path, got %q", record.Path)
	}
	if record.RouteMode != "anthropic_count_tokens_compat" {
		t.Fatalf("expected anthropic_count_tokens_compat route mode, got %q", record.RouteMode)
	}
	if record.RequestedModel != "claude-opus-4-6-thinking" {
		t.Fatalf("expected requested model preserved, got %q", record.RequestedModel)
	}
	if record.Model != "claude-opus-4-6-thinking" {
		t.Fatalf("expected effective model to reflect original route model, got %q", record.Model)
	}
	if !record.Success {
		t.Fatalf("expected successful telemetry record")
	}
	if record.Usage.PromptTokens != 21 || record.Usage.TotalTokens != 21 {
		t.Fatalf("expected prompt/total tokens 21, got %+v", record.Usage)
	}
}

func TestMessagesRouteOverridesCallerCredentialsForAnthropic(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected caller authorization header to be stripped, got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("expected upstream x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected default anthropic-version header, got %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "prompt-caching-2024-07-31" {
			t.Fatalf("expected anthropic-beta header to pass through, got %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["model"] != "claude-opus-4-6" {
			t.Fatalf("expected model to stay unchanged, got %#v", payload["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_direct","type":"message","model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":9,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-direct-message-credentials"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer caller-secret")
	req.Header.Set("x-api-key", "caller-key")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(body), `"type":"message"`) || !strings.Contains(string(body), `"text":"ok"`) {
		t.Fatalf("expected anthropic message response, got %q", string(body))
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	record := snapshot.Requests[0]
	if !record.Success {
		t.Fatalf("expected successful request record")
	}
	if record.RouteMode != "direct" {
		t.Fatalf("expected direct route mode, got %q", record.RouteMode)
	}
	if record.Upstream != "claude" {
		t.Fatalf("expected claude upstream, got %q", record.Upstream)
	}
}

func TestMessageCountTokensAnthropicCompatReturns503WhenUsageMissing(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_bad","type":"message","model":"claude-opus-4-6","role":"assistant","content":[{"type":"text","text":"x"}],"usage":{"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1, RetryBackoffMs: 1, RetryBackoffMaxMs: 1},
		Upstreams: []config.Upstream{
			{Name: "claude", BaseURL: upstream.URL, APIKey: "sk-ant", Models: []string{"claude-opus-4-6"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"ping"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-count-tokens-missing-usage"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.MessageCountTokens(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "claude" {
		t.Fatalf("expected upstream header claude, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(body), "anthropic usage missing input_tokens") {
		t.Fatalf("expected missing usage error, got %q", string(body))
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	record := snapshot.Requests[0]
	if record.Success {
		t.Fatalf("expected failed request record")
	}
	if record.RouteMode != "direct" {
		t.Fatalf("expected direct route mode for failed probe, got %q", record.RouteMode)
	}
	if record.Error != "anthropic usage missing input_tokens" {
		t.Fatalf("expected request error to be recorded, got %q", record.Error)
	}
	if len(snapshot.Errors) == 0 {
		t.Fatalf("expected recorded error")
	}
	if snapshot.Errors[0].Message != "anthropic usage missing input_tokens" {
		t.Fatalf("expected error snapshot to capture missing usage, got %q", snapshot.Errors[0].Message)
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

func TestHandlerInfiniteRetryBuffersResponsesEventStreamUntilCompleted(t *testing.T) {
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
		Router: config.RouterConfig{
			Strategy:          "round_robin",
			MaxRetries:        2,
			RetryBackoffMs:    1,
			RetryBackoffMaxMs: 1,
		},
		Proxy: config.ProxyPolicyConfig{
			Retry: config.RetryPolicyConfig{
				InfiniteOnError: true,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "first", BaseURL: first.URL, Models: []string{"gpt-5.4"}, Weight: 1},
			{Name: "second", BaseURL: second.URL, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4","input":"hi","stream":true}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-sse-infinite"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Responses(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "second" {
		t.Fatalf("expected upstream header second, got %q", got)
	}
	attempts, err := strconv.Atoi(resp.Header.Get(observability.AttemptsHeader))
	if err != nil {
		t.Fatalf("expected numeric attempts header, got %q", resp.Header.Get(observability.AttemptsHeader))
	}
	if attempts < 2 {
		t.Fatalf("expected attempts header to reflect retry after incomplete stream, got %d", attempts)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "response.completed") {
		t.Fatalf("expected completed event in buffered response body, got %q", text)
	}
	if strings.Contains(text, "\"resp_1\"") {
		t.Fatalf("expected incomplete first stream not to leak to client, got %q", text)
	}
	if firstCalls.Load() != 1 {
		t.Fatalf("expected first upstream called once, got %d", firstCalls.Load())
	}
	if secondCalls.Load() != 1 {
		t.Fatalf("expected second upstream called once, got %d", secondCalls.Load())
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

func TestHandlerBridgedAnthropicMessagesCompatToChatCompletions(t *testing.T) {
	var messagesCalls atomic.Int32
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			messagesCalls.Add(1)
			if got := r.Header.Get("x-api-key"); got != "sk-gpt" {
				t.Fatalf("expected initial anthropic fallback probe to use x-api-key, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer sk-gpt" {
				t.Fatalf("expected upstream auth header, got %q", got)
			}
			if got := r.Header.Get("anthropic-version"); got != "" {
				t.Fatalf("expected anthropic-version stripped for chat compat, got %q", got)
			}
			if got := r.Header.Get("anthropic-beta"); got != "" {
				t.Fatalf("expected anthropic-beta stripped for chat compat, got %q", got)
			}
			if got := r.Header.Get("x-api-key"); got != "" {
				t.Fatalf("expected x-api-key stripped for chat compat, got %q", got)
			}

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode chat compat body: %v", err)
			}
			if got := payload["model"]; got != "gpt-5.4" {
				t.Fatalf("expected bridged model gpt-5.4, got %#v", got)
			}
			rawMessages, ok := payload["messages"].([]any)
			if !ok || len(rawMessages) != 2 {
				t.Fatalf("expected system + user messages, got %#v", payload["messages"])
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-bridge","object":"chat.completion","created":1700000000,"model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := newTestStore(t)
	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), store)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","system":"You are terse.","max_tokens":64,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-bridge"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli)")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "bridge" {
		t.Fatalf("expected upstream header bridge, got %q", got)
	}
	if got := resp.Header.Get(observability.ModelHeader); got != "gpt-5.4" {
		t.Fatalf("expected effective model header gpt-5.4, got %q", got)
	}
	if got := resp.Header.Get(observability.RequestedModelHeader); got != "claude-opus-4-6" {
		t.Fatalf("expected requested model header claude-opus-4-6, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"type":"message"`) || !strings.Contains(text, `"model":"claude-opus-4-6"`) {
		t.Fatalf("expected anthropic message response, got %q", text)
	}
	if messagesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected one messages probe and one chat compat call, got messages=%d chat=%d", messagesCalls.Load(), chatCalls.Load())
	}

	snapshot := store.Snapshot()
	if len(snapshot.Requests) == 0 {
		t.Fatalf("expected recorded request")
	}
	if got := snapshot.Requests[0].RouteMode; got != "bridged" {
		t.Fatalf("expected route mode bridged, got %q", got)
	}
}

func TestHandlerBridgedAnthropicMessagesCompatToChatCompletionsStream(t *testing.T) {
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode chat compat body: %v", err)
			}
			if got := payload["stream"]; got != false {
				t.Fatalf("expected chat compat request to disable stream, got %#v", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-stream","object":"chat.completion","created":1700000000,"model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-opus-4-6","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"Reply with pong"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-bridge-stream"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli)")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: content_block_delta") || !strings.Contains(text, "event: message_stop") {
		t.Fatalf("expected anthropic SSE body, got %q", text)
	}
	if strings.Contains(text, "[DONE]") {
		t.Fatalf("expected anthropic SSE, not OpenAI SSE, got %q", text)
	}
	if chatCalls.Load() != 1 {
		t.Fatalf("expected one chat compat call, got %d", chatCalls.Load())
	}
}

func TestHandlerBridgedAnthropicMessagesCompatPreservesTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode compat request: %v", err)
			}
			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("expected one forwarded tool, got %#v", payload["tools"])
			}
			messages, ok := payload["messages"].([]any)
			if !ok || len(messages) < 1 {
				t.Fatalf("expected forwarded messages, got %#v", payload["messages"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"id":"chatcmpl-tool",
				"object":"chat.completion",
				"model":"gpt-5.4",
				"choices":[
					{
						"index":0,
						"message":{
							"role":"assistant",
							"content":"I'll inspect that.",
							"tool_calls":[
								{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}}
							]
						},
						"finish_reason":"tool_calls"
					}
				],
				"usage":{"prompt_tokens":11,"completion_tokens":4,"total_tokens":15}
			}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-opus-4-6",
		"max_tokens":64,
		"tools":[{"name":"bash","description":"Run shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}],
		"tool_choice":{"type":"tool","name":"bash"},
		"messages":[{"role":"user","content":"check cwd"}]
	}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-anthropic-bridge-tool"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"type":"tool_use"`) || !strings.Contains(text, `"name":"bash"`) {
		t.Fatalf("expected anthropic tool_use response, got %q", text)
	}
}

func TestHandlerBridgedAnthropicMessagesCompatPreservesImages(t *testing.T) {
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode compat body: %v", err)
			}
			rawMessages, ok := payload["messages"].([]any)
			if !ok || len(rawMessages) != 1 {
				t.Fatalf("expected one forwarded message, got %#v", payload["messages"])
			}
			message, _ := rawMessages[0].(map[string]any)
			content, ok := message["content"].([]any)
			if !ok || len(content) != 2 {
				t.Fatalf("expected text + image chat content, got %#v", message["content"])
			}
			image, _ := content[1].(map[string]any)
			imageURL, _ := image["image_url"].(map[string]any)
			if imageURL["url"] != "https://example.com/cat.png" {
				t.Fatalf("expected image_url preserved, got %#v", image["image_url"])
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-image","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"seen"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		Bridge: config.ModelBridgeConfig{
			Enabled: true,
			Rules: []config.ModelBridgeRule{
				{From: "claude-opus-4-6", To: "gpt-5.4"},
			},
		},
		Router: config.RouterConfig{Strategy: "round_robin", MaxRetries: 1},
		Upstreams: []config.Upstream{
			{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	handler := NewHandler(router.NewManager(state.NewConfigStore(cfg)), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe"},
				{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}
			]}
		]
	}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-bridge-image"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Messages(recorder, req)

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if chatCalls.Load() != 1 {
		t.Fatalf("expected one chat compat call, got %d", chatCalls.Load())
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

func TestHandlerCancelsDisabledInFlightUpstreamAndFailsOver(t *testing.T) {
	var badCalls atomic.Int32
	blocked := make(chan struct{}, 1)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		select {
		case blocked <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer bad.Close()

	var goodCalls atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-failover","object":"chat.completion"}`)
	}))
	defer good.Close()

	badEnabled := true
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:          "round_robin",
			MaxRetries:        1,
			RetryBackoffMs:    1,
			RetryBackoffMaxMs: 1,
		},
		Upstreams: []config.Upstream{
			{Name: "bad", BaseURL: bad.URL, Models: []string{"gpt-4o-mini"}, Weight: 1, Enabled: &badEnabled},
			{Name: "good", BaseURL: good.URL, Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	store := state.NewConfigStore(cfg)
	handler := NewHandler(router.NewManager(store), newTestStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`))
	req = req.WithContext(observability.WithRequestID(req.Context(), "req-disable-failover"))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ChatCompletions(recorder, req)
		close(done)
	}()

	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("expected bad upstream request to start")
	}

	disabled := false
	updated := cfg
	updated.Upstreams = append([]config.Upstream(nil), cfg.Upstreams...)
	updated.Upstreams[0].Enabled = &disabled
	store.Set(updated)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("expected request to fail over after disabling bad upstream")
	}

	resp := recorder.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get(observability.UpstreamHeader); got != "good" {
		t.Fatalf("expected failover to good upstream, got %q", got)
	}
	if badCalls.Load() != 1 {
		t.Fatalf("expected bad upstream called once, got %d", badCalls.Load())
	}
	if goodCalls.Load() != 1 {
		t.Fatalf("expected good upstream called once, got %d", goodCalls.Load())
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

func TestInspectEventStreamResponseRejectsIncompleteAnthropicMessagesStream(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\"}\n\n")),
	}

	cfg := config.Config{}
	cfg.Normalize()

	assessment, _, err := inspectEventStreamResponse(resp, "/v1/messages", cfg.Proxy)
	if err != nil {
		t.Fatalf("inspect stream: %v", err)
	}
	if !assessment.ErrorBody || !assessment.Retryable {
		t.Fatalf("expected incomplete anthropic stream to be retryable failure, got %+v", assessment)
	}
	if assessment.Message != "stream disconnected before completion: stream closed before message_stop" {
		t.Fatalf("unexpected assessment message %q", assessment.Message)
	}
}

func TestBuildAnthropicCountTokensProbeBodyRewritesOpusAndPreservesPayload(t *testing.T) {
	probe, err := buildAnthropicCountTokensProbeBody([]byte(`{
		"model":"claude-opus-4-6",
		"system":"Count carefully.",
		"messages":[{"role":"user","content":"hello"}],
		"metadata":{"tenant":"demo"},
		"max_tokens":256,
		"stream":true
	}`))
	if err != nil {
		t.Fatalf("build probe body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(probe, &payload); err != nil {
		t.Fatalf("unmarshal probe body: %v", err)
	}
	if payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("expected opus rewrite to sonnet, got %#v", payload["model"])
	}
	if payload["max_tokens"] != float64(1) {
		t.Fatalf("expected max_tokens=1, got %#v", payload["max_tokens"])
	}
	if payload["stream"] != false {
		t.Fatalf("expected stream=false, got %#v", payload["stream"])
	}
	if payload["system"] != "Count carefully." {
		t.Fatalf("expected system prompt preserved, got %#v", payload["system"])
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok || metadata["tenant"] != "demo" {
		t.Fatalf("expected metadata preserved, got %#v", payload["metadata"])
	}
}

func TestBuildAnthropicMessagesFromChatConvertsSystemMessagesAndStopSequences(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"system","content":"Follow policy."},
			{"role":"system","content":[{"type":"text","text":"Use concise replies."}]},
			{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":" world"}]},
			{"role":"assistant","content":"ok"}
		],
		"max_tokens":42,
		"temperature":0.2,
		"top_p":0.8,
		"stop":["END","STOP"],
		"stream":true
	}`)

	anthropicBody, stream, err := buildAnthropicMessagesFromChat(body, "")
	if err != nil {
		t.Fatalf("build anthropic body: %v", err)
	}
	if !stream {
		t.Fatalf("expected stream flag to be returned")
	}

	var payload map[string]any
	if err := json.Unmarshal(anthropicBody, &payload); err != nil {
		t.Fatalf("unmarshal anthropic body: %v", err)
	}
	if payload["model"] != "claude-opus-4-6" {
		t.Fatalf("expected model to be preserved, got %#v", payload["model"])
	}
	if payload["system"] != "Follow policy.\n\nUse concise replies." {
		t.Fatalf("expected joined system prompt, got %#v", payload["system"])
	}
	if payload["max_tokens"] != float64(42) {
		t.Fatalf("expected max_tokens=42, got %#v", payload["max_tokens"])
	}
	if payload["temperature"] != 0.2 {
		t.Fatalf("expected temperature to pass through, got %#v", payload["temperature"])
	}
	if payload["top_p"] != 0.8 {
		t.Fatalf("expected top_p to pass through, got %#v", payload["top_p"])
	}
	if payload["stream"] != true {
		t.Fatalf("expected stream=true, got %#v", payload["stream"])
	}

	stopSequences, ok := payload["stop_sequences"].([]any)
	if !ok || len(stopSequences) != 2 || stopSequences[0] != "END" || stopSequences[1] != "STOP" {
		t.Fatalf("expected stop sequences to be mapped from chat stop, got %#v", payload["stop_sequences"])
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected two non-system messages, got %#v", payload["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok || first["role"] != "user" || first["content"] != "hello world" {
		t.Fatalf("expected normalized user message, got %#v", messages[0])
	}
	second, ok := messages[1].(map[string]any)
	if !ok || second["role"] != "assistant" || second["content"] != "ok" {
		t.Fatalf("expected normalized assistant message, got %#v", messages[1])
	}
}

func TestBuildAnthropicMessagesFromChatPreservesImagesToolsAndToolResults(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}
			]},
			{"role":"assistant","content":"calling tool","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"vision","arguments":"{\"mode\":\"describe\"}"}}
			]},
			{"role":"tool","tool_call_id":"call_1","content":"cat"}
		]
	}`)

	anthropicBody, _, err := buildAnthropicMessagesFromChat(body, "")
	if err != nil {
		t.Fatalf("build anthropic body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(anthropicBody, &payload); err != nil {
		t.Fatalf("unmarshal anthropic body: %v", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("expected 3 anthropic messages, got %#v", payload["messages"])
	}

	first, _ := messages[0].(map[string]any)
	firstContent, ok := first["content"].([]any)
	if !ok || len(firstContent) != 2 {
		t.Fatalf("expected user text + image blocks, got %#v", first["content"])
	}
	image, _ := firstContent[1].(map[string]any)
	source, _ := image["source"].(map[string]any)
	if source["type"] != "url" || source["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected user image preserved, got %#v", image["source"])
	}

	second, _ := messages[1].(map[string]any)
	secondContent, ok := second["content"].([]any)
	if !ok || len(secondContent) != 2 {
		t.Fatalf("expected assistant text + tool_use, got %#v", second["content"])
	}
	toolUse, _ := secondContent[1].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["name"] != "vision" {
		t.Fatalf("expected assistant tool_use, got %#v", toolUse)
	}

	third, _ := messages[2].(map[string]any)
	thirdContent, ok := third["content"].([]any)
	if !ok || len(thirdContent) != 1 {
		t.Fatalf("expected tool_result block, got %#v", third["content"])
	}
	toolResult, _ := thirdContent[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" || toolResult["content"] != "cat" {
		t.Fatalf("expected tool result preserved, got %#v", toolResult)
	}
}

func TestBuildCountTokensResponseFromAnthropicRequiresInputTokens(t *testing.T) {
	if _, _, err := buildCountTokensResponseFromAnthropic([]byte(`{"usage":{"input_tokens":0}}`)); err == nil {
		t.Fatalf("expected missing input_tokens to fail")
	}
}

func TestBuildChatFromAnthropicFlattensTextContentAndUsage(t *testing.T) {
	chat, err := buildChatFromAnthropic([]byte(`{
		"id":"msg_123",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4-6",
		"content":[
			{"type":"text","text":"first"},
			{"type":"tool_use","text":"skip me"},
			{"type":"text","text":"second"}
		],
		"usage":{"input_tokens":11,"output_tokens":5}
	}`), "claude-opus-4-6")
	if err != nil {
		t.Fatalf("build chat from anthropic: %v", err)
	}

	if chat["id"] != "msg_123" {
		t.Fatalf("expected chat id preserved, got %#v", chat["id"])
	}
	if chat["model"] != "claude-opus-4-6" {
		t.Fatalf("expected explicit override model, got %#v", chat["model"])
	}

	choices, ok := chat["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("expected one choice, got %#v", chat["choices"])
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		t.Fatalf("expected choice payload, got %#v", choices[0])
	}
	message, ok := choice["message"].(map[string]any)
	if !ok {
		t.Fatalf("expected message payload, got %#v", choice["message"])
	}
	if message["content"] != "first\n\nsecond" {
		t.Fatalf("expected text blocks to be flattened, got %#v", message["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Fatalf("expected stop finish reason, got %#v", choice["finish_reason"])
	}

	usage, ok := chat["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage payload, got %#v", chat["usage"])
	}
	if usage["prompt_tokens"] != 11 || usage["completion_tokens"] != 5 || usage["total_tokens"] != 16 {
		t.Fatalf("expected anthropic usage to map into chat usage, got %#v", usage)
	}
}

func TestBuildChatFromAnthropicPreservesToolUseBlocks(t *testing.T) {
	chat, err := buildChatFromAnthropic([]byte(`{
		"id":"msg_tool",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4-6",
		"content":[
			{"type":"text","text":"checking"},
			{"type":"tool_use","id":"toolu_1","name":"bash","input":{"cmd":"pwd"}}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":9,"output_tokens":4}
	}`), "")
	if err != nil {
		t.Fatalf("build chat from anthropic: %v", err)
	}

	choices, ok := chat["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("expected one choice, got %#v", chat["choices"])
	}
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", choice["finish_reason"])
	}
	message, _ := choice["message"].(map[string]any)
	if message["content"] != "checking" {
		t.Fatalf("expected assistant text preserved, got %#v", message["content"])
	}
	toolCalls := chatToolCallsRaw(message["tool_calls"])
	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", message["tool_calls"])
	}
}

func TestBuildChatCompletionsFromAnthropicPreservesImageBlocks(t *testing.T) {
	chat, _, err := buildChatCompletionsFromAnthropic([]byte(`{
		"model":"gpt-5.4",
		"messages":[
			{"role":"user","content":[
				{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}},
				{"type":"text","text":"describe"}
			]}
		]
	}`), "")
	if err != nil {
		t.Fatalf("build chat completions from anthropic: %v", err)
	}

	messages, ok := chat["messages"].([]map[string]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one chat message, got %#v", chat["messages"])
	}
	content, ok := messages[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected multimodal chat content, got %#v", messages[0]["content"])
	}
	image, _ := content[0].(map[string]any)
	imageURL, _ := image["image_url"].(map[string]any)
	if imageURL["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected image url preserved, got %#v", image["image_url"])
	}
}

func TestBuildChatCompletionsFromAnthropicPreservesBase64ImageBlocks(t *testing.T) {
	chat, _, err := buildChatCompletionsFromAnthropic([]byte(`{
		"model":"gpt-5.4",
		"messages":[
			{"role":"user","content":[
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},
				{"type":"text","text":"describe"}
			]}
		]
	}`), "")
	if err != nil {
		t.Fatalf("build chat completions from anthropic: %v", err)
	}

	messages, ok := chat["messages"].([]map[string]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one chat message, got %#v", chat["messages"])
	}
	content, ok := messages[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected multimodal chat content, got %#v", messages[0]["content"])
	}
	image, _ := content[0].(map[string]any)
	imageURL, _ := image["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,YWJj" {
		t.Fatalf("expected base64 image url preserved, got %#v", image["image_url"])
	}
}

func TestBuildChatCompletionsFromResponsesPreservesStructuredImageBlocks(t *testing.T) {
	chat, stream, err := buildChatCompletionsFromResponses([]byte(`{
		"model":"claude-sonnet-4-6",
		"stream":true,
		"input":[
			{
				"role":"user",
				"content":[
					{"type":"input_text","text":"describe"},
					{"type":"input_image","image_url":"https://example.com/cat.png"}
				]
			}
		]
	}`), "")
	if err != nil {
		t.Fatalf("build chat completions from responses: %v", err)
	}
	if !stream {
		t.Fatalf("expected stream=true")
	}
	messages, ok := chat["messages"].([]map[string]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one chat message, got %#v", chat["messages"])
	}
	content, ok := messages[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected text + image chat content, got %#v", messages[0]["content"])
	}
	image, _ := content[1].(map[string]any)
	imageURL, _ := image["image_url"].(map[string]any)
	if imageURL["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected structured image preserved, got %#v", image["image_url"])
	}
}

func TestBuildAnthropicMessagesFromChatPreservesDataURLImages(t *testing.T) {
	body := []byte(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"}}
			]}
		]
	}`)

	anthropicBody, _, err := buildAnthropicMessagesFromChat(body, "")
	if err != nil {
		t.Fatalf("build anthropic body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(anthropicBody, &payload); err != nil {
		t.Fatalf("unmarshal anthropic body: %v", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected one anthropic message, got %#v", payload["messages"])
	}
	message, _ := messages[0].(map[string]any)
	content, ok := message["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected text + image blocks, got %#v", message["content"])
	}
	image, _ := content[1].(map[string]any)
	source, _ := image["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "YWJj" {
		t.Fatalf("expected base64 image source, got %#v", image["source"])
	}
}

func TestBuildResponsesFromChatPreservesFunctionCalls(t *testing.T) {
	response, _, err := buildResponsesFromChat([]byte(`{
		"id":"chatcmpl_tools",
		"object":"chat.completion",
		"model":"gpt-5.4",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"working",
					"tool_calls":[
						{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}}
					]
				},
				"finish_reason":"tool_calls"
			}
		],
		"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}
	}`), "")
	if err != nil {
		t.Fatalf("build responses from chat: %v", err)
	}

	output, ok := response["output"].([]any)
	if !ok || len(output) != 2 {
		t.Fatalf("expected message + function_call output, got %#v", response["output"])
	}
	functionCall, _ := output[1].(map[string]any)
	if functionCall["type"] != "function_call" || functionCall["call_id"] != "call_1" || functionCall["name"] != "bash" {
		t.Fatalf("expected function_call output item, got %#v", functionCall)
	}
}

func TestBuildChatCompletionsFromAnthropicPreservesToolsAndToolResults(t *testing.T) {
	chat, stream, err := buildChatCompletionsFromAnthropic([]byte(`{
		"model":"gpt-5.4",
		"system":"You are an agent.",
		"stream":true,
		"tools":[{"name":"bash","description":"Run shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}],
		"tool_choice":{"type":"tool","name":"bash"},
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_1","name":"bash","input":{"cmd":"pwd"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"C:/repo"},{"type":"text","text":"continue"}]}
		]
	}`), "")
	if err != nil {
		t.Fatalf("build chat completions from anthropic: %v", err)
	}
	if !stream {
		t.Fatalf("expected stream=true")
	}
	if chat["model"] != "gpt-5.4" {
		t.Fatalf("expected model gpt-5.4, got %#v", chat["model"])
	}
	tools, ok := chat["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one converted tool, got %#v", chat["tools"])
	}
	function, ok := tools[0]["function"].(map[string]any)
	if !ok || function["name"] != "bash" {
		t.Fatalf("expected bash function tool, got %#v", tools[0])
	}
	toolChoice, ok := chat["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("expected specific tool choice, got %#v", chat["tool_choice"])
	}
	choiceFunction, ok := toolChoice["function"].(map[string]any)
	if !ok || choiceFunction["name"] != "bash" {
		t.Fatalf("expected tool choice bash, got %#v", toolChoice)
	}
	messages, ok := chat["messages"].([]map[string]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("expected system + assistant + tool + user messages, got %#v", chat["messages"])
	}
	assistant := messages[1]
	if assistant["role"] != "assistant" {
		t.Fatalf("expected assistant role, got %#v", assistant)
	}
	if assistant["content"] != "checking" {
		t.Fatalf("expected assistant text preserved, got %#v", assistant["content"])
	}
	toolCalls, ok := assistant["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool call preserved, got %#v", assistant["tool_calls"])
	}
	toolMessage := messages[2]
	if toolMessage["role"] != "tool" || toolMessage["tool_call_id"] != "toolu_1" {
		t.Fatalf("expected tool result message, got %#v", toolMessage)
	}
	userMessage := messages[3]
	if userMessage["role"] != "user" || userMessage["content"] != "continue" {
		t.Fatalf("expected trailing user text message, got %#v", userMessage)
	}
}

func TestBuildAnthropicMessageFromChatIncludesToolUseBlocks(t *testing.T) {
	message, err := buildAnthropicMessageFromChat([]byte(`{
		"id":"chatcmpl_1",
		"model":"gpt-5.4",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"working",
					"tool_calls":[
						{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"pwd\"}"}}
					]
				},
				"finish_reason":"tool_calls"
			}
		],
		"usage":{"prompt_tokens":7,"completion_tokens":3}
	}`), "claude-opus-4-6")
	if err != nil {
		t.Fatalf("build anthropic message from chat: %v", err)
	}
	if message["model"] != "claude-opus-4-6" {
		t.Fatalf("expected overridden model, got %#v", message["model"])
	}
	if message["stop_reason"] != "tool_use" {
		t.Fatalf("expected tool_use stop reason, got %#v", message["stop_reason"])
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected text + tool_use blocks, got %#v", message["content"])
	}
	second, ok := content[1].(map[string]any)
	if !ok || second["type"] != "tool_use" || second["name"] != "bash" {
		t.Fatalf("expected tool_use block, got %#v", content[1])
	}
	input, ok := second["input"].(map[string]any)
	if !ok || input["cmd"] != "pwd" {
		t.Fatalf("expected parsed tool input, got %#v", second["input"])
	}

	stream := string(marshalAnthropicMessageCompatStream(message))
	if !strings.Contains(stream, `"type":"tool_use"`) || !strings.Contains(stream, "event: message_stop") {
		t.Fatalf("expected tool_use in anthropic stream, got %q", stream)
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

func TestResolveModelFromJSONKeepsOriginalBodyWhenRewriteNotNeeded(t *testing.T) {
	cfg := config.Config{}
	cfg.Normalize()

	body := []byte(`{"model":"gpt-5.2-codex","input":"hi"}`)
	resolved, err := resolveModel(body, "application/json", "Codex Desktop/0.112.0-alpha.3", cfg, forwardOptions{
		ModelRequired: true,
	})
	if err != nil {
		t.Fatalf("resolve model: %v", err)
	}
	if resolved.Requested != "gpt-5.2-codex" || resolved.Effective != "gpt-5.2-codex" {
		t.Fatalf("unexpected model resolution: %+v", resolved)
	}
	if string(resolved.Body) != string(body) {
		t.Fatalf("expected original json body to remain unchanged")
	}
}

func TestResolveModelFromMultipartKeepsOriginalBodyWhenRewriteNotNeeded(t *testing.T) {
	cfg := config.Config{}
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

	original := body.Bytes()
	contentType := writer.FormDataContentType()
	resolved, err := resolveModel(original, contentType, "Codex Desktop/0.112.0-alpha.3", cfg, forwardOptions{
		ModelRequired: true,
	})
	if err != nil {
		t.Fatalf("resolve multipart model: %v", err)
	}
	if resolved.Requested != "gpt-4o-mini-transcribe" || resolved.Effective != "gpt-4o-mini-transcribe" {
		t.Fatalf("unexpected multipart model resolution: %+v", resolved)
	}
	if resolved.ContentType != contentType {
		t.Fatalf("expected original content type, got %q", resolved.ContentType)
	}
	if string(resolved.Body) != string(original) {
		t.Fatalf("expected original multipart body to remain unchanged")
	}
}

func TestExtractStickyRoutingKeyPrefersPreviousResponseID(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","previous_response_id":"resp_prev","response_id":"resp_current"}`)
	if got := extractStickyRoutingKey("/v1/responses", body, "application/json"); got != "resp_prev" {
		t.Fatalf("expected previous_response_id to win, got %q", got)
	}

	body = []byte(`{"model":"gpt-5.4","response_id":"resp_current"}`)
	if got := extractStickyRoutingKey("/v1/responses", body, "application/json"); got != "resp_current" {
		t.Fatalf("expected response_id fallback, got %q", got)
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
