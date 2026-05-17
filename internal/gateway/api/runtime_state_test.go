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
