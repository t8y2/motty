package workbench

import (
	"context"
	"time"

	"motty/internal/linkwatch"
	"motty/internal/sshconn"
)

// linkVerifyTimeout bounds how long a connection may take to answer after the
// machine's network path changed. It is shorter than a keepalive interval: the
// question is asked because something already happened, so a slow answer is
// itself the answer.
const linkVerifyTimeout = 3 * time.Second

// verifier is a connection that can be asked, once, whether its transport still
// works. Only a real SSH connection can: a local shell, a serial line and a test
// double are left alone, which is why this is a type assertion rather than part
// of Connection.
type verifier interface {
	Verify(context.Context) error
}

// linkWatchNeeded reports whether any open tab still depends on the machine's
// network path. A tab counts when it is not a shell on this computer and either
// has a connection that is being made or is up, or is waiting out (or gave up
// on) a retry: a path change is what revives a retry whose backoff is waiting
// for the network, and what makes a live transport verify itself.
//
// A restored tab that has not been dialed, a local shell, and a tab whose
// connection ended for good have nothing to watch, so none of them keeps the
// 2-second poll running on its own.
func (state *State) linkWatchNeeded() bool {
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.isLocal() {
			continue
		}
		if session.host.mosh() {
			// The mosh client owns its UDP path: it keeps the session through
			// an address change and retries by itself. Nothing here can verify
			// or revive it, and a 2-second poll would be pure idle work.
			continue
		}
		if session.reconnectPending || session.reconnectExhausted {
			return true
		}
		if session.conn == nil {
			continue
		}
		if session.phase == sshconn.PhaseFailed || session.phase == sshconn.PhaseClosed {
			continue
		}
		return true
	}
	return false
}

// syncLinkWatch starts or stops the network watcher so it runs exactly while
// some tab still depends on the network path. It runs on the window's thread,
// where the session list changes; a window with nothing to watch polls nothing.
func (state *State) syncLinkWatch() {
	if state.options.WatchNetworkChanges == nil || state.closed {
		return
	}
	if state.linkWatchNeeded() {
		if state.stopLinkWatch == nil {
			state.stopLinkWatch = state.options.WatchNetworkChanges(state.linkChanged)
		}
		return
	}
	state.stopNetworkWatch()
}

// stopNetworkWatch ends the watcher, if one is running, and forgets a change
// that has already been seen: it belonged to tabs that are no longer live, so
// the next tab that connects must not apply it.
func (state *State) stopNetworkWatch() {
	if state.stopLinkWatch != nil {
		state.stopLinkWatch()
		state.stopLinkWatch = nil
	}
	state.linkPending.Store(false)
}

// linkChanged is the watcher's report. It runs on the watcher's goroutine, so it
// records the change and wakes the window; sessions are touched from Wake, on
// the window's own thread.
func (state *State) linkChanged(change linkwatch.Change) {
	if state.closed {
		return
	}
	state.linkUp.Store(change.Up)
	state.linkPending.Store(true)
	if invalidate := state.options.Invalidate; invalidate != nil {
		invalidate()
	}
}

// applyLinkChange acts on the change Wake found. Every established connection is
// asked whether it survived, because the transport was bound to addresses that
// are gone: no FIN was sent, so nothing else would report the loss until the
// next keepalive tick or the user's next keystroke.
func (state *State) applyLinkChange() {
	if !state.linkPending.Swap(false) {
		return
	}
	if state.linkUp.Load() {
		// The reason a retry was waiting out a backoff is gone.
		state.restartReconnects()
	}
	for index := range state.sessions {
		state.verifyLink(&state.sessions[index])
	}
}

// verifyLink asks one session's connection whether it survived the change. The
// answer is not read here: a failure records the disconnect on the transport,
// which ends it, which is what Wake turns into a reconnect.
func (state *State) verifyLink(session *Session) {
	if session == nil || session.isLocal() || session.conn == nil || session.phase != sshconn.PhaseConnected {
		return
	}
	conn, ok := session.conn.(verifier)
	if !ok {
		return
	}
	notify := state.notifyFor(session)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), linkVerifyTimeout)
		defer cancel()
		if err := conn.Verify(ctx); err != nil {
			notify()
		}
	}()
}

// restartReconnects lets a retry that was waiting, or had given up, try again:
// the network it was waiting for is back. Tabs sharing one transport are all
// reset, but only the owner's attempt actually dials.
//
// This is the one place a policy that already gave up is allowed to try again,
// so it is bounded by how often the network path changes: each change buys one
// policy's worth of attempts, never a loop.
func (state *State) restartReconnects() {
	for index := range state.sessions {
		session := &state.sessions[index]
		pending := session.reconnectPending
		if !pending && !session.reconnectExhausted {
			continue
		}
		session.reconnectAttempt = 0
		session.reconnectExhausted = false
		session.reconnectText = ""
		if pending {
			session.reconnectDue = state.now()
			continue
		}
		if state.connectionReconnect != reconnectOff {
			state.queueReconnect(session)
		}
	}
}
