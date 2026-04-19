//go:build !windows

package contracts

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// TestUnixTransportRoundTrip tests a full Unix socket listen/dial/send/close round-trip.
func TestUnixTransportRoundTrip(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-gateway-contracts.sock"

	// Start listener
	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	// Ensure socket is cleaned up
	defer cleanupSocket(addr)

	// Server goroutine
	serverDone := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- fmt.Sprintf("accept error: %v", err)
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- fmt.Sprintf("read error: %v", err)
			return
		}

		_, err = conn.Write([]byte("pong"))
		if err != nil {
			serverDone <- fmt.Sprintf("write error: %v", err)
			return
		}

		serverDone <- string(buf[:n])
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Dial client
	conn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Send ping
	_, err = conn.Write([]byte("ping"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read response
	respBuf := make([]byte, 1024)
	n, err := io.ReadFull(conn, respBuf[:4])
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify
	if got := string(respBuf[:n]); got != "pong" {
		t.Errorf("Expected pong, got %s", got)
	}

	select {
	case msg := <-serverDone:
		if msg != "ping" {
			t.Errorf("Server received unexpected message: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Server timeout")
	}
}

// TestUnixTransportStaleSocketCleanup verifies that stale sockets are cleaned up on Listen.
func TestUnixTransportStaleSocketCleanup(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-gateway-stale.sock"

	// Clean up any existing socket
	cleanupSocket(addr)

	// Create first listener
	listener1, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("First Listen failed: %v", err)
	}

	// Close first listener (socket file remains)
	listener1.Close()

	// Create second listener - should clean up stale socket
	listener2, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Second Listen failed (stale socket not cleaned): %v", err)
	}
	defer listener2.Close()
	defer cleanupSocket(addr)

	// Verify we can still dial
	serverDone := make(chan struct{})
	go func() {
		conn, err := listener2.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-serverDone
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := transport.Dial(addr)
	if err != nil {
		t.Fatalf("Dial after stale cleanup failed: %v", err)
	}
	defer conn.Close()

	close(serverDone)
}

// TestUnixTransportConcurrentConnections tests multiple concurrent connections.
func TestUnixTransportConcurrentConnections(t *testing.T) {
	transport := DefaultTransport
	addr := "/tmp/test-gateway-concurrent.sock"

	listener, err := transport.Listen(addr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()
	defer cleanupSocket(addr)

	// Echo server
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c Conn) {
				defer c.Close()
				buf := make([]byte, 256)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, err = c.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Run concurrent clients
	numClients := 10
	done := make(chan bool, numClients)

	for i := 0; i < numClients; i++ {
		go func(id int) {
			conn, err := transport.Dial(addr)
			if err != nil {
				t.Errorf("Client %d dial failed: %v", id, err)
				done <- false
				return
			}
			defer conn.Close()

			msg := fmt.Sprintf("client-%d", id)
			_, err = conn.Write([]byte(msg))
			if err != nil {
				t.Errorf("Client %d write failed: %v", id, err)
				done <- false
				return
			}

			buf := make([]byte, len(msg))
			_, err = io.ReadFull(conn, buf)
			if err != nil {
				t.Errorf("Client %d read failed: %v", id, err)
				done <- false
				return
			}

			if string(buf) != msg {
				t.Errorf("Client %d expected %s, got %s", id, msg, string(buf))
				done <- false
				return
			}

			done <- true
		}(i)
	}

	successCount := 0
	for i := 0; i < numClients; i++ {
		select {
		case ok := <-done:
			if ok {
				successCount++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Client timeout")
		}
	}

	if successCount != numClients {
		t.Fatalf("Only %d/%d clients succeeded", successCount, numClients)
	}
}

func cleanupSocket(addr string) {
	// Best-effort cleanup using os.Remove
	os.Remove(addr)
}
