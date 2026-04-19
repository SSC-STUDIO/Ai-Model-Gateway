// Package gatewaycontrol defines the RPC contract for control-plane -> data-plane communication.
// This is the interface that controld uses to manage gatewayd.
package gatewaycontrol

import (
	"time"
)

// GatewayControlRPC is the interface exposed by gatewayd for controld.
// All methods are synchronous RPC calls.
type GatewayControlRPC interface {
	// ApplySnapshot sends a compiled runtime snapshot to gatewayd.
	// The snapshot replaces the current active runtime configuration atomically.
	ApplySnapshot(req ApplySnapshotRequest, resp *ApplySnapshotResponse) error

	// GetStatus returns the current runtime status of gatewayd.
	GetStatus(req GetStatusRequest, resp *GetStatusResponse) error

	// Drain signals gatewayd to stop accepting new requests and wait for
	// in-flight requests to complete. Used for graceful shutdown.
	Drain(req DrainRequest, resp *DrainResponse) error
}

// ApplySnapshotRequest contains the compiled runtime snapshot to apply.
type ApplySnapshotRequest struct {
	// SnapshotID is a unique identifier for this snapshot.
	SnapshotID string

	// RevisionID is the config revision this snapshot was compiled from.
	RevisionID string

	// SnapshotBytes is the serialized snapshot (YAML or JSON).
	SnapshotBytes []byte

	// SchemaVersion is the snapshot schema version.
	SchemaVersion int

	// GeneratedAt is when this snapshot was compiled.
	GeneratedAt time.Time

	// ForceApply bypasses validation (use with caution).
	ForceApply bool
}

// ApplySnapshotResponse is returned after applying a snapshot.
type ApplySnapshotResponse struct {
	// Applied indicates whether the snapshot was successfully applied.
	Applied bool

	// ActiveSnapshotID is the currently active snapshot ID after the operation.
	ActiveSnapshotID string

	// PreviousSnapshotID is the snapshot that was active before this operation.
	PreviousSnapshotID string

	// Error contains any error message if Apply is false.
	Error string

	// ValidationWarnings contains non-fatal validation issues.
	ValidationWarnings []string
}

// GetStatusRequest requests the current gatewayd status.
type GetStatusRequest struct {
	// IncludeDetails requests detailed provider health information.
	IncludeDetails bool
}

// GetStatusResponse contains the current gatewayd status.
type GetStatusResponse struct {
	// ActiveSnapshotID is the currently loaded snapshot ID.
	ActiveSnapshotID string

	// Readiness indicates if gatewayd is ready to serve requests.
	Readiness ReadinessState

	// ActiveRequests is the number of requests currently being processed.
	ActiveRequests int

	// Listener is the address gatewayd is listening on.
	Listener string

	// ProviderHealth contains health status for each provider.
	ProviderHealth map[string]ProviderHealth

	// Uptime is how long gatewayd has been running.
	Uptime time.Duration

	// StartedAt is when gatewayd was started.
	StartedAt time.Time
}

// ReadinessState represents the readiness of gatewayd.
type ReadinessState int

const (
	ReadinessUnknown ReadinessState = iota
	ReadinessStarting
	ReadinessReady
	ReadinessDraining
	ReadinessStopped
)

func (s ReadinessState) String() string {
	switch s {
	case ReadinessStarting:
		return "starting"
	case ReadinessReady:
		return "ready"
	case ReadinessDraining:
		return "draining"
	case ReadinessStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ProviderHealth contains health information for a single provider.
type ProviderHealth struct {
	// Name is the provider identifier.
	Name string

	// Healthy indicates if the provider is accepting requests.
	Healthy bool

	// LastCheck is when the last health check was performed.
	LastCheck time.Time

	// LastSuccess is when the last successful request was made.
	LastSuccess time.Time

	// ConsecutiveFailures is the count of consecutive failed requests.
	ConsecutiveFailures int

	// CooldownUntil is when the provider will be retried after being marked unhealthy.
	CooldownUntil time.Time

	// LatencyMs is the average latency over recent requests.
	LatencyMs int64
}

// DrainRequest signals gatewayd to drain connections.
type DrainRequest struct {
	// Timeout is the maximum time to wait for in-flight requests.
	Timeout time.Duration

	// Force will terminate in-flight requests after timeout.
	Force bool
}

// DrainResponse contains the result of a drain operation.
type DrainResponse struct {
	// Success indicates if the drain completed successfully.
	Success bool

	// RemainingRequests is the number of requests still in-flight (if Force was false).
	RemainingRequests int

	// DrainedAt is when the drain completed.
	DrainedAt time.Time

	// Error contains any error message.
	Error string
}
