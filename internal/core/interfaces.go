package core

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Pipeline stage interfaces — each stage is independently testable
// and must not import infra or adminapi packages.
// ---------------------------------------------------------------------------

// ModelResolver resolves (and optionally rewrites) the requested model name.
// It applies bridge rules, fallback mappings, and validates that at least one
// provider can serve the resolved model.
type ModelResolver interface {
	// Resolve takes the raw model name from the request and returns the
	// canonical model name after applying bridge/rewrite rules.
	Resolve(ctx context.Context, model string, userAgent string) (resolved string, err error)

	// FallbackModel returns the fallback model for the given model, or ""
	// if no fallback is configured.
	FallbackModel(model string) string
}

// RouteSelector picks a Provider for a given model, respecting health state,
// weights, sticky-session affinity, and provider-class preferences.
type RouteSelector interface {
	// Select returns the best available provider for the model.
	// It may return ErrNoProvider if no healthy provider is available.
	Select(ctx context.Context, model string, stickyKey string) (*Provider, error)

	// RememberSticky stores an explicit sticky-session affinity mapping.
	RememberSticky(stickyKey string, providerName string)

	// ReportResult feeds back the outcome of a request so the selector
	// can update health/weight state.
	ReportResult(provider *Provider, statusCode int, latency time.Duration, err error)

	// ListModels returns all models currently routable across all providers.
	ListModels() []string
}

// UpstreamTransport executes a single HTTP round-trip to the selected
// provider, handling timeouts, custom headers, and API-key injection.
type UpstreamTransport interface {
	// Execute sends the request to the provider and returns the raw response.
	// The caller is responsible for closing GatewayResponse.BodyReader.
	Execute(ctx context.Context, req *GatewayRequest) (*GatewayResponse, error)
}

// ResponseInspector examines an upstream response and decides whether it
// should be retried, intercepted, or passed through.
type ResponseInspector interface {
	// Inspect evaluates the response and annotates it:
	//   - sets Retryable=true if the request should be retried
	//   - returns a replacement response if interception applies
	//   - returns (resp, nil) for pass-through
	Inspect(ctx context.Context, req *GatewayRequest, resp *GatewayResponse) (*GatewayResponse, error)
}

// CompatAdapter converts between protocol variants (e.g. OpenAI Chat ↔
// OpenAI Responses, Anthropic Messages) so that the external /v1/* contract
// remains stable regardless of the upstream's native protocol.
type CompatAdapter interface {
	// AdaptRequest may modify the outbound request body/headers for the
	// selected provider's expected protocol.
	AdaptRequest(ctx context.Context, req *GatewayRequest) error

	// AdaptResponse may rewrite the upstream response into the client's
	// expected protocol format.
	AdaptResponse(ctx context.Context, req *GatewayRequest, resp *GatewayResponse) error
}

// TelemetrySink persists request records for observability and admin queries.
type TelemetrySink interface {
	// Record persists a single request telemetry record.
	Record(ctx context.Context, rec *RequestRecord) error

	// Close releases any held resources (DB connections, buffers).
	Close() error
}

// ---------------------------------------------------------------------------
// Composite: the full pipeline orchestrator
// ---------------------------------------------------------------------------

// Pipeline orchestrates the full request lifecycle:
//
//	parse → resolve → select → execute → inspect → compat → telemetry
//
// Retry loops and fallback are handled internally.
type Pipeline interface {
	// Handle processes a single inbound HTTP request through all stages.
	Handle(ctx context.Context, req *GatewayRequest) (*GatewayResponse, error)
}
