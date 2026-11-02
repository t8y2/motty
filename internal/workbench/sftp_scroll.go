package workbench

import (
	"math"

	"github.com/vivaui/viva/ui"
)

// sftpListPointer drags the side panel's scrollbar with the same geometry and
// grab behaviour as the transfer window. The list keeps integer row anchors for
// hit testing while the drag itself can move continuously.
func (state *State) sftpListPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil {
		return ui.EventResult{}, false
	}
	view := session.files
	if !view.scrollDrag && !(event.Kind == ui.PointerDown && event.Target == hitSFTPListScrollbar) {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	folder := view.active()
	geometry := folder.scroll
	switch event.Kind {
	case ui.PointerDown:
		view.scrollDrag = true
		view.scrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			view.scrollGrab = event.Y - geometry.VerticalThumb.Y
		}
	case ui.PointerMove:
		if !event.Captured {
			return ui.EventResult{}, true
		}
	case ui.PointerUp:
		view.scrollDrag = false
		// The row above the pointer keeps the offset the drag left: paint derives
		// the same row from it with floor, so releasing over a partial row no
		// longer snaps the list back by up to half a row.
		folder.first = min(max(0, int(math.Floor(float64(folder.scrollOffset/sftpRowHeight)))), max(0, len(folder.shownRows())-max(folder.visibleRows, 1)))
		return ui.EventResult{Handled: true, Repaint: true}, true
	case ui.PointerExit:
		if !event.Captured {
			view.scrollDrag = false
		}
		return ui.EventResult{Handled: true}, true
	default:
		return ui.EventResult{}, false
	}
	before := folder.scrollOffset
	folder.scrollOffset = geometry.VerticalOffsetForThumb(event.Y - view.scrollGrab)
	folder.first = min(max(0, int(math.Floor(float64(folder.scrollOffset/sftpRowHeight)))), max(0, len(folder.shownRows())-max(folder.visibleRows, 1)))
	return ui.EventResult{Handled: true, Repaint: before != folder.scrollOffset}, true
}
