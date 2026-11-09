package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hostListBar is the bar the hosts page paints for the list in bounds, read from
// the same helper the page uses.
func hostListBar(state *State, bounds gfx.Rect) components.ScrollAreaGeometry {
	_, geometry := hostListScrollArea(nil, state.Theme(), bounds, state.hostsContent, state.hostsScroll)
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

// reservedBarStrip is the room the scroll area keeps for the bar when the list
// overflows; a probe one pixel taller than the list asks the component, so no
// bar size is spelled out here.
func reservedBarStrip(t *testing.T, state *State, bounds gfx.Rect) gfx.Rect {
	t.Helper()
	_, geometry := hostListScrollArea(nil, state.Theme(), bounds, bounds.Height+1, 0)
	if geometry.VerticalTrack.Width <= 0 || geometry.VerticalThumb.Width <= 0 {
		t.Fatalf("the scroll area reserves no strip for a bar: %+v", geometry)
	}
	return geometry.VerticalTrack
}

// An overflowing host list paints Viva's thumb in the strip the scroll area keeps
// beside the rows, and the rows stay out of that strip.
func TestTheHostListPaintsTheScrollbarBesideTheRows(t *testing.T) {
	state := manyHostsState(t, 40)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	list := state.chrome.hostList
	bar := hostListBar(state, list)
	if bar.MaxOffsetY <= 0 || bar.VerticalThumb.Height <= 0 {
		t.Fatalf("an overflowing list has no bar: %+v", bar)
	}
	if bar.Viewport.Width >= list.Width {
		t.Fatalf("the bar's strip was not reserved: viewport %v in %v", bar.Viewport, list)
	}
	row, ok := hitByID(&canvas, hitHostList.At(int(state.hosts[0].ID)))
	if !ok {
		t.Fatal("the first host is not painted")
	}
	if row.Bounds.X+row.Bounds.Width > bar.Viewport.X+bar.Viewport.Width+0.01 {
		t.Fatalf("host row %v runs under the bar (viewport %v)", row.Bounds, bar.Viewport)
	}
	if thumb := paintedIn(&canvas, bar.VerticalTrack); len(thumb) != 1 || thumb[0].Bounds != bar.VerticalThumb {
		t.Fatalf("painted bar = %+v, want the scroll area's thumb %v", thumb, bar.VerticalThumb)
	}
}

// The cards pack into the room left of the bar instead of sitting under it.
func TestTheHostGridKeepsItsCardsOutOfTheScrollbar(t *testing.T) {
	state := manyHostsState(t, 40)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
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
	if thumb := paintedIn(&canvas, bar.VerticalTrack); len(thumb) != 1 || thumb[0].Bounds != bar.VerticalThumb {
		t.Fatalf("painted bar = %+v, want %v", thumb, bar.VerticalThumb)
	}
}

// The thumb follows the list: scrolling to the end moves it down and leaves it
// inside its track.
func TestTheHostListScrollbarFollowsTheScroll(t *testing.T) {
	state := manyHostsState(t, 40)
	size := gfx.Size{Width: 1400, Height: 900}
	var top gfx.Canvas
	state.Paint(&top, size)

	list := state.chrome.hostList
	atTop := hostListBar(state, list)
	if atTop.VerticalThumb.Height <= 0 {
		t.Fatalf("no thumb at the top: %+v", atTop)
	}
	state.hostsScroll = state.hostsContent // past the end: the page clamps it
	var bottom gfx.Canvas
	state.Paint(&bottom, size)

	atBottom := hostListBar(state, list)
	if atBottom.VerticalThumb.Y <= atTop.VerticalThumb.Y {
		t.Fatalf("thumb did not move down: %v then %v", atTop.VerticalThumb, atBottom.VerticalThumb)
	}
	inset := atBottom.VerticalThumb.X - atBottom.VerticalTrack.X
	trackBottom := atBottom.VerticalTrack.Y + atBottom.VerticalTrack.Height - inset
	if math.Abs(float64(atBottom.VerticalThumb.Y+atBottom.VerticalThumb.Height-trackBottom)) > 0.01 {
		t.Fatalf("the thumb %v does not end at the bottom of its track %v", atBottom.VerticalThumb, atBottom.VerticalTrack)
	}
	if thumb := paintedIn(&bottom, atBottom.VerticalTrack); len(thumb) != 1 || thumb[0].Bounds != atBottom.VerticalThumb {
		t.Fatalf("painted bar = %+v, want %v", thumb, atBottom.VerticalThumb)
	}
	if _, ok := hitByID(&bottom, hitHostList.At(int(state.hosts[len(state.hosts)-1].ID))); !ok {
		t.Fatal("the last host is not reachable at the end of the list")
	}
}

// A host list that fits paints no bar and keeps the whole width for its rows.
func TestAShortHostListPaintsNoScrollbar(t *testing.T) {
	state := manyHostsState(t, 3)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	list := state.chrome.hostList
	bar := hostListBar(state, list)
	if bar.MaxOffsetY != 0 || bar.VerticalThumb != (gfx.Rect{}) {
		t.Fatalf("a list that fits has a bar: %+v", bar)
	}
	if bar.Viewport != list {
		t.Fatalf("a list that fits kept a strip for a bar: viewport %v in %v", bar.Viewport, list)
	}
	if painted := paintedIn(&canvas, reservedBarStrip(t, state, list)); len(painted) != 0 {
		t.Fatalf("a list that fits painted %d bar rects: %+v", len(painted), painted)
	}
}

// The bar starts exactly when the content stops fitting: content equal to the
// window paints nothing, one pixel more paints the thumb.
func TestTheHostListScrollbarStartsWhenTheContentOverflows(t *testing.T) {
	bounds := gfx.Rect{X: 223, Y: 139, Width: 1151, Height: 13 * hostRowHeight}
	state := manyHostsState(t, 13)
	var warm gfx.Canvas
	state.Paint(&warm, gfx.Size{Width: 1400, Height: 900}) // the real palette and chrome

	var exact gfx.Canvas
	state.paintHostTable(&exact, bounds)
	if state.hostsContent != bounds.Height {
		t.Fatalf("content %v, viewport %v", state.hostsContent, bounds.Height)
	}
	if bar := hostListBar(state, bounds); bar.MaxOffsetY != 0 {
		t.Fatalf("content equal to the window scrolls: %+v", bar)
	}
	if painted := paintedIn(&exact, reservedBarStrip(t, state, bounds)); len(painted) != 0 {
		t.Fatalf("content equal to the window painted a bar: %+v", painted)
	}

	short := bounds
	short.Height--
	var over gfx.Canvas
	state.paintHostTable(&over, short)
	bar := hostListBar(state, short)
	if bar.MaxOffsetY <= 0 || bar.VerticalThumb.Height <= 0 {
		t.Fatalf("content one pixel taller than the window did not scroll: %+v", bar)
	}
	if thumb := paintedIn(&over, bar.VerticalTrack); len(thumb) != 1 || thumb[0].Bounds != bar.VerticalThumb {
		t.Fatalf("painted bar = %+v, want %v", thumb, bar.VerticalThumb)
	}
}
