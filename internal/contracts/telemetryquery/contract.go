// Package telemetryquery defines the RPC contract for control-plane -> telemetry-plane queries.
// This is the interface that controld uses to query telemetry data from telemetryd.
package telemetryquery

import (
	"time"
)

// TelemetryQueryRPC is the interface exposed by telemetryd for controld.
// All methods are read-only queries.
type TelemetryQueryRPC interface {
	// GetOverview returns aggregated metrics for dashboard overview.
	GetOverview(req OverviewRequest, resp *OverviewResponse) error

	// GetTelemetry returns recent telemetry events.
	GetTelemetry(req TelemetryRequest, resp *TelemetryResponse) error

	// GetTimeSeries returns time-bucketed metrics for charts.
	GetTimeSeries(req TimeSeriesRequest, resp *TimeSeriesResponse) error

	// GetModelBenchmark returns benchmark metrics for model comparison.
	GetModelBenchmark(req BenchmarkRequest, resp *BenchmarkResponse) error

	// Ping checks if telemetryd is healthy and responsive.
	Ping(req PingRequest, resp *PingResponse) error
}

// OverviewRequest requests dashboard overview metrics.
type OverviewRequest struct {
	// WindowSets specifies which time windows to include.
	WindowSets []WindowSpec
}

// WindowSpec defines a time window for aggregation.
type WindowSpec struct {
	// Name is a human-readable name (e.g., "last_1m", "last_24h").
	Name string

	// Duration is the window duration.
	Duration time.Duration
}

// OverviewResponse contains dashboard overview metrics.
type OverviewResponse struct {
	// Windows contains metrics for each requested window.
	Windows map[string]WindowMetrics

	// Runtime contains runtime information.
	Runtime RuntimeInfo

	// AvailableModels is the list of models that have been used.
	AvailableModels []string
}

// WindowMetrics contains aggregated metrics for a time window.
type WindowMetrics struct {
	// Requests is the total number of requests.
	Requests int64

	// Successes is the number of successful requests (2xx).
	Successes int64

	// Failures is the number of failed requests (4xx, 5xx).
	Failures int64

	// AvgLatencyMs is the average latency in milliseconds.
	AvgLatencyMs float64

	// InputTokens is the total input tokens used.
	InputTokens int64

	// CachedPromptTokens is the total cached prompt tokens.
	CachedPromptTokens int64

	// OutputTokens is the total output tokens generated.
	OutputTokens int64
}

// RuntimeInfo contains runtime information.
type RuntimeInfo struct {
	// ProviderCount is the number of configured providers.
	ProviderCount int

	// EnabledProviderCount is the number of enabled providers.
	EnabledProviderCount int

	// RouterStrategy is the active routing strategy.
	RouterStrategy string

	// HealthEnabled indicates if health checks are enabled.
	HealthEnabled bool

	// StickySessionsEnabled indicates if sticky sessions are enabled.
	StickySessionsEnabled bool

	// BridgeEnabled indicates if model bridging is enabled.
	BridgeEnabled bool
}

// TelemetryRequest requests recent telemetry events.
type TelemetryRequest struct {
	// WindowHours is the time window in hours.
	WindowHours int

	// Limit is the maximum number of events to return.
	Limit int

	// Offset is the pagination offset.
	Offset int

	// Filters specifies optional filters.
	Filters TelemetryFilters
}

// TelemetryFilters specifies filters for telemetry queries.
type TelemetryFilters struct {
	// Models filters by model names.
	Models []string

	// Providers filters by provider names.
	Providers []string

	// StatusCodes filters by HTTP status codes.
	StatusCodes []int

	// ErrorsOnly returns only events with errors.
	ErrorsOnly bool

	// MinLatencyMs filters by minimum latency.
	MinLatencyMs int64

	// MaxLatencyMs filters by maximum latency.
	MaxLatencyMs int64

	// SyntheticKind filters synthetic traffic by kind when set.
	SyntheticKind string

	// BenchmarkRunID filters benchmark synthetic traffic to one run when set.
	BenchmarkRunID string

	// BenchmarkTargetID filters benchmark synthetic traffic to one target when set.
	BenchmarkTargetID string

	// BenchmarkCaseID filters benchmark synthetic traffic to one case when set.
	BenchmarkCaseID string
}

// TelemetryResponse contains telemetry events.
type TelemetryResponse struct {
	// Events is the list of telemetry events.
	Events []TelemetryEvent

	// Total is the total number of matching events (for pagination).
	Total int64

	// WindowHours is the actual window used.
	WindowHours int

	// Models contains full-window request distribution by model.
	Models []TelemetryDistributionItem

	// Upstreams contains full-window request distribution by upstream provider.
	Upstreams []TelemetryDistributionItem

	// Pricing contains pricing economics snapshot.
	Pricing PricingEconomics
}

// TelemetryDistributionItem contains full-window distribution metrics.
type TelemetryDistributionItem struct {
	// Value is the model or upstream provider value.
	Value string

	// Requests is the total requests for the value.
	Requests int64

	// Successes is the successful requests for the value.
	Successes int64

	// Failures is the failed requests for the value.
	Failures int64

	// InputTokens is the input tokens used.
	InputTokens int64

	// CachedPromptTokens is the cached prompt tokens.
	CachedPromptTokens int64

	// OutputTokens is the output tokens generated.
	OutputTokens int64

	// AvgLatencyMs is the average latency.
	AvgLatencyMs float64
}

// PricingEconomics contains pricing economics data for the telemetry response.
type PricingEconomics struct {
	// Summary is the pricing summary.
	Summary PricingSummary `json:"summary"`

	// Models is the per-model pricing breakdown.
	Models []PricingModelSummary `json:"models"`
}

// PricingSummary contains aggregated pricing information.
type PricingSummary struct {
	Currency           string                   `json:"currency"`
	Prompt             float64                  `json:"prompt"`
	Completion         float64                  `json:"completion"`
	Total              float64                  `json:"total"`
	PromptUsd          float64                  `json:"prompt_usd,omitempty"`
	CompletionUsd      float64                  `json:"completion_usd,omitempty"`
	TotalUsd           float64                  `json:"total_usd,omitempty"`
	CachedPromptTokens int64                    `json:"cached_prompt_tokens"`
	CacheSavings       float64                  `json:"cache_savings"`
	CacheSavingsUsd    float64                  `json:"cache_savings_usd,omitempty"`
	PricedModels       int                      `json:"priced_models"`
	UnpricedModels     int                      `json:"unpriced_models"`
	ExactTotalUsd      float64                  `json:"exact_total_usd,omitempty"`
	EstimatedTotalUsd  float64                  `json:"estimated_total_usd,omitempty"`
	ExactRequests      int64                    `json:"exact_requests,omitempty"`
	EstimatedRequests  int64                    `json:"estimated_requests,omitempty"`
	ExactModels        int                      `json:"exact_models,omitempty"`
	EstimatedModels    int                      `json:"estimated_models,omitempty"`
	TotalsByCurrency   []PricingCurrencySummary `json:"totals_by_currency,omitempty"`
}

// PricingCurrencySummary contains totals grouped by native currency.
type PricingCurrencySummary struct {
	Currency     string  `json:"currency"`
	Prompt       float64 `json:"prompt"`
	Completion   float64 `json:"completion"`
	Total        float64 `json:"total"`
	CacheSavings float64 `json:"cache_savings"`
	PricedModels int     `json:"priced_models"`
}

// PricingModelSummary contains pricing breakdown for a single model.
type PricingModelSummary struct {
	DisplayModel    string       `json:"display_model"`
	RequestedModel  string       `json:"requested_model,omitempty"`
	EffectiveModel  string       `json:"effective_model,omitempty"`
	Upstream        string       `json:"upstream,omitempty"`
	PricingModel    string       `json:"pricing_model,omitempty"`
	PricingStatus   string       `json:"pricing_status,omitempty"`
	PricingSourceID string       `json:"pricing_source_id,omitempty"`
	Usage           PricingUsage `json:"usage"`
	Cost            PricingCost  `json:"cost"`
}

// PricingUsage contains token usage for pricing calculation.
type PricingUsage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CachedPromptTokens int `json:"cached_prompt_tokens,omitempty"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
}

// PricingCost contains cost breakdown.
type PricingCost struct {
	Currency      string  `json:"currency,omitempty"`
	Prompt        float64 `json:"prompt"`
	Completion    float64 `json:"completion"`
	Total         float64 `json:"total"`
	PromptUsd     float64 `json:"prompt_usd,omitempty"`
	CompletionUsd float64 `json:"completion_usd,omitempty"`
	TotalUsd      float64 `json:"total_usd,omitempty"`
}

// TelemetryEvent represents a single telemetry event for queries.
type TelemetryEvent struct {
	// EventID is the unique event identifier.
	EventID string

	// Timestamp is when the event occurred.
	Timestamp time.Time

	// RequestID is the request identifier.
	RequestID string

	// Path is the request path.
	Path string

	// RequestedModel is the originally requested model.
	RequestedModel string

	// EffectiveModel is the model that was used.
	EffectiveModel string

	// Provider is the provider that handled the request.
	Provider string

	// RouteMode is how the request was routed.
	RouteMode string

	// StatusCode is the HTTP status code.
	StatusCode int

	// LatencyMs is the latency in milliseconds.
	LatencyMs int64

	// Attempts is the number of attempts.
	Attempts int

	// InputTokens is the input tokens used.
	InputTokens int64

	// CachedPromptTokens is the cached prompt tokens.
	CachedPromptTokens int64

	// OutputTokens is the output tokens generated.
	OutputTokens int64

	// PricingStatus indicates whether request cost was fixed or estimated.
	PricingStatus string

	// PricingTotalCostUSD is the fixed or estimated total cost in USD.
	PricingTotalCostUSD float64

	// SyntheticKind marks synthetic traffic excluded from standard dashboards.
	SyntheticKind string

	// BenchmarkRunID identifies the benchmark verification run for synthetic traffic.
	BenchmarkRunID string

	// BenchmarkTargetID identifies the benchmark verification target for synthetic traffic.
	BenchmarkTargetID string

	// BenchmarkCaseID identifies the benchmark verification case for synthetic traffic.
	BenchmarkCaseID string

	// Stream indicates if this was a streaming request.
	Stream bool

	// Error contains any error message.
	Error string
}

// TimeSeriesRequest requests time-bucketed metrics.
type TimeSeriesRequest struct {
	// WindowHours is the time window in hours.
	WindowHours int

	// BucketMinutes is the bucket size in minutes.
	BucketMinutes int

	// GroupBy specifies optional grouping (model, provider).
	GroupBy string
}

// TimeSeriesResponse contains time-bucketed metrics.
type TimeSeriesResponse struct {
	// Buckets is the list of time buckets.
	Buckets []TimeBucket

	// WindowHours is the actual window used.
	WindowHours int

	// BucketMinutes is the actual bucket size used.
	BucketMinutes int
}

// TimeBucket represents metrics for a single time bucket.
type TimeBucket struct {
	// Bucket is the bucket timestamp (ISO 8601).
	Bucket string

	// Requests is the total requests in this bucket.
	Requests int64

	// Successes is the successful requests.
	Successes int64

	// Failures is the failed requests.
	Failures int64

	// InputTokens is the input tokens used.
	InputTokens int64

	// CachedPromptTokens is the cached prompt tokens.
	CachedPromptTokens int64

	// OutputTokens is the output tokens generated.
	OutputTokens int64

	// AvgLatencyMs is the average latency.
	AvgLatencyMs float64

	// GroupValue is the group-by value (if grouped).
	GroupValue string
}

// BenchmarkRequest requests model benchmark metrics.
type BenchmarkRequest struct {
	// WindowHours is the time window in hours.
	WindowHours int

	// Models specifies which models to include (empty = all).
	Models []string

	// Group selects the benchmark grouping dimension: "model" or "upstream".
	Group string

	// StartTime is an optional explicit start time.
	StartTime *time.Time

	// EndTime is an optional explicit end time.
	EndTime *time.Time
}

// BenchmarkResponse contains model benchmark metrics.
type BenchmarkResponse struct {
	// Benchmarks is the list of model benchmarks.
	Benchmarks []ModelBenchmark

	// WindowHours is the actual window used.
	WindowHours int

	// ModelCount is the number of models included.
	ModelCount int

	// Group is the grouping dimension used for the response.
	Group string
}

// ModelBenchmark contains benchmark metrics for a single model.
type ModelBenchmark struct {
	// Model is the model name.
	Model string

	// Upstream is the upstream provider name when grouped by upstream.
	Upstream string

	// Label is the display label for the grouped row.
	Label string

	// Requests is the total requests.
	Requests int64

	// Successes is the successful requests.
	Successes int64

	// Failures is the failed requests.
	Failures int64

	// InputTokens is the input tokens used.
	InputTokens int64

	// CachedPromptTokens is the cached prompt tokens.
	CachedPromptTokens int64

	// OutputTokens is the output tokens generated.
	OutputTokens int64

	// AvgLatencyMs is the average latency.
	AvgLatencyMs float64

	// P50LatencyMs is the 50th percentile latency.
	P50LatencyMs float64

	// P95LatencyMs is the 95th percentile latency.
	P95LatencyMs float64

	// P99LatencyMs is the 99th percentile latency.
	P99LatencyMs float64

	// MaxLatencyMs is the maximum latency.
	MaxLatencyMs int64

	// SuccessRate is the success rate percentage.
	SuccessRate float64

	// EstimatedCostUSD is the estimated cost in USD.
	EstimatedCostUSD float64

	// ExactCostUSD is the fixed cost in USD from persisted pricing rows.
	ExactCostUSD float64

	// EstimatedLegacyCostUSD is the non-fixed legacy estimate in USD, if available.
	EstimatedLegacyCostUSD float64
}

// PingRequest checks telemetryd health.
type PingRequest struct {
	// Timestamp is when the ping was sent.
	Timestamp time.Time
}

// PingResponse is returned from a ping.
type PingResponse struct {
	// Version is the telemetryd version.
	Version string

	// ServerTime is the current server time.
	ServerTime time.Time

	// EventCount is the total number of events stored.
	EventCount int64

	// Healthy indicates if telemetryd is healthy.
	Healthy bool
}
