package config

import (
	"strings"
	"testing"
)

func TestNewConfigBuilder(t *testing.T) {
	cb := NewConfigBuilder()
	if cb == nil {
		t.Fatal("NewConfigBuilder returned nil")
	}

	cfg := cb.Config()

	// Verify defaults are applied
	if cfg.Listen != ":8080" {
		t.Errorf("expected default listen :8080, got %s", cfg.Listen)
	}
	if cfg.Router.Strategy != RouterStrategyHealthWeightedRR {
		t.Errorf("expected default strategy %s, got %s", RouterStrategyHealthWeightedRR, cfg.Router.Strategy)
	}
}

func TestNewConfigBuilderWithDefaults(t *testing.T) {
	cb := NewConfigBuilderWithDefaults()
	if cb == nil {
		t.Fatal("NewConfigBuilderWithDefaults returned nil")
	}

	cfg := cb.Config()
	if cfg.Listen != ":8080" {
		t.Errorf("expected default listen :8080, got %s", cfg.Listen)
	}
}

func TestConfigBuilder_WithListen(t *testing.T) {
	cb := NewConfigBuilder().WithListen(":9090")
	cfg := cb.Config()

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestConfigBuilder_WithAdminEnabled(t *testing.T) {
	cb := NewConfigBuilder().WithAdminEnabled(true)
	cfg := cb.Config()

	if !cfg.Admin.Enabled {
		t.Error("expected admin to be enabled")
	}
}

func TestConfigBuilder_WithAdminAuthToken(t *testing.T) {
	token := "my-secure-token-with-at-least-32-chars"
	cb := NewConfigBuilder().WithAdminAuthToken(token)
	cfg := cb.Config()

	if cfg.Admin.AuthToken != token {
		t.Errorf("expected auth token %q, got %q", token, cfg.Admin.AuthToken)
	}
}

func TestConfigBuilder_WithAdminLanguage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"en", AdminLanguageEnglish},
		{"EN", AdminLanguageEnglish},
		{"invalid", AdminLanguageChinese}, // falls back to default
	}

	for _, tt := range tests {
		cb := NewConfigBuilder().WithAdminLanguage(tt.input)
		cfg := cb.Config()

		if cfg.Admin.Language != tt.expected {
			t.Errorf("WithAdminLanguage(%q): expected %q, got %q", tt.input, tt.expected, cfg.Admin.Language)
		}
	}
}

func TestConfigBuilder_WithRouterStrategy(t *testing.T) {
	cb := NewConfigBuilder().WithRouterStrategy("round_robin")
	cfg := cb.Config()

	if cfg.Router.Strategy != RouterStrategyRoundRobin {
		t.Errorf("expected strategy %s, got %s", RouterStrategyRoundRobin, cfg.Router.Strategy)
	}
}

func TestConfigBuilder_WithMaxRetries(t *testing.T) {
	tests := []struct {
		retries  int
		wantErr  bool
		expected int
	}{
		{5, false, 5},
		{0, false, 0},
		{-1, true, 0}, // error case
	}

	for _, tt := range tests {
		cb := NewConfigBuilder().WithMaxRetries(tt.retries)
		cfg, err := cb.Build()

		if tt.wantErr {
			if err == nil {
				t.Error("expected error for negative retries")
			}
		} else {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if cfg.Router.MaxRetries != tt.expected {
				t.Errorf("expected max_retries %d, got %d", tt.expected, cfg.Router.MaxRetries)
			}
		}
	}
}

func TestConfigBuilder_WithRetryBackoff(t *testing.T) {
	tests := []struct {
		backoffMs int
		wantErr   bool
		expected  int
	}{
		{1000, false, 1000},
		{0, false, 0},
		{-100, true, 0}, // error case
	}

	for _, tt := range tests {
		cb := NewConfigBuilder().WithRetryBackoff(tt.backoffMs)
		cfg, err := cb.Build()

		if tt.wantErr {
			if err == nil {
				t.Error("expected error for negative backoff")
			}
		} else {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if cfg.Router.RetryBackoffMs != tt.expected {
				t.Errorf("expected retry_backoff_ms %d, got %d", tt.expected, cfg.Router.RetryBackoffMs)
			}
		}
	}
}

func TestConfigBuilder_WithHealthCheck(t *testing.T) {
	cb := NewConfigBuilder().WithHealthCheck(true)
	cfg := cb.Config()

	if !cfg.Health.Enabled {
		t.Error("expected health check to be enabled")
	}
}

func TestConfigBuilder_WithHealthPath(t *testing.T) {
	path := "/custom/health"
	cb := NewConfigBuilder().WithHealthPath(path)
	cfg := cb.Config()

	if cfg.Health.Path != path {
		t.Errorf("expected health path %q, got %q", path, cfg.Health.Path)
	}
}

func TestConfigBuilder_WithTelemetryPath(t *testing.T) {
	path := "/custom/telemetry.db"
	cb := NewConfigBuilder().WithTelemetryPath(path)
	cfg := cb.Config()

	if cfg.Telemetry.SQLitePath != path {
		t.Errorf("expected telemetry path %q, got %q", path, cfg.Telemetry.SQLitePath)
	}
}

func TestConfigBuilder_WithPricingCachePath(t *testing.T) {
	path := "/custom/pricing.json"
	cb := NewConfigBuilder().WithPricingCachePath(path)
	cfg := cb.Config()

	if cfg.Pricing.CachePath != path {
		t.Errorf("expected pricing path %q, got %q", path, cfg.Pricing.CachePath)
	}
}

func TestConfigBuilder_WithBridgeEnabled(t *testing.T) {
	cb := NewConfigBuilder().WithBridgeEnabled(true)
	cfg := cb.Config()

	if !cfg.Bridge.Enabled {
		t.Error("expected bridge to be enabled")
	}
}

func TestConfigBuilder_WithBridgeRule(t *testing.T) {
	cb := NewConfigBuilder().
		WithBridgeRule("gpt-5", "gpt-4").
		WithBridgeRule("claude-3", "claude-4")
	cfg := cb.Config()

	if len(cfg.Bridge.Rules) != 2 {
		t.Fatalf("expected 2 bridge rules, got %d", len(cfg.Bridge.Rules))
	}
	if cfg.Bridge.Rules[0].From != "gpt-5" || cfg.Bridge.Rules[0].To != "gpt-4" {
		t.Error("first bridge rule mismatch")
	}
}

func TestConfigBuilder_WithFallbackEnabled(t *testing.T) {
	cb := NewConfigBuilder().WithFallbackEnabled(true)
	cfg := cb.Config()

	if !cfg.Fallback.Enabled {
		t.Error("expected fallback to be enabled")
	}
}

func TestConfigBuilder_WithFallbackModel(t *testing.T) {
	cb := NewConfigBuilder().
		WithFallbackModel("gpt-4", "gpt-3.5-turbo").
		WithFallbackModel("claude-3-opus", "claude-3-sonnet")
	cfg := cb.Config()

	if len(cfg.Fallback.Models) != 2 {
		t.Fatalf("expected 2 fallback models, got %d", len(cfg.Fallback.Models))
	}
	if cfg.Fallback.Models["gpt-4"] != "gpt-3.5-turbo" {
		t.Error("fallback model mapping mismatch")
	}
}

func TestConfigBuilder_WithUpstream(t *testing.T) {
	upstream := Upstream{
		Name:    "custom-upstream",
		BaseURL: "https://custom.example.com",
		APIKey:  "custom-key",
		Models:  []string{"model1", "model2"},
	}

	cb := NewConfigBuilder().WithUpstream(upstream)
	cfg := cb.Config()

	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
	}
	if cfg.Upstreams[0].Name != upstream.Name {
		t.Errorf("expected upstream name %q, got %q", upstream.Name, cfg.Upstreams[0].Name)
	}
}

func TestConfigBuilder_WithSimpleUpstream(t *testing.T) {
	cb := NewConfigBuilder().WithSimpleUpstream(
		"simple-upstream",
		"https://simple.example.com",
		"api-key",
		"gpt-4",
		"gpt-3.5-turbo",
	)
	cfg := cb.Config()

	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(cfg.Upstreams))
	}
	u := cfg.Upstreams[0]
	if u.Name != "simple-upstream" {
		t.Errorf("expected name 'simple-upstream', got %q", u.Name)
	}
	if len(u.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(u.Models))
	}
	if u.Weight != 1 {
		t.Errorf("expected default weight 1, got %d", u.Weight)
	}
}

func TestConfigBuilder_WithStickySessions(t *testing.T) {
	cb := NewConfigBuilder().WithStickySessions(true, 3600)
	cfg := cb.Config()

	if !cfg.Router.StickySessions.Enabled {
		t.Error("expected sticky sessions to be enabled")
	}
	if cfg.Router.StickySessions.TTLSec != 3600 {
		t.Errorf("expected TTL 3600, got %d", cfg.Router.StickySessions.TTLSec)
	}
}

func TestConfigBuilder_Build(t *testing.T) {
	cb := NewConfigBuilder().
		WithListen(":9090").
		WithAdminEnabled(true).
		WithAdminAuthToken("secure-token-with-at-least-32-characters-long")

	cfg, err := cb.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestConfigBuilder_Build_WithErrors(t *testing.T) {
	cb := NewConfigBuilder().
		WithMaxRetries(-1).
		WithRetryBackoff(-1)

	_, err := cb.Build()
	if err == nil {
		t.Fatal("expected error from Build() with negative values")
	}

	// Should return the first error
	if !strings.Contains(err.Error(), "max_retries") && !strings.Contains(err.Error(), "retry_backoff") {
		t.Errorf("expected error about invalid values, got: %v", err)
	}
}

func TestConfigBuilder_MustBuild(t *testing.T) {
	cb := NewConfigBuilder().WithListen(":9090")

	cfg := cb.MustBuild()
	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestConfigBuilder_MustBuild_Panic(t *testing.T) {
	cb := NewConfigBuilder().WithMaxRetries(-1)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustBuild to panic on error")
		}
	}()

	_ = cb.MustBuild()
}

func TestConfigBuilder_Validate(t *testing.T) {
	// Valid config
	cb := NewConfigBuilder().
		WithListen(":9090").
		WithSimpleUpstream("test", "https://example.com", "", "gpt-4")

	err := cb.Validate()
	if err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}

	// Invalid config - no upstreams
	cb2 := NewConfigBuilder().WithListen(":9090")
	err = cb2.Validate()
	if err == nil {
		t.Error("expected validation error for missing upstreams")
	}
}

func TestConfigBuilder_Clone(t *testing.T) {
	cb := NewConfigBuilder().
		WithListen(":9090").
		WithSimpleUpstream("test", "https://example.com", "key", "gpt-4")

	original := cb.Config()
	cloned := cb.Clone()

	// Verify cloned has same values
	if cloned.Listen != original.Listen {
		t.Error("cloned Listen mismatch")
	}

	// Modify clone and verify original is unchanged
	cloned.Listen = ":9999"
	cloned.Upstreams[0].Name = "modified"

	if cb.Config().Listen == ":9999" {
		t.Error("modifying cloned config affected builder")
	}
}

func TestConfigBuilder_ToImmutable(t *testing.T) {
	cb := NewConfigBuilder().WithListen(":9090")
	cfg := cb.ToImmutable()

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestCloneConfig(t *testing.T) {
	original := Config{
		Listen: ":8080",
		Upstreams: []Upstream{
			{
				Name:    "test",
				BaseURL: "https://example.com",
				Models:  []string{"gpt-4"},
				Headers: map[string]string{"X-Key": "value"},
			},
		},
		Bridge: ModelBridgeConfig{
			Rules:             []ModelBridgeRule{{From: "a", To: "b"}},
			ExcludeUserAgents: []string{"Bot/*"},
		},
		Fallback: ModelFallbackConfig{
			Models: map[string]string{"gpt-4": "gpt-3.5"},
		},
		Proxy: ProxyPolicyConfig{
			Intercepts: []ResponseInterceptRule{
				{
					Name:            "test",
					Paths:           []string{"/v1/chat"},
					StatusCodes:     []int{500},
					MessageKeywords: []string{"error"},
				},
			},
			Retry: RetryPolicyConfig{
				StatusCodes:     []int{429},
				MessageKeywords: []string{"rate limit"},
			},
		},
	}
	original.Normalize()

	cloned := cloneConfig(original)

	// Verify all fields are copied
	if cloned.Listen != original.Listen {
		t.Error("Listen not cloned correctly")
	}

	// Verify slices are deep copied
	original.Upstreams[0].Name = "modified"
	if cloned.Upstreams[0].Name == "modified" {
		t.Error("Upstream slice was not deep copied")
	}

	// Verify maps are deep copied
	original.Fallback.Models["gpt-4"] = "modified"
	if cloned.Fallback.Models["gpt-4"] == "modified" {
		t.Error("Fallback map was not deep copied")
	}
}

func TestCloneUpstream(t *testing.T) {
	original := Upstream{
		Name:    "test",
		Models:  []string{"gpt-4"},
		Headers: map[string]string{"X-Key": "value"},
	}

	cloned := cloneUpstream(original)

	// Modify original
	original.Models[0] = "modified"
	original.Headers["X-Key"] = "modified"

	// Verify clone is unchanged
	if cloned.Models[0] != "gpt-4" {
		t.Error("Models not deep copied")
	}
	if cloned.Headers["X-Key"] != "value" {
		t.Error("Headers not deep copied")
	}
}

func TestCloneResponseInterceptRule(t *testing.T) {
	original := ResponseInterceptRule{
		Name:            "test",
		Paths:           []string{"/v1"},
		StatusCodes:     []int{500},
		MessageKeywords: []string{"error"},
	}

	cloned := cloneResponseInterceptRule(original)

	// Modify original
	original.Paths[0] = "/modified"
	original.StatusCodes[0] = 999

	// Verify clone is unchanged
	if cloned.Paths[0] != "/v1" {
		t.Error("Paths not deep copied")
	}
	if cloned.StatusCodes[0] != 500 {
		t.Error("StatusCodes not deep copied")
	}
}
