package workbench

import (
	"errors"
	"fmt"
	"log"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// MenuBar is the File/Edit/View menu the macOS application menu bar shows. Its
// entries are the shortcuts the user kept, so a remapped key shows the key the
// application listens for; a session-less window disables the entries that need
// one.
func (state *State) MenuBar() []app.Menu {
	hasSession := state.menuBarHasSession()
	item := func(action shortcutAction, disabled bool) app.MenuBarItem {
		return app.MenuBarItem{
			Title:    state.shortcutActionLabel(action),
			Shortcut: state.shortcuts.chords.Chord(action),
			Disabled: disabled || state.appAccessBlocked(),
			OnSelect: func() gfx.EventResult { return state.runMenuAction(action) },
		}
	}
	return []app.Menu{
		{Title: state.messages.MenuFile, Items: []app.MenuBarItem{
			item(shortcutNewWindow, false),
			item(shortcutNewTab, false),
			item(shortcutLocalTerminal, false),
			{Separator: true},
			item(shortcutCloseTab, !hasSession),
			{Separator: true},
			item(shortcutSettings, false),
		}},
		{Title: state.messages.MenuEdit, Items: []app.MenuBarItem{
			item(shortcutCopy, !hasSession),
			item(shortcutPaste, !hasSession),
			{Separator: true},
			item(shortcutFind, !hasSession),
		}},
		{Title: state.messages.MenuView, Items: []app.MenuBarItem{
			item(shortcutCommandPalette, false),
			item(shortcutAssistant, false),
			item(shortcutHostsPanel, false),
			item(shortcutSFTP, !hasSession),
			{Separator: true},
			item(shortcutZoomIn, false),
			item(shortcutZoomOut, false),
			item(shortcutZoomReset, false),
		}},
	}
}

// SyncMenuBar builds the menu bar now, if the window has one and the state it
// shows changed. The application calls it once at startup; Wake calls it after.
func (state *State) SyncMenuBar() { state.syncMenuBar() }

func (state *State) syncMenuBar() {
	if state.setMenuBar == nil {
		return
	}
	signature := fmt.Sprintf("%t|%t|%d|%s|%d", state.appAccessBlocked(), state.menuBarHasSession(), state.activeSessionID, state.messages.MenuFile, state.shortcuts.revision)
	if signature == state.menuBarSig {
		return
	}
	state.menuBarSig = signature
	if err := state.setMenuBar(state.MenuBar()); err != nil && !errors.Is(err, app.ErrUnsupported) {
		log.Print("motty: application menu bar: ", err)
	}
}

func (state *State) menuBarHasSession() bool {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return ok && session.term != nil
}

// runMenuAction runs one of the window's own shortcut actions from a menu
// choice, on the event-loop thread, and returns what it did. A native menu runs
// the entry outside the window's event handlers; the application carries the
// result out on the window, so a command chosen from the menu moves the
// keyboard exactly as its shortcut does.
func (state *State) runMenuAction(action shortcutAction) gfx.EventResult {
	if state.closed || state.callbacksClosed.Load() || state.appAccessBlocked() {
		return gfx.EventResult{}
	}
	result := state.executeShortcut(action)
	// The menu writes the clipboard itself so a failure reaches the user as a
	// notice; the field is cleared so the runtime does not write it again.
	if result.ClipboardText != "" && state.options.SetClipboardText != nil {
		if err := state.options.SetClipboardText(result.ClipboardText); err != nil {
			// The main window does not paint state.status, so the failure has to
			// reach the user through the action notice, which asks for its own
			// repaint.
			state.showActionFailure(state.messages.MenuCopy, err, state.messages.ActionFeedback.ClipboardFailed)
		}
		result.ClipboardText = ""
	}
	return result
}
