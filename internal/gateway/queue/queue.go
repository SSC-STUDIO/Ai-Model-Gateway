// Package queue provides a three-tier priority request queue with
// semaphore-based concurrency control and weighted scheduling.
package queue

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"ai-model-gateway/internal/infra/logger"
)

// Priority represents the priority level of a queued request.
// Lower numeric values indicate higher priority.
type Priority int

const (
	PriorityHigh   Priority = 0
	PriorityMedium Priority = 1
	PriorityLow    Priority = 2
)

// PendingRequest represents a request waiting to be executed.
type PendingRequest struct {
	Priority Priority
	Execute  func() error
}

// QueueStats holds a snapshot of queue state.
type QueueStats struct {
	HighWaiting   int `json:"high_waiting"`
	MedWaiting    int `json:"medium_waiting"`
	LowWaiting    int `json:"low_waiting"`
	ActiveCount   int `json:"active_count"`
	MaxConcurrent int `json:"max_concurrent"`
}

// Queue is a three-tier priority queue that schedules requests using
// strict priority ordering across high, medium and low priority channels.
// Concurrency is bounded by a semaphore.
type Queue struct {
	highCh  chan PendingRequest
	medCh   chan PendingRequest
	lowCh   chan PendingRequest
	sem     chan struct{}
	notify  chan struct{} // signals scheduler that new work is available
	maxConc int

	// Atomic counters for fast stats reads.
	highWaiting atomic.Int64
	medWaiting  atomic.Int64
	lowWaiting  atomic.Int64
	activeCount atomic.Int64

	// closed is set atomically when Close is called.
	closed atomic.Bool

	// Lifecycle management.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewQueue creates a priority queue that allows up to maxConcurrent
// requests to execute concurrently. highPriorityPct (0-100) controls
// the percentage of scheduler ticks dedicated to the high-priority
// channel. The remaining capacity is split equally between medium and
// low priority channels.
//
// Panics if maxConcurrent <= 0 or highPriorityPct is not in [0,100].
func NewQueue(maxConcurrent int, highPriorityPct int) *Queue {
	if maxConcurrent <= 0 {
		panic("queue: maxConcurrent must be > 0")
	}
	if highPriorityPct < 0 || highPriorityPct > 100 {
		panic("queue: highPriorityPct must be in [0,100]")
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &Queue{
		highCh:  make(chan PendingRequest, maxConcurrent*4),
		medCh:   make(chan PendingRequest, maxConcurrent*4),
		lowCh:   make(chan PendingRequest, maxConcurrent*4),
		sem:     make(chan struct{}, maxConcurrent),
		notify:  make(chan struct{}, maxConcurrent*4),
		maxConc: maxConcurrent,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start scheduler goroutines. We run multiple schedulers so that
	// bursts of requests can be dispatched in parallel.
	numSchedulers := maxConcurrent
	if numSchedulers < 1 {
		numSchedulers = 1
	}
	for i := 0; i < numSchedulers; i++ {
		q.wg.Add(1)
		go q.schedule()
	}

	return q
}

// Enqueue adds a request to the appropriate priority channel.
// It blocks until the request is accepted into the channel buffer or
// until ctx is cancelled. Returns an error if the queue is closed or
// the context is already cancelled.
func (q *Queue) Enqueue(ctx context.Context, priority Priority, req PendingRequest) error {
	// Fast check: refuse enqueues on a closed queue.
	if q.closed.Load() {
		return context.Canceled
	}

	// Fast check: refuse if caller context is already done.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	req.Priority = priority

	var ch chan PendingRequest
	switch priority {
	case PriorityHigh:
		ch = q.highCh
		q.highWaiting.Add(1)
	case PriorityMedium:
		ch = q.medCh
		q.medWaiting.Add(1)
	case PriorityLow:
		ch = q.lowCh
		q.lowWaiting.Add(1)
	default:
		ch = q.medCh
		q.medWaiting.Add(1)
	}

	select {
	case ch <- req:
		// Signal the scheduler that new work is available.
		// Non-blocking send: if the notify channel is full, schedulers
		// are already awake and will see the work on the next pick.
		select {
		case q.notify <- struct{}{}:
		default:
		}
		return nil
	case <-ctx.Done():
		// Roll back the waiting counter we just incremented.
		switch priority {
		case PriorityHigh:
			q.highWaiting.Add(-1)
		case PriorityMedium:
			q.medWaiting.Add(-1)
		case PriorityLow:
			q.lowWaiting.Add(-1)
		}
		return ctx.Err()
	case <-q.ctx.Done():
		switch priority {
		case PriorityHigh:
			q.highWaiting.Add(-1)
		case PriorityMedium:
			q.medWaiting.Add(-1)
		case PriorityLow:
			q.lowWaiting.Add(-1)
		}
		return q.ctx.Err()
	}
}

// Stats returns a snapshot of the current queue state.
func (q *Queue) Stats() QueueStats {
	return QueueStats{
		HighWaiting:   int(q.highWaiting.Load()),
		MedWaiting:    int(q.medWaiting.Load()),
		LowWaiting:    int(q.lowWaiting.Load()),
		ActiveCount:   int(q.activeCount.Load()),
		MaxConcurrent: q.maxConc,
	}
}

// Close stops all scheduler goroutines and waits for them to finish.
// In-flight requests are allowed to complete. Subsequent Enqueue calls
// return an error.
func (q *Queue) Close() {
	q.closed.Store(true)
	q.cancel()
	q.wg.Wait()
}

// schedule is the main scheduling loop. It uses weighted selection to
// pick the next priority channel to drain, then acquires a semaphore
// slot and executes the request in a separate goroutine.
func (q *Queue) schedule() {
	defer q.wg.Done()

	for {
		// Check if the queue is shutting down.
		select {
		case <-q.ctx.Done():
			return
		default:
		}

		req, ok := q.pick()
		if !ok {
			// Context cancelled during pick.
			continue
		}

		// Decrement the waiting counter for the chosen priority.
		switch req.Priority {
		case PriorityHigh:
			q.highWaiting.Add(-1)
		case PriorityMedium:
			q.medWaiting.Add(-1)
		case PriorityLow:
			q.lowWaiting.Add(-1)
		}

		// Acquire semaphore slot.
		select {
		case q.sem <- struct{}{}:
		case <-q.ctx.Done():
			return
		}

		q.activeCount.Add(1)

		go func(r PendingRequest) {
			defer func() {
				<-q.sem
				q.activeCount.Add(-1)
				if rec := recover(); rec != nil {
					// A panic inside Execute is a bug in the request handler, not in the
					// queue itself. Log it with a full stack trace so the team can triage
					// without crashing the gatewayd process (Go would otherwise terminate
					// on an unrecovered panic).
					logger.Error("queue worker panic recovered",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(debug.Stack()),
					)
				}
			}()
			_ = r.Execute()
		}(req)
	}
}

// pick performs priority-ordered selection across the three channels.
// It always drains in strict order: high -> medium -> low.
// When no work is available, it blocks on a notify signal and retries
// the priority scan, avoiding Go's random select behavior.
func (q *Queue) pick() (PendingRequest, bool) {
	for {
		// Try strict priority order.
		select {
		case req := <-q.highCh:
			return req, true
		default:
		}

		select {
		case req := <-q.medCh:
			return req, true
		default:
		}

		select {
		case req := <-q.lowCh:
			return req, true
		default:
		}

		// No work available. Wait for a notification or shutdown.
		select {
		case <-q.notify:
			// New work may be available; loop back to priority scan.
			continue
		case <-q.ctx.Done():
			return PendingRequest{}, false
		}
	}
}
