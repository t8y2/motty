package workbench

import (
	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hitWindowBarTabsBar      = tree.ID(shellHits, "window-bar-tabs-scrollbar")
	hitWindowBarTabsPrevious = tree.ID(shellHits, "window-bar-tabs-previous")
	hitWindowBarTabsNext     = tree.ID(shellHits, "window-bar-tabs-next")
)

// scrollbarHoverSize is the thickness components.ScrollArea gives a
// HoverScrollbars bar. The pure geometry must use the same thickness, or a drag
// would map to a thumb other than the one that was painted.
const scrollbarHoverSize = float32(6)

// windowBarTabsBarRestSize is the thickness the title bar's tab scrollbar keeps
// while the pointer is only over the tabs. The full bar at rest would lie as a
// light band across the bar's bottom edge, which reads as a rule rather than a
// scrollbar when the tabs overflow by less than one tab and the thumb spans
// nearly the whole track. It stays a hairline until the pointer reaches it or a
// drag holds it, as desktop overlay scrollbars do.
const windowBarTabsBarRestSize = float32(4)

// windowBarScroll is the hover scrollbar under the title bar's tabs. The wheel
// already scrolls them; the bar shows how far, and lets the pointer drag them
// when there is no wheel to turn sideways.
type windowBarScroll struct {
	over     bool // the pointer is over the tabs, so the bar is painted
	near     bool // the pointer is on the bar itself, so it is at full thickness
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

// declareWindowBarTabArrows lays an arrow over each end of the title bar's tabs
// that still hides tabs, as Windows Terminal does, so the hidden ones can be
// reached with the pointer as well as the wheel. A click moves the tabs one tab
// towards them. The arrows lie over the tab room rather than taking width from
// it, so the tabs keep their layout, and they show only where a tab is hidden.
func (state *State) declareWindowBarTabArrows(c ui.C) {
	strip := state.windowBarStrip()
	if strip.max <= 0 {
		return
	}
	m := state.messages
	ui.Row(c).Fill().MarginSides(0, barButtonSize, 0, 0).Items(ui.Center).Children(func() {
		if strip.offset > 0 {
			state.declareWindowBarTabArrow(c, hitWindowBarTabsPrevious, iconChevronLeft, m.TabStripScrollLeft)
		}
		ui.Spacer(c)
		if strip.offset < strip.max {
			state.declareWindowBarTabArrow(c, hitWindowBarTabsNext, iconChevronRight, m.TabStripScrollRight)
		}
	})
}

// declareWindowBarTabArrow declares one arrow on an opaque square, so the tab
// under it does not show through the icon.
func (state *State) declareWindowBarTabArrow(c ui.C, id gfx.ID, icon uint8, label string) {
	ui.Row(c).Size(barButtonSize, barButtonSize).Shrink(0).Background(state.windowFrameColor()).Children(func() {
		state.barIconButton(c, id, icon, label, "")
	})
}

// windowBarTabsRoom is the part of the tab strip the arrows leave free. The
// tabs' badges and labels are painted over the page, clipped to the strip, so
// they must be clipped clear of the arrows too, or they would paint over them.
// The arrows are found by the hits this paint declared: a box from the page is
// valid only until the next page begins, and an arrow that is not declared
// leaves the room alone.
func (state *State) windowBarTabsRoom(canvas *gfx.Canvas, strip gfx.Rect) gfx.Rect {
	if rect, ok := windowBarArrowBounds(canvas, hitWindowBarTabsPrevious); ok {
		strip.Width -= rect.X + rect.Width - strip.X
		strip.X = rect.X + rect.Width
	}
	if rect, ok := windowBarArrowBounds(canvas, hitWindowBarTabsNext); ok {
		strip.Width = rect.X - strip.X
	}
	return strip
}

// windowBarArrowBounds is where the arrow with id was declared in this paint.
func windowBarArrowBounds(canvas *gfx.Canvas, id gfx.ID) (gfx.Rect, bool) {
	for _, hit := range canvas.Hits() {
		if hit.ID == id {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

// scrollWindowBarTabs moves the title bar's tabs by one tab, towards the end
// (direction 1) or the start (direction -1) that the arrow points at.
func (state *State) scrollWindowBarTabs(direction float32) {
	strip := state.windowBarStrip()
	strip.scrollBy(direction * strip.pitch)
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

// windowBarScrollLane is the strip along the bar's bottom edge that the pointer
// has to reach for the bar to grow to its full thickness. It is the resting
// bar's own track, so the bar grows exactly where the thin one lies.
func (state *State) windowBarScrollLane() gfx.Rect {
	bounds := state.windowBarScrollBounds()
	if bounds.Width <= 0 {
		return gfx.Rect{}
	}
	return gfx.Rect{
		X: bounds.X, Y: bounds.Y + bounds.Height - windowBarTabsBarRestSize,
		Width: bounds.Width, Height: windowBarTabsBarRestSize,
	}
}

// paintWindowBarScrollbar paints the bar after the tabs, so it lies over them.
// It paints, and can be hit, only while the pointer is over the tabs or drags
// it, as a hairline until the pointer reaches the bar itself.
func (state *State) paintWindowBarScrollbar(canvas *gfx.Canvas) {
	view := &state.windowBarScroll
	view.geometry = components.ScrollAreaGeometry{}
	bounds := state.windowBarScrollBounds()
	if bounds.Width <= 0 {
		view.over, view.near, view.dragging = false, false, false
		return
	}
	strip := state.windowBarStrip()
	size, hot := windowBarTabsBarRestSize, false
	if view.near || view.dragging {
		size, hot = scrollbarHoverSize, true
	}
	view.geometry = base.LayoutScrollArea(bounds, gfx.Size{Width: bounds.Width + strip.max, Height: bounds.Height},
		base.ScrollAreaLayout{OffsetX: strip.offset, BarSize: size})
	paintScrollbarBar(canvas, state.Theme(), view.geometry, false,
		hitWindowBarTabsBar, state.messages.TabStripScrollbar, "", view.over || view.dragging, hot)
}

// trackWindowBarScroll records whether the pointer is over the title bar's tabs
// and whether it has reached the bar they scroll with. A captured move belongs
// to a press elsewhere and does not change it; the release that ends that press
// does, so a drag let go away from the bar does not leave it showing.
func (state *State) trackWindowBarScroll(event gfx.PointerEvent) bool {
	view := &state.windowBarScroll
	if event.Captured && event.Kind != gfx.PointerUp {
		return false
	}
	over := event.Kind != gfx.PointerExit && state.modal == modalNone && !state.tabMenu.open && !state.tabDrag.pressed &&
		pointInRect(event.X, event.Y, state.windowBarScrollBounds())
	near := over && pointInRect(event.X, event.Y, state.windowBarScrollLane())
	changed := view.over != over || view.near != near
	view.over, view.near = over, near
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
