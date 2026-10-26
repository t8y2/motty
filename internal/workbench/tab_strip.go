package workbench

import (
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabStrip is the scroll state of the window bar's tabs. Idle, it is two floats
// and an ID; nothing animates and nothing runs on a timer.
type tabStrip struct {
	area     ui.Rect // where the tabs are painted, as of the last paint
	origin   float32 // where the first tab starts when not scrolled
	pitch    float32 // distance from one tab to the next
	tabWidth float32
	offset   float32 // how far the tabs are scrolled left
	max      float32 // the furthest offset; zero when every tab fits
	revealed layoutcore.WorkspaceID
}

// layout fits the offset to this paint's room and returns it. The active tab is
// scrolled into view once, when it becomes active, so a wheel gesture that moves
// it out again is not undone on the next paint.
func (strip *tabStrip) layout(area ui.Rect, content float32, tabs []layoutcore.WorkspaceInfo, pitch, width float32, active layoutcore.WorkspaceID) float32 {
	strip.area, strip.origin, strip.pitch, strip.tabWidth = area, area.X, pitch, width
	strip.max = max(content-barGap-area.Width, 0)
	if active != 0 && active != strip.revealed {
		for index, tab := range tabs {
			if tab.ID != active {
				continue
			}
			start := float32(index) * pitch
			if start < strip.offset {
				strip.offset = start
			} else if start+width > strip.offset+area.Width {
				strip.offset = start + width - area.Width
			}
		}
	}
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
	if delta == 0 {
		delta = event.DeltaY
	}
	moved := min(max(strip.offset+delta, 0), strip.max)
	if moved == strip.offset {
		return ui.EventResult{Handled: true}, true
	}
	strip.offset = moved
	return ui.EventResult{Handled: true, Repaint: true}, true
}
