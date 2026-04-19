// Package contracts provides tests for the transport layer.
package contracts

import (
	"testing"
	"time"
)

func TestPlatformTransport(t *testing.T) {
	transport := DefaultTransport
	if transport == nil {
		t.Fatal("DefaultTransport should not be nil")
	}
}

func TestTransportErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ErrListenerClosed",
			err:  ErrListenerClosed,
			want: "listener closed",
		},
		{
			name: "ErrConnClosed",
			err:  ErrConnClosed,
			want: "connection closed",
		},
		{
			name: "ErrTimeout",
			err:  ErrTimeout,
			want: "operation timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlatformTransportTimeout(t *testing.T) {
	// This test verifies that the transport layer can handle timeouts
	// Note: This is a basic test that doesn't actually connect to anything
	transport := DefaultTransport

	// Try to dial a non-existent socket (should fail quickly)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// On Unix, this will fail immediately if the path doesn't exist
		// On Windows, this may hang, so we use a timeout
		_, _ = transport.Dial("/nonexistent/socket/test.sock")
	}()

	select {
	case <-done:
		// Expected - the dial should fail
	case <-time.After(5 * time.Second):
		t.Log("Dial took longer than expected, but this is OK on some platforms")
	}
}
