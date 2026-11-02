package workbench

import (
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabStrip keeps the tab list's scroll offset along its layout axis. Nothing
// animates and nothing runs on a timer.
type tabStrip struct {
	area     ui.Rect // where the tabs are painted, as of the last paint
	vertical bool
	origin   float32 // where the first tab starts when not scrolled
	pitch    float32 // distance from one tab to the next
	tabWidth float32
	offset   float32 // how far the tabs are scrolled along the layout axis
	max      float32 // the furthest offset; zero when every tab fits
	revealed layoutcore.WorkspaceID
}

// layout fits the offset to this paint's room and returns it. The active tab is
// scrolled into view when it becomes active, and kept visible across geometry
// changes. A wheel gesture that moves it out again is not undone on a repaint.
func (strip *tabStrip) layout(area ui.Rect, content float32, tabs []layoutcore.WorkspaceInfo, pitch, width float32, active layoutcore.WorkspaceID) float32 {
	extent, previousExtent, origin := area.Width, strip.area.Width, area.X
	if strip.vertical {
		extent, previousExtent, origin = area.Height, strip.area.Height, area.Y
	}
	strip.max = max(content-barGap-extent, 0)
	geometryChanged := extent != previousExtent || pitch != strip.pitch || width != strip.tabWidth
	if active != 0 && (active != strip.revealed || geometryChanged) {
		for index, tab := range tabs {
			if tab.ID != active {
				continue
			}
			previousStart := float32(index) * strip.pitch
			previouslyVisible := previousStart >= strip.offset && previousStart+strip.tabWidth <= strip.offset+previousExtent
			if active == strip.revealed && !previouslyVisible {
				break // preserve a deliberate scroll away from the active tab
			}
			start := float32(index) * pitch
			if start < strip.offset {
				strip.offset = start
			} else if start+width > strip.offset+extent {
				strip.offset = start + width - extent
			}
		}
	}
	strip.area, strip.origin, strip.pitch, strip.tabWidth = area, origin, pitch, width
	strip.revealed = active
	strip.offset = min(max(strip.offset, 0), strip.max)
	return strip.offset
}

// scroll moves the tabs by a wheel or trackpad gesture over the strip. A mouse
// wheel scrolls vertically, so that counts as horizontal here.
func (strip *tabStrip) scroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	if strip.max <= 0 || !pointInRect(event.X, event.Y, strip.area) {
		return ui.EventResult{}, false
	}
	delta := event.DeltaX
	if strip.vertical {
		delta = event.DeltaY
	}
	if delta == 0 {
		if strip.vertical {
			delta = event.DeltaX
		} else {
			delta = event.DeltaY
		}
	}
	moved := min(max(strip.offset+delta, 0), strip.max)
	if moved == strip.offset {
		return ui.EventResult{Handled: true}, true
	}
	strip.offset = moved
	return ui.EventResult{Handled: true, Repaint: true}, true
}
