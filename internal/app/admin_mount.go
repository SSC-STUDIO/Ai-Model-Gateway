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
) {
	if cfg == nil || !cfg.Admin.Enabled {
		return
	}

	authenticator := auth.New(cfg.Admin.BootstrapToken, cfg.Admin.CookieSigningKey)
	deps := adminapi.Deps{
		Auth:      authenticator,
		Store:     store,
		Selector:  selector,
		GetConfig: getConfig,
	}
	runtimedeps.InjectOptionalFields(&deps, runtimedeps.OptionalAdminFields(adminRuntime))
	adminapi.Mount(r, deps)
}
