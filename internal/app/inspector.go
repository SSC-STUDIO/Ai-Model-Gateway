package app

import (
	"context"
	"strings"
	"sync"

	"ai-model-gateway/internal/core"
)

// inspector implements core.ResponseInspector.
// It examines upstream responses and decides whether to retry or intercept.
type inspector struct {
	mu         sync.RWMutex
	retry      core.RetryPolicyConfig
	intercepts []core.InterceptRule
}

// NewResponseInspector creates a ResponseInspector from routing config.
func NewResponseInspector(cfg core.RoutingConfig) core.ResponseInspector {
	return &inspector{
		retry:      cfg.Retry,
		intercepts: cfg.Intercepts,
	}
}

func (ins *inspector) UpdateConfig(cfg core.RoutingConfig) {
	ins.mu.Lock()
	defer ins.mu.Unlock()
	ins.retry = cfg.Retry
	ins.intercepts = cfg.Intercepts
}

func (ins *inspector) Inspect(_ context.Context, req *core.GatewayRequest, resp *core.GatewayResponse) (*core.GatewayResponse, error) {
	ins.mu.RLock()
	retry := ins.retry
	intercepts := append([]core.InterceptRule(nil), ins.intercepts...)
	ins.mu.RUnlock()

	if resp.Error != nil {
		resp.Retryable = true
		return resp, nil
	}

	// Check intercept rules first (they may override retry logic).
	for _, rule := range intercepts {
		if !rule.IsEnabled() {
			continue
		}
		if ins.matchesIntercept(rule, req, resp) {
			switch rule.Action {
			case "fail":
				resp.Retryable = false
				return resp, nil
			case "retry":
				resp.Retryable = true
				return resp, nil
			}
		}
	}

	// Standard retry logic based on status code and message keywords.
	if shouldRetry(retry, resp) {
		resp.Retryable = true
	}

	return resp, nil
}

// shouldRetry checks if the response qualifies for a retry based on the
// retry policy configuration.
func shouldRetry(retry core.RetryPolicyConfig, resp *core.GatewayResponse) bool {
	sc := resp.StatusCode

	// Explicit status code match.
	for _, code := range retry.StatusCodes {
		if sc == code {
			return true
		}
	}

	// Status code >= min threshold.
	if retry.StatusCodeMin != nil && sc >= *retry.StatusCodeMin {
		return true
	}

	// Message keyword match (in response body).
	if len(resp.Body) > 0 {
		bodyLower := strings.ToLower(string(resp.Body))
		for _, kw := range retry.MessageKeywords {
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				return true
			}
		}
	}

	return false
}

// matchesIntercept checks if a response matches an intercept rule.
func (ins *inspector) matchesIntercept(rule core.InterceptRule, req *core.GatewayRequest, resp *core.GatewayResponse) bool {
	// Path match.
	if len(rule.Paths) > 0 {
		pathMatched := false
		for _, p := range rule.Paths {
			if matchGlob(p, req.Path) {
				pathMatched = true
				break
			}
		}
		if !pathMatched {
			return false
		}
	}

	// Status code match.
	if len(rule.StatusCodes) > 0 {
		codeMatched := false
		for _, code := range rule.StatusCodes {
			if resp.StatusCode == code {
				codeMatched = true
				break
			}
		}
		if !codeMatched {
			return false
		}
	}

	// Status code >= min.
	if rule.StatusCodeMin != nil && resp.StatusCode < *rule.StatusCodeMin {
		return false
	}

	// Message keyword match.
	if len(rule.MessageKeywords) > 0 && len(resp.Body) > 0 {
		bodyLower := strings.ToLower(string(resp.Body))
		kwMatched := false
		for _, kw := range rule.MessageKeywords {
			if strings.Contains(bodyLower, strings.ToLower(kw)) {
				kwMatched = true
				break
			}
		}
		if !kwMatched {
			return false
		}
	}

	return true
}
