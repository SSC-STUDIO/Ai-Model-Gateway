package router

import (
	"errors"
	"net/http"
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
