package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"ai-model-gateway/internal/observability"
	"ai-model-gateway/internal/core"
)

// healthHandler returns an HTTP handler for the /-/health endpoint.
func healthHandler(getConfig func() *core.Config, sel core.RouteSelector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := &core.Config{}
		if getConfig != nil {
			if current := getConfig(); current != nil {
				cfg = current
			}
		}

		requestID := strings.TrimSpace(r.Header.Get(observability.RequestIDHeader))
		if requestID != "" {
			w.Header().Set(observability.RequestIDHeader, requestID)
		}

		models := append([]string(nil), sel.ListModels()...)
		sort.Strings(models)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"request_id":       requestID,
			"router_strategy":  cfg.Routing.Strategy,
			"bridge":           cfg.Compat.Bridge,
			"available_models": models,
			"upstreams":        buildHealthUpstreamView(cfg, sel),
		})
	}
}

func buildHealthUpstreamView(cfg *core.Config, sel core.RouteSelector) map[string]any {
	upstreams := make(map[string]any)
	if cfg == nil {
		return upstreams
	}

	healthByProvider := selectorHealthSnapshot(sel)
	for _, provider := range cfg.Providers {
		healthy := provider.IsEnabled()
		if state, ok := healthByProvider[provider.Name]; ok {
			healthy = state
		}
		upstreams[provider.Name] = map[string]any{
			"healthy":        healthy,
			"enabled":        provider.IsEnabled(),
			"provider_class": provider.ProviderClass,
			"models":         append([]string(nil), provider.Models...),
			"weight":         provider.Weight,
		}
	}
	return upstreams
}

func selectorHealthSnapshot(sel core.RouteSelector) map[string]bool {
	live, ok := sel.(*selector)
	if !ok || live == nil {
		return map[string]bool{}
	}

	live.mu.Lock()
	defer live.mu.Unlock()

	snapshot := make(map[string]bool, len(live.providers))
	for _, provider := range live.providers {
		healthy := provider.IsEnabled()
		if status, ok := live.status[provider.Name]; ok && live.isCoolingDown(status) {
			healthy = false
		}
		snapshot[provider.Name] = healthy
	}
	return snapshot
}
