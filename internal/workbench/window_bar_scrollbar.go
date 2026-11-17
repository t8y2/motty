package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var hitWindowBarTabsBar = tree.ID(shellHits, "window-bar-tabs-scrollbar")

// windowBarScroll is the hover scrollbar under the title bar's tabs. The wheel
// already scrolls them; the bar shows how far, and lets the pointer drag them
// when there is no wheel to turn sideways.
type windowBarScroll struct {
	over     bool
	dragging bool
	grab     float32 // where along the thumb the pointer took it
	geometry components.ScrollAreaGeometry
}

// windowBarStrip is the strip whose tabs are in the title bar: the sessions and
// file managers with horizontal tabs, only the file managers with vertical ones.
func (state *State) windowBarStrip() *tabStrip {
	if state.tabOrientation == tabsVertical {
		return &state.fileTabStrip
	}
	return &state.tabStrip
}

// windowBarScrollBounds spans the strip's tabs across the whole height of the
// bar. The thin bar sits on the bar's bottom edge, over the last pixels of the
// tabs, and the pointer anywhere over the tabs reveals it.
func (state *State) windowBarScrollBounds() gfx.Rect {
	strip := state.windowBarStrip()
	if strip.max <= 0 || strip.area.Width <= 0 {
		return gfx.Rect{}
	}
	return gfx.Rect{X: strip.area.X, Width: strip.area.Width, Height: windowBarHeight}
}

// paintWindowBarScrollbar paints the bar after the tabs, so it lies over them.
// It paints, and can be hit, only while the pointer is over the tabs or drags it.
func (state *State) paintWindowBarScrollbar(canvas *gfx.Canvas) {
	view := &state.windowBarScroll
	view.geometry = components.ScrollAreaGeometry{}
	bounds := state.windowBarScrollBounds()
	if bounds.Width <= 0 {
		view.over, view.dragging = false, false
		return
	}
	strip := state.windowBarStrip()
	area := components.BeginScrollArea(canvas, state.Theme(), bounds, gfx.Size{Width: bounds.Width + strip.max, Height: bounds.Height}).
		Offset(strip.offset, 0).
		HorizontalScrollbar(hitWindowBarTabsBar, state.messages.TabStripScrollbar, "").
		HoverScrollbars(view.over || view.dragging)
	view.geometry = area.EndContent()
}

// trackWindowBarScroll records whether the pointer is over the title bar's tabs.
// A captured move belongs to a press elsewhere and does not change it.
func (state *State) trackWindowBarScroll(event gfx.PointerEvent) bool {
	view := &state.windowBarScroll
	if event.Captured {
		return false
	}
	over := event.Kind != gfx.PointerExit && state.modal == modalNone && !state.tabMenu.open && !state.tabDrag.pressed &&
		pointInRect(event.X, event.Y, state.windowBarScrollBounds())
	changed := view.over != over
	view.over = over
	return changed
}

// windowBarScrollPointer drags the title bar's scrollbar. A press on the thumb
// keeps the point it took; a press beside it brings the thumb's middle there and
// keeps dragging. The bar stays painted until the drag ends, even outside it.
func (state *State) windowBarScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	view := &state.windowBarScroll
	if event.Target != hitWindowBarTabsBar && !view.dragging {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	strip := state.windowBarStrip()
	switch event.Kind {
	case gfx.PointerDown:
		thumb := view.geometry.HorizontalThumb
		if thumb.Width <= 0 || view.geometry.MaxOffsetX <= 0 {
			return gfx.EventResult{}, false
		}
		view.dragging = true
		view.grab = thumb.Width / 2
		if pointInRect(event.X, event.Y, thumb) {
			view.grab = event.X - thumb.X
		}
		strip.offset = view.geometry.HorizontalOffsetForThumb(event.X - view.grab)
	case gfx.PointerMove:
		if !view.dragging {
			return gfx.EventResult{}, false
		}
		strip.offset = view.geometry.HorizontalOffsetForThumb(event.X - view.grab)
	case gfx.PointerUp, gfx.PointerExit:
		if !view.dragging {
			return gfx.EventResult{}, false
		}
		view.dragging = false
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
