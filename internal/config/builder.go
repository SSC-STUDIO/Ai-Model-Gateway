package config

import (
	"fmt"
)

// ConfigBuilder provides a fluent interface for building configuration objects.
// This is useful for programmatic configuration creation and testing.
type ConfigBuilder struct {
	config Config
	errors []error
}

// NewConfigBuilder creates a new ConfigBuilder with default values applied.
func NewConfigBuilder() *ConfigBuilder {
	cb := &ConfigBuilder{
		config: Config{},
	}
	cb.config.Normalize()
	return cb
}

// NewConfigBuilderWithDefaults creates a new ConfigBuilder with all defaults applied.
func NewConfigBuilderWithDefaults() *ConfigBuilder {
	return NewConfigBuilder()
}

// WithListen sets the listen address.
func (cb *ConfigBuilder) WithListen(address string) *ConfigBuilder {
	cb.config.Listen = address
	return cb
}

// WithAdminEnabled enables or disables the admin API.
func (cb *ConfigBuilder) WithAdminEnabled(enabled bool) *ConfigBuilder {
	cb.config.Admin.Enabled = enabled
	return cb
}

// WithAdminAuthToken sets the admin authentication token.
func (cb *ConfigBuilder) WithAdminAuthToken(token string) *ConfigBuilder {
	cb.config.Admin.AuthToken = token
	return cb
}

// WithAdminLanguage sets the admin UI language.
func (cb *ConfigBuilder) WithAdminLanguage(language string) *ConfigBuilder {
	cb.config.Admin.Language = NormalizeAdminLanguage(language)
	return cb
}

// WithRouterStrategy sets the routing strategy.
func (cb *ConfigBuilder) WithRouterStrategy(strategy string) *ConfigBuilder {
	cb.config.Router.Strategy = NormalizeRouterStrategy(strategy)
	return cb
}

// WithMaxRetries sets the maximum retry count.
func (cb *ConfigBuilder) WithMaxRetries(retries int) *ConfigBuilder {
	if retries < 0 {
		cb.errors = append(cb.errors, fmt.Errorf("max_retries must be >= 0"))
		return cb
	}
	cb.config.Router.MaxRetries = retries
	return cb
}

// WithRetryBackoff sets the retry backoff in milliseconds.
func (cb *ConfigBuilder) WithRetryBackoff(ms int) *ConfigBuilder {
	if ms < 0 {
		cb.errors = append(cb.errors, fmt.Errorf("retry_backoff_ms must be >= 0"))
		return cb
	}
	cb.config.Router.RetryBackoffMs = ms
	return cb
}

// WithHealthCheck enables or disables health checking.
func (cb *ConfigBuilder) WithHealthCheck(enabled bool) *ConfigBuilder {
	cb.config.Health.Enabled = enabled
	return cb
}

// WithHealthPath sets the health check endpoint path.
func (cb *ConfigBuilder) WithHealthPath(path string) *ConfigBuilder {
	cb.config.Health.Path = path
	return cb
}

// WithTelemetryPath sets the SQLite database path for telemetry.
func (cb *ConfigBuilder) WithTelemetryPath(path string) *ConfigBuilder {
	cb.config.Telemetry.SQLitePath = path
	return cb
}

// WithPricingCachePath sets the pricing cache file path.
func (cb *ConfigBuilder) WithPricingCachePath(path string) *ConfigBuilder {
	cb.config.Pricing.CachePath = path
	return cb
}

// WithBridgeEnabled enables or disables model name bridging.
func (cb *ConfigBuilder) WithBridgeEnabled(enabled bool) *ConfigBuilder {
	cb.config.Bridge.Enabled = enabled
	return cb
}

// WithBridgeRule adds a model bridge rule.
func (cb *ConfigBuilder) WithBridgeRule(from, to string) *ConfigBuilder {
	cb.config.Bridge.Rules = append(cb.config.Bridge.Rules, ModelBridgeRule{
		From: from,
		To:   to,
	})
	return cb
}

// WithFallbackEnabled enables or disables model fallback.
func (cb *ConfigBuilder) WithFallbackEnabled(enabled bool) *ConfigBuilder {
	cb.config.Fallback.Enabled = enabled
	return cb
}

// WithFallbackModel sets a fallback model mapping.
func (cb *ConfigBuilder) WithFallbackModel(from, to string) *ConfigBuilder {
	if cb.config.Fallback.Models == nil {
		cb.config.Fallback.Models = make(map[string]string)
	}
	cb.config.Fallback.Models[from] = to
	return cb
}

// WithUpstream adds an upstream configuration.
func (cb *ConfigBuilder) WithUpstream(upstream Upstream) *ConfigBuilder {
	cb.config.Upstreams = append(cb.config.Upstreams, upstream)
	return cb
}

// WithSimpleUpstream adds a simple upstream with minimal configuration.
func (cb *ConfigBuilder) WithSimpleUpstream(name, baseURL, apiKey string, models ...string) *ConfigBuilder {
	cb.config.Upstreams = append(cb.config.Upstreams, Upstream{
		Name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Models:  models,
		Weight:  1,
	})
	return cb
}

// WithStickySessions enables or disables sticky sessions.
func (cb *ConfigBuilder) WithStickySessions(enabled bool, ttlSec int) *ConfigBuilder {
	cb.config.Router.StickySessions.Enabled = enabled
	if ttlSec > 0 {
		cb.config.Router.StickySessions.TTLSec = ttlSec
	}
	return cb
}

// Build returns the constructed configuration and any accumulated errors.
// If errors exist, returns the first error and a zero Config.
func (cb *ConfigBuilder) Build() (Config, error) {
	if len(cb.errors) > 0 {
		return Config{}, cb.errors[0]
	}
	// Return a copy to maintain immutability
	return cb.Clone(), nil
}

// MustBuild returns the constructed configuration or panics if errors exist.
// Use with caution - primarily for tests and development.
func (cb *ConfigBuilder) MustBuild() Config {
	cfg, err := cb.Build()
	if err != nil {
		panic(err)
	}
	return cfg
}

// Clone creates a deep copy of the configuration.
// This ensures immutability when configs are passed around.
func (cb *ConfigBuilder) Clone() Config {
	return cloneConfig(cb.config)
}

// Config returns a reference to the current configuration state.
// Note: This returns the internal state for inspection; use Build() for a copy.
func (cb *ConfigBuilder) Config() Config {
	return cb.config
}

// Validate runs validation on the current configuration state.
func (cb *ConfigBuilder) Validate() error {
	return cb.config.Validate()
}

// cloneConfig creates a deep copy of a Config.
func cloneConfig(cfg Config) Config {
	cloned := Config{
		Listen:    cfg.Listen,
		Reload:    cfg.Reload,
		Router:    cfg.Router,
		Health:    cfg.Health,
		Admin:     cfg.Admin,
		Telemetry: cfg.Telemetry,
		Pricing:   cfg.Pricing,
		Bridge:    cfg.Bridge,
		Fallback:  cfg.Fallback,
		Proxy:     cfg.Proxy,
	}

	// Deep copy slices
	if cfg.Upstreams != nil {
		cloned.Upstreams = make([]Upstream, len(cfg.Upstreams))
		for i, u := range cfg.Upstreams {
			cloned.Upstreams[i] = cloneUpstream(u)
		}
	}

	// Deep copy bridge rules
	if cfg.Bridge.Rules != nil {
		cloned.Bridge.Rules = make([]ModelBridgeRule, len(cfg.Bridge.Rules))
		copy(cloned.Bridge.Rules, cfg.Bridge.Rules)
	}

	// Deep copy bridge exclude patterns
	if cfg.Bridge.ExcludeUserAgents != nil {
		cloned.Bridge.ExcludeUserAgents = make([]string, len(cfg.Bridge.ExcludeUserAgents))
		copy(cloned.Bridge.ExcludeUserAgents, cfg.Bridge.ExcludeUserAgents)
	}

	// Deep copy fallback models
	if cfg.Fallback.Models != nil {
		cloned.Fallback.Models = make(map[string]string, len(cfg.Fallback.Models))
		for k, v := range cfg.Fallback.Models {
			cloned.Fallback.Models[k] = v
		}
	}

	// Deep copy proxy intercepts
	if cfg.Proxy.Intercepts != nil {
		cloned.Proxy.Intercepts = make([]ResponseInterceptRule, len(cfg.Proxy.Intercepts))
		for i, r := range cfg.Proxy.Intercepts {
			cloned.Proxy.Intercepts[i] = cloneResponseInterceptRule(r)
		}
	}

	// Deep copy proxy retry status codes
	if cfg.Proxy.Retry.StatusCodes != nil {
		cloned.Proxy.Retry.StatusCodes = make([]int, len(cfg.Proxy.Retry.StatusCodes))
		copy(cloned.Proxy.Retry.StatusCodes, cfg.Proxy.Retry.StatusCodes)
	}

	// Deep copy proxy retry keywords
	if cfg.Proxy.Retry.MessageKeywords != nil {
		cloned.Proxy.Retry.MessageKeywords = make([]string, len(cfg.Proxy.Retry.MessageKeywords))
		copy(cloned.Proxy.Retry.MessageKeywords, cfg.Proxy.Retry.MessageKeywords)
	}

	return cloned
}

// cloneUpstream creates a deep copy of an Upstream.
func cloneUpstream(u Upstream) Upstream {
	cloned := Upstream{
		Name:                u.Name,
		BaseURL:             u.BaseURL,
		AnthropicBaseURL:    u.AnthropicBaseURL,
		APIKey:              u.APIKey,
		ProviderClass:       u.ProviderClass,
		Weight:              u.Weight,
		TimeoutMs:           u.TimeoutMs,
		SameUpstreamRetries: u.SameUpstreamRetries,
		Enabled:             u.Enabled,
	}

	if u.Models != nil {
		cloned.Models = make([]string, len(u.Models))
		copy(cloned.Models, u.Models)
	}

	if u.Headers != nil {
		cloned.Headers = make(map[string]string, len(u.Headers))
		for k, v := range u.Headers {
			cloned.Headers[k] = v
		}
	}

	return cloned
}

// cloneResponseInterceptRule creates a deep copy of a ResponseInterceptRule.
func cloneResponseInterceptRule(r ResponseInterceptRule) ResponseInterceptRule {
	cloned := ResponseInterceptRule{
		Name:          r.Name,
		Enabled:       r.Enabled,
		StatusCodeMin: r.StatusCodeMin,
		Action:        r.Action,
	}

	if r.Paths != nil {
		cloned.Paths = make([]string, len(r.Paths))
		copy(cloned.Paths, r.Paths)
	}

	if r.StatusCodes != nil {
		cloned.StatusCodes = make([]int, len(r.StatusCodes))
		copy(cloned.StatusCodes, r.StatusCodes)
	}

	if r.MessageKeywords != nil {
		cloned.MessageKeywords = make([]string, len(r.MessageKeywords))
		copy(cloned.MessageKeywords, r.MessageKeywords)
	}

	return cloned
}

// ToImmutable returns an immutable copy of the configuration.
// This is an alias for Clone() for explicit semantic clarity.
func (cb *ConfigBuilder) ToImmutable() Config {
	return cb.Clone()
}
