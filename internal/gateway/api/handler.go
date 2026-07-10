// Package api provides HTTP handlers for gatewayd.
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/gateway/cache"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/proxy"

	"github.com/google/uuid"
)

// sharedHTTPClient is a reusable HTTP client for upstream requests.
// Its Transport pins DNS resolution at dial time via proxy.SSRFChecker.NewSafeTransport,
// which eliminates the DNS-rebinding TOCTOU window between URL validation and the
// actual TCP connection. All upstream requests routed through this client are
// protected from SSRF via DNS rebinding.
var sharedHTTPClient = newSharedHTTPClient()

func newSharedHTTPClient() *http.Client {
	base := &http.Transport{
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:      true,
		MaxResponseHeaderBytes: 1 << 20,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	safe := proxy.NewSSRFChecker().NewSafeTransport(base)
	// Preserve the tuned settings from `base` on the wrapped transport because
	// NewSafeTransport only overrides DialContext.
	return &http.Client{
		Transport: safe,
		Timeout:   0, // per-request timeout set via context
	}
}

type urlValidator interface {
	ValidateURL(rawURL string) error
}

var ssrfChecker urlValidator = proxy.NewSSRFChecker()

// SetSSRFCheckerFromSnapshot rebuilds the package-level SSRF checker from
// the snapshot's routing policy so the data-plane forwarder and the health
// probe honor the same allowlist. Fixes the SSRF inconsistency in #13.
func SetSSRFCheckerFromSnapshot(snap *snapshot.Snapshot) {
	if snap == nil {
		ssrfChecker = proxy.NewSSRFChecker()
		return
	}
	ssrfChecker = proxy.NewSSRFCheckerWithConfig(proxy.SSRFConfig{
		AllowLocalhost: snap.RoutingPolicy.SSRF.AllowLocalhost,
		AllowPrivateIP: snap.RoutingPolicy.SSRF.AllowPrivateIP,
	})
}

// SetSharedHTTPClientForTesting swaps the shared HTTP client for tests and returns a restore function.
func SetSharedHTTPClientForTesting(client *http.Client) func() {
	original := sharedHTTPClient
	sharedHTTPClient = client
	return func() {
		sharedHTTPClient = original
	}
}

// SetSSRFCheckerForTesting swaps the SSRF checker for tests and returns a restore function.
func SetSSRFCheckerForTesting(checker urlValidator) func() {
	original := ssrfChecker
	if checker == nil {
		ssrfChecker = proxy.NewSSRFChecker()
	} else {
		ssrfChecker = checker
	}
	return func() {
		ssrfChecker = original
	}
}

// responseCache is a shared LRU cache instance keyed by config parameters.
var responseCache struct {
	mu     sync.Mutex
	cache  *cache.Cache
	cfgKey string
}

func getResponseCache(snap *snapshot.Snapshot) *cache.Cache {
	cfgKey := fmt.Sprintf("%d:%d", snap.RoutingPolicy.Cache.MaxEntries, snap.RoutingPolicy.Cache.TTLSec)
	responseCache.mu.Lock()
	defer responseCache.mu.Unlock()
	if responseCache.cache == nil || responseCache.cfgKey != cfgKey {
		responseCache.cache = cache.NewCache(snap.RoutingPolicy.Cache.MaxEntries, snap.RoutingPolicy.Cache.TTLSec)
		responseCache.cfgKey = cfgKey
	}
	return responseCache.cache
}

// HandleChatCompletion handles a chat completion request.
func HandleChatCompletion(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request) {
	handleChatOrMessages(ctx, snap, runtimeState, telClient, pricingResolver, w, r, formatChatCompletions)
}

// HandleMessages handles an Anthropic Messages API request.
func HandleMessages(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request) {
	handleChatOrMessages(ctx, snap, runtimeState, telClient, pricingResolver, w, r, formatAnthropic)
}

// HandleResponses handles an OpenAI Responses API request.
func HandleResponses(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request) {
	handleChatOrMessages(ctx, snap, runtimeState, telClient, pricingResolver, w, r, formatResponses)
}

// handleChatOrMessages handles chat completion, messages, and responses requests.
func handleChatOrMessages(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request, clientFmt clientFormat) {
	start := time.Now()
	opts := executionOptionsFromContext(ctx)
	requestID := uuid.New().String()
	if opts != nil && strings.TrimSpace(opts.RequestID) != "" {
		requestID = strings.TrimSpace(opts.RequestID)
	}

	// Read request body
	body, err := io.ReadAll(io.LimitReader(r.Body, snap.Ingress.MaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Parse only the fields the gateway actually needs. The rest of the
	// payload should remain schema-agnostic so multimodal content can pass through.
	reqMeta, err := parseChatCompletionRequestMeta(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if reqMeta.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}
	requestedModel := reqMeta.Model
	routingModel := resolveBridgeModel(snap, requestedModel, r.UserAgent())

	// Resolve sticky key early so both cache lookup and store use the same
	// namespace. Previously the lookup used "" while the store used stickyKey,
	// causing every cached entry written with a sticky key to be unfindable
	// (the SHA-256 keys diverged).
	stickyKey := resolveStickyKey(reqMeta, r.Header)
	if opts != nil && opts.DisableSticky {
		stickyKey = ""
	}

	// Check request cache for non-streaming requests.
	if !reqMeta.Stream && snap.RoutingPolicy.Cache.Enabled && (opts == nil || !opts.DisableCache) {
		c := getResponseCache(snap)
		cacheKey := c.MakeKey(body, reqMeta.Model, stickyKey)
		if cached, ok := c.Get(cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached)

			emitTelemetry(telClient, requestID, start, r.URL.Path, reqMeta.Model, reqMeta.Model, "",
				"cache", http.StatusOK, time.Since(start), 0, 0, 0, 0, false, "",
				resolveFixedPricing(pricingResolver, reqMeta.Model, reqMeta.Model, "", 0, 0, 0, true, http.StatusOK), opts)
			captureExecutionResult(opts, http.StatusOK, "application/json", time.Since(start), 0, 0, 0, "", reqMeta.Model, "cache", 0, "")
			return
		}
	}
	candidates := collectProviderCandidatesForRequest(snap, routingModel)
	if len(candidates) == 0 {
		writeError(w, http.StatusNotFound, "model not found: "+routingModel)
		return
	}
	pinnedProviderID := ""
	if opts != nil {
		pinnedProviderID = strings.TrimSpace(opts.PinnedProviderID)
	}
	if pinnedProviderID != "" {
		filtered := make([]providerCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.provider != nil && candidate.provider.ProviderID == pinnedProviderID {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
		if len(candidates) == 0 {
			writeError(w, http.StatusNotFound, "provider not available for requested model")
			captureExecutionResult(opts, http.StatusNotFound, "application/json", time.Since(start), 0, 0, 0, "", reqMeta.Model, "", 0, "provider not available for requested model")
			return
		}
	}
	var orderedCandidates []providerCandidate
	if pinnedProviderID != "" {
		orderedCandidates = orderProviderCandidates(candidates)
	} else if runtimeState != nil {
		orderedCandidates = runtimeState.orderCandidates(snap, routingModel, stickyKey, candidates)
	} else {
		orderedCandidates = orderProviderCandidates(candidates)
	}
	routeMode := determineRouteMode(orderedCandidates, snap)
	if routingModel != requestedModel {
		routeMode = "model_bridge"
	}
	maxAttempts := maxTotalAttempts(snap)
	if opts != nil && opts.DisableRetries {
		maxAttempts = 1
	}
	var streamRetry *streamRetrySession
	retryStartedAt := time.Now()
	retryBudget := time.Duration(snap.RoutingPolicy.Retry.MaxElapsedMs) * time.Millisecond

	var (
		attempts            int
		finalStatusCode     = http.StatusBadGateway
		finalRespBody       []byte
		finalStreamBody     io.ReadCloser
		finalContentType    string
		finalLatency        time.Duration
		finalForwardErr     error
		finalProvider       *snapshot.ProviderSnapshot
		finalEffectiveModel = routingModel
		finalErrorMessage   string
		finalCompatPlan     compatPlan
	)

attemptLoop:
	for i := range orderedCandidates {
		candidate := orderedCandidates[i]
		compatPlan, compatErr := buildCompatPlan(clientFmt, candidate.provider, requestedModel, candidate.upstreamModel, body)
		if compatErr != nil {
			finalStatusCode = http.StatusBadRequest
			if errors.Is(compatErr, errMessagesAPIRequiresAnthropicProvider) {
				finalStatusCode = http.StatusNotImplemented
			}
			finalForwardErr = nil
			finalProvider = candidate.provider
			finalEffectiveModel = candidate.upstreamModel
			finalErrorMessage = compatErr.Error()
			finalCompatPlan = compatPlan
			break
		}

		sameProviderAttempts := 1 + maxInt(candidate.provider.ExecutionPolicy.SameRetries, 0)
		if opts != nil && opts.DisableRetries {
			sameProviderAttempts = 1
		}
		// When maxAttempts == 0 (infinite retry), also allow infinite same-provider attempts.
		// Streaming requests can keep the client connection alive through streamRetry.
		innerLimit := sameProviderAttempts
		if maxAttempts == 0 {
			innerLimit = 0
		}
		for providerAttempt := 0; (innerLimit == 0 || providerAttempt < innerLimit) && (maxAttempts == 0 || attempts < maxAttempts); providerAttempt++ {
			if retryBudget > 0 && time.Since(retryStartedAt) >= retryBudget {
				finalForwardErr = context.DeadlineExceeded
				finalErrorMessage = "retry time budget exhausted"
				break attemptLoop
			}
			// Hard cap for infinite-retry mode so a pathological upstream cannot
			// hold the client forever. 20 attempts across providers is plenty
			// for transient blips while still terminating. Fixes #14.
			if maxAttempts == 0 && attempts >= 20 {
				break attemptLoop
			}
			attempts++

			attemptLabel := strconv.Itoa(maxAttempts)
			if maxAttempts == 0 {
				attemptLabel = "inf"
			}
			log.Printf("[gatewayd] request_id=%s model=%s upstream_model=%s provider=%s attempt=%d/%s",
				requestID, requestedModel, candidate.upstreamModel, candidate.provider.ProviderID, attempts, attemptLabel)

			statusCode, respBody, streamBody, streamContentType, latency, forwardErr := forwardToUpstream(
				ctx,
				runtimeState,
				candidate.provider,
				compatPlan.forwardPath,
				compatPlan.forwardBody,
				reqMeta.Stream,
				r.Header,
				compatPlan.upstreamIsAnthropic,
			)

			finalStatusCode = statusCode
			finalRespBody = respBody
			finalStreamBody = streamBody
			finalContentType = streamContentType
			finalLatency = latency
			finalForwardErr = forwardErr
			finalProvider = candidate.provider
			finalEffectiveModel = candidate.upstreamModel
			finalCompatPlan = compatPlan
			finalErrorMessage = extractErrorMessage(respBody, forwardErr)
			if runtimeState != nil {
				runtimeState.reportAttemptResult(candidate.provider.ProviderID, statusCode, latency, forwardErr, snap)
			}

			if forwardErr == nil && statusCode < http.StatusBadRequest {
				break attemptLoop
			}

			if !shouldRetryAttempt(ctx, snap, statusCode, finalErrorMessage, forwardErr) || (maxAttempts != 0 && attempts >= maxAttempts) {
				break attemptLoop
			}

			if reqMeta.Stream && maxAttempts == 0 && streamRetry == nil {
				streamRetry = startStreamRetrySession(w)
			}

			if finalStreamBody != nil {
				_ = finalStreamBody.Close()
				finalStreamBody = nil
			}
			waitRetryBackoff(ctx, snap, attempts-1)
		}
	}

	if finalProvider == nil {
		if streamRetry == nil && (opts == nil || !opts.DisableFallback) && tryFallbackModels(ctx, snap, runtimeState, telClient, pricingResolver, w, r, clientFmt, reqMeta, body, requestID, start, opts) {
			return
		}
		writeError(w, http.StatusBadGateway, "no provider available")
		return
	}
	if runtimeState != nil && finalForwardErr == nil && finalStatusCode < http.StatusBadRequest {
		runtimeState.rememberSticky(routingModel, stickyKey, finalProvider.ProviderID, snap)
	}

	if finalForwardErr != nil || finalStatusCode >= http.StatusBadRequest {
		// #15: when forwardErr is nil but status is non-2xx we still need
		// the upstream body in the log so operators can diagnose without
		// having to re-attach a packet capture.
		if finalForwardErr == nil && finalStatusCode >= http.StatusBadRequest {
			bodySnippet := string(finalRespBody)
			if len(bodySnippet) > 512 {
				bodySnippet = bodySnippet[:512] + "..."
			}
			log.Printf("[gatewayd] request_id=%s upstream error: status=%d body=%q",
				requestID, finalStatusCode, bodySnippet)
		} else {
			log.Printf("[gatewayd] request_id=%s upstream error: status=%d err=%v", requestID, finalStatusCode, finalForwardErr)
		}

		// Attempt fallback models before returning the error to the client.
		if streamRetry == nil && (opts == nil || !opts.DisableFallback) && tryFallbackModels(ctx, snap, runtimeState, telClient, pricingResolver, w, r, clientFmt, reqMeta, body, requestID, start, opts) {
			return
		}

		errorBody := finalRespBody
		if len(errorBody) > 0 {
			if adapted, contentType, adaptErr := adaptResponseBodyForClient(finalCompatPlan, finalStatusCode, errorBody); adaptErr == nil {
				errorBody = adapted
				if contentType != "" {
					finalContentType = contentType
				}
			}
		}

		attemptRouteMode := routeModeForAttempt(routeMode, false, clientFmt == formatAnthropic, finalProvider)
		emitTelemetry(telClient, requestID, start, r.URL.Path, requestedModel, finalEffectiveModel, finalProvider.ProviderID,
			attemptRouteMode, finalStatusCode, finalLatency, attempts, 0, 0, 0, reqMeta.Stream, finalErrorMessage,
			resolveFixedPricing(pricingResolver, requestedModel, finalEffectiveModel, finalProvider.ProviderID, 0, 0, 0, false, finalStatusCode), opts)
		captureExecutionResult(opts, finalStatusCode, finalContentType, finalLatency, 0, 0, 0, finalProvider.ProviderID, finalEffectiveModel, attemptRouteMode, 0, finalErrorMessage)

		if streamRetry != nil {
			streamRetry.Stop()
			writeStreamErrorEvent(w, finalStatusCode, finalErrorMessage)
			return
		}

		if len(errorBody) > 0 {
			if strings.TrimSpace(finalContentType) == "" {
				finalContentType = "application/json"
			}
			w.Header().Set("Content-Type", finalContentType)
			w.WriteHeader(finalStatusCode)
			_, _ = w.Write(errorBody)
		} else {
			writeError(w, finalStatusCode, finalErrorMessage)
		}
		return
	}

	var promptTokens, cachedPromptTokens, completionTokens int64

	// Handle streaming response
	if reqMeta.Stream && finalStreamBody != nil {
		if streamRetry != nil {
			streamRetry.Stop()
			promptTokens, cachedPromptTokens, completionTokens = writeCompatStreamResponseStarted(w, streamRetry.flusher, finalStreamBody, finalCompatPlan)
		} else {
			promptTokens, cachedPromptTokens, completionTokens = writeCompatStreamResponse(w, finalStatusCode, finalContentType, finalStreamBody, finalCompatPlan)
		}
	} else {
		clientRespBody, clientContentType, adaptErr := adaptResponseBodyForClient(finalCompatPlan, finalStatusCode, finalRespBody)
		if adaptErr != nil {
			writeError(w, http.StatusBadGateway, "failed to adapt upstream response")
			attemptRouteMode := routeModeForAttempt(routeMode, false, clientFmt == formatAnthropic, finalProvider)
			emitTelemetry(telClient, requestID, start, r.URL.Path, requestedModel, finalEffectiveModel, finalProvider.ProviderID,
				attemptRouteMode, http.StatusBadGateway, finalLatency, attempts, 0, 0, 0, reqMeta.Stream, adaptErr.Error(),
				resolveFixedPricing(pricingResolver, requestedModel, finalEffectiveModel, finalProvider.ProviderID, 0, 0, 0, false, http.StatusBadGateway), opts)
			captureExecutionResult(opts, http.StatusBadGateway, "application/json", finalLatency, 0, 0, 0, finalProvider.ProviderID, finalEffectiveModel, attemptRouteMode, 0, adaptErr.Error())
			return
		}
		// Non-streaming: pass through response
		if strings.TrimSpace(clientContentType) == "" {
			clientContentType = "application/json"
		}
		finalContentType = clientContentType
		w.Header().Set("Content-Type", clientContentType)
		w.WriteHeader(finalStatusCode)
		_, _ = w.Write(clientRespBody)

		// Extract usage from response
		promptTokens, cachedPromptTokens, completionTokens = extractUsage(clientRespBody)

		// Store successful response in cache.
		if snap.RoutingPolicy.Cache.Enabled && finalStatusCode < 400 && (opts == nil || !opts.DisableCache) {
			c := getResponseCache(snap)
			cacheKey := c.MakeKey(body, reqMeta.Model, stickyKey)
			c.Put(cacheKey, clientRespBody)
		}
	}

	// Emit telemetry
	fixedPricing := resolveFixedPricing(pricingResolver, requestedModel, finalEffectiveModel, finalProvider.ProviderID, promptTokens, cachedPromptTokens, completionTokens, false, finalStatusCode)
	attemptRouteMode := routeModeForAttempt(routeMode, false, clientFmt == formatAnthropic, finalProvider)
	emitTelemetry(telClient, requestID, start, r.URL.Path, requestedModel, finalEffectiveModel, finalProvider.ProviderID,
		attemptRouteMode, finalStatusCode, finalLatency, attempts, promptTokens, cachedPromptTokens, completionTokens, reqMeta.Stream, "", fixedPricing, opts)
	captureExecutionResult(opts, finalStatusCode, finalContentType, finalLatency, promptTokens, cachedPromptTokens, completionTokens, finalProvider.ProviderID, finalEffectiveModel, attemptRouteMode, fixedPricing.TotalCostUSD, "")
}

// emitTelemetry emits a telemetry event for a completed request.
func emitTelemetry(telClient TelemetryEmitter, requestID string, start time.Time, path, requestedModel, effectiveModel, providerID, routeMode string,
	statusCode int, latency time.Duration, attempts int, promptTokens, cachedPromptTokens, completionTokens int64, stream bool, errMsg string, fixedPricing FixedPricing, opts *ExecutionOptions) {

	if telClient == nil {
		return
	}

	event := telemetryingest.Event{
		EventID:       uuid.New().String(),
		EventType:     "gateway.attempt.completed",
		SchemaVersion: 1,
		SourceService: "gatewayd",
		EmittedAt:     time.Now(),
		Payload: telemetryingest.EventPayload{
			RequestID:                requestID,
			Timestamp:                start,
			Path:                     path,
			RequestedModel:           requestedModel,
			EffectiveModel:           effectiveModel,
			ProviderID:               providerID,
			RouteMode:                routeMode,
			StatusCode:               statusCode,
			Latency:                  latency,
			Attempts:                 attempts,
			PromptTokens:             promptTokens,
			CachedPromptTokens:       cachedPromptTokens,
			CompletionTokens:         completionTokens,
			PricingStatus:            fixedPricing.Status,
			PricingSourceID:          fixedPricing.SourceID,
			PricingCurrency:          fixedPricing.Currency,
			PricingFXRateToUSD:       fixedPricing.FXRateToUSD,
			PricingInputPer1M:        fixedPricing.InputPer1M,
			PricingCachedInputPer1M:  fixedPricing.CachedInputPer1M,
			PricingOutputPer1M:       fixedPricing.OutputPer1M,
			PricingPromptCost:        fixedPricing.PromptCost,
			PricingCompletionCost:    fixedPricing.CompletionCost,
			PricingTotalCost:         fixedPricing.TotalCost,
			PricingPromptCostUSD:     fixedPricing.PromptCostUSD,
			PricingCompletionCostUSD: fixedPricing.CompletionCostUSD,
			PricingTotalCostUSD:      fixedPricing.TotalCostUSD,
			SyntheticKind:            syntheticKindFromOptions(opts),
			BenchmarkRunID:           benchmarkRunIDFromOptions(opts),
			BenchmarkTargetID:        benchmarkTargetIDFromOptions(opts),
			BenchmarkCaseID:          benchmarkCaseIDFromOptions(opts),
			Stream:                   stream,
			Error:                    errMsg,
		},
	}
	if err := telClient.Emit(event); err != nil {
		log.Printf("[gatewayd] telemetry emit error: %v", err)
	}
}

// TelemetryEmitter is the interface for emitting telemetry events.
type TelemetryEmitter interface {
	Emit(event telemetryingest.Event) error
}

// ChatCompletionRequest represents a chat completion request.
type ChatCompletionRequest struct {
	Model            string             `json:"model"`
	Messages         []Message          `json:"messages"`
	MaxTokens        int                `json:"max_tokens,omitempty"`
	Temperature      float64            `json:"temperature,omitempty"`
	TopP             float64            `json:"top_p,omitempty"`
	N                int                `json:"n,omitempty"`
	Stream           bool               `json:"stream,omitempty"`
	Stop             interface{}        `json:"stop,omitempty"`
	PresencePenalty  float64            `json:"presence_penalty,omitempty"`
	FrequencyPenalty float64            `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`
	User             string             `json:"user,omitempty"`
}

type chatCompletionRequestMeta struct {
	Model         string `json:"model"`
	Stream        bool   `json:"stream,omitempty"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage,omitempty"`
	} `json:"stream_options,omitempty"`
	User           string `json:"user,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

func parseChatCompletionRequestMeta(body []byte) (chatCompletionRequestMeta, error) {
	var meta chatCompletionRequestMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return chatCompletionRequestMeta{}, err
	}
	return meta, nil
}

func resolveStickyKey(meta chatCompletionRequestMeta, headers http.Header) string {
	if value := firstHeaderValue(headers, "X-Sticky-Key", "X-Session-ID", "X-Client-Session", "OpenAI-Conversation-ID"); value != "" {
		return value
	}
	for _, candidate := range []string{meta.SessionID, meta.ConversationID, meta.User} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func firstHeaderValue(headers http.Header, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

// Message represents a chat message.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ChatCompletionResponse represents a chat completion response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// findProvider finds a provider for the given model.
func collectProviderCandidates(snap *snapshot.Snapshot, model string) []providerCandidate {
	if snap == nil {
		return nil
	}
	candidates := make([]providerCandidate, 0, len(snap.Providers))
	for i := range snap.Providers {
		p := &snap.Providers[i]
		if !p.ExecutionPolicy.Enabled {
			continue
		}
		for _, m := range p.ModelTable {
			if m.PublicModel == model {
				candidates = append(candidates, providerCandidate{
					provider:      p,
					upstreamModel: m.UpstreamModel,
					weight:        normalizeWeight(p.ExecutionPolicy.Weight),
				})
				break
			}
		}
	}
	return candidates
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// writeError writes an error response in OpenAI-compatible envelope format:
//   { "error": { "message": ..., "type": ..., "param": null, "code": null } }
// Fixes #7 (gateway returned the wrong shape: a flat string under "error").
func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorWithType(w, status, message, "")
}

// writeErrorWithType is the typed variant of writeError.
func writeErrorWithType(w http.ResponseWriter, status int, message string, errType string) {
	if strings.TrimSpace(errType) == "" {
		switch {
		case status == http.StatusUnauthorized:
			errType = "authentication_error"
		case status == http.StatusNotFound:
			errType = "invalid_request_error"
		case status >= 500:
			errType = "upstream_error"
		default:
			errType = "invalid_request_error"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
			"param":   nil,
			"code":    nil,
		},
	})
}

func writeStreamErrorEvent(w http.ResponseWriter, status int, message string) {
	if strings.TrimSpace(message) == "" {
		message = "upstream request failed"
	}
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"status":  status,
		},
	})
	if err != nil {
		payload = []byte(`{"error":{"message":"upstream request failed"}}`)
	}
	_, _ = w.Write([]byte("event: error\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
