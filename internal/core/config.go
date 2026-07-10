package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Configuration — maps to config.yaml
// Top-level: server, admin, routing, providers, telemetry, pricing, compat
// ---------------------------------------------------------------------------

// Config holds the complete gateway configuration.
type Config struct {
	Server       ServerConfig       `yaml:"server"       json:"server"`
	Admin        AdminConfig        `yaml:"admin"        json:"admin"`
	Routing      RoutingConfig      `yaml:"routing"      json:"routing"`
	Providers    []Provider         `yaml:"providers"    json:"providers"`
	Telemetry    TelemetryConfig    `yaml:"telemetry"    json:"telemetry"`
	Pricing      PricingConfig      `yaml:"pricing"      json:"pricing"`
	Benchmarking BenchmarkingConfig `yaml:"benchmarking" json:"benchmarking"`
	Compat       CompatConfig       `yaml:"compat"       json:"compat"`
}

// ServerConfig controls the network listener and general server behaviour.
type ServerConfig struct {
	Listen         string `yaml:"listen"           json:"listen"`
	ReadTimeoutMs  int    `yaml:"read_timeout_ms"  json:"read_timeout_ms"`
	WriteTimeoutMs int    `yaml:"write_timeout_ms" json:"write_timeout_ms"`
	IdleTimeoutMs  int    `yaml:"idle_timeout_ms"  json:"idle_timeout_ms"`
	MaxBodyBytes   int64  `yaml:"max_body_bytes"   json:"max_body_bytes"`
}

// AdminConfig controls the admin API and frontend.
type AdminConfig struct {
	Enabled             bool          `yaml:"enabled"            json:"enabled"`
	BootstrapToken      string        `yaml:"bootstrap_token"    json:"-"`
	CookieSigningKey    string        `yaml:"cookie_signing_key" json:"-"`
	PublishHistoryLimit int           `yaml:"publish_history_limit" json:"publish_history_limit"`
	Tokens              []TokenConfig `yaml:"tokens"             json:"tokens"`
	Language            string        `yaml:"language"            json:"language"`
	RateLimit           struct {
		RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
		Burst             int     `yaml:"burst"               json:"burst"`
	} `yaml:"rate_limit" json:"rate_limit"`
}

// TokenConfig defines a named API token with a role.
type TokenConfig struct {
	Name  string `yaml:"name"  json:"name"`
	Token string `yaml:"token" json:"-"`
	Role  string `yaml:"role"  json:"role"` // "admin" or "viewer"
}

// RoutingConfig controls request routing, retry, and health-check behaviour.
type RoutingConfig struct {
	Strategy       string              `yaml:"strategy"        json:"strategy"`
	MaxRetries     int                 `yaml:"max_retries"     json:"max_retries"`
	RetryBackoff   RetryBackoffConfig  `yaml:"retry_backoff"   json:"retry_backoff"`
	Health         HealthCheckConfig   `yaml:"health"          json:"health"`
	StickySessions StickySessionConfig `yaml:"sticky_sessions" json:"sticky_sessions"`
	FailurePolicy  FailurePolicyConfig `yaml:"failure_policy"  json:"failure_policy"`
	Retry          RetryPolicyConfig   `yaml:"retry"           json:"retry"`
	Intercepts     []InterceptRule     `yaml:"intercepts"      json:"intercepts"`
	RateLimit      RateLimitConfig     `yaml:"rate_limit"      json:"rate_limit"`
	Cache          CacheConfig         `yaml:"cache"           json:"cache"`
	KeyRotation    KeyRotationConfig   `yaml:"key_rotation"    json:"key_rotation"`
	Queue          QueueConfig         `yaml:"queue"           json:"queue"`
	Compression    CompressionConfig   `yaml:"compression"     json:"compression"`
	SSRF           SSRFConfig          `yaml:"ssrf"           json:"ssrf"`
}

// RetryBackoffConfig controls exponential backoff between retries.
type RetryBackoffConfig struct {
	InitialMs int `yaml:"initial_ms" json:"initial_ms"`
	MaxMs     int `yaml:"max_ms"     json:"max_ms"`
}

// HealthCheckConfig controls upstream health probing.
type HealthCheckConfig struct {
	Enabled     bool   `yaml:"enabled"      json:"enabled"`
	IntervalSec int    `yaml:"interval_sec" json:"interval_sec"`
	TimeoutMs   int    `yaml:"timeout_ms"   json:"timeout_ms"`
	Path        string `yaml:"path"         json:"path"`
}

// StickySessionConfig controls sticky-session affinity.
type StickySessionConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	TTLSec  int  `yaml:"ttl_sec" json:"ttl_sec"`
}

// FailurePolicyConfig controls circuit-breaker / cooldown behaviour.
type FailurePolicyConfig struct {
	Threshold                int  `yaml:"threshold"                   json:"threshold"`
	CooldownSec              int  `yaml:"cooldown_sec"                json:"cooldown_sec"`
	PassthroughAfterSec      int  `yaml:"passthrough_after_sec"       json:"passthrough_after_sec"`
	QuotaRecoveryIntervalMin int  `yaml:"quota_recovery_interval_min" json:"quota_recovery_interval_min"`
	DisableCooldown          bool `yaml:"disable_cooldown"           json:"disable_cooldown"`
}

// RetryPolicyConfig defines when requests should be retried.
type RetryPolicyConfig struct {
	InfiniteOnError bool     `yaml:"infinite_on_error" json:"infinite_on_error"`
	MaxElapsedMs    int      `yaml:"max_elapsed_ms"    json:"max_elapsed_ms"`
	AllErrors       bool     `yaml:"all_errors"        json:"all_errors"`
	StatusCodes     []int    `yaml:"status_codes"      json:"status_codes"`
	StatusCodeMin   *int     `yaml:"status_code_min"   json:"status_code_min,omitempty"`
	MessageKeywords []string `yaml:"message_keywords"  json:"message_keywords"`
}

// InterceptRule defines a rule for intercepting specific upstream responses.
type InterceptRule struct {
	Name            string   `yaml:"name"             json:"name"`
	Enabled         *bool    `yaml:"enabled"          json:"enabled"`
	Paths           []string `yaml:"paths"            json:"paths"`
	StatusCodes     []int    `yaml:"status_codes"     json:"status_codes"`
	StatusCodeMin   *int     `yaml:"status_code_min"  json:"status_code_min,omitempty"`
	MessageKeywords []string `yaml:"message_keywords" json:"message_keywords"`
	Action          string   `yaml:"action"           json:"action"`
}

// RateLimitConfig controls request rate limiting.
type RateLimitConfig struct {
	Enabled           bool    `yaml:"enabled"             json:"enabled"`
	RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int     `yaml:"burst"               json:"burst"`
}

// CacheConfig controls request caching.
type CacheConfig struct {
	Enabled    bool `yaml:"enabled"     json:"enabled"`
	MaxEntries int  `yaml:"max_entries" json:"max_entries"`
	TTLSec     int  `yaml:"ttl_seconds" json:"ttl_seconds"`
}

// KeyRotationConfig controls API key rotation.
type KeyRotationConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// QueueConfig controls request queuing.
type QueueConfig struct {
	MaxConcurrent   int `yaml:"max_concurrent"    json:"max_concurrent"`
	HighPriorityPct int `yaml:"high_priority_pct" json:"high_priority_pct"`
}

// CompressionConfig controls response compression.
type CompressionConfig struct {
	Level        int `yaml:"level"         json:"level"`
	MinSizeBytes int `yaml:"min_size_bytes" json:"min_size_bytes"`
}

// SSRFConfig controls SSRF protection for upstream connections.
type SSRFConfig struct {
	AllowLocalhost bool `yaml:"allow_localhost" json:"allow_localhost"`
	AllowPrivateIP bool `yaml:"allow_private_ip" json:"allow_private_ip"`
}

// IsEnabled returns whether the intercept rule is enabled (defaults to true).
func (r *InterceptRule) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// TelemetryConfig controls telemetry storage (SQLite).
type TelemetryConfig struct {
	SQLitePath     string `yaml:"sqlite_path"      json:"sqlite_path"`
	RetentionDays  int    `yaml:"retention_days"    json:"retention_days"`
	AggregationSec int    `yaml:"aggregation_sec"   json:"aggregation_sec"`
	CacheTTLSec    int    `yaml:"cache_ttl_sec"     json:"cache_ttl_sec"`
}

// PricingConfig controls pricing cache and refresh behaviour.
type PricingConfig struct {
	CachePath              string                `yaml:"cache_path"                json:"cache_path"`
	RefreshIntervalHours   int                   `yaml:"refresh_interval_hours"    json:"refresh_interval_hours"`
	RefreshIntervalMinutes int                   `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
	RequestTimeoutMs       int                   `yaml:"request_timeout_ms"        json:"request_timeout_ms"`
	Sources                []PricingSourceConfig `yaml:"sources,omitempty"         json:"sources,omitempty"`
	FX                     PricingFXConfig       `yaml:"fx"                        json:"fx"`
	ManualPrices           []PricingManualPrice  `yaml:"manual_prices"             json:"manual_prices"`
	sourcesSet             bool
}

type pricingConfigAlias PricingConfig

func (p *PricingConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawPricingConfig pricingConfigAlias
	var raw rawPricingConfig
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = PricingConfig(raw)
	p.sourcesSet = mappingHasKey(value, "sources")
	return nil
}

func (p *PricingConfig) UnmarshalJSON(data []byte) error {
	type rawPricingConfig pricingConfigAlias
	var raw rawPricingConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*p = PricingConfig(raw)
	_, p.sourcesSet = fields["sources"]
	return nil
}

func mappingHasKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

// PricingSourceConfig defines an official pricing source to poll.
type PricingSourceConfig struct {
	ID                     string `yaml:"id"                        json:"id"`
	Vendor                 string `yaml:"vendor"                    json:"vendor"`
	URL                    string `yaml:"url"                       json:"url"`
	Enabled                *bool  `yaml:"enabled,omitempty"         json:"enabled,omitempty"`
	TimeoutMs              int    `yaml:"timeout_ms"                json:"timeout_ms"`
	RefreshIntervalMinutes int    `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
}

// IsEnabled returns whether the pricing source is enabled (defaults to true).
func (s PricingSourceConfig) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

// PricingFXConfig defines foreign exchange refresh behaviour for USD normalization.
type PricingFXConfig struct {
	Enabled                *bool  `yaml:"enabled,omitempty"         json:"enabled,omitempty"`
	CachePath              string `yaml:"cache_path"                json:"cache_path"`
	RefreshIntervalMinutes int    `yaml:"refresh_interval_minutes"  json:"refresh_interval_minutes"`
}

// IsEnabled returns whether FX normalization is enabled (defaults to true).
func (c PricingFXConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// PricingManualPrice allows operator-defined pricing overrides by model and optional provider.
type PricingManualPrice struct {
	Provider         string  `yaml:"provider,omitempty"            json:"provider,omitempty"`
	Model            string  `yaml:"model"                         json:"model"`
	Currency         string  `yaml:"currency,omitempty"            json:"currency,omitempty"`
	InputPer1M       float64 `yaml:"input_per_1m"                json:"input_per_1m"`
	CachedInputPer1M float64 `yaml:"cached_input_per_1m,omitempty" json:"cached_input_per_1m,omitempty"`
	OutputPer1M      float64 `yaml:"output_per_1m"               json:"output_per_1m"`
	Enabled          *bool   `yaml:"enabled,omitempty"            json:"enabled,omitempty"`
	Source           string  `yaml:"source,omitempty"             json:"source,omitempty"`
}

// IsEnabled returns whether the manual pricing rule is enabled (defaults to true).
func (p PricingManualPrice) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// BenchmarkingConfig controls model verification benchmark execution.
type BenchmarkingConfig struct {
	Enabled           bool                       `yaml:"enabled"            json:"enabled"`
	DefaultSuite      string                     `yaml:"default_suite"      json:"default_suite"`
	Judge             BenchmarkJudgeConfig       `yaml:"judge"              json:"judge"`
	Limits            BenchmarkLimitsConfig      `yaml:"limits"             json:"limits"`
	VerdictThresholds BenchmarkVerdictThresholds `yaml:"verdict_thresholds" json:"verdict_thresholds"`
	Aliases           []BenchmarkAliasConfig     `yaml:"aliases"            json:"aliases"`
}

// BenchmarkJudgeConfig defines the judge model route used for open-ended cases.
type BenchmarkJudgeConfig struct {
	PublicModel string `yaml:"public_model" json:"public_model"`
	Provider    string `yaml:"provider"     json:"provider,omitempty"`
	TimeoutMs   int    `yaml:"timeout_ms"   json:"timeout_ms"`
}

// BenchmarkLimitsConfig controls benchmark run concurrency and case timeouts.
type BenchmarkLimitsConfig struct {
	MaxParallelRuns  int `yaml:"max_parallel_runs"  json:"max_parallel_runs"`
	MaxParallelCases int `yaml:"max_parallel_cases" json:"max_parallel_cases"`
	PerCaseTimeoutMs int `yaml:"per_case_timeout_ms" json:"per_case_timeout_ms"`
}

// BenchmarkVerdictThresholds controls the suspicion verdict thresholds.
type BenchmarkVerdictThresholds struct {
	NormalMaxGap                float64 `yaml:"normal_max_gap"                   json:"normal_max_gap"`
	SuspectMaxGap               float64 `yaml:"suspect_max_gap"                  json:"suspect_max_gap"`
	HighSuspectProtocolFailures int     `yaml:"high_suspect_protocol_failures"   json:"high_suspect_protocol_failures"`
}

// BenchmarkAliasConfig maps provider/model aliases to a canonical model ID.
type BenchmarkAliasConfig struct {
	CanonicalModelID string   `yaml:"canonical_model_id" json:"canonical_model_id"`
	Provider         string   `yaml:"provider"           json:"provider,omitempty"`
	Models           []string `yaml:"models"             json:"models"`
}

// CompatConfig controls protocol compatibility (bridge, fallback).
type CompatConfig struct {
	Bridge   BridgeConfig   `yaml:"bridge"   json:"bridge"`
	Fallback FallbackConfig `yaml:"fallback" json:"fallback"`
}

// BridgeConfig controls model name rewriting.
type BridgeConfig struct {
	Enabled           bool         `yaml:"enabled"             json:"enabled"`
	Rules             []BridgeRule `yaml:"rules"               json:"rules"`
	ExcludeUserAgents []string     `yaml:"exclude_user_agents" json:"exclude_user_agents"`
}

// BridgeRule defines a model name rewrite rule.
type BridgeRule struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to"   json:"to"`
}

// FallbackConfig controls automatic fallback to alternative models.
type FallbackConfig struct {
	Enabled          bool              `yaml:"enabled"            json:"enabled"`
	DetectRepetition bool              `yaml:"detect_repetition"  json:"detect_repetition"`
	Models           map[string]string `yaml:"models"             json:"models"`
}

// ---------------------------------------------------------------------------
// Defaults & Normalisation
// ---------------------------------------------------------------------------

// Normalize applies default values where zero-values are present.
func (c *Config) Normalize() {
	c.Server.normalize()
	c.Admin.normalize()
	c.Routing.normalize()
	c.Telemetry.normalize()
	c.Pricing.normalize()
	c.Benchmarking.normalize()
	c.Compat.normalize()
	for i := range c.Providers {
		c.Providers[i].normalize()
	}
}

func (s *ServerConfig) normalize() {
	if s.Listen == "" {
		s.Listen = ":18080"
	}
	if s.ReadTimeoutMs == 0 {
		s.ReadTimeoutMs = 30000
	}
	if s.IdleTimeoutMs == 0 {
		s.IdleTimeoutMs = 120000
	}
	if s.MaxBodyBytes == 0 {
		s.MaxBodyBytes = 100 << 20 // 100 MB
	}
}

func (a *AdminConfig) normalize() {
	a.Language = normalizeLanguage(a.Language)
	if a.PublishHistoryLimit <= 0 {
		a.PublishHistoryLimit = DefaultAdminPublishHistoryLimit
	}
}

func (r *RoutingConfig) normalize() {
	r.Strategy = normalizeStrategy(r.Strategy)
	if r.MaxRetries == 0 {
		r.MaxRetries = 2
	}
	if r.RetryBackoff.InitialMs == 0 {
		r.RetryBackoff.InitialMs = 3000
	}
	if r.RetryBackoff.MaxMs == 0 {
		r.RetryBackoff.MaxMs = 30000
	}
	if r.Health.IntervalSec == 0 {
		r.Health.IntervalSec = 10
	}
	if r.Health.TimeoutMs == 0 {
		r.Health.TimeoutMs = 2000
	}
	if r.Health.Path == "" {
		r.Health.Path = "/v1/models"
	}
	if r.StickySessions.TTLSec <= 0 {
		r.StickySessions.TTLSec = 1800
	}
	if r.FailurePolicy.Threshold == 0 {
		r.FailurePolicy.Threshold = 20
	}
	if r.FailurePolicy.CooldownSec == 0 {
		r.FailurePolicy.CooldownSec = 60
	}
	if r.FailurePolicy.PassthroughAfterSec == 0 {
		r.FailurePolicy.PassthroughAfterSec = 600
	}
	if r.Retry.StatusCodeMin == nil {
		min := 500
		r.Retry.StatusCodeMin = &min
	}
	if len(r.Retry.StatusCodes) == 0 {
		r.Retry.StatusCodes = []int{408, 429}
	}
	if len(r.Retry.MessageKeywords) == 0 {
		r.Retry.MessageKeywords = DefaultRetryKeywords()
	}
	if r.Retry.MaxElapsedMs <= 0 {
		r.Retry.MaxElapsedMs = 120000
	}
	if r.Cache.TTLSec == 0 {
		r.Cache.TTLSec = 300
	}
	if r.Cache.MaxEntries == 0 {
		r.Cache.MaxEntries = 1000
	}
	if r.Queue.MaxConcurrent == 0 {
		r.Queue.MaxConcurrent = 100
	}
	if r.Queue.HighPriorityPct == 0 {
		r.Queue.HighPriorityPct = 60
	}
	if r.Compression.Level == 0 {
		r.Compression.Level = 5
	}
	if r.Compression.MinSizeBytes == 0 {
		r.Compression.MinSizeBytes = 1024
	}
	for i := range r.Intercepts {
		if strings.TrimSpace(r.Intercepts[i].Action) == "" {
			r.Intercepts[i].Action = "fail"
		}
	}
}

func (t *TelemetryConfig) normalize() {
	if t.SQLitePath == "" {
		t.SQLitePath = "data/telemetry.db"
	}
	if t.RetentionDays == 0 {
		t.RetentionDays = 30
	}
	if t.AggregationSec == 0 {
		t.AggregationSec = 60
	}
	if t.CacheTTLSec == 0 {
		t.CacheTTLSec = 5
	}
}

func (p *PricingConfig) normalize() {
	if p.CachePath == "" {
		p.CachePath = "data/pricing-cache.json"
	}
	if p.RefreshIntervalMinutes <= 0 {
		switch {
		case p.RefreshIntervalHours > 0:
			p.RefreshIntervalMinutes = p.RefreshIntervalHours * 60
		default:
			p.RefreshIntervalMinutes = 15
		}
	}
	p.RefreshIntervalHours = (p.RefreshIntervalMinutes + 59) / 60
	if p.RequestTimeoutMs <= 0 {
		p.RequestTimeoutMs = 15000
	}
	if p.FX.CachePath == "" {
		p.FX.CachePath = "data/pricing-fx-cache.json"
	}
	if p.FX.RefreshIntervalMinutes <= 0 {
		p.FX.RefreshIntervalMinutes = 24 * 60
	}
	if p.FX.Enabled == nil {
		enabled := true
		p.FX.Enabled = &enabled
	}
	if len(p.Sources) == 0 && !p.sourcesSet {
		p.Sources = defaultPricingSources(p.RefreshIntervalMinutes, p.RequestTimeoutMs)
	}
	for i := range p.Sources {
		p.Sources[i].ID = strings.TrimSpace(p.Sources[i].ID)
		p.Sources[i].Vendor = strings.ToLower(strings.TrimSpace(p.Sources[i].Vendor))
		p.Sources[i].URL = strings.TrimSpace(p.Sources[i].URL)
		if p.Sources[i].RefreshIntervalMinutes <= 0 {
			p.Sources[i].RefreshIntervalMinutes = p.RefreshIntervalMinutes
		}
		if p.Sources[i].TimeoutMs <= 0 {
			p.Sources[i].TimeoutMs = p.RequestTimeoutMs
		}
		if p.Sources[i].Enabled == nil {
			enabled := true
			p.Sources[i].Enabled = &enabled
		}
	}
	for i := range p.ManualPrices {
		p.ManualPrices[i].Provider = strings.TrimSpace(p.ManualPrices[i].Provider)
		p.ManualPrices[i].Model = strings.TrimSpace(p.ManualPrices[i].Model)
		p.ManualPrices[i].Currency = normalizePricingCurrency(p.ManualPrices[i].Currency)
		p.ManualPrices[i].Source = strings.TrimSpace(p.ManualPrices[i].Source)
		if p.ManualPrices[i].Source == "" {
			p.ManualPrices[i].Source = "manual"
		}
		if p.ManualPrices[i].Enabled == nil {
			enabled := true
			p.ManualPrices[i].Enabled = &enabled
		}
	}
}

func (c *CompatConfig) normalize() {
	if c.Fallback.Models == nil {
		c.Fallback.Models = make(map[string]string)
	}
}

func (b *BenchmarkingConfig) normalize() {
	if b.DefaultSuite == "" {
		b.DefaultSuite = BenchmarkSuiteGeneralProtocolV1
	}
	b.Judge.PublicModel = strings.TrimSpace(b.Judge.PublicModel)
	b.Judge.Provider = strings.TrimSpace(b.Judge.Provider)
	if b.Judge.TimeoutMs <= 0 {
		b.Judge.TimeoutMs = 30000
	}
	if b.Limits.MaxParallelRuns <= 0 {
		b.Limits.MaxParallelRuns = 1
	}
	if b.Limits.MaxParallelCases <= 0 {
		b.Limits.MaxParallelCases = 2
	}
	if b.Limits.PerCaseTimeoutMs <= 0 {
		b.Limits.PerCaseTimeoutMs = 30000
	}
	if b.VerdictThresholds.NormalMaxGap <= 0 {
		b.VerdictThresholds.NormalMaxGap = 8
	}
	if b.VerdictThresholds.SuspectMaxGap <= 0 {
		b.VerdictThresholds.SuspectMaxGap = 20
	}
	if b.VerdictThresholds.HighSuspectProtocolFailures <= 0 {
		b.VerdictThresholds.HighSuspectProtocolFailures = 2
	}
	for i := range b.Aliases {
		b.Aliases[i].CanonicalModelID = strings.TrimSpace(b.Aliases[i].CanonicalModelID)
		b.Aliases[i].Provider = strings.TrimSpace(b.Aliases[i].Provider)
		normalized := make([]string, 0, len(b.Aliases[i].Models))
		for _, model := range b.Aliases[i].Models {
			if value := strings.TrimSpace(model); value != "" {
				normalized = append(normalized, value)
			}
		}
		b.Aliases[i].Models = normalized
	}
}

func (p *Provider) normalize() {
	p.ProviderClass = NormalizeProviderClass(string(p.ProviderClass))
	p.ProtocolAdapter = NormalizeProtocolAdapter(p.ProtocolAdapter, p.AnthropicBaseURL)
	if p.Weight == 0 {
		p.Weight = 1
	}
	if p.TimeoutMs == 0 {
		p.TimeoutMs = 30000
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Validate checks the configuration for security and consistency issues.
func (c *Config) Validate() error {
	if c.Admin.Enabled {
		if c.Admin.BootstrapToken == "" {
			return errors.New("admin.bootstrap_token must be set when admin is enabled")
		}
		if len(c.Admin.BootstrapToken) < 32 {
			return errors.New("admin.bootstrap_token must be at least 32 characters")
		}
		if c.Admin.CookieSigningKey == "" {
			return errors.New("admin.cookie_signing_key must be set when admin is enabled")
		}
		if len(c.Admin.CookieSigningKey) < 32 {
			return errors.New("admin.cookie_signing_key must be at least 32 characters")
		}
	}
	if c.Admin.PublishHistoryLimit < 0 {
		return errors.New("admin.publish_history_limit must be >= 0")
	}
	if c.Pricing.RefreshIntervalHours < 0 {
		return errors.New("pricing.refresh_interval_hours must be >= 0")
	}
	if c.Pricing.RefreshIntervalMinutes < 0 {
		return errors.New("pricing.refresh_interval_minutes must be >= 0")
	}
	if c.Pricing.RequestTimeoutMs < 0 {
		return errors.New("pricing.request_timeout_ms must be >= 0")
	}
	if c.Pricing.FX.RefreshIntervalMinutes < 0 {
		return errors.New("pricing.fx.refresh_interval_minutes must be >= 0")
	}
	if c.Benchmarking.Limits.MaxParallelRuns < 0 {
		return errors.New("benchmarking.limits.max_parallel_runs must be >= 0")
	}
	if c.Benchmarking.Limits.MaxParallelCases < 0 {
		return errors.New("benchmarking.limits.max_parallel_cases must be >= 0")
	}
	if c.Benchmarking.Limits.PerCaseTimeoutMs < 0 {
		return errors.New("benchmarking.limits.per_case_timeout_ms must be >= 0")
	}
	for i, source := range c.Pricing.Sources {
		if strings.TrimSpace(source.ID) == "" {
			return fmt.Errorf("pricing.sources[%d].id must not be empty", i)
		}
		if strings.TrimSpace(source.Vendor) == "" {
			return fmt.Errorf("pricing.sources[%d].vendor must not be empty", i)
		}
		if source.TimeoutMs < 0 {
			return fmt.Errorf("pricing.sources[%d].timeout_ms must be >= 0", i)
		}
		if source.RefreshIntervalMinutes < 0 {
			return fmt.Errorf("pricing.sources[%d].refresh_interval_minutes must be >= 0", i)
		}
	}
	for i, manual := range c.Pricing.ManualPrices {
		if strings.TrimSpace(manual.Model) == "" {
			return fmt.Errorf("pricing.manual_prices[%d].model must not be empty", i)
		}
		if manual.InputPer1M < 0 {
			return fmt.Errorf("pricing.manual_prices[%d].input_per_1m must be >= 0", i)
		}
		if manual.CachedInputPer1M < 0 {
			return fmt.Errorf("pricing.manual_prices[%d].cached_input_per_1m must be >= 0", i)
		}
		if manual.OutputPer1M < 0 {
			return fmt.Errorf("pricing.manual_prices[%d].output_per_1m must be >= 0", i)
		}
	}
	if c.Benchmarking.Enabled && strings.TrimSpace(c.Benchmarking.Judge.PublicModel) == "" {
		return errors.New("benchmarking.judge.public_model must be set when benchmarking is enabled")
	}
	if c.Benchmarking.Enabled {
		enabledProviders := 0
		judgeProviderName := strings.TrimSpace(c.Benchmarking.Judge.Provider)
		judgeModel := strings.TrimSpace(c.Benchmarking.Judge.PublicModel)
		judgeModelServed := false
		for _, provider := range c.Providers {
			if !provider.IsEnabled() {
				continue
			}
			enabledProviders++
			for _, model := range provider.Models {
				if !strings.EqualFold(strings.TrimSpace(model), judgeModel) {
					continue
				}
				if judgeProviderName == "" || strings.EqualFold(strings.TrimSpace(provider.Name), judgeProviderName) {
					judgeModelServed = true
				}
			}
		}
		if enabledProviders == 0 {
			return errors.New("benchmarking requires at least one enabled provider")
		}
		switch {
		case judgeProviderName != "" && !judgeModelServed:
			return fmt.Errorf("benchmarking.judge.provider %q must be enabled and advertise model %q", judgeProviderName, judgeModel)
		case judgeProviderName == "" && !judgeModelServed:
			return fmt.Errorf("benchmarking.judge.public_model %q must be served by at least one enabled provider", judgeModel)
		}
	}
	if c.Benchmarking.DefaultSuite != "" && c.Benchmarking.DefaultSuite != BenchmarkSuiteGeneralProtocolV1 {
		return fmt.Errorf("benchmarking.default_suite must be %q", BenchmarkSuiteGeneralProtocolV1)
	}
	for i, alias := range c.Benchmarking.Aliases {
		if strings.TrimSpace(alias.CanonicalModelID) == "" {
			return fmt.Errorf("benchmarking.aliases[%d].canonical_model_id must not be empty", i)
		}
		if len(alias.Models) == 0 {
			return fmt.Errorf("benchmarking.aliases[%d].models must not be empty", i)
		}
	}
	if len(c.Providers) == 0 {
		return errors.New("at least one provider must be configured")
	}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("providers[%d].name must not be empty", i)
		}
		if p.BaseURL == "" {
			return fmt.Errorf("providers[%d].base_url must not be empty", i)
		}
		switch NormalizeProtocolAdapter(p.ProtocolAdapter, p.AnthropicBaseURL) {
		case ProtocolAdapterOpenAIChatCompletions, ProtocolAdapterAnthropicMessages:
		default:
			return fmt.Errorf("providers[%d].protocol_adapter must be one of %q or %q", i, ProtocolAdapterOpenAIChatCompletions, ProtocolAdapterAnthropicMessages)
		}
		if p.RateLimit.RequestsPerSecond < 0 {
			return fmt.Errorf("providers[%d].rate_limit.requests_per_second must be >= 0", i)
		}
		if p.RateLimit.Burst < 0 {
			return fmt.Errorf("providers[%d].rate_limit.burst must be >= 0", i)
		}
		if p.RateLimit.Enabled && p.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("providers[%d].rate_limit.requests_per_second must be > 0 when provider rate limiting is enabled", i)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	StrategyHealthWeightedRR             = "health_weighted_rr"
	StrategyRoundRobin                   = "round_robin"
	StrategyWeightedRR                   = "weighted_rr"
	ProtocolAdapterOpenAIChatCompletions = "openai_chat_completions"
	ProtocolAdapterAnthropicMessages     = "anthropic_messages"
	// BenchmarkProtocolOpenAIResponses is the protocol string for RunBenchmarkCase to exercise POST /v1/responses.
	BenchmarkProtocolOpenAIResponses = "openai_responses"
	BenchmarkSuiteGeneralProtocolV1  = "general_protocol_v1"

	DefaultAdminPublishHistoryLimit = 256

	LangZH = "zh"
	LangEN = "en"
	LangJA = "ja"
	LangKO = "ko"
	LangES = "es"
	LangFR = "fr"
	LangDE = "de"
)

func normalizeStrategy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", StrategyHealthWeightedRR:
		return StrategyHealthWeightedRR
	case StrategyRoundRobin:
		return StrategyRoundRobin
	case StrategyWeightedRR:
		return StrategyWeightedRR
	default:
		return strings.TrimSpace(s)
	}
}

func normalizeLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case LangEN:
		return LangEN
	case LangJA:
		return LangJA
	case LangKO:
		return LangKO
	case LangES:
		return LangES
	case LangFR:
		return LangFR
	case LangDE:
		return LangDE
	default:
		return LangZH
	}
}

func normalizePricingCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return "USD"
	}
	return currency
}

func defaultPricingSources(refreshIntervalMinutes int, timeoutMs int) []PricingSourceConfig {
	sources := []PricingSourceConfig{
		{ID: "openai", Vendor: "openai", URL: "https://openai.com/api/pricing/"},
		{ID: "anthropic", Vendor: "anthropic", URL: "https://docs.anthropic.com/en/docs/about-claude/pricing"},
		{ID: "gemini", Vendor: "gemini", URL: "https://ai.google.dev/gemini-api/docs/pricing"},
		{ID: "moonshot", Vendor: "moonshot", URL: "https://platform.moonshot.cn/docs/pricing/chat"},
		{ID: "zhipu", Vendor: "zhipu", URL: "https://open.bigmodel.cn/pricing"},
		{ID: "minimax", Vendor: "minimax", URL: "https://platform.minimaxi.com/docs/pricing"},
		{ID: "deepseek", Vendor: "deepseek", URL: "https://api-docs.deepseek.com/quick_start/pricing"},
		{ID: "xai", Vendor: "xai", URL: "https://x.ai/api"},
		{ID: "step", Vendor: "step", URL: "https://platform.stepfun.com/docs/pricing"},
		{ID: "xiaomi", Vendor: "xiaomi", URL: "https://platform.xiaomi.com/"},
	}
	for i := range sources {
		sources[i].TimeoutMs = timeoutMs
		sources[i].RefreshIntervalMinutes = refreshIntervalMinutes
		enabled := true
		if sources[i].Vendor == "xiaomi" {
			enabled = false
		}
		sources[i].Enabled = &enabled
	}
	return sources
}

// NormalizeProviderClass normalises a provider class string.
func NormalizeProviderClass(value string) ProviderClass {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "free", "gratis", "public":
		return ProviderClassFree
	default:
		return ProviderClassQuotaLimited
	}
}

// NormalizeProtocolAdapter normalizes a provider adapter while preserving
// legacy AnthropicBaseURL behavior for existing configs.
func NormalizeProtocolAdapter(value string, anthropicBaseURL string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", ProtocolAdapterOpenAIChatCompletions:
		if strings.TrimSpace(anthropicBaseURL) != "" && value == "" {
			return ProtocolAdapterAnthropicMessages
		}
		return ProtocolAdapterOpenAIChatCompletions
	case ProtocolAdapterAnthropicMessages:
		return ProtocolAdapterAnthropicMessages
	default:
		return value
	}
}

// DefaultRetryKeywords returns the default set of retryable error keywords.
func DefaultRetryKeywords() []string {
	return []string{
		"429",
		"too many requests",
		"rate limit",
		"quota exceeded",
		"insufficient quota",
		"insufficient_quota",
		"exceeded your current quota",
		"billing hard limit",
		"credit balance is too low",
		"stream disconnected before completion",
		"stream closed before response.completed",
		"response.completed",
		"upstream request failed",
		"server busy",
		"system busy",
		"temporarily unavailable",
		"temporarily overloaded",
		"overloaded",
		"please try again later",
		"service unavailable",
	}
}
