package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

var windowBarTestSize = gfx.Size{Width: 1400, Height: 900}

func hoverWindowBarTabs(t *testing.T, state *State) gfx.Hit {
	t.Helper()
	over := state.tabStrip.area
	if result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: over.X + 40, Y: windowBarHeight / 2, Viewport: windowBarTestSize}); !result.Repaint {
		t.Fatal("moving over the tabs must repaint to reveal their scrollbar")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, windowBarTestSize)
	bar, ok := hitByID(&canvas, hitWindowBarTabsBar)
	if !ok {
		t.Fatal("hovering the overflowing tabs did not reveal their scrollbar")
	}
	return bar
}

func TestTheTabsScrollbarIsOnlyThereWhenTheTabsOverflow(t *testing.T) {
	state, _ := manyTabs(t, 3)
	over := state.tabStrip.area
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: over.X + 20, Y: windowBarHeight / 2, Viewport: windowBarTestSize})
	var canvas gfx.Canvas
	state.Paint(&canvas, windowBarTestSize)
	if _, ok := hitByID(&canvas, hitWindowBarTabsBar); ok {
		t.Fatal("three tabs fit the bar, yet it paints a scrollbar")
	}
}

// The bar is a thin overlay on the title bar's bottom edge. It is hidden, and
// cannot be hit, until the pointer is over the tabs, so the tabs keep their
// click regions down to the content below the bar.
func TestTheTabsScrollbarAppearsOverTheTabsBottomEdgeOnHover(t *testing.T) {
	state, canvas := manyTabs(t, 30)
	if _, ok := hitByID(canvas, hitWindowBarTabsBar); ok {
		t.Fatal("the scrollbar is painted before the pointer is over the tabs")
	}
	bar := hoverWindowBarTabs(t, state)
	if bar.Role != gfx.RoleSeparator {
		t.Fatalf("the scrollbar has role %v", bar.Role)
	}
	strip := state.tabStrip.area
	if bar.Bounds.Height >= 10 || bar.Bounds.Y+bar.Bounds.Height != windowBarHeight {
		t.Fatalf("the scrollbar %+v is not a thin bar on the title bar's bottom edge", bar.Bounds)
	}
	if bar.Bounds.X < strip.X || bar.Bounds.X+bar.Bounds.Width > strip.X+strip.Width {
		t.Fatalf("the scrollbar %+v runs outside the tabs %+v", bar.Bounds, strip)
	}

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: 700, Y: 400, Viewport: windowBarTestSize})
	var painted gfx.Canvas
	state.Paint(&painted, windowBarTestSize)
	if _, ok := hitByID(&painted, hitWindowBarTabsBar); ok {
		t.Fatal("the scrollbar stayed after the pointer left the tabs")
	}
}

func TestDraggingTheTabsScrollbarScrollsTheTabs(t *testing.T) {
	state, _ := manyTabs(t, 30)
	hoverWindowBarTabs(t, state)
	end := state.tabStrip.offset
	thumb := state.windowBarScroll.geometry.HorizontalThumb
	if end <= 0 || thumb.Width <= 0 {
		t.Fatalf("expected the strip scrolled to its last tab with a thumb: offset %v thumb %+v", end, thumb)
	}
	grab := gfx.Point{X: thumb.X + thumb.Width - 2, Y: thumb.Y + thumb.Height/2}
	if result := press(state, hitWindowBarTabsBar, grab); !result.Handled {
		t.Fatal("pressing the thumb was not handled")
	}
	if state.tabStrip.offset != end {
		t.Fatalf("taking the thumb moved the tabs: %v -> %v", end, state.tabStrip.offset)
	}
	dragTo(state, hitWindowBarTabsBar, gfx.Point{X: grab.X - 200, Y: grab.Y})
	dragged := state.tabStrip.offset
	if dragged >= end {
		t.Fatalf("dragging the thumb left did not scroll the tabs: %v -> %v", end, dragged)
	}
	// The pointer is far below the bar now; the drag still owns it.
	dragTo(state, hitWindowBarTabsBar, gfx.Point{X: grab.X - 200, Y: 500})
	var canvas gfx.Canvas
	state.Paint(&canvas, windowBarTestSize)
	if state.tabStrip.offset != dragged {
		t.Fatalf("the next paint moved the dragged tabs: %v -> %v", dragged, state.tabStrip.offset)
	}
	if _, ok := hitByID(&canvas, hitWindowBarTabsBar); !ok {
		t.Fatal("the scrollbar hid while it was being dragged")
	}
	release(state, hitWindowBarTabsBar, gfx.Point{X: grab.X - 200, Y: 500})
	if state.windowBarScroll.dragging {
		t.Fatal("releasing the pointer did not end the drag")
	}

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit, X: -10, Y: -10, Viewport: windowBarTestSize})
	canvas = gfx.Canvas{}
	state.Paint(&canvas, windowBarTestSize)
	if _, ok := hitByID(&canvas, hitWindowBarTabsBar); ok {
		t.Fatal("the scrollbar stayed after the pointer left the window")
	}
}

func TestPressingTheTabsScrollbarBesideTheThumbBringsItThere(t *testing.T) {
	state, _ := manyTabs(t, 30)
	hoverWindowBarTabs(t, state)
	geometry := state.windowBarScroll.geometry
	at := gfx.Point{X: geometry.HorizontalTrack.X + 2, Y: geometry.HorizontalTrack.Y + geometry.HorizontalTrack.Height/2}
	if at.X >= geometry.HorizontalThumb.X {
		t.Fatalf("the thumb %+v already starts at the track's start %+v", geometry.HorizontalThumb, geometry.HorizontalTrack)
	}
	if result := press(state, hitWindowBarTabsBar, at); !result.Handled || !result.Repaint {
		t.Fatalf("pressing the track was not handled: %+v", result)
	}
	if state.tabStrip.offset != 0 {
		t.Fatalf("pressing the track's start must bring the tabs to their start, offset %v", state.tabStrip.offset)
	}
	release(state, hitWindowBarTabsBar, at)
}

// With vertical tabs the sessions are in the sidebar, which has a bar of its
// own; the title bar keeps only the file managers' tabs.
func TestVerticalTabsGiveTheTitleBarNoSessionScrollbar(t *testing.T) {
	state, _ := manyTabs(t, 30)
	state.setTabOrientation(tabsVertical)
	var canvas gfx.Canvas
	state.Paint(&canvas, windowBarTestSize)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: 700, Y: windowBarHeight / 2, Viewport: windowBarTestSize})
	canvas = gfx.Canvas{}
	state.Paint(&canvas, windowBarTestSize)
	if _, ok := hitByID(&canvas, hitWindowBarTabsBar); ok {
		t.Fatal("the title bar paints a scrollbar for tabs it does not show")
	}
}

func TestWithVerticalTabsTheTitleBarScrollbarDragsTheFileTabs(t *testing.T) {
	state, _, active, _, _ := openSFTPTabFixture(t)
	state.setTabOrientation(tabsVertical)
	for index := range 20 {
		id := active.session.ID + uint32(index) + 1
		manager := &sftpWindow{
			owner: state,
			session: Session{
				ID:      id,
				HostID:  active.session.HostID,
				Title:   "overflow-" + string(rune('A'+index)),
				Address: active.session.Address,
				host:    active.session.host,
			},
			view: &sftpView{},
		}
		state.sftpWindows[id] = manager
		state.sftpTabs = append(state.sftpTabs, manager)
	}
	state.rebuildFileTabOptions()
	size := gfx.Size{Width: 720, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	strip := state.fileTabStrip.area
	if state.fileTabStrip.max <= 0 {
		t.Fatal("twenty file tabs did not overflow the narrow title bar")
	}
	if result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: strip.X + 24, Y: windowBarHeight / 2, Viewport: size}); !result.Repaint {
		t.Fatal("moving over the file tabs must repaint to reveal their scrollbar")
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	bar, ok := hitByID(&canvas, hitWindowBarTabsBar)
	if !ok || bar.Bounds.X < strip.X || bar.Bounds.X+bar.Bounds.Width > strip.X+strip.Width {
		t.Fatalf("the scrollbar %+v (%v) does not run along the file tabs %+v", bar.Bounds, ok, strip)
	}
	geometry := state.windowBarScroll.geometry
	end := geometry.HorizontalTrack.X + geometry.HorizontalTrack.Width - 2
	at := gfx.Point{X: end, Y: geometry.HorizontalTrack.Y + geometry.HorizontalTrack.Height/2}
	before := state.fileTabStrip.offset
	press(state, hitWindowBarTabsBar, at)
	release(state, hitWindowBarTabsBar, at)
	if state.fileTabStrip.offset != state.fileTabStrip.max || state.fileTabStrip.offset == before {
		t.Fatalf("pressing the track's end moved the file tabs %v -> %v, want %v", before, state.fileTabStrip.offset, state.fileTabStrip.max)
	}
}
