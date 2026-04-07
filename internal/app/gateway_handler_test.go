package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/core"
	v2httpserver "ai-model-gateway/internal/infra/httpserver"

	"github.com/go-chi/chi/v5"
)

type recordingPipeline struct {
	requests []*core.GatewayRequest
	resp     *core.GatewayResponse
	err      error
}

func (p *recordingPipeline) Handle(_ context.Context, req *core.GatewayRequest) (*core.GatewayResponse, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return nil, p.err
	}
	if p.resp != nil {
		return p.resp, nil
	}
	return &core.GatewayResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

type zeroReadCloser struct {
	remaining int64
}

func (r *zeroReadCloser) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	for i := int64(0); i < n; i++ {
		p[i] = 'a'
	}
	r.remaining -= n
	return int(n), nil
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

type multipartPayload struct {
	body        []byte
	contentType string
}

func mustMultipartPayload(t *testing.T, fields map[string]string, fileField string, fileName string, fileBody string) multipartPayload {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field %s: %v", key, err)
		}
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create form file %s: %v", fileField, err)
		}
		if _, err := io.WriteString(part, fileBody); err != nil {
			t.Fatalf("write form file %s: %v", fileField, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	return multipartPayload{
		body:        body.Bytes(),
		contentType: writer.FormDataContentType(),
	}
}

func TestMountGatewayRoutes_RejectsUnknownPaths(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	sel := NewRouteSelector(core.RoutingConfig{}, nil)
	MountGatewayRoutes(router, pl, sel)

	req := httptest.NewRequest(http.MethodPost, "/v1/not-real", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if got := len(pl.requests); got != 0 {
		t.Fatalf("expected pipeline not to be called, got %d requests", got)
	}
}

func TestMountGatewayRoutes_UsesExplicitRouteContracts(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	tr := true
	sel := NewRouteSelector(core.RoutingConfig{}, []core.Provider{
		{Name: "upstream", Models: []string{"gpt-4o"}, Enabled: &tr},
	})
	MountGatewayRoutes(router, pl, sel)

	cases := []struct {
		name         string
		method       string
		target       string
		body         io.Reader
		contentType  string
		wantModel    string
		wantRequired bool
		wantSkip     bool
	}{
		{
			name:         "chat completions requires model",
			method:       http.MethodPost,
			target:       "/v1/chat/completions",
			body:         bytes.NewBufferString(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`),
			contentType:  "application/json",
			wantModel:    "gpt-4o",
			wantRequired: true,
		},
		{
			name:         "messages requires model",
			method:       http.MethodPost,
			target:       "/v1/messages",
			body:         bytes.NewBufferString(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`),
			contentType:  "application/json",
			wantModel:    "claude-opus-4-6",
			wantRequired: true,
		},
		{
			name:         "messages count tokens requires model",
			method:       http.MethodPost,
			target:       "/v1/messages/count_tokens",
			body:         bytes.NewBufferString(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"hi"}]}`),
			contentType:  "application/json",
			wantModel:    "claude-opus-4-6",
			wantRequired: true,
		},
		{
			name:         "responses compact skips bridge rewrite",
			method:       http.MethodPost,
			target:       "/v1/responses/compact",
			body:         bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"checkpoint"}`),
			contentType:  "application/json",
			wantModel:    "gpt-5.2-codex",
			wantRequired: true,
			wantSkip:     true,
		},
		{
			name:         "response resource allows empty model",
			method:       http.MethodGet,
			target:       "/v1/responses/resp_123",
			wantModel:    "",
			wantRequired: false,
		},
		{
			name:         "image generation allows empty model",
			method:       http.MethodPost,
			target:       "/v1/images/generations",
			body:         bytes.NewBufferString(`{"prompt":"a red bird"}`),
			contentType:  "application/json",
			wantModel:    "",
			wantRequired: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, tc.body)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if got := len(pl.requests); got != 1 {
				t.Fatalf("expected one pipeline request, got %d", got)
			}
			gotReq := pl.requests[0]
			pl.requests = nil

			if gotReq.Model != tc.wantModel {
				t.Fatalf("expected model %q, got %q", tc.wantModel, gotReq.Model)
			}
			if gotReq.ModelRequired != tc.wantRequired {
				t.Fatalf("expected ModelRequired=%t, got %t", tc.wantRequired, gotReq.ModelRequired)
			}
			if gotReq.SkipModelRewrite != tc.wantSkip {
				t.Fatalf("expected SkipModelRewrite=%t, got %t", tc.wantSkip, gotReq.SkipModelRewrite)
			}
			if gotReq.Path != tc.target {
				t.Fatalf("expected path %q, got %q", tc.target, gotReq.Path)
			}
		})
	}
}

func TestMountGatewayRoutes_RegistersRemainingPublicContracts(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	tr := true
	sel := NewRouteSelector(core.RoutingConfig{}, []core.Provider{
		{Name: "upstream", Models: []string{"gpt-4o", "gpt-4o-mini-tts", "gpt-4o-mini-transcribe"}, Enabled: &tr},
	})
	MountGatewayRoutes(router, pl, sel)

	audioTranslation := mustMultipartPayload(t, map[string]string{"model": "gpt-4o-mini-transcribe"}, "file", "speech.wav", "RIFF")
	imageEdit := mustMultipartPayload(t, nil, "image", "image.png", "PNG")
	imageVariation := mustMultipartPayload(t, nil, "image", "image.png", "PNG")
	fileUpload := mustMultipartPayload(t, nil, "file", "input.jsonl", `{"id":"file-1"}`)

	cases := []struct {
		name         string
		method       string
		target       string
		body         []byte
		contentType  string
		wantModel    string
		wantRequired bool
	}{
		{
			name:         "completions",
			method:       http.MethodPost,
			target:       "/v1/completions",
			body:         []byte(`{"model":"gpt-4o","prompt":"hi"}`),
			contentType:  "application/json",
			wantModel:    "gpt-4o",
			wantRequired: true,
		},
		{
			name:         "embeddings",
			method:       http.MethodPost,
			target:       "/v1/embeddings",
			body:         []byte(`{"model":"gpt-4o","input":"hi"}`),
			contentType:  "application/json",
			wantModel:    "gpt-4o",
			wantRequired: true,
		},
		{
			name:         "moderations",
			method:       http.MethodPost,
			target:       "/v1/moderations",
			body:         []byte(`{"input":"hi"}`),
			contentType:  "application/json",
			wantRequired: false,
		},
		{
			name:         "images edits",
			method:       http.MethodPost,
			target:       "/v1/images/edits",
			body:         imageEdit.body,
			contentType:  imageEdit.contentType,
			wantRequired: false,
		},
		{
			name:         "images variations",
			method:       http.MethodPost,
			target:       "/v1/images/variations",
			body:         imageVariation.body,
			contentType:  imageVariation.contentType,
			wantRequired: false,
		},
		{
			name:         "audio speech",
			method:       http.MethodPost,
			target:       "/v1/audio/speech",
			body:         []byte(`{"model":"gpt-4o-mini-tts","input":"hello"}`),
			contentType:  "application/json",
			wantModel:    "gpt-4o-mini-tts",
			wantRequired: true,
		},
		{
			name:         "audio translations",
			method:       http.MethodPost,
			target:       "/v1/audio/translations",
			body:         audioTranslation.body,
			contentType:  audioTranslation.contentType,
			wantModel:    "gpt-4o-mini-transcribe",
			wantRequired: true,
		},
		{
			name:         "files list",
			method:       http.MethodGet,
			target:       "/v1/files",
			wantRequired: false,
		},
		{
			name:         "files upload",
			method:       http.MethodPost,
			target:       "/v1/files",
			body:         fileUpload.body,
			contentType:  fileUpload.contentType,
			wantRequired: false,
		},
		{
			name:         "files resource get",
			method:       http.MethodGet,
			target:       "/v1/files/file-123",
			wantRequired: false,
		},
		{
			name:         "files resource delete",
			method:       http.MethodDelete,
			target:       "/v1/files/file-123",
			wantRequired: false,
		},
		{
			name:         "files content",
			method:       http.MethodGet,
			target:       "/v1/files/file-123/content",
			wantRequired: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body io.Reader
			if tc.body != nil {
				body = bytes.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.target, body)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if got := len(pl.requests); got != 1 {
				t.Fatalf("expected one pipeline request, got %d", got)
			}

			gotReq := pl.requests[0]
			pl.requests = nil

			if gotReq.Path != tc.target {
				t.Fatalf("expected path %q, got %q", tc.target, gotReq.Path)
			}
			if gotReq.Model != tc.wantModel {
				t.Fatalf("expected model %q, got %q", tc.wantModel, gotReq.Model)
			}
			if gotReq.ModelRequired != tc.wantRequired {
				t.Fatalf("expected ModelRequired=%t, got %t", tc.wantRequired, gotReq.ModelRequired)
			}
		})
	}
}

func TestModelsHandler_ReturnsV1CompatiblePayload(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	tr := true
	sel := NewRouteSelector(core.RoutingConfig{}, []core.Provider{
		{Name: "zeta", Models: []string{"z-model"}, Enabled: &tr},
		{Name: "alpha", Models: []string{"a-model"}, Enabled: &tr},
	})
	MountGatewayRoutes(router, pl, sel)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	type modelItem struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	var payload struct {
		Object string      `json:"object"`
		Data   []modelItem `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode models payload: %v", err)
	}
	if payload.Object != "list" {
		t.Fatalf("expected object list, got %q", payload.Object)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("expected two models, got %d", len(payload.Data))
	}
	if payload.Data[0].ID != "a-model" || payload.Data[1].ID != "z-model" {
		t.Fatalf("expected sorted model IDs, got %#v", payload.Data)
	}
	if payload.Data[0].OwnedBy != "ai-model-gateway" || payload.Data[1].OwnedBy != "ai-model-gateway" {
		t.Fatalf("expected owned_by ai-model-gateway, got %#v", payload.Data)
	}
	if payload.Data[0].Created <= 0 || payload.Data[1].Created <= 0 {
		t.Fatalf("expected positive created timestamps, got %#v", payload.Data)
	}
	if payload.Data[0].Created != payload.Data[1].Created {
		t.Fatalf("expected one response timestamp for all models, got %#v", payload.Data)
	}
}

func TestPipelineHandler_ExtractsModelFromMultipartRequests(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	MountGatewayRoutes(router, pl, NewRouteSelector(core.RoutingConfig{}, nil))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", "gpt-4o-mini-transcribe"); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	part, err := writer.CreateFormFile("file", "sample.wav")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(part, "RIFF....WAVE"); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := len(pl.requests); got != 1 {
		t.Fatalf("expected one pipeline request, got %d", got)
	}
	if got := pl.requests[0].Model; got != "gpt-4o-mini-transcribe" {
		t.Fatalf("expected extracted model, got %q", got)
	}
	if !pl.requests[0].ModelRequired {
		t.Fatal("expected multipart audio route to require model")
	}
}

func TestPipelineHandler_ReturnsBadRequestForModelValidationErrors(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{err: core.ErrModelNotFound}
	MountGatewayRoutes(router, pl, NewRouteSelector(core.RoutingConfig{}, nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPipelineHandler_PreservesInvalidRequestErrorForOrdinaryBodyReadFailure(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	MountGatewayRoutes(router, pl, NewRouteSelector(core.RoutingConfig{}, nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(observability.RequestIDHeader, "req-body-read-error")
	req.Body = &errReadCloser{err: errors.New("boom")}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if got := rec.Header().Get(observability.RequestIDHeader); got != "req-body-read-error" {
		t.Fatalf("expected request id header, got %q", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	errorMap, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested error payload, got %#v", payload["error"])
	}
	if errorMap["type"] != "invalid_request_error" {
		t.Fatalf("expected invalid_request_error, got %#v", errorMap["type"])
	}
	if errorMap["message"] != "read request body: boom" {
		t.Fatalf("unexpected error message %#v", errorMap["message"])
	}
}

func TestPipelineHandler_RejectsRequestBodyOverLimitWith413(t *testing.T) {
	srv := v2httpserver.New(core.ServerConfig{})
	pl := &recordingPipeline{}
	MountGatewayRoutes(srv.Router(), pl, NewRouteSelector(core.RoutingConfig{}, nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(observability.RequestIDHeader, "req-too-large")
	req.Body = &zeroReadCloser{remaining: 17}

	rec := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16)
		srv.Router().ServeHTTP(w, r)
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
	if got := rec.Header().Get(observability.RequestIDHeader); got != "req-too-large" {
		t.Fatalf("expected request id header, got %q", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	errorMap, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested error payload, got %#v", payload["error"])
	}
	if errorMap["type"] != "request_too_large" {
		t.Fatalf("expected request_too_large, got %#v", errorMap["type"])
	}
}

func TestGatewayRoute_ResourcePathsPreserveQueryString(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/responses/resp_123" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.URL.RawQuery != "include=output%2Cusage&expand=steps" {
			t.Fatalf("expected query string to be preserved, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_123","object":"response"}`)
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "alpha", BaseURL: upstream.URL, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		Health:     core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123?include=output%2Cusage&expand=steps", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGatewayRoute_AddsObservabilityHeadersAndForwardsRequestID(t *testing.T) {
	var upstreamRequestID atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamRequestID.Store(r.Header.Get(observability.RequestIDHeader))

		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.Model != "gpt-4o" {
			t.Fatalf("expected rewritten model gpt-4o, got %q", payload.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-123","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "alpha", BaseURL: upstream.URL, Models: []string{"gpt-4o"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		Health:     core.HealthCheckConfig{Enabled: false},
	}
	compat := core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "gpt-4", To: "gpt-4o"},
			},
		},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(compat),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(compat),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	gotRequestID := rec.Header().Get(observability.RequestIDHeader)
	if strings.TrimSpace(gotRequestID) == "" {
		t.Fatal("expected gateway to set request ID header")
	}
	if gotUpstreamID, _ := upstreamRequestID.Load().(string); gotUpstreamID != gotRequestID {
		t.Fatalf("expected upstream request ID %q to match response header %q", gotUpstreamID, gotRequestID)
	}
	if got := rec.Header().Get(observability.UpstreamHeader); got != "alpha" {
		t.Fatalf("expected upstream header alpha, got %q", got)
	}
	if got := rec.Header().Get(observability.AttemptsHeader); got != "1" {
		t.Fatalf("expected attempts header 1, got %q", got)
	}
	if got := rec.Header().Get(observability.ModelHeader); got != "gpt-4o" {
		t.Fatalf("expected model header gpt-4o, got %q", got)
	}
	if got := rec.Header().Get(observability.RequestedModelHeader); got != "gpt-4" {
		t.Fatalf("expected requested model header gpt-4, got %q", got)
	}
}

func TestMountGatewayRoutes_ExtractsStickyKeyForResponsesRequests(t *testing.T) {
	router := chi.NewRouter()
	pl := &recordingPipeline{}
	MountGatewayRoutes(router, pl, NewRouteSelector(core.RoutingConfig{}, nil))

	cases := []struct {
		name       string
		method     string
		target     string
		body       io.Reader
		wantSticky string
	}{
		{
			name:       "responses prefers previous response id",
			method:     http.MethodPost,
			target:     "/v1/responses",
			body:       bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"follow up","previous_response_id":"resp_prev","response_id":"resp_cur"}`),
			wantSticky: "resp_prev",
		},
		{
			name:       "responses compact falls back to response id",
			method:     http.MethodPost,
			target:     "/v1/responses/compact",
			body:       bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"checkpoint","response_id":"resp_cur"}`),
			wantSticky: "resp_cur",
		},
		{
			name:       "response resource uses path id",
			method:     http.MethodGet,
			target:     "/v1/responses/resp_path",
			wantSticky: "resp_path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, tc.body)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if got := len(pl.requests); got != 1 {
				t.Fatalf("expected one pipeline request, got %d", got)
			}
			gotReq := pl.requests[0]
			pl.requests = nil

			if gotReq.StickyKey != tc.wantSticky {
				t.Fatalf("expected sticky key %q, got %q", tc.wantSticky, gotReq.StickyKey)
			}
		})
	}
}

func TestResponsesReuseStickyUpstreamFromPreviousResponseID(t *testing.T) {
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

	tr := true
	providers := []core.Provider{
		{Name: "alpha", BaseURL: alpha.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
		{Name: "beta", BaseURL: beta.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		StickySessions: core.StickySessionConfig{
			Enabled: true,
			TTLSec:  1800,
		},
		Health: core.HealthCheckConfig{Enabled: false},
	}

	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	first := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"hi"}`))
	first.Header.Set("Content-Type", "application/json")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first response 200, got %d", firstRec.Code)
	}

	second := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"follow up","previous_response_id":"resp_alpha"}`))
	second.Header.Set("Content-Type", "application/json")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("expected second response 200, got %d", secondRec.Code)
	}

	if alphaCalls.Load() != 2 {
		t.Fatalf("expected alpha to handle both sticky requests, got %d calls", alphaCalls.Load())
	}
	if betaCalls.Load() != 0 {
		t.Fatalf("expected beta to be skipped by sticky routing, got %d calls", betaCalls.Load())
	}
}

func TestResponsesCompactSkipsBridgeRewrite(t *testing.T) {
	var receivedModel atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		receivedModel.Store(payload.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_compact","object":"response.compaction"}`)
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "codex", BaseURL: upstream.URL, Models: []string{"gpt-5.2-codex"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		Health:     core.HealthCheckConfig{Enabled: false},
	}
	compat := core.CompatConfig{
		Bridge: core.BridgeConfig{
			Enabled: true,
			Rules: []core.BridgeRule{
				{From: "*", To: "gpt-5.4"},
			},
		},
	}

	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(compat),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(compat),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewBufferString(`{"model":"gpt-5.2-codex","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got, _ := receivedModel.Load().(string); got != "gpt-5.2-codex" {
		t.Fatalf("expected upstream model gpt-5.2-codex, got %q", got)
	}
}

func TestMessagesCountTokensCompatSuccess(t *testing.T) {
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("expected count_tokens probe to use AnthropicBaseURL")
	}))
	defer openAIUpstream.Close()

	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("expected rewritten upstream path /v1/messages, got %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected caller authorization stripped, got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode probe body: %v", err)
		}
		if got := payload["model"]; got != "claude-sonnet-4-6" {
			t.Fatalf("expected rewritten probe model claude-sonnet-4-6, got %#v", got)
		}
		if got := payload["max_tokens"]; got != float64(1) {
			t.Fatalf("expected probe max_tokens 1, got %#v", got)
		}
		if got := payload["stream"]; got != false {
			t.Fatalf("expected probe stream=false, got %#v", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_count","type":"message","usage":{"input_tokens":21,"output_tokens":1}}`)
	}))
	defer anthropicUpstream.Close()

	tr := true
	providers := []core.Provider{
		{
			Name:             "claude",
			BaseURL:          openAIUpstream.URL,
			AnthropicBaseURL: anthropicUpstream.URL,
			APIKey:           "sk-ant",
			Models:           []string{"claude-opus-4-6-thinking"},
			Weight:           1,
			TimeoutMs:        5000,
			Enabled:          &tr,
		},
	}
	routing := core.RoutingConfig{
		Strategy:   core.StrategyHealthWeightedRR,
		MaxRetries: 1,
		Health:     core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewBufferString(`{"model":"claude-opus-4-6-thinking","system":"Count carefully.","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer caller-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "{\"input_tokens\":21}" {
		t.Fatalf("expected token count response, got %q", got)
	}
}

func TestMessagesCountTokensCompatMissingUsageReturns503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_bad","type":"message","usage":{"output_tokens":1}}`)
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{
			Name:      "claude",
			BaseURL:   upstream.URL,
			APIKey:    "sk-ant",
			Models:    []string{"claude-opus-4-6"},
			Weight:    1,
			TimeoutMs: 5000,
			Enabled:   &tr,
		},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewBufferString(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "anthropic usage missing input_tokens") {
		t.Fatalf("expected missing usage error, got %q", rec.Body.String())
	}
}

func TestChatCompletionsAnthropicMessagesCompatFallbackForNonClaudeWhenAnthropicBaseURLConfigured(t *testing.T) {
	var chatCalls atomic.Int32
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected OpenAI path %q", r.URL.Path)
		}
		chatCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"forbidden"}}`)
	}))
	defer openAIUpstream.Close()

	var messagesCalls atomic.Int32
	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected anthropic path %q", r.URL.Path)
		}
		messagesCalls.Add(1)
		if got := r.Header.Get("x-api-key"); got != "sk-kimi" {
			t.Fatalf("expected anthropic x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected anthropic-version header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_kimi_chat","type":"message","model":"kimi-for-coding","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2}}`)
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
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"object":"chat.completion"`) || !strings.Contains(body, `"content":"ok"`) {
		t.Fatalf("expected chat payload with ok, got %q", body)
	}
	if chatCalls.Load() != 1 || messagesCalls.Load() != 1 {
		t.Fatalf("expected one chat call and one anthropic messages call, got chat=%d messages=%d", chatCalls.Load(), messagesCalls.Load())
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
			if got := r.Header.Get("x-api-key"); got != "sk-ant" {
				t.Fatalf("expected anthropic x-api-key header, got %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			anthropicRequestBody.Store(string(body))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_456","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":13,"output_tokens":1}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
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
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"Reply with exactly ok"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected chat completion chunk payload, got %q", body)
	}
	if !strings.Contains(body, `"content":"ok"`) {
		t.Fatalf("expected streamed content ok, got %q", body)
	}
	if !strings.Contains(body, `data: [DONE]`) {
		t.Fatalf("expected done marker, got %q", body)
	}
	rawCompatBody, _ := anthropicRequestBody.Load().(string)
	if !strings.Contains(rawCompatBody, `"stream":false`) {
		t.Fatalf("expected anthropic fallback request to disable stream, got %q", rawCompatBody)
	}
	if messagesCalls.Load() != 1 {
		t.Fatalf("expected one anthropic messages call, got %d", messagesCalls.Load())
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
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe"},
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}}
			]}
		]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if messagesCalls.Load() != 1 {
		t.Fatalf("expected one anthropic messages fallback call, got %d", messagesCalls.Load())
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

	tr := true
	providers := []core.Provider{
		{Name: "gm12331", BaseURL: upstream.URL, Models: []string{"claude-opus-4-6"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"claude-opus-4-6","input":"ping"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"object":"response"`) || !strings.Contains(body, `"output_text":"pong"`) {
		t.Fatalf("expected responses payload with pong, got %q", body)
	}
	if responsesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected one responses call and one chat call, got responses=%d chat=%d", responsesCalls.Load(), chatCalls.Load())
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

	tr := true
	providers := []core.Provider{
		{Name: "gm12331", BaseURL: upstream.URL, Models: []string{"claude-opus-4-6"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"claude-opus-4-6","input":"ping","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.completed") {
		t.Fatalf("expected response.completed event, got %q", body)
	}
	if !strings.Contains(body, "pong") {
		t.Fatalf("expected output pong, got %q", body)
	}
	if responsesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected one responses call and one chat call, got responses=%d chat=%d", responsesCalls.Load(), chatCalls.Load())
	}
}

func TestResponsesAnthropicMessagesCompatFallbackForNonClaudeWhenAnthropicBaseURLConfigured(t *testing.T) {
	var responsesCalls atomic.Int32
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/responses":
			responsesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"The requested resource was not found"}}`)
		case "/v1/chat/completions":
			t.Fatalf("expected anthropic compat fallback instead of chat completions fallback")
		default:
			t.Fatalf("unexpected OpenAI path %q", r.URL.Path)
		}
	}))
	defer openAIUpstream.Close()

	var messagesCalls atomic.Int32
	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected anthropic path %q", r.URL.Path)
		}
		messagesCalls.Add(1)
		if got := r.Header.Get("x-api-key"); got != "sk-kimi" {
			t.Fatalf("expected anthropic x-api-key header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_kimi","type":"message","model":"kimi-for-coding","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":11,"output_tokens":1}}`)
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
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"kimi-for-coding","input":"Reply with exactly ok","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"object":"response"`) || !strings.Contains(body, `"output_text":"ok"`) {
		t.Fatalf("expected responses payload with ok, got %q", body)
	}
	if responsesCalls.Load() != 1 || messagesCalls.Load() != 1 {
		t.Fatalf("expected one responses call and one anthropic messages call, got responses=%d messages=%d", responsesCalls.Load(), messagesCalls.Load())
	}
}

func TestMessagesCompatFallbackToChatCompletions(t *testing.T) {
	var messagesCalls atomic.Int32
	var chatCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			messagesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-kimi","object":"chat.completion","created":1700000000,"model":"kimi-for-coding","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "kimi", BaseURL: upstream.URL, APIKey: "sk-kimi", Models: []string{"kimi-for-coding"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"kimi-for-coding","max_tokens":32,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"message"`) || !strings.Contains(body, `"text":"ok"`) {
		t.Fatalf("expected anthropic message response, got %q", body)
	}
	if messagesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected one messages call and one chat call, got messages=%d chat=%d", messagesCalls.Load(), chatCalls.Load())
	}
}

func TestMessagesCompatFallbackToChatCompletionsStream(t *testing.T) {
	var messagesCalls atomic.Int32
	var chatCalls atomic.Int32
	var compatBody atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/messages":
			messagesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"message":"This group does not allow /v1/messages dispatch"}}`)
		case "/v1/chat/completions":
			chatCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			compatBody.Store(string(body))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl-kimi-stream","object":"chat.completion","created":1700000000,"model":"kimi-for-coding","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	tr := true
	providers := []core.Provider{
		{Name: "kimi", BaseURL: upstream.URL, APIKey: "sk-kimi", Models: []string{"kimi-for-coding"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver:  NewModelResolver(core.CompatConfig{}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"kimi-for-coding","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"Reply with exactly ok"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: content_block_delta") || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("expected anthropic SSE body, got %q", body)
	}
	if strings.Contains(body, "[DONE]") {
		t.Fatalf("expected anthropic SSE, not OpenAI SSE, got %q", body)
	}
	rawCompatBody, _ := compatBody.Load().(string)
	if !strings.Contains(rawCompatBody, `"stream":false`) {
		t.Fatalf("expected compat chat request to disable stream, got %q", rawCompatBody)
	}
	if messagesCalls.Load() != 1 || chatCalls.Load() != 1 {
		t.Fatalf("expected one messages call and one chat call, got messages=%d chat=%d", messagesCalls.Load(), chatCalls.Load())
	}
}

func TestMessagesCompatFallbackToChatCompletionsPreservesTools(t *testing.T) {
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

	tr := true
	providers := []core.Provider{
		{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver: NewModelResolver(core.CompatConfig{
			Bridge: core.BridgeConfig{
				Enabled: true,
				Rules: []core.BridgeRule{
					{From: "claude-opus-4-6", To: "gpt-5.4"},
				},
			},
		}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{
		"model":"claude-opus-4-6",
		"max_tokens":64,
		"tools":[{"name":"bash","description":"Run shell","input_schema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}],
		"tool_choice":{"type":"tool","name":"bash"},
		"messages":[{"role":"user","content":"check cwd"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"name":"bash"`) {
		t.Fatalf("expected anthropic tool_use response, got %q", body)
	}
}

func TestMessagesCompatFallbackToChatCompletionsPreservesImages(t *testing.T) {
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

	tr := true
	providers := []core.Provider{
		{Name: "bridge", BaseURL: upstream.URL, APIKey: "sk-gpt", Models: []string{"gpt-5.4"}, Weight: 1, TimeoutMs: 5000, Enabled: &tr},
	}
	routing := core.RoutingConfig{
		Strategy:     core.StrategyHealthWeightedRR,
		MaxRetries:   1,
		RetryBackoff: core.RetryBackoffConfig{InitialMs: 1, MaxMs: 1},
		Health:       core.HealthCheckConfig{Enabled: false},
	}
	selector := NewRouteSelector(routing, providers)
	pl := NewPipeline(PipelineParams{
		Resolver: NewModelResolver(core.CompatConfig{
			Bridge: core.BridgeConfig{
				Enabled: true,
				Rules: []core.BridgeRule{
					{From: "claude-opus-4-6", To: "gpt-5.4"},
				},
			},
		}),
		Selector:  selector,
		Transport: newUpstreamTransport(nil),
		Inspector: NewResponseInspector(routing),
		Compat:    NewCompatAdapter(core.CompatConfig{}),
		Sink:      &mockSink{},
		Cfg:       routing,
	})

	router := chi.NewRouter()
	MountGatewayRoutes(router, pl, selector)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{
		"model":"claude-opus-4-6",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe"},
				{"type":"image","source":{"type":"url","url":"https://example.com/cat.png"}}
			]}
		]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if chatCalls.Load() != 1 {
		t.Fatalf("expected one chat compat call, got %d", chatCalls.Load())
	}
}
