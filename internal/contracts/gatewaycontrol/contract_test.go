package gatewaycontrol

import (
	"testing"
	"time"
)

// TestReadinessStateString tests the String method for all readiness states.
func TestReadinessStateString(t *testing.T) {
	tests := []struct {
		state ReadinessState
		want  string
	}{
		{ReadinessUnknown, "unknown"},
		{ReadinessStarting, "starting"},
		{ReadinessReady, "ready"},
		{ReadinessDraining, "draining"},
		{ReadinessStopped, "stopped"},
		{ReadinessState(999), "unknown"}, // Unknown value
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("ReadinessState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestApplySnapshotRequest tests the request struct field initialization.
func TestApplySnapshotRequest(t *testing.T) {
	now := time.Now()
	req := ApplySnapshotRequest{
		SnapshotID:    "snap-001",
		RevisionID:    "rev-001",
		SnapshotBytes: []byte("test snapshot"),
		SchemaVersion: 1,
		GeneratedAt:   now,
		ForceApply:    true,
	}

	if req.SnapshotID != "snap-001" {
		t.Errorf("SnapshotID = %q, want snap-001", req.SnapshotID)
	}
	if req.RevisionID != "rev-001" {
		t.Errorf("RevisionID = %q, want rev-001", req.RevisionID)
	}
	if string(req.SnapshotBytes) != "test snapshot" {
		t.Errorf("SnapshotBytes = %q, want test snapshot", string(req.SnapshotBytes))
	}
	if req.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", req.SchemaVersion)
	}
	if !req.ForceApply {
		t.Error("ForceApply should be true")
	}
}

// TestApplySnapshotResponse tests the response struct.
func TestApplySnapshotResponse(t *testing.T) {
	resp := ApplySnapshotResponse{
		Applied:           true,
		ActiveSnapshotID:  "snap-002",
		PreviousSnapshotID: "snap-001",
		Error:             "",
		ValidationWarnings: []string{"warning1", "warning2"},
	}

	if !resp.Applied {
		t.Error("Applied should be true")
	}
	if resp.ActiveSnapshotID != "snap-002" {
		t.Errorf("ActiveSnapshotID = %q, want snap-002", resp.ActiveSnapshotID)
	}
	if resp.PreviousSnapshotID != "snap-001" {
		t.Errorf("PreviousSnapshotID = %q, want snap-001", resp.PreviousSnapshotID)
	}
	if len(resp.ValidationWarnings) != 2 {
		t.Errorf("ValidationWarnings length = %d, want 2", len(resp.ValidationWarnings))
	}
}

// TestGetStatusRequest tests the status request.
func TestGetStatusRequest(t *testing.T) {
	req := GetStatusRequest{IncludeDetails: true}
	if !req.IncludeDetails {
		t.Error("IncludeDetails should be true")
	}
}

// TestGetStatusResponse tests the status response.
func TestGetStatusResponse(t *testing.T) {
	now := time.Now()
	resp := GetStatusResponse{
		ActiveSnapshotID: "snap-001",
		Readiness:        ReadinessReady,
		ActiveRequests:   5,
		Listener:         ":8080",
		ProviderHealth: map[string]ProviderHealth{
			"openai": {
				Name:                "openai",
				Healthy:             true,
				LastCheck:           now,
				LastSuccess:         now,
				ConsecutiveFailures: 0,
				CooldownUntil:       time.Time{},
				LatencyMs:           100,
			},
		},
		Uptime:    time.Hour,
		StartedAt: now.Add(-time.Hour),
	}

	if resp.ActiveSnapshotID != "snap-001" {
		t.Errorf("ActiveSnapshotID = %q, want snap-001", resp.ActiveSnapshotID)
	}
	if resp.Readiness != ReadinessReady {
		t.Errorf("Readiness = %d, want %d", resp.Readiness, ReadinessReady)
	}
	if resp.ActiveRequests != 5 {
		t.Errorf("ActiveRequests = %d, want 5", resp.ActiveRequests)
	}
	if resp.Listener != ":8080" {
		t.Errorf("Listener = %q, want :8080", resp.Listener)
	}
	if len(resp.ProviderHealth) != 1 {
		t.Errorf("ProviderHealth length = %d, want 1", len(resp.ProviderHealth))
	}
	if resp.Uptime != time.Hour {
		t.Errorf("Uptime = %v, want %v", resp.Uptime, time.Hour)
	}
}

// TestProviderHealth tests the provider health struct.
func TestProviderHealth(t *testing.T) {
	now := time.Now()
	health := ProviderHealth{
		Name:                "anthropic",
		Healthy:             false,
		LastCheck:           now,
		LastSuccess:         now.Add(-time.Minute),
		ConsecutiveFailures: 3,
		CooldownUntil:       now.Add(5 * time.Minute),
		LatencyMs:           250,
	}

	if health.Name != "anthropic" {
		t.Errorf("Name = %q, want anthropic", health.Name)
	}
	if health.Healthy {
		t.Error("Healthy should be false")
	}
	if health.ConsecutiveFailures != 3 {
		t.Errorf("ConsecutiveFailures = %d, want 3", health.ConsecutiveFailures)
	}
	if health.LatencyMs != 250 {
		t.Errorf("LatencyMs = %d, want 250", health.LatencyMs)
	}
}

// TestDrainRequest tests the drain request.
func TestDrainRequest(t *testing.T) {
	req := DrainRequest{
		Timeout: 30 * time.Second,
		Force:   true,
	}

	if req.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", req.Timeout)
	}
	if !req.Force {
		t.Error("Force should be true")
	}
}

// TestDrainResponse tests the drain response.
func TestDrainResponse(t *testing.T) {
	now := time.Now()
	resp := DrainResponse{
		Success:          true,
		RemainingRequests: 0,
		DrainedAt:        now,
		Error:            "",
	}

	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.RemainingRequests != 0 {
		t.Errorf("RemainingRequests = %d, want 0", resp.RemainingRequests)
	}
}

// TestGatewayControlRPCInterface ensures the interface is correctly defined.
func TestGatewayControlRPCInterface(t *testing.T) {
	// This test verifies the interface by creating a mock implementation
	var _ GatewayControlRPC = (*mockGatewayControl)(nil)
}

type mockGatewayControl struct{}

func (m *mockGatewayControl) ApplySnapshot(req ApplySnapshotRequest, resp *ApplySnapshotResponse) error {
	return nil
}

func (m *mockGatewayControl) GetStatus(req GetStatusRequest, resp *GetStatusResponse) error {
	return nil
}

func (m *mockGatewayControl) Drain(req DrainRequest, resp *DrainResponse) error {
	return nil
}
