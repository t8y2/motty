package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// terminalSelection is anchored to buffer lines, not view rows, so it stays on
// the same text while the view scrolls under a drag.
type terminalSelection struct {
	anchor, focus term.Point
	dragging      bool
	selected      bool
	edge          selectionEdge
	// mark is where the last press was, kept after a click that selected nothing
	// so that a Shift-click can select from it to another point, however far the
	// view was scrolled in between.
	mark   term.Point
	marked bool
	// unit is how far one step of a drag reaches: a cell, or, after a double or
	// triple click, a whole word or line. origin is the word or line that was
	// clicked; the drag keeps it selected whichever way it goes.
	unit   selectionUnit
	origin [2]term.Point
}

// selectionUnit is the granularity a drag snaps to.
type selectionUnit uint8

const (
	unitCell selectionUnit = iota
	unitWord
	unitLine
)

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
	// selectionScrollMaxSteps caps what one frame may pay back after a stall.
	selectionScrollMaxSteps = 4
)

func (selection *terminalSelection) clear() bool {
	changed := selection.dragging || selection.selected
	*selection = terminalSelection{}
	return changed
}

// dismiss drops a selection that came to nothing, a click, and keeps where the
// click was.
func (selection *terminalSelection) dismiss() bool {
	mark, marked := selection.mark, selection.marked
	changed := selection.clear()
	selection.mark, selection.marked = mark, marked
	return changed
}

// beginUnit selects the word or line at point and leaves the drag running, so
// that holding the button and moving on grows the selection by words or lines.
func (selection *terminalSelection) beginUnit(screen *term.Terminal, point term.Point, unit selectionUnit) {
	selection.clear()
	selection.unit = unit
	selection.origin[0], selection.origin[1] = selection.unitRange(screen, point)
	selection.anchor, selection.focus = selection.origin[0], selection.origin[1]
	selection.dragging, selection.selected = true, true
	selection.mark, selection.marked = selection.origin[0], true
}

// unitRange is the word or line around point, by the selection's unit.
func (selection *terminalSelection) unitRange(screen *term.Terminal, point term.Point) (term.Point, term.Point) {
	if selection.unit == unitLine {
		return screen.LineRange(point)
	}
	return screen.WordRange(point)
}

// extendUnit moves the drag's focus to point, snapped to whole words or lines.
// The clicked word or line stays inside the selection: dragging before it
// anchors at its end, and dragging after it anchors at its start.
func (selection *terminalSelection) extendUnit(screen *term.Terminal, point term.Point) bool {
	if selection.unit == unitCell {
		return selection.extend(point)
	}
	if !selection.dragging {
		return false
	}
	start, end := selection.unitRange(screen, point)
	anchor, focus := selection.origin[0], end
	if pointBefore(point, selection.origin[0]) {
		anchor, focus = selection.origin[1], start
	}
	if selection.anchor == anchor && selection.focus == focus {
		return false
	}
	selection.anchor, selection.focus = anchor, focus
	return true
}

func (selection *terminalSelection) begin(point term.Point) bool {
	changed := selection.clear()
	selection.anchor = point
	selection.focus = point
	selection.dragging = true
	selection.mark, selection.marked = point, true
	return changed
}

// extendTo is a Shift-click: the selection, or failing one the last press, is
// stretched to point, as terminals have always done. With a selection the end
// nearer to point moves and the other stays, so it grows or shrinks from either
// side; the drag that may follow goes on adjusting the same end. It reports false
// when there is nothing to extend from.
func (selection *terminalSelection) extendTo(point term.Point) (changed, ok bool) {
	var anchor term.Point
	switch {
	case selection.selected:
		start, end := selection.anchor, selection.focus
		if pointBefore(end, start) {
			start, end = end, start
		}
		switch {
		case pointBefore(point, start):
			anchor = end
		case pointBefore(end, point):
			anchor = start
		case closerToStart(point, start, end):
			anchor = end
		default:
			anchor = start
		}
	case selection.marked:
		anchor = selection.mark
	default:
		return false, false
	}
	previous := *selection
	selection.anchor, selection.focus = anchor, point
	selection.unit = unitCell
	selection.dragging = true
	selection.selected = anchor != point
	selection.mark, selection.marked = anchor, true
	selection.edge = selectionEdge{}
	return previous.anchor != anchor || previous.focus != point || previous.selected != selection.selected, true
}

func pointBefore(left, right term.Point) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Column < right.Column
}

// closerToStart reports whether point, lying inside start..end, is nearer to start
// than to end: by lines first, then by columns when it is level with both.
func closerToStart(point, start, end term.Point) bool {
	fromStart, toEnd := point.Line-start.Line, end.Line-point.Line
	if fromStart != toEnd {
		return fromStart < toEnd
	}
	return point.Column-start.Column < end.Column-point.Column
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
	selection.unit = unitCell
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

func (state *State) terminalPointer(session *Session, event gfx.PointerEvent) gfx.EventResult {
	terminal := session.term
	terminal.screen.SetWordSeparators(state.terminalWordSeparators)
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
	case gfx.PointerDown:
		if event.Button == gfx.PointerButtonSecondary {
			if state.terminalRightClickPaste && event.Modifiers&gfx.ModifierShift == 0 && terminal.selection.selected {
				state.terminalMenu.open = false
				if err := state.options.SetClipboardText(terminal.selection.text(terminal.screen)); err != nil {
					state.status = err.Error()
				}
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			// A program that asked for the mouse already had this press; what
			// reaches here is the terminal's own. It pastes when the user chose
			// that, and Shift keeps the menu one press away.
			if state.terminalRightClickPaste && event.Modifiers&gfx.ModifierShift == 0 {
				state.terminalMenu.open = false
				state.pasteIntoTerminal(session)
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			state.openTerminalMenu(session)
			state.offerActionsAt(session, position)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}
		}
		state.terminalMenu.open = false
		if event.ClickCount >= 2 {
			terminal.links.clear()
			// A double click takes the word, a triple click the whole line, soft
			// wraps included. The button is still down, so the drag goes on by
			// the same unit.
			unit := unitWord
			if event.ClickCount >= 3 {
				unit = unitLine
			}
			selection.beginUnit(terminal.screen, terminal.screen.PointAt(position), unit)
			result := gfx.EventResult{Handled: true, Repaint: true}
			if state.terminalCopyOnSelect {
				result.ClipboardText = selection.text(terminal.screen)
			}
			return result
		}
		linkChanged := terminal.links.clear()
		if event.Modifiers&gfx.ModifierShift != 0 {
			// Shift-click stretches the selection to here. It is the terminal's own
			// selection even when a program has the mouse, which Shift hands back.
			if changed, ok := selection.extendTo(terminal.screen.PointAt(position)); ok {
				return gfx.EventResult{Handled: true, Repaint: changed || linkChanged}
			}
		}
		if linksEnabled && event.Button == gfx.PointerButtonPrimary {
			linkChanged = terminal.links.press(terminal.screen, linkPosition) || linkChanged
		}
		return gfx.EventResult{Handled: true, Repaint: selection.begin(terminal.screen.PointAt(position)) || linkChanged}
	case gfx.PointerMove:
		if !selection.dragging || !event.Captured {
			if !linksEnabled {
				return gfx.EventResult{Repaint: terminal.links.clear()}
			}
			changed := terminal.links.hover(terminal.screen, linkPosition)
			_, hovered := terminal.links.hoveredLink(terminal.screen)
			return gfx.EventResult{Handled: hovered, Repaint: changed}
		}
		changed := selection.extendUnit(terminal.screen, terminal.screen.PointAt(position))
		state.armSelectionScroll(session, event, position.Column)
		if changed {
			changed = terminal.links.clear() || changed
		}
		return gfx.EventResult{Handled: true, Repaint: changed}
	case gfx.PointerUp, gfx.PointerExit:
		if !selection.dragging {
			if event.Kind == gfx.PointerUp && linksEnabled && event.Button == gfx.PointerButtonPrimary {
				target, open, linkChanged := terminal.links.release(terminal.screen, linkPosition)
				if open && terminal.openLink != nil {
					terminal.openLink(target)
				}
				return gfx.EventResult{Handled: event.Captured || open, Repaint: linkChanged}
			}
			changed := terminal.links.clear()
			return gfx.EventResult{Handled: event.Captured, Repaint: changed}
		}
		changed := false
		if event.Kind == gfx.PointerUp {
			changed = selection.extendUnit(terminal.screen, terminal.screen.PointAt(position))
		}
		selection.dragging = false
		selection.edge = selectionEdge{}
		open := false
		target := ""
		if !selection.selected {
			changed = selection.dismiss() || changed
			if event.Kind == gfx.PointerUp && linksEnabled && event.Button == gfx.PointerButtonPrimary {
				var linkChanged bool
				target, open, linkChanged = terminal.links.release(terminal.screen, linkPosition)
				changed = changed || linkChanged
			}
			// A link the click opens is not also a cursor move.
			if !open && !state.terminalInputClickOff && terminalInputClick(event) && state.moveInputCursor(session, position) {
				return gfx.EventResult{Handled: true, Repaint: changed}
			}
		} else {
			changed = terminal.links.clear() || changed
		}
		if event.Kind == gfx.PointerExit {
			changed = terminal.links.clear() || changed
			open = false
		}
		if open && terminal.openLink != nil {
			terminal.openLink(target)
		}
		result := gfx.EventResult{Handled: true, Repaint: changed}
		if state.terminalCopyOnSelect && selection.selected {
			result.ClipboardText = selection.text(terminal.screen)
		}
		return result
	}
	return gfx.EventResult{}
}

// armSelectionScroll decides from where a drag's pointer is whether the view
// has to follow it. A pointer held above or below the grid keeps scrolling
// even when it stops moving, so the steps come from the frames paint asks for
// (see stepSelectionScroll), not from further pointer events.
func (state *State) armSelectionScroll(session *Session, event gfx.PointerEvent, column int) {
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

// stepSelectionScroll scrolls a dragging terminal toward the pointer, at the
// rate the edge asked for, and moves the selection's focus onto the row it
// uncovers. It runs from Wake, where state may change, on the frames paint
// asked for. A frame arrives when the compositor has one, so how many intervals
// have passed decides the step: a frame a little early is not a lost step, and
// a stalled one is not paid back all at once.
func (state *State) stepSelectionScroll(terminal *terminal, now time.Duration) {
	selection := &terminal.selection
	edge := selection.edge
	if !selection.dragging || edge.direction == 0 {
		return
	}
	steps, next := 1, now
	if edge.stepped {
		elapsed := now - edge.last
		if elapsed < selectionScrollInterval {
			return
		}
		steps = min(int(elapsed/selectionScrollInterval), selectionScrollMaxSteps)
		next = edge.last + time.Duration(steps)*selectionScrollInterval
	}
	if !terminal.screen.Scroll(edge.direction * edge.lines * steps) {
		selection.edge = selectionEdge{}
		return
	}
	row := 0
	if edge.direction < 0 {
		_, rows := terminal.screen.Size()
		row = rows - 1
	}
	selection.edge.last, selection.edge.stepped = next, true
	selection.extendUnit(terminal.screen, terminal.screen.PointAt(term.Position{Row: row, Column: edge.column}))
}

func (state *State) terminalMousePointer(session *Session, event gfx.PointerEvent, position term.Position) (gfx.EventResult, bool) {
	terminal := session.term
	if terminal.selection.dragging {
		return gfx.EventResult{}, false
	}
	modes := terminal.screen.Modes()
	switch event.Kind {
	case gfx.PointerDown:
		if modes.Mouse == term.MouseOff || event.Modifiers&gfx.ModifierShift != 0 {
			return gfx.EventResult{}, false
		}
		button, ok := terminalMouseButton(event.Button)
		if !ok {
			return gfx.EventResult{}, false
		}
		terminal.mouseButton = button
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MousePress, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return gfx.EventResult{Handled: true, Repaint: repaint}, true
	case gfx.PointerMove:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			if modes.Mouse != term.MouseAny || event.Modifiers&gfx.ModifierShift != 0 {
				return gfx.EventResult{}, false
			}
		}
		if terminal.mousePositionValid && terminal.mousePosition == position {
			return gfx.EventResult{Handled: true}, true
		}
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseMotion, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return gfx.EventResult{Handled: true, Repaint: repaint}, true
	case gfx.PointerUp:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			return gfx.EventResult{}, false
		}
		terminal.mouseButton = term.MouseButtonNone
		terminal.mousePosition = position
		terminal.mousePositionValid = true
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseRelease, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return gfx.EventResult{Handled: true, Repaint: repaint}, true
	case gfx.PointerExit:
		button := terminal.mouseButton
		if button == term.MouseButtonNone {
			return gfx.EventResult{}, false
		}
		terminal.mouseButton = term.MouseButtonNone
		if terminal.mousePositionValid {
			position = terminal.mousePosition
		}
		repaint := terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseRelease, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		})
		return gfx.EventResult{Handled: true, Repaint: repaint}, true
	}
	return gfx.EventResult{}, false
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

func terminalMouseButton(button gfx.PointerButton) (term.MouseEventButton, bool) {
	switch button {
	case gfx.PointerButtonPrimary:
		return term.MouseButtonLeft, true
	case gfx.PointerButtonMiddle:
		return term.MouseButtonMiddle, true
	case gfx.PointerButtonSecondary:
		return term.MouseButtonRight, true
	default:
		return term.MouseButtonNone, false
	}
}

func terminalMouseModifiers(modifiers gfx.Modifiers) term.MouseModifiers {
	var result term.MouseModifiers
	if modifiers&gfx.ModifierShift != 0 {
		result |= term.MouseModifierShift
	}
	if modifiers&gfx.ModifierMeta != 0 {
		result |= term.MouseModifierMeta
	}
	if modifiers&gfx.ModifierControl != 0 {
		result |= term.MouseModifierControl
	}
	return result
}

func (state *State) terminalPositionAt(session *Session, viewport gfx.Size, x, y float32) term.Position {
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
