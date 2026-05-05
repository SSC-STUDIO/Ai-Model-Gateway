// Package snapshot defines the runtime snapshot model for gatewayd.
// A snapshot is a compiled, immutable execution artifact that gatewayd loads
// to configure its routing, providers, and policies.
package snapshot

import (
	"time"
)

// Snapshot is the compiled runtime configuration for gatewayd.
// It is machine-oriented, fully resolved, and immutable.
type Snapshot struct {
	// Meta contains snapshot metadata.
	Meta SnapshotMeta `yaml:"meta" json:"meta"`

	// Ingress contains the listener configuration.
	Ingress IngressConfig `yaml:"ingress" json:"ingress"`

	// Contract contains the public API contract configuration.
	Contract ContractConfig `yaml:"contract" json:"contract"`

	// Providers contains the compiled provider configurations.
	Providers []ProviderSnapshot `yaml:"providers" json:"providers"`

	// RoutingPolicy contains the routing and retry configuration.
	RoutingPolicy RoutingPolicy `yaml:"routing_policy" json:"routing_policy"`

	// CompatPolicy contains protocol/model compatibility rewrites.
	CompatPolicy CompatPolicy `yaml:"compat_policy" json:"compat_policy"`

	// TelemetryEmit contains the telemetry emission configuration.
	TelemetryEmit TelemetryEmitConfig `yaml:"telemetry_emit" json:"telemetry_emit"`

	// Pricing contains runtime pricing refresh and manual override configuration.
	Pricing PricingConfig `yaml:"pricing" json:"pricing"`
}

// SnapshotMeta contains metadata about the snapshot.
type SnapshotMeta struct {
	// SnapshotID is a unique identifier for this snapshot.
	SnapshotID string `yaml:"snapshot_id" json:"snapshot_id"`

	// SchemaVersion is the snapshot schema version.
	SchemaVersion int `yaml:"schema_version" json:"schema_version"`

	// RevisionID is the config revision this snapshot was compiled from.
	RevisionID string `yaml:"revision_id" json:"revision_id"`

	// GeneratedAt is when this snapshot was compiled.
	GeneratedAt time.Time `yaml:"generated_at" json:"generated_at"`

	// CompilerVersion is the version of the snapshot compiler.
	CompilerVersion string `yaml:"compiler_version" json:"compiler_version"`
}

// IngressConfig contains the listener configuration.
type IngressConfig struct {
	// Listen is the address to listen on (e.g., "127.0.0.1:18080").
	Listen string `yaml:"listen" json:"listen"`

	// ReadTimeoutMs is the read timeout in milliseconds.
	ReadTimeoutMs int `yaml:"read_timeout_ms" json:"read_timeout_ms"`

	// WriteTimeoutMs is the write timeout in milliseconds.
	WriteTimeoutMs int `yaml:"write_timeout_ms" json:"write_timeout_ms"`

	// IdleTimeoutMs is the idle timeout in milliseconds.
	IdleTimeoutMs int `yaml:"idle_timeout_ms" json:"idle_timeout_ms"`

	// MaxBodyBytes is the maximum request body size.
	MaxBodyBytes int64 `yaml:"max_body_bytes" json:"max_body_bytes"`
}

// ContractConfig contains the public API contract configuration.
type ContractConfig struct {
	// PublicAPI is the public API contract (e.g., "openai_chat_completions").
	PublicAPI string `yaml:"public_api" json:"public_api"`

	// EnabledRoutes is the list of enabled routes.
	EnabledRoutes []string `yaml:"enabled_routes" json:"enabled_routes"`
}

// ProviderSnapshot contains the compiled provider configuration.
type ProviderSnapshot struct {
	// ProviderID is the unique provider identifier.
	ProviderID string `yaml:"provider_id" json:"provider_id"`

	// ProtocolAdapter is the protocol adapter to use.
	ProtocolAdapter string `yaml:"protocol_adapter" json:"protocol_adapter"`

	// BaseURL is the provider's base URL.
	BaseURL string `yaml:"base_url" json:"base_url"`

	// AnthropicBaseURL is the Anthropic-specific base URL (for /v1/messages).
	// When set, /v1/messages requests use this URL instead of BaseURL.
	AnthropicBaseURL string `yaml:"anthropic_base_url" json:"anthropic_base_url,omitempty"`

	// Credentials contains the authentication credentials.
	Credentials Credentials `yaml:"credentials" json:"-"` // Omit from JSON for security

	// Headers contains additional headers to send.
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`

	// ModelTable contains the model mapping.
	ModelTable []ModelMapping `yaml:"model_table" json:"model_table"`

	// CapabilityTable contains the provider capabilities.
	CapabilityTable CapabilityTable `yaml:"capability_table" json:"capability_table"`

	// ExecutionPolicy contains the execution policy.
	ExecutionPolicy ExecutionPolicy `yaml:"execution_policy" json:"execution_policy"`

	// FallbackModels lists fallback model names when all providers for the primary model fail.
	FallbackModels []string `yaml:"fallback_models" json:"fallback_models,omitempty"`

	// APIKeys contains multiple API keys for rotation.
	APIKeys []APIKey `yaml:"api_keys" json:"api_keys,omitempty"`
}

// APIKey defines a rotatable API key.
type APIKey struct {
	Name      string `yaml:"name"       json:"name"`
	Value     string `yaml:"value"      json:"-"`
	Disabled  bool   `yaml:"disabled"   json:"disabled"`
	FailCount int    `yaml:"fail_count" json:"fail_count"`
}

// Credentials contains authentication credentials.
type Credentials struct {
	// Kind is the credential type (e.g., "bearer", "api_key").
	Kind string `yaml:"kind" json:"kind"`

	// Value is the credential value (API key, token, etc.).
	Value string `yaml:"value" json:"-"` // Omit from serialization

	// HeaderName is the header name for API key credentials.
	HeaderName string `yaml:"header_name,omitempty" json:"header_name,omitempty"`
}

// ModelMapping maps a public model to an upstream model.
type ModelMapping struct {
	// PublicModel is the model name exposed to clients.
	PublicModel string `yaml:"public_model" json:"public_model"`

	// UpstreamModel is the model name sent to the provider.
	UpstreamModel string `yaml:"upstream_model" json:"upstream_model"`
}

// CapabilityTable contains provider capabilities.
type CapabilityTable struct {
	// SupportsChatCompletions indicates support for chat completions.
	SupportsChatCompletions bool `yaml:"supports_chat_completions" json:"supports_chat_completions"`

	// SupportsStreaming indicates support for streaming responses.
	SupportsStreaming bool `yaml:"supports_streaming" json:"supports_streaming"`

	// UsageAccounting is the usage accounting method.
	UsageAccounting string `yaml:"usage_accounting" json:"usage_accounting"`

	// ErrorClassifier is the error classification method.
	ErrorClassifier string `yaml:"error_classifier" json:"error_classifier"`
}

// ExecutionPolicy contains the execution policy for a provider.
type ExecutionPolicy struct {
	// Enabled indicates if the provider is enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Weight is the selection weight.
	Weight int `yaml:"weight" json:"weight"`

	// TimeoutMs is the request timeout in milliseconds.
	TimeoutMs int `yaml:"timeout_ms" json:"timeout_ms"`

	// SameRetries is the number of retries on the same provider.
	SameRetries int `yaml:"same_retries" json:"same_retries"`

	// ProviderClass is the provider class (free, quota_limited).
	ProviderClass string `yaml:"provider_class" json:"provider_class"`
}

// RoutingPolicy contains the routing and retry configuration.
type RoutingPolicy struct {
	// Strategy is the compiled routing strategy (for example, round_robin or health_weighted_rr).
	Strategy string `yaml:"strategy" json:"strategy"`

	// MaxRetries is the maximum number of retries.
	MaxRetries int `yaml:"max_retries" json:"max_retries"`

	// RetryBackoff contains the retry backoff configuration.
	RetryBackoff RetryBackoff `yaml:"retry_backoff" json:"retry_backoff"`

	// Health contains the health check configuration.
	Health HealthConfig `yaml:"health" json:"health"`

	// StickySessions contains sticky-session affinity configuration.
	StickySessions StickySessionConfig `yaml:"sticky_sessions" json:"sticky_sessions"`

	// FailurePolicy contains the failure policy.
	FailurePolicy FailurePolicy `yaml:"failure_policy" json:"failure_policy"`

	// Retry contains the retry policy.
	Retry RetryPolicy `yaml:"retry" json:"retry"`

	// RateLimit contains the rate limit configuration.
	RateLimit RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`

	// Cache contains the cache configuration.
	Cache CacheConfig `yaml:"cache" json:"cache"`

	// Queue contains the queue configuration.
	Queue QueueConfig `yaml:"queue" json:"queue"`

	// KeyRotation contains the key rotation configuration.
	KeyRotation KeyRotationConfig `yaml:"key_rotation" json:"key_rotation"`

	// Compression contains the compression configuration.
	Compression CompressionConfig `yaml:"compression" json:"compression"`
}

// CompatPolicy contains compatibility rules compiled for gatewayd.
type CompatPolicy struct {
	Bridge BridgePolicy `yaml:"bridge" json:"bridge"`
}

// BridgePolicy controls model name rewriting before provider selection.
type BridgePolicy struct {
	Enabled           bool         `yaml:"enabled" json:"enabled"`
	Rules             []BridgeRule `yaml:"rules" json:"rules"`
	ExcludeUserAgents []string     `yaml:"exclude_user_agents" json:"exclude_user_agents,omitempty"`
}

// BridgeRule defines a model name rewrite rule.
type BridgeRule struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

// StickySessionConfig contains sticky-session configuration.
type StickySessionConfig struct {
	// Enabled indicates if sticky sessions are enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// TTLSec is the affinity TTL in seconds.
	TTLSec int `yaml:"ttl_sec" json:"ttl_sec"`
}

// RetryBackoff contains retry backoff configuration.
type RetryBackoff struct {
	// InitialMs is the initial backoff in milliseconds.
	InitialMs int `yaml:"initial_ms" json:"initial_ms"`

	// MaxMs is the maximum backoff in milliseconds.
	MaxMs int `yaml:"max_ms" json:"max_ms"`
}

// HealthConfig contains health check configuration.
type HealthConfig struct {
	// Enabled indicates if health checks are enabled.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// IntervalSec is the health check interval in seconds.
	IntervalSec int `yaml:"interval_sec" json:"interval_sec"`

	// TimeoutMs is the health check timeout in milliseconds.
	TimeoutMs int `yaml:"timeout_ms" json:"timeout_ms"`

	// Path is the health check endpoint path.
	Path string `yaml:"path" json:"path"`
}

// FailurePolicy contains the failure policy configuration.
type FailurePolicy struct {
	// Threshold is the consecutive failure threshold.
	Threshold int `yaml:"threshold" json:"threshold"`

	// CooldownSec is the cooldown period in seconds.
	CooldownSec int `yaml:"cooldown_sec" json:"cooldown_sec"`

	// PassthroughAfterSec is how long to wait before allowing passthrough.
	PassthroughAfterSec int `yaml:"passthrough_after_sec" json:"passthrough_after_sec"`

	// QuotaRecoveryIntervalMin is the quota recovery interval in minutes.
	QuotaRecoveryIntervalMin int `yaml:"quota_recovery_interval_min" json:"quota_recovery_interval_min"`

	// DisableCooldown bypasses runtime provider blocking after upstream failures.
	DisableCooldown bool `yaml:"disable_cooldown" json:"disable_cooldown"`
}

// RetryPolicy contains the retry policy configuration.
type RetryPolicy struct {
	// InfiniteOnError enables infinite retries on retryable errors.
	InfiniteOnError bool `yaml:"infinite_on_error" json:"infinite_on_error"`

	// AllErrors retries every upstream HTTP error response.
	AllErrors bool `yaml:"all_errors" json:"all_errors"`

	// StatusCodes is the list of retryable status codes.
	StatusCodes []int `yaml:"status_codes" json:"status_codes"`

	// StatusCodeMin is the minimum status code for retry.
	StatusCodeMin int `yaml:"status_code_min" json:"status_code_min"`

	// MessageKeywords is the list of retryable error message keywords.
	MessageKeywords []string `yaml:"message_keywords" json:"message_keywords"`
}

// TelemetryEmitConfig contains telemetry emission configuration.
type TelemetryEmitConfig struct {
	// Channel is the telemetry channel (IPC endpoint name).
	Channel string `yaml:"channel" json:"channel"`

	// Batching contains the batching configuration.
	Batching BatchingConfig `yaml:"batching" json:"batching"`
}

// PricingConfig contains runtime pricing catalog configuration.
type PricingConfig struct {
	CachePath              string               `yaml:"cache_path"                json:"cache_path"`
	RefreshIntervalMinutes int                  `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
	RequestTimeoutMs       int                  `yaml:"request_timeout_ms"        json:"request_timeout_ms"`
	Sources                []PricingSource      `yaml:"sources"                   json:"sources"`
	FX                     PricingFXConfig      `yaml:"fx"                        json:"fx"`
	ManualPrices           []PricingManualPrice `yaml:"manual_prices"             json:"manual_prices"`
}

// PricingSource defines a single official source to refresh.
type PricingSource struct {
	ID                     string `yaml:"id"                        json:"id"`
	Vendor                 string `yaml:"vendor"                    json:"vendor"`
	URL                    string `yaml:"url"                       json:"url"`
	Enabled                bool   `yaml:"enabled"                   json:"enabled"`
	TimeoutMs              int    `yaml:"timeout_ms"                json:"timeout_ms"`
	RefreshIntervalMinutes int    `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
}

// PricingFXConfig defines runtime FX normalization settings.
type PricingFXConfig struct {
	Enabled                bool   `yaml:"enabled"                   json:"enabled"`
	CachePath              string `yaml:"cache_path"                json:"cache_path"`
	RefreshIntervalMinutes int    `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
}

// PricingManualPrice defines a compiled manual pricing override.
type PricingManualPrice struct {
	Provider         string  `yaml:"provider,omitempty"             json:"provider,omitempty"`
	Model            string  `yaml:"model"                          json:"model"`
	Currency         string  `yaml:"currency,omitempty"             json:"currency,omitempty"`
	InputPer1M       float64 `yaml:"input_per_1m"                   json:"input_per_1m"`
	CachedInputPer1M float64 `yaml:"cached_input_per_1m,omitempty"  json:"cached_input_per_1m,omitempty"`
	OutputPer1M      float64 `yaml:"output_per_1m"                  json:"output_per_1m"`
	Enabled          bool    `yaml:"enabled"                        json:"enabled"`
	Source           string  `yaml:"source,omitempty"               json:"source,omitempty"`
}

// BatchingConfig contains batching configuration.
type BatchingConfig struct {
	// MaxBatchSize is the maximum batch size.
	MaxBatchSize int `yaml:"max_batch_size" json:"max_batch_size"`

	// FlushIntervalMs is the flush interval in milliseconds.
	FlushIntervalMs int `yaml:"flush_interval_ms" json:"flush_interval_ms"`
}

// RateLimitConfig contains rate limit configuration.
type RateLimitConfig struct {
	Enabled           bool    `yaml:"enabled"             json:"enabled"`
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int     `yaml:"burst"               json:"burst"`
}

// CacheConfig contains cache configuration.
type CacheConfig struct {
	Enabled    bool `yaml:"enabled"     json:"enabled"`
	MaxEntries int  `yaml:"max_entries" json:"max_entries"`
	TTLSec     int  `yaml:"ttl_seconds" json:"ttl_seconds"`
	// MaxBytesMB is the maximum total cached bytes in megabytes (0 = no byte limit).
	MaxBytesMB int `yaml:"max_bytes_mb" json:"max_bytes_mb"`
}

// QueueConfig contains queue configuration.
type QueueConfig struct {
	Enabled         bool `yaml:"enabled"          json:"enabled"`
	MaxConcurrent   int  `yaml:"max_concurrent"   json:"max_concurrent"`
	HighPriorityPct int  `yaml:"high_priority_pct" json:"high_priority_pct"`
}

// KeyRotationConfig contains key rotation configuration.
type KeyRotationConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// CompressionConfig contains compression configuration.
type CompressionConfig struct {
	Enabled      bool `yaml:"enabled"        json:"enabled"`
	MinSizeBytes int  `yaml:"min_size_bytes" json:"min_size_bytes"`
	Level        int  `yaml:"level"          json:"level"`
}

// CurrentSchemaVersion is the current snapshot schema version.
const CurrentSchemaVersion = 1
