package workbench

import (
	"context"
	"errors"
	"sync"
)

// The assistant works on goroutines (a model request streams for seconds, a tool
// call waits for the user), but a State is only touched on the window thread. An
// aiInbox is how work crosses over: a goroutine posts a function, the window is
// woken, and Wake runs it with the State in hand.

var errAIClosed = errors.New("the window is closed")

type aiInbox struct {
	mu     sync.Mutex
	queue  []func(*State)
	closed bool
}

// post queues fn and reports whether it was accepted (false once closed).
func (inbox *aiInbox) post(fn func(*State)) bool {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	if inbox.closed {
		return false
	}
	inbox.queue = append(inbox.queue, fn)
	return true
}

// take removes and returns everything queued.
func (inbox *aiInbox) take(dst []func(*State)) []func(*State) {
	inbox.mu.Lock()
	defer inbox.mu.Unlock()
	dst = append(dst[:0], inbox.queue...)
	clear(inbox.queue)
	inbox.queue = inbox.queue[:0]
	return dst
}

func (inbox *aiInbox) close() {
	inbox.mu.Lock()
	inbox.closed = true
	inbox.queue = nil
	inbox.mu.Unlock()
}

// SetPost gives the window the application's main-thread post. Nil keeps the
// window's own inbox, which Wake drains.
func (state *State) SetPost(post func(func())) { state.post = post }

// aiPost runs fn on the window thread soon. It is safe from any goroutine.
func (state *State) aiPost(fn func(*State)) bool {
	return state.aiPostWithInvalidator(fn, state.options.Invalidate)
}

// aiPostWithInvalidator uses the callback captured when work was submitted.
// Window options may be replaced by tests while a journal write is in flight.
//
// With a Post the work runs on the loop as the application accepts it, and the
// repaint is asked for after it, so a paint cannot read the state the work has
// not changed yet. The inbox remains for tests and for a headless window, where
// Wake is the defined point work settles at.
func (state *State) aiPostWithInvalidator(fn func(*State), invalidate func()) bool {
	if state.callbacksClosed.Load() {
		return false
	}
	if state.post != nil {
		state.post(func() {
			if state.callbacksClosed.Load() {
				return
			}
			fn(state)
			if invalidate != nil {
				invalidate()
			}
		})
		return true
	}
	if state.ai.inbox == nil || !state.ai.inbox.post(fn) {
		return false
	}
	if invalidate != nil {
		invalidate()
	}
	return true
}

// drainAI runs what goroutines posted. Wake calls it.
func (state *State) drainAI() {
	if state.ai.inbox == nil {
		return
	}
	state.ai.drained = state.ai.inbox.take(state.ai.drained)
	for index, fn := range state.ai.drained {
		fn(state)
		state.ai.drained[index] = nil
	}
	// A question may have opened the panel; the terminals follow its width.
	state.aiPanelSync()
}

// aiCall runs fn on the window thread and waits for its result, or for ctx.
func aiCall[T any](state *State, ctx context.Context, fn func() (T, error)) (T, error) {
	type reply struct {
		value T
		err   error
	}
	done := make(chan reply, 1)
	if !state.aiPost(func(*State) {
		value, err := fn()
		done <- reply{value, err}
	}) {
		var zero T
		return zero, errAIClosed
	}
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
