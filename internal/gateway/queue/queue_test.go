package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewQueue_BasicConstruction(t *testing.T) {
	q := NewQueue(4, 70)
	defer q.Close()

	if q == nil {
		t.Fatal("expected non-nil Queue")
	}
	stats := q.Stats()
	if stats.MaxConcurrent != 4 {
		t.Fatalf("expected MaxConcurrent=4, got %d", stats.MaxConcurrent)
	}
	if stats.ActiveCount != 0 {
		t.Fatalf("expected ActiveCount=0, got %d", stats.ActiveCount)
	}
}

func TestNewQueue_PanicsOnInvalidArgs(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for maxConcurrent=0")
		}
	}()
	NewQueue(0, 50)
}

func TestNewQueue_PanicsOnNegativePct(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for negative highPriorityPct")
		}
	}()
	NewQueue(4, -1)
}

func TestNewQueue_PanicsOnOverPct(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for highPriorityPct > 100")
		}
	}()
	NewQueue(4, 101)
}

func TestEnqueue_ExecuteRequest(t *testing.T) {
	q := NewQueue(2, 60)
	defer q.Close()

	var executed atomic.Int32

	err := q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
		Execute: func() error {
			executed.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for the scheduler to pick up and execute the request.
	deadline := time.After(2 * time.Second)
	for {
		if executed.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for request execution")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	if executed.Load() != 1 {
		t.Fatalf("expected 1 execution, got %d", executed.Load())
	}
}

func TestEnqueue_AllPriorities(t *testing.T) {
	q := NewQueue(4, 60)
	defer q.Close()

	var highCount, medCount, lowCount atomic.Int32

	enq := func(p Priority, counter *atomic.Int32) {
		err := q.Enqueue(context.Background(), p, PendingRequest{
			Execute: func() error {
				counter.Add(1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error enqueuing priority %d: %v", p, err)
		}
	}

	enq(PriorityHigh, &highCount)
	enq(PriorityMedium, &medCount)
	enq(PriorityLow, &lowCount)

	deadline := time.After(2 * time.Second)
	for {
		if highCount.Load() >= 1 && medCount.Load() >= 1 && lowCount.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out: high=%d med=%d low=%d",
				highCount.Load(), medCount.Load(), lowCount.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestEnqueue_PreCancelledContext(t *testing.T) {
	// Use an already-cancelled context to verify Enqueue returns
	// the context error immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	q := NewQueue(2, 50)
	defer q.Close()

	err := q.Enqueue(ctx, PriorityHigh, PendingRequest{
		Execute: func() error { return nil },
	})
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestEnqueue_CancelWhileBlocked(t *testing.T) {
	// Create a queue with concurrency 1 and fill the low-priority channel
	// buffer so a subsequent Enqueue blocks and can be cancelled.
	q := NewQueue(1, 50)
	defer q.Close()

	// Hold the single slot with a blocking request.
	blockSem := make(chan struct{})
	err := q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
		Execute: func() error {
			<-blockSem
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait until active.
	deadline := time.After(2 * time.Second)
	for {
		if q.Stats().ActiveCount >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for active request")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Let schedulers settle so they block on the empty channels.
	time.Sleep(50 * time.Millisecond)

	// Fill the low-priority channel buffer entirely.
	for i := 0; i < cap(q.lowCh); i++ {
		q.lowCh <- PendingRequest{Priority: PriorityLow, Execute: func() error { return nil }}
		q.lowWaiting.Add(1)
	}

	// Enqueue with a context that will be cancelled shortly.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = q.Enqueue(ctx, PriorityLow, PendingRequest{
		Execute: func() error { return nil },
	})
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	// Unblock so the queue can drain and close.
	close(blockSem)
}

func TestStats_ConcurrentExecution(t *testing.T) {
	const maxConc = 3
	q := NewQueue(maxConc, 50)

	var started atomic.Int32
	var release atomic.Bool

	for i := 0; i < maxConc; i++ {
		err := q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
			Execute: func() error {
				started.Add(1)
				for !release.Load() {
					time.Sleep(1 * time.Millisecond)
				}
				return nil
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Wait for all 3 to start executing.
	deadline := time.After(3 * time.Second)
	for {
		if started.Load() >= int32(maxConc) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for starts, got %d", started.Load())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	stats := q.Stats()
	if stats.ActiveCount != maxConc {
		t.Fatalf("expected ActiveCount=%d, got %d", maxConc, stats.ActiveCount)
	}

	// Release all.
	release.Store(true)
	q.Close()
}

func TestPriorityOrdering(t *testing.T) {
	// Use a queue with concurrency 1 so requests are dispatched
	// one at a time. We verify that the scheduler picks high-priority
	// requests before lower-priority ones.
	q := NewQueue(1, 60)

	var mu sync.Mutex
	var order []Priority

	// Block the single slot so we can enqueue multiple requests.
	gate := make(chan struct{})

	// Enqueue a blocking request first to hold the slot.
	err := q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
		Execute: func() error {
			<-gate
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for it to become active.
	deadline := time.After(2 * time.Second)
	for {
		if q.Stats().ActiveCount >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for active request")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Enqueue low first, then medium, then high.
	// Each request records its priority at the start of execution.
	err = q.Enqueue(context.Background(), PriorityLow, PendingRequest{
		Execute: func() error {
			mu.Lock()
			order = append(order, PriorityLow)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = q.Enqueue(context.Background(), PriorityMedium, PendingRequest{
		Execute: func() error {
			mu.Lock()
			order = append(order, PriorityMedium)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
		Execute: func() error {
			mu.Lock()
			order = append(order, PriorityHigh)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Give the scheduler a moment to drain the queued requests into
	// its internal pick state, then release the gate.
	time.Sleep(50 * time.Millisecond)
	close(gate)

	// Wait for all to complete.
	deadline = time.After(3 * time.Second)
	for {
		mu.Lock()
		done := len(order) >= 3
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for completions, order so far: %v", order)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	q.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("expected 3 completions, got %d: %v", len(order), order)
	}

	// The high-priority request should be first (index 0) since the
	// scheduler drains high before medium and low.
	if order[0] != PriorityHigh {
		t.Fatalf("expected first executed to be PriorityHigh, got %v (order: %v)", order[0], order)
	}
}

func TestClose_TerminatesSchedulers(t *testing.T) {
	q := NewQueue(2, 50)
	q.Close()

	// After Close, Enqueue should return an error (queue context cancelled).
	err := q.Enqueue(context.Background(), PriorityHigh, PendingRequest{
		Execute: func() error { return nil },
	})
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}

func TestEnqueue_ExecuteReturnsError(t *testing.T) {
	q := NewQueue(2, 50)
	defer q.Close()

	var executed atomic.Bool

	err := q.Enqueue(context.Background(), PriorityMedium, PendingRequest{
		Execute: func() error {
			executed.Store(true)
			return errors.New("something failed")
		},
	})
	if err != nil {
		t.Fatalf("unexpected enqueue error: %v", err)
	}

	// The error is silently consumed (the queue does not propagate it),
	// but the request should still execute.
	deadline := time.After(2 * time.Second)
	for {
		if executed.Load() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for execution")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestEnqueue_DefaultPriority(t *testing.T) {
	q := NewQueue(2, 50)
	defer q.Close()

	// Use an arbitrary Priority value (not one of the defined constants).
	var executed atomic.Bool

	err := q.Enqueue(context.Background(), Priority(99), PendingRequest{
		Execute: func() error {
			executed.Store(true)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		if executed.Load() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for default-priority execution")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestStats_Initial(t *testing.T) {
	q := NewQueue(8, 80)
	defer q.Close()

	stats := q.Stats()
	if stats.HighWaiting != 0 {
		t.Fatalf("expected HighWaiting=0, got %d", stats.HighWaiting)
	}
	if stats.MedWaiting != 0 {
		t.Fatalf("expected MedWaiting=0, got %d", stats.MedWaiting)
	}
	if stats.LowWaiting != 0 {
		t.Fatalf("expected LowWaiting=0, got %d", stats.LowWaiting)
	}
	if stats.ActiveCount != 0 {
		t.Fatalf("expected ActiveCount=0, got %d", stats.ActiveCount)
	}
	if stats.MaxConcurrent != 8 {
		t.Fatalf("expected MaxConcurrent=8, got %d", stats.MaxConcurrent)
	}
}
