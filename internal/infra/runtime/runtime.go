package runtime

import (
	"context"
	"errors"
	"reflect"
	"time"

	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configstate"
	"ai-model-gateway/internal/infra/pricing"
	"ai-model-gateway/internal/infra/telemetrydb"
)

// ConfigState defines the config state surface needed by admin/runtime features.
type ConfigState interface {
	Path() string
	Current() *core.Config
	SetCurrent(*core.Config)
	Save(*core.Config) error
	Rollback() (*core.Config, error)
	RollbackVersion(string) (*core.Config, error)
	ListVersions() ([]configstate.Version, error)
	ReadCurrentFile() ([]byte, error)
	ReadVersionFile(string) (configstate.Version, []byte, error)
}

// PricingCatalog defines pricing/economics catalog access for admin/runtime features.
type PricingCatalog interface {
	Start(context.Context)
	UpdateConfig(core.PricingConfig)
	Snapshot() pricing.Snapshot
}

// TelemetryStore defines the telemetry queries required by admin economics hooks.
type TelemetryStore interface {
	QueryModelRouteUsage(window time.Duration, limit int) []telemetrydb.ModelRouteUsage
}

// AdminRuntime groups optional admin dependencies assembled in app/infra layers.
type AdminRuntime struct {
	ConfigState    ConfigState
	PricingCatalog PricingCatalog
	TelemetryStore TelemetryStore
}

// NewAdminRuntime assembles optional runtime dependencies from the v2 config.
func NewAdminRuntime(configPath string, initial *core.Config) (*AdminRuntime, error) {
	if initial == nil {
		return nil, errors.New("initial config is nil")
	}

	configStore, err := configstate.New(configPath, initial)
	if err != nil {
		return nil, err
	}

	return &AdminRuntime{
		ConfigState:    configStore,
		PricingCatalog: pricing.NewCatalog(initial.Pricing),
	}, nil
}

// OptionalAdminFields returns field candidates for optional injection into admin deps.
func OptionalAdminFields(rt *AdminRuntime) map[string]any {
	if rt == nil {
		return map[string]any{}
	}
	fields := map[string]any{
		"ConfigState":    rt.ConfigState,
		"ConfigStore":    rt.ConfigState,
		"PricingCatalog": rt.PricingCatalog,
		"Pricing":        rt.PricingCatalog,
		"Economics":      rt.PricingCatalog,
	}
	if rt.ConfigState != nil {
		fields["ConfigExport"] = BuildConfigExportHook(rt.ConfigState)
		fields["ConfigSave"] = BuildConfigSaveHook(rt.ConfigState)
		fields["ConfigHistory"] = BuildConfigHistoryHook(rt.ConfigState)
		fields["ConfigHistoryDiff"] = BuildConfigHistoryDiffHook(rt.ConfigState)
		fields["ConfigRollback"] = BuildConfigRollbackHook(rt.ConfigState)
	}
	if rt.TelemetryStore != nil {
		fields["PricingEconomics"] = BuildPricingEconomicsHook(rt.TelemetryStore, rt.PricingCatalog)
	}
	return fields
}

// InjectOptionalFields sets struct fields by name when they exist and types are compatible.
func InjectOptionalFields(target any, values map[string]any) {
	if target == nil || len(values) == 0 {
		return
	}

	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	elem := v.Elem()
	if elem.Kind() != reflect.Struct {
		return
	}

	for name, value := range values {
		field := elem.FieldByName(name)
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if value == nil {
			if canBeNil(field.Kind()) {
				field.Set(reflect.Zero(field.Type()))
			}
			continue
		}

		incoming := reflect.ValueOf(value)
		if incoming.Type().AssignableTo(field.Type()) {
			field.Set(incoming)
			continue
		}
		if incoming.Type().ConvertibleTo(field.Type()) {
			field.Set(incoming.Convert(field.Type()))
		}
	}
}

func canBeNil(kind reflect.Kind) bool {
	switch kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}
