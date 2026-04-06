package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/proxy"
)

// transport implements core.UpstreamTransport.
// It executes a single HTTP round-trip to the selected provider.
type transport struct {
	client      *http.Client
	ssrfChecker *proxy.SSRFChecker
}

// NewUpstreamTransport creates an UpstreamTransport with sensible defaults.
func NewUpstreamTransport() core.UpstreamTransport {
	return newUpstreamTransport(proxy.NewSSRFChecker())
}

func newUpstreamTransport(checker *proxy.SSRFChecker) core.UpstreamTransport {
	return &transport{
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:           500,                // Increased from 200 for higher throughput
				MaxIdleConnsPerHost:    100,                // Increased from 20 for better per-host concurrency
				MaxConnsPerHost:        200,                // New: limit per host connections
				IdleConnTimeout:        90 * time.Second,   // Tuned for connection reuse
				TLSHandshakeTimeout:    10 * time.Second,   // New: explicit TLS handshake timeout
				ExpectContinueTimeout:  1 * time.Second,    // New: 100-continue timeout
				DialContext: (&net.Dialer{
					Timeout:   5 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:      true,
				MaxResponseHeaderBytes: 1 << 20, // 1 MB
				// Security: Limit TLS to secure versions only
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
			// Per-request timeout is set via context, not here.
			Timeout: 0,
		},
		ssrfChecker: checker,
	}
}

func (t *transport) Execute(ctx context.Context, req *core.GatewayRequest) (*core.GatewayResponse, error) {
	provider := req.Provider
	if provider == nil {
		return nil, fmt.Errorf("no provider set on request")
	}

	upstreamPath := req.Path
	if strings.TrimSpace(req.UpstreamPath) != "" {
		upstreamPath = req.UpstreamPath
	}

	// Build upstream URL.
	upstreamURL := buildUpstreamURL(provider, upstreamPath)
	if t.ssrfChecker != nil {
		if err := t.ssrfChecker.ValidateURL(upstreamURL); err != nil {
			return &core.GatewayResponse{
				StatusCode: http.StatusBadGateway,
				Provider:   provider,
				Error:      fmt.Errorf("SSRF validation failed: %w", err),
				Retryable:  true,
			}, nil
		}
	}

	// Create the HTTP request.
	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = strings.NewReader(string(req.Body))
	}

	timeout := time.Duration(provider.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, req.Method, upstreamURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	// Copy relevant headers from the original request.
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	// Set provider-specific headers.
	// Only set auth headers if API key is non-empty to prevent "Bearer " with empty token.
	if usesAnthropicHeaders(upstreamPath) {
		httpReq.Header.Del("Authorization")
		if strings.TrimSpace(provider.APIKey) != "" {
			httpReq.Header.Set("x-api-key", strings.TrimSpace(provider.APIKey))
		}
		if strings.TrimSpace(httpReq.Header.Get("anthropic-version")) == "" {
			httpReq.Header.Set("anthropic-version", "2023-06-01")
		}
	} else if strings.TrimSpace(provider.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	}
	if httpReq.Header.Get("Content-Type") == "" && len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range provider.Headers {
		httpReq.Header.Set(k, v)
	}

	// Execute.
	start := time.Now()
	resp, err := t.client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		if ctx.Err() != nil {
			return &core.GatewayResponse{
				StatusCode: http.StatusGatewayTimeout,
				Provider:   provider,
				Latency:    latency,
				Error:      core.ErrUpstreamTimeout,
				Retryable:  true,
			}, nil
		}
		return &core.GatewayResponse{
			StatusCode: http.StatusBadGateway,
			Provider:   provider,
			Latency:    latency,
			Error:      fmt.Errorf("upstream request failed: %w", err),
			Retryable:  true,
		}, nil
	}

	gwResp := &core.GatewayResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Provider:   provider,
		Latency:    latency,
		Stream:     isSSE(resp),
	}

	if gwResp.Stream {
		gwResp.BodyReader = resp.Body
	} else {
		// Use pooled buffer for reading response body
		buf := GetBodyBuffer()
		defer PutBodyBuffer(buf)

		// Read into pooled buffer using a limited reader approach
		var body []byte
		var readErr error

		// Try to read with the pooled buffer first
		n, err := resp.Body.Read(buf)
		if err != nil && err != io.EOF {
			// Error occurred during read
			readErr = err
		} else if err == io.EOF {
			// Single read was enough
			body = buf[:n]
		} else {
			// More data to read - need to grow
			var builder bytes.Buffer
			builder.Grow(n + len(buf))
			builder.Write(buf[:n])
			_, readErr = builder.ReadFrom(resp.Body)
			if readErr == nil {
				body = builder.Bytes()
			}
		}

		resp.Body.Close()
		if readErr != nil {
			gwResp.Error = fmt.Errorf("read upstream body: %w", readErr)
			gwResp.Retryable = true
		} else {
			gwResp.Body = body
		}
	}

	return gwResp, nil
}

// buildUpstreamURL constructs the full URL for the upstream provider.
func buildUpstreamURL(p *core.Provider, path string) string {
	base := strings.TrimRight(p.BaseURL, "/")

	// Use Anthropic base URL for Anthropic-protocol paths.
	if p.AnthropicBaseURL != "" && usesAnthropicHeaders(path) {
		base = strings.TrimRight(p.AnthropicBaseURL, "/")
	}

	return base + path
}

// isSSE checks whether the response is an SSE stream.
func isSSE(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/event-stream")
}

func usesAnthropicHeaders(path string) bool {
	return strings.HasPrefix(path, "/v1/messages")
}
