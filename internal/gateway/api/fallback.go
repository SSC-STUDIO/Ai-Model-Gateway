package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
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
	w http.ResponseWriter,
	r *http.Request,
	isAnthropic bool,
	reqMeta chatCompletionRequestMeta,
	body []byte,
	requestID string,
	start time.Time,
) bool {
	fallbacks := ResolveFallbackModels(snap, reqMeta.Model)
	if len(fallbacks) == 0 {
		return false
	}

	for _, fallbackModel := range fallbacks {
		candidates := collectProviderCandidates(snap, fallbackModel)
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
			candidate := orderedCandidates[i]

			log.Printf("[gatewayd] request_id=%s fallback model=%s upstream_model=%s provider=%s",
				requestID, fallbackModel, candidate.upstreamModel, candidate.provider.ProviderID)

			fbBody := rewriteModelInBody(body, reqMeta.Model, candidate.upstreamModel)

			statusCode, respBody, streamBody, streamContentType, latency, forwardErr := forwardToUpstream(
				ctx,
				candidate.provider,
				r.URL.Path,
				fbBody,
				reqMeta.Stream,
				r.Header,
				isAnthropic,
			)

			if runtimeState != nil {
				runtimeState.reportAttemptResult(candidate.provider.ProviderID, statusCode, latency, forwardErr, snap)
			}

			if forwardErr != nil || statusCode >= http.StatusBadRequest {
				if streamBody != nil {
					_ = streamBody.Close()
				}
				log.Printf("[gatewayd] request_id=%s fallback failed: model=%s status=%d err=%v",
					requestID, fallbackModel, statusCode, forwardErr)
				continue
			}

			// Fallback succeeded -- write the response.
			if reqMeta.Stream && streamBody != nil {
				handleStreamResponse(w, statusCode, streamContentType, streamBody)
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(statusCode)
				_, _ = w.Write(respBody)
			}

			// Emit a telemetry event for the successful fallback attempt.
			emitTelemetry(telClient, requestID, start, r.URL.Path,
				reqMeta.Model, candidate.upstreamModel, candidate.provider.ProviderID,
				"model_fallback", statusCode, latency, 1, 0, 0, 0, reqMeta.Stream, "")

			log.Printf("[gatewayd] request_id=%s fallback succeeded: model=%s provider=%s latency=%s",
				requestID, fallbackModel, candidate.provider.ProviderID, latency)
			return true
		}
	}

	return false
}
