package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-model-gateway/internal/core"
)

func TestTransportExecute_UsesAnthropicHeadersForMessagesPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected Authorization header to be stripped, got %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant" {
			t.Fatalf("expected x-api-key header, got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("expected default anthropic-version header, got %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "prompt-caching-2024-07-31" {
			t.Fatalf("expected anthropic-beta header to pass through, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_direct","type":"message"}`)
	}))
	defer upstream.Close()

	tr := true
	req := &core.GatewayRequest{
		Method: "POST",
		Path:   "/v1/messages",
		Headers: http.Header{
			"Content-Type":   []string{"application/json"},
			"Authorization":  []string{"Bearer caller-secret"},
			"x-api-key":      []string{"caller-key"},
			"anthropic-beta": []string{"prompt-caching-2024-07-31"},
		},
		Body: []byte(`{"model":"claude-opus-4-6","messages":[{"role":"user","content":"Reply with exactly ok"}]}`),
		Provider: &core.Provider{
			Name:      "claude",
			BaseURL:   upstream.URL,
			APIKey:    "sk-ant",
			Models:    []string{"claude-opus-4-6"},
			TimeoutMs: 5000,
			Enabled:   &tr,
		},
	}

	resp, err := newUpstreamTransport(nil).Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTransportExecute_UsesAnthropicBaseURLForMessagesPath(t *testing.T) {
	openAIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("expected messages path to avoid BaseURL and use AnthropicBaseURL")
	}))
	defer openAIUpstream.Close()

	anthropicUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("expected /v1/messages path, got %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_direct","type":"message"}`)
	}))
	defer anthropicUpstream.Close()

	tr := true
	req := &core.GatewayRequest{
		Method: "POST",
		Path:   "/v1/messages",
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: []byte(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"ok"}]}`),
		Provider: &core.Provider{
			Name:             "kimi",
			BaseURL:          openAIUpstream.URL,
			AnthropicBaseURL: anthropicUpstream.URL,
			APIKey:           "sk-kimi",
			Models:           []string{"kimi-for-coding"},
			TimeoutMs:        5000,
			Enabled:          &tr,
		},
	}

	resp, err := newUpstreamTransport(nil).Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTransportExecute_RejectsSSRFLoopbackUpstream(t *testing.T) {
	tr := true
	req := &core.GatewayRequest{
		Method: "POST",
		Path:   "/v1/chat/completions",
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: []byte(`{"model":"chat-only-model","messages":[{"role":"user","content":"hi"}]}`),
		Provider: &core.Provider{
			Name:      "loopback",
			BaseURL:   "http://127.0.0.1:1",
			APIKey:    "sk-test",
			Models:    []string{"chat-only-model"},
			TimeoutMs: 5000,
			Enabled:   &tr,
		},
	}

	resp, err := NewUpstreamTransport().Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute() returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected gateway response")
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	if resp.Error == nil || resp.Error.Error() == "" {
		t.Fatalf("expected SSRF validation error, got %#v", resp.Error)
	}
	if got := resp.Error.Error(); !strings.HasPrefix(got, "SSRF validation failed") {
		t.Fatalf("expected SSRF validation failure, got %q", got)
	}
	if !resp.Retryable {
		t.Fatalf("expected SSRF transport failure to remain retryable for alternate upstreams")
	}
}
