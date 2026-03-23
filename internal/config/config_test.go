package config

import (
	"testing"
)

func TestNormalizeDefaults(t *testing.T) {
	var cfg Config
	cfg.Normalize()
	if cfg.Listen != ":8080" {
		t.Fatalf("expected default listen :8080, got %s", cfg.Listen)
	}
	if cfg.Router.Strategy != "health_weighted_rr" {
		t.Fatalf("expected default strategy health_weighted_rr, got %s", cfg.Router.Strategy)
	}
	if cfg.Router.MaxRetries != 2 {
		t.Fatalf("expected default max_retries 2, got %d", cfg.Router.MaxRetries)
	}
	if cfg.Health.Path != "/v1/models" {
		t.Fatalf("expected default health path /v1/models, got %s", cfg.Health.Path)
	}
}

func TestValidateRejectsUnknownRouterStrategy(t *testing.T) {
	cfg := Config{
		Router: RouterConfig{Strategy: "weighted_random"},
		Upstreams: []Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Weight: 1},
		},
	}
	cfg.Normalize()

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid router strategy to fail validation")
	}
	if got := err.Error(); got != "router.strategy must be health_weighted_rr or round_robin" {
		t.Fatalf("unexpected validation error %q", got)
	}
}

func TestNormalizeAdminLanguage(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"zh", "zh"},
		{"en", "en"},
		{"ja", "ja"},
		{"ko", "ko"},
		{"es", "es"},
		{"fr", "fr"},
		{"de", "de"},
		{"", "zh"},
		{"invalid", "zh"},
		{" EN ", "en"},
	}
	for _, tt := range tests {
		got := NormalizeAdminLanguage(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeAdminLanguage(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRewriteModel(t *testing.T) {
	cfg := Config{
		Bridge: ModelBridgeConfig{
			Enabled: true,
			Rules: []ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
				{From: "gpt-5.3*", To: "gpt-5.4"},
			},
		},
	}
	tests := []struct{ input, expected string }{
		{"gpt-5.2", "gpt-5.4"},
		{"gpt-5.3-codex", "gpt-5.4"},
		{"gpt-5.4", "gpt-5.4"},
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"", ""},
	}
	for _, tt := range tests {
		got := cfg.RewriteModel(tt.input)
		if got != tt.expected {
			t.Errorf("RewriteModel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRewriteModelDisabled(t *testing.T) {
	cfg := Config{
		Bridge: ModelBridgeConfig{
			Enabled: false,
			Rules: []ModelBridgeRule{
				{From: "gpt-5.2", To: "gpt-5.4"},
			},
		},
	}
	if got := cfg.RewriteModel("gpt-5.2"); got != "gpt-5.2" {
		t.Fatalf("expected gpt-5.2 when bridge disabled, got %s", got)
	}
}

func TestUpstreamIsEnabled(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name string
		u    Upstream
		want bool
	}{
		{"nil enabled", Upstream{}, true},
		{"explicitly enabled", Upstream{Enabled: &enabled}, true},
		{"explicitly disabled", Upstream{Enabled: &disabled}, false},
	}
	for _, tt := range tests {
		if got := tt.u.IsEnabled(); got != tt.want {
			t.Errorf("%s: IsEnabled() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNormalizeUpstreamClass(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"free", "free"},
		{"quota_limited", "quota_limited"},
		{"", "quota_limited"},
		{"invalid", "quota_limited"},
		{" FREE ", "free"},
	}
	for _, tt := range tests {
		got := NormalizeUpstreamClass(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeUpstreamClass(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
