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

// The bar is a hairline while the pointer is only over the tabs, and grows to
// its full thickness once the pointer reaches it or a drag holds it. At rest it
// would lie across the bar's bottom edge as a light band, which reads as a rule
// when the tabs overflow by less than one tab.
func TestTheTabsScrollbarThickensWhereThePointerReachesIt(t *testing.T) {
	state, _ := manyTabs(t, 30)
	strip := state.tabStrip.area
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: strip.X + 40, Y: windowBarHeight / 2, Viewport: windowBarTestSize})
	hairline, ok := hitByID(paintWindowBar(state), hitWindowBarTabsBar)
	if !ok {
		t.Fatal("the overflowing tabs have no scrollbar")
	}
	if hairline.Bounds.Height != windowBarTabsBarRestSize {
		t.Fatalf("the bar over the tabs is %v thick, want the %v hairline", hairline.Bounds.Height, windowBarTabsBarRestSize)
	}
	if hairline.Bounds.Y+hairline.Bounds.Height != windowBarHeight {
		t.Fatalf("the hairline %+v left the title bar's bottom edge", hairline.Bounds)
	}
	thin := state.windowBarScroll.geometry.HorizontalThumb

	// Reaching the bar grows it, without moving the thumb the drag maps from.
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: strip.X + 40, Y: windowBarHeight - 2, Viewport: windowBarTestSize})
	full, ok := hitByID(paintWindowBar(state), hitWindowBarTabsBar)
	if !ok || full.Bounds.Height != scrollbarHoverSize {
		t.Fatalf("the bar under the pointer is %+v (%v), want %v thick", full.Bounds, ok, scrollbarHoverSize)
	}
	if full.Bounds.Y+full.Bounds.Height != windowBarHeight {
		t.Fatalf("the thick bar %+v left the title bar's bottom edge", full.Bounds)
	}
	thick := state.windowBarScroll.geometry.HorizontalThumb
	if thick.Height <= thin.Height {
		t.Fatalf("the thumb did not grow with the bar: %v -> %v", thin.Height, thick.Height)
	}
	if thick.X != thin.X || thick.Width != thin.Width {
		t.Fatalf("thickening moved the thumb the drag maps from: %+v -> %+v", thin, thick)
	}

	// Leaving the bar returns it to the hairline while the pointer stays on the tabs.
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: strip.X + 40, Y: windowBarHeight / 2, Viewport: windowBarTestSize})
	if back, ok := hitByID(paintWindowBar(state), hitWindowBarTabsBar); !ok || back.Bounds.Height != windowBarTabsBarRestSize {
		t.Fatalf("the bar away from the pointer is %+v (%v), want the %v hairline", back.Bounds, ok, windowBarTabsBarRestSize)
	}

	// A drag that starts on the bar keeps it thick, even with the pointer below it.
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: strip.X + 40, Y: windowBarHeight - 2, Viewport: windowBarTestSize})
	at := gfx.Point{X: strip.X + 40, Y: windowBarHeight - 2}
	if result := press(state, hitWindowBarTabsBar, at); !result.Handled {
		t.Fatal("pressing the bar was not handled")
	}
	dragTo(state, hitWindowBarTabsBar, gfx.Point{X: at.X - 60, Y: 400})
	if held, ok := hitByID(paintWindowBar(state), hitWindowBarTabsBar); !ok || held.Bounds.Height != scrollbarHoverSize {
		t.Fatalf("the dragged bar is %+v (%v), want %v thick", held.Bounds, ok, scrollbarHoverSize)
	}
	release(state, hitWindowBarTabsBar, gfx.Point{X: at.X - 60, Y: 400})
	if view := state.windowBarScroll; view.dragging {
		t.Fatal("releasing the pointer did not end the drag")
	}
	// Let go away from the bar, it goes away with the pointer.
	if _, ok := hitByID(paintWindowBar(state), hitWindowBarTabsBar); ok {
		t.Fatal("the bar stayed after the drag was let go away from the tabs")
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

// The arrows show only where a tab is hidden, and each click moves the tabs one
// tab towards that end, stopping at the first and last tab.
func TestTabArrowsScrollOneTabTowardTheHiddenOnes(t *testing.T) {
	state, canvas := manyTabs(t, 30)
	if _, ok := hitByID(canvas, hitWindowBarTabsNext); ok {
		t.Fatal("the tabs are scrolled to the active last tab, yet an arrow points further on")
	}
	if _, ok := hitByID(canvas, hitWindowBarTabsPrevious); !ok {
		t.Fatal("tabs are hidden before the visible ones, yet no arrow points back")
	}
	pitch := state.tabStrip.pitch
	before := state.tabStrip.offset
	state.Click(hitWindowBarTabsPrevious)
	if got := state.tabStrip.offset; got != before-pitch {
		t.Fatalf("the previous arrow moved the tabs %v -> %v, want one tab (%v)", before, got, before-pitch)
	}
	canvas = paintWindowBar(state)
	if _, ok := hitByID(canvas, hitWindowBarTabsNext); !ok {
		t.Fatal("the tabs moved off their end, yet the next arrow is missing")
	}
	for range 100 {
		state.Click(hitWindowBarTabsPrevious)
	}
	if state.tabStrip.offset != 0 {
		t.Fatalf("the previous arrow did not stop at the first tab: offset %v", state.tabStrip.offset)
	}
	canvas = paintWindowBar(state)
	if _, ok := hitByID(canvas, hitWindowBarTabsPrevious); ok {
		t.Fatal("the first tab is in view, yet an arrow points before it")
	}
	state.Click(hitWindowBarTabsNext)
	if got := state.tabStrip.offset; got != pitch {
		t.Fatalf("the next arrow moved the tabs to %v, want one tab (%v)", got, pitch)
	}
}

func TestNoTabArrowsWhenTheTabsFit(t *testing.T) {
	_, canvas := manyTabs(t, 3)
	for _, id := range []gfx.ID{hitWindowBarTabsPrevious, hitWindowBarTabsNext} {
		if _, ok := hitByID(canvas, id); ok {
			t.Fatalf("three tabs fit the bar, yet an arrow %v is painted", id)
		}
	}
}

// paintWindowBar paints the window once more and returns what it declared.
func paintWindowBar(state *State) *gfx.Canvas {
	var canvas gfx.Canvas
	state.Paint(&canvas, windowBarTestSize)
	return &canvas
}
