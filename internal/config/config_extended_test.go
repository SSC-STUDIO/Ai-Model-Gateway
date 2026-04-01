package config

import (
	"strings"
	"testing"
)

func TestNormalizeRouterStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", RouterStrategyHealthWeightedRR},
		{"health_weighted_rr", RouterStrategyHealthWeightedRR},
		{"round_robin", RouterStrategyRoundRobin},
		{"  Health_Weighted_RR  ", RouterStrategyHealthWeightedRR},
		{"ROUND_ROBIN", RouterStrategyRoundRobin},
		{"unknown_custom", "unknown_custom"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeRouterStrategy(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeRouterStrategy(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeUpstreamClass_Extended(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Standard values
		{"free", UpstreamClassFree},
		{"quota_limited", UpstreamClassQuotaLimited},
		{"", UpstreamClassQuotaLimited},
		// Aliases
		{"quota", UpstreamClassQuotaLimited},
		{"quota-limited", UpstreamClassQuotaLimited},
		{"limited", UpstreamClassQuotaLimited},
		{"metered", UpstreamClassQuotaLimited},
		{"paid", UpstreamClassQuotaLimited},
		{"gratis", UpstreamClassFree},
		{"public", UpstreamClassFree},
		// Case variations
		{"FREE", UpstreamClassFree},
		{"Free", UpstreamClassFree},
		{"  FREE  ", UpstreamClassFree},
		// Invalid values
		{"invalid", UpstreamClassQuotaLimited},
		{"unknown", UpstreamClassQuotaLimited},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeUpstreamClass(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeUpstreamClass(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidateAdminLanguage_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty string", "", false},
		{"valid zh", "zh", false},
		{"valid en", "en", false},
		{"valid ja", "ja", false},
		{"valid ko", "ko", false},
		{"valid es", "es", false},
		{"valid fr", "fr", false},
		{"valid de", "de", false},
		{"case insensitive EN", "EN", false},
		{"whitespace padded", "  en  ", false},
		{"invalid language", "invalid", true},
		{"partial match", "eng", true},
		{"chinese variant", "cn", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdminLanguage(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateAdminLanguage(%q) expected error", tt.input)
				}
				// Verify error message contains expected content
				if err != nil && !strings.Contains(err.Error(), "admin.language must be one of") {
					t.Errorf("unexpected error message: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAdminLanguage(%q) unexpected error: %v", tt.input, err)
				}
			}
		})
	}
}

func TestAdminLanguageValidationMessage(t *testing.T) {
	msg := AdminLanguageValidationMessage()
	if msg == "" {
		t.Error("AdminLanguageValidationMessage() should not return empty string")
	}
	if !strings.Contains(msg, "admin.language") {
		t.Error("message should contain 'admin.language'")
	}
}

func TestNormalizeAdminLanguage_Extended(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Standard values
		{"zh", AdminLanguageChinese},
		{"en", AdminLanguageEnglish},
		{"ja", AdminLanguageJapanese},
		{"ko", AdminLanguageKorean},
		{"es", AdminLanguageSpanish},
		{"fr", AdminLanguageFrench},
		{"de", AdminLanguageGerman},
		// Case variations
		{"EN", AdminLanguageEnglish},
		{"En", AdminLanguageEnglish},
		// Whitespace
		{"  en  ", AdminLanguageEnglish},
		{"en ", AdminLanguageEnglish},
		{" en", AdminLanguageEnglish},
		// Default fallback
		{"", AdminLanguageChinese},
		{"invalid", AdminLanguageChinese},
		{"xyz", AdminLanguageChinese},
		{"中文", AdminLanguageChinese},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeAdminLanguage(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeAdminLanguage(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUpstream_IsEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name     string
		upstream Upstream
		want     bool
	}{
		{"nil enabled defaults to true", Upstream{}, true},
		{"explicitly true", Upstream{Enabled: &enabled}, true},
		{"explicitly false", Upstream{Enabled: &disabled}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.upstream.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpstream_ProviderClassNormalized(t *testing.T) {
	tests := []struct {
		providerClass string
		expected      string
	}{
		{"", UpstreamClassQuotaLimited},
		{"free", UpstreamClassFree},
		{"FREE", UpstreamClassFree},
		{"quota_limited", UpstreamClassQuotaLimited},
	}

	for _, tt := range tests {
		t.Run(tt.providerClass, func(t *testing.T) {
			u := Upstream{ProviderClass: tt.providerClass}
			got := u.ProviderClassNormalized()
			if got != tt.expected {
				t.Errorf("ProviderClassNormalized() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestResponseInterceptRule_IsEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name string
		rule ResponseInterceptRule
		want bool
	}{
		{"nil enabled defaults to true", ResponseInterceptRule{}, true},
		{"explicitly true", ResponseInterceptRule{Enabled: &enabled}, true},
		{"explicitly false", ResponseInterceptRule{Enabled: &disabled}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.IsEnabled()
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		// Exact match
		{"gpt-4", "gpt-4", true},
		// Wildcard
		{"*", "anything", true},
		{"*", "", true},
		// Pattern matching
		{"gpt-*", "gpt-4", true},
		{"gpt-*", "gpt-4-turbo", true},
		{"gpt-*", "claude-3", false},
		// Empty pattern
		{"", "anything", false},
		{"", "", false},
		// Case sensitivity - EqualFold fallback makes these match
		{"GPT-4", "gpt-4", true},
		{"gpt-4", "GPT-4", true},
		// EqualFold fallback
		{"gpt-4", "GPT-4", true},
		// Whitespace handling
		{"  gpt-4  ", "gpt-4", true}, // whitespace is trimmed
		// Complex patterns
		{"*.com", "example.com", true},
		{"*.com", "example.org", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.value, func(t *testing.T) {
			got := MatchesPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("MatchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestMatchesPattern_ExportedAlias(t *testing.T) {
	// Verify MatchesPattern is properly exported as an alias
	if !MatchesPattern("*", "test") {
		t.Error("MatchesPattern should work as exported alias")
	}
}

func TestRewriteModelExtended(t *testing.T) {
	cfg := Config{
		Bridge: ModelBridgeConfig{
			Enabled: true,
			Rules: []ModelBridgeRule{
				{From: "gpt-4", To: "gpt-4-turbo"},
				{From: "claude-*", To: "claude-3-opus"},
			},
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-4", "gpt-4-turbo"},
		{"claude-instant", "claude-3-opus"},
		{"unmatched", "unmatched"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cfg.RewriteModel(tt.input)
			if got != tt.expected {
				t.Errorf("RewriteModel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRewriteModelForRequest(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		userAgent string
		exclude   []string
		expected  string
	}{
		{
			name:      "basic rewrite",
			model:     "gpt-4",
			userAgent: "",
			exclude:   nil,
			expected:  "gpt-4-turbo",
		},
		{
			name:      "excluded user agent",
			model:     "gpt-4",
			userAgent: "CustomAgent/1.0",
			exclude:   []string{"CustomAgent*"},
			expected:  "gpt-4", // no rewrite due to excluded UA
		},
		{
			name:      "empty model",
			model:     "",
			userAgent: "",
			exclude:   nil,
			expected:  "",
		},
		{
			name:      "bridge disabled",
			model:     "gpt-4",
			userAgent: "",
			exclude:   nil,
			expected:  "gpt-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled := tt.name != "bridge disabled"
			cfg := Config{
				Bridge: ModelBridgeConfig{
					Enabled:           enabled,
					Rules:             []ModelBridgeRule{{From: "gpt-4", To: "gpt-4-turbo"}},
					ExcludeUserAgents: tt.exclude,
				},
			}
			got := cfg.RewriteModelForRequest(tt.model, tt.userAgent)
			if got != tt.expected {
				t.Errorf("RewriteModelForRequest(%q, %q) = %q, want %q", tt.model, tt.userAgent, got, tt.expected)
			}
		})
	}
}

func TestModelBridgeRule_Matches(t *testing.T) {
	rule := ModelBridgeRule{From: "gpt-*", To: "output"}

	tests := []struct {
		model    string
		expected bool
	}{
		{"gpt-4", true},
		{"gpt-4-turbo", true},
		{"claude-3", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := rule.matches(tt.model)
			if got != tt.expected {
				t.Errorf("matches(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}

func TestModelBridgeConfig_ShouldSkipUserAgent(t *testing.T) {
	cfg := ModelBridgeConfig{
		ExcludeUserAgents: []string{"Bot/*", "Crawler"},
	}

	tests := []struct {
		userAgent string
		expected  bool
	}{
		{"", false},                         // empty UA - don't skip
		{"Bot/1.0", true},                   // matches pattern
		{"Bot/2.0/Special", true},           // matches wildcard
		{"Crawler", true},                   // exact match
		{"Mozilla/5.0", false},              // no match
		{"  Bot/1.0  ", true},               // whitespace trimmed
		{"DifferentBot/1.0", false},         // doesn't match
	}

	for _, tt := range tests {
		t.Run(tt.userAgent, func(t *testing.T) {
			got := cfg.shouldSkipUserAgent(tt.userAgent)
			if got != tt.expected {
				t.Errorf("shouldSkipUserAgent(%q) = %v, want %v", tt.userAgent, got, tt.expected)
			}
		})
	}
}

func TestGetFallbackModel(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		models   map[string]string
		input    string
		expected string
	}{
		{
			name:     "fallback enabled with match",
			enabled:  true,
			models:   map[string]string{"gpt-4": "gpt-3.5-turbo"},
			input:    "gpt-4",
			expected: "gpt-3.5-turbo",
		},
		{
			name:     "fallback enabled no match",
			enabled:  true,
			models:   map[string]string{"gpt-4": "gpt-3.5-turbo"},
			input:    "claude-3",
			expected: "",
		},
		{
			name:     "fallback disabled",
			enabled:  false,
			models:   map[string]string{"gpt-4": "gpt-3.5-turbo"},
			input:    "gpt-4",
			expected: "",
		},
		{
			name:     "fallback to empty string",
			enabled:  true,
			models:   map[string]string{"gpt-4": ""},
			input:    "gpt-4",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Fallback: ModelFallbackConfig{
					Enabled: tt.enabled,
					Models:  tt.models,
				},
			}
			got := cfg.GetFallbackModel(tt.input)
			if got != tt.expected {
				t.Errorf("GetFallbackModel(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestModelFallbackConfig_Normalize(t *testing.T) {
	cfg := ModelFallbackConfig{}
	cfg.normalize()

	if cfg.Models == nil {
		t.Error("normalize() should initialize Models map")
	}

	// Test that existing map is preserved
	existing := ModelFallbackConfig{
		Models: map[string]string{"a": "b"},
	}
	existing.normalize()
	if existing.Models["a"] != "b" {
		t.Error("normalize() should preserve existing map entries")
	}
}

func TestApplyDefaultProxyPolicy(t *testing.T) {
	// Test nil policy
	applyDefaultProxyPolicy(nil) // should not panic

	// Test empty policy
	policy := &ProxyPolicyConfig{}
	applyDefaultProxyPolicy(policy)

	if policy.Retry.StatusCodeMin == nil {
		t.Error("StatusCodeMin should be set to default")
	} else if *policy.Retry.StatusCodeMin != 500 {
		t.Errorf("StatusCodeMin should be 500, got %d", *policy.Retry.StatusCodeMin)
	}

	if len(policy.Retry.StatusCodes) != 2 {
		t.Errorf("StatusCodes should have 2 entries, got %d", len(policy.Retry.StatusCodes))
	}

	if len(policy.Retry.MessageKeywords) == 0 {
		t.Error("MessageKeywords should have default values")
	}
}

func TestApplyDefaultProxyPolicy_Intercepts(t *testing.T) {
	policy := &ProxyPolicyConfig{
		Intercepts: []ResponseInterceptRule{
			{Name: "rule1", Action: ""},
			{Name: "rule2", Action: "retry"},
			{Name: "rule3", Action: "  "},
		},
	}
	applyDefaultProxyPolicy(policy)

	if policy.Intercepts[0].Action != "fail" {
		t.Errorf("empty action should default to 'fail', got %q", policy.Intercepts[0].Action)
	}
	if policy.Intercepts[1].Action != "retry" {
		t.Errorf("'retry' action should be preserved, got %q", policy.Intercepts[1].Action)
	}
	if policy.Intercepts[2].Action != "fail" {
		t.Errorf("whitespace action should default to 'fail', got %q", policy.Intercepts[2].Action)
	}
}

func TestDefaultRetryableKeywords(t *testing.T) {
	keywords := defaultRetryableKeywords()

	if len(keywords) == 0 {
		t.Error("defaultRetryableKeywords should return non-empty slice")
	}

	expectedKeywords := []string{
		"429",
		"too many requests",
		"rate limit",
		"quota exceeded",
	}

	for _, kw := range expectedKeywords {
		found := false
		for _, k := range keywords {
			if k == kw {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected keyword %q not found in default keywords", kw)
		}
	}
}

func TestValidateConfig_Security(t *testing.T) {
	tests := []struct {
		name      string
		admin     AdminConfig
		wantErr   bool
		errPrefix string
	}{
		{
			name:      "admin disabled no token needed",
			admin:     AdminConfig{Enabled: false, AuthToken: ""},
			wantErr:   false,
			errPrefix: "",
		},
		{
			name:      "admin enabled with default token",
			admin:     AdminConfig{Enabled: true, AuthToken: "change-me-admin-token"},
			wantErr:   true,
			errPrefix: "admin auth_token must be set to a secure value",
		},
		{
			name:      "admin enabled with empty token",
			admin:     AdminConfig{Enabled: true, AuthToken: ""},
			wantErr:   true,
			errPrefix: "admin auth_token must be set to a secure value",
		},
		{
			name:      "admin enabled with short token",
			admin:     AdminConfig{Enabled: true, AuthToken: "short"},
			wantErr:   true,
			errPrefix: "admin auth_token must be at least 32 characters",
		},
		{
			name:      "admin enabled with secure token",
			admin:     AdminConfig{Enabled: true, AuthToken: "this-is-a-secure-token-with-32-chars-min"},
			wantErr:   false,
			errPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Admin: tt.admin}
			err := ValidateConfig(cfg)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				} else if tt.errPrefix != "" && !strings.HasPrefix(err.Error(), tt.errPrefix) {
					t.Errorf("expected error starting with %q, got: %v", tt.errPrefix, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNormalize_AllDefaults(t *testing.T) {
	cfg := Config{}
	cfg.Normalize()

	// Check all default values are applied
	if cfg.Listen != ":8080" {
		t.Errorf("Listen default: expected :8080, got %s", cfg.Listen)
	}
	if cfg.Reload.DebounceMs != 200 {
		t.Errorf("Reload.DebounceMs default: expected 200, got %d", cfg.Reload.DebounceMs)
	}
	if cfg.Router.Strategy != RouterStrategyHealthWeightedRR {
		t.Errorf("Router.Strategy default: expected %s, got %s", RouterStrategyHealthWeightedRR, cfg.Router.Strategy)
	}
	if cfg.Router.MaxRetries != 2 {
		t.Errorf("Router.MaxRetries default: expected 2, got %d", cfg.Router.MaxRetries)
	}
	if cfg.Router.RetryBackoffMs != 3000 {
		t.Errorf("Router.RetryBackoffMs default: expected 3000, got %d", cfg.Router.RetryBackoffMs)
	}
	if cfg.Router.RetryBackoffMaxMs != 30000 {
		t.Errorf("Router.RetryBackoffMaxMs default: expected 30000, got %d", cfg.Router.RetryBackoffMaxMs)
	}
	if cfg.Router.FailureThreshold != 20 {
		t.Errorf("Router.FailureThreshold default: expected 20, got %d", cfg.Router.FailureThreshold)
	}
	if cfg.Router.CooldownSec != 60 {
		t.Errorf("Router.CooldownSec default: expected 60, got %d", cfg.Router.CooldownSec)
	}
	if cfg.Router.FailurePassthroughAfterSec != 600 {
		t.Errorf("Router.FailurePassthroughAfterSec default: expected 600, got %d", cfg.Router.FailurePassthroughAfterSec)
	}
	if cfg.Router.StickySessions.TTLSec != 1800 {
		t.Errorf("Router.StickySessions.TTLSec default: expected 1800, got %d", cfg.Router.StickySessions.TTLSec)
	}
	if cfg.Health.IntervalSec != 10 {
		t.Errorf("Health.IntervalSec default: expected 10, got %d", cfg.Health.IntervalSec)
	}
	if cfg.Health.TimeoutMs != 2000 {
		t.Errorf("Health.TimeoutMs default: expected 2000, got %d", cfg.Health.TimeoutMs)
	}
	if cfg.Health.Path != "/v1/models" {
		t.Errorf("Health.Path default: expected /v1/models, got %s", cfg.Health.Path)
	}
	if cfg.Admin.Language != AdminLanguageChinese {
		t.Errorf("Admin.Language default: expected %s, got %s", AdminLanguageChinese, cfg.Admin.Language)
	}
	if cfg.Telemetry.SQLitePath != "data/telemetry.db" {
		t.Errorf("Telemetry.SQLitePath default: expected data/telemetry.db, got %s", cfg.Telemetry.SQLitePath)
	}
	if cfg.Pricing.CachePath != "data/pricing-cache.json" {
		t.Errorf("Pricing.CachePath default: expected data/pricing-cache.json, got %s", cfg.Pricing.CachePath)
	}
	if cfg.Pricing.RefreshIntervalHours != 12 {
		t.Errorf("Pricing.RefreshIntervalHours default: expected 12, got %d", cfg.Pricing.RefreshIntervalHours)
	}
	if cfg.Pricing.RequestTimeoutMs != 15000 {
		t.Errorf("Pricing.RequestTimeoutMs default: expected 15000, got %d", cfg.Pricing.RequestTimeoutMs)
	}
}

func TestNormalize_UpstreamDefaults(t *testing.T) {
	cfg := Config{
		Upstreams: []Upstream{
			{Name: "test1", BaseURL: "https://example.com"},
			{Name: "test2", BaseURL: "https://example2.com", Weight: 5, TimeoutMs: 60000},
		},
	}
	cfg.Normalize()

	if cfg.Upstreams[0].Weight != 1 {
		t.Errorf("Upstream Weight default: expected 1, got %d", cfg.Upstreams[0].Weight)
	}
	if cfg.Upstreams[0].TimeoutMs != 30000 {
		t.Errorf("Upstream TimeoutMs default: expected 30000, got %d", cfg.Upstreams[0].TimeoutMs)
	}
	if cfg.Upstreams[1].Weight != 5 {
		t.Errorf("Upstream Weight should preserve custom value: expected 5, got %d", cfg.Upstreams[1].Weight)
	}
	if cfg.Upstreams[1].TimeoutMs != 60000 {
		t.Errorf("Upstream TimeoutMs should preserve custom value: expected 60000, got %d", cfg.Upstreams[1].TimeoutMs)
	}
}

func TestNormalize_NegativeStickySessionTTL(t *testing.T) {
	cfg := Config{
		Router: RouterConfig{
			StickySessions: StickySessionConfig{
				TTLSec: -1,
			},
		},
	}
	cfg.Normalize()

	if cfg.Router.StickySessions.TTLSec != 1800 {
		t.Errorf("Negative TTL should default to 1800, got %d", cfg.Router.StickySessions.TTLSec)
	}
}

func TestNormalize_ZeroStickySessionTTL(t *testing.T) {
	cfg := Config{
		Router: RouterConfig{
			StickySessions: StickySessionConfig{
				TTLSec: 0,
			},
		},
	}
	cfg.Normalize()

	if cfg.Router.StickySessions.TTLSec != 1800 {
		t.Errorf("Zero TTL should default to 1800, got %d", cfg.Router.StickySessions.TTLSec)
	}
}

func TestNormalize_ProviderClassNormalization(t *testing.T) {
	cfg := Config{
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com", ProviderClass: "FREE"},
		},
	}
	cfg.Normalize()

	if cfg.Upstreams[0].ProviderClass != UpstreamClassFree {
		t.Errorf("ProviderClass should be normalized: expected %s, got %s", UpstreamClassFree, cfg.Upstreams[0].ProviderClass)
	}
}
