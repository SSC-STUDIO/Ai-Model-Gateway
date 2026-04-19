// Package contracts defines the IPC contracts and transport layer for inter-daemon communication.
package contracts

import (
	"errors"
	"net"
)

// Transport abstracts platform-specific IPC mechanisms.
// Linux uses Unix domain sockets, Windows uses named pipes.
type Transport interface {
	// Listen creates a listener at the given address.
	// On Linux, addr is a filesystem path like "/run/gateway-control.sock".
	// On Windows, addr is a pipe name like "gateway-control".
	Listen(addr string) (Listener, error)

	// Dial connects to a listener at the given address.
	Dial(addr string) (Conn, error)
}

// Listener is a platform-agnostic listener interface.
type Listener interface {
	// Accept waits for and returns the next connection.
	Accept() (Conn, error)

	// Close closes the listener.
	Close() error

	// Addr returns the listener's network address.
	Addr() net.Addr
}

// Conn is a platform-agnostic connection interface.
type Conn interface {
	// Read reads data from the connection.
	Read(b []byte) (n int, err error)

	// Write writes data to the connection.
	Write(b []byte) (n int, err error)

	// Close closes the connection.
	Close() error

	// LocalAddr returns the local network address.
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address.
	RemoteAddr() net.Addr
}

// Errors returned by the transport layer.
var (
	ErrListenerClosed = errors.New("listener closed")
	ErrConnClosed     = errors.New("connection closed")
	ErrTimeout        = errors.New("operation timed out")
)
