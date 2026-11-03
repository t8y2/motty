package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

// terminalSelection is anchored to buffer lines, not view rows, so it stays on
// the same text while the view scrolls under a drag.
type terminalSelection struct {
	anchor, focus term.Point
	dragging      bool
	selected      bool
	edge          selectionEdge
}

// selectionEdge is the auto-scroll a drag asks for while the pointer is held
// beyond the top or bottom of the grid.
type selectionEdge struct {
	direction int // +1 scrolls back into history, -1 toward the live screen, 0 rests
	lines     int // lines per step, growing with the distance from the grid
	column    int // the column the focus keeps while the view moves under it
	last      time.Duration
	stepped   bool
}

const (
	selectionScrollInterval = 50 * time.Millisecond
	selectionScrollMaxLines = 8
)

func (selection *terminalSelection) clear() bool {
	changed := selection.dragging || selection.selected
	*selection = terminalSelection{}
	return changed
}

func (selection *terminalSelection) begin(point term.Point) bool {
	changed := selection.clear()
	selection.anchor = point
	selection.focus = point
	selection.dragging = true
	return changed
}

func (selection *terminalSelection) extend(point term.Point) bool {
	if !selection.dragging || selection.focus == point {
		return false
	}
	selection.focus = point
	selection.selected = selection.anchor != selection.focus
	return true
}

func (selection *terminalSelection) selectRange(anchor, focus term.Point) {
	selection.anchor = anchor
	selection.focus = focus
	selection.dragging = false
	selection.selected = true
	selection.edge = selectionEdge{}
}

// text is the selected text, wherever in the retained buffer it lies.
func (selection terminalSelection) text(screen *term.Terminal) string {
	return screen.SelectedRange(selection.anchor, selection.focus)
}

// span is the part of one displayed row the selection covers; lineNumber is the
// buffer line that row shows.
func (selection terminalSelection) span(lineNumber uint64, line term.Row, cols int) (int, int, bool) {
	if !selection.selected {
		return 0, 0, false
	}
	start, end := selection.anchor, selection.focus
	if end.Line < start.Line || end.Line == start.Line && end.Column < start.Column {
		start, end = end, start
	}
	if lineNumber < start.Line || lineNumber > end.Line {
		return 0, 0, false
	}
	from, to := 0, cols
	if lineNumber == start.Line {
		from = start.Column
	}
	if lineNumber == end.Line {
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
	position := state.terminalPositionAt(session, event.Viewport, event.X, event.Y)
	if result, handled := state.terminalMousePointer(session, event, position); handled {
		result.Repaint = terminal.links.clear() || result.Repaint
		return result
	}
	row := terminal.screen.Row(position.Row)
	if position.Column < len(row.Cells) && row.Cells[position.Column].Attr&term.WideTail != 0 && position.Column > 0 {
		position.Column--
	}
	linkPosition := terminalLinkPosition(terminal.screen, position)
	linksEnabled := terminal.screen.Modes().Mouse == term.MouseOff && state.terminalLinkOpens(event.Modifiers)
	switch event.Kind {
	case ui.PointerDown:
		if event.Button == ui.PointerButtonSecondary {
			// A program that asked for the mouse already had this press; what
			// reaches here is the terminal's own. It pastes when the user chose
			// that, and Shift keeps the menu one press away.
			if state.terminalRightClickPaste && event.Modifiers&ui.ModifierShift == 0 {
				state.terminalMenu.open = false
				state.pasteIntoTerminal(session)
				return ui.EventResult{Handled: true, Repaint: true}
			}
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
			selection.selectRange(terminal.screen.PointAt(start), terminal.screen.PointAt(end))
			result := ui.EventResult{Handled: true, Repaint: true}
			if state.terminalCopyOnSelect {
				result.ClipboardText = selection.text(terminal.screen)
			}
			return result
		}
		linkChanged := terminal.links.clear()
		if linksEnabled && event.Button == ui.PointerButtonPrimary {
			linkChanged = terminal.links.press(terminal.screen, linkPosition) || linkChanged
		}
		return ui.EventResult{Handled: true, Repaint: selection.begin(terminal.screen.PointAt(position)) || linkChanged}
	case ui.PointerMove:
		if !selection.dragging || !event.Captured {
			if !linksEnabled {
				return ui.EventResult{Repaint: terminal.links.clear()}
			}
			changed := terminal.links.hover(terminal.screen, linkPosition)
			_, hovered := terminal.links.hoveredLink(terminal.screen)
			return ui.EventResult{Handled: hovered, Repaint: changed}
		}
		changed := selection.extend(terminal.screen.PointAt(position))
		state.armSelectionScroll(session, event, position.Column)
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
		changed := selection.extend(terminal.screen.PointAt(position))
		selection.dragging = false
		selection.edge = selectionEdge{}
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
		result := ui.EventResult{Handled: true, Repaint: changed}
		if state.terminalCopyOnSelect && selection.selected {
			result.ClipboardText = selection.text(terminal.screen)
		}
		return result
	}
	return ui.EventResult{}
}

// armSelectionScroll decides from where a drag's pointer is whether the view
// has to follow it. A pointer held above or below the grid keeps scrolling
// even when it stops moving, so the steps come from the frames paint asks for
// (see stepSelectionScroll), not from further pointer events.
func (state *State) armSelectionScroll(session *Session, event ui.PointerEvent, column int) {
	selection := &session.term.selection
	geometry := state.terminalLayoutForEvent(session.ID, event.Viewport)
	_, rows := session.term.screen.Size()
	bottom := geometry.top + float32(rows)*state.cellHeight
	edge := selectionEdge{column: column, last: selection.edge.last, stepped: selection.edge.stepped}
	var distance float32
	switch {
	case event.Y < geometry.top && session.term.screen.ViewOffset() < session.term.screen.Scrollback():
		edge.direction, distance = 1, geometry.top-event.Y
	case event.Y >= bottom && session.term.screen.ViewOffset() > 0:
		edge.direction, distance = -1, event.Y-bottom
	default:
		selection.edge = selectionEdge{}
		return
	}
	if state.cellHeight > 0 {
		edge.lines = min(1+int(distance/state.cellHeight), selectionScrollMaxLines)
	} else {
		edge.lines = 1
	}
	selection.edge = edge
}

// stepSelectionScroll scrolls a dragging terminal toward the pointer, once per
// interval, and moves the selection's focus onto the row it uncovers. It runs
// from Wake, where state may change, on the frames paint asked for.
func (state *State) stepSelectionScroll(terminal *terminal, now time.Duration) {
	selection := &terminal.selection
	edge := selection.edge
	if !selection.dragging || edge.direction == 0 {
		return
	}
	if edge.stepped && now-edge.last < selectionScrollInterval {
		return
	}
	if !terminal.screen.Scroll(edge.direction * edge.lines) {
		selection.edge = selectionEdge{}
		return
	}
	row := 0
	if edge.direction < 0 {
		_, rows := terminal.screen.Size()
		row = rows - 1
	}
	selection.edge.last, selection.edge.stepped = now, true
	selection.extend(terminal.screen.PointAt(term.Position{Row: row, Column: edge.column}))
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

func (state *State) terminalPositionAt(session *Session, viewport ui.Size, x, y float32) term.Position {
	geometry := state.terminalLayoutForEvent(session.ID, viewport)
	terminal := session.term
	if terminal == nil {
		return term.Position{}
	}
	// The visible grid can lead the buffer's resize while a window changes.
	// Clamp against both so a click in the painted pane stays in the screen.
	cols, rows := terminal.screen.Size()
	if cols <= 0 || rows <= 0 || geometry.cols <= 0 || geometry.rows <= 0 {
		return term.Position{}
	}
	column, row := 0, 0
	if state.cellWidth > 0 {
		column = int(math.Floor(float64((x - geometry.left) / state.cellWidth)))
	}
	if state.cellHeight > 0 {
		row = int(math.Floor(float64((y - geometry.top) / state.cellHeight)))
	}
	return term.Position{
		Row:    min(max(row, 0), min(geometry.rows, rows)-1),
		Column: min(max(column, 0), min(geometry.cols, cols)-1),
	}
}
