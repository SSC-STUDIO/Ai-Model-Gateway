// Package core defines the pure types, interfaces, and rules for the gateway.
// This is the innermost layer — it has zero dependencies on infra, app, or adminapi.
package core

import (
	"context"
	"io"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// Request / Response value objects used across the forwarding pipeline
// ---------------------------------------------------------------------------

// GatewayRequest is the normalised representation of an inbound API request
// after protocol-specific parsing (OpenAI chat, responses, etc.).
type GatewayRequest struct {
	// ID is a unique request identifier (generated at ingress).
	ID string

	// Model is the resolved model name after bridge/rewrite rules.
	OriginalModel string
	Model         string

	// Provider is the selected upstream provider (populated after routing).
	Provider *Provider

	// Stream indicates whether the client requested SSE streaming.
	Stream bool

	// ModelRequired indicates whether the endpoint contract requires a model.
	ModelRequired bool

	// SkipModelRewrite indicates that bridge rewrite should be bypassed for this endpoint.
	SkipModelRewrite bool

	// HTTP-level fields carried through the pipeline.
	Method       string
	Path         string
	UpstreamPath string
	Headers      http.Header
	Body         []byte

	// UserAgent from the original request (used for bridge exclusion, etc.).
	UserAgent string

	// Attempt tracks retry state.
	Attempt     int
	MaxAttempts int

	// StickyKey is an opaque key for sticky-session affinity (e.g. session-id).
	StickyKey string

	// Context carries deadlines and cancellation.
	Ctx context.Context
}

// GatewayResponse is the normalised representation of an upstream response
// before protocol-specific compat conversion.
type GatewayResponse struct {
	// StatusCode from the upstream HTTP response.
	StatusCode int
	Headers    http.Header

	// Body is the raw response body (non-streaming).
	// For streaming responses, BodyReader is used instead.
	Body       []byte
	BodyReader io.ReadCloser

	// Stream indicates whether this is a streaming (SSE) response.
	Stream bool

	// Provider that produced this response.
	Provider *Provider

	// Latency of the upstream round-trip.
	Latency time.Duration

	// Model echoed back from the upstream (may differ from requested).
	Model string

	// RouteMode overrides telemetry route mode for synthesized compat responses.
	RouteMode string

	// Retryable indicates that the pipeline may retry on a different upstream.
	Retryable bool

	// Error is set when the upstream returned an error or the request failed.
	Error error
}

// ---------------------------------------------------------------------------
// Provider (renamed from Upstream in v1)
// ---------------------------------------------------------------------------

// Provider represents an upstream AI model provider endpoint.
type Provider struct {
	Name             string            `yaml:"name"              json:"name"`
	ProtocolAdapter  string            `yaml:"protocol_adapter"  json:"protocol_adapter,omitempty"`
	BaseURL          string            `yaml:"base_url"          json:"base_url"`
	AnthropicBaseURL string            `yaml:"anthropic_base_url" json:"anthropic_base_url,omitempty"`
	APIKey           string            `yaml:"api_key"           json:"-"`
	ProviderClass    ProviderClass     `yaml:"provider_class"    json:"provider_class"`
	Models           []string          `yaml:"models"            json:"models"`
	ModelAliases     map[string]string `yaml:"model_aliases"     json:"model_aliases,omitempty"`
	Weight           int               `yaml:"weight"            json:"weight"`
	TimeoutMs        int               `yaml:"timeout_ms"        json:"timeout_ms"`
	SameRetries      int               `yaml:"same_retries"      json:"same_retries"`
	RateLimit        RateLimitConfig   `yaml:"rate_limit"        json:"rate_limit"`
	Enabled          *bool             `yaml:"enabled"           json:"enabled"`
	Headers          map[string]string `yaml:"headers"           json:"headers,omitempty"`
	FallbackModels   []string          `yaml:"fallback_models"   json:"fallback_models,omitempty"`
}

// IsEnabled returns whether the provider is enabled (defaults to true).
func (p *Provider) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// ProviderClass enumerates billing classes for upstream providers.
type ProviderClass string

const (
	ProviderClassFree         ProviderClass = "free"
	ProviderClassQuotaLimited ProviderClass = "quota_limited"
)

// ---------------------------------------------------------------------------
// Telemetry record
// ---------------------------------------------------------------------------

// RequestRecord is the telemetry data captured per forwarded request.
type RequestRecord struct {
	RequestID string
	Timestamp time.Time
	Path      string
	// RequestedModel is the original model requested by the client.
	RequestedModel string
	// EffectiveModel is the routed model after rewrite/fallback.
	EffectiveModel string
	// Model is kept as a backward-compatible alias for EffectiveModel.
	Model              string
	RouteMode          string
	Attempts           int
	Provider           string
	StatusCode         int
	Latency            time.Duration
	InputTokens        int64
	CachedPromptTokens int64
	OutputTokens       int64
	Stream             bool
	Error              string
}
