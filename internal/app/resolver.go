package app

import (
	"context"
	"path"
	"strings"

	"ai-model-gateway/internal/core"
)

// resolver implements core.ModelResolver using bridge rules and fallback config.
type resolver struct {
	bridge   core.BridgeConfig
	fallback core.FallbackConfig
}

// NewModelResolver creates a ModelResolver from the compat config section.
func NewModelResolver(compat core.CompatConfig) core.ModelResolver {
	return &resolver{
		bridge:   compat.Bridge,
		fallback: compat.Fallback,
	}
}

func (r *resolver) Resolve(_ context.Context, model string, userAgent string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		// Empty model is valid for passthrough endpoints (files, GET responses, etc.)
		return "", nil
	}

	if !r.bridge.Enabled {
		return model, nil
	}

	// Check user-agent exclusion.
	if r.shouldSkipUA(userAgent) {
		return model, nil
	}

	// Apply bridge rewrite rules.
	for _, rule := range r.bridge.Rules {
		if matchGlob(rule.From, model) {
			return strings.TrimSpace(rule.To), nil
		}
	}
	return model, nil
}

func (r *resolver) FallbackModel(model string) string {
	if !r.fallback.Enabled {
		return ""
	}
	if fb, ok := r.fallback.Models[model]; ok && fb != "" {
		return fb
	}
	return ""
}

func (r *resolver) shouldSkipUA(ua string) bool {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return false
	}
	for _, pattern := range r.bridge.ExcludeUserAgents {
		if matchGlob(pattern, ua) {
			return true
		}
	}
	return false
}

// matchGlob performs a case-insensitive glob match, consistent with v1 logic.
func matchGlob(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	value = strings.TrimSpace(value)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	// Fast path: no glob metacharacters — do exact match.
	if !strings.ContainsAny(pattern, "*?[]") {
		return strings.EqualFold(pattern, value)
	}
	if ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(value)); err == nil && ok {
		return true
	}
	return false
}

func sanitizeGlob(s string) string {
	s = strings.ReplaceAll(s, "/", "\x00")
	s = strings.ReplaceAll(s, "\\", "\x01")
	return s
}
