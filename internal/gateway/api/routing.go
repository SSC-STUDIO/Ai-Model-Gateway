package api

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
)

var routingSequence atomic.Uint64

type providerCandidate struct {
	provider      *snapshot.ProviderSnapshot
	upstreamModel string
	weight        int
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
	if snap.RoutingPolicy.Retry.InfiniteOnError {
		return 0
	}
	return 1 + max(snap.RoutingPolicy.MaxRetries, 0)
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
	if snap.RoutingPolicy.Retry.AllErrors && statusCode >= http.StatusBadRequest {
		return true
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

func normalizeWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}
