package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
)

const desktopFullscreenChord = "Ctrl+Alt+Enter"

// desktopFullscreenBar is the floating bar a fullscreen remote desktop shows
// over the top of its picture. Its pin keeps the bar visible; unpinned, the bar
// hides and comes back while the pointer reaches the top edge, as the platform
// remote desktop client's connection bar does. zone is the band in page
// coordinates that reveals it, laid out by the last paint; over is whether the
// pointer is in that band.
type desktopFullscreenBar struct {
	autoHide bool
	over     bool
	zone     gfx.Rect
}

// fullscreenBarRevealSlack is how far below the bar a pointer still counts as
// reaching for it, so the bar does not flicker while it is being aimed at.
const fullscreenBarRevealSlack = float32(6)

// SetFullscreenControl connects the desktop action to Viva's native window.
func (state *State) SetFullscreenControl(set func(bool) error) {
	state.winChrome.setFullscreen = set
}

// SetWindowFullscreen follows native changes, including the system's exit action.
func (state *State) SetWindowFullscreen(fullscreen bool) {
	state.winChrome.fullscreen = fullscreen
	if session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession); ok && session.desk != nil {
		session.desk.focusPending = true
	}
	if !fullscreen {
		state.desktopBar.over = false
		for i := range state.sessions {
			if view := state.sessions[i].desk; view != nil {
				view.fullscreen = false
			}
		}
		state.winChrome.fullscreenSession = 0
	} else if session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession); ok && session.desk != nil {
		session.desk.fullscreen = true
	}
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

func (state *State) fullscreenDesktop() *Session {
	if !state.winChrome.fullscreen || state.modal != modalNone || state.activeSection != sectionTerminal {
		return nil
	}
	session, ok := state.sessionByIDPointer(state.winChrome.fullscreenSession)
	if !ok || session.ID != state.activeSessionID || session.desk == nil || session.desk.ended {
		return nil
	}
	return session
}

func (state *State) toggleDesktopFullscreen(session *Session) {
	chrome := &state.winChrome
	if chrome.setFullscreen == nil {
		state.showActionError(state.messages.Desktop.Fullscreen, state.messages.Desktop.FullscreenUnavailable)
		return
	}
	enter := !chrome.fullscreen && chrome.fullscreenSession == 0
	if enter {
		if session == nil || session.desk == nil || session.desk.ended {
			return
		}
		chrome.fullscreenSession = session.ID
		// An unpinned bar starts hidden: the pointer is on the control that
		// entered fullscreen, not at the top edge the bar answers to.
		state.desktopBar.over = false
	}
	if selected, ok := state.sessionByIDPointer(chrome.fullscreenSession); ok && selected.desk != nil {
		selected.desk.releaseInput()
	}
	if err := chrome.setFullscreen(enter); err != nil {
		if enter {
			chrome.fullscreenSession = 0
		}
		state.showActionFailure(state.messages.Desktop.Fullscreen, err, fmt.Sprintf(state.messages.Desktop.FullscreenFailedFmt, err))
	}
}

func (state *State) desktopFullscreenKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Key != gfx.KeyEnter || event.Modifiers != gfx.ModifierControl|gfx.ModifierAlt {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if state.winChrome.fullscreenSession == 0 &&
		(!ok || session.desk == nil || state.activeSection != sectionTerminal || state.modal != modalNone) {
		return gfx.EventResult{}, false
	}
	if !event.Release && !event.Repeat {
		state.toggleDesktopFullscreen(session)
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) syncDesktopFullscreen() {
	if state.winChrome.fullscreen && state.winChrome.fullscreenSession != 0 && state.fullscreenDesktop() == nil {
		state.toggleDesktopFullscreen(nil)
	}
}

// setDesktopFullscreenBarAutoHide turns the fullscreen bar's pin off or on: off
// keeps the bar always visible, on hides it until the pointer reaches the top
// edge. The choice is this computer's and is written to the state file.
func (state *State) setDesktopFullscreenBarAutoHide(autoHide bool) bool {
	bar := &state.desktopBar
	if bar.autoHide == autoHide {
		return false
	}
	bar.autoHide = autoHide
	if autoHide {
		// The pin was just pressed, so the pointer is on the bar: keep it out
		// until the pointer leaves the reveal band.
		bar.over = true
	}
	state.save()
	return true
}

// trackDesktopFullscreenBar follows the pointer for an unpinned fullscreen bar.
// The band is the one the last paint laid out: moving into the top edge shows
// the bar and leaving it hides the bar again. A drag that captured the pointer
// keeps the bar as it was, so panning the picture cannot make it appear.
func (state *State) trackDesktopFullscreenBar(event gfx.PointerEvent) bool {
	bar := &state.desktopBar
	if !bar.autoHide || event.Captured || state.fullscreenDesktop() == nil {
		return false
	}
	over := bar.over
	switch event.Kind {
	case gfx.PointerDown, gfx.PointerMove:
		over = pointInRect(event.X, event.Y, bar.zone)
	case gfx.PointerExit:
		over = false
	}
	changed := over != bar.over
	bar.over = over
	return changed
}
