package main

import (
	"net"
	"testing"
)

// mockTelemetryConn implements contracts.Conn for testing telemetry RPC
type mockTelemetryConn struct {
	readErr  error
	writeErr error
	closeErr error
}

func (m *mockTelemetryConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	return 0, nil
}

func (m *mockTelemetryConn) Write(b []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(b), nil
}

func (m *mockTelemetryConn) Close() error {
	return m.closeErr
}

func (m *mockTelemetryConn) LocalAddr() net.Addr {
	return nil
}

func (m *mockTelemetryConn) RemoteAddr() net.Addr {
	return nil
}

func TestNewIngestRPCServerNotNil(t *testing.T) {
	d := &Daemon{}
	server := NewIngestRPCServer(d)
	if server == nil {
		t.Fatal("NewIngestRPCServer() returned nil")
	}
}

func TestNewQueryRPCServerNotNil(t *testing.T) {
	d := &Daemon{}
	server := NewQueryRPCServer(d)
	if server == nil {
		t.Fatal("NewQueryRPCServer() returned nil")
	}
}

func TestIngestRPCServerServeConn(t *testing.T) {
	d := &Daemon{}
	server := NewIngestRPCServer(d)
	if server == nil {
		t.Fatal("NewIngestRPCServer() returned nil")
	}
	// ServeConn will block, but we can verify it doesn't panic
	// by calling it with a mock connection that returns errors
	conn := &mockTelemetryConn{readErr: &testError{"read error"}}
	// This should not panic
	server.ServeConn(conn)
}

func TestQueryRPCServerServeConn(t *testing.T) {
	d := &Daemon{}
	server := NewQueryRPCServer(d)
	if server == nil {
		t.Fatal("NewQueryRPCServer() returned nil")
	}
	conn := &mockTelemetryConn{readErr: &testError{"read error"}}
	server.ServeConn(conn)
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestIngestRPCConnAdapterReadWrite(t *testing.T) {
	conn := &mockTelemetryConn{}
	adapter := &connAdapter{conn: conn}

	if _, err := adapter.Read(nil); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if _, err := adapter.Write([]byte("test")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
