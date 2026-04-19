package main

import (
	"net"
	"testing"
)

// mockConn implements contracts.Conn for testing
type mockConn struct {
	readErr  error
	writeErr error
	closeErr error
	readData []byte
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	if len(m.readData) > 0 {
		copy(b, m.readData)
		return len(m.readData), nil
	}
	return 0, nil
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(b), nil
}

func (m *mockConn) Close() error {
	return m.closeErr
}

func (m *mockConn) LocalAddr() net.Addr {
	return nil
}

func (m *mockConn) RemoteAddr() net.Addr {
	return nil
}

func TestGatewayClientCloseWithNilClient(t *testing.T) {
	client := &GatewayClient{client: nil}
	if err := client.Close(); err != nil {
		t.Fatalf("Close with nil client returned error: %v", err)
	}
}

func TestTelemetryClientCloseWithNilClient(t *testing.T) {
	client := &TelemetryClient{client: nil}
	if err := client.Close(); err != nil {
		t.Fatalf("Close with nil client returned error: %v", err)
	}
}

func TestNewGatewayClient(t *testing.T) {
	conn := &mockConn{}
	client := NewGatewayClient(conn)
	if client == nil {
		t.Fatal("NewGatewayClient() returned nil")
	}
}

func TestNewTelemetryClient(t *testing.T) {
	conn := &mockConn{}
	client := NewTelemetryClient(conn)
	if client == nil {
		t.Fatal("NewTelemetryClient() returned nil")
	}
}

func TestGatewayClientConnAdapter(t *testing.T) {
	conn := &mockConn{}
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

func TestTelemetryClientConnAdapter(t *testing.T) {
	conn := &mockConn{}
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

func TestGatewayClientClose(t *testing.T) {
	conn := &mockConn{}
	client := NewGatewayClient(conn)
	// Close will fail because there's no actual connection, but we test Close doesn't panic
	_ = client.Close()
}

func TestTelemetryClientClose(t *testing.T) {
	conn := &mockConn{}
	client := NewTelemetryClient(conn)
	_ = client.Close()
}
