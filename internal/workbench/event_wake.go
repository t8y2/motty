package workbench

import "time"

// scheduleWake asks the window's event loop to run Wake after d. A timer that
// advances connection state cannot rely on a paint: a hidden, minimized, or
// covered window does not get one, and the loop still runs the work on time.
// The returned stop cancels the remaining call; nil means the host cannot
// schedule wakes (a headless test drives Wake itself).
func (state *State) scheduleWake(d time.Duration) func() {
	if state.options.PostAfter == nil || state.callbacksClosed.Load() {
		return nil
	}
	return state.options.PostAfter(d, func() {
		if state.callbacksClosed.Load() {
			return
		}
		state.Wake()
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	})
}
