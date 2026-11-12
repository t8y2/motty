package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// catalogScrollStep is how far one wheel notch moves a catalog table.
const catalogScrollStep = float32(36)

// catalogBarGutter is the strip beside a catalog table's rows that the hover
// bar occupies. It is the right margin those pages' content slots keep, and the
// bar's own thickness, so the bar never covers a cell and the rows' width does
// not change with it.
const catalogBarGutter = float32(10)

// catalogList is the scroll state of a catalog page whose rows the application
// paints itself, with heights the framework's Table cannot know: the snippets
// page's expanded command. The fixed-row catalog pages use ui.Table, which owns
// its own scroll, bars and wheel. A table-like page with variable rows keeps its
// offset, body rectangle, drag and hover here.
type catalogList struct {
	scroll  float32
	content float32
	bounds  gfx.Rect
	bar     scrollBarDrag
	over    bool
}

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

// area is the list's geometry as the last paint left it.
func (list *catalogList) area(theme gfx.Theme) components.ScrollArea {
	return list.newArea(nil, theme)
}

// newArea builds the scroll area; a nil canvas measures without painting. The
// bar is the hover style the rest of Motty's lists use: a slim thumb painted
// only while the pointer is over the list or dragging it. Its bounds include the
// gutter beside the rows, so the bar is painted outside the cells.
func (list *catalogList) newArea(canvas *gfx.Canvas, theme gfx.Theme) components.ScrollArea {
	return components.BeginScrollArea(canvas, theme, list.bounds, gfx.Size{Height: list.content}).
		Offset(0, list.scroll).
		ScrollbarSize(catalogBarGutter).
		HoverScrollbars(list.over || list.bar.dragging)
}

// begin sizes the list's rows and returns its scroll area with the bar and
// viewport identities. body is the rows' rectangle; the bar lives in the gutter
// beside it. The caller paints its fixed header and rows at body's width, then
// calls BeginContent for the translated rows.
func (list *catalogList) begin(canvas *gfx.Canvas, theme gfx.Theme, body gfx.Rect, content float32, viewportID, barID gfx.ID, viewportLabel, barLabel string) (components.ScrollArea, components.ScrollAreaGeometry) {
	list.bounds = gfx.Rect{X: body.X, Y: body.Y, Width: body.Width + catalogBarGutter, Height: body.Height}
	list.content = content
	area := list.newArea(canvas, theme).
		Viewport(viewportID, viewportLabel).
		VerticalScrollbar(barID, barLabel, "")
	return area, area.Geometry()
}

// track records whether the pointer is over the list, which reveals its bar.
func (list *catalogList) track(event gfx.PointerEvent) bool {
	if event.Captured {
		return false
	}
	over := event.Kind != gfx.PointerExit && pointInRect(event.X, event.Y, list.bounds)
	changed := list.over != over
	list.over = over
	return changed
}

// holdingBar reports whether the pointer is down on the bar. While it is, the
// press owns the pointer: rows paint no hover and take no clicks, because what
// the pointer happens to pass over is not what the user is acting on.
func (list *catalogList) holdingBar() bool { return list.bar.dragging }

// wheel moves the list when the wheel is over its body.
func (list *catalogList) wheel(event gfx.ScrollEvent, theme gfx.Theme) gfx.EventResult {
	if !pointInRect(event.X, event.Y, list.bounds) {
		return gfx.EventResult{}
	}
	before := list.scroll
	list.scroll = min(max(0, before+event.StepY(catalogScrollStep)), list.area(theme).Geometry().MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: list.scroll != before}
}

// pointer drags the list's scrollbar. thumb names the bar's track and focus the
// viewport the drag gives the keyboard.
func (list *catalogList) pointer(event gfx.PointerEvent, theme gfx.Theme, thumb, focus gfx.ID) (gfx.EventResult, bool) {
	return list.bar.pointer(event, list.area(theme).Geometry(), thumb, focus, &list.scroll)
}

// key scrolls the list when its viewport has the keyboard.
func (list *catalogList) key(event gfx.KeyEvent, theme gfx.Theme, focus gfx.ID) (gfx.EventResult, bool) {
	if event.Target != focus {
		return gfx.EventResult{}, false
	}
	_, y, ok := list.area(theme).ScrollForKey(0, list.scroll, event.Key, 0)
	if !ok {
		return gfx.EventResult{}, false
	}
	changed := y != list.scroll
	list.scroll = y
	return gfx.EventResult{Handled: true, Repaint: changed}, true
}

// activeCatalogList is the scroll state of the snippets page, the one catalog
// page whose rows are painted by the application.
func (state *State) activeCatalogList() *catalogList {
	if state.activeSection == sectionSnippets {
		return &state.snippets.scroll
	}
	return nil
}

// trackCatalogList records whether the pointer is over the shown page's table,
// which reveals its bar. A dialog owns the pointer while it is open, so no bar
// stays revealed over a surface the user cannot scroll.
func (state *State) trackCatalogList(event gfx.PointerEvent) bool {
	list := state.activeCatalogList()
	if list == nil {
		return false
	}
	if state.modal != modalNone {
		changed := list.over
		list.over = false
		return changed
	}
	return list.track(event)
}
