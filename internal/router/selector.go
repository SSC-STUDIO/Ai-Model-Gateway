package router

import "ai-model-gateway/internal/config"

func SupportsModel(upstream config.Upstream, model string) bool {
	if !upstream.IsEnabled() {
		return false
	}
	if model == "" || len(upstream.Models) == 0 {
		return true
	}
	for _, candidate := range upstream.Models {
		if candidate == model {
			return true
		}
	}
	return false
}
