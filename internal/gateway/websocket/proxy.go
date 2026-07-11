// Package websocket provides WebSocket proxying for the OpenAI Realtime API.
package websocket

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/infra/logger"
	"ai-model-gateway/internal/proxy"

	"github.com/gorilla/websocket"
)

// SSRFChecker is the interface for SSRF validation.
type SSRFChecker interface {
	ValidateURL(rawURL string) error
}

// Proxy handles WebSocket upgrade and proxying to upstream providers.
type Proxy struct {
	upgrader      websocket.Upgrader
	dialer        websocket.Dialer
	ssrfChecker   SSRFChecker
	allowedOrigin func(r *http.Request) bool
}

// DefaultCheckOrigin returns a CheckOrigin function that validates the Origin
// header against a configurable allowlist. When allowedOrigins is empty, all
// cross-origin requests are rejected. Requests with no Origin header (non-browser
// clients) are allowed through.
func DefaultCheckOrigin(allowedOrigins []string) func(r *http.Request) bool {
	// Build a lowercase lookup set for fast matching.
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(strings.ToLower(o))
		if o != "" {
			allowed[o] = true
		}
	}

	return func(r *http.Request) bool {
		if r == nil {
			// No request context — treat as non-browser client (no Origin header).
			return true
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser clients (curl, SDK) don't send Origin — allow.
			return true
		}

		// If no origins configured, reject all cross-origin.
		if len(allowed) == 0 {
			return false
		}

		// Normalize: lowercase, strip trailing slash.
		normalized := strings.ToLower(strings.TrimRight(origin, "/"))

		// Exact match.
		if allowed[normalized] {
			return true
		}

		// Strip scheme prefix for matching (e.g. "https://example.com" → "example.com").
		for _, prefix := range []string{"https://", "http://"} {
			if strings.HasPrefix(normalized, prefix) {
				host := strings.TrimPrefix(normalized, prefix)
				// Strip port if present.
				if idx := strings.LastIndex(host, ":"); idx > 0 {
					host = host[:idx]
				}
				if allowed[host] {
					return true
				}
			}
		}

		return false
	}
}

// NewProxy creates a new WebSocket proxy that rejects all cross-origin requests
// by default. Use NewProxyWithOrigin or SetAllowedOrigin to configure allowed origins.
func NewProxy() *Proxy {
	return NewProxyWithOrigin(nil, nil)
}

// NewProxyWithSSRFChecker creates a new WebSocket proxy with a custom SSRF checker.
func NewProxyWithSSRFChecker(checker SSRFChecker) *Proxy {
	p := NewProxyWithOrigin(checker, nil)
	return p
}

// NewProxyWithOrigin creates a new WebSocket proxy with a custom SSRF checker
// and an origin validation function. When fn is non-nil, it replaces the default
// (reject-all) CheckOrigin.
func NewProxyWithOrigin(checker SSRFChecker, fn func(*http.Request) bool) *Proxy {
	if fn == nil {
		fn = DefaultCheckOrigin(nil)
	}
	p := &Proxy{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// Default: reject all cross-origin. SetAllowedOrigin or fn overrides.
			CheckOrigin: fn,
		},
		dialer: websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		},
		ssrfChecker:   checker,
		allowedOrigin: fn,
	}

	if p.ssrfChecker == nil {
		p.ssrfChecker = proxy.NewSSRFChecker()
	}

	// Apply DNS-pinning dialer to prevent DNS rebinding attacks.
	if s, ok := p.ssrfChecker.(*proxy.SSRFChecker); ok {
		p.dialer = s.NewSafeDialer(p.dialer)
	} else {
		// Reset to non-pinned dialer for custom checkers (e.g. test mocks).
		p.dialer = websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}
	}

	return p
}

// SetAllowedOrigin sets the origin validation function for the WebSocket upgrader.
// When fn is nil, CheckOrigin rejects all cross-origin requests.
func (p *Proxy) SetAllowedOrigin(fn func(r *http.Request) bool) {
	p.allowedOrigin = fn
	p.upgrader.CheckOrigin = fn
}

// ServeHTTP handles WebSocket upgrade and proxying.
// It extracts the model from query parameters, selects a provider,
// and establishes a bidirectional WebSocket connection.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request, snap *snapshot.Snapshot) {
	if snap == nil {
		http.Error(w, "no snapshot available", http.StatusInternalServerError)
		return
	}

	// Extract model from query parameters
	model := r.URL.Query().Get("model")
	if model == "" {
		http.Error(w, "model parameter is required", http.StatusBadRequest)
		return
	}

	// Upgrade client connection
	clientConn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("upgrade error", "error", err)
		return
	}
	defer clientConn.Close()

	// Select provider
	candidates := collectProviderCandidates(snap, model)
	if len(candidates) == 0 {
		logger.Warn("model not found", "model", model)
		if err := clientConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"model not found: `+model+`"}`)); err != nil {
			logger.Warn("failed to send websocket error", "error", err)
		}
		return
	}

	// Try providers in order
	var upstreamConn *websocket.Conn
	var selectedProvider *snapshot.ProviderSnapshot
	var upstreamModel string

	for _, candidate := range candidates {
		upstreamURL := buildUpstreamURL(candidate.provider, r.URL.Path, model, candidate.upstreamModel)

		// SSRF check
		if err := p.ssrfChecker.ValidateURL(upstreamURL); err != nil {
			logger.Warn("SSRF validation failed", "provider", candidate.provider.ProviderID, "error", err)
			continue
		}

		// Build request headers
		headers := http.Header{}
		setAuthHeaders(headers, candidate.provider)

		// Connect to upstream
		conn, _, err := p.dialer.Dial(upstreamURL, headers)
		if err != nil {
			logger.Warn("failed to connect to upstream", "provider", candidate.provider.ProviderID, "error", err)
			continue
		}

		upstreamConn = conn
		selectedProvider = candidate.provider
		upstreamModel = candidate.upstreamModel
		break
	}

	if upstreamConn == nil {
		logger.Warn("no provider available", "model", model)
		if err := clientConn.WriteMessage(websocket.TextMessage, []byte(`{"error":"no provider available"}`)); err != nil {
			logger.Warn("failed to send websocket error", "error", err)
		}
		return
	}
	defer upstreamConn.Close()

	logger.Info("proxy established", "model", model, "provider", selectedProvider.ProviderID, "upstream_model", upstreamModel)

	// Bidirectional forwarding
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Client -> Upstream
	go func() {
		defer wg.Done()
		defer cancel()
		p.forwardMessages(clientConn, upstreamConn, "client->upstream")
	}()

	// Upstream -> Client
	go func() {
		defer wg.Done()
		defer cancel()
		p.forwardMessages(upstreamConn, clientConn, "upstream->client")
	}()

	// Wait for either direction to finish
	wg.Wait()
}

// forwardMessages forwards WebSocket messages from src to dst.
func (p *Proxy) forwardMessages(src, dst *websocket.Conn, direction string) {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warn("read error", "direction", direction, "error", err)
			}
			return
		}

		if err := dst.WriteMessage(messageType, message); err != nil {
			logger.Warn("write error", "direction", direction, "error", err)
			return
		}
	}
}

// providerCandidate represents a candidate provider for routing.
type providerCandidate struct {
	provider      *snapshot.ProviderSnapshot
	upstreamModel string
	weight        int
}

// collectProviderCandidates collects providers that support the given model.
func collectProviderCandidates(snap *snapshot.Snapshot, model string) []providerCandidate {
	if snap == nil {
		return nil
	}

	candidates := make([]providerCandidate, 0, len(snap.Providers))
	for i := range snap.Providers {
		p := &snap.Providers[i]
		if !p.ExecutionPolicy.Enabled {
			continue
		}

		for _, m := range p.ModelTable {
			if m.PublicModel == model {
				candidates = append(candidates, providerCandidate{
					provider:      p,
					upstreamModel: m.UpstreamModel,
					weight:        normalizeWeight(p.ExecutionPolicy.Weight),
				})
				break
			}
		}
	}

	return candidates
}

// buildUpstreamURL constructs the upstream WebSocket URL.
func buildUpstreamURL(provider *snapshot.ProviderSnapshot, path, model, upstreamModel string) string {
	baseURL := strings.TrimRight(provider.BaseURL, "/")

	// Convert HTTP(S) URL to WebSocket URL
	if strings.HasPrefix(baseURL, "https://") {
		baseURL = "wss://" + strings.TrimPrefix(baseURL, "https://")
	} else if strings.HasPrefix(baseURL, "http://") {
		baseURL = "ws://" + strings.TrimPrefix(baseURL, "http://")
	}

	// Build URL with model query parameter
	url := baseURL + path
	if strings.Contains(url, "?") {
		url = strings.Replace(url, "model="+model, "model="+upstreamModel, 1)
	} else {
		url = url + "?model=" + upstreamModel
	}

	return url
}

// setAuthHeaders sets authentication headers for the upstream connection.
func setAuthHeaders(headers http.Header, provider *snapshot.ProviderSnapshot) {
	if provider.Credentials.Kind == "bearer" && provider.Credentials.Value != "" {
		headers.Set("Authorization", "Bearer "+provider.Credentials.Value)
	} else if provider.Credentials.Kind == "api_key" && provider.Credentials.Value != "" {
		headerName := provider.Credentials.HeaderName
		if headerName == "" {
			headerName = "x-api-key"
		}
		headers.Set(headerName, provider.Credentials.Value)
	}

	for k, v := range provider.Headers {
		headers.Set(k, v)
	}
}

// normalizeWeight normalizes the weight value.
func normalizeWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}
