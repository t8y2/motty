package sessionlog

import (
	"fmt"
	"sync"
)

// queueBuffer bounds the operations waiting for the writer. Submit refuses a
// full queue instead of blocking the window event thread.
const queueBuffer = 64

// queuedOperation is one operation and the channel that reports its result.
type queuedOperation struct {
	operation Operation
	ack       chan error
	done      chan struct{}
}

// Queue runs the store's writes on one goroutine in submission order, so the UI
// event path never waits for SQLite. It has no callback hooks: the returned
// acknowledgment channel is the only notification, and no database work runs on
// the caller's goroutine.
//
// An acknowledgment channel is buffered for one result and then closed, so a
// caller may ignore it without blocking the writer. Close drains every submitted
// operation, stops the writer, and returns the first error the writer saw. Queue
// does not close its Store: close the Queue first (so pending writes land), then
// the Store. Submit after Close returns a closed channel holding ErrQueueClosed.
type Queue struct {
	store *Store
	ops   chan queuedOperation
	done  chan struct{}

	mu        sync.Mutex
	closed    bool
	errMu     sync.Mutex
	firstErr  error
	closeOnce sync.Once
	closeErr  error
	lastDone  <-chan struct{}
}

// NewQueue starts an ordered writer for store.
func NewQueue(store *Store) *Queue {
	queue := &Queue{
		store: store,
		ops:   make(chan queuedOperation, queueBuffer),
		done:  make(chan struct{}),
	}
	go queue.run()
	return queue
}

func (q *Queue) run() {
	defer close(q.done)
	for item := range q.ops {
		if item.operation.Wait != nil {
			<-item.operation.Wait
		}
		var err error
		if item.operation.Kind == OpWait {
			// A fence has no store side effect.
		} else {
			err = q.store.apply(item.operation)
		}
		if err != nil {
			q.errMu.Lock()
			if q.firstErr == nil {
				q.firstErr = err
			}
			q.errMu.Unlock()
		}
		item.ack <- err
		close(item.ack)
		close(item.done)
	}
}

// Submit queues one operation. Operations are applied in submission order, also
// when many goroutines submit. The returned channel receives exactly one error
// and is then closed. Submit never blocks for the database or a full queue.
func (q *Queue) Submit(operation Operation) <-chan error {
	operation.IDs = append([]string(nil), operation.IDs...)
	ack := make(chan error, 1)
	item := queuedOperation{operation: operation, ack: ack, done: make(chan struct{})}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		ack <- ErrQueueClosed
		close(ack)
		return ack
	}
	// A nonblocking send under mu is safe against Close and keeps a slow disk
	// from freezing the connection path.
	select {
	case q.ops <- item:
		q.lastDone = item.done
	default:
		q.errMu.Lock()
		if q.firstErr == nil {
			q.firstErr = ErrQueueFull
		}
		q.errMu.Unlock()
		ack <- ErrQueueFull
		close(ack)
	}
	q.mu.Unlock()
	return ack
}

// Fence is completed after all operations accepted before this call have run.
// It never needs a free slot in the bounded queue.
func (q *Queue) Fence() <-chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.lastDone
}

// Close flushes the submitted operations, stops the writer and returns the first
// write error it saw. It blocks until every operation that was submitted before
// the close has been applied. It is safe to call more than once, and it does not
// close the Store.
func (q *Queue) Close() error {
	q.closeOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		close(q.ops)
		q.mu.Unlock()
		<-q.done
		q.errMu.Lock()
		q.closeErr = q.firstErr
		q.errMu.Unlock()
	})
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closeErr
}

// apply performs one operation. The Queue calls it only from its writer.
func (s *Store) apply(operation Operation) error {
	switch operation.Kind {
	case OpStart:
		return s.Start(operation.Record)
	case OpAppendEvent:
		return s.AppendEvent(operation.Event)
	case OpFinish:
		return s.Finish(operation.ID, operation.Status, operation.EndedAt, operation.Reason)
	case OpSetFavorite:
		return s.SetFavorite(operation.ID, operation.Favorite)
	case OpDelete:
		return s.Delete(operation.ID)
	case OpClear:
		return s.Clear(operation.IncludeFavorites)
	case OpDeleteSelection:
		return s.DeleteSelection(operation.IDs, operation.IncludeFavorites)
	default:
		return fmt.Errorf("%w: operation kind %d", ErrInvalidRecord, operation.Kind)
	}
}
