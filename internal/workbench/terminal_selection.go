package workbench

import (
	"math"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

type terminalSelection struct {
	anchor, focus term.Position
	dragging      bool
	selected      bool
}

func (selection *terminalSelection) clear() bool {
	changed := selection.dragging || selection.selected
	*selection = terminalSelection{}
	return changed
}

func (selection *terminalSelection) begin(position term.Position) bool {
	changed := selection.clear()
	selection.anchor = position
	selection.focus = position
	selection.dragging = true
	return changed
}

func (selection *terminalSelection) extend(position term.Position) bool {
	if !selection.dragging || selection.focus == position {
		return false
	}
	selection.focus = position
	selection.selected = selection.anchor != selection.focus
	return true
}

func (selection *terminalSelection) selectRange(anchor, focus term.Position) {
	selection.anchor = anchor
	selection.focus = focus
	selection.dragging = false
	selection.selected = true
}

func (selection terminalSelection) span(row int, line term.Row, cols int) (int, int, bool) {
	if !selection.selected {
		return 0, 0, false
	}
	start, end := selection.anchor, selection.focus
	if end.Row < start.Row || end.Row == start.Row && end.Column < start.Column {
		start, end = end, start
	}
	if row < start.Row || row > end.Row {
		return 0, 0, false
	}
	from, to := 0, cols
	if row == start.Row {
		from = start.Column
	}
	if row == end.Row {
		to = end.Column + 1
	}
	if from < len(line.Cells) && line.Cells[from].Attr&term.WideTail != 0 && from > 0 {
		from--
	}
	if to > 0 && to <= len(line.Cells) && line.Cells[to-1].Attr&term.WideHead != 0 {
		to++
	}
	return max(from, 0), min(to, cols), from < to
}

func (state *State) terminalPointer(session *Session, event ui.PointerEvent) ui.EventResult {
	terminal := session.term
	selection := &terminal.selection
	position := state.terminalPositionAt(event.Viewport, event.X, event.Y)
	if result, handled := state.terminalMousePointer(session, event, position); handled {
		result.Repaint = terminal.links.clear() || result.Repaint
		return result
	}
	row := terminal.screen.Row(position.Row)
	if position.Column < len(row.Cells) && row.Cells[position.Column].Attr&term.WideTail != 0 && position.Column > 0 {
		position.Column--
	}
	linkPosition := terminalLinkPosition(terminal.screen, position)
	linksEnabled := terminal.screen.Modes().Mouse == term.MouseOff && event.Modifiers&ui.ModifierShift == 0
	switch event.Kind {
	case ui.PointerDown:
		if event.Button == ui.PointerButtonSecondary {
			// A program that asked for the mouse already had this press; what
			// reaches here is the terminal's own, so it gets the menu.
			state.openTerminalMenu(session)
			state.offerActionsAt(session, position)
			return ui.EventResult{Handled: true, Repaint: true}
		}
		if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
			return ui.EventResult{}
		}
		state.terminalMenu.open = false
		if event.ClickCount >= 2 {
			terminal.links.clear()
			start, end := terminal.screen.WordBounds(position)
			selection.selectRange(start, end)
			return ui.EventResult{Handled: true, Repaint: true}
		}
		linkChanged := terminal.links.clear()
		if linksEnabled && event.Button == ui.PointerButtonPrimary {
			linkChanged = terminal.links.press(terminal.screen, linkPosition) || linkChanged
		}
		return ui.EventResult{Handled: true, Repaint: selection.begin(position) || linkChanged}
	case ui.PointerMove:
		if !selection.dragging || !event.Captured {
			if !linksEnabled {
				return ui.EventResult{Repaint: terminal.links.clear()}
			}
			changed := terminal.links.hover(terminal.screen, linkPosition)
			_, hovered := terminal.links.hoveredLink(terminal.screen)
			return ui.EventResult{Handled: hovered, Repaint: changed}
		}
		changed := selection.extend(position)
		if changed {
			changed = terminal.links.clear() || changed
		}
		return ui.EventResult{Handled: true, Repaint: changed}
	case ui.PointerUp, ui.PointerExit:
		if !selection.dragging {
			if event.Kind == ui.PointerUp && linksEnabled && event.Button == ui.PointerButtonPrimary {
				target, open, linkChanged := terminal.links.release(terminal.screen, linkPosition)
				if open && terminal.openLink != nil {
					terminal.openLink(target)
				}
				return ui.EventResult{Handled: event.Captured || open, Repaint: linkChanged}
			}
			changed := terminal.links.clear()
			return ui.EventResult{Handled: event.Captured, Repaint: changed}
		}
		changed := selection.extend(position)
		selection.dragging = false
		open := false
		target := ""
		if !selection.selected {
			changed = selection.clear() || changed
			if event.Kind == ui.PointerUp && linksEnabled && event.Button == ui.PointerButtonPrimary {
				var linkChanged bool
				target, open, linkChanged = terminal.links.release(terminal.screen, linkPosition)
				changed = changed || linkChanged
			}
		} else {
			changed = terminal.links.clear() || changed
		}
		if event.Kind == ui.PointerExit {
			changed = terminal.links.clear() || changed
			open = false
		}
		if open && terminal.openLink != nil {
			terminal.openLink(target)
		}
		return ui.EventResult{Handled: true, Repaint: changed}
	}
	return ui.EventResult{}
}

func (state *State) terminalMousePointer(session *Session, event ui.PointerEvent, position term.Position) (ui.EventResult, bool) {
	terminal := session.term
	if terminal.selection.dragging {
		return ui.EventResult{}, false
	}
	modes := terminal.screen.Modes()
	switch event.Kind {
	case ui.PointerDown:
		if modes.Mouse == term.MouseOff || event.Modifiers&ui.ModifierShift != 0 {
			return ui.EventResult{}, false
		}
		button, ok := terminalMouseButton(event.Button)
		if !ok {
			return ui.EventResult{}, false
		}
		terminal.mouseButton = button
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MousePress, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return ui.EventResult{Handled: true, Repaint: repaint}, true
	case ui.PointerMove:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			if modes.Mouse != term.MouseAny || event.Modifiers&ui.ModifierShift != 0 {
				return ui.EventResult{}, false
			}
		}
		if terminal.mousePositionValid && terminal.mousePosition == position {
			return ui.EventResult{Handled: true}, true
		}
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseMotion, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return ui.EventResult{Handled: true, Repaint: repaint}, true
	case ui.PointerUp:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			return ui.EventResult{}, false
		}
		terminal.mouseButton = term.MouseButtonNone
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseRelease, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return ui.EventResult{Handled: true, Repaint: repaint}, true
	case ui.PointerExit:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			return ui.EventResult{}, false
		}
		terminal.mouseButton = term.MouseButtonNone
		if terminal.mousePositionValid {
			position = terminal.mousePosition
		}
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseRelease, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return ui.EventResult{Handled: true, Repaint: repaint}, true
	}
	return ui.EventResult{}, false
}

func (terminal *terminal) sendMouse(event term.MouseEvent) bool {
	report, ok := term.AppendMouse(terminal.mouseReport[:0], terminal.screen.Modes(), event)
	if !ok {
		return false
	}
	repaint := terminal.selection.selected || terminal.screen.ViewOffset() > 0
	terminal.send(report)
	return repaint
}

func terminalMouseButton(button ui.PointerButton) (term.MouseEventButton, bool) {
	switch button {
	case ui.PointerButtonPrimary:
		return term.MouseButtonLeft, true
	case ui.PointerButtonMiddle:
		return term.MouseButtonMiddle, true
	case ui.PointerButtonSecondary:
		return term.MouseButtonRight, true
	default:
		return term.MouseButtonNone, false
	}
}

func terminalMouseModifiers(modifiers ui.Modifiers) term.MouseModifiers {
	var result term.MouseModifiers
	if modifiers&ui.ModifierShift != 0 {
		result |= term.MouseModifierShift
	}
	if modifiers&ui.ModifierMeta != 0 {
		result |= term.MouseModifierMeta
	}
	if modifiers&ui.ModifierControl != 0 {
		result |= term.MouseModifierControl
	}
	return result
}

func (state *State) terminalPositionAt(viewport ui.Size, x, y float32) term.Position {
	geometry := terminalLayout(viewport, state.cellWidth, state.cellHeight, state.padding())
	column, row := 0, 0
	if state.cellWidth > 0 {
		column = int(math.Floor(float64((x - geometry.left) / state.cellWidth)))
	}
	if state.cellHeight > 0 {
		row = int(math.Floor(float64((y - geometry.top) / state.cellHeight)))
	}
	return term.Position{Row: min(max(row, 0), geometry.rows-1), Column: min(max(column, 0), geometry.cols-1)}
}
