package config

import (
	"errors"
	"path"
	"strings"
)

// Config holds the complete gateway configuration.
type Config struct {
	Listen    string              `yaml:"listen"`
	Reload    ReloadConfig        `yaml:"reload"`
	Router    RouterConfig        `yaml:"router"`
	Health    HealthConfig        `yaml:"health"`
	Admin     AdminConfig         `yaml:"admin"`
	Telemetry TelemetryConfig     `yaml:"telemetry"`
	Pricing   PricingConfig       `yaml:"pricing"`
	Bridge    ModelBridgeConfig   `yaml:"bridge"`
	Fallback  ModelFallbackConfig `yaml:"fallback"`
	Proxy     ProxyPolicyConfig   `yaml:"proxy"`
	Upstreams []Upstream          `yaml:"upstreams"`
}

// ReloadConfig controls configuration file reloading behavior.
type ReloadConfig struct {
	Enabled    bool `yaml:"enabled"`
	DebounceMs int  `yaml:"debounce_ms"`
}

// RouterConfig controls request routing and retry behavior.
type RouterConfig struct {
	Strategy                          string              `yaml:"strategy"`
	MaxRetries                        int                 `yaml:"max_retries"`
	RetryBackoffMs                    int                 `yaml:"retry_backoff_ms"`
	RetryBackoffMaxMs                 int                 `yaml:"retry_backoff_max_ms"`
	FailureThreshold                  int                 `yaml:"failure_threshold"`
	CooldownSec                       int                 `yaml:"cooldown_sec"`
	FailurePassthroughAfterSec        int                 `yaml:"failure_passthrough_after_sec"`
	QuotaBlockRecoveryIntervalMinutes int                 `yaml:"quota_block_recovery_interval_minutes"`
	StickySessions                    StickySessionConfig `yaml:"sticky_sessions"`
}

// StickySessionConfig controls sticky session behavior.
type StickySessionConfig struct {
	Enabled bool `yaml:"enabled"`
	TTLSec  int  `yaml:"ttl_sec"`
}

// HealthConfig controls health checking behavior.
type HealthConfig struct {
	Enabled     bool   `yaml:"enabled"`
	IntervalSec int    `yaml:"interval_sec"`
	TimeoutMs   int    `yaml:"timeout_ms"`
	Path        string `yaml:"path"`
}

// AdminConfig controls the admin API and UI.
type AdminConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AuthToken string `yaml:"auth_token"`
	Language  string `yaml:"language"`
}

// TelemetryConfig controls telemetry storage.
type TelemetryConfig struct {
	SQLitePath string `yaml:"sqlite_path"`
}

// PricingConfig controls pricing data fetching.
type PricingConfig struct {
	CachePath            string `yaml:"cache_path"`
	RefreshIntervalHours int    `yaml:"refresh_interval_hours"`
	RequestTimeoutMs     int    `yaml:"request_timeout_ms"`
}

// ModelBridgeConfig controls model name rewriting for compatibility.
type ModelBridgeConfig struct {
	Enabled           bool              `yaml:"enabled"`
	Rules             []ModelBridgeRule `yaml:"rules"`
	ExcludeUserAgents []string          `yaml:"exclude_user_agents"`
}

// ModelBridgeRule defines a model name rewrite rule.
type ModelBridgeRule struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// ModelFallbackConfig controls automatic fallback to alternative models.
type ModelFallbackConfig struct {
	Enabled          bool              `yaml:"enabled"`
	DetectRepetition bool              `yaml:"detect_repetition"`
	Models           map[string]string `yaml:"models"`
}

// ModelFallbackRule defines a fallback model mapping.
type ModelFallbackRule struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// ProxyPolicyConfig controls proxy retry and response interception.
type ProxyPolicyConfig struct {
	Retry      RetryPolicyConfig       `yaml:"retry"`
	Intercepts []ResponseInterceptRule `yaml:"intercepts"`
}

// RetryPolicyConfig defines when requests should be retried.
type RetryPolicyConfig struct {
	InfiniteOnError bool     `yaml:"infinite_on_error"`
	StatusCodes     []int    `yaml:"status_codes"`
	StatusCodeMin   *int     `yaml:"status_code_min"`
	MessageKeywords []string `yaml:"message_keywords"`
}

// ResponseInterceptRule defines a rule for intercepting and handling specific responses.
type ResponseInterceptRule struct {
	Name            string   `yaml:"name"`
	Enabled         *bool    `yaml:"enabled"`
	Paths           []string `yaml:"paths"`
	StatusCodes     []int    `yaml:"status_codes"`
	StatusCodeMin   *int     `yaml:"status_code_min"`
	MessageKeywords []string `yaml:"message_keywords"`
	Action          string   `yaml:"action"`
}

// Upstream defines an upstream AI model provider.
type Upstream struct {
	Name                string            `yaml:"name"`
	BaseURL             string            `yaml:"base_url"`
	AnthropicBaseURL    string            `yaml:"anthropic_base_url"`
	APIKey              string            `yaml:"api_key"`
	ProviderClass       string            `yaml:"provider_class"`
	Models              []string          `yaml:"models"`
	Weight              int               `yaml:"weight"`
	TimeoutMs           int               `yaml:"timeout_ms"`
	SameUpstreamRetries int               `yaml:"same_upstream_retries"`
	Enabled             *bool             `yaml:"enabled"`
	Headers             map[string]string `yaml:"headers"`
}

const (
	RouterStrategyHealthWeightedRR = "health_weighted_rr"
	RouterStrategyRoundRobin       = "round_robin"
	UpstreamClassFree              = "free"
	UpstreamClassQuotaLimited      = "quota_limited"
	AdminLanguageChinese           = "zh"
	AdminLanguageEnglish           = "en"
	AdminLanguageJapanese          = "ja"
	AdminLanguageKorean            = "ko"
	AdminLanguageSpanish           = "es"
	AdminLanguageFrench            = "fr"
	AdminLanguageGerman            = "de"
	adminLanguageValidationMessage = "admin.language must be one of zh, en, ja, ko, es, fr, de (supported: Chinese/zh, English/en, Japanese/ja, Korean/ko, Spanish/es, French/fr, German/de)"
)

// Normalize applies default values to configuration fields.
func (c *Config) Normalize() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Reload.DebounceMs == 0 {
		c.Reload.DebounceMs = 200
	}
	c.Router.Strategy = NormalizeRouterStrategy(c.Router.Strategy)
	if c.Router.MaxRetries == 0 {
		c.Router.MaxRetries = 2
	}
	if c.Router.RetryBackoffMs == 0 {
		c.Router.RetryBackoffMs = 3000
	}
	if c.Router.RetryBackoffMaxMs == 0 {
		c.Router.RetryBackoffMaxMs = 30000
	}
	if c.Router.FailureThreshold == 0 {
		c.Router.FailureThreshold = 20
	}
	if c.Router.CooldownSec == 0 {
		c.Router.CooldownSec = 60
	}
	if c.Router.FailurePassthroughAfterSec == 0 {
		c.Router.FailurePassthroughAfterSec = 600
	}
	if c.Router.StickySessions.TTLSec <= 0 {
		c.Router.StickySessions.TTLSec = 1800
	}
	if c.Health.IntervalSec == 0 {
		c.Health.IntervalSec = 10
	}
	if c.Health.TimeoutMs == 0 {
		c.Health.TimeoutMs = 2000
	}
	if c.Health.Path == "" {
		c.Health.Path = "/v1/models"
	}
	c.Admin.Language = NormalizeAdminLanguage(c.Admin.Language)
	if c.Telemetry.SQLitePath == "" {
		c.Telemetry.SQLitePath = "data/telemetry.db"
	}
	if c.Pricing.CachePath == "" {
		c.Pricing.CachePath = "data/pricing-cache.json"
	}
	if c.Pricing.RefreshIntervalHours <= 0 {
		c.Pricing.RefreshIntervalHours = 12
	}
	if c.Pricing.RequestTimeoutMs <= 0 {
		c.Pricing.RequestTimeoutMs = 15000
	}
	applyDefaultProxyPolicy(&c.Proxy)
	c.Fallback.normalize()
	for i := range c.Upstreams {
		c.Upstreams[i].ProviderClass = NormalizeUpstreamClass(c.Upstreams[i].ProviderClass)
		if c.Upstreams[i].Weight == 0 {
			c.Upstreams[i].Weight = 1
		}
		if c.Upstreams[i].TimeoutMs == 0 {
			c.Upstreams[i].TimeoutMs = 30000
		}
	}
}

// normalize applies defaults to fallback configuration.
func (f *ModelFallbackConfig) normalize() {
	if f.Models == nil {
		f.Models = make(map[string]string)
	}
}

// IsEnabled returns whether the upstream is enabled (defaults to true).
func (u Upstream) IsEnabled() bool {
	if u.Enabled == nil {
		return true
	}
	return *u.Enabled
}

// ProviderClassNormalized returns the normalized provider class.
func (u Upstream) ProviderClassNormalized() string {
	return NormalizeUpstreamClass(u.ProviderClass)
}

// NormalizeAdminLanguage normalizes and validates an admin language code.
func NormalizeAdminLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case AdminLanguageEnglish:
		return AdminLanguageEnglish
	case AdminLanguageJapanese:
		return AdminLanguageJapanese
	case AdminLanguageKorean:
		return AdminLanguageKorean
	case AdminLanguageSpanish:
		return AdminLanguageSpanish
	case AdminLanguageFrench:
		return AdminLanguageFrench
	case AdminLanguageGerman:
		return AdminLanguageGerman
	default:
		return AdminLanguageChinese
	}
}

// ValidateAdminLanguage validates an admin language code.
func ValidateAdminLanguage(language string) error {
	if strings.TrimSpace(language) == "" {
		return nil
	}
	if NormalizeAdminLanguage(language) != strings.ToLower(strings.TrimSpace(language)) {
		return errors.New(adminLanguageValidationMessage)
	}
	return nil
}

// AdminLanguageValidationMessage returns the validation error message for languages.
func AdminLanguageValidationMessage() string {
	return adminLanguageValidationMessage
}

// NormalizeRouterStrategy normalizes a router strategy name.
func NormalizeRouterStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "", RouterStrategyHealthWeightedRR:
		return RouterStrategyHealthWeightedRR
	case RouterStrategyRoundRobin:
		return RouterStrategyRoundRobin
	default:
		return strings.TrimSpace(strategy)
	}
}

// RewriteModel rewrites a model name using bridge rules.
func (c Config) RewriteModel(model string) string {
	return c.RewriteModelForRequest(model, "")
}

// GetFallbackModel returns the fallback model for a given model, if configured.
func (c Config) GetFallbackModel(model string) string {
	if !c.Fallback.Enabled {
		return ""
	}
	if fallback, ok := c.Fallback.Models[model]; ok && fallback != "" {
		return fallback
	}
	return ""
}

// RewriteModelForRequest rewrites a model name based on bridge rules and user agent.
func (c Config) RewriteModelForRequest(model string, userAgent string) string {
	model = strings.TrimSpace(model)
	if model == "" || !c.Bridge.Enabled || c.Bridge.shouldSkipUserAgent(userAgent) {
		return model
	}

	for _, rule := range c.Bridge.Rules {
		if rule.matches(model) {
			return strings.TrimSpace(rule.To)
		}
	}
	return model
}

// matches checks if the rule matches the given model.
func (r ModelBridgeRule) matches(model string) bool {
	return matchesPattern(r.From, model)
}

// shouldSkipUserAgent checks if the user agent should be excluded from bridging.
func (b ModelBridgeConfig) shouldSkipUserAgent(userAgent string) bool {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" {
		return false
	}
	for _, pattern := range b.ExcludeUserAgents {
		if matchesPattern(pattern, userAgent) {
			return true
		}
	}
	return false
}

// IsEnabled returns whether the intercept rule is enabled (defaults to true).
func (r ResponseInterceptRule) IsEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// matchesPattern checks if a value matches a glob pattern.
func matchesPattern(pattern string, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	sanitizedPattern := sanitizeGlobValue(pattern)
	sanitizedValue := sanitizeGlobValue(value)
	if ok, err := path.Match(sanitizedPattern, sanitizedValue); err == nil && ok {
		return true
	}
	return strings.EqualFold(pattern, value)
}

// sanitizeGlobValue sanitizes a value for glob matching by replacing path separators.
func sanitizeGlobValue(value string) string {
	value = strings.ReplaceAll(value, "/", "\x00")
	value = strings.ReplaceAll(value, "\\", "\x01")
	return value
}

// MatchesPattern checks if a value matches a glob pattern (exported version).
func MatchesPattern(pattern string, value string) bool {
	return matchesPattern(pattern, value)
}

// NormalizeUpstreamClass normalizes a provider class string.
func NormalizeUpstreamClass(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "quota", "quota_limited", "quota-limited", "limited", "metered", "paid":
		return UpstreamClassQuotaLimited
	case "free", "gratis", "public":
		return UpstreamClassFree
	default:
		return UpstreamClassQuotaLimited
	}
}

// applyDefaultProxyPolicy applies default values to proxy policy configuration.
func applyDefaultProxyPolicy(policy *ProxyPolicyConfig) {
	if policy == nil {
		return
	}
	if policy.Retry.StatusCodeMin == nil {
		defaultMin := 500
		policy.Retry.StatusCodeMin = &defaultMin
	}
	if len(policy.Retry.StatusCodes) == 0 {
		policy.Retry.StatusCodes = []int{408, 429}
	}
	if len(policy.Retry.MessageKeywords) == 0 {
		policy.Retry.MessageKeywords = defaultRetryableKeywords()
	}
	for i := range policy.Intercepts {
		if strings.TrimSpace(policy.Intercepts[i].Action) == "" {
			policy.Intercepts[i].Action = "fail"
		}
	}
}

// defaultRetryableKeywords returns the default list of retryable error keywords.
func defaultRetryableKeywords() []string {
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

// ValidateConfig validates the configuration for security issues
func ValidateConfig(cfg *Config) error {
	if cfg.Admin.Enabled {
		if cfg.Admin.AuthToken == "" || cfg.Admin.AuthToken == "change-me-admin-token" {
			return fmt.Errorf("admin auth_token must be set to a secure value (not default)")
		}
		if len(cfg.Admin.AuthToken) < 32 {
			return fmt.Errorf("admin auth_token must be at least 32 characters")
		}
	}
	return nil
}
