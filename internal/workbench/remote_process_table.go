package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

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
	if !state.remoteToolsVisible() || view.processDetails.open || view.containerDetails.open || view.auto.open ||
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
	if view.auto.open {
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

// remoteProcessTableNavigate is the process table's roving selection, without
// the hit identity check: the tools window's table is one page Control and its
// keys reach the control directly.
func (state *State) remoteProcessTableNavigate(event gfx.KeyEvent) gfx.EventResult {
	view := &state.remote
	if view.tab != remoteProcesses || event.Release {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEnter && !event.Repeat {
		if process, ok := state.selectedProcess(); ok {
			state.openRemoteProcessDetails(process)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	if len(view.rows) == 0 {
		switch event.Key {
		case gfx.KeyUp, gfx.KeyDown, gfx.KeyPageUp, gfx.KeyPageDown, gfx.KeyHome, gfx.KeyEnd:
			return gfx.EventResult{Handled: true}
		}
		return gfx.EventResult{}
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
		return gfx.EventResult{}
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
	}
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
