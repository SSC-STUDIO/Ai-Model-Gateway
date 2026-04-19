//go:build !windows

package contracts

import (
	"net"
	"os"
	"testing"
)

// TestDirOf tests the directory extraction function.
func TestDirOf(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		want  string
	}{
		{
			name: "simple path",
			path: "/tmp/test.sock",
			want: "/tmp",
		},
		{
			name: "nested path",
			path: "/var/run/gateway/control.sock",
			want: "/var/run/gateway",
		},
		{
			name: "root level",
			path: "/test.sock",
			want: "",
		},
		{
			name: "no slash",
			path: "test.sock",
			want: "",
		},
		{
			name: "empty string",
			path: "",
			want: "",
		},
		{
			name: "multiple trailing slashes",
			path: "/tmp/run///test.sock",
			want: "/tmp/run//",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dirOf(tt.path); got != tt.want {
				t.Errorf("dirOf(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestCleanupStaleSocket tests the stale socket cleanup logic.
func TestCleanupStaleSocket(t *testing.T) {
	t.Run("nonexistent socket", func(t *testing.T) {
		addr := "/tmp/nonexistent-test-socket-12345.sock"
		err := cleanupStaleSocket(addr)
		if err != nil {
			t.Errorf("cleanupStaleSocket for nonexistent socket should return nil, got: %v", err)
		}
	})

	t.Run("regular file not socket", func(t *testing.T) {
		addr := "/tmp/test-regular-file.sock"
		// Create a regular file (not a socket)
		f, err := os.Create(addr)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
		f.Close()
		defer os.Remove(addr)

		err = cleanupStaleSocket(addr)
		if err == nil {
			t.Error("cleanupStaleSocket should return error for non-socket file")
		}
	})

	t.Run("stale socket cleanup", func(t *testing.T) {
		addr := "/tmp/test-stale-socket-cleanup.sock"
		// Clean up any existing
		os.Remove(addr)

		// Create a listener and close it to leave a stale socket
		l, err := net.Listen("unix", addr)
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		l.Close()

		// Verify socket file exists
		if _, err := os.Stat(addr); err != nil {
			t.Fatalf("Socket file should exist: %v", err)
		}

		// cleanupStaleSocket should remove it since it's not in use
		err = cleanupStaleSocket(addr)
		if err != nil {
			t.Errorf("cleanupStaleSocket failed: %v", err)
		}

		// Verify socket was removed
		if _, err := os.Stat(addr); !os.IsNotExist(err) {
			t.Errorf("Socket file should have been removed")
			os.Remove(addr)
		}
	})
}

// TestIsSocketInUse tests the socket in-use detection.
func TestIsSocketInUse(t *testing.T) {
	t.Run("nonexistent socket", func(t *testing.T) {
		addr := "/tmp/nonexistent-inuse-check.sock"
		if isSocketInUse(addr) {
			t.Error("isSocketInUse should return false for nonexistent socket")
		}
	})

	t.Run("active socket", func(t *testing.T) {
		addr := "/tmp/test-active-socket.sock"
		os.Remove(addr)

		l, err := net.Listen("unix", addr)
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		defer l.Close()
		defer os.Remove(addr)

		// Socket is being listened on, should be in use
		if !isSocketInUse(addr) {
			t.Error("isSocketInUse should return true for active listener")
		}
	})

	t.Run("closed socket", func(t *testing.T) {
		addr := "/tmp/test-closed-socket.sock"
		os.Remove(addr)

		l, err := net.Listen("unix", addr)
		if err != nil {
			t.Fatalf("Failed to create listener: %v", err)
		}
		l.Close()
		defer os.Remove(addr)

		// Socket is closed, should not be in use
		if isSocketInUse(addr) {
			t.Error("isSocketInUse should return false for closed socket")
		}
	})
}

// TestListenCreatesDirectory tests that Listen creates the socket directory if needed.
func TestListenCreatesDirectory(t *testing.T) {
	transport := DefaultTransport
	dir := "/tmp/test-gateway-socket-dir"
	addr := dir + "/subdir/test.sock"

	// Clean up any existing directory
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	// Listen should create the directory
	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	// Verify directory was created
	if _, err := os.Stat(dir + "/subdir"); err != nil {
		t.Errorf("Socket directory should have been created: %v", err)
	}

	// Verify socket permissions
	info, err := os.Stat(addr)
	if err != nil {
		t.Fatalf("Socket file should exist: %v", err)
	}

	// Socket should have 0600 permissions
	expectedPerms := os.FileMode(0600)
	if info.Mode().Perm() != expectedPerms {
		t.Errorf("Socket permissions = %v, want %v", info.Mode().Perm(), expectedPerms)
	}
}

// TestListenSocketInUseError tests that Listen fails when socket is in use.
func TestListenSocketInUseError(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-socket-inuse.sock"
	os.Remove(addr)

	// Create first listener
	listener1, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("First Listen failed: %v", err)
	}
	defer listener1.Close()
	defer os.Remove(addr)

	// Second listener should fail because socket is in use
	_, err = transport.Listen(addr)
	if err == nil {
		t.Error("Second Listen should fail when socket is in use")
	}
}

// TestUnixListenerAddr tests the listener address method.
func TestUnixListenerAddr(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-listener-addr.sock"
	os.Remove(addr)

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()
	defer os.Remove(addr)

	// Check Addr() returns correct network and address
	listenerAddr := listener.Addr()
	if listenerAddr.Network() != "unix" {
		t.Errorf("Addr().Network() = %v, want unix", listenerAddr.Network())
	}
	if listenerAddr.String() != addr {
		t.Errorf("Addr().String() = %v, want %v", listenerAddr.String(), addr)
	}
}

// TestConnAddresses tests the connection address methods.
func TestConnAddresses(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-conn-addr.sock"
	os.Remove(addr)

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()
	defer os.Remove(addr)

	serverReady := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		close(serverReady)
		// Keep connection open until test completes
		select {}
	}()

	// Give server time to start
	<-serverReady

	conn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// LocalAddr and RemoteAddr should not panic/return nil
	localAddr := conn.LocalAddr()
	remoteAddr := conn.RemoteAddr()

	if localAddr == nil {
		t.Error("LocalAddr should not be nil")
	}
	if remoteAddr == nil {
		t.Error("RemoteAddr should not be nil")
	}
}
