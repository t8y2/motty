package workbench

import (
	"math"

	"github.com/vivaui/viva/gfx"
)

// sftpListPointer drags the side panel's scrollbar with the same geometry and
// grab behaviour as the transfer window. The list keeps integer row anchors for
// hit testing while the drag itself can move continuously.
func (state *State) sftpListPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil {
		return gfx.EventResult{}, false
	}
	view := session.files
	if !view.scrollDrag && !(event.Kind == gfx.PointerDown && event.Target == hitSFTPListScrollbar) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	folder := view.active()
	geometry := folder.scroll
	switch event.Kind {
	case gfx.PointerDown:
		view.scrollDrag = true
		view.scrollGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			view.scrollGrab = event.Y - geometry.VerticalThumb.Y
		}
	case gfx.PointerMove:
		if !event.Captured {
			return gfx.EventResult{}, true
		}
	case gfx.PointerUp:
		view.scrollDrag = false
		// The row above the pointer keeps the offset the drag left: paint derives
		// the same row from it with floor, so releasing over a partial row no
		// longer snaps the list back by up to half a row.
		folder.first = min(max(0, int(math.Floor(float64(folder.scrollOffset/sftpRowHeight)))), max(0, len(folder.shownRows())-max(folder.visibleRows, 1)))
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerExit:
		if !event.Captured {
			view.scrollDrag = false
		}
		return gfx.EventResult{Handled: true}, true
	default:
		return gfx.EventResult{}, false
	}
	before := folder.scrollOffset
	folder.scrollOffset = geometry.VerticalOffsetForThumb(event.Y - view.scrollGrab)
	folder.first = min(max(0, int(math.Floor(float64(folder.scrollOffset/sftpRowHeight)))), max(0, len(folder.shownRows())-max(folder.visibleRows, 1)))
	return gfx.EventResult{Handled: true, Repaint: before != folder.scrollOffset}, true
}
