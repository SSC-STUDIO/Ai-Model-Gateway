// Package gatewaycontrol defines the RPC contract for control-plane -> data-plane communication.
// This is the interface that controld uses to manage gatewayd.
package gatewaycontrol

import (
	"time"
)

// GatewayControlRPC is the interface exposed by gatewayd for controld.
// All methods are synchronous RPC calls.
type GatewayControlRPC interface {
	// ApplySnapshot sends a compiled runtime snapshot to gatewayd.
	// The snapshot replaces the current active runtime configuration atomically.
	ApplySnapshot(req ApplySnapshotRequest, resp *ApplySnapshotResponse) error

	// GetStatus returns the current runtime status of gatewayd.
	GetStatus(req GetStatusRequest, resp *GetStatusResponse) error

	// Drain signals gatewayd to stop accepting new requests and wait for
	// in-flight requests to complete. Used for graceful shutdown.
	Drain(req DrainRequest, resp *DrainResponse) error

	// GetPricingStatus returns the current live pricing refresh state.
	GetPricingStatus(req GetPricingStatusRequest, resp *GetPricingStatusResponse) error

	// RefreshPricing forces a live pricing refresh.
	RefreshPricing(req RefreshPricingRequest, resp *RefreshPricingResponse) error

	// RunBenchmarkCase executes a synthetic benchmark case through the live request pipeline.
	RunBenchmarkCase(req RunBenchmarkCaseRequest, resp *RunBenchmarkCaseResponse) error
}

// ApplySnapshotRequest contains the compiled runtime snapshot to apply.
type ApplySnapshotRequest struct {
	// SnapshotID is a unique identifier for this snapshot.
	SnapshotID string

	// RevisionID is the config revision this snapshot was compiled from.
	RevisionID string

	// SnapshotBytes is the serialized snapshot (YAML or JSON).
	SnapshotBytes []byte

	// SchemaVersion is the snapshot schema version.
	SchemaVersion int

	// GeneratedAt is when this snapshot was compiled.
	GeneratedAt time.Time

	// ForceApply bypasses validation (use with caution).
	ForceApply bool
}

// ApplySnapshotResponse is returned after applying a snapshot.
type ApplySnapshotResponse struct {
	// Applied indicates whether the snapshot was successfully applied.
	Applied bool

	// ActiveSnapshotID is the currently active snapshot ID after the operation.
	ActiveSnapshotID string

	// PreviousSnapshotID is the snapshot that was active before this operation.
	PreviousSnapshotID string

	// Error contains any error message if Apply is false.
	Error string

	// ValidationWarnings contains non-fatal validation issues.
	ValidationWarnings []string
}

// GetStatusRequest requests the current gatewayd status.
type GetStatusRequest struct {
	// IncludeDetails requests detailed provider health information.
	IncludeDetails bool
}

// GetStatusResponse contains the current gatewayd status.
type GetStatusResponse struct {
	// ActiveSnapshotID is the currently loaded snapshot ID.
	ActiveSnapshotID string

	// Readiness indicates if gatewayd is ready to serve requests.
	Readiness ReadinessState

	// ActiveRequests is the number of requests currently being processed.
	ActiveRequests int

	// Listener is the address gatewayd is listening on.
	Listener string

	// ProviderHealth contains health status for each provider.
	ProviderHealth map[string]ProviderHealth

	// Uptime is how long gatewayd has been running.
	Uptime time.Duration

	// StartedAt is when gatewayd was started.
	StartedAt time.Time

	// LastAutoRemediationReason describes the most recent automatic recovery action on the data plane.
	LastAutoRemediationReason string `json:"last_auto_remediation_reason,omitempty"`
	// LastAutoRemediationAt is when LastAutoRemediationReason was recorded.
	LastAutoRemediationAt time.Time `json:"last_auto_remediation_at,omitempty"`
}

// GetPricingStatusRequest requests the current pricing state.
type GetPricingStatusRequest struct{}

// GetPricingStatusResponse contains the live pricing state.
type GetPricingStatusResponse struct {
	SourceURL     string               `json:"source_url,omitempty"`
	UpdatedAt     time.Time            `json:"updated_at,omitempty"`
	LastAttemptAt time.Time            `json:"last_attempt_at,omitempty"`
	LastError     string               `json:"last_error,omitempty"`
	CatalogSize   int                  `json:"catalog_size"`
	Sources       []PricingSourceState `json:"sources,omitempty"`
	FX            PricingFXSnapshot    `json:"fx,omitempty"`
}

// RefreshPricingRequest triggers an immediate refresh.
type RefreshPricingRequest struct{}

// RefreshPricingResponse contains the post-refresh state.
type RefreshPricingResponse struct {
	Refreshed bool                     `json:"refreshed"`
	Error     string                   `json:"error,omitempty"`
	Status    GetPricingStatusResponse `json:"status"`
}

// RunBenchmarkCaseRequest describes one synthetic benchmark request execution.
type RunBenchmarkCaseRequest struct {
	RunID             string            `json:"run_id"`
	CaseID            string            `json:"case_id"`
	BenchmarkTargetID string            `json:"benchmark_target_id,omitempty"`
	ProviderID        string            `json:"provider_id"`
	PublicModel       string            `json:"public_model"`
	Protocol          string            `json:"protocol"`
	RequestBody       []byte            `json:"request_body"`
	Headers           map[string]string `json:"headers,omitempty"`
	TimeoutMs         int               `json:"timeout_ms"`
	SyntheticKind     string            `json:"synthetic_kind,omitempty"`
	DisableCache      bool              `json:"disable_cache,omitempty"`
	DisableFallback   bool              `json:"disable_fallback,omitempty"`
	DisableRetries    bool              `json:"disable_retries,omitempty"`
}

// RunBenchmarkCaseResponse captures the synthetic benchmark execution result.
type RunBenchmarkCaseResponse struct {
	StatusCode          int                 `json:"status_code"`
	Headers             map[string][]string `json:"headers,omitempty"`
	ResponseBody        []byte              `json:"response_body,omitempty"`
	ContentType         string              `json:"content_type,omitempty"`
	LatencyMs           int64               `json:"latency_ms"`
	PromptTokens        int64               `json:"prompt_tokens,omitempty"`
	CachedPromptTokens  int64               `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens    int64               `json:"completion_tokens,omitempty"`
	ProviderID          string              `json:"provider_id,omitempty"`
	EffectiveModel      string              `json:"effective_model,omitempty"`
	RouteMode           string              `json:"route_mode,omitempty"`
	PricingTotalCostUSD float64             `json:"pricing_total_cost_usd,omitempty"`
	Error               string              `json:"error,omitempty"`
}

// ReadinessState represents the readiness of gatewayd.
type ReadinessState int

const (
	ReadinessUnknown ReadinessState = iota
	ReadinessStarting
	ReadinessReady
	ReadinessDraining
	ReadinessStopped
)

func (s ReadinessState) String() string {
	switch s {
	case ReadinessStarting:
		return "starting"
	case ReadinessReady:
		return "ready"
	case ReadinessDraining:
		return "draining"
	case ReadinessStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ProviderHealth contains health information for a single provider.
type ProviderHealth struct {
	// Name is the provider identifier.
	Name string

	// UpstreamID is the logical upstream identity shared by providers that use
	// the same effective base URL.
	UpstreamID string

	// ProviderIDs are the concrete configured providers grouped into this
	// logical upstream.
	ProviderIDs []string

	// BaseURL is the configured provider base URL.
	BaseURL string

	// AnthropicBaseURL is the Anthropic-specific base URL, when configured.
	AnthropicBaseURL string

	// Healthy indicates if the provider is accepting requests.
	Healthy bool

	// LastCheck is when the last health check was performed.
	LastCheck time.Time

	// LastSuccess is when the last successful request was made.
	LastSuccess time.Time

	// ConsecutiveFailures is the count of consecutive failed requests.
	ConsecutiveFailures int

	// CooldownUntil is when the provider will be retried after being marked unhealthy.
	CooldownUntil time.Time

	// LatencyMs is the average latency over recent requests.
	LatencyMs int64
}

// PricingSourceState contains one pricing source refresh state.
type PricingSourceState struct {
	ID            string    `json:"id"`
	Vendor        string    `json:"vendor"`
	URL           string    `json:"url,omitempty"`
	Enabled       bool      `json:"enabled"`
	Status        string    `json:"status,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	ModelCount    int       `json:"model_count,omitempty"`
}

// PricingFXSnapshot contains FX refresh state.
type PricingFXSnapshot struct {
	Enabled       bool               `json:"enabled"`
	SourceURL     string             `json:"source_url,omitempty"`
	BaseCurrency  string             `json:"base_currency,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at,omitempty"`
	LastAttemptAt time.Time          `json:"last_attempt_at,omitempty"`
	LastError     string             `json:"last_error,omitempty"`
	RatesToUSD    map[string]float64 `json:"rates_to_usd,omitempty"`
}

// DrainRequest signals gatewayd to drain connections.
type DrainRequest struct {
	// Timeout is the maximum time to wait for in-flight requests.
	Timeout time.Duration

	// Force will terminate in-flight requests after timeout.
	Force bool
}

// DrainResponse contains the result of a drain operation.
type DrainResponse struct {
	// Success indicates if the drain completed successfully.
	Success bool

	// RemainingRequests is the number of requests still in-flight (if Force was false).
	RemainingRequests int

	// DrainedAt is when the drain completed.
	DrainedAt time.Time

	// Error contains any error message.
	Error string
}
