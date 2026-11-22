package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

// The menu of the accept-suggestion badge: the three keys Warp offers and a way
// into the keybinding dialog for anything else. It hangs over the grid, so it
// is painted at the window level like the terminal's right-click menu, with the
// same backdrop and keyboard routes.
var ghostMenuHits = tree.Feature("ghost-menu")

var (
	hitGhostMenuRight      = tree.ID(ghostMenuHits, "ghost-menu-right")
	hitGhostMenuTab        = tree.ID(ghostMenuHits, "ghost-menu-tab")
	hitGhostMenuShiftRight = tree.ID(ghostMenuHits, "ghost-menu-shift-right")
	hitGhostMenuCustom     = tree.ID(ghostMenuHits, "ghost-menu-custom")
	hitGhostMenuTurnOff    = tree.ID(ghostMenuHits, "ghost-menu-turn-off")
)

// ghostMenu is the badge's open menu: which session it belongs to and where
// the badge was when it opened.
type ghostMenu struct {
	open    bool
	session uint32
	// anchor is the badge's rectangle when the menu opened. It is kept rather
	// than read back from the frame: the menu takes the keyboard, the terminal
	// stops painting suggestions, and the menu still has to stay where the
	// badge was.
	anchor gfx.Rect
}

// ghostMenuPresets are the keys the menu offers, in the order it shows them.
var ghostMenuPresets = [3]keymap.Chord{
	{Key: gfx.KeyRight},
	{Key: gfx.KeyTab},
	{Modifiers: gfx.ModifierShift, Key: gfx.KeyRight},
}

var ghostMenuPresetHits = [3]gfx.ID{hitGhostMenuRight, hitGhostMenuTab, hitGhostMenuShiftRight}

// ghostMenuSession is the session the open menu acts on: still there, still the
// active terminal, with no dialog on top.
func (state *State) ghostMenuSession() (*Session, bool) {
	if !state.ghostMenu.open {
		return nil, false
	}
	return state.ghostMenuTarget()
}

// ghostMenuTarget is the session the menu was opened on, while it is still the active
// terminal: a choice is run once the menu has closed.
func (state *State) ghostMenuTarget() (*Session, bool) {
	if state.activeSection != sectionTerminal || state.modal != modalNone {
		return nil, false
	}
	session, ok := state.sessionByIDPointer(state.ghostMenu.session)
	if !ok || session.term == nil || session.ID != state.activeSessionID {
		return nil, false
	}
	return session, true
}

// openGhostMenu shows the badge's menu. The menu and the terminal's right-click
// menu are peers: only one can be open.
func (state *State) openGhostMenu(session *Session) {
	if session == nil || session.term == nil {
		return
	}
	if state.ghostMenu.open && state.ghostMenu.session == session.ID {
		return
	}
	state.terminalMenu.open = false
	state.ghostMenu = ghostMenu{open: true, session: session.ID, anchor: session.term.hintRect}
}

// chooseGhostMenu runs a choice of the menu, which closed with it.
func (state *State) chooseGhostMenu(id gfx.ID) {
	if _, live := state.ghostMenuTarget(); !live {
		return
	}
	for index, chord := range ghostMenuPresets {
		if id == ghostMenuPresetHits[index] {
			state.bindAcceptSuggestion(chord)
			return
		}
	}
	if id == hitGhostMenuCustom {
		state.openShortcutCapture(shortcutAcceptSuggestion)
		return
	}
	if id == hitGhostMenuTurnOff {
		// Re-enabling is deliberate: it lives in Settings, not in a menu the
		// absent badge can no longer show.
		state.history.acceptHintOff = true
		state.save()
	}
}

// bindAcceptSuggestion gives the accept action a chord and saves it, saying why
// when another action or a snippet already holds it.
func (state *State) bindAcceptSuggestion(chord keymap.Chord) bool {
	if owner := state.snippetNamed(chord); owner != "" {
		state.status = fmt.Sprintf(state.messages.ShortcutsConflictFmt, owner)
		return false
	}
	conflict, bound := state.shortcuts.bind(shortcutAcceptSuggestion, chord)
	if !bound {
		state.status = fmt.Sprintf(state.messages.ShortcutsConflictFmt, state.shortcutActionLabel(conflict))
		return false
	}
	state.save()
	return true
}

// acceptSuggestionPreset reports whether a chord is one of the menu's keys.
func acceptSuggestionPreset(chord keymap.Chord) bool {
	for _, preset := range ghostMenuPresets {
		if chord == preset {
			return true
		}
	}
	return false
}

// declareGhostMenu declares the menu above the badge, or below it when the row is
// too close to the top. The anchor is the badge's rectangle from the frame the
// menu opened in; the terminal stops painting the suggestion once the menu has
// the keyboard, so it is not read back from later frames.
func (state *State) declareGhostMenu(c ui.C) {
	if !state.ghostMenu.open {
		return
	}
	if _, ok := state.ghostMenuSession(); !ok {
		state.ghostMenu.open = false
		return
	}
	pill := state.ghostMenu.anchor
	if pill.Width <= 0 || pill.Height <= 0 {
		state.ghostMenu.open = false
		return
	}
	m := state.messages.Completion
	current := state.shortcuts.chords.Chord(shortcutAcceptSuggestion)
	var rows menuRows
	var scratch [5]string
	for index, preset := range ghostMenuPresets {
		disabled := false
		if owner := state.shortcuts.boundAction(preset); owner != shortcutNone && owner != shortcutAcceptSuggestion {
			disabled = true
		}
		if state.snippetNamed(preset) != "" {
			disabled = true
		}
		rows.add(components.ContextMenuItem{
			ID: ghostMenuPresetHits[index], Label: strings.Join(preset.Keycaps(scratch[:0]), ""),
			Kind: components.ContextMenuRadio, Selected: current == preset, Disabled: disabled,
		})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitGhostMenuCustom, Label: m.AcceptHintCustom})
	if current.Valid() && !acceptSuggestionPreset(current) {
		// A custom key is named on the row that goes and changes it.
		rows.chord(current)
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitGhostMenuTurnOff, Label: m.AcceptHintTurnOff})
	state.declareAnchoredMenu(c, &state.ghostMenu.open, pill, &rows, state.chooseGhostMenu)
}
