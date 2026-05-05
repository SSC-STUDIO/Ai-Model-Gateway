package compiler

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
)

type stubRevisionConfigSource struct {
	configs map[string]*core.Config
	err     error
}

func (s *stubRevisionConfigSource) LoadRevisionConfig(revisionID string) (*core.Config, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.configs[revisionID], nil
}

func TestCompileFromConfig_CompilesNormalizedSnapshot(t *testing.T) {
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:        " primary ",
				BaseURL:     " https://api.example.com/v1 ",
				APIKey:      " sk-test ",
				Models:      []string{"gpt-4o", " ", "gpt-4o", "gpt-4.1"},
				SameRetries: 1,
				Headers: map[string]string{
					"X-Test": "1",
					"   ":    "ignored",
				},
			},
		},
	}

	comp := NewCompiler()
	snap, err := comp.CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	if !strings.HasPrefix(snap.Meta.SnapshotID, "snap_") {
		t.Fatalf("expected generated snapshot id, got %q", snap.Meta.SnapshotID)
	}
	if snap.Meta.SchemaVersion != snapshot.CurrentSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", snapshot.CurrentSchemaVersion, snap.Meta.SchemaVersion)
	}
	if snap.Meta.RevisionID != "" {
		t.Fatalf("expected empty revision id, got %q", snap.Meta.RevisionID)
	}
	if snap.Meta.CompilerVersion != compilerVersion {
		t.Fatalf("expected compiler version %q, got %q", compilerVersion, snap.Meta.CompilerVersion)
	}
	if snap.Meta.GeneratedAt.IsZero() {
		t.Fatal("expected generated_at to be set")
	}

	if snap.Ingress.Listen != ":18080" {
		t.Fatalf("expected default listen :18080, got %q", snap.Ingress.Listen)
	}
	if snap.Ingress.ReadTimeoutMs != 30000 {
		t.Fatalf("expected default read timeout 30000, got %d", snap.Ingress.ReadTimeoutMs)
	}
	if snap.Ingress.WriteTimeoutMs != 0 {
		t.Fatalf("expected unresolved write timeout default 0, got %d", snap.Ingress.WriteTimeoutMs)
	}
	if snap.Ingress.IdleTimeoutMs != 120000 {
		t.Fatalf("expected default idle timeout 120000, got %d", snap.Ingress.IdleTimeoutMs)
	}
	if snap.Ingress.MaxBodyBytes != 100<<20 {
		t.Fatalf("expected default max body 100MB, got %d", snap.Ingress.MaxBodyBytes)
	}

	if snap.Contract.PublicAPI != publicAPIOpenAIChatCompletions {
		t.Fatalf("expected public api %q, got %q", publicAPIOpenAIChatCompletions, snap.Contract.PublicAPI)
	}
	if !reflect.DeepEqual(snap.Contract.EnabledRoutes, gatewayEnabledRoutes) {
		t.Fatalf("unexpected enabled routes: %#v", snap.Contract.EnabledRoutes)
	}

	if len(snap.Providers) != 1 {
		t.Fatalf("expected 1 compiled provider, got %d", len(snap.Providers))
	}
	provider := snap.Providers[0]
	if provider.ProviderID != "primary" {
		t.Fatalf("expected trimmed provider id %q, got %q", "primary", provider.ProviderID)
	}
	if provider.ProtocolAdapter != publicAPIOpenAIChatCompletions {
		t.Fatalf("expected protocol adapter %q, got %q", publicAPIOpenAIChatCompletions, provider.ProtocolAdapter)
	}
	if provider.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected trimmed base URL, got %q", provider.BaseURL)
	}
	if provider.Credentials.Kind != credentialKindBearer {
		t.Fatalf("expected bearer credentials, got %q", provider.Credentials.Kind)
	}
	if provider.Credentials.Value != "sk-test" {
		t.Fatalf("expected trimmed api key, got %q", provider.Credentials.Value)
	}
	if !reflect.DeepEqual(provider.Headers, map[string]string{"X-Test": "1"}) {
		t.Fatalf("unexpected provider headers: %#v", provider.Headers)
	}
	if !reflect.DeepEqual(provider.ModelTable, []snapshot.ModelMapping{
		{PublicModel: "gpt-4o", UpstreamModel: "gpt-4o"},
		{PublicModel: "gpt-4.1", UpstreamModel: "gpt-4.1"},
	}) {
		t.Fatalf("unexpected model table: %#v", provider.ModelTable)
	}
	if !provider.CapabilityTable.SupportsChatCompletions || !provider.CapabilityTable.SupportsStreaming {
		t.Fatalf("expected chat/streaming capabilities, got %#v", provider.CapabilityTable)
	}
	if provider.CapabilityTable.UsageAccounting != usageAccountingOpenAI {
		t.Fatalf("expected usage accounting %q, got %q", usageAccountingOpenAI, provider.CapabilityTable.UsageAccounting)
	}
	if provider.CapabilityTable.ErrorClassifier != errorClassifierOpenAI {
		t.Fatalf("expected error classifier %q, got %q", errorClassifierOpenAI, provider.CapabilityTable.ErrorClassifier)
	}
	if !provider.ExecutionPolicy.Enabled {
		t.Fatal("expected compiled provider to be enabled")
	}
	if provider.ExecutionPolicy.Weight != 1 {
		t.Fatalf("expected normalized provider weight 1, got %d", provider.ExecutionPolicy.Weight)
	}
	if provider.ExecutionPolicy.TimeoutMs != 30000 {
		t.Fatalf("expected normalized provider timeout 30000, got %d", provider.ExecutionPolicy.TimeoutMs)
	}
	if provider.ExecutionPolicy.SameRetries != 1 {
		t.Fatalf("expected same retries 1, got %d", provider.ExecutionPolicy.SameRetries)
	}
	if provider.ExecutionPolicy.ProviderClass != string(core.ProviderClassQuotaLimited) {
		t.Fatalf("expected provider class %q, got %q", core.ProviderClassQuotaLimited, provider.ExecutionPolicy.ProviderClass)
	}

	if snap.RoutingPolicy.MaxRetries != 2 {
		t.Fatalf("expected default max retries 2, got %d", snap.RoutingPolicy.MaxRetries)
	}
	if snap.RoutingPolicy.Strategy != core.StrategyHealthWeightedRR {
		t.Fatalf("expected default routing strategy %q, got %q", core.StrategyHealthWeightedRR, snap.RoutingPolicy.Strategy)
	}
	if snap.RoutingPolicy.Health.Enabled {
		t.Fatalf("expected health checks disabled by default, got %#v", snap.RoutingPolicy.Health)
	}
	if snap.RoutingPolicy.Health.IntervalSec != 10 {
		t.Fatalf("expected default health interval 10, got %d", snap.RoutingPolicy.Health.IntervalSec)
	}
	if snap.RoutingPolicy.Health.TimeoutMs != 2000 {
		t.Fatalf("expected default health timeout 2000, got %d", snap.RoutingPolicy.Health.TimeoutMs)
	}
	if snap.RoutingPolicy.Health.Path != "/v1/models" {
		t.Fatalf("expected default health path /v1/models, got %q", snap.RoutingPolicy.Health.Path)
	}
	if snap.RoutingPolicy.StickySessions.Enabled {
		t.Fatalf("expected sticky sessions disabled by default, got %#v", snap.RoutingPolicy.StickySessions)
	}
	if snap.RoutingPolicy.StickySessions.TTLSec != 1800 {
		t.Fatalf("expected default sticky session ttl 1800, got %d", snap.RoutingPolicy.StickySessions.TTLSec)
	}
	if snap.RoutingPolicy.FailurePolicy.Threshold != 20 {
		t.Fatalf("expected default failure threshold 20, got %d", snap.RoutingPolicy.FailurePolicy.Threshold)
	}
	if snap.RoutingPolicy.FailurePolicy.CooldownSec != 60 {
		t.Fatalf("expected default cooldown 60, got %d", snap.RoutingPolicy.FailurePolicy.CooldownSec)
	}
	if snap.RoutingPolicy.FailurePolicy.PassthroughAfterSec != 600 {
		t.Fatalf("expected default passthrough_after_sec 600, got %d", snap.RoutingPolicy.FailurePolicy.PassthroughAfterSec)
	}
	if snap.RoutingPolicy.FailurePolicy.QuotaRecoveryIntervalMin != 60 {
		t.Fatalf("expected runtime default quota recovery 60, got %d", snap.RoutingPolicy.FailurePolicy.QuotaRecoveryIntervalMin)
	}
	if !reflect.DeepEqual(snap.RoutingPolicy.Retry.StatusCodes, []int{408, 429}) {
		t.Fatalf("unexpected retry status codes: %#v", snap.RoutingPolicy.Retry.StatusCodes)
	}
	if snap.RoutingPolicy.Retry.StatusCodeMin != 500 {
		t.Fatalf("expected retry status code min 500, got %d", snap.RoutingPolicy.Retry.StatusCodeMin)
	}
	if !reflect.DeepEqual(snap.RoutingPolicy.Retry.MessageKeywords, core.DefaultRetryKeywords()) {
		t.Fatalf("unexpected retry keywords: %#v", snap.RoutingPolicy.Retry.MessageKeywords)
	}

	if snap.TelemetryEmit.Channel != telemetryChannel {
		t.Fatalf("expected telemetry channel %q, got %q", telemetryChannel, snap.TelemetryEmit.Channel)
	}
	if snap.TelemetryEmit.Batching.MaxBatchSize != 256 {
		t.Fatalf("expected telemetry batch size 256, got %d", snap.TelemetryEmit.Batching.MaxBatchSize)
	}
	if snap.TelemetryEmit.Batching.FlushIntervalMs != 100 {
		t.Fatalf("expected telemetry flush interval 100, got %d", snap.TelemetryEmit.Batching.FlushIntervalMs)
	}

	if cfg.Providers[0].Weight != 0 {
		t.Fatalf("expected input config to remain unnormalized, got weight %d", cfg.Providers[0].Weight)
	}
	if cfg.Routing.Retry.StatusCodeMin != nil {
		t.Fatalf("expected input config retry status code min to remain nil, got %v", *cfg.Routing.Retry.StatusCodeMin)
	}
}

func TestCompileFromConfig_SkipsDisabledProviders(t *testing.T) {
	disabled := false
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:    "disabled",
				BaseURL: "https://disabled.example.com/v1",
				Models:  []string{"gpt-4o"},
				Enabled: &disabled,
			},
			{
				Name:    "enabled",
				BaseURL: "https://enabled.example.com/v1",
				Models:  []string{"gpt-4o"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}
	if len(snap.Providers) != 1 {
		t.Fatalf("expected only enabled providers to compile, got %d", len(snap.Providers))
	}
	if snap.Providers[0].ProviderID != "enabled" {
		t.Fatalf("expected enabled provider to remain, got %q", snap.Providers[0].ProviderID)
	}
}

func TestCompileFromConfig_PreservesConfiguredRoutingStrategy(t *testing.T) {
	cfg := core.Config{
		Routing: core.RoutingConfig{
			Strategy: core.StrategyRoundRobin,
			StickySessions: core.StickySessionConfig{
				Enabled: true,
				TTLSec:  900,
			},
			FailurePolicy: core.FailurePolicyConfig{
				PassthroughAfterSec: 45,
			},
		},
		Providers: []core.Provider{
			{
				Name:    "primary",
				BaseURL: "https://example.com/v1",
				Models:  []string{"gpt-4o"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}
	if snap.RoutingPolicy.Strategy != core.StrategyRoundRobin {
		t.Fatalf("routing strategy = %q, want %q", snap.RoutingPolicy.Strategy, core.StrategyRoundRobin)
	}
	if !snap.RoutingPolicy.StickySessions.Enabled || snap.RoutingPolicy.StickySessions.TTLSec != 900 {
		t.Fatalf("sticky sessions = %#v, want enabled ttl 900", snap.RoutingPolicy.StickySessions)
	}
	if snap.RoutingPolicy.FailurePolicy.PassthroughAfterSec != 45 {
		t.Fatalf("passthrough_after_sec = %d, want 45", snap.RoutingPolicy.FailurePolicy.PassthroughAfterSec)
	}
}

func TestCompileFromConfig_PreservesDisableCooldownAndInfiniteRetry(t *testing.T) {
	cfg := core.Config{
		Routing: core.RoutingConfig{
			FailurePolicy: core.FailurePolicyConfig{
				DisableCooldown:          true,
				CooldownSec:              0,
				QuotaRecoveryIntervalMin: 0,
			},
			Retry: core.RetryPolicyConfig{
				InfiniteOnError: true,
				AllErrors:       true,
			},
		},
		Providers: []core.Provider{
			{
				Name:    "primary",
				BaseURL: "https://example.com/v1",
				Models:  []string{"gpt-4o"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}
	if !snap.RoutingPolicy.FailurePolicy.DisableCooldown {
		t.Fatal("expected disable_cooldown to compile through to snapshot")
	}
	if snap.RoutingPolicy.FailurePolicy.QuotaRecoveryIntervalMin != 0 {
		t.Fatalf("expected quota recovery interval to stay disabled, got %d", snap.RoutingPolicy.FailurePolicy.QuotaRecoveryIntervalMin)
	}
	if !snap.RoutingPolicy.Retry.InfiniteOnError {
		t.Fatal("expected infinite_on_error to compile through to snapshot")
	}
	if !snap.RoutingPolicy.Retry.AllErrors {
		t.Fatal("expected all_errors to compile through to snapshot")
	}
}

func TestCompileFromConfig_RejectsEnabledProviderWithoutModels(t *testing.T) {
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:    "broken",
				BaseURL: "https://example.com/v1",
			},
		},
	}

	_, err := NewCompiler().CompileFromConfig(cfg)
	if err == nil {
		t.Fatal("expected compile error for provider without models")
	}
	if !strings.Contains(err.Error(), "providers[0].models must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileFromConfig_RejectsWhenNoProvidersCompile(t *testing.T) {
	disabled := false
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:    "disabled",
				BaseURL: "https://example.com/v1",
				Models:  []string{"gpt-4o"},
				Enabled: &disabled,
			},
		},
	}

	_, err := NewCompiler().CompileFromConfig(cfg)
	if err == nil {
		t.Fatal("expected compile error when no enabled providers remain")
	}
	if !strings.Contains(err.Error(), "at least one provider is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileFromConfig_RejectsUnsupportedConfigType(t *testing.T) {
	_, err := NewCompiler().CompileFromConfig("not-a-config")
	if err == nil {
		t.Fatal("expected type error")
	}
	if !strings.Contains(err.Error(), "unsupported config type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_RequiresRevisionConfigSource(t *testing.T) {
	snap, err := NewCompiler().Compile("rev-123")
	if err == nil {
		t.Fatal("expected compile error when no revision config source is configured")
	}
	if snap != nil {
		t.Fatalf("expected no snapshot, got %#v", snap)
	}
	if !strings.Contains(err.Error(), "revision config source is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompile_LoadsRevisionConfigFromSource(t *testing.T) {
	comp := NewCompiler()
	comp.SetRevisionConfigSource(&stubRevisionConfigSource{
		configs: map[string]*core.Config{
			"rev_123": {
				Server: core.ServerConfig{
					Listen: "127.0.0.1:18080",
				},
				Providers: []core.Provider{
					{
						Name:    "demo",
						BaseURL: "https://example.com/v1",
						Models:  []string{"gpt-4o-mini"},
					},
				},
			},
		},
	})

	snap, err := comp.Compile("rev_123")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if snap == nil {
		t.Fatal("expected compiled snapshot")
	}
	if snap.Meta.RevisionID != "rev_123" {
		t.Fatalf("snapshot.Meta.RevisionID = %q, want %q", snap.Meta.RevisionID, "rev_123")
	}
	if snap.Ingress.Listen != "127.0.0.1:18080" {
		t.Fatalf("snapshot.Ingress.Listen = %q, want %q", snap.Ingress.Listen, "127.0.0.1:18080")
	}
}

func TestCompile_PropagatesRevisionSourceErrors(t *testing.T) {
	comp := NewCompiler()
	comp.SetRevisionConfigSource(&stubRevisionConfigSource{
		err: errors.New("state store unavailable"),
	})

	snap, err := comp.Compile("rev_123")
	if err == nil {
		t.Fatal("expected compile error when revision source fails")
	}
	if snap != nil {
		t.Fatalf("expected no snapshot, got %#v", snap)
	}
	if !strings.Contains(err.Error(), "state store unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileCredentials_AnthropicBaseURL(t *testing.T) {
	tests := []struct {
		name           string
		provider       core.Provider
		expectedKind   string
		expectedHeader string
		expectedValue  string
	}{
		{
			name: "bearer token when no AnthropicBaseURL",
			provider: core.Provider{
				Name:    "test",
				BaseURL: "https://api.example.com",
				APIKey:  "sk-test",
			},
			expectedKind:  "bearer",
			expectedValue: "sk-test",
		},
		{
			name: "api_key with x-api-key header when AnthropicBaseURL set",
			provider: core.Provider{
				Name:             "test",
				BaseURL:          "https://api.example.com",
				AnthropicBaseURL: "https://api.anthropic.com",
				APIKey:           "sk-ant-test",
			},
			expectedKind:   "api_key",
			expectedHeader: "x-api-key",
			expectedValue:  "sk-ant-test",
		},
		{
			name: "api_key with x-api-key header when anthropic protocol adapter is explicit",
			provider: core.Provider{
				Name:            "test",
				BaseURL:         "https://api.example.com",
				ProtocolAdapter: core.ProtocolAdapterAnthropicMessages,
				APIKey:          "sk-ant-explicit",
			},
			expectedKind:   "api_key",
			expectedHeader: "x-api-key",
			expectedValue:  "sk-ant-explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := compileCredentials(tt.provider)
			if creds.Kind != tt.expectedKind {
				t.Errorf("expected kind %s, got %s", tt.expectedKind, creds.Kind)
			}
			if creds.Value != tt.expectedValue {
				t.Errorf("expected value %s, got %s", tt.expectedValue, creds.Value)
			}
			if tt.expectedHeader != "" && creds.HeaderName != tt.expectedHeader {
				t.Errorf("expected header %s, got %s", tt.expectedHeader, creds.HeaderName)
			}
		})
	}
}

func TestCompileProvider_AnthropicBaseURLValidation(t *testing.T) {
	tests := []struct {
		name        string
		provider    core.Provider
		expectError bool
	}{
		{
			name: "valid AnthropicBaseURL",
			provider: core.Provider{
				Name:             "test",
				BaseURL:          "https://api.example.com",
				AnthropicBaseURL: "https://api.anthropic.com",
				Models:           []string{"claude-3"},
			},
			expectError: false,
		},
		{
			name: "invalid AnthropicBaseURL",
			provider: core.Provider{
				Name:             "test",
				BaseURL:          "https://api.example.com",
				AnthropicBaseURL: "not-a-valid-url",
				Models:           []string{"claude-3"},
			},
			expectError: true,
		},
		{
			name: "empty AnthropicBaseURL is valid",
			provider: core.Provider{
				Name:             "test",
				BaseURL:          "https://api.example.com",
				AnthropicBaseURL: "",
				Models:           []string{"claude-3"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileProvider(tt.provider, 0)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCompileProvider_AnthropicProtocolAdapterCapabilities(t *testing.T) {
	provider, err := compileProvider(core.Provider{
		Name:            "anthropic",
		BaseURL:         "https://api.example.com",
		ProtocolAdapter: core.ProtocolAdapterAnthropicMessages,
		APIKey:          "sk-ant",
		Models:          []string{"claude-3-7-sonnet"},
	}, 0)
	if err != nil {
		t.Fatalf("compileProvider() error = %v", err)
	}

	if provider.ProtocolAdapter != core.ProtocolAdapterAnthropicMessages {
		t.Fatalf("protocol_adapter = %q, want %q", provider.ProtocolAdapter, core.ProtocolAdapterAnthropicMessages)
	}
	if provider.Credentials.Kind != credentialKindAPIKey || provider.Credentials.HeaderName != "x-api-key" {
		t.Fatalf("unexpected anthropic credentials: %+v", provider.Credentials)
	}
	if provider.CapabilityTable.UsageAccounting != usageAccountingAnthropic {
		t.Fatalf("usage_accounting = %q, want %q", provider.CapabilityTable.UsageAccounting, usageAccountingAnthropic)
	}
	if provider.CapabilityTable.ErrorClassifier != errorClassifierAnthropic {
		t.Fatalf("error_classifier = %q, want %q", provider.CapabilityTable.ErrorClassifier, errorClassifierAnthropic)
	}
}
