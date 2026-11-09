package workbench

import (
	"math"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// paintRemoteProcessTable paints the independent tools window. The snapshot
// stays complete in memory; only the rows intersecting the viewport become
// text runs and hit regions.
func (state *State) paintRemoteProcessTable(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, theme := &state.remote, state.Theme()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	view.processTableBounds = bounds
	pal := state.pal
	canvas.RoundedBorder(bounds, radius.control, 1, pal.line, pal.workspace)

	headerHeight, rowHeight := state.remoteProcessTableMetrics()
	headerBounds := gfx.Rect{X: bounds.X + 1, Y: bounds.Y + 1, Width: max(bounds.Width-2, 0), Height: headerHeight}
	bodyBounds := gfx.Rect{
		X: bounds.X + 1, Y: headerBounds.Bottom(),
		Width: max(bounds.Width-2, 0), Height: max(bounds.Height-headerHeight-1, 0),
	}
	columns, headings := state.processTableSchema(bounds.Width - 2)
	var headerCells [6]components.TableHeader
	order := view.tableSort()
	for index, heading := range headings {
		headerCells[index] = components.TableHeader{
			ID: remoteTableHeaderID(remoteProcesses, index), Label: heading,
			Disabled: view.loading, FocusableWhenDisabled: true,
			Description: state.messages.Tools.RemoteSortAction,
		}
		if order.Direction != components.TableSortNone && order.Column == index {
			headerCells[index].Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				headerCells[index].Description = state.messages.Tools.RemoteSortAscending
			} else {
				headerCells[index].Description = state.messages.Tools.RemoteSortDescending
			}
		}
	}
	components.FitSortableTableHeadings(canvas, theme, columns, headerCells[:len(headings)])
	header := components.BeginTable(canvas, theme, headerBounds, columns).
		HeaderHeight(headerHeight).
		RowHeight(rowHeight)
	header.SortableHeader(headerCells[:len(headings)])

	contentHeight := max(float32(len(view.rows))*rowHeight, bodyBounds.Height)
	firstVisible := min(len(view.rows), 1+int(view.processScroll/rowHeight))
	if view.processScrollFirst != firstVisible || view.processScrollCount != len(view.rows) {
		view.processScrollFirst, view.processScrollCount = firstVisible, len(view.rows)
		view.processScrollValue = strconv.Itoa(firstVisible) + "/" + strconv.Itoa(len(view.rows))
	}
	area := components.BeginScrollArea(canvas, theme, bodyBounds, gfx.Size{Width: bodyBounds.Width, Height: contentHeight}).
		Offset(0, view.processScroll).
		Viewport(hitRemoteProcessTable, state.messages.Tools.RemoteTabProcesses).
		VerticalScrollbar(hitRemoteProcessScroll, state.messages.Tools.RemoteTabProcesses,
			view.processScrollValue)
	geometry := area.Geometry()
	view.processScroll = geometry.OffsetY
	view.processScrollBar = geometry
	geometry = area.BeginContent()
	view.processScroll = geometry.OffsetY

	if len(view.rows) == 0 {
		message := state.messages.Tools.RemoteLoading
		color := pal.muted
		if !view.loading {
			message = view.status
			if message == "" {
				message = state.messages.Tools.RemoteTabProcesses
			}
		}
		canvas.LabelCenter(message, geometry.Viewport, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		area.EndContent()
		return
	}

	first := int(math.Floor(float64(geometry.OffsetY / rowHeight)))
	first = min(max(first, 0), len(view.rows))
	visible := int(math.Ceil(float64(geometry.Viewport.Height/rowHeight))) + 1
	last := min(first+visible, len(view.rows))
	rowRemainder := float32(math.Mod(float64(geometry.OffsetY), float64(rowHeight)))
	tableBounds := gfx.Rect{
		X: geometry.Viewport.X, Y: geometry.Viewport.Y - rowRemainder,
		Width:  geometry.Viewport.Width,
		Height: min(geometry.Viewport.Height+rowHeight, float32(last-first)*rowHeight),
	}
	table := components.BeginTable(canvas, theme, tableBounds, columns).RowHeight(rowHeight)
	for index := first; index < last; index++ {
		row := &view.rows[index]
		id := hitRemoteRowList.At(index)
		table.Row(id, row.cells[:len(headings)], row.key == view.selected)
		state.openRemoteProcessMenuOn(canvas, id, index)
	}
	area.EndContent()
}

func (state *State) remoteProcessTableMetrics() (headerHeight, rowHeight float32) {
	theme := state.Theme()
	return components.TableHeaderHeight(theme), components.TableRowHeight(theme)
}

// remoteProcessScrollPointer drags the shared virtual process table's
// scrollbar with stable pointer capture and thumb geometry.
func (state *State) remoteProcessScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if result, handled := state.remoteProcessEnvironmentScrollPointer(event); handled {
		return result, true
	}
	view := &state.remote
	if !state.remoteToolsVisible() || view.processDetails.open || view.containerDetails.open || view.processMenu.open || view.auto.open ||
		(!view.processScrollDragging &&
			!(event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary &&
				event.Target == hitRemoteProcessScroll)) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	geometry := view.processScrollBar
	before := view.processScroll
	switch event.Kind {
	case gfx.PointerDown:
		view.processScrollDragging = true
		view.processScrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y &&
			event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			view.processScrollGrab = event.Y - geometry.VerticalThumb.Y
		} else {
			view.processScroll = geometry.VerticalOffsetForThumb(event.Y - view.processScrollGrab)
		}
	case gfx.PointerMove:
		if !event.Captured {
			return gfx.EventResult{}, true
		}
		view.processScroll = geometry.VerticalOffsetForThumb(event.Y - view.processScrollGrab)
	case gfx.PointerUp:
		view.processScrollDragging = false
		return gfx.EventResult{Handled: true, Repaint: before != view.processScroll}, true
	case gfx.PointerExit:
		if !event.Captured {
			view.processScrollDragging = false
		}
		return gfx.EventResult{Handled: true, Repaint: before != view.processScroll}, true
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: before != view.processScroll}, true
}

// remoteProcessWheel scrolls the complete snapshot while the renderer keeps
// touching only the rows intersecting the viewport.
func (state *State) remoteProcessWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	view := &state.remote
	if !state.remoteToolsVisible() {
		return gfx.EventResult{}, false
	}
	if view.containerDetails.open {
		if result, handled := state.remoteContainerDetailsWheel(event); handled {
			return result, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if view.processDetails.open {
		if result, handled := state.remoteProcessEnvironmentWheel(event); handled {
			return result, true
		}
		// The details dialog owns the wheel over its backdrop, including when
		// its selectable document fits and has no range.
		return gfx.EventResult{Handled: true}, true
	}
	if view.processMenu.open || view.auto.open {
		return gfx.EventResult{}, false
	}
	if len(view.rows) == 0 ||
		(view.processTableBounds.Width <= 0 && view.processScrollBar.Bounds.Width <= 0) {
		return gfx.EventResult{}, false
	}
	inside := pointInRect(event.X, event.Y, view.processTableBounds) ||
		pointInRect(event.X, event.Y, view.processScrollBar.Bounds) ||
		event.Target == hitRemoteProcessTable || event.Target == hitRemoteProcessScroll ||
		event.Target.Scope() == hitRemoteRowList
	if !inside {
		return gfx.EventResult{}, false
	}
	_, rowHeight := state.remoteProcessTableMetrics()
	step := rowHeight
	before := view.processScroll
	view.processScroll = min(max(view.processScroll+event.StepY(step), 0), view.processScrollBar.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != view.processScroll}, true
}

// remoteProcessTableKey provides roving selection over the filtered and sorted
// snapshot. The selected row is kept inside the virtual viewport.
func (state *State) remoteProcessTableKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	view := &state.remote
	if !state.remoteToolsVisible() || view.tab != remoteProcesses || event.Release {
		return gfx.EventResult{}, false
	}
	targeted := event.Target == hitRemoteProcessTable ||
		event.Target == hitRemoteProcessScroll ||
		event.Target.Scope() == hitRemoteRowList
	if !targeted {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEnter && !event.Repeat {
		if process, ok := state.selectedProcess(); ok {
			state.openRemoteProcessDetails(process)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if len(view.rows) == 0 {
		switch event.Key {
		case gfx.KeyUp, gfx.KeyDown, gfx.KeyPageUp, gfx.KeyPageDown, gfx.KeyHome, gfx.KeyEnd:
			return gfx.EventResult{Handled: true}, true
		}
		return gfx.EventResult{}, false
	}

	current := -1
	for index := range view.rows {
		if view.rows[index].key == view.selected {
			current = index
			break
		}
	}
	_, rowHeight := state.remoteProcessTableMetrics()
	pageRows := max(int(view.processScrollBar.Viewport.Height/rowHeight), 1)
	next := current
	switch event.Key {
	case gfx.KeyUp:
		if current < 0 {
			next = 0
		} else {
			next = max(current-1, 0)
		}
	case gfx.KeyDown:
		if current < 0 {
			next = 0
		} else {
			next = min(current+1, len(view.rows)-1)
		}
	case gfx.KeyPageUp:
		next = max(current-pageRows, 0)
	case gfx.KeyPageDown:
		next = min(max(current, 0)+pageRows, len(view.rows)-1)
	case gfx.KeyHome:
		next = 0
	case gfx.KeyEnd:
		next = len(view.rows) - 1
	default:
		return gfx.EventResult{}, false
	}
	view.selected = view.rows[next].key
	top := float32(next) * rowHeight
	bottom := top + rowHeight
	viewportHeight := view.processScrollBar.Viewport.Height
	if top < view.processScroll {
		view.processScroll = top
	} else if bottom > view.processScroll+viewportHeight {
		view.processScroll = min(bottom-viewportHeight, view.processScrollBar.MaxOffsetY)
	}
	return gfx.EventResult{
		Handled: true, Repaint: true,
		Focus: hitRemoteRowList.At(next), FocusVisible: true,
	}, true
}

func (state *State) processTableSchema(width float32) ([]components.TableColumn, []string) {
	m := state.messages.Tools
	view := &state.remote
	view.processColumns = [6]components.TableColumn{
		{Width: 72, Alignment: components.TableAlignRight},
		{Width: 110},
		{Width: 78, Alignment: components.TableAlignRight},
		{Width: 88, Alignment: components.TableAlignRight},
		{Width: 108},
		{Width: 0},
	}
	view.columns = [6]string{m.RemoteColPID, m.RemoteColUser, m.RemoteColCPU, m.RemoteColMemory, m.RemoteColTime, m.RemoteColCommand}
	const fixed = 72 + 110 + 78 + 88 + 108
	if width < fixed+140 {
		available := max(width-120, 1)
		scale := min(available/fixed, 1)
		for index := range view.processColumns[:5] {
			view.processColumns[index].Width *= scale
		}
	}
	return view.processColumns[:], view.columns[:]
}
