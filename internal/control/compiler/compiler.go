// Package compiler compiles authoring configuration into runtime snapshots.
package compiler

import (
	crypto_rand "crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
	gatewaytelemetry "ai-model-gateway/internal/gateway/telemetry"
)

const (
	compilerVersion                  = "1.0.0"
	publicAPIOpenAIChatCompletions   = "openai_chat_completions"
	protocolAdapterAnthropicMessages = "anthropic_messages"
	telemetryChannel                 = "telemetry-ingest"
	credentialKindBearer             = "bearer"
	credentialKindAPIKey             = "api_key"
	usageAccountingOpenAI            = "openai_usage"
	errorClassifierOpenAI            = "openai_error"
	usageAccountingAnthropic         = "anthropic_usage"
	errorClassifierAnthropic         = "anthropic_error"
	defaultQuotaRecoveryIntervalMin  = 60
	minimumQuotaRecoveryIntervalMin  = 5
)

var gatewayEnabledRoutes = []string{
	"POST /v1/chat/completions",
	"POST /v1/messages",
	"GET /v1/models",
	"GET /-/health",
}

// Compiler compiles authoring configuration into runtime snapshots.
type Compiler struct {
	version        string
	revisionSource RevisionConfigSource
}

// RevisionConfigSource loads authoring config payloads for stored revisions.
type RevisionConfigSource interface {
	LoadRevisionConfig(revisionID string) (*core.Config, error)
}

// NewCompiler creates a new compiler.
func NewCompiler() *Compiler {
	return &Compiler{
		version: compilerVersion,
	}
}

// SetRevisionConfigSource configures how revision-backed Compile loads config payloads.
func (c *Compiler) SetRevisionConfigSource(source RevisionConfigSource) {
	c.revisionSource = source
}

// Compile compiles a configuration revision into a runtime snapshot.
func (c *Compiler) Compile(revisionID string) (*snapshot.Snapshot, error) {
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" {
		return nil, fmt.Errorf("revision_id is required")
	}

	if c.revisionSource == nil {
		return nil, fmt.Errorf("compile revision %q: revision config source is not configured", revisionID)
	}

	cfg, err := c.revisionSource.LoadRevisionConfig(revisionID)
	if err != nil {
		return nil, fmt.Errorf("compile revision %q: load config: %w", revisionID, err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("compile revision %q: revision not found", revisionID)
	}

	snap, err := c.CompileFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("compile revision %q: %w", revisionID, err)
	}
	snap.Meta.RevisionID = revisionID
	return snap, nil
}

// CompileFromConfig compiles a configuration into a runtime snapshot.
func (c *Compiler) CompileFromConfig(cfg interface{}) (*snapshot.Snapshot, error) {
	source, err := asCoreConfig(cfg)
	if err != nil {
		return nil, err
	}

	normalized := cloneConfig(source)
	normalized.Normalize()

	now := time.Now().UTC()
	telemetryDefaults := gatewaytelemetry.DefaultClientConfig()
	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SnapshotID:      generateSnapshotID(),
			SchemaVersion:   snapshot.CurrentSchemaVersion,
			GeneratedAt:     now,
			CompilerVersion: c.version,
		},
		Ingress: compileIngress(normalized.Server),
		Contract: snapshot.ContractConfig{
			PublicAPI:     publicAPIOpenAIChatCompletions,
			EnabledRoutes: append([]string(nil), gatewayEnabledRoutes...),
		},
		Providers:     make([]snapshot.ProviderSnapshot, 0, len(normalized.Providers)),
		RoutingPolicy: compileRoutingPolicy(normalized.Routing),
		TelemetryEmit: snapshot.TelemetryEmitConfig{
			Channel: telemetryChannel,
			Batching: snapshot.BatchingConfig{
				MaxBatchSize:    telemetryDefaults.BatchSize,
				FlushIntervalMs: int(telemetryDefaults.FlushInterval / time.Millisecond),
			},
		},
		Pricing: compilePricing(normalized.Pricing),
	}

	for i, provider := range normalized.Providers {
		if !provider.IsEnabled() {
			continue
		}
		providerSnap, err := compileProvider(provider, i)
		if err != nil {
			return nil, err
		}
		snap.Providers = append(snap.Providers, providerSnap)
	}

	if err := c.Validate(snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// Validate validates a snapshot.
func (c *Compiler) Validate(snap *snapshot.Snapshot) error {
	if snap.Meta.SnapshotID == "" {
		return fmt.Errorf("snapshot_id is required")
	}
	if snap.Meta.SchemaVersion != snapshot.CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema version: %d (expected %d)",
			snap.Meta.SchemaVersion, snapshot.CurrentSchemaVersion)
	}
	if snap.Ingress.Listen == "" {
		return fmt.Errorf("ingress.listen is required")
	}
	if len(snap.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}
	for i, p := range snap.Providers {
		if p.ProviderID == "" {
			return fmt.Errorf("providers[%d].provider_id is required", i)
		}
		if p.BaseURL == "" {
			return fmt.Errorf("providers[%d].base_url is required", i)
		}
		if len(p.ModelTable) == 0 {
			return fmt.Errorf("providers[%d].model_table is required", i)
		}
	}
	return nil
}

func asCoreConfig(cfg interface{}) (*core.Config, error) {
	switch typed := cfg.(type) {
	case core.Config:
		return &typed, nil
	case *core.Config:
		if typed == nil {
			return nil, fmt.Errorf("config is nil")
		}
		return typed, nil
	default:
		return nil, fmt.Errorf("unsupported config type %T; expected core.Config or *core.Config", cfg)
	}
}

func cloneConfig(src *core.Config) core.Config {
	cloned := *src

	if len(src.Admin.Tokens) > 0 {
		cloned.Admin.Tokens = append([]core.TokenConfig(nil), src.Admin.Tokens...)
	}
	if len(src.Routing.Intercepts) > 0 {
		cloned.Routing.Intercepts = append([]core.InterceptRule(nil), src.Routing.Intercepts...)
	}
	if len(src.Providers) > 0 {
		cloned.Providers = append([]core.Provider(nil), src.Providers...)
	}
	if len(src.Pricing.ManualPrices) > 0 {
		cloned.Pricing.ManualPrices = append([]core.PricingManualPrice(nil), src.Pricing.ManualPrices...)
	}
	if len(src.Pricing.Sources) > 0 {
		cloned.Pricing.Sources = append([]core.PricingSourceConfig(nil), src.Pricing.Sources...)
	}
	if src.Compat.Fallback.Models != nil {
		cloned.Compat.Fallback.Models = cloneStringMap(src.Compat.Fallback.Models)
	}

	return cloned
}

func compileIngress(cfg core.ServerConfig) snapshot.IngressConfig {
	writeTimeoutMs := cfg.WriteTimeoutMs
	if writeTimeoutMs <= 0 {
		writeTimeoutMs = 0
	}
	return snapshot.IngressConfig{
		Listen:         cfg.Listen,
		ReadTimeoutMs:  cfg.ReadTimeoutMs,
		WriteTimeoutMs: writeTimeoutMs,
		IdleTimeoutMs:  cfg.IdleTimeoutMs,
		MaxBodyBytes:   cfg.MaxBodyBytes,
	}
}

func compilePricing(cfg core.PricingConfig) snapshot.PricingConfig {
	result := snapshot.PricingConfig{
		CachePath:              cfg.CachePath,
		RefreshIntervalMinutes: cfg.RefreshIntervalMinutes,
		RequestTimeoutMs:       cfg.RequestTimeoutMs,
		FX: snapshot.PricingFXConfig{
			Enabled:                cfg.FX.IsEnabled(),
			CachePath:              cfg.FX.CachePath,
			RefreshIntervalMinutes: cfg.FX.RefreshIntervalMinutes,
		},
	}
	if len(cfg.Sources) > 0 {
		result.Sources = make([]snapshot.PricingSource, 0, len(cfg.Sources))
		for _, source := range cfg.Sources {
			result.Sources = append(result.Sources, snapshot.PricingSource{
				ID:                     source.ID,
				Vendor:                 source.Vendor,
				URL:                    source.URL,
				Enabled:                source.IsEnabled(),
				TimeoutMs:              source.TimeoutMs,
				RefreshIntervalMinutes: source.RefreshIntervalMinutes,
			})
		}
	}
	if len(cfg.ManualPrices) > 0 {
		result.ManualPrices = make([]snapshot.PricingManualPrice, 0, len(cfg.ManualPrices))
		for _, manual := range cfg.ManualPrices {
			result.ManualPrices = append(result.ManualPrices, snapshot.PricingManualPrice{
				Provider:         manual.Provider,
				Model:            manual.Model,
				Currency:         manual.Currency,
				InputPer1M:       manual.InputPer1M,
				CachedInputPer1M: manual.CachedInputPer1M,
				OutputPer1M:      manual.OutputPer1M,
				Enabled:          manual.IsEnabled(),
				Source:           manual.Source,
			})
		}
	}
	return result
}

func compileRoutingPolicy(cfg core.RoutingConfig) snapshot.RoutingPolicy {
	statusCodeMin := 0
	if cfg.Retry.StatusCodeMin != nil {
		statusCodeMin = *cfg.Retry.StatusCodeMin
	}

	return snapshot.RoutingPolicy{
		Strategy:   cfg.Strategy,
		MaxRetries: cfg.MaxRetries,
		RetryBackoff: snapshot.RetryBackoff{
			InitialMs: cfg.RetryBackoff.InitialMs,
			MaxMs:     cfg.RetryBackoff.MaxMs,
		},
		Health: snapshot.HealthConfig{
			Enabled:     cfg.Health.Enabled,
			IntervalSec: cfg.Health.IntervalSec,
			TimeoutMs:   cfg.Health.TimeoutMs,
			Path:        cfg.Health.Path,
		},
		StickySessions: snapshot.StickySessionConfig{
			Enabled: cfg.StickySessions.Enabled,
			TTLSec:  cfg.StickySessions.TTLSec,
		},
		FailurePolicy: snapshot.FailurePolicy{
			Threshold:                cfg.FailurePolicy.Threshold,
			CooldownSec:              cfg.FailurePolicy.CooldownSec,
			PassthroughAfterSec:      cfg.FailurePolicy.PassthroughAfterSec,
			QuotaRecoveryIntervalMin: resolveQuotaRecoveryIntervalMin(cfg.FailurePolicy.QuotaRecoveryIntervalMin),
		},
		Retry: snapshot.RetryPolicy{
			InfiniteOnError: cfg.Retry.InfiniteOnError,
			StatusCodes:     append([]int(nil), cfg.Retry.StatusCodes...),
			StatusCodeMin:   statusCodeMin,
			MessageKeywords: append([]string(nil), cfg.Retry.MessageKeywords...),
		},
		RateLimit: snapshot.RateLimitConfig{
			Enabled:           cfg.RateLimit.Enabled,
			RequestsPerSecond: cfg.RateLimit.RequestsPerSecond,
			Burst:             cfg.RateLimit.Burst,
		},
		Cache: snapshot.CacheConfig{
			Enabled:    cfg.Cache.Enabled,
			MaxEntries: cfg.Cache.MaxEntries,
			TTLSec:     cfg.Cache.TTLSec,
		},
		Queue: snapshot.QueueConfig{
			Enabled:         cfg.Queue.Enabled,
			MaxConcurrent:   cfg.Queue.MaxConcurrent,
			HighPriorityPct: cfg.Queue.HighPriorityPct,
		},
		KeyRotation: snapshot.KeyRotationConfig{
			Enabled: cfg.KeyRotation.Enabled,
		},
		Compression: snapshot.CompressionConfig{
			Enabled:      cfg.Compression.Enabled,
			MinSizeBytes: cfg.Compression.MinSizeBytes,
			Level:        cfg.Compression.Level,
		},
	}
}

func resolveQuotaRecoveryIntervalMin(configured int) int {
	if configured <= 0 {
		return defaultQuotaRecoveryIntervalMin
	}
	if configured < minimumQuotaRecoveryIntervalMin {
		return minimumQuotaRecoveryIntervalMin
	}
	return configured
}

func compileProvider(provider core.Provider, index int) (snapshot.ProviderSnapshot, error) {
	providerID := strings.TrimSpace(provider.Name)
	if providerID == "" {
		return snapshot.ProviderSnapshot{}, fmt.Errorf("providers[%d].name must not be empty", index)
	}

	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		return snapshot.ProviderSnapshot{}, fmt.Errorf("providers[%d].base_url must not be empty", index)
	}

	modelTable := compileModelTable(provider.Models)
	if len(modelTable) == 0 {
		return snapshot.ProviderSnapshot{}, fmt.Errorf("providers[%d].models must not be empty", index)
	}

	// Validate and compile AnthropicBaseURL
	anthropicBaseURL := strings.TrimSpace(provider.AnthropicBaseURL)
	if anthropicBaseURL != "" {
		parsed, err := url.Parse(anthropicBaseURL)
		if err != nil {
			return snapshot.ProviderSnapshot{}, fmt.Errorf("providers[%d].anthropic_base_url is invalid: %w", index, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return snapshot.ProviderSnapshot{}, fmt.Errorf("providers[%d].anthropic_base_url must be an absolute URL", index)
		}
	}

	protocolAdapter := core.NormalizeProtocolAdapter(provider.ProtocolAdapter, provider.AnthropicBaseURL)
	usageAccounting := usageAccountingOpenAI
	errorClassifier := errorClassifierOpenAI
	if protocolAdapter == protocolAdapterAnthropicMessages {
		usageAccounting = usageAccountingAnthropic
		errorClassifier = errorClassifierAnthropic
	}

	return snapshot.ProviderSnapshot{
		ProviderID:       providerID,
		ProtocolAdapter:  protocolAdapter,
		BaseURL:          baseURL,
		AnthropicBaseURL: anthropicBaseURL,
		Credentials:      compileCredentials(provider),
		Headers:          compileHeaders(provider.Headers),
		ModelTable:       modelTable,
		CapabilityTable: snapshot.CapabilityTable{
			SupportsChatCompletions: true,
			SupportsStreaming:       true,
			UsageAccounting:         usageAccounting,
			ErrorClassifier:         errorClassifier,
		},
		ExecutionPolicy: snapshot.ExecutionPolicy{
			Enabled:       provider.IsEnabled(),
			Weight:        provider.Weight,
			TimeoutMs:     provider.TimeoutMs,
			SameRetries:   provider.SameRetries,
			ProviderClass: string(provider.ProviderClass),
		},
		FallbackModels: append([]string(nil), provider.FallbackModels...),
	}, nil
}

func compileCredentials(provider core.Provider) snapshot.Credentials {
	apiKey := strings.TrimSpace(provider.APIKey)
	if apiKey == "" {
		return snapshot.Credentials{}
	}

	// When AnthropicBaseURL is set, use api_key credential type with x-api-key header
	if strings.TrimSpace(provider.AnthropicBaseURL) != "" ||
		core.NormalizeProtocolAdapter(provider.ProtocolAdapter, provider.AnthropicBaseURL) == protocolAdapterAnthropicMessages {
		return snapshot.Credentials{
			Kind:       credentialKindAPIKey,
			Value:      apiKey,
			HeaderName: "x-api-key",
		}
	}

	// Default: use bearer token
	return snapshot.Credentials{
		Kind:  credentialKindBearer,
		Value: apiKey,
	}
}

func compileHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	compiled := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		compiled[key] = value
	}
	if len(compiled) == 0 {
		return nil
	}
	return compiled
}

func compileModelTable(models []string) []snapshot.ModelMapping {
	if len(models) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(models))
	table := make([]snapshot.ModelMapping, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		table = append(table, snapshot.ModelMapping{
			PublicModel:   model,
			UpstreamModel: model,
		})
	}
	return table
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func generateSnapshotID() string {
	return "snap_" + time.Now().UTC().Format("20060102_150405") + "_" + randomSuffix()
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	buf := make([]byte, 6)
	_, _ = crypto_rand.Read(buf)
	for i := range b {
		b[i] = chars[int(buf[i])%len(chars)]
	}
	return string(b)
}
