package providerfallback_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	gatewayapi "ai-model-gateway/internal/gateway/api"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestProviderFallbackDemo(t *testing.T) {
	restoreSSRF := gatewayapi.SetSSRFCheckerForTesting(allowAllURLs{})
	t.Cleanup(restoreSSRF)
	restoreHTTP := gatewayapi.SetSharedHTTPClientForTesting(&http.Client{})
	t.Cleanup(restoreHTTP)

	primary := newDemoUpstream(t, http.StatusTooManyRequests, `{"error":{"message":"demo quota exceeded"}}`)
	fallback := newDemoUpstream(t, http.StatusOK, `{"id":"cmpl-fallback-demo","choices":[{"message":{"role":"assistant","content":"fallback ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)

	telemetry := &telemetryRecorder{ch: make(chan telemetryingest.Event, 8)}
	snap := fallbackDemoSnapshot(primary.URL(), fallback.URL())
	state := gatewayapi.NewRuntimeState()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayapi.HandleChatCompletion(r.Context(), snap, state, telemetry, nil, w, r)
	}))
	t.Cleanup(gateway.Close)

	resp, err := http.Post(gateway.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"demo-primary","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("post gateway request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gateway status = %d, body = %s", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "cmpl-fallback-demo") {
		t.Fatalf("expected fallback response body, got %s", string(body))
	}
	if got := len(primary.Requests()); got != 1 {
		t.Fatalf("primary requests = %d, want 1", got)
	}
	fallbackRequests := fallback.Requests()
	if got := len(fallbackRequests); got != 1 {
		t.Fatalf("fallback requests = %d, want 1", got)
	}
	var forwarded map[string]any
	if err := json.Unmarshal(fallbackRequests[0].Body, &forwarded); err != nil {
		t.Fatalf("decode fallback request body: %v; body = %s", err, string(fallbackRequests[0].Body))
	}
	if forwarded["model"] != "demo-fallback-upstream" {
		t.Fatalf("fallback request model = %v, want demo-fallback-upstream; body = %s", forwarded["model"], prettyJSON(fallbackRequests[0].Body))
	}

	event, ok := telemetry.Wait(time.Second)
	if !ok {
		t.Fatal("expected a telemetry event for fallback success")
	}
	if event.Payload.RouteMode != "model_fallback" {
		t.Fatalf("route_mode = %q, want model_fallback", event.Payload.RouteMode)
	}
	if event.Payload.ProviderID != "fallback-provider" {
		t.Fatalf("provider_id = %q, want fallback-provider", event.Payload.ProviderID)
	}
	if event.Payload.EffectiveModel != "demo-fallback-upstream" {
		t.Fatalf("effective_model = %q, want demo-fallback-upstream", event.Payload.EffectiveModel)
	}

	t.Logf("primary returned 429; fallback provider=%s route_mode=%s effective_model=%s",
		event.Payload.ProviderID,
		event.Payload.RouteMode,
		event.Payload.EffectiveModel,
	)
}

func fallbackDemoSnapshot(primaryURL string, fallbackURL string) *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			MaxRetries: 1,
			Retry: snapshot.RetryPolicy{
				StatusCodes:   []int{http.StatusTooManyRequests},
				StatusCodeMin: http.StatusInternalServerError,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "primary-provider",
				BaseURL:    primaryURL,
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "demo-primary", UpstreamModel: "demo-primary-upstream"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"demo-fallback"},
			},
			{
				ProviderID: "fallback-provider",
				BaseURL:    fallbackURL,
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "demo-fallback", UpstreamModel: "demo-fallback-upstream"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}
}

type allowAllURLs struct{}

func (allowAllURLs) ValidateURL(string) error {
	return nil
}

type demoUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	reqs   []capturedRequest
}

type capturedRequest struct {
	Path string
	Body []byte
}

func newDemoUpstream(t *testing.T, status int, body string) *demoUpstream {
	t.Helper()
	upstream := &demoUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.reqs = append(upstream.reqs, capturedRequest{Path: r.URL.Path, Body: append([]byte(nil), reqBody...)})
		upstream.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *demoUpstream) URL() string {
	return u.server.URL
}

func (u *demoUpstream) Requests() []capturedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]capturedRequest, len(u.reqs))
	copy(out, u.reqs)
	for i := range out {
		out[i].Body = append([]byte(nil), out[i].Body...)
	}
	return out
}

type telemetryRecorder struct {
	ch chan telemetryingest.Event
}

func (r *telemetryRecorder) Emit(event telemetryingest.Event) error {
	r.ch <- event
	return nil
}

func (r *telemetryRecorder) Wait(timeout time.Duration) (telemetryingest.Event, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event := <-r.ch:
			if event.Payload.RouteMode == "model_fallback" {
				return event, true
			}
		case <-timer.C:
			return telemetryingest.Event{}, false
		}
	}
}

func prettyJSON(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	formatted, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(formatted)
}
