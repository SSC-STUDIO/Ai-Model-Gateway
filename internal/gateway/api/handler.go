// Package api provides HTTP handlers for gatewayd.
package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/cache"
	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/proxy"

	"github.com/google/uuid"
)

// sharedHTTPClient is a reusable HTTP client for upstream requests.
var sharedHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
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
	},
	Timeout: 0, // per-request timeout set via context
}

type urlValidator interface {
	ValidateURL(rawURL string) error
}

var ssrfChecker urlValidator = proxy.NewSSRFChecker()
var routingSequence atomic.Uint64

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
	handleChatOrMessages(ctx, snap, runtimeState, telClient, pricingResolver, w, r, false)
}

// HandleMessages handles an Anthropic Messages API request.
func HandleMessages(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request) {
	handleChatOrMessages(ctx, snap, runtimeState, telClient, pricingResolver, w, r, true)
}

// handleChatOrMessages handles both chat completion and messages requests.
func handleChatOrMessages(ctx context.Context, snap *snapshot.Snapshot, runtimeState *RuntimeState, telClient TelemetryEmitter, pricingResolver PricingResolver, w http.ResponseWriter, r *http.Request, isAnthropic bool) {
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

	// Check request cache for non-streaming requests.
	if !reqMeta.Stream && snap.RoutingPolicy.Cache.Enabled && (opts == nil || !opts.DisableCache) {
		c := getResponseCache(snap)
		cacheKey := c.MakeKey(body, reqMeta.Model)
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

	stickyKey := resolveStickyKey(reqMeta, r.Header)
	if opts != nil && opts.DisableSticky {
		stickyKey = ""
	}
	candidates, unsupportedMatches := collectProviderCandidatesForRequest(snap, reqMeta.Model, isAnthropic)
	if len(candidates) == 0 {
		if isAnthropic && unsupportedMatches {
			writeError(w, http.StatusNotImplemented, errMessagesAPIRequiresAnthropicProvider.Error())
			return
		}
		writeError(w, http.StatusNotFound, "model not found: "+reqMeta.Model)
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
		orderedCandidates = runtimeState.orderCandidates(snap, reqMeta.Model, stickyKey, candidates)
	} else {
		orderedCandidates = orderProviderCandidates(candidates)
	}
	routeMode := determineRouteMode(orderedCandidates, snap)
	maxAttempts := maxTotalAttempts(snap)
	if opts != nil && opts.DisableRetries {
		maxAttempts = 1
	}

	var (
		attempts            int
		finalStatusCode     = http.StatusBadGateway
		finalRespBody       []byte
		finalStreamBody     io.ReadCloser
		finalContentType    string
		finalLatency        time.Duration
		finalForwardErr     error
		finalProvider       *snapshot.ProviderSnapshot
		finalEffectiveModel = reqMeta.Model
		finalErrorMessage   string
		finalCompatPlan     compatPlan
	)

attemptLoop:
	for i := range orderedCandidates {
		candidate := orderedCandidates[i]
		compatPlan, compatErr := buildCompatPlan(isAnthropic, candidate.provider, reqMeta.Model, candidate.upstreamModel, body)
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
		for providerAttempt := 0; providerAttempt < sameProviderAttempts && attempts < maxAttempts; providerAttempt++ {
			attempts++

			log.Printf("[gatewayd] request_id=%s model=%s upstream_model=%s provider=%s attempt=%d/%d",
				requestID, reqMeta.Model, candidate.upstreamModel, candidate.provider.ProviderID, attempts, maxAttempts)

			statusCode, respBody, streamBody, streamContentType, latency, forwardErr := forwardToUpstream(
				ctx,
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

			if !shouldRetryAttempt(ctx, snap, statusCode, finalErrorMessage, forwardErr) || attempts >= maxAttempts {
				break attemptLoop
			}

			if finalStreamBody != nil {
				_ = finalStreamBody.Close()
				finalStreamBody = nil
			}
			waitRetryBackoff(ctx, snap, attempts-1)
		}
	}

	if finalProvider == nil {
		writeError(w, http.StatusBadGateway, "no provider available")
		return
	}
	if runtimeState != nil && finalForwardErr == nil && finalStatusCode < http.StatusBadRequest {
		runtimeState.rememberSticky(reqMeta.Model, stickyKey, finalProvider.ProviderID, snap)
	}

	if finalForwardErr != nil || finalStatusCode >= http.StatusBadRequest {
		log.Printf("[gatewayd] request_id=%s upstream error: status=%d err=%v", requestID, finalStatusCode, finalForwardErr)

		// Attempt fallback models before returning the error to the client.
		if (opts == nil || !opts.DisableFallback) && tryFallbackModels(ctx, snap, runtimeState, telClient, pricingResolver, w, r, isAnthropic, reqMeta, body, requestID, start, opts) {
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

		emitTelemetry(telClient, requestID, start, r.URL.Path, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID,
			routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), finalStatusCode, finalLatency, attempts, 0, 0, 0, reqMeta.Stream, finalErrorMessage,
			resolveFixedPricing(pricingResolver, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID, 0, 0, 0, false, finalStatusCode), opts)
		captureExecutionResult(opts, finalStatusCode, finalContentType, finalLatency, 0, 0, 0, finalProvider.ProviderID, finalEffectiveModel, routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), 0, finalErrorMessage)

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
		promptTokens, cachedPromptTokens, completionTokens = writeCompatStreamResponse(w, finalStatusCode, finalContentType, finalStreamBody, finalCompatPlan)
	} else {
		clientRespBody, clientContentType, adaptErr := adaptResponseBodyForClient(finalCompatPlan, finalStatusCode, finalRespBody)
		if adaptErr != nil {
			writeError(w, http.StatusBadGateway, "failed to adapt upstream response")
			emitTelemetry(telClient, requestID, start, r.URL.Path, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID,
				routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), http.StatusBadGateway, finalLatency, attempts, 0, 0, 0, reqMeta.Stream, adaptErr.Error(),
				resolveFixedPricing(pricingResolver, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID, 0, 0, 0, false, http.StatusBadGateway), opts)
			captureExecutionResult(opts, http.StatusBadGateway, "application/json", finalLatency, 0, 0, 0, finalProvider.ProviderID, finalEffectiveModel, routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), 0, adaptErr.Error())
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
			cacheKey := c.MakeKey(body, reqMeta.Model)
			c.Put(cacheKey, clientRespBody)
		}
	}

	// Emit telemetry
	fixedPricing := resolveFixedPricing(pricingResolver, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID, promptTokens, cachedPromptTokens, completionTokens, false, finalStatusCode)
	emitTelemetry(telClient, requestID, start, r.URL.Path, reqMeta.Model, finalEffectiveModel, finalProvider.ProviderID,
		routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), finalStatusCode, finalLatency, attempts, promptTokens, cachedPromptTokens, completionTokens, reqMeta.Stream, "", fixedPricing, opts)
	captureExecutionResult(opts, finalStatusCode, finalContentType, finalLatency, promptTokens, cachedPromptTokens, completionTokens, finalProvider.ProviderID, finalEffectiveModel, routeModeForAttempt(routeMode, false, isAnthropic, finalProvider), fixedPricing.TotalCostUSD, "")
}

// forwardToUpstream forwards the request to the upstream provider.
func forwardToUpstream(ctx context.Context, provider *snapshot.ProviderSnapshot, path string, body []byte, stream bool, origHeaders http.Header, isAnthropic bool) (statusCode int, respBody []byte, streamBody io.ReadCloser, streamContentType string, latency time.Duration, err error) {
	// Build upstream URL
	var upstreamURL string
	if isAnthropic && provider.AnthropicBaseURL != "" {
		// Use Anthropic-specific base URL for /v1/messages
		upstreamURL = strings.TrimRight(provider.AnthropicBaseURL, "/") + path
	} else {
		upstreamURL = strings.TrimRight(provider.BaseURL, "/") + path
	}

	// SSRF check
	if err := ssrfChecker.ValidateURL(upstreamURL); err != nil {
		return http.StatusBadGateway, nil, nil, "", 0, err
	}

	// Create upstream request
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	timeout := time.Duration(provider.ExecutionPolicy.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, upstreamURL, bodyReader)
	if err != nil {
		cancel()
		return http.StatusInternalServerError, nil, nil, "", 0, err
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	// Try key rotation first, then fall back to static credentials
	kr := NewKeyRotator(provider)
	if keyValue := kr.Next(); keyValue != "" {
		if provider.Credentials.Kind == "bearer" {
			httpReq.Header.Set("Authorization", "Bearer "+keyValue)
		} else {
			headerName := provider.Credentials.HeaderName
			if headerName == "" {
				headerName = "x-api-key"
			}
			httpReq.Header.Set(headerName, keyValue)
		}
	} else if provider.Credentials.Kind == "bearer" && provider.Credentials.Value != "" {
		httpReq.Header.Set("Authorization", "Bearer "+provider.Credentials.Value)
	} else if provider.Credentials.Kind == "api_key" && provider.Credentials.Value != "" {
		headerName := provider.Credentials.HeaderName
		if headerName == "" {
			headerName = "x-api-key"
		}
		httpReq.Header.Set(headerName, provider.Credentials.Value)
	}
	for k, v := range provider.Headers {
		httpReq.Header.Set(k, v)
	}

	// Add anthropic-version header for Anthropic API
	if isAnthropic {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	// Forward select original headers
	if ua := origHeaders.Get("User-Agent"); ua != "" {
		httpReq.Header.Set("User-Agent", ua)
	}

	// Execute
	start := time.Now()
	resp, err := sharedHTTPClient.Do(httpReq)
	latency = time.Since(start)

	if err != nil {
		cancel()
		if reqCtx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return http.StatusGatewayTimeout, nil, nil, "", latency, err
		}
		return http.StatusBadGateway, nil, nil, "", latency, err
	}

	// Keep successful SSE bodies open so the caller can stream them through.
	if stream && resp.StatusCode < http.StatusBadRequest && isSSE(resp) {
		return resp.StatusCode, nil, cancelOnClose(resp.Body, cancel), resp.Header.Get("Content-Type"), latency, nil
	}

	defer cancel()
	defer resp.Body.Close()

	// Read response body (with size limit)
	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if readErr != nil {
		return resp.StatusCode, nil, nil, "", latency, readErr
	}

	return resp.StatusCode, respBytes, nil, "", latency, nil
}

// handleStreamResponse writes a streaming response.
func handleStreamResponse(w http.ResponseWriter, statusCode int, contentType string, respBody io.ReadCloser) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if strings.TrimSpace(contentType) == "" {
		contentType = "text/event-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(statusCode)

	flusher, _ := w.(http.Flusher)
	return copyStreamingBody(w, respBody, flusher)
}

func copyStreamingBody(w http.ResponseWriter, body io.ReadCloser, flusher http.Flusher) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if body == nil {
		return 0, 0, 0
	}
	defer body.Close()

	reader := bufio.NewReader(body)
	var eventData bytes.Buffer
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = w.Write(line)
			if flusher != nil {
				flusher.Flush()
			}

			trimmedLine := strings.TrimRight(string(line), "\r\n")
			if strings.TrimSpace(trimmedLine) == "" {
				if pt, cpt, ct, ok := extractUsageFromSSEEvent(eventData.Bytes()); ok {
					promptTokens, cachedPromptTokens, completionTokens = pt, cpt, ct
				}
				eventData.Reset()
			} else if strings.HasPrefix(trimmedLine, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:"))
				if eventData.Len() > 0 {
					eventData.WriteByte('\n')
				}
				eventData.WriteString(data)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if pt, cpt, ct, ok := extractUsageFromSSEEvent(eventData.Bytes()); ok {
					promptTokens, cachedPromptTokens, completionTokens = pt, cpt, ct
				}
			}
			return promptTokens, cachedPromptTokens, completionTokens
		}
	}
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelOnCloseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

func cancelOnClose(body io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	if body == nil {
		return nil
	}
	return &cancelOnCloseReadCloser{ReadCloser: body, cancel: cancel}
}

// isSSE checks if the response is a Server-Sent Events stream.
func isSSE(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/event-stream")
}

// resolveUpstreamModel resolves the upstream model name from the public model name.
func resolveUpstreamModel(provider *snapshot.ProviderSnapshot, publicModel string) string {
	for _, m := range provider.ModelTable {
		if m.PublicModel == publicModel {
			return m.UpstreamModel
		}
	}
	return publicModel
}

// rewriteModelInBody replaces the model name in the JSON body using proper parsing.
func rewriteModelInBody(body []byte, oldModel, newModel string) []byte {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		// Fallback: if body isn't valid JSON, return as-is
		return body
	}

	// Only rewrite if the model field matches exactly
	if modelRaw, ok := raw["model"]; ok {
		var currentModel string
		if err := json.Unmarshal(modelRaw, &currentModel); err == nil && currentModel == oldModel {
			raw["model"], _ = json.Marshal(newModel)
			result, err := json.Marshal(raw)
			if err != nil {
				return body
			}
			return result
		}
	}

	return body
}

// extractUsage extracts token usage from the response body.
func extractUsage(respBody []byte) (promptTokens, cachedPromptTokens, completionTokens int64) {
	if len(respBody) == 0 {
		return 0, 0, 0
	}

	var payload struct {
		Usage struct {
			PromptTokens             int64 `json:"prompt_tokens"`
			CompletionTokens         int64 `json:"completion_tokens"`
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			PromptDetails            struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, 0, 0
	}

	promptTokens = payload.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = payload.Usage.InputTokens
	}
	if payload.Usage.CacheReadInputTokens > 0 || payload.Usage.CacheCreationInputTokens > 0 {
		promptTokens = payload.Usage.InputTokens + payload.Usage.CacheCreationInputTokens + payload.Usage.CacheReadInputTokens
	}
	completionTokens = payload.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = payload.Usage.OutputTokens
	}
	cachedPromptTokens = payload.Usage.PromptDetails.CachedTokens
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.InputDetails.CachedTokens
	}
	if cachedPromptTokens == 0 {
		cachedPromptTokens = payload.Usage.CacheReadInputTokens
	}
	if promptTokens < cachedPromptTokens {
		promptTokens = cachedPromptTokens
	}

	return promptTokens, cachedPromptTokens, completionTokens
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
	Model          string `json:"model"`
	Stream         bool   `json:"stream,omitempty"`
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

func orderProviderCandidates(candidates []providerCandidate) []providerCandidate {
	if len(candidates) <= 1 {
		return append([]providerCandidate(nil), candidates...)
	}

	weightedPool := make([]int, 0, len(candidates))
	for idx, candidate := range candidates {
		for repeat := 0; repeat < normalizeWeight(candidate.weight); repeat++ {
			weightedPool = append(weightedPool, idx)
		}
	}
	if len(weightedPool) == 0 {
		return append([]providerCandidate(nil), candidates...)
	}

	start := int((routingSequence.Add(1) - 1) % uint64(len(weightedPool)))
	ordered := make([]providerCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for offset := 0; offset < len(weightedPool) && len(ordered) < len(candidates); offset++ {
		candidate := candidates[weightedPool[(start+offset)%len(weightedPool)]]
		if _, exists := seen[candidate.provider.ProviderID]; exists {
			continue
		}
		seen[candidate.provider.ProviderID] = struct{}{}
		ordered = append(ordered, candidate)
	}
	return ordered
}

func determineRouteMode(candidates []providerCandidate, snap *snapshot.Snapshot) string {
	if len(candidates) <= 1 && maxTotalAttempts(snap) <= 1 {
		return "direct"
	}
	if snap != nil {
		if strategy := strings.TrimSpace(snap.RoutingPolicy.Strategy); strategy != "" {
			return strategy
		}
	}
	return "weighted_failover"
}

func routeModeForAttempt(defaultMode string, usedFallback bool, clientAnthropic bool, provider *snapshot.ProviderSnapshot) string {
	if !clientAnthropic && providerProtocolAdapter(provider) == core.ProtocolAdapterAnthropicMessages {
		if usedFallback {
			return "bridge_fallback"
		}
		return "bridged"
	}
	if usedFallback {
		return "model_fallback"
	}
	return defaultMode
}

func maxTotalAttempts(snap *snapshot.Snapshot) int {
	if snap == nil {
		return 1
	}
	return 1 + maxInt(snap.RoutingPolicy.MaxRetries, 0)
}

func shouldRetryAttempt(ctx context.Context, snap *snapshot.Snapshot, statusCode int, errMsg string, forwardErr error) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if forwardErr != nil {
		return true
	}
	if snap == nil {
		return false
	}
	for _, code := range snap.RoutingPolicy.Retry.StatusCodes {
		if statusCode == code {
			return true
		}
	}
	if minCode := snap.RoutingPolicy.Retry.StatusCodeMin; minCode > 0 && statusCode >= minCode {
		return true
	}
	if errMsg != "" {
		lowerMsg := strings.ToLower(errMsg)
		for _, keyword := range snap.RoutingPolicy.Retry.MessageKeywords {
			if keyword != "" && strings.Contains(lowerMsg, strings.ToLower(keyword)) {
				return true
			}
		}
	}
	return false
}

func waitRetryBackoff(ctx context.Context, snap *snapshot.Snapshot, retryIndex int) {
	if snap == nil {
		return
	}
	delay := time.Duration(snap.RoutingPolicy.RetryBackoff.InitialMs) * time.Millisecond
	if delay <= 0 {
		return
	}
	for i := 0; i < retryIndex; i++ {
		delay *= 2
		maxDelay := time.Duration(snap.RoutingPolicy.RetryBackoff.MaxMs) * time.Millisecond
		if maxDelay > 0 && delay >= maxDelay {
			delay = maxDelay
			break
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func extractUsageFromSSEEvent(data []byte) (promptTokens, cachedPromptTokens, completionTokens int64, ok bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[DONE]" {
		return 0, 0, 0, false
	}
	promptTokens, cachedPromptTokens, completionTokens = extractUsage([]byte(trimmed))
	if promptTokens == 0 && cachedPromptTokens == 0 && completionTokens == 0 {
		var payload struct {
			Message struct {
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			promptTokens, cachedPromptTokens, completionTokens = payload.Message.Usage.tokenTriplet()
		}
	}
	if promptTokens == 0 && cachedPromptTokens == 0 && completionTokens == 0 {
		return 0, 0, 0, false
	}
	return promptTokens, cachedPromptTokens, completionTokens, true
}

func extractErrorMessage(respBody []byte, forwardErr error) string {
	if forwardErr != nil {
		return forwardErr.Error()
	}
	if len(respBody) == 0 {
		return ""
	}
	var payload struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(respBody, &payload); err == nil {
		switch value := payload.Error.(type) {
		case string:
			if value != "" {
				return value
			}
		case map[string]any:
			if message, ok := value["message"].(string); ok && message != "" {
				return message
			}
		}
	}
	message := strings.TrimSpace(string(respBody))
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func normalizeWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

type providerCandidate struct {
	provider      *snapshot.ProviderSnapshot
	upstreamModel string
	weight        int
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
