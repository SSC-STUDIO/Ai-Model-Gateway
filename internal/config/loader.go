package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	cfg.Normalize()
	resolveRelativePaths(&cfg, path)

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen is required")
	}
	if c.Router.MaxRetries < 0 {
		return errors.New("router.max_retries must be >= 0")
	}
	if c.Router.RetryBackoffMs < 0 {
		return errors.New("router.retry_backoff_ms must be >= 0")
	}
	if c.Router.RetryBackoffMaxMs < 0 {
		return errors.New("router.retry_backoff_max_ms must be >= 0")
	}
	if c.Router.FailureThreshold < 0 {
		return errors.New("router.failure_threshold must be >= 0")
	}
	if c.Router.CooldownSec < 0 {
		return errors.New("router.cooldown_sec must be >= 0")
	}
	if c.Router.FailurePassthroughAfterSec < 0 {
		return errors.New("router.failure_passthrough_after_sec must be >= 0")
	}
	if c.Admin.Enabled && c.Admin.AuthToken == "" {
		return errors.New("admin.auth_token is required when admin is enabled")
	}
	if c.Pricing.RefreshIntervalHours < 0 {
		return errors.New("pricing.refresh_interval_hours must be >= 0")
	}
	if c.Pricing.RequestTimeoutMs < 0 {
		return errors.New("pricing.request_timeout_ms must be >= 0")
	}
	for i, code := range c.Proxy.Retry.StatusCodes {
		if code < 100 || code > 599 {
			return fmt.Errorf("proxy.retry.status_codes[%d] must be between 100 and 599", i)
		}
	}
	if c.Proxy.Retry.StatusCodeMin != nil && *c.Proxy.Retry.StatusCodeMin < 0 {
		return errors.New("proxy.retry.status_code_min must be >= 0")
	}
	for i, rule := range c.Proxy.Intercepts {
		action := strings.ToLower(strings.TrimSpace(rule.Action))
		if action == "" {
			continue
		}
		if action != "retry" && action != "fail" {
			return fmt.Errorf("proxy.intercepts[%d].action must be retry or fail", i)
		}
		for j, code := range rule.StatusCodes {
			if code < 100 || code > 599 {
				return fmt.Errorf("proxy.intercepts[%d].status_codes[%d] must be between 100 and 599", i, j)
			}
		}
		if rule.StatusCodeMin != nil && *rule.StatusCodeMin < 0 {
			return fmt.Errorf("proxy.intercepts[%d].status_code_min must be >= 0", i)
		}
	}
	for i, rule := range c.Bridge.Rules {
		if strings.TrimSpace(rule.From) == "" {
			return fmt.Errorf("bridge.rules[%d].from is required", i)
		}
		if strings.TrimSpace(rule.To) == "" {
			return fmt.Errorf("bridge.rules[%d].to is required", i)
		}
	}
	if len(c.Upstreams) == 0 {
		return errors.New("at least one upstream is required")
	}

	hasEnabled := false
	seen := map[string]struct{}{}

	for i, u := range c.Upstreams {
		if u.Name == "" {
			return fmt.Errorf("upstreams[%d].name is required", i)
		}
		if _, ok := seen[u.Name]; ok {
			return fmt.Errorf("duplicate upstream name: %s", u.Name)
		}
		seen[u.Name] = struct{}{}

		if u.BaseURL == "" {
			return fmt.Errorf("upstreams[%d].base_url is required", i)
		}
		if _, err := url.ParseRequestURI(u.BaseURL); err != nil {
			return fmt.Errorf("upstreams[%d].base_url invalid: %w", i, err)
		}
		if raw := strings.TrimSpace(u.ProviderClass); raw != "" {
			normalized := NormalizeUpstreamClass(raw)
			if !strings.EqualFold(raw, normalized) &&
				!(strings.EqualFold(raw, "quota-limited") && normalized == UpstreamClassQuotaLimited) {
				return fmt.Errorf("upstreams[%d].provider_class must be free or quota_limited", i)
			}
		}
		if u.SameUpstreamRetries < 0 {
			return fmt.Errorf("upstreams[%d].same_upstream_retries must be >= 0", i)
		}
		if u.IsEnabled() {
			hasEnabled = true
		}
	}

	if !hasEnabled {
		return errors.New("no enabled upstreams")
	}

	return nil
}

func resolveRelativePaths(cfg *Config, sourcePath string) {
	baseDir := filepath.Dir(sourcePath)
	if cfg.Telemetry.SQLitePath != "" && !filepath.IsAbs(cfg.Telemetry.SQLitePath) {
		cfg.Telemetry.SQLitePath = filepath.Clean(filepath.Join(baseDir, cfg.Telemetry.SQLitePath))
	}
	if cfg.Pricing.CachePath != "" && !filepath.IsAbs(cfg.Pricing.CachePath) {
		cfg.Pricing.CachePath = filepath.Clean(filepath.Join(baseDir, cfg.Pricing.CachePath))
	}
}
