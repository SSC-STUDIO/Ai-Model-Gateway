package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-model-gateway/internal/telemetry"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/infra/configstate"
	"ai-model-gateway/internal/infra/telemetrydb"

	"gopkg.in/yaml.v3"
)

func TestInjectOptionalFieldsSetsMatchingAssignableFields(t *testing.T) {
	t.Parallel()

	type deps struct {
		ConfigState    interface{}
		PricingCatalog interface{}
		Count          int
	}

	d := deps{}
	InjectOptionalFields(&d, map[string]any{
		"ConfigState":    "state-adapter",
		"PricingCatalog": "pricing-adapter",
		"MissingField":   "ignored",
		"Count":          3,
	})

	if d.ConfigState != "state-adapter" {
		t.Fatalf("unexpected ConfigState: %#v", d.ConfigState)
	}
	if d.PricingCatalog != "pricing-adapter" {
		t.Fatalf("unexpected PricingCatalog: %#v", d.PricingCatalog)
	}
	if d.Count != 3 {
		t.Fatalf("unexpected Count: %d", d.Count)
	}
}

func TestInjectOptionalFieldsSkipsIncompatibleValues(t *testing.T) {
	t.Parallel()

	type deps struct {
		Count int
	}
	d := deps{Count: 7}

	InjectOptionalFields(&d, map[string]any{
		"Count": "not-an-int",
	})

	if d.Count != 7 {
		t.Fatalf("count should stay unchanged on incompatible value, got %d", d.Count)
	}
}

func TestOptionalAdminFieldsInjectsConfigHooksFromRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.v2.yaml")
	initial := testRuntimeConfig("alpha")
	writeRuntimeConfigFile(t, path, initial)

	store, err := configstate.New(path, &initial)
	if err != nil {
		t.Fatalf("new configstate store: %v", err)
	}
	updated := testRuntimeConfig("beta")
	if err := store.Save(&updated); err != nil {
		t.Fatalf("save config: %v", err)
	}

	rt := &AdminRuntime{
		ConfigState: store,
		TelemetryStore: fakeTelemetryStore{
			rows: []telemetrydb.ModelRouteUsage{
				{
					RequestedModel:     "gpt-5.2",
					EffectiveModel:     "gpt-5.4",
					Requests:           2,
					InputTokens:        1_000_000,
					CachedPromptTokens: 400_000,
					OutputTokens:       1_000_000,
					TotalTokens:        2_000_000,
				},
			},
		},
	}

	type deps struct {
		ConfigExport      func() ([]byte, error)
		ConfigSave        func(json.RawMessage) (interface{}, error)
		ConfigHistory     func() (interface{}, error)
		ConfigHistoryDiff func(string) (interface{}, error)
		ConfigRollback    func(string) (interface{}, error)
		PricingEconomics  func() (interface{}, error)
	}
	var d deps
	InjectOptionalFields(&d, OptionalAdminFields(rt))

	if d.ConfigExport == nil {
		t.Fatal("expected ConfigExport hook to be injected")
	}
	if d.ConfigSave == nil {
		t.Fatal("expected ConfigSave hook to be injected")
	}
	if d.ConfigHistory == nil {
		t.Fatal("expected ConfigHistory hook to be injected")
	}
	if d.ConfigHistoryDiff == nil {
		t.Fatal("expected ConfigHistoryDiff hook to be injected")
	}
	if d.ConfigRollback == nil {
		t.Fatal("expected ConfigRollback hook to be injected")
	}
	if d.PricingEconomics == nil {
		t.Fatal("expected PricingEconomics hook to be injected")
	}

	exported, err := d.ConfigExport()
	if err != nil {
		t.Fatalf("config export: %v", err)
	}
	exportText := string(exported)
	if !strings.Contains(exportText, "providers:") {
		t.Fatalf("expected providers in export payload, got %q", exportText)
	}
	if strings.Contains(exportText, "sk-secret") {
		t.Fatalf("expected export payload to redact secrets, got %q", exportText)
	}

	savePayload := json.RawMessage(`{
		"server":{"listen":":19191"},
		"providers":[
			{
				"name":"beta",
				"base_url":"https://example.com",
				"provider_class":"quota_limited",
				"models":["gpt-5.4"],
				"enabled":true
			}
		]
	}`)
	savedPayload, err := d.ConfigSave(savePayload)
	if err != nil {
		t.Fatalf("config save: %v", err)
	}
	savedMap, ok := savedPayload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected config save payload map, got %T", savedPayload)
	}
	for _, key := range []string{"server", "admin", "routing", "telemetry", "pricing", "compat", "providers"} {
		if _, ok := savedMap[key]; !ok {
			t.Fatalf("expected %q in config save response, got %#v", key, savedMap)
		}
	}
	switch server := savedMap["server"].(type) {
	case map[string]interface{}:
		if server["listen"] != ":19191" {
			t.Fatalf("expected saved server listen :19191, got %#v", server)
		}
	case core.ServerConfig:
		if server.Listen != ":19191" {
			t.Fatalf("expected saved server listen :19191, got %#v", server)
		}
	default:
		t.Fatalf("expected saved server payload, got %#v", savedMap["server"])
	}
	current := store.Current()
	if current == nil || current.Server.Listen != ":19191" {
		t.Fatalf("expected store current config to update, got %#v", current)
	}
	if len(current.Providers) != 1 || current.Providers[0].APIKey != "sk-secret" {
		t.Fatalf("expected config save to preserve hidden provider secret, got %+v", current.Providers)
	}
	if got := current.Providers[0].Headers["Authorization"]; got != "Bearer provider-secret" {
		t.Fatalf("expected config save to preserve hidden provider header secret, got %q", got)
	}
	savedProvidersJSON, err := json.Marshal(savedMap["providers"])
	if err != nil {
		t.Fatalf("marshal saved providers: %v", err)
	}
	var savedProviders []map[string]interface{}
	if err := json.Unmarshal(savedProvidersJSON, &savedProviders); err != nil {
		t.Fatalf("unmarshal saved providers: %v", err)
	}
	if len(savedProviders) != 1 {
		t.Fatalf("expected one saved provider payload, got %#v", savedMap["providers"])
	}
	headers, ok := savedProviders[0]["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected provider headers map in save response, got %#v", savedProviders[0]["headers"])
	}
	if headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("expected saved provider Authorization header to be redacted, got %#v", headers["Authorization"])
	}
	if headers["X-Test-Header"] != "enabled" {
		t.Fatalf("expected saved provider non-sensitive header to survive, got %#v", headers["X-Test-Header"])
	}

	historyPayload, err := d.ConfigHistory()
	if err != nil {
		t.Fatalf("config history: %v", err)
	}
	historyMap, ok := historyPayload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected history payload as map, got %T", historyPayload)
	}
	var (
		firstVersion map[string]interface{}
		foundVersion bool
	)
	switch versions := historyMap["versions"].(type) {
	case []interface{}:
		if len(versions) > 0 {
			firstVersion, foundVersion = versions[0].(map[string]interface{})
		}
	case []map[string]interface{}:
		if len(versions) > 0 {
			firstVersion = versions[0]
			foundVersion = true
		}
	}
	if !foundVersion {
		t.Fatalf("expected non-empty versions, got %#v", historyMap["versions"])
	}
	versionID, _ := firstVersion["id"].(string)
	if strings.TrimSpace(versionID) == "" {
		t.Fatalf("expected non-empty version id, got %#v", firstVersion["id"])
	}

	diffPayload, err := d.ConfigHistoryDiff(versionID)
	if err != nil {
		t.Fatalf("config history diff: %v", err)
	}
	diffMap, ok := diffPayload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected diff payload as map, got %T", diffPayload)
	}
	if _, ok := diffMap["summary"]; !ok {
		t.Fatalf("expected summary in diff payload, got %#v", diffMap)
	}

	rollbackPayload, err := d.ConfigRollback(versionID)
	if err != nil {
		t.Fatalf("config rollback: %v", err)
	}
	rollbackMap, ok := rollbackPayload.(map[string]interface{})
	if !ok {
		t.Fatalf("expected rollback payload map, got %T", rollbackPayload)
	}
	providersJSON, err := json.Marshal(rollbackMap["providers"])
	if err != nil {
		t.Fatalf("marshal rollback providers: %v", err)
	}
	var providers []map[string]interface{}
	if err := json.Unmarshal(providersJSON, &providers); err != nil {
		t.Fatalf("unmarshal rollback providers: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("unexpected rollback providers payload: %#v", rollbackMap["providers"])
	}
	if restored := store.Current(); restored == nil || restored.Server.Listen == ":19191" {
		t.Fatalf("expected rollback to move away from saved listen :19191, got %#v", restored)
	}
	if providers[0]["name"] == "" {
		t.Fatalf("expected rollback payload to include provider identity, got %#v", providers[0])
	}

	economicsPayload, err := d.PricingEconomics()
	if err != nil {
		t.Fatalf("pricing economics: %v", err)
	}
	economics, ok := economicsPayload.(telemetry.PricingSnapshot)
	if !ok {
		t.Fatalf("expected pricing snapshot, got %T", economicsPayload)
	}
	if economics.Summary.TotalUsd <= 0 {
		t.Fatalf("expected pricing summary total usd > 0, got %+v", economics.Summary)
	}
	if economics.Summary.CacheSavingsUsd <= 0 {
		t.Fatalf("expected cache savings > 0, got %+v", economics.Summary)
	}
	if len(economics.Models) != 1 || economics.Models[0].PricingModel != "gpt-5.4" {
		t.Fatalf("expected bridged pricing model gpt-5.4, got %+v", economics.Models)
	}
}

type fakeTelemetryStore struct {
	rows []telemetrydb.ModelRouteUsage
}

func (f fakeTelemetryStore) QueryModelRouteUsage(_ time.Duration, _ int) []telemetrydb.ModelRouteUsage {
	return append([]telemetrydb.ModelRouteUsage(nil), f.rows...)
}

func testRuntimeConfig(provider string) core.Config {
	return core.Config{
		Server: core.ServerConfig{
			Listen: ":18080",
		},
		Admin: core.AdminConfig{
			Enabled: false,
		},
		Routing: core.RoutingConfig{},
		Providers: []core.Provider{
			{
				Name:          provider,
				BaseURL:       "https://example.com",
				APIKey:        "sk-secret",
				ProviderClass: core.ProviderClassQuotaLimited,
				Models:        []string{"gpt-5.2"},
				Weight:        1,
				TimeoutMs:     30000,
				SameRetries:   1,
				Headers: map[string]string{
					"Authorization": "Bearer provider-secret",
					"X-Test-Header": "enabled",
				},
			},
		},
		Telemetry: core.TelemetryConfig{
			SQLitePath: filepath.Join("data", "telemetry.db"),
		},
		Pricing: core.PricingConfig{
			CachePath:            filepath.Join("data", "pricing-cache.json"),
			RefreshIntervalHours: 12,
			RequestTimeoutMs:     15000,
		},
		Compat: core.CompatConfig{
			Fallback: core.FallbackConfig{Models: map[string]string{}},
		},
	}
}

func writeRuntimeConfigFile(t *testing.T, path string, cfg core.Config) {
	t.Helper()
	cfg.Normalize()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}
