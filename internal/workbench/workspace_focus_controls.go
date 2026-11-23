package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func (state *State) declarePaneMaximizeButton(c ui.C, id layoutcore.PaneID) {
	workspace := state.deck.ActiveWorkspace()
	if layout, ok := state.deck.Layout(workspace); !ok || layout.Len() < 2 {
		return
	}
	labels := &state.paneMaximizeHints
	chord := state.shortcuts.labels[shortcutPaneMaximize]
	if labels[0] != state.messages.PaneMaximize || labels[1] != state.messages.PaneRestore || labels[2] != chord {
		labels[0], labels[1], labels[2] = state.messages.PaneMaximize, state.messages.PaneRestore, chord
		labels[3], labels[4] = labels[0], labels[1]
		if state.shortcuts.chords.Chord(shortcutPaneMaximize).Valid() {
			labels[3] += " (" + chord + ")"
			labels[4] += " (" + chord + ")"
		}
	}
	mark, hint := uint8(iconPaneMaximize), labels[3]
	if state.deck.PaneMaximized(workspace) {
		mark, hint = iconPaneRestore, labels[4]
	}
	state.declareIconButtonWithOptions(c, hitPaneMaximizeList.At(int(id)), mark, hint, false, iconButtonQuiet, 14, gfx.HitOptions{}).
		Size(barToolSize, barToolSize).Shrink(0).MarginSides(0, 0, 0, 6)
}

func (state *State) declareSessionMaximizeButton(c ui.C, session *Session) {
	_, pane, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if ok {
		state.declarePaneMaximizeButton(c, pane)
	}
}

func (state *State) workspaceFocusTabsKey(event gfx.KeyEvent) gfx.EventResult {
	if !event.Release && event.Key == gfx.KeyEscape && state.focusTabs.drag.pane != 0 {
		state.focusTabs.drag = workspaceFocusDrag{}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if state.modal != modalNone || !state.terminalPage() || !state.deck.PaneMaximized(state.deck.ActiveWorkspace()) || event.Target.Scope() != hitWorkspaceFocusTab || event.Release || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace {
		state.selectWorkspaceFocusTab(layoutcore.PaneID(item(event.Target)), true)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activePaneContentFocus()}
	}
	delta := 0
	switch event.Key {
	case gfx.KeyLeft:
		delta = -1
	case gfx.KeyRight:
		delta = 1
	default:
		return gfx.EventResult{}
	}
	panes := state.deck.Panes(state.deck.ActiveWorkspace(), nil)
	for index, pane := range panes {
		if pane.ID != layoutcore.PaneID(item(event.Target)) {
			continue
		}
		target := panes[(index+delta+len(panes))%len(panes)].ID
		state.selectWorkspaceFocusTab(target, false)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitWorkspaceFocusTab.At(int(target))}
	}
	return gfx.EventResult{}
}
