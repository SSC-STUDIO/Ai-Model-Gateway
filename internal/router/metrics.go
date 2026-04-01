package router

import (
	"sync"
	"time"
)

// MetricsCollector defines the interface for routing metrics collection.
// This abstraction allows for different metrics backends.
type MetricsCollector interface {
	// RecordRouting records a routing decision.
	RecordRouting(upstream string, model string, success bool)

	// RecordLatency records the latency of an upstream request.
	RecordLatency(upstream string, latency time.Duration)

	// RecordHealthStatus records the health status of an upstream.
	RecordHealthStatus(upstream string, healthy bool)

	// RecordRetry records a retry attempt.
	RecordRetry(upstream string, reason string)
}

// NoopMetricsCollector is a metrics collector that does nothing.
// Useful for testing or when metrics are disabled.
type NoopMetricsCollector struct{}

// RecordRouting is a no-op implementation.
func (n *NoopMetricsCollector) RecordRouting(upstream string, model string, success bool) {}

// RecordLatency is a no-op implementation.
func (n *NoopMetricsCollector) RecordLatency(upstream string, latency time.Duration) {}

// RecordHealthStatus is a no-op implementation.
func (n *NoopMetricsCollector) RecordHealthStatus(upstream string, healthy bool) {}

// RecordRetry is a no-op implementation.
func (n *NoopMetricsCollector) RecordRetry(upstream string, reason string) {}

// InMemoryMetricsCollector collects metrics in memory for testing.
type InMemoryMetricsCollector struct {
	mu             sync.RWMutex
	routingEvents  []RoutingEvent
	latencyRecords []LatencyRecord
	healthRecords  []HealthRecord
	retryRecords   []RetryRecord
}

// RoutingEvent represents a routing decision.
type RoutingEvent struct {
	Timestamp time.Time
	Upstream  string
	Model     string
	Success   bool
}

// LatencyRecord represents a latency measurement.
type LatencyRecord struct {
	Timestamp time.Time
	Upstream  string
	Latency   time.Duration
}

// HealthRecord represents a health status recording.
type HealthRecord struct {
	Timestamp time.Time
	Upstream  string
	Healthy   bool
}

// RetryRecord represents a retry event.
type RetryRecord struct {
	Timestamp time.Time
	Upstream  string
	Reason    string
}

// NewInMemoryMetricsCollector creates a new in-memory metrics collector.
func NewInMemoryMetricsCollector() *InMemoryMetricsCollector {
	return &InMemoryMetricsCollector{
		routingEvents:  make([]RoutingEvent, 0),
		latencyRecords: make([]LatencyRecord, 0),
		healthRecords:  make([]HealthRecord, 0),
		retryRecords:   make([]RetryRecord, 0),
	}
}

// RecordRouting records a routing decision.
func (m *InMemoryMetricsCollector) RecordRouting(upstream string, model string, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routingEvents = append(m.routingEvents, RoutingEvent{
		Timestamp: time.Now(),
		Upstream:  upstream,
		Model:     model,
		Success:   success,
	})
}

// RecordLatency records the latency of an upstream request.
func (m *InMemoryMetricsCollector) RecordLatency(upstream string, latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencyRecords = append(m.latencyRecords, LatencyRecord{
		Timestamp: time.Now(),
		Upstream:  upstream,
		Latency:   latency,
	})
}

// RecordHealthStatus records the health status of an upstream.
func (m *InMemoryMetricsCollector) RecordHealthStatus(upstream string, healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthRecords = append(m.healthRecords, HealthRecord{
		Timestamp: time.Now(),
		Upstream:  upstream,
		Healthy:   healthy,
	})
}

// RecordRetry records a retry attempt.
func (m *InMemoryMetricsCollector) RecordRetry(upstream string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryRecords = append(m.retryRecords, RetryRecord{
		Timestamp: time.Now(),
		Upstream:  upstream,
		Reason:    reason,
	})
}

// GetRoutingEvents returns all recorded routing events.
func (m *InMemoryMetricsCollector) GetRoutingEvents() []RoutingEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]RoutingEvent, len(m.routingEvents))
	copy(result, m.routingEvents)
	return result
}

// GetLatencyRecords returns all recorded latency records.
func (m *InMemoryMetricsCollector) GetLatencyRecords() []LatencyRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]LatencyRecord, len(m.latencyRecords))
	copy(result, m.latencyRecords)
	return result
}

// GetHealthRecords returns all recorded health records.
func (m *InMemoryMetricsCollector) GetHealthRecords() []HealthRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]HealthRecord, len(m.healthRecords))
	copy(result, m.healthRecords)
	return result
}

// GetRetryRecords returns all recorded retry records.
func (m *InMemoryMetricsCollector) GetRetryRecords() []RetryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]RetryRecord, len(m.retryRecords))
	copy(result, m.retryRecords)
	return result
}

// Reset clears all recorded metrics.
func (m *InMemoryMetricsCollector) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routingEvents = m.routingEvents[:0]
	m.latencyRecords = m.latencyRecords[:0]
	m.healthRecords = m.healthRecords[:0]
	m.retryRecords = m.retryRecords[:0]
}

// RoutingCount returns the number of routing events for an upstream.
func (m *InMemoryMetricsCollector) RoutingCount(upstream string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, event := range m.routingEvents {
		if event.Upstream == upstream {
			count++
		}
	}
	return count
}

// AverageLatency returns the average latency for an upstream.
func (m *InMemoryMetricsCollector) AverageLatency(upstream string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total time.Duration
	count := 0
	for _, record := range m.latencyRecords {
		if record.Upstream == upstream {
			total += record.Latency
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / time.Duration(count)
}
