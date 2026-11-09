package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
)

const desktopFullscreenChord = "Ctrl+Alt+Enter"

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
