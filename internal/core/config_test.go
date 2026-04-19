package core

import (
	"strings"
	"testing"
)

func TestConfig_Normalize_Defaults(t *testing.T) {
	cfg := Config{}
	cfg.Normalize()

	if cfg.Server.Listen != ":18080" {
		t.Errorf("expected listen :18080, got %s", cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeoutMs != 30000 {
		t.Errorf("expected read_timeout_ms 30000, got %d", cfg.Server.ReadTimeoutMs)
	}
	if cfg.Server.WriteTimeoutMs != 0 {
		t.Errorf("expected write_timeout_ms 0, got %d", cfg.Server.WriteTimeoutMs)
	}
	if cfg.Server.MaxBodyBytes != 100<<20 {
		t.Errorf("expected max_body_bytes 100MB, got %d", cfg.Server.MaxBodyBytes)
	}
	if cfg.Routing.Strategy != StrategyHealthWeightedRR {
		t.Errorf("expected strategy %s, got %s", StrategyHealthWeightedRR, cfg.Routing.Strategy)
	}
	if cfg.Routing.MaxRetries != 2 {
		t.Errorf("expected max_retries 2, got %d", cfg.Routing.MaxRetries)
	}
	if cfg.Routing.RetryBackoff.InitialMs != 3000 {
		t.Errorf("expected retry_backoff.initial_ms 3000, got %d", cfg.Routing.RetryBackoff.InitialMs)
	}
	if cfg.Routing.Health.Path != "/v1/models" {
		t.Errorf("expected health.path /v1/models, got %s", cfg.Routing.Health.Path)
	}
	if cfg.Routing.StickySessions.TTLSec != 1800 {
		t.Errorf("expected sticky_sessions.ttl_sec 1800, got %d", cfg.Routing.StickySessions.TTLSec)
	}
	if cfg.Telemetry.SQLitePath != "data/telemetry.db" {
		t.Errorf("expected sqlite_path data/telemetry.db, got %s", cfg.Telemetry.SQLitePath)
	}
	if cfg.Telemetry.RetentionDays != 30 {
		t.Errorf("expected retention_days 30, got %d", cfg.Telemetry.RetentionDays)
	}
	if cfg.Pricing.CachePath != "data/pricing-cache.json" {
		t.Errorf("expected pricing.cache_path data/pricing-cache.json, got %s", cfg.Pricing.CachePath)
	}
	if cfg.Pricing.RefreshIntervalHours != 12 {
		t.Errorf("expected pricing.refresh_interval_hours 12, got %d", cfg.Pricing.RefreshIntervalHours)
	}
	if cfg.Pricing.RequestTimeoutMs != 15000 {
		t.Errorf("expected pricing.request_timeout_ms 15000, got %d", cfg.Pricing.RequestTimeoutMs)
	}
	if len(cfg.Pricing.ManualPrices) != 0 {
		t.Errorf("expected no default manual prices, got %d", len(cfg.Pricing.ManualPrices))
	}
	if cfg.Admin.Language != LangZH {
		t.Errorf("expected language zh, got %s", cfg.Admin.Language)
	}
	if cfg.Admin.PublishHistoryLimit != DefaultAdminPublishHistoryLimit {
		t.Errorf("expected publish_history_limit %d, got %d", DefaultAdminPublishHistoryLimit, cfg.Admin.PublishHistoryLimit)
	}
}

func TestConfig_Normalize_PreservesExplicitValues(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{Listen: ":9090", ReadTimeoutMs: 5000},
		Routing: RoutingConfig{
			Strategy:   StrategyRoundRobin,
			MaxRetries: 5,
		},
		Admin: AdminConfig{Language: "en", PublishHistoryLimit: 64},
	}
	cfg.Normalize()

	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Server.Listen)
	}
	if cfg.Server.ReadTimeoutMs != 5000 {
		t.Errorf("expected read_timeout_ms 5000, got %d", cfg.Server.ReadTimeoutMs)
	}
	if cfg.Routing.Strategy != StrategyRoundRobin {
		t.Errorf("expected strategy %s, got %s", StrategyRoundRobin, cfg.Routing.Strategy)
	}
	if cfg.Routing.MaxRetries != 5 {
		t.Errorf("expected max_retries 5, got %d", cfg.Routing.MaxRetries)
	}
	if cfg.Admin.Language != LangEN {
		t.Errorf("expected language en, got %s", cfg.Admin.Language)
	}
	if cfg.Admin.PublishHistoryLimit != 64 {
		t.Errorf("expected publish_history_limit 64, got %d", cfg.Admin.PublishHistoryLimit)
	}
}

func TestConfig_Validate_RequiresProviders(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty providers")
	}
}

func TestConfig_Validate_RequiresProviderName(t *testing.T) {
	cfg := Config{
		Providers: []Provider{{BaseURL: "https://example.com"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty provider name")
	}
}

func TestConfig_Validate_RequiresProviderBaseURL(t *testing.T) {
	cfg := Config{
		Providers: []Provider{{Name: "test"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty provider base_url")
	}
}

func TestConfig_Validate_AdminRequiresTokens(t *testing.T) {
	cfg := Config{
		Admin: AdminConfig{Enabled: true},
		Providers: []Provider{{
			Name:    "test",
			BaseURL: "https://example.com",
		}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing admin bootstrap_token")
	}

	cfg.Admin.BootstrapToken = "short"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for short bootstrap_token")
	}

	cfg.Admin.BootstrapToken = strings.Repeat("a", 34)
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing cookie_signing_key")
	}

	cfg.Admin.CookieSigningKey = strings.Repeat("b", 34)
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	cfg.Admin.PublishHistoryLimit = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "admin.publish_history_limit must be >= 0") {
		t.Fatalf("expected publish_history_limit validation error, got %v", err)
	}
}

func TestConfig_Validate_ValidMinimal(t *testing.T) {
	cfg := Config{
		Providers: []Provider{{
			Name:    "test",
			BaseURL: "https://example.com",
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for valid minimal config, got %v", err)
	}
}

func TestConfig_Validate_PricingMustBeNonNegative(t *testing.T) {
	base := Config{
		Providers: []Provider{{
			Name:    "test",
			BaseURL: "https://example.com",
		}},
	}

	cfg := base
	cfg.Pricing.RefreshIntervalHours = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pricing.refresh_interval_hours must be >= 0") {
		t.Fatalf("expected refresh_interval_hours validation error, got %v", err)
	}

	cfg = base
	cfg.Pricing.RequestTimeoutMs = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pricing.request_timeout_ms must be >= 0") {
		t.Fatalf("expected request_timeout_ms validation error, got %v", err)
	}

	cfg = base
	cfg.Pricing.ManualPrices = []PricingManualPrice{{
		Model:      "glm-5.1",
		InputPer1M: -1,
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pricing.manual_prices[0].input_per_1m must be >= 0") {
		t.Fatalf("expected manual input_per_1m validation error, got %v", err)
	}

	cfg = base
	cfg.Pricing.ManualPrices = []PricingManualPrice{{
		Currency: "cny",
	}}
	cfg.Normalize()
	if cfg.Pricing.ManualPrices[0].Currency != "CNY" {
		t.Fatalf("expected manual price currency to normalize to CNY, got %q", cfg.Pricing.ManualPrices[0].Currency)
	}
	if cfg.Pricing.ManualPrices[0].Source != "manual" {
		t.Fatalf("expected manual price source default manual, got %q", cfg.Pricing.ManualPrices[0].Source)
	}
	if !cfg.Pricing.ManualPrices[0].IsEnabled() {
		t.Fatalf("expected manual price enabled by default")
	}

	cfg = base
	cfg.Pricing.ManualPrices = []PricingManualPrice{{
		Model:       "",
		InputPer1M:  1,
		OutputPer1M: 2,
	}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "pricing.manual_prices[0].model must not be empty") {
		t.Fatalf("expected manual model validation error, got %v", err)
	}
}

func TestProvider_IsEnabled(t *testing.T) {
	p := Provider{}
	if !p.IsEnabled() {
		t.Error("expected nil Enabled to default to true")
	}

	f := false
	p.Enabled = &f
	if p.IsEnabled() {
		t.Error("expected Enabled=false to return false")
	}
}

func TestProvider_Normalize(t *testing.T) {
	p := Provider{}
	p.normalize()

	if p.Weight != 1 {
		t.Errorf("expected weight 1, got %d", p.Weight)
	}
	if p.TimeoutMs != 30000 {
		t.Errorf("expected timeout_ms 30000, got %d", p.TimeoutMs)
	}
	if p.ProviderClass != ProviderClassQuotaLimited {
		t.Errorf("expected provider_class quota_limited, got %s", p.ProviderClass)
	}
}

func TestNormalizeProviderClass(t *testing.T) {
	tests := []struct {
		in   string
		want ProviderClass
	}{
		{"free", ProviderClassFree},
		{"Free", ProviderClassFree},
		{"gratis", ProviderClassFree},
		{"public", ProviderClassFree},
		{"quota_limited", ProviderClassQuotaLimited},
		{"paid", ProviderClassQuotaLimited},
		{"", ProviderClassQuotaLimited},
		{"unknown", ProviderClassQuotaLimited},
	}
	for _, tt := range tests {
		got := NormalizeProviderClass(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeProviderClass(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestInterceptRule_IsEnabled(t *testing.T) {
	r := InterceptRule{}
	if !r.IsEnabled() {
		t.Error("expected nil Enabled to default to true")
	}

	f := false
	r.Enabled = &f
	if r.IsEnabled() {
		t.Error("expected Enabled=false to return false")
	}
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"en", LangEN},
		{"EN", LangEN},
		{" ja ", LangJA},
		{"ko", LangKO},
		{"zh", LangZH},
		{"", LangZH},
		{"unknown", LangZH},
	}
	for _, tt := range tests {
		got := normalizeLanguage(tt.in)
		if got != tt.want {
			t.Errorf("normalizeLanguage(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}
