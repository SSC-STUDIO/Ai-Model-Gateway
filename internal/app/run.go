// Package app is the v2 application entry point that wires together
// core interfaces, infra adapters, and the HTTP server.
package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configloader"
	"ai-model-gateway/internal/infra/httpserver"
	runtimedeps "ai-model-gateway/internal/infra/runtime"
	"ai-model-gateway/internal/infra/telemetrydb"
)

var newRunTransport = func() core.UpstreamTransport {
	return NewUpstreamTransport()
}

func updatePublicContractRuntime(core.Pipeline, core.RouteSelector) {}

// Run loads the v2 config, assembles all components, and starts the server.
// It blocks until ctx is cancelled or a fatal error occurs.
func Run(ctx context.Context, configPath string) error {
	// --- Config watcher (hot-reload) ---

	watcher, err := configloader.NewWatcher(configPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("init config watcher: %w", err)
	}
	watcher.Start()
	defer watcher.Stop()

	cfg := watcher.Config()
	log.Printf("[v2] config loaded from %s", configPath)

	// --- Build optional runtime deps (config history/pricing catalog) ---

	adminRuntime, err := runtimedeps.NewAdminRuntime(configPath, cfg)
	if err != nil {
		return fmt.Errorf("init admin runtime deps: %w", err)
	}
	if adminRuntime.PricingCatalog != nil {
		adminRuntime.PricingCatalog.Start(ctx)
	}
	watcher.OnChange(func(next *core.Config) {
		if adminRuntime.ConfigState != nil {
			adminRuntime.ConfigState.SetCurrent(next)
		}
		if adminRuntime.PricingCatalog != nil {
			adminRuntime.PricingCatalog.UpdateConfig(next.Pricing)
		}
	})

	// --- Build infra adapters ---

	store, err := telemetrydb.New(cfg.Telemetry)
	if err != nil {
		return fmt.Errorf("init telemetry store: %w", err)
	}
	adminRuntime.TelemetryStore = store
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("[v2] close telemetry store: %v", err)
		}
	}()

	// --- Build app-layer pipeline ---

	routeSelector := NewRouteSelector(cfg.Routing, cfg.Providers)
	transport := newRunTransport()

	if liveSelector, ok := routeSelector.(*selector); ok {
		liveSelector.StartHealthChecks(ctx)
		watcher.OnChange(func(next *core.Config) {
			liveSelector.UpdateConfig(next.Routing, next.Providers)
		})
	}

	buildPipeline := func(current *core.Config) core.Pipeline {
		return NewPipeline(PipelineParams{
			Resolver:  NewModelResolver(current.Compat),
			Selector:  routeSelector,
			Transport: transport,
			Inspector: NewResponseInspector(current.Routing),
			Compat:    NewCompatAdapter(current.Compat),
			Sink:      store,
			Cfg:       current.Routing,
		})
	}
	pl := newLivePipeline(buildPipeline(cfg))
	watcher.OnChange(func(next *core.Config) {
		pl.Update(buildPipeline(next))
	})

	// --- Build HTTP server ---

	srv := httpserver.New(cfg.Server)
	r := srv.Router()
	getConfig := func() *core.Config {
		if adminRuntime.ConfigState != nil {
			if current := adminRuntime.ConfigState.Current(); current != nil {
				return current
			}
		}
		return watcher.Config()
	}

	// Health endpoint (always available)
	r.Get("/-/health", healthHandler(getConfig, routeSelector))

	// Gateway /v1/* routes
	MountGatewayRoutes(r, pl, routeSelector)

	// Admin routes (if enabled)
	if cfg.Admin.Enabled {
		mountAdminRoutes(r, cfg, store, routeSelector, getConfig, adminRuntime)
	}

	log.Printf("[v2] starting gateway-v2 on %s", cfg.Server.Listen)
	return srv.ListenAndServe(ctx)
}
