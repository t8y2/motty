package workbench

import (
	"time"

	"github.com/vivaui/viva/ui"
)

// The window as the desktop sees it: where it is and whether the window manager
// minimized it. Only the first window of a process remembers its frame, so
// tearing a tab off does not overwrite where the main window was.

const (
	// windowFrameSaveDelay is how long a window drag waits for more movement
	// before the frame is written. A drag reports every intermediate position.
	windowFrameSaveDelay = 400 * time.Millisecond
)

type windowChrome struct {
	remember          bool
	frame             ui.Rect
	hasFrame          bool
	minimized         bool
	saveTimer         *time.Timer
	fullscreen        bool
	fullscreenSession uint32
	setFullscreen     func(bool) error
}

// RestoredWindowFrame is where the last first window was, if a previous run
// saved it. The window opens there and at that size.
func (state *State) RestoredWindowFrame() (ui.Rect, bool) {
	return state.winChrome.frame, state.winChrome.hasFrame
}

// restoreWindowFrame takes the frame a saved state carries and shares it, so
// another window's later save writes the same place instead of erasing it.
func (state *State) restoreWindowFrame(frame ui.Rect) {
	if frame.Width < 1 || frame.Height < 1 {
		return
	}
	state.winChrome.frame, state.winChrome.hasFrame = frame, true
	if group := state.options.Group; group != nil {
		group.setWindowFrame(frame)
	}
}

// NoteWindowFrame records where the window is, as Viva reports every move and
// size change, and saves it once the drag settles. Windows that do not remember
// their frame ignore it.
func (state *State) NoteWindowFrame(frame ui.Rect) {
	if !state.winChrome.remember || state.closed || state.winChrome.fullscreen || state.winChrome.fullscreenSession != 0 {
		return
	}
	if state.winChrome.hasFrame && state.winChrome.frame == frame {
		return
	}
	state.winChrome.frame, state.winChrome.hasFrame = frame, true
	if group := state.options.Group; group != nil {
		group.setWindowFrame(frame)
	}
	state.scheduleWindowFrameSave()
}

// scheduleWindowFrameSave writes the frame after the window stops moving. The
// timer is the only one a still window does not hold.
func (state *State) scheduleWindowFrameSave() {
	if state.winChrome.saveTimer != nil {
		state.winChrome.saveTimer.Stop()
	}
	state.winChrome.saveTimer = time.AfterFunc(windowFrameSaveDelay, func() {
		state.aiPost(func(*State) {
			state.winChrome.saveTimer = nil
			if !state.closed {
				state.save()
			}
		})
	})
}

// closeWindowChrome stops the pending save and writes it now, so a quit in the
// middle of the delay keeps the place the window was.
func (state *State) closeWindowChrome() {
	if state.winChrome.saveTimer == nil {
		return
	}
	state.winChrome.saveTimer.Stop()
	state.winChrome.saveTimer = nil
	state.save()
}

// savedWindowFrame is the frame to write: the newest one any window of the
// process reported, so a secondary window's save does not roll it back.
func (state *State) savedWindowFrame() (ui.Rect, bool) {
	if group := state.options.Group; group != nil {
		if frame, ok := group.windowFrame(); ok {
			return frame, true
		}
	}
	return state.winChrome.frame, state.winChrome.hasFrame
}

// SetWindowMinimized records what the window manager did, and wakes the window
// so remote desktops stop asking for pictures nobody can see.
func (state *State) SetWindowMinimized(minimized bool) {
	if state.winChrome.minimized == minimized {
		return
	}
	state.winChrome.minimized = minimized
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

// WindowMinimized reports whether the window manager has the window minimized.
func (state *State) WindowMinimized() bool {
	return state.winChrome.minimized
}
