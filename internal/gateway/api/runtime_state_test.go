package api

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/gatewaycontrol"
	"ai-model-gateway/internal/gateway/snapshot"
)

func TestNewRuntimeState(t *testing.T) {
	state := NewRuntimeState()
	if state == nil {
		t.Fatal("expected non-nil runtime state")
	}
	if state.providers == nil {
		t.Error("expected initialized providers map")
	}
	if state.sticky == nil {
		t.Error("expected initialized sticky map")
	}
}

func TestRuntimeStateApplySnapshot(t *testing.T) {
	state := NewRuntimeState()

	snap := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "provider-1"},
			{ProviderID: "provider-2"},
		},
	}

	state.ApplySnapshot(snap)

	state.mu.Lock()
	if len(state.providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(state.providers))
	}
	if _, ok := state.providers["provider-1"]; !ok {
		t.Error("expected provider-1 to exist")
	}
	if _, ok := state.providers["provider-2"]; !ok {
		t.Error("expected provider-2 to exist")
	}
	state.mu.Unlock()
}

func TestRuntimeStateApplySnapshotRemovesInactive(t *testing.T) {
	state := NewRuntimeState()

	// Add initial providers
	snap1 := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "provider-1"},
			{ProviderID: "provider-2"},
		},
	}
	state.ApplySnapshot(snap1)

	// Apply new snapshot with only one provider
	snap2 := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "provider-1"},
		},
	}
	state.ApplySnapshot(snap2)

	state.mu.Lock()
	if len(state.providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(state.providers))
	}
	if _, ok := state.providers["provider-2"]; ok {
		t.Error("expected provider-2 to be removed")
	}
	state.mu.Unlock()
}

func TestRuntimeStateApplySnapshotNil(t *testing.T) {
	state := NewRuntimeState()

	// Should not panic
	state.ApplySnapshot(nil)

	var nilState *RuntimeState
	nilState.ApplySnapshot(nil)
}

func TestRuntimeStateProviderHealthSnapshot(t *testing.T) {
	state := NewRuntimeState()

	snap := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "provider-1"},
		},
	}
	state.ApplySnapshot(snap)

	health := state.ProviderHealthSnapshot(snap)
	if health == nil {
		t.Fatal("expected non-nil health map")
	}
	if _, ok := health["provider-1"]; !ok {
		t.Error("expected provider-1 in health map")
	}
}

func TestRuntimeStateProviderHealthSnapshotGroupsByUpstreamID(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.May, 17, 3, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:   5,
				CooldownSec: 60,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{ProviderID: "key-a", UpstreamID: "https://shared.example.com/v1", BaseURL: "https://shared.example.com/v1"},
			{ProviderID: "key-b", UpstreamID: "https://shared.example.com/v1", BaseURL: "https://shared.example.com/v1"},
		},
	}
	state.ApplySnapshot(snap)
	state.reportAttemptResult("key-a", http.StatusTooManyRequests, 20*time.Millisecond, nil, snap)
	state.reportAttemptResult("key-b", http.StatusOK, 40*time.Millisecond, nil, snap)

	health := state.ProviderHealthSnapshot(snap)
	if len(health) != 1 {
		t.Fatalf("expected one logical upstream, got %#v", health)
	}
	item := health["https://shared.example.com/v1"]
	if item.UpstreamID != "https://shared.example.com/v1" || item.Name != item.UpstreamID {
		t.Fatalf("unexpected upstream identity: %#v", item)
	}
	if !reflect.DeepEqual(item.ProviderIDs, []string{"key-a", "key-b"}) {
		t.Fatalf("provider ids = %#v", item.ProviderIDs)
	}
	if !item.Healthy {
		t.Fatalf("expected grouped upstream to remain healthy while below threshold: %#v", item)
	}
	if item.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", item.ConsecutiveFailures)
	}
	if item.LatencyMs != 40 {
		t.Fatalf("latency ms = %d, want max 40", item.LatencyMs)
	}
}

func TestWaitForUpstreamSlotGroupsByUpstreamID(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.May, 17, 4, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	limit := snapshot.RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             1,
	}
	providerA := &snapshot.ProviderSnapshot{
		ProviderID: "key-a",
		UpstreamID: "https://shared.example.com/v1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			RateLimit: limit,
		},
	}
	providerB := &snapshot.ProviderSnapshot{
		ProviderID: "key-b",
		UpstreamID: "https://shared.example.com/v1",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			RateLimit: limit,
		},
	}

	if err := state.WaitForUpstreamSlot(context.Background(), providerA); err != nil {
		t.Fatalf("first wait error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := state.WaitForUpstreamSlot(ctx, providerB); err == nil {
		t.Fatal("expected second provider with same upstream id to wait and hit context timeout")
	}
}

func TestUpstreamRateLimiterSerializesConcurrentReservations(t *testing.T) {
	now := time.Date(2026, time.May, 17, 4, 0, 0, 0, time.UTC)
	limiter := newUpstreamRateLimiter(snapshot.RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             1,
	}, now)

	if wait := limiter.reserveDelay(now); wait != 0 {
		t.Fatalf("first reservation wait = %v, want 0", wait)
	}
	if wait := limiter.reserveDelay(now); wait != time.Second {
		t.Fatalf("second reservation wait = %v, want 1s", wait)
	}
	if wait := limiter.reserveDelay(now); wait != 2*time.Second {
		t.Fatalf("third reservation wait = %v, want 2s", wait)
	}
	if wait := limiter.reserveDelay(now); wait != 3*time.Second {
		t.Fatalf("fourth reservation wait = %v, want 3s", wait)
	}
}

func TestWaitForUpstreamSlotDoesNothingWhenDisabled(t *testing.T) {
	state := NewRuntimeState()
	provider := &snapshot.ProviderSnapshot{
		ProviderID: "provider",
		ExecutionPolicy: snapshot.ExecutionPolicy{
			RateLimit: snapshot.RateLimitConfig{
				Enabled:           false,
				RequestsPerSecond: 0,
				Burst:             0,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := state.WaitForUpstreamSlot(ctx, provider); err != nil {
		t.Fatalf("disabled limiter should not wait: %v", err)
	}
}

func TestRuntimeStateProviderHealthSnapshotNil(t *testing.T) {
	state := NewRuntimeState()

	if health := state.ProviderHealthSnapshot(nil); health != nil {
		t.Error("expected nil health for nil snapshot")
	}

	var nilState *RuntimeState
	if health := nilState.ProviderHealthSnapshot(nil); health != nil {
		t.Error("expected nil health for nil state")
	}
}

func TestProviderHealthFields(t *testing.T) {
	now := time.Now()
	health := gatewaycontrol.ProviderHealth{
		Healthy:             true,
		LatencyMs:           150,
		ConsecutiveFailures: 0,
		LastCheck:           now,
		LastSuccess:         now,
	}

	if !health.Healthy {
		t.Error("expected healthy to be true")
	}
	if health.LatencyMs != 150 {
		t.Errorf("expected latency 150ms, got %d", health.LatencyMs)
	}
}

func TestRuntimeStateDisableCooldownKeepsProviderRoutable(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:                1,
				CooldownSec:              60,
				PassthroughAfterSec:      600,
				QuotaRecoveryIntervalMin: 30,
				DisableCooldown:          true,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "provider-1",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-a", UpstreamModel: "model-a-upstream"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{
					Enabled: true,
					Weight:  1,
				},
			},
		},
	}
	state.ApplySnapshot(snap)
	state.reportAttemptResult("provider-1", http.StatusTooManyRequests, 10*time.Millisecond, nil, snap)

	health := state.ProviderHealthSnapshot(snap)
	if got := health["provider-1"]; !got.Healthy {
		t.Fatalf("expected provider to remain healthy when cooldown is disabled, got %#v", got)
	}

	candidates := collectProviderCandidatesForRequest(snap, "model-a")
	ordered := state.orderCandidates(snap, "model-a", "", candidates)
	if len(ordered) != 1 || ordered[0].provider.ProviderID != "provider-1" {
		t.Fatalf("expected provider to remain routable, got %#v", ordered)
	}
}

// TestCircuitBreakerBlocksAfterThreshold proves that a provider exceeding the
// consecutive failure threshold is blocked from routing until cooldown expires.
// This prevents a failing upstream from amplifying retries across all requests.
func TestCircuitBreakerBlocksAfterThreshold(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:   3,
				CooldownSec: 60,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "failing",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-x", UpstreamModel: "model-x-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
			{
				ProviderID: "healthy",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-x", UpstreamModel: "model-x-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
		},
	}
	state.ApplySnapshot(snap)

	// Fail "failing" provider below threshold — should still be routable.
	for i := 0; i < 2; i++ {
		state.reportAttemptResult("failing", http.StatusTooManyRequests, 10*time.Millisecond, nil, snap)
	}
	candidates := collectProviderCandidatesForRequest(snap, "model-x")
	ordered := state.orderCandidates(snap, "model-x", "", candidates)
	if len(ordered) != 2 {
		t.Fatalf("expected both providers below threshold, got %d", len(ordered))
	}

	// Third failure hits threshold — provider should be blocked.
	state.reportAttemptResult("failing", http.StatusTooManyRequests, 10*time.Millisecond, nil, snap)
	ordered = state.orderCandidates(snap, "model-x", "", candidates)
	if len(ordered) != 1 || ordered[0].provider.ProviderID != "healthy" {
		t.Fatalf("expected only healthy provider, got %v", ordered)
	}

	// Advance past cooldown — failing provider should be routable again.
	now = now.Add(61 * time.Second)
	state.now = func() time.Time { return now }
	ordered = state.orderCandidates(snap, "model-x", "", candidates)
	if len(ordered) != 2 {
		t.Fatalf("expected both providers after cooldown, got %d", len(ordered))
	}
}

// TestCircuitBreakerPassthroughAllowed proves that after PassthroughAfterSec
// elapses during a cooldown window, the blocked provider appears at the end of
// the candidate list as a passthrough option.
func TestCircuitBreakerPassthroughAllowed(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:           2,
				CooldownSec:         120,
				PassthroughAfterSec: 10,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "primary",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-y", UpstreamModel: "model-y-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
			{
				ProviderID: "secondary",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-y", UpstreamModel: "model-y-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
		},
	}
	state.ApplySnapshot(snap)

	// Hit threshold on secondary.
	for i := 0; i < 2; i++ {
		state.reportAttemptResult("secondary", http.StatusInternalServerError, 10*time.Millisecond, nil, snap)
	}

	// Before passthrough window — secondary should be excluded entirely.
	candidates := collectProviderCandidatesForRequest(snap, "model-y")
	ordered := state.orderCandidates(snap, "model-y", "", candidates)
	if len(ordered) != 1 || ordered[0].provider.ProviderID != "primary" {
		t.Fatalf("expected only primary before passthrough, got %v", ordered)
	}

	// Advance past passthrough window but within cooldown.
	now = now.Add(11 * time.Second)
	state.now = func() time.Time { return now }
	ordered = state.orderCandidates(snap, "model-y", "", candidates)
	if len(ordered) != 2 {
		t.Fatalf("expected primary + passthrough secondary, got %d", len(ordered))
	}
	// Secondary should be at the end (passthrough, lowest priority).
	if ordered[len(ordered)-1].provider.ProviderID != "secondary" {
		t.Fatalf("expected secondary as passthrough at end, got %v", ordered[len(ordered)-1].provider.ProviderID)
	}
}

// TestCircuitBreakerQuotaCooldownRecovery proves that a 429-triggered quota
// cooldown blocks the provider for QuotaRecoveryIntervalMin, then recovers.
func TestCircuitBreakerQuotaCooldownRecovery(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		RoutingPolicy: snapshot.RoutingPolicy{
			FailurePolicy: snapshot.FailurePolicy{
				Threshold:                10, // High threshold so regular failures don't block
				CooldownSec:              60,
				QuotaRecoveryIntervalMin: 5,
			},
		},
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "quota-provider",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-z", UpstreamModel: "model-z-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
			{
				ProviderID: "backup",
				ModelTable: []snapshot.ModelMapping{
					{PublicModel: "model-z", UpstreamModel: "model-z-up"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
			},
		},
	}
	state.ApplySnapshot(snap)

	// Single 429 triggers quota cooldown.
	state.reportAttemptResult("quota-provider", http.StatusTooManyRequests, 10*time.Millisecond, nil, snap)

	// Provider should be blocked during quota recovery window.
	candidates := collectProviderCandidatesForRequest(snap, "model-z")
	ordered := state.orderCandidates(snap, "model-z", "", candidates)
	if len(ordered) != 1 || ordered[0].provider.ProviderID != "backup" {
		t.Fatalf("expected only backup during quota cooldown, got %v", ordered)
	}

	// Advance past quota recovery.
	now = now.Add(6 * time.Minute)
	state.now = func() time.Time { return now }
	ordered = state.orderCandidates(snap, "model-z", "", candidates)
	if len(ordered) != 2 {
		t.Fatalf("expected both providers after quota recovery, got %d", len(ordered))
	}
}

// TestTryRecoverAPIKeysNilReceiver proves that calling TryRecoverAPIKeys on a
// nil RuntimeState returns false without panicking.
func TestTryRecoverAPIKeysNilReceiver(t *testing.T) {
	var state *RuntimeState
	snap := &snapshot.Snapshot{}
	if state.TryRecoverAPIKeys(snap, time.Minute) {
		t.Fatal("expected false from nil receiver")
	}
}

// TestTryRecoverAPIKeysNilSnapshot proves that calling TryRecoverAPIKeys with a
// nil snapshot returns false without panicking.
func TestTryRecoverAPIKeysNilSnapshot(t *testing.T) {
	state := NewRuntimeState()
	if state.TryRecoverAPIKeys(nil, time.Minute) {
		t.Fatal("expected false from nil snapshot")
	}
}

// TestTryRecoverAPIKeysRecoversExhaustedKeys proves the full integration path:
// ApplySnapshot creates key rotators, ReportFailure exhausts a key, time passes,
// and TryRecoverAPIKeys successfully recovers the exhausted key so it becomes
// routable again.
func TestTryRecoverAPIKeysRecoversExhaustedKeys(t *testing.T) {
	state := NewRuntimeState()
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	state.now = func() time.Time { return now }

	snap := &snapshot.Snapshot{
		Providers: []snapshot.ProviderSnapshot{
			{
				ProviderID: "multi-key",
				APIKeys: []snapshot.APIKey{
					{Name: "key-a", Value: "val-a"},
					{Name: "key-b", Value: "val-b"},
				},
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
				ModelTable:      []snapshot.ModelMapping{{PublicModel: "m", UpstreamModel: "m-up"}},
			},
			{
				ProviderID: "no-keys",
				ExecutionPolicy: snapshot.ExecutionPolicy{Enabled: true, Weight: 1},
				ModelTable:      []snapshot.ModelMapping{{PublicModel: "m", UpstreamModel: "m-up"}},
			},
		},
	}
	state.ApplySnapshot(snap)

	// Exhaust key-a via the key rotator directly.
	kr := state.keyRotators["multi-key"]
	if kr == nil {
		t.Fatal("expected key rotator to be created by ApplySnapshot")
	}
	for i := 0; i < 3; i++ {
		kr.ReportFailure("val-a")
	}

	// Before recovery, only val-b should be returned.
	for i := 0; i < 4; i++ {
		k := kr.Next()
		if k != "val-b" {
			t.Fatalf("expected only val-b before recovery, got %q", k)
		}
	}

	// TryRecoverAPIKeys with 0 cooldown should not recover (lastFail is now).
	changed := state.TryRecoverAPIKeys(snap, 0)
	if changed {
		t.Fatal("expected no recovery with 0 cooldown")
	}

	// Manipulate lastFail to simulate passage of time (KeyRotator.TryRecover
	// uses time.Now() internally, so we backdate the failure timestamp).
	kr.mu.Lock()
	for i := range kr.keys {
		if kr.keys[i].value == "val-a" {
			kr.keys[i].lastFail = time.Now().Add(-2 * time.Minute)
		}
	}
	kr.mu.Unlock()

	// Now TryRecoverAPIKeys with 1 minute cooldown should recover key-a.
	changed = state.TryRecoverAPIKeys(snap, time.Minute)
	if !changed {
		t.Fatal("expected recovery after cooldown")
	}

	// After recovery, both keys should be routable.
	seen := make(map[string]bool)
	for i := 0; i < 6; i++ {
		seen[kr.Next()] = true
	}
	if !seen["val-a"] {
		t.Fatal("expected val-a to be recovered and routable")
	}
	if !seen["val-b"] {
		t.Fatal("expected val-b to remain routable")
	}
}
