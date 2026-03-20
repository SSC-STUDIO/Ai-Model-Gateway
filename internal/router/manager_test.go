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
