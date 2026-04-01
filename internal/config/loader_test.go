package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromFile_Success(t *testing.T) {
	content := `
listen: ":9090"
upstreams:
  - name: test-upstream
    base_url: https://api.example.com
    api_key: test-key
    models:
      - gpt-4
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
	}
	if cfg.Upstreams[0].Name != "test-upstream" {
		t.Errorf("expected upstream name 'test-upstream', got %s", cfg.Upstreams[0].Name)
	}
}

func TestLoadFromFile_MissingFile(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("expected 'read config' error, got: %v", err)
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	content := `
listen: ":8080"
upstreams: [invalid yaml :::
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected 'parse config' error, got: %v", err)
	}
}

func TestLoadFromFile_ValidationFails(t *testing.T) {
	// Config with no upstreams should fail validation
	content := `
listen: ":8080"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadFromFile(configPath)
	if err == nil {
		t.Fatal("expected validation error for missing upstreams")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Errorf("expected upstream error, got: %v", err)
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantErr   bool
		errPrefix string
	}{
		{
			name: "valid minimal config",
			content: `
listen: ":8080"
upstreams:
  - name: test
    base_url: https://api.example.com
`,
			wantErr: false,
		},
		{
			name:      "invalid YAML",
			content:   "{invalid",
			wantErr:   true,
			errPrefix: "parse config",
		},
		{
			name: "empty upstreams is valid YAML",
			content: `
listen: ":8080"
upstreams: []
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.content))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errPrefix != "" && !strings.HasPrefix(err.Error(), tt.errPrefix) {
					t.Errorf("expected error starting with %q, got: %v", tt.errPrefix, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Basic sanity check
			_ = cfg
		})
	}
}

func TestConfigValidate_ListenRequired(t *testing.T) {
	cfg := Config{
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}
	// Don't normalize - set empty listen explicitly
	cfg.Listen = ""
	cfg.Router.Strategy = ""
	cfg.Admin.Language = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing listen")
	}
	if !strings.Contains(err.Error(), "listen is required") {
		t.Errorf("expected 'listen is required' error, got: %v", err)
	}
}

func TestConfigValidate_InvalidRouterStrategy(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Router: RouterConfig{Strategy: "invalid_strategy"},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid router strategy")
	}
	if !strings.Contains(err.Error(), "router.strategy") {
		t.Errorf("expected router.strategy error, got: %v", err)
	}
}

func TestConfigValidate_NegativeValues(t *testing.T) {
	tests := []struct {
		name      string
		modifier  func(*Config)
		wantErr   string
	}{
		{
			name: "negative max_retries",
			modifier: func(c *Config) {
				c.Router.MaxRetries = -1
			},
			wantErr: "router.max_retries must be >= 0",
		},
		{
			name: "negative retry_backoff_ms",
			modifier: func(c *Config) {
				c.Router.RetryBackoffMs = -1
			},
			wantErr: "router.retry_backoff_ms must be >= 0",
		},
		{
			name: "negative retry_backoff_max_ms",
			modifier: func(c *Config) {
				c.Router.RetryBackoffMaxMs = -1
			},
			wantErr: "router.retry_backoff_max_ms must be >= 0",
		},
		{
			name: "negative failure_threshold",
			modifier: func(c *Config) {
				c.Router.FailureThreshold = -1
			},
			wantErr: "router.failure_threshold must be >= 0",
		},
		{
			name: "negative cooldown_sec",
			modifier: func(c *Config) {
				c.Router.CooldownSec = -1
			},
			wantErr: "router.cooldown_sec must be >= 0",
		},
		{
			name: "negative failure_passthrough_after_sec",
			modifier: func(c *Config) {
				c.Router.FailurePassthroughAfterSec = -1
			},
			wantErr: "router.failure_passthrough_after_sec must be >= 0",
		},
		{
			name: "negative pricing.refresh_interval_hours",
			modifier: func(c *Config) {
				c.Pricing.RefreshIntervalHours = -1
			},
			wantErr: "pricing.refresh_interval_hours must be >= 0",
		},
		{
			name: "negative pricing.request_timeout_ms",
			modifier: func(c *Config) {
				c.Pricing.RequestTimeoutMs = -1
			},
			wantErr: "pricing.request_timeout_ms must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Listen: ":8080",
				Upstreams: []Upstream{
					{Name: "test", BaseURL: "https://example.com"},
				},
			}
			tt.modifier(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestConfigValidate_AdminEnabledMissingAuthToken(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Admin:  AdminConfig{Enabled: true, AuthToken: ""},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for admin enabled without auth_token")
	}
	if !strings.Contains(err.Error(), "admin.auth_token is required") {
		t.Errorf("expected auth_token error, got: %v", err)
	}
}

func TestConfigValidate_InvalidStatusCodes(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Proxy: ProxyPolicyConfig{
			Retry: RetryPolicyConfig{
				StatusCodes: []int{50, 200, 999},
			},
		},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid status codes")
	}
	if !strings.Contains(err.Error(), "status_codes") {
		t.Errorf("expected status_codes error, got: %v", err)
	}
}

func TestConfigValidate_InvalidProxyRetryStatusCodeMin(t *testing.T) {
	negative := -1
	cfg := Config{
		Listen: ":8080",
		Proxy: ProxyPolicyConfig{
			Retry: RetryPolicyConfig{
				StatusCodeMin: &negative,
			},
		},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative status_code_min")
	}
	if !strings.Contains(err.Error(), "proxy.retry.status_code_min must be >= 0") {
		t.Errorf("expected status_code_min error, got: %v", err)
	}
}

func TestConfigValidate_InvalidInterceptAction(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Proxy: ProxyPolicyConfig{
			Intercepts: []ResponseInterceptRule{
				{Action: "invalid_action"},
			},
		},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid intercept action")
	}
	if !strings.Contains(err.Error(), "action must be retry or fail") {
		t.Errorf("expected action error, got: %v", err)
	}
}

func TestConfigValidate_InvalidInterceptStatusCodes(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Proxy: ProxyPolicyConfig{
			Intercepts: []ResponseInterceptRule{
				{Action: "retry", StatusCodes: []int{999}},
			},
		},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid intercept status codes")
	}
	if !strings.Contains(err.Error(), "status_codes") {
		t.Errorf("expected status_codes error, got: %v", err)
	}
}

func TestConfigValidate_InvalidInterceptStatusCodeMin(t *testing.T) {
	negative := -1
	cfg := Config{
		Listen: ":8080",
		Proxy: ProxyPolicyConfig{
			Intercepts: []ResponseInterceptRule{
				{Action: "retry", StatusCodeMin: &negative},
			},
		},
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative intercept status_code_min")
	}
	if !strings.Contains(err.Error(), "status_code_min must be >= 0") {
		t.Errorf("expected status_code_min error, got: %v", err)
	}
}

func TestConfigValidate_InvalidBridgeRule(t *testing.T) {
	tests := []struct {
		name    string
		rules   []ModelBridgeRule
		wantErr string
	}{
		{
			name:    "empty from",
			rules:   []ModelBridgeRule{{From: "", To: "target"}},
			wantErr: "bridge.rules[0].from is required",
		},
		{
			name:    "empty to",
			rules:   []ModelBridgeRule{{From: "source", To: ""}},
			wantErr: "bridge.rules[0].to is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Listen: ":8080",
				Bridge: ModelBridgeConfig{
					Enabled: true,
					Rules:   tt.rules,
				},
				Upstreams: []Upstream{
					{Name: "test", BaseURL: "https://example.com"},
				},
			}

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestConfigValidate_UpstreamValidation(t *testing.T) {
	tests := []struct {
		name      string
		upstreams []Upstream
		wantErr   string
	}{
		{
			name:      "no upstreams",
			upstreams: []Upstream{},
			wantErr:   "at least one upstream is required",
		},
		{
			name:      "upstream missing name",
			upstreams: []Upstream{{Name: "", BaseURL: "https://example.com"}},
			wantErr:   "upstreams[0].name is required",
		},
		{
			name:      "upstream missing base_url",
			upstreams: []Upstream{{Name: "test", BaseURL: ""}},
			wantErr:   "upstreams[0].base_url is required",
		},
		{
			name:      "upstream invalid base_url",
			upstreams: []Upstream{{Name: "test", BaseURL: "://invalid-url"}},
			wantErr:   "upstreams[0].base_url invalid",
		},
		{
			name:      "duplicate upstream names",
			upstreams: []Upstream{{Name: "test", BaseURL: "https://a.com"}, {Name: "test", BaseURL: "https://b.com"}},
			wantErr:   "duplicate upstream name: test",
		},
		{
			name:      "no enabled upstreams",
			upstreams: []Upstream{{Name: "test", BaseURL: "https://example.com", Enabled: boolPtr(false)}},
			wantErr:   "no enabled upstreams",
		},
		{
			name:      "invalid anthropic_base_url",
			upstreams: []Upstream{{Name: "test", BaseURL: "https://example.com", AnthropicBaseURL: "://invalid"}},
			wantErr:   "upstreams[0].anthropic_base_url invalid",
		},
		{
			name:      "invalid provider_class",
			upstreams: []Upstream{{Name: "test", BaseURL: "https://example.com", ProviderClass: "invalid_class"}},
			wantErr:   "provider_class must be free or quota_limited",
		},
		{
			name:      "negative same_upstream_retries",
			upstreams: []Upstream{{Name: "test", BaseURL: "https://example.com", SameUpstreamRetries: -1}},
			wantErr:   "same_upstream_retries must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Listen:    ":8080",
				Upstreams: tt.upstreams,
			}

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestConfigValidate_ValidConfig(t *testing.T) {
	cfg := Config{
		Listen: ":8080",
		Upstreams: []Upstream{
			{Name: "test1", BaseURL: "https://example1.com"},
			{Name: "test2", BaseURL: "https://example2.com", Enabled: boolPtr(true)},
		},
		Router: RouterConfig{
			Strategy: "health_weighted_rr",
		},
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestResolveRelativePaths(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config", "gateway.yaml")

	// Create config dir
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: "data/telemetry.db",
		},
		Pricing: PricingConfig{
			CachePath: "cache/pricing.json",
		},
	}

	resolveRelativePaths(&cfg, configPath)

	expectedTelemetry := filepath.Join(tmpDir, "config", "data", "telemetry.db")
	expectedPricing := filepath.Join(tmpDir, "config", "cache", "pricing.json")

	if cfg.Telemetry.SQLitePath != expectedTelemetry {
		t.Errorf("telemetry path: expected %q, got %q", expectedTelemetry, cfg.Telemetry.SQLitePath)
	}
	if cfg.Pricing.CachePath != expectedPricing {
		t.Errorf("pricing path: expected %q, got %q", expectedPricing, cfg.Pricing.CachePath)
	}
}

func TestResolveRelativePaths_AbsolutePaths(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	absolutePath := filepath.Join(tmpDir, "absolute", "path.db")
	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: absolutePath,
		},
	}

	resolveRelativePaths(&cfg, configPath)

	// Absolute paths should remain unchanged
	if cfg.Telemetry.SQLitePath != absolutePath {
		t.Errorf("absolute path should not change: expected %q, got %q", absolutePath, cfg.Telemetry.SQLitePath)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
