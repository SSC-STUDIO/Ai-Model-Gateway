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
	if provider.UpstreamID != "https://api.example.com/v1" {
		t.Fatalf("expected URL-derived upstream id, got %q", provider.UpstreamID)
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

func TestCompileFromConfig_GroupsLogicalUpstreamByEffectiveBaseURL(t *testing.T) {
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:             "provider-key-a",
				BaseURL:          "https://shared.example.com/v1/",
				AnthropicBaseURL: "https://Shared.Example.com/v1/",
				APIKey:           "sk-a",
				Models:           []string{"claude-sonnet"},
			},
			{
				Name:             "provider-key-b",
				BaseURL:          "https://other.example.com/v1",
				AnthropicBaseURL: "https://shared.example.com/v1",
				APIKey:           "sk-b",
				Models:           []string{"claude-sonnet"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}
	if len(snap.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(snap.Providers))
	}
	if snap.Providers[0].ProviderID == snap.Providers[1].ProviderID {
		t.Fatalf("expected distinct provider ids, got %q", snap.Providers[0].ProviderID)
	}
	if snap.Providers[0].UpstreamID != "https://shared.example.com/v1" {
		t.Fatalf("first upstream id = %q", snap.Providers[0].UpstreamID)
	}
	if snap.Providers[1].UpstreamID != snap.Providers[0].UpstreamID {
		t.Fatalf("expected shared upstream id, got %q and %q", snap.Providers[0].UpstreamID, snap.Providers[1].UpstreamID)
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

func TestCloneStringMap(t *testing.T) {
	t.Run("non-empty map", func(t *testing.T) {
		src := map[string]string{"a": "1", "b": "2"}
		dst := cloneStringMap(src)
		if !reflect.DeepEqual(dst, src) {
			t.Fatalf("cloneStringMap result = %v, want %v", dst, src)
		}
		// Verify mutation independence
		dst["a"] = "changed"
		if src["a"] != "1" {
			t.Fatalf("mutation of clone affected source")
		}
	})

	t.Run("empty map", func(t *testing.T) {
		dst := cloneStringMap(map[string]string{})
		if dst == nil {
			t.Fatal("expected non-nil empty map")
		}
		if len(dst) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(dst))
		}
	})

	t.Run("nil map", func(t *testing.T) {
		dst := cloneStringMap(nil)
		if dst == nil {
			t.Fatal("expected non-nil map from nil input")
		}
		if len(dst) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(dst))
		}
	})
}

func TestCompileCompatPolicy(t *testing.T) {
	t.Run("with bridge rules and excluded user agents", func(t *testing.T) {
		cfg := core.CompatConfig{
			Bridge: core.BridgeConfig{
				Enabled:           true,
				ExcludeUserAgents: []string{"Bot/1.0", "Crawler"},
				Rules: []core.BridgeRule{
					{From: "gpt-4", To: "gpt-4o"},
					{From: "   ", To: "gpt-4o"},   // empty From, skipped
					{From: "claude-3", To: "   "}, // empty To, skipped
					{From: "llama", To: "llama-3"},
				},
			},
		}

		policy := compileCompatPolicy(cfg)
		if !policy.Bridge.Enabled {
			t.Fatal("expected bridge enabled")
		}
		if len(policy.Bridge.Rules) != 2 {
			t.Fatalf("expected 2 valid rules (empty from/to skipped), got %d", len(policy.Bridge.Rules))
		}
		if policy.Bridge.Rules[0].From != "gpt-4" || policy.Bridge.Rules[0].To != "gpt-4o" {
			t.Fatalf("rule[0] = {from: %q, to: %q}", policy.Bridge.Rules[0].From, policy.Bridge.Rules[0].To)
		}
		if policy.Bridge.Rules[1].From != "llama" || policy.Bridge.Rules[1].To != "llama-3" {
			t.Fatalf("rule[1] = {from: %q, to: %q}", policy.Bridge.Rules[1].From, policy.Bridge.Rules[1].To)
		}
		if !reflect.DeepEqual(policy.Bridge.ExcludeUserAgents, []string{"Bot/1.0", "Crawler"}) {
			t.Fatalf("exclude user agents = %v", policy.Bridge.ExcludeUserAgents)
		}
	})

	t.Run("empty bridge", func(t *testing.T) {
		policy := compileCompatPolicy(core.CompatConfig{})
		if policy.Bridge.Enabled {
			t.Fatal("expected bridge disabled")
		}
		if len(policy.Bridge.Rules) != 0 {
			t.Fatalf("expected 0 rules, got %d", len(policy.Bridge.Rules))
		}
		if len(policy.Bridge.ExcludeUserAgents) != 0 {
			t.Fatalf("expected empty ExcludeUserAgents slice, got %v", policy.Bridge.ExcludeUserAgents)
		}
	})
}

func TestValidate_MissingProviderFields(t *testing.T) {
	comp := NewCompiler()

	t.Run("missing provider_id", func(t *testing.T) {
		snap := validSnapshot()
		snap.Providers[0].ProviderID = ""
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "provider_id is required") {
			t.Fatalf("expected provider_id error, got %v", err)
		}
	})

	t.Run("missing base_url", func(t *testing.T) {
		snap := validSnapshot()
		snap.Providers[0].BaseURL = ""
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "base_url is required") {
			t.Fatalf("expected base_url error, got %v", err)
		}
	})

	t.Run("missing model_table", func(t *testing.T) {
		snap := validSnapshot()
		snap.Providers[0].ModelTable = nil
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "model_table is required") {
			t.Fatalf("expected model_table error, got %v", err)
		}
	})

	t.Run("missing snapshot_id", func(t *testing.T) {
		snap := validSnapshot()
		snap.Meta.SnapshotID = ""
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "snapshot_id is required") {
			t.Fatalf("expected snapshot_id error, got %v", err)
		}
	})

	t.Run("wrong schema version", func(t *testing.T) {
		snap := validSnapshot()
		snap.Meta.SchemaVersion = 999
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
			t.Fatalf("expected schema version error, got %v", err)
		}
	})

	t.Run("missing ingress listen", func(t *testing.T) {
		snap := validSnapshot()
		snap.Ingress.Listen = ""
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "ingress.listen is required") {
			t.Fatalf("expected ingress.listen error, got %v", err)
		}
	})

	t.Run("empty providers", func(t *testing.T) {
		snap := validSnapshot()
		snap.Providers = nil
		err := comp.Validate(snap)
		if err == nil || !strings.Contains(err.Error(), "at least one provider") {
			t.Fatalf("expected empty providers error, got %v", err)
		}
	})

	t.Run("valid snapshot passes", func(t *testing.T) {
		err := comp.Validate(validSnapshot())
		if err != nil {
			t.Fatalf("expected nil error for valid snapshot, got %v", err)
		}
	})
}

func TestResolveQuotaRecoveryIntervalMin(t *testing.T) {
	t.Run("disable cooldown returns 0", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{DisableCooldown: true, QuotaRecoveryIntervalMin: 100})
		if got != 0 {
			t.Fatalf("got %d, want 0", got)
		}
	})

	t.Run("zero config returns default", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{})
		if got != defaultQuotaRecoveryIntervalMin {
			t.Fatalf("got %d, want %d", got, defaultQuotaRecoveryIntervalMin)
		}
	})

	t.Run("negative config returns default", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{QuotaRecoveryIntervalMin: -5})
		if got != defaultQuotaRecoveryIntervalMin {
			t.Fatalf("got %d, want %d", got, defaultQuotaRecoveryIntervalMin)
		}
	})

	t.Run("below minimum returns minimum", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{QuotaRecoveryIntervalMin: 3})
		if got != minimumQuotaRecoveryIntervalMin {
			t.Fatalf("got %d, want %d", got, minimumQuotaRecoveryIntervalMin)
		}
	})

	t.Run("valid configured value returned as-is", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{QuotaRecoveryIntervalMin: 30})
		if got != 30 {
			t.Fatalf("got %d, want 30", got)
		}
	})

	t.Run("exact minimum returns minimum", func(t *testing.T) {
		got := resolveQuotaRecoveryIntervalMin(core.FailurePolicyConfig{QuotaRecoveryIntervalMin: minimumQuotaRecoveryIntervalMin})
		if got != minimumQuotaRecoveryIntervalMin {
			t.Fatalf("got %d, want %d", got, minimumQuotaRecoveryIntervalMin)
		}
	})
}

func TestCompile_EmptyRevisionID(t *testing.T) {
	comp := NewCompiler()
	comp.SetRevisionConfigSource(&stubRevisionConfigSource{})

	_, err := comp.Compile("")
	if err == nil {
		t.Fatal("expected error for empty revision ID")
	}
	if !strings.Contains(err.Error(), "revision_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = comp.Compile("   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only revision ID")
	}
}

func TestCompile_NilConfigFromSource(t *testing.T) {
	comp := NewCompiler()
	comp.SetRevisionConfigSource(&stubRevisionConfigSource{
		configs: map[string]*core.Config{
			"rev_nil": nil,
		},
	})

	_, err := comp.Compile("rev_nil")
	if err == nil {
		t.Fatal("expected error for nil config from source")
	}
	if !strings.Contains(err.Error(), "revision not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompileFromConfig_WithPricingSources(t *testing.T) {
	cfg := core.Config{
		Pricing: core.PricingConfig{
			CachePath:              "/tmp/pricing.json",
			RefreshIntervalMinutes: 60,
			RequestTimeoutMs:       5000,
			Sources: []core.PricingSourceConfig{
				{
					ID:                     "openai-official",
					Vendor:                 "openai",
					URL:                    "https://api.openai.com/pricing",
					Enabled:                boolPtr(true),
					TimeoutMs:              3000,
					RefreshIntervalMinutes: 120,
				},
			},
			ManualPrices: []core.PricingManualPrice{
				{
					Provider:    "custom",
					Model:       "custom-model",
					Currency:    "USD",
					InputPer1M:  10.0,
					OutputPer1M: 20.0,
					Enabled:     boolPtr(true),
					Source:      "manual",
				},
			},
			FX: core.PricingFXConfig{
				CachePath:              "/tmp/fx.json",
				RefreshIntervalMinutes: 1440,
			},
		},
		Providers: []core.Provider{
			{
				Name:    "primary",
				BaseURL: "https://api.example.com/v1",
				Models:  []string{"gpt-4o"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	if snap.Pricing.CachePath != "/tmp/pricing.json" {
		t.Fatalf("pricing cache_path = %q", snap.Pricing.CachePath)
	}
	if len(snap.Pricing.Sources) != 1 {
		t.Fatalf("expected 1 pricing source, got %d", len(snap.Pricing.Sources))
	}
	if snap.Pricing.Sources[0].ID != "openai-official" {
		t.Fatalf("source ID = %q", snap.Pricing.Sources[0].ID)
	}
	if !snap.Pricing.Sources[0].Enabled {
		t.Fatal("expected source enabled")
	}
	if len(snap.Pricing.ManualPrices) != 1 {
		t.Fatalf("expected 1 manual price, got %d", len(snap.Pricing.ManualPrices))
	}
	if snap.Pricing.ManualPrices[0].Provider != "custom" {
		t.Fatalf("manual price provider = %q", snap.Pricing.ManualPrices[0].Provider)
	}
	if snap.Pricing.FX.CachePath != "/tmp/fx.json" {
		t.Fatalf("fx cache_path = %q", snap.Pricing.FX.CachePath)
	}
}

func TestCompileFromConfig_ClonePreservesOriginalPricingSources(t *testing.T) {
	sources := []core.PricingSourceConfig{{ID: "src1", Vendor: "openai"}}
	cfg := core.Config{
		Pricing: core.PricingConfig{Sources: sources},
		Providers: []core.Provider{
			{Name: "p1", BaseURL: "https://example.com", Models: []string{"m1"}},
		},
	}

	_, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	// Original config pricing sources should remain intact
	if len(cfg.Pricing.Sources) != 1 {
		t.Fatalf("original pricing sources changed")
	}
}

func TestCompileFromConfig_ClonePreservesOriginalFallbackModels(t *testing.T) {
	models := map[string]string{"old": "new"}
	cfg := core.Config{
		Compat: core.CompatConfig{
			Fallback: core.FallbackConfig{Models: models},
		},
		Providers: []core.Provider{
			{Name: "p1", BaseURL: "https://example.com", Models: []string{"m1"}},
		},
	}

	_, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	// Original should be unchanged
	if cfg.Compat.Fallback.Models["old"] != "new" {
		t.Fatal("original fallback models mutated")
	}
}

func TestCompileFromConfig_HeadersAllEmpty(t *testing.T) {
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:    "p1",
				BaseURL: "https://example.com",
				Models:  []string{"m1"},
				Headers: map[string]string{"   ": "val", "  ": "val2"},
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	if snap.Providers[0].Headers != nil {
		t.Fatalf("expected nil headers when all keys are empty, got %v", snap.Providers[0].Headers)
	}
}

func TestCompileFromConfig_EmptyCredentials(t *testing.T) {
	cfg := core.Config{
		Providers: []core.Provider{
			{
				Name:    "p1",
				BaseURL: "https://example.com",
				Models:  []string{"m1"},
				APIKey:  "",
			},
		},
	}

	snap, err := NewCompiler().CompileFromConfig(&cfg)
	if err != nil {
		t.Fatalf("CompileFromConfig() error = %v", err)
	}

	if snap.Providers[0].Credentials.Kind != "" {
		t.Fatalf("expected empty credentials kind, got %q", snap.Providers[0].Credentials.Kind)
	}
}

func TestCompileProvider_InvalidAnthropicBaseURL_Scheme(t *testing.T) {
	_, err := compileProvider(core.Provider{
		Name:             "test",
		BaseURL:          "https://api.example.com",
		AnthropicBaseURL: "not-a-url",
		Models:           []string{"m1"},
	}, 0)
	if err == nil || (!strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "absolute URL")) {
		t.Fatalf("expected URL error, got %v", err)
	}
}

func TestCompileProvider_EmptyName(t *testing.T) {
	_, err := compileProvider(core.Provider{
		Name:    "   ",
		BaseURL: "https://api.example.com",
		Models:  []string{"m1"},
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "name must not be empty") {
		t.Fatalf("expected empty name error, got %v", err)
	}
}

func TestCompileProvider_EmptyBaseURL(t *testing.T) {
	_, err := compileProvider(core.Provider{
		Name:    "test",
		BaseURL: "   ",
		Models:  []string{"m1"},
	}, 0)
	if err == nil || !strings.Contains(err.Error(), "base_url must not be empty") {
		t.Fatalf("expected empty base_url error, got %v", err)
	}
}

func validSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:      "snap_test",
			SchemaVersion:   snapshot.CurrentSchemaVersion,
			CompilerVersion: compilerVersion,
		},
		Ingress: snapshot.IngressConfig{Listen: ":18080"},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "primary",
				BaseURL:    "https://example.com",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4o", UpstreamModel: "gpt-4o"},
				},
			},
		},
	}
}

func boolPtr(v bool) *bool { return &v }
