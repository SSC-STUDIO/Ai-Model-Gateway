// Package telemetryingest defines the RPC contract for data-plane -> telemetry-plane communication.
// This is the interface that gatewayd uses to emit telemetry events to telemetryd.
package telemetryingest

import (
	"time"
)

// TelemetryIngestRPC is the interface exposed by telemetryd for gatewayd.
// All methods are synchronous RPC calls, but the implementation may batch and async-write.
type TelemetryIngestRPC interface {
	// AppendBatch appends a batch of events to the event log.
	AppendBatch(req AppendBatchRequest, resp *AppendBatchResponse) error

	// Flush forces all buffered events to be persisted.
	Flush(req FlushRequest, resp *FlushResponse) error

	// Ping checks if telemetryd is healthy and responsive.
	Ping(req PingRequest, resp *PingResponse) error
}

// Event represents a single telemetry event.
// The canonical event type is "gateway.attempt.completed".
type Event struct {
	// EventID is a unique identifier for this event.
	EventID string

	// EventType is the event type (e.g., "gateway.attempt.completed").
	EventType string

	// SchemaVersion is the event schema version.
	SchemaVersion int

	// SourceService identifies the source (e.g., "gatewayd").
	SourceService string

	// SourceInstance identifies the specific instance (for multi-gateway setups).
	SourceInstance string

	// EmittedAt is when the event was generated.
	EmittedAt time.Time

	// Imported indicates if this event was imported from legacy data.
	Imported bool

	// Payload contains the event-specific data.
	Payload EventPayload
}

// EventPayload contains the payload for a gateway.attempt.completed event.
type EventPayload struct {
	// RequestID is the unique identifier for the request.
	RequestID string

	// Timestamp is when the request was received.
	Timestamp time.Time

	// Path is the request path (e.g., "/v1/chat/completions").
	Path string

	// RequestedModel is the model name from the original request.
	RequestedModel string

	// EffectiveModel is the model that was actually used (after bridge/fallback).
	EffectiveModel string

	// ProviderID is the provider that handled the request.
	ProviderID string

	// RouteMode indicates how the request was routed (direct, bridged, bridge_fallback).
	RouteMode string

	// StatusCode is the HTTP status code returned.
	StatusCode int

	// Latency is the total request latency.
	Latency time.Duration

	// Attempts is the number of attempts made (including retries).
	Attempts int

	// PromptTokens is the number of input tokens used.
	PromptTokens int64

	// CachedPromptTokens is the number of cached prompt tokens.
	CachedPromptTokens int64

	// CompletionTokens is the number of output tokens generated.
	CompletionTokens int64

	// Stream indicates if this was a streaming request.
	Stream bool

	// Error contains any error message.
	Error string
}

// AppendBatchRequest contains a batch of events to append.
type AppendBatchRequest struct {
	// Events is the batch of events to append.
	Events []Event

	// BatchID is a unique identifier for this batch.
	BatchID string

	// SourceInstance identifies the gatewayd instance sending the batch.
	SourceInstance string
}

// AppendBatchResponse is returned after appending events.
type AppendBatchResponse struct {
	// Accepted is the number of events accepted.
	Accepted int

	// Dropped is the number of events dropped (e.g., due to backpressure).
	Dropped int

	// HighWatermark is the latest event ID that has been persisted.
	HighWatermark string

	// Error contains any error message.
	Error string
}

// FlushRequest asks telemetryd to flush all buffered events.
type FlushRequest struct {
	// Timeout is the maximum time to wait for flush to complete.
	Timeout time.Duration
}

// FlushResponse is returned after flushing.
type FlushResponse struct {
	// Success indicates if the flush completed.
	Success bool

	// FlushedCount is the number of events flushed.
	FlushedCount int

	// Error contains any error message.
	Error string
}

// PingRequest checks telemetryd health.
type PingRequest struct {
	// Timestamp is when the ping was sent.
	Timestamp time.Time
}

// PingResponse is returned from a ping.
type PingResponse struct {
	// Version is the telemetryd version.
	Version string

	// ServerTime is the current server time.
	ServerTime time.Time

	// EventCount is the total number of events stored.
	EventCount int64

	// Healthy indicates if telemetryd is healthy.
	Healthy bool
}
