package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// catalogBarGutter is the strip beside a catalog table's rows that the hover
// bar occupies: the pages' right margin and the bar's own thickness, so the bar
// never covers a cell.
const catalogBarGutter = float32(10)

// scrollBarDrag is a pointer's hold on a scrollbar thumb: whether the press
// still owns the pointer and where along the thumb it took it. The page owns the
// offset; this only turns the drag into one, with the geometry the last paint
// left.
type scrollBarDrag struct {
	dragging bool
	grab     float32
}

// pointer drags a bar. bar names the bar's track and focus the viewport the
// press gives the keyboard (zero for none). It moves *offset by the drag and
// reports whether it took the event.
func (drag *scrollBarDrag) pointer(event gfx.PointerEvent, geometry components.ScrollAreaGeometry, bar, focus gfx.ID, offset *float32) (gfx.EventResult, bool) {
	if drag.dragging {
		switch event.Kind {
		case gfx.PointerMove, gfx.PointerUp:
			*offset = geometry.VerticalOffsetForThumb(event.Y - drag.grab)
			if event.Kind == gfx.PointerUp {
				drag.dragging = false
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerExit:
			drag.dragging = false
		}
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Target == bar {
		drag.dragging = true
		drag.grab = event.Y - geometry.VerticalThumb.Y
		return gfx.EventResult{Handled: true, Focus: focus}, true
	}
	return gfx.EventResult{}, false
}
