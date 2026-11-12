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

// catalogList is the scroll state of a catalog page's painted table: the offset
// its rows are painted at, the body rectangle and content height of the last
// paint, a scrollbar drag, and whether the pointer is over the body. Those pages
// paint rows with components.Table instead of declaring a ui.List, so the page
// owns these; a table itself neither scrolls nor measures a body.
type catalogList struct {
	scroll   float32
	content  float32
	bounds   gfx.Rect
	dragging bool
	grab     float32
	over     bool
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
		HoverScrollbars(list.over || list.dragging)
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
func (list *catalogList) holdingBar() bool { return list.dragging }

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
	if list.dragging {
		switch event.Kind {
		case gfx.PointerMove, gfx.PointerUp:
			list.scroll = list.area(theme).Geometry().VerticalOffsetForThumb(event.Y - list.grab)
			if event.Kind == gfx.PointerUp {
				list.dragging = false
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerExit:
			list.dragging = false
		}
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Target == thumb {
		list.dragging = true
		list.grab = event.Y - list.area(theme).Geometry().VerticalThumb.Y
		return gfx.EventResult{Handled: true, Focus: focus}, true
	}
	return gfx.EventResult{}, false
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

// activeCatalogList is the scroll state of the catalog page the window shows.
func (state *State) activeCatalogList() *catalogList {
	switch state.activeSection {
	case sectionForwards:
		return &state.forwardList
	case sectionKeys:
		return &state.keysList
	case sectionProxies:
		return &state.proxyList
	case sectionSnippets:
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
