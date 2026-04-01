package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"ai-model-gateway/internal/config"
	"ai-model-gateway/internal/state"
)

func TestManagerPickPrefersHealthyAndFallsBackWhenNeeded(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "health_weighted_rr"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestFailure("alpha", time.Millisecond, 0, errors.New("alpha down"), true, "transport")
	manager.ReportRequestSuccess("beta", time.Millisecond, 200)

	upstream, ok := manager.Pick("gpt-4o-mini", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected an upstream")
	}
	if upstream.Name != "beta" {
		t.Fatalf("expected healthy upstream beta, got %s", upstream.Name)
	}

	upstream, ok = manager.Pick("gpt-4o-mini", map[string]struct{}{"beta": {}})
	if !ok {
		t.Fatalf("expected fallback upstream")
	}
	if upstream.Name != "alpha" {
		t.Fatalf("expected fallback to alpha, got %s", upstream.Name)
	}
}

func TestManagerCooldownAndPassthroughWindow(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:                   "round_robin",
			FailureThreshold:           2,
			CooldownSec:                60,
			FailurePassthroughAfterSec: 600,
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestFailure("alpha", time.Millisecond, 429, errors.New("rate limit"), true, "status")
	manager.ReportRequestFailure("alpha", time.Millisecond, 429, errors.New("rate limit"), true, "status")

	if _, ok := manager.Pick("gpt-4o-mini", map[string]struct{}{}); ok {
		t.Fatalf("expected upstream to be skipped during cooldown")
	}

	manager.mu.Lock()
	status := manager.statuses["alpha"]
	status.CooldownUntil = time.Now().Add(-time.Minute)
	status.RetryableFailureSince = time.Now().Add(-11 * time.Minute)
	manager.statuses["alpha"] = status
	manager.mu.Unlock()

	if !manager.ShouldPassthroughFailure("alpha") {
		t.Fatalf("expected passthrough window to be active")
	}
}

func TestManagerPickStickyPrefersAssignedHealthyUpstream(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1800,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
	manager.ReportRequestSuccess("beta", time.Millisecond, 200)
	manager.RememberSticky("resp_123", "beta")

	upstream, ok := manager.PickSticky("gpt-4o-mini", "resp_123", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected sticky upstream")
	}
	if upstream.Name != "beta" {
		t.Fatalf("expected sticky upstream beta, got %s", upstream.Name)
	}
}

func TestManagerPickStickyFallsBackWhenAssignedUpstreamIsUnhealthy(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1800,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
	manager.ReportRequestFailure("beta", time.Millisecond, 503, errors.New("beta down"), true, "status")
	manager.RememberSticky("resp_123", "beta")

	upstream, ok := manager.PickSticky("gpt-4o-mini", "resp_123", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected fallback upstream")
	}
	if upstream.Name != "alpha" {
		t.Fatalf("expected fallback to alpha when sticky upstream is unhealthy, got %s", upstream.Name)
	}
}

func TestManagerPickPrefersFreeClassBeforeQuotaLimited(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "quota", BaseURL: "https://quota.example.com", ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.2-codex"}, Weight: 1},
			{Name: "free", BaseURL: "https://free.example.com", ProviderClass: config.UpstreamClassFree, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("quota", time.Millisecond, http.StatusOK)
	manager.ReportRequestSuccess("free", time.Millisecond, http.StatusOK)

	upstream, ok := manager.Pick("gpt-5.2-codex", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected an upstream")
	}
	if upstream.Name != "free" {
		t.Fatalf("expected free class upstream first, got %s", upstream.Name)
	}
}

func TestManagerReportSuccessClearsQuotaBlock(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "quota", BaseURL: "https://quota.example.com", ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.BlockUpstreamQuota("quota", "insufficient_quota")

	if _, ok := manager.Pick("gpt-5.2-codex", map[string]struct{}{}); ok {
		t.Fatalf("expected blocked upstream to be skipped")
	}

	manager.ReportRequestSuccess("quota", time.Millisecond, http.StatusOK)

	status := manager.Snapshot()["quota"]
	if status.QuotaBlocked {
		t.Fatalf("expected quota block to be cleared after success")
	}
	if !status.Healthy {
		t.Fatalf("expected upstream to be healthy after success")
	}
	if status.LastError != "" {
		t.Fatalf("expected cleared last error, got %q", status.LastError)
	}

	upstream, ok := manager.Pick("gpt-5.2-codex", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected upstream to become selectable again")
	}
	if upstream.Name != "quota" {
		t.Fatalf("expected quota upstream after unblock, got %s", upstream.Name)
	}
}

func TestManagerSetConfigClearsQuotaBlockWhenProviderBecomesFree(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "provider-a", BaseURL: "https://quota.example.com", ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.2-codex"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.BlockUpstreamQuota("provider-a", "quota exceeded")

	updated := cfg
	updated.Upstreams = []config.Upstream{
		{Name: "provider-a", BaseURL: "https://quota.example.com", ProviderClass: config.UpstreamClassFree, Models: []string{"gpt-5.2-codex"}, Weight: 1},
	}
	updated.Normalize()
	manager.SetConfig(updated)

	status := manager.Snapshot()["provider-a"]
	if status.QuotaBlocked {
		t.Fatalf("expected quota block to be cleared after config change")
	}
	if !status.Healthy {
		t.Fatalf("expected provider to be re-armed after config change")
	}

	upstream, ok := manager.Pick("gpt-5.2-codex", map[string]struct{}{})
	if !ok {
		t.Fatalf("expected upstream to be selectable after config change")
	}
	if upstream.Name != "provider-a" {
		t.Fatalf("expected provider-a after config change, got %s", upstream.Name)
	}
	if upstream.ProviderClassNormalized() != config.UpstreamClassFree {
		t.Fatalf("expected free provider class, got %q", upstream.ProviderClassNormalized())
	}
}

func TestManagerRememberStickyPrunesExpiredAssignments(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1800,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.mu.Lock()
	manager.sticky["expired"] = stickyAssignment{Upstream: "alpha", ExpiresAt: time.Now().Add(-time.Minute)}
	manager.nextStickyPrune = time.Now().Add(-time.Second)
	manager.mu.Unlock()

	manager.RememberSticky("resp_123", "alpha")

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if _, ok := manager.sticky["expired"]; ok {
		t.Fatalf("expected expired sticky assignment to be pruned")
	}
	if _, ok := manager.sticky["resp_123"]; !ok {
		t.Fatalf("expected new sticky assignment to be retained")
	}
}

func TestManagerPickPreservesWeightedSelectionWithoutPoolExpansion(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "weighted_rr"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4o-mini"}, Weight: 2},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4o-mini"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, http.StatusOK)
	manager.ReportRequestSuccess("beta", time.Millisecond, http.StatusOK)

	expected := []string{"alpha", "alpha", "beta", "alpha", "alpha", "beta"}
	for index, name := range expected {
		upstream, ok := manager.Pick("gpt-4o-mini", map[string]struct{}{})
		if !ok {
			t.Fatalf("pick %d: expected an upstream", index)
		}
		if upstream.Name != name {
			t.Fatalf("pick %d: expected %s, got %s", index, name, upstream.Name)
		}
	}
}

func TestManagerNewManagerWithOptions(t *testing.T) {
	cfg := config.Config{
		Upstreams: []config.Upstream{
			{Name: "test", BaseURL: "https://test.example.com"},
		},
	}
	cfg.Normalize()

	registry := NewStrategyRegistry()
	metrics := NewInMemoryMetricsCollector()

	manager := NewManager(
		state.NewConfigStore(cfg),
		WithStrategyRegistry(registry),
		WithMetricsCollector(metrics),
	)

	if manager.strategyRegistry == nil {
		t.Error("expected strategy registry to be set")
	}
	if manager.metricsCollector == nil {
		t.Error("expected metrics collector to be set")
	}

	// Test that metrics are recorded
	manager.ReportRequestSuccess("test", time.Millisecond, 200)
	if len(metrics.GetHealthRecords()) != 1 {
		t.Errorf("expected 1 health record, got %d", len(metrics.GetHealthRecords()))
	}
}

func TestManagerPick_AllUpstreamsUnhealthy(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	
	// Mark all upstreams as unhealthy
	manager.ReportRequestFailure("alpha", time.Millisecond, 503, errors.New("down"), true, "status")
	manager.ReportRequestFailure("beta", time.Millisecond, 503, errors.New("down"), true, "status")

	// Should still return an upstream (fallback behavior)
	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if !ok {
		t.Error("expected fallback upstream when all are unhealthy")
	}
	if upstream.Name != "alpha" && upstream.Name != "beta" {
		t.Errorf("expected alpha or beta, got %s", upstream.Name)
	}
}

func TestManagerPick_ModelNotSupported(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)

	// Try to pick a model that alpha doesn't support
	_, ok := manager.Pick("gpt-3.5-turbo", map[string]struct{}{})
	if ok {
		t.Error("expected no upstream for unsupported model")
	}
}

func TestManagerPick_SkipExcluded(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
	manager.ReportRequestSuccess("beta", time.Millisecond, 200)

	// Exclude alpha, should get beta
	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{"alpha": {}})
	if !ok {
		t.Fatal("expected an upstream")
	}
	if upstream.Name != "beta" {
		t.Errorf("expected beta (alpha excluded), got %s", upstream.Name)
	}
}

func TestManagerPick_DisabledUpstream(t *testing.T) {
	disabled := false
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}, Weight: 1, Enabled: &disabled},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("beta", time.Millisecond, 200)

	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if !ok {
		t.Fatal("expected an upstream")
	}
	if upstream.Name != "beta" {
		t.Errorf("expected beta (alpha disabled), got %s", upstream.Name)
	}
}

func TestManagerHealthStateTransitions(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "test", BaseURL: "https://test.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))

	// Initial state: healthy (default)
	status := manager.Snapshot()["test"]
	if !status.Healthy {
		t.Error("expected initial healthy state")
	}

	// Transition to unhealthy
	manager.ReportRequestFailure("test", time.Millisecond, 503, errors.New("down"), true, "status")
	status = manager.Snapshot()["test"]
	if status.Healthy {
		t.Error("expected unhealthy after failure")
	}
	if status.ConsecutiveFailures != 1 {
		t.Errorf("expected 1 consecutive failure, got %d", status.ConsecutiveFailures)
	}

	// Transition back to healthy
	manager.ReportRequestSuccess("test", time.Millisecond, 200)
	status = manager.Snapshot()["test"]
	if !status.Healthy {
		t.Error("expected healthy after success")
	}
	if status.ConsecutiveSuccess != 1 {
		t.Errorf("expected 1 consecutive success, got %d", status.ConsecutiveSuccess)
	}
	if status.ConsecutiveFailures != 0 {
		t.Errorf("expected 0 consecutive failures, got %d", status.ConsecutiveFailures)
	}
}

func TestManagerQuotaBlockRecovery(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:                          "round_robin",
			QuotaBlockRecoveryIntervalMinutes: 0, // Use default 60 minutes
		},
		Upstreams: []config.Upstream{
			{Name: "test", BaseURL: "https://test.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))

	// Block upstream
	manager.BlockUpstreamQuota("test", "quota exceeded")

	// Should be blocked
	_, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if ok {
		t.Error("expected upstream to be blocked")
	}

	// Manually set quota blocked time to be in the past (beyond recovery window)
	manager.mu.Lock()
	status := manager.statuses["test"]
	status.QuotaBlockedAt = time.Now().Add(-61 * time.Minute)
	manager.statuses["test"] = status
	manager.mu.Unlock()

	// Now upstream should be available again (auto-recovered)
	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if !ok {
		t.Error("expected upstream to be auto-recovered")
	}
	if upstream.Name != "test" {
		t.Errorf("expected test upstream, got %s", upstream.Name)
	}
}

func TestManagerCooldown(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy:         "round_robin",
			FailureThreshold: 3,
			CooldownSec:      60,
		},
		Upstreams: []config.Upstream{
			{Name: "test", BaseURL: "https://test.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))

	// Trigger cooldown after 3 failures
	for i := 0; i < 3; i++ {
		manager.ReportRequestFailure("test", time.Millisecond, 503, errors.New("down"), true, "status")
	}

	// Should be in cooldown
	_, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if ok {
		t.Error("expected upstream to be in cooldown")
	}

	// Manually clear cooldown
	manager.mu.Lock()
	status := manager.statuses["test"]
	status.CooldownUntil = time.Now().Add(-time.Second)
	manager.statuses["test"] = status
	manager.mu.Unlock()

	// Should be available again
	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if !ok {
		t.Error("expected upstream to be available after cooldown")
	}
	if upstream.Name != "test" {
		t.Errorf("expected test upstream, got %s", upstream.Name)
	}
}

func TestManagerIsUpstreamEnabled(t *testing.T) {
	disabled := false
	enabled := true
	cfg := config.Config{
		Upstreams: []config.Upstream{
			{Name: "enabled", BaseURL: "https://enabled.example.com", Enabled: &enabled},
			{Name: "disabled", BaseURL: "https://disabled.example.com", Enabled: &disabled},
			{Name: "default", BaseURL: "https://default.example.com"}, // nil = enabled by default
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))

	if !manager.IsUpstreamEnabled("enabled") {
		t.Error("expected enabled upstream to be enabled")
	}
	if manager.IsUpstreamEnabled("disabled") {
		t.Error("expected disabled upstream to be disabled")
	}
	if !manager.IsUpstreamEnabled("default") {
		t.Error("expected default (nil) upstream to be enabled")
	}
	if manager.IsUpstreamEnabled("nonexistent") {
		t.Error("expected nonexistent upstream to be disabled")
	}
	if manager.IsUpstreamEnabled("") {
		t.Error("expected empty name to be disabled")
	}
}

func TestManagerModels(t *testing.T) {
	cfg := config.Config{
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4", "gpt-3.5"}},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4", "claude-3"}},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	models := manager.Models()

	expected := []string{"claude-3", "gpt-3.5", "gpt-4"}
	if len(models) != len(expected) {
		t.Errorf("expected %d models, got %d: %v", len(expected), len(models), models)
	}
	for i, m := range models {
		if m != expected[i] {
			t.Errorf("expected model %s at index %d, got %s", expected[i], i, m)
		}
	}
}

func TestManagerCurrentConfig(t *testing.T) {
	cfg := config.Config{
		Listen: ":9090",
		Upstreams: []config.Upstream{
			{Name: "test", BaseURL: "https://test.example.com"},
		},
	}
	cfg.Normalize()

	store := state.NewConfigStore(cfg)
	manager := NewManager(store)

	current := manager.CurrentConfig()
	if current.Listen != ":9090" {
		t.Errorf("expected Listen=:9090, got %s", current.Listen)
	}

	configStore := manager.ConfigStore()
	if configStore != store {
		t.Error("expected ConfigStore to return the same store")
	}
}

func TestManagerSetConfig(t *testing.T) {
	cfg := config.Config{
		Upstreams: []config.Upstream{
			{Name: "old", BaseURL: "https://old.example.com"},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.BlockUpstreamQuota("old", "quota exceeded")

	newCfg := config.Config{
		Upstreams: []config.Upstream{
			{Name: "new", BaseURL: "https://new.example.com"},
		},
	}
	newCfg.Normalize()

	manager.SetConfig(newCfg)

	// Old upstream should be removed from statuses
	statuses := manager.Snapshot()
	if _, ok := statuses["old"]; ok {
		t.Error("expected old upstream to be removed from statuses")
	}
}

func TestManagerRememberSticky_Disabled(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: false,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com"},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.RememberSticky("key1", "alpha")

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.sticky) != 0 {
		t.Error("expected sticky assignment to not be saved when disabled")
	}
}

func TestManagerRememberSticky_InvalidInput(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: true,
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com"},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))

	// Empty key
	manager.RememberSticky("", "alpha")
	// Empty upstream
	manager.RememberSticky("key1", "")
	// Whitespace only
	manager.RememberSticky("   ", "alpha")

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if len(manager.sticky) != 0 {
		t.Error("expected no sticky assignments for invalid input")
	}
}

func TestManagerStickyExpiration(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{
			Strategy: "round_robin",
			StickySessions: config.StickySessionConfig{
				Enabled: true,
				TTLSec:  1, // 1 second TTL
			},
		},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
	
	// First, verify sticky assignment works when not expired
	manager.RememberSticky("key1", "alpha")
	upstream, ok := manager.PickSticky("gpt-4", "key1", map[string]struct{}{})
	if !ok || upstream.Name != "alpha" {
		t.Error("expected sticky assignment to work before expiration")
	}
	
	// Verify the assignment exists in the map
	manager.mu.Lock()
	_, exists := manager.sticky["key1"]
	manager.mu.Unlock()
	if !exists {
		t.Fatal("expected sticky assignment to exist")
	}

	// Now manually set it as expired and trigger pruning via RememberSticky
	manager.mu.Lock()
	manager.sticky["expired_key"] = stickyAssignment{
		Upstream:  "alpha",
		ExpiresAt: time.Now().Add(-time.Second), // Already expired
	}
	manager.nextStickyPrune = time.Now().Add(-time.Second) // Force pruning
	manager.mu.Unlock()

	// Trigger pruning
	manager.RememberSticky("key2", "alpha")

	// Verify the expired assignment was pruned
	manager.mu.Lock()
	_, exists = manager.sticky["expired_key"]
	manager.mu.Unlock()
	if exists {
		t.Error("expected expired sticky assignment to be pruned")
	}

	// Verify the new assignment exists
	manager.mu.Lock()
	_, exists = manager.sticky["key2"]
	manager.mu.Unlock()
	if !exists {
		t.Error("expected new sticky assignment to be retained")
	}
}

func TestManagerNilSafety(t *testing.T) {
	// Test with nil manager
	var nilManager *Manager

	// These should not panic
	nilManager.SetConfig(config.Config{})
	if nilManager.IsUpstreamEnabled("test") {
		t.Error("nil manager should return false for IsUpstreamEnabled")
	}
}

func TestManagerConcurrency(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Models: []string{"gpt-4"}, Weight: 1},
			{Name: "beta", BaseURL: "https://beta.example.com", Models: []string{"gpt-4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
	manager.ReportRequestSuccess("beta", time.Millisecond, 200)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		
		// Concurrent picks
		go func() {
			defer wg.Done()
			manager.Pick("gpt-4", map[string]struct{}{})
		}()
		
		// Concurrent status reports
		go func() {
			defer wg.Done()
			manager.ReportRequestSuccess("alpha", time.Millisecond, 200)
		}()
		
		// Concurrent sticky operations
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i)
			manager.RememberSticky(key, "alpha")
			manager.PickSticky("gpt-4", key, map[string]struct{}{})
		}(i)
	}
	wg.Wait()

	// Verify manager is still functional after concurrent access
	upstream, ok := manager.Pick("gpt-4", map[string]struct{}{})
	if !ok {
		t.Error("expected manager to still work after concurrent access")
	}
	if upstream.Name != "alpha" && upstream.Name != "beta" {
		t.Errorf("expected alpha or beta, got %s", upstream.Name)
	}
}

func TestManagerWithMockHealthChecker(t *testing.T) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "round_robin"},
		Health: config.HealthConfig{
			Enabled:   true,
			IntervalSec: 1,
		},
		Upstreams: []config.Upstream{
			{Name: "healthy", BaseURL: "https://healthy.example.com"},
			{Name: "unhealthy", BaseURL: "https://unhealthy.example.com"},
		},
	}
	cfg.Normalize()

	mockChecker := NewMockHealthChecker()
	mockChecker.SetResult("healthy", HealthResult{
		Healthy:    true,
		Latency:    10 * time.Millisecond,
		StatusCode: 200,
	})
	mockChecker.SetResult("unhealthy", HealthResult{
		Healthy:    false,
		Latency:    0,
		StatusCode: 503,
		Error:      errors.New("unhealthy"),
	})

	manager := NewManager(
		state.NewConfigStore(cfg),
		WithHealthChecker(mockChecker),
	)

	// Run one round of health checks
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	manager.runHealthChecksOnce(ctx)

	// Verify health statuses were updated
	statuses := manager.Snapshot()
	if !statuses["healthy"].Healthy {
		t.Error("expected healthy upstream to be marked healthy")
	}
	if statuses["unhealthy"].Healthy {
		t.Error("expected unhealthy upstream to be marked unhealthy")
	}
}

func BenchmarkManagerPickHealthWeightedRR(b *testing.B) {
	cfg := config.Config{
		Router: config.RouterConfig{Strategy: "health_weighted_rr"},
		Upstreams: []config.Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", ProviderClass: config.UpstreamClassFree, Models: []string{"gpt-5.4"}, Weight: 4},
			{Name: "beta", BaseURL: "https://beta.example.com", ProviderClass: config.UpstreamClassFree, Models: []string{"gpt-5.4"}, Weight: 3},
			{Name: "gamma", BaseURL: "https://gamma.example.com", ProviderClass: config.UpstreamClassFree, Models: []string{"gpt-5.4"}, Weight: 2},
			{Name: "delta", BaseURL: "https://delta.example.com", ProviderClass: config.UpstreamClassQuotaLimited, Models: []string{"gpt-5.4"}, Weight: 1},
		},
	}
	cfg.Normalize()

	manager := NewManager(state.NewConfigStore(cfg))
	manager.ReportRequestSuccess("alpha", time.Millisecond, http.StatusOK)
	manager.ReportRequestSuccess("beta", time.Millisecond, http.StatusOK)
	manager.ReportRequestFailure("gamma", time.Millisecond, http.StatusTooManyRequests, errors.New("rate limit"), true, "status")
	manager.ReportRequestSuccess("delta", time.Millisecond, http.StatusOK)

	excluded := map[string]struct{}{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := manager.Pick("gpt-5.4", excluded); !ok {
			b.Fatal("expected upstream")
		}
	}
}
