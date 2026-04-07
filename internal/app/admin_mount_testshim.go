//go:build app_testshim
// +build app_testshim

package app

import (
	"ai-model-gateway/internal/adminapi"
	"ai-model-gateway/internal/core"
	runtimedeps "ai-model-gateway/internal/infra/runtime"
	"ai-model-gateway/internal/infra/telemetrydb"

	"github.com/go-chi/chi/v5"
)

func mountAdminRoutes(
	_ chi.Router,
	_ *core.Config,
	_ *telemetrydb.Store,
	_ core.RouteSelector,
	_ func() *core.Config,
	_ *runtimedeps.AdminRuntime,
) *adminapi.EventBus {
	panic("mountAdminRoutes should not be called in app_testshim builds")
}
