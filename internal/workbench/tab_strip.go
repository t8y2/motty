package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// tabStrip keeps the tab list's scroll offset along its layout axis. Nothing
// animates and nothing runs on a timer.
type tabStrip struct {
	area     gfx.Rect // where the tabs are painted, as of the last paint
	vertical bool
	origin   float32 // where the first tab starts when not scrolled
	pitch    float32 // distance from one tab to the next
	tabWidth float32
	offset   float32 // how far the tabs are scrolled along the layout axis
	max      float32 // the furthest offset; zero when every tab fits
	revealed layoutcore.WorkspaceID
	// revealedIndex is the active tab's position in the combined top strip, where
	// workspace tabs come first and runtime file-manager tabs follow.
	revealedIndex    int
	hasRevealedIndex bool
}

// layout fits the offset to this paint's room and returns it. The active tab is
// scrolled into view when it becomes active, and kept visible across geometry
// changes. A wheel gesture that moves it out again is not undone on a repaint.
func (strip *tabStrip) layout(area gfx.Rect, content float32, tabs []layoutcore.WorkspaceInfo, pitch, width float32, active layoutcore.WorkspaceID) float32 {
	activeIndex := -1
	if active != 0 {
		for index, tab := range tabs {
			if tab.ID == active {
				activeIndex = index
				break
			}
		}
	}
	return strip.layoutIndex(area, content, len(tabs), activeIndex, pitch, width)
}

// layoutIndex is layout for a strip whose workspace and runtime tabs share one
// scroll position. activeIndex is -1 when no tab is active.
func (strip *tabStrip) layoutIndex(area gfx.Rect, content float32, count, activeIndex int, pitch, width float32) float32 {
	extent, previousExtent, origin := area.Width, strip.area.Width, area.X
	if strip.vertical {
		extent, previousExtent, origin = area.Height, strip.area.Height, area.Y
	}
	strip.max = max(content-barGap-extent, 0)
	geometryChanged := extent != previousExtent || pitch != strip.pitch || width != strip.tabWidth
	revealChanged := activeIndex >= 0 && (!strip.hasRevealedIndex || activeIndex != strip.revealedIndex)
	if activeIndex >= 0 && activeIndex < count && (revealChanged || geometryChanged) {
		previousStart := float32(activeIndex) * strip.pitch
		previouslyVisible := strip.hasRevealedIndex && previousStart >= strip.offset && previousStart+strip.tabWidth <= strip.offset+previousExtent
		if !revealChanged && geometryChanged && !previouslyVisible {
			// A deliberate wheel scroll away from the active tab survives a repaint.
		} else {
			start := float32(activeIndex) * pitch
			if start < strip.offset {
				strip.offset = start
			} else if start+width > strip.offset+extent {
				strip.offset = start + width - extent
			}
		}
	}
	strip.area, strip.origin, strip.pitch, strip.tabWidth = area, origin, pitch, width
	strip.revealedIndex, strip.hasRevealedIndex = activeIndex, activeIndex >= 0
	strip.offset = min(max(strip.offset, 0), strip.max)
	return strip.offset
}

// scroll moves the tabs by a wheel or trackpad gesture over the strip. A mouse
// wheel scrolls vertically, so that counts as horizontal here.
func (strip *tabStrip) scroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if strip.max <= 0 || !pointInRect(event.X, event.Y, strip.area) {
		return gfx.EventResult{}, false
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
		return gfx.EventResult{Handled: true}, true
	}
	strip.offset = moved
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
