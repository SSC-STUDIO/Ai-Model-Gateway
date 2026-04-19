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
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	if m.readErr != nil {
		return 0, m.readErr
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

func TestNewRPCClient(t *testing.T) {
	conn := &mockConn{}
	client := NewRPCClient(conn)
	if client == nil {
		t.Fatal("NewRPCClient() returned nil")
	}
}

func TestRPCClientClose(t *testing.T) {
	conn := &mockConn{}
	client := NewRPCClient(conn)
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestConnAdapter(t *testing.T) {
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
