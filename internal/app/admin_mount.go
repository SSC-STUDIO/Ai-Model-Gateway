//go:build !app_testshim
// +build !app_testshim

package app

import (
	"ai-model-gateway/internal/adminapi"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/auth"
	runtimedeps "ai-model-gateway/internal/infra/runtime"
	"ai-model-gateway/internal/infra/telemetrydb"

	"github.com/go-chi/chi/v5"
)

func mountAdminRoutes(
	r chi.Router,
	cfg *core.Config,
	store *telemetrydb.Store,
	selector core.RouteSelector,
	getConfig func() *core.Config,
	adminRuntime *runtimedeps.AdminRuntime,
) *adminapi.EventBus {
	if cfg == nil || !cfg.Admin.Enabled {
		return nil
	}

	authenticator := auth.New(cfg.Admin.BootstrapToken, cfg.Admin.CookieSigningKey)
	if len(cfg.Admin.Tokens) > 0 {
		entries := make([]auth.TokenEntry, len(cfg.Admin.Tokens))
		for i, t := range cfg.Admin.Tokens {
			entries[i] = auth.TokenEntry{Name: t.Name, Token: t.Token, Role: t.Role}
		}
		authenticator.SetTokens(entries)
	}
	eventBus := adminapi.NewEventBus(50)
	deps := adminapi.Deps{
		Auth:      authenticator,
		Store:     store,
		Selector:  selector,
		GetConfig: getConfig,
		EventBus:  eventBus,
	}
	runtimedeps.InjectOptionalFields(&deps, runtimedeps.OptionalAdminFields(adminRuntime))
	adminapi.Mount(r, deps)
	return eventBus
}
