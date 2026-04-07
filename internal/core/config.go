package core

import (
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Configuration — maps to config.yaml
// Top-level: server, admin, routing, providers, telemetry, pricing, compat
// ---------------------------------------------------------------------------

// Config holds the complete gateway configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"    json:"server"`
	Admin     AdminConfig     `yaml:"admin"     json:"admin"`
	Routing   RoutingConfig   `yaml:"routing"   json:"routing"`
	Providers []Provider      `yaml:"providers" json:"providers"`
	Telemetry TelemetryConfig `yaml:"telemetry" json:"telemetry"`
	Pricing   PricingConfig   `yaml:"pricing"   json:"pricing"`
	Compat    CompatConfig    `yaml:"compat"    json:"compat"`
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
	Enabled          bool   `yaml:"enabled"            json:"enabled"`
	BootstrapToken   string `yaml:"bootstrap_token"    json:"-"`
	CookieSigningKey string `yaml:"cookie_signing_key" json:"-"`
	Language         string `yaml:"language"            json:"language"`
	RateLimit        struct {
		RequestsPerSecond float64 `yaml:"requests_per_second" json:"requests_per_second"`
		Burst             int     `yaml:"burst"               json:"burst"`
	} `yaml:"rate_limit" json:"rate_limit"`
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
	Threshold                int `yaml:"threshold"                   json:"threshold"`
	CooldownSec              int `yaml:"cooldown_sec"                json:"cooldown_sec"`
	PassthroughAfterSec      int `yaml:"passthrough_after_sec"       json:"passthrough_after_sec"`
	QuotaRecoveryIntervalMin int `yaml:"quota_recovery_interval_min" json:"quota_recovery_interval_min"`
}

// RetryPolicyConfig defines when requests should be retried.
type RetryPolicyConfig struct {
	InfiniteOnError bool     `yaml:"infinite_on_error" json:"infinite_on_error"`
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
	CachePath            string `yaml:"cache_path"             json:"cache_path"`
	RefreshIntervalHours int    `yaml:"refresh_interval_hours" json:"refresh_interval_hours"`
	RequestTimeoutMs     int    `yaml:"request_timeout_ms"     json:"request_timeout_ms"`
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
	if s.WriteTimeoutMs == 0 {
		s.WriteTimeoutMs = 60000
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
	if p.RefreshIntervalHours <= 0 {
		p.RefreshIntervalHours = 12
	}
	if p.RequestTimeoutMs <= 0 {
		p.RequestTimeoutMs = 15000
	}
}

func (c *CompatConfig) normalize() {
	if c.Fallback.Models == nil {
		c.Fallback.Models = make(map[string]string)
	}
}

func (p *Provider) normalize() {
	p.ProviderClass = NormalizeProviderClass(string(p.ProviderClass))
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
	if c.Pricing.RefreshIntervalHours < 0 {
		return errors.New("pricing.refresh_interval_hours must be >= 0")
	}
	if c.Pricing.RequestTimeoutMs < 0 {
		return errors.New("pricing.request_timeout_ms must be >= 0")
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
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	StrategyHealthWeightedRR = "health_weighted_rr"
	StrategyRoundRobin       = "round_robin"
	StrategyWeightedRR       = "weighted_rr"

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

// NormalizeProviderClass normalises a provider class string.
func NormalizeProviderClass(value string) ProviderClass {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "free", "gratis", "public":
		return ProviderClassFree
	default:
		return ProviderClassQuotaLimited
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
