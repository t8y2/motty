package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// terminalToolbarMenu is the right-click menu for empty space on a terminal's
// side rail. It changes global terminal-toolbar preferences, so it does not
// belong to one session.
type terminalToolbarMenu struct {
	open bool
	at   ui.Point
}

// terminalToolbarMenuPointer opens the menu only from rail background. A press
// on a control remains the control's own event, and a press outside an open menu
// retains the same modal dismissal behavior as the other context menus.
func (state *State) terminalToolbarMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	menu := &state.terminalToolbarMenu
	if menu.open {
		if event.Kind == ui.PointerDown && event.Target == hitTerminalToolbarMenuDismiss &&
			event.Button != ui.PointerButtonPrimary {
			menu.open = false
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return ui.EventResult{}, false
	}
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonSecondary ||
		state.modal != modalNone || state.terminalMenu.open || state.openMenuScroll() != nil {
		return ui.EventResult{}, false
	}
	session, ok := state.terminalSideRailSession()
	if !ok || !terminalRailBlankAt(session, event.X, event.Y) {
		return ui.EventResult{}, false
	}
	*menu = terminalToolbarMenu{open: true, at: ui.Point{X: event.X, Y: event.Y}}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// terminalRailBlankAt reports whether a point is on a painted rail but outside
// every rail control, including controls with custom status-chip rendering.
func terminalRailBlankAt(session *Session, x, y float32) bool {
	if session == nil || session.term == nil {
		return false
	}
	for side := range session.term.rails {
		rail := &session.term.rails[side]
		if rail.bounds.Width <= 0 || rail.bounds.Height <= 0 || !pointInRect(x, y, rail.bounds) {
			continue
		}
		for index := 0; index < rail.count; index++ {
			if pointInRect(x, y, rail.rects[index]) {
				return false
			}
		}
		return true
	}
	return false
}

func (state *State) terminalToolbarMenuAvailable() bool {
	if !state.terminalToolbarMenu.open {
		return false
	}
	if state.activeSection != sectionTerminal || state.modal != modalNone {
		state.terminalToolbarMenu.open = false
		return false
	}
	if _, ok := state.terminalSideRailSession(); !ok {
		state.terminalToolbarMenu.open = false
		return false
	}
	return true
}

func (state *State) paintTerminalToolbarMenu(canvas *ui.Canvas, viewport ui.Rect) {
	if !state.terminalToolbarMenuAvailable() {
		return
	}
	m := state.messages
	var rows menuRows
	rows.add(components.ContextMenuItem{
		ID: hitTerminalToolbarMenuSmall, Label: m.SettingsToolbarSizeSmall,
		Kind: components.ContextMenuRadio, Selected: state.terminalToolbarSize == terminalToolbarSmall,
		Value: m.SettingsToolbarSizeSmall,
	})
	rows.add(components.ContextMenuItem{
		ID: hitTerminalToolbarMenuMedium, Label: m.SettingsToolbarSizeMedium,
		Kind: components.ContextMenuRadio, Selected: state.terminalToolbarSize == terminalToolbarMedium,
		Value: m.SettingsToolbarSizeMedium,
	})
	rows.add(components.ContextMenuItem{
		ID: hitTerminalToolbarMenuLarge, Label: m.SettingsToolbarSizeLarge,
		Kind: components.ContextMenuRadio, Selected: state.terminalToolbarSize == terminalToolbarLarge,
		Value: m.SettingsToolbarSizeLarge,
	})
	rows.separate()
	rows.add(components.ContextMenuItem{
		ID: hitTerminalToolbarMenuReset, Label: m.Toolbars.Reset,
		Icon: lucideIcons[iconRefresh], Value: m.Toolbars.Reset,
	})

	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, state.terminalToolbarMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitTerminalToolbarMenuDismiss,
		Viewport: viewport, Scroll: state.menuScrollFor(hitTerminalToolbarMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

// clickTerminalToolbarMenu applies a size or restores the complete terminal
// toolbar configuration. Restoring defaults includes the small size, side and
// order assignments, visibility, connection color and latency.
func (state *State) clickTerminalToolbarMenu(id ui.ID) bool {
	if !state.terminalToolbarMenu.open {
		return false
	}
	state.terminalToolbarMenu.open = false
	changed, layoutChanged := false, false
	switch id {
	case hitTerminalToolbarMenuSmall:
		changed = state.setTerminalToolbarSize(terminalToolbarSmall)
		layoutChanged = changed
	case hitTerminalToolbarMenuMedium:
		changed = state.setTerminalToolbarSize(terminalToolbarMedium)
		layoutChanged = changed
	case hitTerminalToolbarMenuLarge:
		changed = state.setTerminalToolbarSize(terminalToolbarLarge)
		layoutChanged = changed
	case hitTerminalToolbarMenuReset:
		state.terminalToolbarSize = terminalToolbarSmall
		state.toolbars.layouts[toolbarTerminal] = nil
		state.syncTerminalStatusEntries()
		state.applyTerminalToolbarLayoutChange()
		state.save()
		return true
	default:
		return true
	}
	if !changed {
		return true
	}
	if layoutChanged {
		state.applyTerminalToolbarLayoutChange()
	}
	state.save()
	return true
}
