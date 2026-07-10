package api

import (
	"context"
	"net/http"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
	"ai-model-gateway/internal/infra/logger"
)

// ResolveFallbackModels returns fallback model names for the given primary model.
// It scans all providers in the snapshot and collects FallbackModels entries from
// any provider that serves the requested model, preserving order and removing
// duplicates. It also filters out the primary model to prevent infinite loops.
func ResolveFallbackModels(snap *snapshot.Snapshot, model string) []string {
	if snap == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var fallbacks []string
	for i := range snap.Providers {
		p := &snap.Providers[i]
		matched := false
		for _, m := range p.ModelTable {
			if m.PublicModel == model {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, fb := range p.FallbackModels {
			if fb == "" {
				continue
			}
			// Prevent infinite loop: skip if fallback equals primary model
			if fb == model {
				continue
			}
			if _, ok := seen[fb]; ok {
				continue
			}
			seen[fb] = struct{}{}
			fallbacks = append(fallbacks, fb)
		}
	}
	return fallbacks
}

// maxFallbackAttempts is the hard upper bound on total fallback upstream requests
// across all fallback models. Prevents an unbounded fallback loop from holding
// the client connection indefinitely.
const maxFallbackAttempts = 10

// tryFallbackModels attempts to route to fallback models when all primary providers
// have failed. It rewrites the request body with each fallback model and tries the
// normal candidate collection and forwarding path.
//
// Returns true if a fallback model was successfully served (the response has been
// written to w). Returns false if all fallbacks also failed (nothing written to w).
func tryFallbackModels(
	ctx context.Context,
	snap *snapshot.Snapshot,
	runtimeState *RuntimeState,
	telClient TelemetryEmitter,
	pricingResolver PricingResolver,
	w http.ResponseWriter,
	r *http.Request,
	clientFmt clientFormat,
	reqMeta chatCompletionRequestMeta,
	body []byte,
	requestID string,
	start time.Time,
	opts *ExecutionOptions,
) bool {
	fallbacks := ResolveFallbackModels(snap, reqMeta.Model)
	if len(fallbacks) == 0 {
		return false
	}

	retryBudget := time.Duration(snap.RoutingPolicy.Retry.MaxElapsedMs) * time.Millisecond
	fallbackAttempts := 0

	for _, fallbackModel := range fallbacks {
		// Check context cancellation and retry budget before each fallback model.
		if ctx.Err() != nil {
			return false
		}
		if retryBudget > 0 && time.Since(start) >= retryBudget {
			logger.Info("fallback retry budget exhausted", "request_id", requestID, "elapsed", time.Since(start))
			return false
		}

		candidates := collectProviderCandidatesForRequest(snap, fallbackModel)
		if len(candidates) == 0 {
			continue
		}

		var orderedCandidates []providerCandidate
		if runtimeState != nil {
			orderedCandidates = runtimeState.orderCandidates(snap, fallbackModel, "", candidates)
		} else {
			orderedCandidates = orderProviderCandidates(candidates)
		}

		for i := range orderedCandidates {
			// Check context cancellation, retry budget, and hard cap between each candidate.
			if ctx.Err() != nil {
				return false
			}
			if retryBudget > 0 && time.Since(start) >= retryBudget {
				logger.Info("fallback retry budget exhausted", "request_id", requestID, "elapsed", time.Since(start))
				return false
			}
			if fallbackAttempts >= maxFallbackAttempts {
				logger.Info("fallback hard cap reached", "request_id", requestID, "attempts", fallbackAttempts)
				return false
			}
			candidate := orderedCandidates[i]

			logger.Info("fallback request attempt", "request_id", requestID, "model", fallbackModel, "upstream_model", candidate.upstreamModel, "provider", candidate.provider.ProviderID)

			fallbackAttempts++

			compatPlan, compatErr := buildCompatPlan(clientFmt, candidate.provider, reqMeta.Model, candidate.upstreamModel, body)
			if compatErr != nil {
				logger.Warn("fallback compat failed", "request_id", requestID, "model", fallbackModel, "provider", candidate.provider.ProviderID, "error", compatErr)
				continue
			}

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

			if runtimeState != nil {
				runtimeState.reportAttemptResult(candidate.provider.ProviderID, statusCode, latency, forwardErr, snap)
			}

			if forwardErr != nil || statusCode >= http.StatusBadRequest {
				if streamBody != nil {
					_ = streamBody.Close()
				}
				logger.Warn("fallback failed", "request_id", requestID, "model", fallbackModel, "status", statusCode, "error", forwardErr)
				continue
			}

			// Fallback succeeded -- write the response.
			var promptTokens, cachedPromptTokens, completionTokens int64
			capturedContentType := streamContentType
			if reqMeta.Stream && streamBody != nil {
				promptTokens, cachedPromptTokens, completionTokens = writeCompatStreamResponse(w, statusCode, streamContentType, streamBody, compatPlan)
			} else {
				clientRespBody, clientContentType, adaptErr := adaptResponseBodyForClient(compatPlan, statusCode, respBody)
				if adaptErr != nil {
					logger.Warn("fallback adapt failed", "request_id", requestID, "model", fallbackModel, "provider", candidate.provider.ProviderID, "error", adaptErr)
					continue
				}
				if clientContentType == "" {
					clientContentType = "application/json"
				}
				capturedContentType = clientContentType
				w.Header().Set("Content-Type", clientContentType)
				w.WriteHeader(statusCode)
				_, _ = w.Write(clientRespBody)
				promptTokens, cachedPromptTokens, completionTokens = extractUsage(clientRespBody)
			}

			// Emit a telemetry event for the successful fallback attempt.
			fixedPricing := resolveFixedPricing(pricingResolver, reqMeta.Model, candidate.upstreamModel, candidate.provider.ProviderID, promptTokens, cachedPromptTokens, completionTokens, false, statusCode)
			emitTelemetry(telClient, requestID, start, r.URL.Path,
				reqMeta.Model, candidate.upstreamModel, candidate.provider.ProviderID,
				routeModeForAttempt("model_fallback", true, clientFmt == formatAnthropic, candidate.provider), statusCode, latency, 1, promptTokens, cachedPromptTokens, completionTokens, reqMeta.Stream, "",
				fixedPricing, opts)
			captureExecutionResult(opts, statusCode, capturedContentType, latency, promptTokens, cachedPromptTokens, completionTokens, candidate.provider.ProviderID, candidate.upstreamModel, routeModeForAttempt("model_fallback", true, clientFmt == formatAnthropic, candidate.provider), fixedPricing.TotalCostUSD, "")

			logger.Info("fallback succeeded", "request_id", requestID, "model", fallbackModel, "provider", candidate.provider.ProviderID, "latency", latency)
			return true
		}
	}

	return false
}
