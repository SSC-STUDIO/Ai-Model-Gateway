package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

func TestForwardToUpstream_SSRFBlocked(t *testing.T) {
	// Use the real SSRF checker (default), which blocks localhost.
	// Do NOT call allowLocalAnthropicTestUpstreams here.
	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://127.0.0.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}
	ctx := context.Background()

	statusCode, respBody, streamBody, _, _, err := forwardToUpstream(ctx, nil, provider, "/v1/chat/completions", nil, false, nil, false)
	if err == nil {
		t.Fatal("expected SSRF error, got nil")
	}
	if statusCode != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d", http.StatusBadGateway, statusCode)
	}
	if respBody != nil {
		t.Errorf("expected nil body, got %d bytes", len(respBody))
	}
	if streamBody != nil {
		t.Errorf("expected nil stream body")
	}
}

func TestForwardToUpstream_SuccessNonStreaming(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	const upstreamBody = `{"id":"cmpl-abc","object":"chat.completion","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3}}`

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(upstreamBody)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	statusCode, respBody, streamBody, ct, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	if string(respBody) != upstreamBody {
		t.Fatalf("expected body %q, got %q", upstreamBody, string(respBody))
	}
	if streamBody != nil {
		t.Fatal("expected nil stream body for non-streaming response")
	}
	if ct != "" {
		t.Fatalf("expected empty content type, got %q", ct)
	}
}

func TestForwardToUpstream_WaitsOnProviderRateLimitBeforeSending(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	var calls atomic.Int32
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"id":"ok","choices":[]}`)),
			}, nil
		}),
	})

	state := NewRuntimeState()
	provider := &snapshot.ProviderSnapshot{
		ProviderID: "key-a",
		UpstreamID: "https://shared.example.com/v1",
		BaseURL:    "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
			RateLimit: snapshot.RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 1,
				Burst:             1,
			},
		},
	}

	statusCode, _, _, _, _, err := forwardToUpstream(context.Background(), state, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err != nil || statusCode != http.StatusOK {
		t.Fatalf("first request status=%d err=%v", statusCode, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	statusCode, _, _, _, _, err = forwardToUpstream(ctx, state, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err == nil {
		t.Fatal("expected second request to wait for upstream slot and time out")
	}
	if statusCode != http.StatusGatewayTimeout {
		t.Fatalf("second request status=%d, want %d", statusCode, http.StatusGatewayTimeout)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected second request not to reach upstream before slot, calls=%d", calls.Load())
	}
}

func TestForwardToUpstream_SSEDetection(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "data: hello\n\n")
				_ = pw.Close()
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream; charset=utf-8"},
				},
				Body: pr,
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	statusCode, respBody, streamBody, ct, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/chat/completions", []byte(`{"model":"test","stream":true}`), true, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	if respBody != nil {
		t.Fatal("expected nil respBody for SSE response")
	}
	if streamBody == nil {
		t.Fatal("expected non-nil streamBody for SSE response")
	}
	defer streamBody.Close()
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected event-stream content type, got %q", ct)
	}

	data, err := io.ReadAll(streamBody)
	if err != nil {
		t.Fatalf("read stream body: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected streamed data to contain 'hello', got %q", string(data))
	}
}

func TestForwardToUpstream_ResponseTooLarge(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	// Create a response body that exceeds 10MB.
	largeBody := strings.Repeat("A", 10<<20+1)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(largeBody)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	statusCode, respBody, streamBody, _, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
	if statusCode != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", statusCode)
	}
	if respBody != nil {
		t.Fatal("expected nil respBody on oversized response")
	}
	if streamBody != nil {
		t.Fatal("expected nil streamBody on oversized response")
	}
}

func TestForwardToUpstream_Timeout(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			// Block until the request context cancels.
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 1, // very short timeout
		},
	}

	ctx := context.Background()
	statusCode, respBody, streamBody, _, _, err := forwardToUpstream(ctx, nil, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected context deadline exceeded error, got: %v", err)
	}
	if statusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d", statusCode)
	}
	if respBody != nil {
		t.Fatal("expected nil respBody on timeout")
	}
	if streamBody != nil {
		t.Fatal("expected nil streamBody on timeout")
	}
}

func TestForwardToUpstream_500Upstream(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":"internal error"}`)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	statusCode, respBody, streamBody, _, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, nil, false)
	if err != nil {
		t.Fatalf("expected no error for 500 response, got: %v", err)
	}
	if statusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", statusCode)
	}
	if respBody == nil {
		t.Fatal("expected non-nil respBody even on 500")
	}
	if !strings.Contains(string(respBody), "internal error") {
		t.Fatalf("expected error message in body, got %q", string(respBody))
	}
	if streamBody != nil {
		t.Fatal("expected nil streamBody for non-stream response")
	}
}

func TestForwardToUpstream_ForwardsUserAgent(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	var receivedUA string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedUA = req.Header.Get("User-Agent")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	headers := http.Header{}
	headers.Set("User-Agent", "test-agent/1.0")

	_, _, _, _, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/chat/completions", []byte(`{"model":"test"}`), false, headers, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedUA != "test-agent/1.0" {
		t.Fatalf("expected User-Agent 'test-agent/1.0', got %q", receivedUA)
	}
}

func TestForwardToUpstream_AnthropicVersionHeader(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	var receivedAnthropicVersion string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedAnthropicVersion = req.Header.Get("anthropic-version")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL: "http://203.0.113.1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	_, _, _, _, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/messages", []byte(`{"model":"claude"}`), false, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAnthropicVersion != "2023-06-01" {
		t.Fatalf("expected anthropic-version '2023-06-01', got %q", receivedAnthropicVersion)
	}
}

func TestForwardToUpstream_AnthropicBaseURL(t *testing.T) {
	allowLocalAnthropicTestUpstreams(t)

	var receivedURL string
	swapSharedHTTPClient(t, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			receivedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		}),
	})

	provider := &snapshot.ProviderSnapshot{
		BaseURL:          "http://203.0.113.1",
		AnthropicBaseURL: "http://203.0.113.2/anthropic",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:   true,
			Weight:    1,
			TimeoutMs: 5000,
		},
	}

	_, _, _, _, _, err := forwardToUpstream(context.Background(), nil, provider, "/v1/messages", []byte(`{"model":"claude"}`), false, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedURL := "http://203.0.113.2/anthropic/v1/messages"
	if receivedURL != expectedURL {
		t.Fatalf("expected URL %q, got %q", expectedURL, receivedURL)
	}
}
