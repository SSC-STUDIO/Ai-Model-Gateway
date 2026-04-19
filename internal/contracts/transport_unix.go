//go:build !windows

package contracts

import (
	"fmt"
	"net"
	"os"
)

// platformTransport implements Transport using Unix domain sockets.
type platformTransport struct{}

func (t *platformTransport) Listen(addr string) (Listener, error) {
	// Ensure the socket directory exists
	if dir := dirOf(addr); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create socket directory: %w", err)
		}
	}

	// Clean up stale socket file
	if err := cleanupStaleSocket(addr); err != nil {
		return nil, fmt.Errorf("cleanup stale socket: %w", err)
	}

	// Create Unix domain socket listener
	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("listen unix socket %s: %w", addr, err)
	}

	// Restrict socket permissions to owner only (security)
	if err := os.Chmod(addr, 0600); err != nil {
		l.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return &unixListener{Listener: l}, nil
}

func (t *platformTransport) Dial(addr string) (Conn, error) {
	conn, err := net.Dial("unix", addr)
	if err != nil {
		return nil, err
	}
	return &unixConn{Conn: conn}, nil
}

// DefaultTransport returns the platform-appropriate transport implementation.
var DefaultTransport Transport = &platformTransport{}

type unixListener struct {
	net.Listener
}

func (l *unixListener) Accept() (Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &unixConn{Conn: c}, nil
}

type unixConn struct {
	net.Conn
}

// cleanupStaleSocket removes a stale socket file if it exists.
// If the socket is actively in use by another process, it returns an error.
func cleanupStaleSocket(addr string) error {
	info, err := os.Stat(addr)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No stale socket, clean start
		}
		return err
	}

	// Verify it's actually a socket
	if info.Mode()&os.ModeType != os.ModeSocket {
		// Not a socket - don't remove it (could be a regular file)
		return fmt.Errorf("path %s exists but is not a socket", addr)
	}

	// Check if the socket is actively in use
	if isSocketInUse(addr) {
		return fmt.Errorf("socket %s is actively in use by another process", addr)
	}

	// Stale socket - remove it
	if err := os.Remove(addr); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	return nil
}

// isSocketInUse checks if a Unix socket is actively being listened on.
func isSocketInUse(addr string) bool {
	conn, err := net.DialTimeout("unix", addr, 0)
	if err != nil {
		return false // Connection refused = not in use
	}
	conn.Close()
	return true
}

// dirOf extracts the directory portion of a file path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}
