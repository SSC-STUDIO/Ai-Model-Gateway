// Package telemetry provides tests for the telemetry client.
package telemetry

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
)

// mockRPCClient is a mock RPC client for testing.
type mockRPCClient struct {
	callCount  atomic.Int32
	failCount  int
	failErr    error
	shouldFail func() bool
}

func (m *mockRPCClient) Call(method string, args interface{}, reply interface{}) error {
	m.callCount.Add(1)
	if m.shouldFail != nil && m.shouldFail() {
		return m.failErr
	}
	if m.failCount > 0 {
		m.failCount--
		return m.failErr
	}

	if resp, ok := reply.(*telemetryingest.AppendBatchResponse); ok {
		req := args.(telemetryingest.AppendBatchRequest)
		resp.Accepted = len(req.Events)
		resp.Dropped = 0
	}
	return nil
}

func (m *mockRPCClient) Close() error {
	return nil
}

func TestClientEmitAndFlush(t *testing.T) {
	mock := &mockRPCClient{}
	client := NewClient(mock, DefaultClientConfig())
	defer client.Close()

	event := telemetryingest.Event{
		EventID:   "evt-1",
		EventType: "gateway.attempt.completed",
		Payload: telemetryingest.EventPayload{
			RequestID:  "req-1",
			StatusCode: 200,
		},
	}

	if err := client.Emit(event); err != nil {
		t.Fatalf("Emit failed: %v", err)
	}

	// Force flush
	if err := client.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if mock.callCount.Load() == 0 {
		t.Error("Expected RPC call after flush")
	}

	sent, dropped := client.Stats()
	if sent != 1 {
		t.Errorf("Expected 1 sent, got %d", sent)
	}
	if dropped != 0 {
		t.Errorf("Expected 0 dropped, got %d", dropped)
	}
}

func TestClientBatching(t *testing.T) {
	mock := &mockRPCClient{}
	config := DefaultClientConfig()
	config.BatchSize = 5
	client := NewClient(mock, config)
	defer client.Close()

	// Emit 4 events - should not trigger flush
	for i := 0; i < 4; i++ {
		if err := client.Emit(telemetryingest.Event{EventID: fmt.Sprintf("evt-%d", i)}); err != nil {
			t.Fatalf("Emit event %d failed: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)
	if mock.callCount.Load() != 0 {
		t.Error("Expected no RPC calls before batch is full")
	}

	// Emit 1 more event - should trigger flush
	if err := client.Emit(telemetryingest.Event{EventID: "evt-4"}); err != nil {
		t.Fatalf("Emit batch trigger event failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if mock.callCount.Load() != 1 {
		t.Errorf("Expected 1 RPC call after batch full, got %d", mock.callCount.Load())
	}
}

func TestClientReconnection(t *testing.T) {
	callCount := atomic.Int32{}
	dialCount := atomic.Int32{}

	dialer := func() (RPCClient, error) {
		count := dialCount.Add(1)
		return &mockRPCClient{
			shouldFail: func() bool {
				// First client fails on first call, second client succeeds
				return count == 1 && callCount.Load() == 0
			},
			failErr: fmt.Errorf("broken pipe"),
		}, nil
	}

	config := DefaultClientConfig()
	config.ReconnectInterval = 10 * time.Millisecond
	client, err := NewClientWithDialer(dialer, config)
	if err != nil {
		t.Fatalf("NewClientWithDialer failed: %v", err)
	}
	defer client.Close()

	// Emit an event - should trigger reconnection after failure
	if err := client.Emit(telemetryingest.Event{EventID: "evt-1", EventType: "gateway.attempt.completed"}); err != nil {
		t.Fatalf("Emit reconnection event failed: %v", err)
	}
	client.Flush()

	// Give reconnection time to happen
	time.Sleep(100 * time.Millisecond)

	if dialCount.Load() < 2 {
		t.Errorf("Expected at least 2 dials (initial + reconnect), got %d", dialCount.Load())
	}

	sent, _ := client.Stats()
	if sent != 1 {
		t.Errorf("Expected 1 sent after reconnect, got %d", sent)
	}
}

func TestClientReconnectionExhausted(t *testing.T) {
	dialCount := atomic.Int32{}

	dialer := func() (RPCClient, error) {
		dialCount.Add(1)
		return &mockRPCClient{
			shouldFail: func() bool { return true },
			failErr:    syscall.ECONNREFUSED,
		}, nil
	}

	config := DefaultClientConfig()
	config.ReconnectInterval = 10 * time.Millisecond
	config.MaxReconnectAttempts = 2
	client, err := NewClientWithDialer(dialer, config)
	if err != nil {
		t.Fatalf("NewClientWithDialer failed: %v", err)
	}
	defer client.Close()

	if err := client.Emit(telemetryingest.Event{EventID: "evt-1", EventType: "gateway.attempt.completed"}); err != nil {
		t.Fatalf("Emit retry exhaustion event failed: %v", err)
	}
	client.Flush()

	time.Sleep(200 * time.Millisecond)

	_, dropped := client.Stats()
	if dropped != 1 {
		t.Errorf("Expected 1 dropped after exhausted reconnects, got %d", dropped)
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "random error",
			err:  errors.New("something went wrong"),
			want: false,
		},
		{
			name: "net.Error timeout",
			err:  &net.OpError{Op: "dial", Err: errors.New("timeout")},
			want: true,
		},
		{
			name: "syscall ECONNREFUSED",
			err:  syscall.ECONNREFUSED,
			want: true,
		},
		{
			name: "syscall EPIPE",
			err:  syscall.EPIPE,
			want: true,
		},
		{
			name: "net.ErrClosed",
			err:  net.ErrClosed,
			want: true,
		},
		{
			name: "broken pipe message",
			err:  errors.New("write: broken pipe"),
			want: true,
		},
		{
			name: "connection refused message",
			err:  errors.New("connection refused"),
			want: true,
		},
		{
			name: "wrapped connection error",
			err:  fmt.Errorf("wrapped: %w", syscall.ECONNRESET),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionError(tt.err)
			if got != tt.want {
				t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClientContextCancellation(t *testing.T) {
	mock := &mockRPCClient{}
	client := NewClient(mock, DefaultClientConfig())

	client.Close()

	// After close, Emit should fail
	err := client.Emit(telemetryingest.Event{EventID: "evt-after-close"})
	if err == nil {
		t.Error("Expected error after client close")
	}
}
