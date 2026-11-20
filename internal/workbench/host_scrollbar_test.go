package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hostListBar is the bar the card grid still paints for the list in bounds, read
// from the same helper the page uses.
func hostListBar(state *State, bounds gfx.Rect) components.ScrollAreaGeometry {
	_, geometry := hostListScrollArea(nil, state.Theme(), bounds, state.hostsContent, state.hostsScroll, false)
	return geometry
}

// paintedIn returns the rectangles painted wholly inside room.
func paintedIn(canvas *gfx.Canvas, room gfx.Rect) []gfx.Rectangle {
	var out []gfx.Rectangle
	for _, rect := range canvas.Rectangles() {
		bounds := rect.Bounds
		if bounds.X >= room.X-0.01 && bounds.X+bounds.Width <= room.X+room.Width+0.01 &&
			bounds.Y >= room.Y-0.01 && bounds.Y+bounds.Height <= room.Y+room.Height+0.01 {
			out = append(out, rect)
		}
	}
	return out
}

// An overflowing host list keeps its rows out of the strip the Tree's scrollbar
// reserves, and the Tree holds the scroll position the page reads.
func TestTheHostListKeepsItsRowsOutOfTheScrollbar(t *testing.T) {
	state := manyHostsState(t, 40)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	list := state.chrome.hostList
	if state.hostList.scroll == nil || state.hostList.scroll.Content <= state.hostList.scroll.Viewport {
		t.Fatal("an overflowing list has no scroll position")
	}
	if state.hostList.rowWidth > list.Width-hostListBarWidth+0.01 {
		t.Fatalf("rows keep %v of %v, no strip for the bar", state.hostList.rowWidth, list.Width)
	}
	row, ok := hostListHostHit(&canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host is not painted")
	}
	if row.Bounds.X+row.Bounds.Width > list.X+state.hostList.rowWidth+0.01 {
		t.Fatalf("host row %v runs under the bar (rows end at %v)", row.Bounds, list.X+state.hostList.rowWidth)
	}
}

// The cards pack into the room left of the bar instead of sitting under it.
func TestTheHostGridKeepsItsCardsOutOfTheScrollbar(t *testing.T) {
	state := manyHostsState(t, 40)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	state.hostList.over = true // the bar is hover-revealed
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	list := state.chrome.hostList
	bar := hostListBar(state, list)
	if bar.MaxOffsetY <= 0 || bar.VerticalThumb.Height <= 0 {
		t.Fatalf("the overflowing grid has no bar: %+v", bar)
	}
	for _, host := range state.hosts {
		card, ok := hitByID(&canvas, hitHostList.At(int(host.ID)))
		if !ok {
			continue
		}
		if card.Bounds.X+card.Bounds.Width > bar.Viewport.X+bar.Viewport.Width+0.01 {
			t.Fatalf("card %v runs under the bar (viewport %v)", card.Bounds, bar.Viewport)
		}
	}
}
