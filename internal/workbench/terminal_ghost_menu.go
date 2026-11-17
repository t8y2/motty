package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

// The menu of the accept-suggestion badge: the three keys Warp offers and a way
// into the keybinding dialog for anything else. It hangs over the grid, so it
// is painted at the window level like the terminal's right-click menu, with the
// same backdrop and keyboard routes.
var ghostMenuHits = tree.Feature("ghost-menu")

var (
	hitGhostMenuDismiss    = tree.ID(ghostMenuHits, "ghost-menu-dismiss")
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
	if !state.ghostMenu.open || state.activeSection != sectionTerminal || state.modal != modalNone {
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

// ghostMenuPointer closes the open menu on a press of any non-primary button
// outside it; the backdrop swallows the press, and only a primary one is also a
// click.
func (state *State) ghostMenuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if !state.ghostMenu.open || event.Target != hitGhostMenuDismiss || event.Kind != gfx.PointerDown || event.Button == gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	state.ghostMenu.open = false
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// clickGhostMenu runs the menu's choice. A press anywhere else only closes it.
func (state *State) clickGhostMenu(id gfx.ID) bool {
	if !state.ghostMenu.open {
		return false
	}
	_, live := state.ghostMenuSession()
	state.ghostMenu.open = false
	if !live {
		return true
	}
	for index, chord := range ghostMenuPresets {
		if id == ghostMenuPresetHits[index] {
			state.bindAcceptSuggestion(chord)
			return true
		}
	}
	if id == hitGhostMenuCustom {
		state.openShortcutCapture(shortcutAcceptSuggestion)
		return true
	}
	if id == hitGhostMenuTurnOff {
		// Re-enabling is deliberate: it lives in Settings, not in a menu the
		// absent badge can no longer show.
		state.history.acceptHintOff = true
		state.save()
	}
	return true
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

// paintGhostMenu paints the menu above the badge, or below it when the row is
// too close to the top. The anchor is the badge's rectangle from the frame the
// menu opened in; the terminal stops painting the suggestion once the menu has
// the keyboard, so it is not read back from later frames.
func (state *State) paintGhostMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
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
	var presets [3]components.ContextMenuItem
	var scratch [5]string
	for index, preset := range ghostMenuPresets {
		label := strings.Join(preset.Keycaps(scratch[:0]), "")
		disabled := false
		if owner := state.shortcuts.boundAction(preset); owner != shortcutNone && owner != shortcutAcceptSuggestion {
			disabled = true
		}
		if state.snippetNamed(preset) != "" {
			disabled = true
		}
		presets[index] = components.ContextMenuItem{
			ID: ghostMenuPresetHits[index], Label: label, Value: preset.Label(), Hint: preset.Label(),
			Kind: components.ContextMenuRadio, Selected: current == preset, Disabled: disabled,
		}
	}
	custom := components.ContextMenuItem{ID: hitGhostMenuCustom, Label: m.AcceptHintCustom}
	if current.Valid() && !acceptSuggestionPreset(current) {
		// A custom key is named on the row that goes and changes it.
		custom.Keys = current.Keycaps(nil)
	}
	turnOff := components.ContextMenuItem{ID: hitGhostMenuTurnOff, Label: m.AcceptHintTurnOff}
	var rows menuRows
	for _, item := range presets {
		rows.add(item)
	}
	rows.separate()
	rows.add(custom)
	rows.separate()
	rows.add(turnOff)

	width, height := rows.size(canvas, state.contextMenuTheme())
	// BeginContextMenu never paints narrower than 144; place the menu with the
	// width it will actually have.
	width = max(width, 144)
	gap := float32(4)
	x := min(max(pill.X, viewport.X+6), max(viewport.X+viewport.Width-width-6, viewport.X+6))
	y := pill.Y - height - gap
	if y < viewport.Y+6 {
		y = pill.Y + pill.Height + gap
	}
	y = min(max(y, viewport.Y+6), max(viewport.Y+viewport.Height-height-6, viewport.Y+6))
	bounds := gfx.Rect{X: x, Y: y, Width: width, Height: height}
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitGhostMenuDismiss, Viewport: viewport,
		Scroll: state.menuScrollFor(hitGhostMenuDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	selection := state.Theme().Selection
	selection.Alpha = 0.30
	if state.pal.dark {
		selection.Alpha = 1
	}
	for index, item := range presets {
		if current == ghostMenuPresets[index] {
			// The current key keeps the menu's selected-row fill, as the reference
			// does; the radio check remains for assistive technology.
			if row := menu.NextItemBounds(); row.Width > 0 {
				// The menu's rows are on its dialog layer; the fill has to share
				// it to sit above the surface and under the row's own content.
				canvas.PushLayer(dialogLayer)
				canvas.Rounded(row, min(contextMenuRow/2, float32(5)), selection)
				canvas.PopLayer()
			}
		}
		menu.AddItem(item)
	}
	menu.AddSeparator()
	menu.AddItem(custom)
	menu.AddSeparator()
	menu.AddItem(turnOff)
	menu.End()
}
