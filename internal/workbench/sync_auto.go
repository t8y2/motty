package workbench

import (
	"errors"
	"time"

	"motty/internal/backup"
	"motty/internal/cloudsync"
	"motty/internal/vault"
)

// Syncing by itself. A change to anything that is shared, or to a saved
// password, starts a sync a few seconds later, so a burst of edits is sent once.
// The application also syncs once when it starts, to pick up what the other
// computers did meanwhile. There is no timer between those: an application with
// nothing to say to the server holds no timer and does no work, and what another
// computer changed arrives the next time this one syncs.

const (
	// syncDelayDefault is how long a change waits for more before it is sent.
	syncDelayDefault = 5 * time.Second
	// syncRetryBaseDefault is the wait before the first retry of a failed sync;
	// each further failure doubles it, up to syncRetryMax.
	syncRetryBaseDefault = 30 * time.Second
	syncRetryMax         = 30 * time.Minute
)

// scheduleSync asks for a sync soon. Windows of one process share the work: the
// first one does it, and the others hand their changes to it.
func (state *State) scheduleSync() {
	if state.sync.holdBack {
		return
	}
	leader := state.syncLeader()
	if leader != state {
		leader.scheduleSync()
		return
	}
	switch {
	case state.closed || !state.sync.configured() || state.sync.manual:
	case state.sync.working:
		state.sync.again = true
	case state.sync.timer == nil:
		state.armSyncTimer(state.syncDelay)
	case state.sync.retries > 0:
		// A change while a failed sync waits to be retried is worth trying now.
		state.armSyncTimer(state.syncDelay)
	}
}

// requestSync is scheduleSync from any goroutine, for a change the vault made. The
// vault is shared by the windows, so the window that set its stamper may be gone;
// then the oldest window still open takes the request.
func (state *State) requestSync() {
	target := state
	if state.closed && state.options.Group != nil {
		target = state.options.Group.first()
	}
	if target != nil {
		target.aiPost(func(state *State) { state.scheduleSync() })
	}
}

// armSyncTimer starts the one pending sync.
func (state *State) armSyncTimer(delay time.Duration) {
	state.stopSyncTimer()
	state.sync.timer = time.AfterFunc(delay, func() {
		state.aiPost(func(state *State) {
			state.sync.timer = nil
			state.autoSync()
		})
	})
}

func (state *State) stopSyncTimer() {
	if state.sync.timer != nil {
		state.sync.timer.Stop()
		state.sync.timer = nil
	}
}

// autoSync is a sync nobody asked for.
func (state *State) autoSync() {
	if state.closed || !state.sync.configured() || state.sync.manual {
		return
	}
	state.runSync(true)
}

// syncLeader is the window that syncs for the process.
func (state *State) syncLeader() *State {
	if group := state.options.Group; group != nil {
		if leader := group.first(); leader != nil {
			return leader
		}
	}
	return state
}

// reloadOtherWindows brings the other windows' shared data up to date after a
// sync took records in.
func (state *State) reloadOtherWindows() {
	if state.options.Group == nil {
		return
	}
	for _, window := range state.options.Group.windows() {
		if window != state && !window.closed && window.config != nil {
			_ = window.reloadShared()
		}
	}
}

// syncRetryable says whether trying again later could help: a network that was
// down can come back, a wrong password cannot.
func syncRetryable(err error) bool {
	switch {
	case errors.Is(err, cloudsync.ErrUnauthorized), errors.Is(err, cloudsync.ErrInsecure), errors.Is(err, cloudsync.ErrTooLarge),
		errors.Is(err, backup.ErrWrongPassword), errors.Is(err, backup.ErrNotBackup), errors.Is(err, backup.ErrTooNew),
		errors.Is(err, backup.ErrWeakPassword), errors.Is(err, vault.ErrNeedsConversion):
		return false
	}
	return true
}

// first is the oldest window still open.
func (group *Group) first() *State {
	for _, window := range group.windows() {
		if !window.closed {
			return window
		}
	}
	return nil
}

// windows is a copy of the group's windows, oldest first.
func (group *Group) windows() []*State {
	group.mu.Lock()
	defer group.mu.Unlock()
	return append([]*State(nil), group.members...)
}
