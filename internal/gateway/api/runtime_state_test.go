package api

import (
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
