// Package telemetry provides the telemetry client for gatewayd to emit events to telemetryd.
package telemetry

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"ai-model-gateway/internal/contracts/telemetryingest"
)

// Client is the telemetry client for gatewayd.
// It batches events and sends them asynchronously to telemetryd.
// Supports automatic reconnection when the connection is lost.
type Client struct {
	mu sync.RWMutex

	// Dialer function for creating new RPC connections
	dialer RPCDialer

	// Current RPC client (may be replaced on reconnect)
	rpcClient atomic.Pointer[RPCClient]

	// Configuration
	config ClientConfig

	// Buffer for batching
	buffer []telemetryingest.Event

	// Channel for signaling flush
	flushCh chan chan struct{}

	// Background context
	ctx    context.Context
	cancel context.CancelFunc

	// Wait group for graceful shutdown
	wg sync.WaitGroup

	// Metrics
	droppedCount atomic.Int64
	sentCount    atomic.Int64

	// Reconnection state
	reconnecting atomic.Bool
	lastError    atomic.Pointer[error]
}

// RPCClient is the interface for RPC communication.
type RPCClient interface {
	Call(method string, args interface{}, reply interface{}) error
	Close() error
}

// RPCDialer creates a new RPC connection.
type RPCDialer func() (RPCClient, error)

// ClientConfig contains the telemetry client configuration.
type ClientConfig struct {
	// BatchSize is the maximum batch size before flush.
	BatchSize int

	// FlushInterval is the interval between flushes.
	FlushInterval time.Duration

	// QueueSize is the size of the event queue.
	QueueSize int

	// SourceInstance identifies this gatewayd instance.
	SourceInstance string

	// ReconnectInterval is the interval between reconnection attempts.
	ReconnectInterval time.Duration

	// MaxReconnectAttempts is the maximum number of reconnection attempts (0 = unlimited).
	MaxReconnectAttempts int
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		BatchSize:            256,
		FlushInterval:        100 * time.Millisecond,
		QueueSize:            1024,
		SourceInstance:       "gatewayd",
		ReconnectInterval:    5 * time.Second,
		MaxReconnectAttempts: 0, // unlimited
	}
}

// NewClient creates a new telemetry client.
func NewClient(rpcClient RPCClient, config ClientConfig) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		config:  config,
		buffer:  make([]telemetryingest.Event, 0, config.BatchSize),
		flushCh: make(chan chan struct{}, 4),
		ctx:     ctx,
		cancel:  cancel,
	}
	c.rpcClient.Store(&rpcClient)
	c.wg.Add(1)
	go c.batchLoop()
	return c
}

// NewClientWithDialer creates a new telemetry client with reconnection support.
func NewClientWithDialer(dialer RPCDialer, config ClientConfig) (*Client, error) {
	rpcClient, err := dialer()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		dialer:  dialer,
		config:  config,
		buffer:  make([]telemetryingest.Event, 0, config.BatchSize),
		flushCh: make(chan chan struct{}, 4),
		ctx:     ctx,
		cancel:  cancel,
	}
	c.rpcClient.Store(&rpcClient)
	c.wg.Add(1)
	go c.batchLoop()
	return c, nil
}

// Emit emits a telemetry event.
// This is non-blocking and returns immediately.
// Events are buffered and sent in batches.
func (c *Client) Emit(event telemetryingest.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if context is cancelled
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}

	// Set source instance if not set
	if event.SourceInstance == "" {
		event.SourceInstance = c.config.SourceInstance
	}

	// Add to buffer
	c.buffer = append(c.buffer, event)

	// Flush if buffer is full
	if len(c.buffer) >= c.config.BatchSize {
		go c.flush()
	}

	return nil
}

// Flush flushes all buffered events.
// This blocks until the flush is complete.
func (c *Client) Flush() error {
	done := make(chan struct{})
	select {
	case c.flushCh <- done:
		<-done
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

// Close closes the client and flushes remaining events.
func (c *Client) Close() error {
	c.cancel()
	c.wg.Wait()

	// Close the RPC client
	if client := c.rpcClient.Load(); client != nil && *client != nil {
		_ = (*client).Close()
	}
	return nil
}

// Stats returns the client statistics.
func (c *Client) Stats() (sent, dropped int64) {
	return c.sentCount.Load(), c.droppedCount.Load()
}

// IsConnected returns whether the client has an active connection.
func (c *Client) IsConnected() bool {
	return c.rpcClient.Load() != nil && !c.reconnecting.Load()
}

// flush flushes the current buffer.
func (c *Client) flush() {
	c.mu.Lock()
	if len(c.buffer) == 0 {
		c.mu.Unlock()
		return
	}

	// Swap buffer
	events := c.buffer
	c.buffer = make([]telemetryingest.Event, 0, c.config.BatchSize)
	c.mu.Unlock()

	// Send batch
	c.sendBatch(events)
}

// sendBatch sends a batch of events to telemetryd with reconnection support.
func (c *Client) sendBatch(events []telemetryingest.Event) {
	if len(events) == 0 {
		return
	}

	// Generate batch ID
	batchID := generateBatchID()

	// Create request
	req := telemetryingest.AppendBatchRequest{
		Events:         events,
		BatchID:        batchID,
		SourceInstance: c.config.SourceInstance,
	}

	// Try to send with reconnection
	maxAttempts := c.config.MaxReconnectAttempts
	if maxAttempts == 0 {
		maxAttempts = 1000 // effectively unlimited
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client := c.rpcClient.Load()
		if client == nil || *client == nil {
			if !c.reconnect() {
				c.droppedCount.Add(int64(len(events)))
				return
			}
			client = c.rpcClient.Load()
		}

		var resp telemetryingest.AppendBatchResponse
		err := (*client).Call("TelemetryIngestRPC.AppendBatch", req, &resp)
		if err != nil {
			log.Printf("[telemetry] send batch error (attempt %d): %v", attempt+1, err)

			// Check if we should reconnect
			if isConnectionError(err) {
				c.reconnecting.Store(true)
				if c.reconnect() {
					c.reconnecting.Store(false)
					continue // retry with new connection
				}
				c.reconnecting.Store(false)
			}

			// If this was our last attempt or we can't reconnect, drop the events
			if attempt == maxAttempts-1 || c.ctx.Err() != nil {
				c.droppedCount.Add(int64(len(events)))
				return
			}

			// Wait before retrying
			time.Sleep(c.config.ReconnectInterval)
			continue
		}

		// Success
		c.sentCount.Add(int64(resp.Accepted))
		c.droppedCount.Add(int64(resp.Dropped))

		if resp.Dropped > 0 {
			log.Printf("[telemetry] batch %s: %d accepted, %d dropped", batchID, resp.Accepted, resp.Dropped)
		}
		return
	}

	// All attempts failed
	c.droppedCount.Add(int64(len(events)))
}

// reconnect attempts to establish a new connection.
func (c *Client) reconnect() bool {
	if c.dialer == nil {
		return false
	}

	// Close old client
	if oldClient := c.rpcClient.Load(); oldClient != nil && *oldClient != nil {
		_ = (*oldClient).Close()
	}

	// Dial new connection
	newClient, err := c.dialer()
	if err != nil {
		log.Printf("[telemetry] reconnect failed: %v", err)
		return false
	}

	c.rpcClient.Store(&newClient)
	log.Printf("[telemetry] reconnected successfully")
	return true
}

// batchLoop runs the background batching loop.
func (c *Client) batchLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Final flush on shutdown
			c.flush()
			return

		case <-ticker.C:
			c.flush()

		case done := <-c.flushCh:
			c.flush()
			close(done)
		}
	}
}

// isConnectionError checks if an error indicates a connection problem.
// Uses type checking with errors.As for robust detection.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for net.Error types (timeout, temporary)
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// Check for syscall errors (connection refused, broken pipe, etc.)
	var syscallErr syscall.Errno
	if errors.As(err, &syscallErr) {
		switch syscallErr {
		case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.EPIPE,
			syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH:
			return true
		}
	}

	// Check for specific error types via errors.Is
	if errors.Is(err, net.ErrClosed) {
		return true
	}

	// Fallback: check for common error message patterns
	// This handles wrapped errors that don't expose their type
	errStr := err.Error()
	return containsSubstring(errStr, "broken pipe") ||
		containsSubstring(errStr, "reset by peer") ||
		containsSubstring(errStr, "connection refused") ||
		containsSubstring(errStr, "connection reset") ||
		containsSubstring(errStr, "use of closed")
}

func containsSubstring(s, substr string) bool {
	if len(substr) == 0 || len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// generateBatchID generates a unique batch ID.
func generateBatchID() string {
	return time.Now().UTC().Format("20060102-150405.000") + "-" + randomString(8)
}

// randomString generates a random string of the given length.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	for i := range b {
		b[i] = letters[int(buf[i])%len(letters)]
	}
	return string(b)
}
