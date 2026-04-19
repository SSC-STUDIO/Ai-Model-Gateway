//go:build windows

package contracts

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// platformTransport implements Transport using Windows named pipes.
type platformTransport struct{}

func (t *platformTransport) Listen(addr string) (Listener, error) {
	// Convert address to full pipe path
	pipePath := `\\.\pipe\` + addr

	// Create named pipe listener with security attributes
	// Allow only local access for security
	l, err := winio.ListenPipe(pipePath, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;AU)", // Allow generic access to authenticated users
		MessageMode:        false,             // Byte-stream mode
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	})
	if err != nil {
		return nil, err
	}

	return &pipeListener{Listener: l, addr: &pipeAddr{name: addr}}, nil
}

func (t *platformTransport) Dial(addr string) (Conn, error) {
	pipePath := `\\.\pipe\` + addr

	// Dial with timeout
	conn, err := winio.DialPipe(pipePath, nil)
	if err != nil {
		return nil, err
	}

	return &pipeConn{Conn: conn}, nil
}

// DefaultTransport returns the platform-appropriate transport implementation.
var DefaultTransport Transport = &platformTransport{}

type pipeListener struct {
	net.Listener
	addr net.Addr
}

func (l *pipeListener) Accept() (Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &pipeConn{Conn: c}, nil
}

func (l *pipeListener) Addr() net.Addr {
	return l.addr
}

type pipeConn struct {
	net.Conn
}

type pipeAddr struct {
	name string
}

func (a *pipeAddr) Network() string {
	return "pipe"
}

func (a *pipeAddr) String() string {
	return `\\.\pipe\` + a.name
}

// DialPipeTimeout connects to a named pipe with a timeout.
func DialPipeTimeout(addr string, timeout time.Duration) (Conn, error) {
	pipePath := `\\.\pipe\` + addr
	conn, err := winio.DialPipe(pipePath, &timeout)
	if err != nil {
		return nil, err
	}
	return &pipeConn{Conn: conn}, nil
}
