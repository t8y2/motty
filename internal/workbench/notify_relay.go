package workbench

import "sync/atomic"

// notifyRelay is the wake-up a session's goroutines (its connection, shell,
// file worker and monitor) call when something changed. It is one level between
// them and the window's own Invalidate, so that a session which moves to another
// window takes its goroutines' wake-ups along: they keep calling the relay, and
// the relay now calls the new window.
type notifyRelay struct {
	target atomic.Pointer[func()]
	// call is the method value handed to goroutines; made once.
	call func()
}

func newNotifyRelay(first func()) *notifyRelay {
	relay := &notifyRelay{}
	relay.set(first)
	relay.call = func() {
		if target := relay.target.Load(); target != nil && *target != nil {
			(*target)()
		}
	}
	return relay
}

// set points the relay at a window's Invalidate.
func (relay *notifyRelay) set(target func()) { relay.target.Store(&target) }

// notifyForView wakes whatever shows the view: the session's relay, which
// follows it into a detached window, and that window's own paint.
func (state *State) notifyForView(session *Session, view *sftpView) func() {
	notify := state.notifyFor(session)
	return func() {
		notify()
		view.invalidateDetached()
	}
}

// notifyFor is the wake-up to give a session's goroutines.
func (state *State) notifyFor(session *Session) func() {
	if session.relay == nil {
		session.relay = newNotifyRelay(state.options.Invalidate)
	}
	return session.relay.call
}
