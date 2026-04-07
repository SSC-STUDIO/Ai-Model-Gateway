package app

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/core"
)

// pipeline implements core.Pipeline — the 7-stage forwarding orchestrator.
//
// Stages: parse → resolve → select → execute → inspect → compat → telemetry
//
// Retry and fallback loops are handled internally.
type pipeline struct {
	resolver  core.ModelResolver
	selector  core.RouteSelector
	transport core.UpstreamTransport
	inspector core.ResponseInspector
	compat    core.CompatAdapter
	sink      core.TelemetrySink
	cfgMu     sync.RWMutex
	cfg       core.RoutingConfig
}

type livePipeline struct {
	mu      sync.RWMutex
	current core.Pipeline
}

// PipelineParams groups the dependencies for constructing a Pipeline.
type PipelineParams struct {
	Resolver  core.ModelResolver
	Selector  core.RouteSelector
	Transport core.UpstreamTransport
	Inspector core.ResponseInspector
	Compat    core.CompatAdapter
	Sink      core.TelemetrySink
	Cfg       core.RoutingConfig
}

// NewPipeline creates a Pipeline from its constituent stages.
func NewPipeline(p PipelineParams) core.Pipeline {
	return &pipeline{
		resolver:  p.Resolver,
		selector:  p.Selector,
		transport: p.Transport,
		inspector: p.Inspector,
		compat:    p.Compat,
		sink:      p.Sink,
		cfg:       p.Cfg,
	}
}

func newLivePipeline(current core.Pipeline) *livePipeline {
	return &livePipeline{current: current}
}

func (lp *livePipeline) Update(current core.Pipeline) {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	lp.current = current
}

func (lp *livePipeline) Handle(ctx context.Context, req *core.GatewayRequest) (*core.GatewayResponse, error) {
	lp.mu.RLock()
	current := lp.current
	lp.mu.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("pipeline unavailable")
	}
	return current.Handle(ctx, req)
}

func (pl *pipeline) UpdateConfig(cfg core.RoutingConfig) {
	pl.cfgMu.Lock()
	defer pl.cfgMu.Unlock()
	pl.cfg = cfg
}

func (pl *pipeline) Handle(ctx context.Context, req *core.GatewayRequest) (*core.GatewayResponse, error) {
	start := time.Now()
	cfg := pl.routingConfig()
	req.Model = strings.TrimSpace(req.Model)
	originalBody := cloneBytes(req.Body)

	if req.ModelRequired && req.Model == "" {
		return nil, core.ErrModelNotFound
	}

	req.OriginalModel = req.Model
	if !req.SkipModelRewrite {
		// --- Stage 1: Model Resolution ---
		resolved, err := pl.resolver.Resolve(ctx, req.Model, req.UserAgent)
		if err != nil {
			return nil, fmt.Errorf("resolve model: %w", err)
		}
		req.Model = resolved
	}

	// Determine max attempts.
	maxAttempts := cfg.MaxRetries + 1
	if cfg.Retry.InfiniteOnError {
		maxAttempts = 999 // effectively unlimited, bounded by timeout
	}
	req.MaxAttempts = maxAttempts

	var lastResp *core.GatewayResponse
	currentModel := req.Model
	fallbackActivated := false
	excluded := make(map[string]struct{})
	sameProviderRetriesUsed := make(map[string]int)
	var retryProvider *core.Provider

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			break
		}

		req.Attempt = attempt
		req.Model = currentModel
		req.Body = cloneBytes(originalBody)

		// --- Stage 2: Route Selection ---
		provider := retryProvider
		retryProvider = nil
		if provider == nil {
			var selErr error
			provider, selErr = pl.selectProvider(ctx, currentModel, req.StickyKey, excluded)
			if selErr != nil {
				// If primary model has no provider, try fallback.
				if attempt == 0 {
					fb := pl.resolver.FallbackModel(currentModel)
					if fb != "" {
						fallbackActivated = true
						currentModel = fb
						continue
					}
				}
				if lastResp != nil {
					return lastResp, nil
				}
				return nil, selErr
			}
		}
		req.Provider = provider

		// --- Stage 3: Compat Adapt Request ---
		if pl.compat != nil {
			if err := pl.compat.AdaptRequest(ctx, req); err != nil {
				return nil, fmt.Errorf("compat adapt request: %w", err)
			}
		}

		// --- Stage 4: Upstream Execution ---
		resp, execErr := pl.transport.Execute(ctx, req)
		if execErr != nil {
			pl.selector.ReportResult(provider, 0, time.Since(start), execErr)
			lastResp = &core.GatewayResponse{
				StatusCode: 502,
				Provider:   provider,
				Latency:    time.Since(start),
				Error:      execErr,
				Retryable:  true,
			}
			retryProvider = updateRetryCandidate(provider, sameProviderRetriesUsed, excluded)
			pl.backoff(ctx, cfg, attempt)
			continue
		}

		// --- Stage 5: Response Inspection ---
		resp, _ = pl.inspect(ctx, req, resp, cfg)
		resp = pl.tryResponsesCompat(ctx, req, resp)
		resp = pl.tryResponsesAnthropicCompat(ctx, req, resp)
		resp = pl.tryChatAnthropicCompat(ctx, req, resp)
		resp = pl.tryMessagesCompat(ctx, req, resp)

		// Report result to selector for health tracking.
		var reportErr error
		if resp.Error != nil {
			reportErr = resp.Error
		}
		pl.selector.ReportResult(provider, resp.StatusCode, resp.Latency, reportErr)

		// --- Stage 6: Compat Adapt Response ---
		if pl.compat != nil && !resp.Stream {
			if err := pl.compat.AdaptResponse(ctx, req, resp); err != nil {
				log.Printf("[pipeline] compat adapt response error: %v", err)
				// Sanitize error message to prevent information leakage
				safeErrMsg := sanitizeErrorMessage(err.Error())
				resp = &core.GatewayResponse{
					StatusCode: http.StatusServiceUnavailable,
					Headers:    http.Header{"Content-Type": []string{"application/json"}},
					Body:       []byte(fmt.Sprintf(`{"error":"%s"}`, safeErrMsg)),
					Provider:   provider,
					Latency:    resp.Latency,
					Retryable:  true,
					Error:      err,
				}
			}
		}

		// --- Stage 7: Telemetry ---
		if pl.sink != nil {
			inTokens, cachedPromptTokens, outTokens := extractTelemetryUsage(resp)
			errMsg := ""
			if resp.Error != nil {
				errMsg = resp.Error.Error()
			}
			effectiveModel := strings.TrimSpace(currentModel)
			if effectiveModel == "" {
				effectiveModel = strings.TrimSpace(resp.Model)
			}
			if effectiveModel == "" {
				effectiveModel = strings.TrimSpace(req.Model)
			}
			requestedModel := strings.TrimSpace(req.OriginalModel)
			routeMode := strings.TrimSpace(resp.RouteMode)
			if routeMode == "" {
				routeMode = requestRouteMode(requestedModel, effectiveModel, fallbackActivated)
			}
			_ = pl.sink.Record(ctx, &core.RequestRecord{
				RequestID:          req.ID,
				Timestamp:          time.Now(),
				Path:               req.Path,
				RequestedModel:     requestedModel,
				EffectiveModel:     effectiveModel,
				Model:              effectiveModel,
				RouteMode:          routeMode,
				Attempts:           attempt + 1,
				Provider:           provider.Name,
				StatusCode:         resp.StatusCode,
				Latency:            resp.Latency,
				InputTokens:        inTokens,
				CachedPromptTokens: cachedPromptTokens,
				OutputTokens:       outTokens,
				Stream:             resp.Stream,
				Error:              errMsg,
			})
		}

		if !resp.Retryable && resp.StatusCode < http.StatusBadRequest && provider != nil {
			rememberStickyRouting(pl.selector, req.Path, req.StickyKey, provider.Name, resp)
		}

		// If response is not retryable, return immediately.
		if !resp.Retryable {
			return resp, nil
		}

		lastResp = resp
		retryProvider = updateRetryCandidate(provider, sameProviderRetriesUsed, excluded)

		// Try fallback model on first failure.
		if attempt == 0 {
			fb := pl.resolver.FallbackModel(currentModel)
			if fb != "" {
				fallbackActivated = true
				currentModel = fb
				retryProvider = nil
				continue
			}
		}

		pl.backoff(ctx, cfg, attempt)
	}

	// All attempts exhausted.
	if lastResp != nil {
		return lastResp, nil
	}
	return nil, core.ErrRetryExhausted
}

// backoff applies exponential backoff with jitter between retries.
// It returns early if ctx is cancelled.
func (pl *pipeline) backoff(ctx context.Context, cfg core.RoutingConfig, attempt int) {
	baseMs := cfg.RetryBackoff.InitialMs
	maxMs := cfg.RetryBackoff.MaxMs
	if baseMs <= 0 {
		baseMs = 1000
	}
	if maxMs <= 0 {
		maxMs = 30000
	}

	ms := float64(baseMs) * math.Pow(2, float64(attempt))
	if ms > float64(maxMs) {
		ms = float64(maxMs)
	}
	// Add ±25% jitter.
	jitter := ms * 0.25 * (rand.Float64()*2 - 1)
	sleep := time.Duration(ms+jitter) * time.Millisecond

	timer := time.NewTimer(sleep)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (pl *pipeline) routingConfig() core.RoutingConfig {
	pl.cfgMu.RLock()
	defer pl.cfgMu.RUnlock()
	return pl.cfg
}

func (pl *pipeline) inspect(ctx context.Context, req *core.GatewayRequest, resp *core.GatewayResponse, cfg core.RoutingConfig) (*core.GatewayResponse, error) {
	if _, ok := pl.inspector.(*inspector); ok {
		live := &inspector{
			retry:      cfg.Retry,
			intercepts: append([]core.InterceptRule(nil), cfg.Intercepts...),
		}
		return live.Inspect(ctx, req, resp)
	}
	return pl.inspector.Inspect(ctx, req, resp)
}

func (pl *pipeline) selectProvider(ctx context.Context, model string, stickyKey string, excluded map[string]struct{}) (*core.Provider, error) {
	type exclusionAwareSelector interface {
		SelectWithExclusions(context.Context, string, string, map[string]struct{}) (*core.Provider, error)
	}

	if selector, ok := pl.selector.(exclusionAwareSelector); ok {
		provider, err := selector.SelectWithExclusions(ctx, model, stickyKey, excluded)
		if err == nil {
			return provider, nil
		}
		// Exclusions are only a preference for failover diversity. If they
		// eliminate every candidate, fall back to the full pool so single-
		// provider routes still honor the configured retry budget.
		if err == core.ErrNoProvider && len(excluded) > 0 {
			return pl.selector.Select(ctx, model, stickyKey)
		}
		return nil, err
	}
	return pl.selector.Select(ctx, model, stickyKey)
}

func updateRetryCandidate(provider *core.Provider, retriesUsed map[string]int, excluded map[string]struct{}) *core.Provider {
	if provider == nil {
		return nil
	}
	if shouldRetrySameProvider(provider, retriesUsed[provider.Name]) {
		retriesUsed[provider.Name]++
		return provider
	}
	excluded[provider.Name] = struct{}{}
	return nil
}

func shouldRetrySameProvider(provider *core.Provider, retriesUsed int) bool {
	return provider != nil && provider.SameRetries > retriesUsed
}

func requestRouteMode(requestedModel string, effectiveModel string, fallbackActivated bool) string {
	if fallbackActivated {
		return "bridge_fallback"
	}
	if requestedModel != "" && effectiveModel != "" && requestedModel != effectiveModel {
		return "bridged"
	}
	return "direct"
}

func extractTelemetryUsage(resp *core.GatewayResponse) (inputTokens int64, cachedPromptTokens int64, outputTokens int64) {
	if resp == nil || len(resp.Body) == 0 {
		return 0, 0, 0
	}

	var payload struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			InputTokens      int64 `json:"input_tokens"`
			OutputTokens     int64 `json:"output_tokens"`
			PromptDetails    struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}

	if err := JSONUnmarshal(resp.Body, &payload); err != nil {
		return 0, 0, 0
	}

	inputTokens = payload.Usage.PromptTokens
	if inputTokens == 0 {
		inputTokens = payload.Usage.InputTokens
	}
	outputTokens = payload.Usage.CompletionTokens
	if outputTokens == 0 {
		outputTokens = payload.Usage.OutputTokens
	}
	cachedPromptTokens = payload.Usage.PromptDetails.CachedTokens
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.InputDetails.CachedTokens
	}
	return inputTokens, cachedPromptTokens, outputTokens
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// sanitizeErrorMessage removes potentially sensitive information from error messages
func sanitizeErrorMessage(msg string) string {
	// List of patterns that might indicate sensitive data
	sensitivePatterns := []string{
		"sk-",           // OpenAI API key prefix
		"Bearer ",       // Auth header
		"bearer ",
		"api-key",       // API key header
		"x-api-key",
		"password",      // Password
		"secret",        // Secret
		"auth_token",    // Auth token
		"access_token",  // Access token
		"refresh_token", // Refresh token
		"credential",    // Credentials
	}

	lowerMsg := strings.ToLower(msg)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerMsg, strings.ToLower(pattern)) {
			return "internal server error"
		}
	}

	// Limit message length to prevent information leakage
	if len(msg) > 200 {
		return msg[:200] + "..."
	}
	return msg
}
