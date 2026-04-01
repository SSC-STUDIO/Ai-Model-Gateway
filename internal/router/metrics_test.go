package router

import (
	"sync"
	"testing"
	"time"
)

func TestNoopMetricsCollector(t *testing.T) {
	// NoopMetricsCollector should do nothing without error
	collector := &NoopMetricsCollector{}

	// All methods should be callable without panic
	collector.RecordRouting("upstream1", "model1", true)
	collector.RecordLatency("upstream1", 100*time.Millisecond)
	collector.RecordHealthStatus("upstream1", true)
	collector.RecordRetry("upstream1", "timeout")
}

func TestInMemoryMetricsCollector_RecordRouting(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordRouting("upstream1", "gpt-4", true)
	collector.RecordRouting("upstream2", "gpt-3.5", false)
	collector.RecordRouting("upstream1", "gpt-4", true)

	events := collector.GetRoutingEvents()
	if len(events) != 3 {
		t.Errorf("expected 3 routing events, got %d", len(events))
	}

	// Check first event
	if events[0].Upstream != "upstream1" {
		t.Errorf("expected upstream1, got %s", events[0].Upstream)
	}
	if events[0].Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", events[0].Model)
	}
	if !events[0].Success {
		t.Error("expected success=true for first event")
	}

	// Check second event
	if events[1].Success {
		t.Error("expected success=false for second event")
	}
}

func TestInMemoryMetricsCollector_RecordLatency(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordLatency("upstream1", 100*time.Millisecond)
	collector.RecordLatency("upstream1", 200*time.Millisecond)
	collector.RecordLatency("upstream2", 50*time.Millisecond)

	records := collector.GetLatencyRecords()
	if len(records) != 3 {
		t.Errorf("expected 3 latency records, got %d", len(records))
	}

	// Check upstream1 records
	if records[0].Latency != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", records[0].Latency)
	}
	if records[1].Latency != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", records[1].Latency)
	}

	// Check upstream2 record
	if records[2].Upstream != "upstream2" {
		t.Errorf("expected upstream2, got %s", records[2].Upstream)
	}
}

func TestInMemoryMetricsCollector_RecordHealthStatus(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordHealthStatus("upstream1", true)
	collector.RecordHealthStatus("upstream1", false)
	collector.RecordHealthStatus("upstream2", true)

	records := collector.GetHealthRecords()
	if len(records) != 3 {
		t.Errorf("expected 3 health records, got %d", len(records))
	}

	if !records[0].Healthy {
		t.Error("expected first record to be healthy")
	}
	if records[1].Healthy {
		t.Error("expected second record to be unhealthy")
	}
}

func TestInMemoryMetricsCollector_RecordRetry(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordRetry("upstream1", "timeout")
	collector.RecordRetry("upstream1", "rate_limit")

	records := collector.GetRetryRecords()
	if len(records) != 2 {
		t.Errorf("expected 2 retry records, got %d", len(records))
	}

	if records[0].Reason != "timeout" {
		t.Errorf("expected reason 'timeout', got %s", records[0].Reason)
	}
	if records[1].Reason != "rate_limit" {
		t.Errorf("expected reason 'rate_limit', got %s", records[1].Reason)
	}
}

func TestInMemoryMetricsCollector_Reset(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordRouting("upstream1", "model1", true)
	collector.RecordLatency("upstream1", 100*time.Millisecond)
	collector.RecordHealthStatus("upstream1", true)
	collector.RecordRetry("upstream1", "timeout")

	collector.Reset()

	if len(collector.GetRoutingEvents()) != 0 {
		t.Error("expected routing events to be cleared")
	}
	if len(collector.GetLatencyRecords()) != 0 {
		t.Error("expected latency records to be cleared")
	}
	if len(collector.GetHealthRecords()) != 0 {
		t.Error("expected health records to be cleared")
	}
	if len(collector.GetRetryRecords()) != 0 {
		t.Error("expected retry records to be cleared")
	}
}

func TestInMemoryMetricsCollector_RoutingCount(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordRouting("upstream1", "model1", true)
	collector.RecordRouting("upstream1", "model2", true)
	collector.RecordRouting("upstream2", "model1", false)

	if count := collector.RoutingCount("upstream1"); count != 2 {
		t.Errorf("expected 2 routings for upstream1, got %d", count)
	}
	if count := collector.RoutingCount("upstream2"); count != 1 {
		t.Errorf("expected 1 routing for upstream2, got %d", count)
	}
	if count := collector.RoutingCount("upstream3"); count != 0 {
		t.Errorf("expected 0 routings for upstream3, got %d", count)
	}
}

func TestInMemoryMetricsCollector_AverageLatency(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	collector.RecordLatency("upstream1", 100*time.Millisecond)
	collector.RecordLatency("upstream1", 200*time.Millisecond)
	collector.RecordLatency("upstream1", 300*time.Millisecond)

	avg := collector.AverageLatency("upstream1")
	expected := 200 * time.Millisecond
	if avg != expected {
		t.Errorf("expected average latency %v, got %v", expected, avg)
	}

	// Test unknown upstream
	if avg := collector.AverageLatency("unknown"); avg != 0 {
		t.Errorf("expected 0 for unknown upstream, got %v", avg)
	}
}

func TestInMemoryMetricsCollector_Concurrency(t *testing.T) {
	collector := NewInMemoryMetricsCollector()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			collector.RecordRouting("upstream1", "model1", i%2 == 0)
			collector.RecordLatency("upstream1", time.Duration(i)*time.Millisecond)
			collector.RecordHealthStatus("upstream1", i%2 == 0)
		}(i)
	}
	wg.Wait()

	events := collector.GetRoutingEvents()
	if len(events) != 100 {
		t.Errorf("expected 100 routing events, got %d", len(events))
	}

	records := collector.GetLatencyRecords()
	if len(records) != 100 {
		t.Errorf("expected 100 latency records, got %d", len(records))
	}

	healthRecords := collector.GetHealthRecords()
	if len(healthRecords) != 100 {
		t.Errorf("expected 100 health records, got %d", len(healthRecords))
	}
}

func TestInMemoryMetricsCollector_Timestamps(t *testing.T) {
	before := time.Now()
	collector := NewInMemoryMetricsCollector()

	collector.RecordRouting("upstream1", "model1", true)

	events := collector.GetRoutingEvents()
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}

	after := time.Now()
	if events[0].Timestamp.Before(before) {
		t.Error("timestamp should not be before test start")
	}
	if events[0].Timestamp.After(after) {
		t.Error("timestamp should not be after test end")
	}
}

func TestRoutingEvent_Struct(t *testing.T) {
	event := RoutingEvent{
		Timestamp: time.Now(),
		Upstream:  "test-upstream",
		Model:     "gpt-4",
		Success:   true,
	}

	if event.Upstream != "test-upstream" {
		t.Errorf("expected Upstream=test-upstream, got %s", event.Upstream)
	}
	if event.Model != "gpt-4" {
		t.Errorf("expected Model=gpt-4, got %s", event.Model)
	}
	if !event.Success {
		t.Error("expected Success=true")
	}
}

func TestLatencyRecord_Struct(t *testing.T) {
	record := LatencyRecord{
		Timestamp: time.Now(),
		Upstream:  "test-upstream",
		Latency:   150 * time.Millisecond,
	}

	if record.Latency != 150*time.Millisecond {
		t.Errorf("expected Latency=150ms, got %v", record.Latency)
	}
}

func TestHealthRecord_Struct(t *testing.T) {
	record := HealthRecord{
		Timestamp: time.Now(),
		Upstream:  "test-upstream",
		Healthy:   false,
	}

	if record.Healthy {
		t.Error("expected Healthy=false")
	}
}

func TestRetryRecord_Struct(t *testing.T) {
	record := RetryRecord{
		Timestamp: time.Now(),
		Upstream:  "test-upstream",
		Reason:    "timeout",
	}

	if record.Reason != "timeout" {
		t.Errorf("expected Reason=timeout, got %s", record.Reason)
	}
}
