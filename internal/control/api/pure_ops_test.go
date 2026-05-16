package api

import (
	"errors"
	"net/http"
	"testing"

	"ai-model-gateway/internal/control/publish"
	"ai-model-gateway/internal/core"
	"ai-model-gateway/internal/gateway/snapshot"
)

// ---------------------------------------------------------------------------
// preflightCheck
// ---------------------------------------------------------------------------

func TestPreflightCheck(t *testing.T) {
	tests := []struct {
		name   string
		ok     bool
		detail string
		want   map[string]any
	}{
		{
			name:   "ok true with empty detail",
			ok:     true,
			detail: "",
			want:   map[string]any{"name": "preflight-test", "ok": true},
		},
		{
			name:   "ok false with detail",
			ok:     false,
			detail: "some error",
			want:   map[string]any{"name": "preflight-test", "ok": false, "detail": "some error"},
		},
		{
			name:   "ok true with nil literal string",
			ok:     true,
			detail: "<nil>",
			want:   map[string]any{"name": "preflight-test", "ok": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preflightCheck("preflight-test", tt.ok, tt.detail)
			if len(got) != len(tt.want) {
				t.Errorf("preflightCheck() len = %d, want %d; got=%v", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("preflightCheck()[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validationError
// ---------------------------------------------------------------------------

func TestValidationError(t *testing.T) {
	tests := []struct {
		name   string
		result *publish.ConfigValidationResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "empty result",
		},
		{
			name:   "multiple errors",
			result: &publish.ConfigValidationResult{Errors: []string{"err1", "err2"}},
			want:   "err1; err2",
		},
		{
			name:   "empty errors",
			result: &publish.ConfigValidationResult{Errors: []string{}},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validationError(tt.result)
			if got != tt.want {
				t.Errorf("validationError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resultError
// ---------------------------------------------------------------------------

func TestResultError(t *testing.T) {
	tests := []struct {
		name   string
		result *publish.PublishResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "empty result",
		},
		{
			name:   "with error message",
			result: &publish.PublishResult{ErrorMessage: "provider not found"},
			want:   "provider not found",
		},
		{
			name:   "empty error message",
			result: &publish.PublishResult{ErrorMessage: ""},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resultError(tt.result)
			if got != tt.want {
				t.Errorf("resultError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// numericStatusValue
// ---------------------------------------------------------------------------

func TestNumericStatusValue(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		wantV  any
		wantOK bool
	}{
		{name: "int", value: int(42), wantV: 42, wantOK: true},
		{name: "int64", value: int64(100), wantV: int64(100), wantOK: true},
		{name: "float64", value: float64(3.14), wantV: float64(3.14), wantOK: true},
		{name: "string", value: "hello", wantV: nil, wantOK: false},
		{name: "nil", value: nil, wantV: nil, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotOK := numericStatusValue(tt.value)
			if gotOK != tt.wantOK {
				t.Errorf("numericStatusValue() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotV != tt.wantV {
				t.Errorf("numericStatusValue() value = %v (%T), want %v (%T)", gotV, gotV, tt.wantV, tt.wantV)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// writeConfigToolUnavailablePreview
// ---------------------------------------------------------------------------

func TestWriteConfigToolUnavailablePreview(t *testing.T) {
	err := errors.New("tool disabled")
	resp := writeConfigToolUnavailablePreview(err)
	if resp == nil {
		t.Fatal("writeConfigToolUnavailablePreview() returned nil")
	}
	if resp.Valid {
		t.Errorf("Valid = true, want false")
	}
	if len(resp.Errors) != 1 || resp.Errors[0] != "tool disabled" {
		t.Errorf("Errors = %v, want [\"tool disabled\"]", resp.Errors)
	}
}

// ---------------------------------------------------------------------------
// secretItemsFromConfig
// ---------------------------------------------------------------------------

func TestSecretItemsFromConfig(t *testing.T) {
	t.Run("nil config returns empty slice", func(t *testing.T) {
		items := secretItemsFromConfig(nil)
		if items == nil {
			t.Fatal("secretItemsFromConfig(nil) returned nil, want empty slice")
		}
		if len(items) != 0 {
			t.Errorf("len = %d, want 0", len(items))
		}
	})

	t.Run("empty admin yields absent bootstrap and cookie", func(t *testing.T) {
		cfg := &core.Config{}
		items := secretItemsFromConfig(cfg)
		if len(items) < 2 {
			t.Fatalf("len = %d, want at least 2", len(items))
		}
		if items[0].Name != "admin.bootstrap_token" || items[0].Present {
			t.Errorf("bootstrap_token: got name=%q present=%v", items[0].Name, items[0].Present)
		}
		if items[1].Name != "admin.cookie_signing_key" || items[1].Present {
			t.Errorf("cookie_signing_key: got name=%q present=%v", items[1].Name, items[1].Present)
		}
	})

	t.Run("admin secrets present when set", func(t *testing.T) {
		cfg := &core.Config{}
		cfg.Admin.BootstrapToken = "tok"
		cfg.Admin.CookieSigningKey = "key"
		items := secretItemsFromConfig(cfg)
		var bootstrapFound, cookieFound bool
		for _, item := range items {
			if item.Name == "admin.bootstrap_token" {
				bootstrapFound = true
				if !item.Present {
					t.Errorf("admin.bootstrap_token Present=false, want true")
				}
			}
			if item.Name == "admin.cookie_signing_key" {
				cookieFound = true
				if !item.Present {
					t.Errorf("admin.cookie_signing_key Present=false, want true")
				}
			}
		}
		if !bootstrapFound {
			t.Error("admin.bootstrap_token not found in items")
		}
		if !cookieFound {
			t.Error("admin.cookie_signing_key not found in items")
		}
	})

	t.Run("admin token present when set", func(t *testing.T) {
		cfg := &core.Config{}
		cfg.Admin.Tokens = []core.TokenConfig{
			{Name: "cli", Token: "abc"},
		}
		items := secretItemsFromConfig(cfg)
		found := false
		for _, item := range items {
			if item.Name == "admin.tokens.cli" {
				found = true
				if !item.Present {
					t.Errorf("admin.tokens.cli Present=false, want true")
				}
				if item.Kind != "admin_token" {
					t.Errorf("admin.tokens.cli Kind=%q, want \"admin_token\"", item.Kind)
				}
			}
		}
		if !found {
			t.Error("admin.tokens.cli not found in items")
		}
	})

	t.Run("unnamed admin token absent when token empty", func(t *testing.T) {
		cfg := &core.Config{}
		cfg.Admin.Tokens = []core.TokenConfig{
			{Name: "", Token: ""},
		}
		items := secretItemsFromConfig(cfg)
		found := false
		for _, item := range items {
			if item.Name == "admin.tokens.unnamed" {
				found = true
				if item.Present {
					t.Errorf("admin.tokens.unnamed Present=true, want false")
				}
			}
		}
		if !found {
			t.Error("admin.tokens.unnamed not found in items")
		}
	})

	t.Run("provider api_key present when set", func(t *testing.T) {
		cfg := &core.Config{}
		cfg.Providers = []core.Provider{
			{Name: "openai", APIKey: "sk-..."},
		}
		items := secretItemsFromConfig(cfg)
		found := false
		for _, item := range items {
			if item.Name == "providers.openai.api_key" {
				found = true
				if !item.Present {
					t.Errorf("providers.openai.api_key Present=false, want true")
				}
				if item.Kind != "provider_api_key" {
					t.Errorf("providers.openai.api_key Kind=%q, want \"provider_api_key\"", item.Kind)
				}
			}
		}
		if !found {
			t.Error("providers.openai.api_key not found in items")
		}
	})

	t.Run("unnamed provider api_key absent when empty", func(t *testing.T) {
		cfg := &core.Config{}
		cfg.Providers = []core.Provider{
			{Name: "", APIKey: ""},
		}
		items := secretItemsFromConfig(cfg)
		found := false
		for _, item := range items {
			if item.Name == "providers.unnamed.api_key" {
				found = true
				if item.Present {
					t.Errorf("providers.unnamed.api_key Present=true, want false")
				}
			}
		}
		if !found {
			t.Error("providers.unnamed.api_key not found in items")
		}
	})
}

// ---------------------------------------------------------------------------
// SummarizeSnapshot
// ---------------------------------------------------------------------------

func TestSummarizeSnapshot_Full(t *testing.T) {
	snap := &snapshot.Snapshot{
		Meta: snapshot.SnapshotMeta{
			SchemaVersion:   1,
			CompilerVersion: "v2.0",
		},
		Ingress: snapshot.IngressConfig{
			Listen: ":18080",
		},
		Contract: snapshot.ContractConfig{
			EnabledRoutes: []string{"/v1/chat/completions", "/v1/completions"},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-a",
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled: true,
				},
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "gpt-4"},
					{PublicModel: "gpt-3.5-turbo"},
				},
			},
			{
				ProviderID: "provider-b",
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled: false,
				},
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "claude-3"},
				},
			},
		},
		Pricing: snapshot.PricingConfig{
			Sources: []snapshot.PricingSource{
				{ID: "openai", Vendor: "openai-vendor"},
				{ID: "anthropic", Vendor: "anthropic-vendor"},
			},
		},
	}

	resp := SummarizeSnapshot(snap, "rev_1", []string{"warn1"})

	if !resp.Valid {
		t.Errorf("Valid = false, want true")
	}
	if resp.RevisionID != "rev_1" {
		t.Errorf("RevisionID = %q, want %q", resp.RevisionID, "rev_1")
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0] != "warn1" {
		t.Errorf("Warnings = %v, want [\"warn1\"]", resp.Warnings)
	}
	if resp.SnapshotSchemaVersion != 1 {
		t.Errorf("SnapshotSchemaVersion = %d, want 1", resp.SnapshotSchemaVersion)
	}
	if resp.CompilerVersion != "v2.0" {
		t.Errorf("CompilerVersion = %q, want %q", resp.CompilerVersion, "v2.0")
	}
	if resp.IngressListen != ":18080" {
		t.Errorf("IngressListen = %q, want %q", resp.IngressListen, ":18080")
	}
	if resp.ProviderCount != 2 {
		t.Errorf("ProviderCount = %d, want 2", resp.ProviderCount)
	}
	if resp.EnabledProviderCount != 1 {
		t.Errorf("EnabledProviderCount = %d, want 1", resp.EnabledProviderCount)
	}

	// EnabledRoutes from Contract
	if len(resp.EnabledRoutes) != 2 {
		t.Errorf("len(EnabledRoutes) = %d, want 2", len(resp.EnabledRoutes))
	}

	// Models should be sorted alphabetically
	wantModels := []string{"claude-3", "gpt-3.5-turbo", "gpt-4"}
	if len(resp.Models) != len(wantModels) {
		t.Errorf("len(Models) = %d, want %d; got=%v", len(resp.Models), len(wantModels), resp.Models)
	} else {
		for i := range wantModels {
			if resp.Models[i] != wantModels[i] {
				t.Errorf("Models[%d] = %q, want %q", i, resp.Models[i], wantModels[i])
			}
		}
	}

	// PricingSources map
	if len(resp.PricingSources) != 2 {
		t.Errorf("len(PricingSources) = %d, want 2", len(resp.PricingSources))
	}
	if resp.PricingSources["openai"] != "openai-vendor" {
		t.Errorf("PricingSources[openai] = %q, want %q", resp.PricingSources["openai"], "openai-vendor")
	}
	if resp.PricingSources["anthropic"] != "anthropic-vendor" {
		t.Errorf("PricingSources[anthropic] = %q, want %q", resp.PricingSources["anthropic"], "anthropic-vendor")
	}
}

func TestSummarizeSnapshot_Empty(t *testing.T) {
	snap := &snapshot.Snapshot{}
	resp := SummarizeSnapshot(snap, "", nil)

	if !resp.Valid {
		t.Errorf("Valid = false, want true")
	}
	if resp.ProviderCount != 0 {
		t.Errorf("ProviderCount = %d, want 0", resp.ProviderCount)
	}
	if resp.EnabledProviderCount != 0 {
		t.Errorf("EnabledProviderCount = %d, want 0", resp.EnabledProviderCount)
	}
	if len(resp.Models) != 0 {
		t.Errorf("len(Models) = %d, want 0", len(resp.Models))
	}
	if len(resp.PricingSources) != 0 {
		t.Errorf("len(PricingSources) = %d, want 0", len(resp.PricingSources))
	}
}

// ---------------------------------------------------------------------------
// recordAudit
// ---------------------------------------------------------------------------

func TestRecordAudit_NilAuditLog(t *testing.T) {
	// deps.AuditLog is nil by default — must not panic and do nothing.
	deps := Deps{Version: "test"}
	recordAudit(deps, nil, "test.action", "test", true, "", map[string]any{"key": "val"})
	// No panic = pass.
}

func TestRecordAudit_NormalCase(t *testing.T) {
	log := &stubAuditLog{}
	deps := Deps{AuditLog: log}
	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		t.Fatal(err)
	}

	recordAudit(deps, req, "test.action", "test", true, "", map[string]any{"key": "val"})

	if len(log.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(log.events))
	}
	ev := log.events[0]
	if ev.Action != "test.action" {
		t.Errorf("Action = %q, want %q", ev.Action, "test.action")
	}
	if ev.Resource != "test" {
		t.Errorf("Resource = %q, want %q", ev.Resource, "test")
	}
	if !ev.Success {
		t.Errorf("Success = false, want true")
	}
	if ev.Error != "" {
		t.Errorf("Error = %q, want empty", ev.Error)
	}
	if ev.Actor == "" {
		t.Errorf("Actor is empty, expected a role or 'anonymous'")
	}
}

func TestRecordAudit_NilRequest(t *testing.T) {
	log := &stubAuditLog{}
	deps := Deps{AuditLog: log}

	recordAudit(deps, nil, "test.action", "test", true, "", nil)

	if len(log.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(log.events))
	}
	ev := log.events[0]
	if ev.Actor != "anonymous" {
		t.Errorf("Actor = %q, want %q", ev.Actor, "anonymous")
	}
	if ev.Source != "" {
		t.Errorf("Source = %q, want empty", ev.Source)
	}
}
