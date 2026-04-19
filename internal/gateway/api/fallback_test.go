package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestResolveFallbackModels(t *testing.T) {
	snap := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-a",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4-turbo"},
				},
				FallbackModels: []string{"gpt-3.5-turbo", "gpt-3.5-turbo-16k"},
			},
			{
				ProviderID: "provider-b",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4", UpstreamModel: "gpt-4-turbo"},
				},
				FallbackModels: []string{"gpt-3.5-turbo", "gpt-4o-mini"},
			},
			{
				ProviderID: "provider-c",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "claude-3", UpstreamModel: "claude-3-opus"},
				},
				FallbackModels: []string{"claude-3-haiku"},
			},
		},
	}

	tests := []struct {
		name  string
		model string
		want  []string
	}{
		{
			name:  "deduplicated ordered fallbacks",
			model: "gpt-4",
			want:  []string{"gpt-3.5-turbo", "gpt-3.5-turbo-16k", "gpt-4o-mini"},
		},
		{
			name:  "single provider fallback",
			model: "claude-3",
			want:  []string{"claude-3-haiku"},
		},
		{
			name:  "no matching model",
			model: "nonexistent",
			want:  nil,
		},
		{
			name:  "empty fallback list",
			model: "gpt-4",
			want:  []string{"gpt-3.5-turbo", "gpt-3.5-turbo-16k", "gpt-4o-mini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveFallbackModels(snap, tt.model)
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveFallbackModels(%q) = %v, want %v", tt.model, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ResolveFallbackModels(%q)[%d] = %q, want %q", tt.model, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResolveFallbackModelsNilSnapshot(t *testing.T) {
	got := ResolveFallbackModels(nil, "gpt-4")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestHandleChatCompletionFallbackModelOnPrimaryFailure(t *testing.T) {
	routingSequence.Store(0)

	var upstreamHosts []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			switch req.URL.Host {
			case "203.0.113.10":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
				}, nil
			case "203.0.113.20":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"cmpl-fb","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
				}, nil
			default:
				return nil, nil
			}
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"fallback-model"},
			},
			{
				ProviderID: "provider-fallback",
				BaseURL:    "http://203.0.113.20",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model", UpstreamModel: "fallback-upstream-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"id":"cmpl-fb"`) {
		t.Fatalf("expected fallback response body, got %s", body)
	}

	// Verify both primary and fallback were attempted.
	if len(upstreamHosts) != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d: %v", len(upstreamHosts), upstreamHosts)
	}
	if upstreamHosts[0] != "203.0.113.10" {
		t.Fatalf("expected primary first, got %s", upstreamHosts[0])
	}
	if upstreamHosts[1] != "203.0.113.20" {
		t.Fatalf("expected fallback second, got %s", upstreamHosts[1])
	}
}

func TestHandleChatCompletionNoFallbackWhenPrimarySucceeds(t *testing.T) {
	routingSequence.Store(0)

	var upstreamHosts []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"cmpl-primary","choices":[]}`)),
			}, nil
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"fallback-model"},
			},
			{
				ProviderID: "provider-fallback",
				BaseURL:    "http://203.0.113.20",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model", UpstreamModel: "fallback-upstream-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if len(upstreamHosts) != 1 {
		t.Fatalf("expected 1 upstream attempt, got %d: %v", len(upstreamHosts), upstreamHosts)
	}
	if upstreamHosts[0] != "203.0.113.10" {
		t.Fatalf("expected primary only, got %s", upstreamHosts[0])
	}
}

func TestHandleChatCompletionFallbackWithNoFallbackModels(t *testing.T) {
	routingSequence.Store(0)

	var upstreamHosts []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
			}, nil
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				// No FallbackModels configured.
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", resp.StatusCode)
	}

	if len(upstreamHosts) != 1 {
		t.Fatalf("expected 1 upstream attempt, got %d: %v", len(upstreamHosts), upstreamHosts)
	}
}

func TestHandleChatCompletionFallbackBodyModelRewrite(t *testing.T) {
	routingSequence.Store(0)

	var forwardedBodies []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			forwardedBodies = append(forwardedBodies, string(body))
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
			}, nil
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"fallback-model"},
			},
			{
				ProviderID: "provider-fallback",
				BaseURL:    "http://203.0.113.20",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model", UpstreamModel: "fallback-upstream-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	// The fallback attempt should rewrite the model in the body.
	if len(forwardedBodies) != 2 {
		t.Fatalf("expected 2 forwarded bodies, got %d", len(forwardedBodies))
	}
	if !strings.Contains(forwardedBodies[0], `"model":"primary-model"`) {
		t.Fatalf("expected primary model in first body, got %s", forwardedBodies[0])
	}
	if !strings.Contains(forwardedBodies[1], `"model":"fallback-upstream-model"`) {
		t.Fatalf("expected fallback upstream model in second body, got %s", forwardedBodies[1])
	}
}

func TestHandleChatCompletionFallbackWithTelemetry(t *testing.T) {
	routingSequence.Store(0)

	tel := &capturingTelemetryEmitter{events: make(chan telemetryingest.Event, 2)}

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "203.0.113.10":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
				}, nil
			case "203.0.113.20":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"cmpl-fb","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3}}`)),
				}, nil
			default:
				return nil, nil
			}
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"fallback-model"},
			},
			{
				ProviderID: "provider-fallback",
				BaseURL:    "http://203.0.113.20",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model", UpstreamModel: "fallback-upstream-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, tel)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// We expect at least one telemetry event from the successful fallback.
	select {
	case event := <-tel.events:
		if event.Payload.RouteMode != "model_fallback" {
			t.Fatalf("expected route_mode model_fallback, got %q", event.Payload.RouteMode)
		}
		if event.Payload.ProviderID != "provider-fallback" {
			t.Fatalf("expected provider provider-fallback, got %q", event.Payload.ProviderID)
		}
		if event.Payload.EffectiveModel != "fallback-upstream-model" {
			t.Fatalf("expected effective model fallback-upstream-model, got %q", event.Payload.EffectiveModel)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telemetry event for fallback")
	}
}

func TestHandleChatCompletionFallbackSkipsUnavailableFallbackModel(t *testing.T) {
	routingSequence.Store(0)

	var upstreamHosts []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			switch req.URL.Host {
			case "203.0.113.10":
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
				}, nil
			case "203.0.113.30":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"id":"cmpl-fb2","choices":[]}`)),
				}, nil
			default:
				return nil, nil
			}
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"missing-model", "fallback-model-2"},
			},
			{
				ProviderID: "provider-fallback-2",
				BaseURL:    "http://203.0.113.30",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model-2", UpstreamModel: "fallback-2-upstream"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify primary and second fallback were attempted (missing-model has no provider).
	if len(upstreamHosts) != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d: %v", len(upstreamHosts), upstreamHosts)
	}
	if upstreamHosts[0] != "203.0.113.10" {
		t.Fatalf("expected primary first, got %s", upstreamHosts[0])
	}
	if upstreamHosts[1] != "203.0.113.30" {
		t.Fatalf("expected fallback-2 second, got %s", upstreamHosts[1])
	}
}

func TestHandleChatCompletionFallbackReturnsErrorWhenAllFail(t *testing.T) {
	routingSequence.Store(0)

	var upstreamHosts []string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			upstreamHosts = append(upstreamHosts, req.URL.Host)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exceeded"}}`)),
			}, nil
		}),
	})

	snap := &snapshot.Snapshot{
		Ingress: snapshot.IngressConfig{MaxBodyBytes: 1 << 20},
		RoutingPolicy: snapshot.RoutingPolicy{
			Retry: snapshot.RetryPolicy{
				StatusCodes: []int{http.StatusRequestTimeout, http.StatusTooManyRequests},
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-primary",
				BaseURL:    "http://203.0.113.10",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: testPublicModel, UpstreamModel: "primary-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
				FallbackModels: []string{"fallback-model"},
			},
			{
				ProviderID: "provider-fallback",
				BaseURL:    "http://203.0.113.20",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "fallback-model", UpstreamModel: "fallback-upstream-model"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled:   true,
					Weight:    1,
					TimeoutMs: 5000,
				},
			},
		},
	}

	server := newGatewayTestServer(t, snap, nil)
	defer server.Close()

	reqBody := `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`
	resp, err := server.Client().Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", resp.StatusCode)
	}

	// Both primary and fallback should have been attempted.
	if len(upstreamHosts) != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d: %v", len(upstreamHosts), upstreamHosts)
	}
}
