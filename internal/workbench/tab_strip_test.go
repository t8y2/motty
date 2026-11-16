package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestTabStripKeepsVisibleActiveTabInViewWhenLockChangesWidth(t *testing.T) {
	var strip tabStrip
	tabs := []layoutcore.WorkspaceInfo{{ID: 1}, {ID: 2}, {ID: 3}}
	area := gfx.Rect{Width: 300}
	strip.layout(area, 300, tabs, 100, 96, 3)
	offset := strip.layout(area, 354, tabs, 118, 114, 3)
	if float32(2*118+114)-offset > area.Width {
		t.Fatal("adding a lock pushed the active tab's close button offscreen")
	}
	// User scrolling must still be respected on a resize or another lock change.
	strip.offset = 0
	if got := strip.layout(gfx.Rect{Width: 280}, 354, tabs, 118, 114, 3); got != 0 {
		t.Fatal("geometry change undid a deliberate scroll")
	}
}

func manyTabs(t *testing.T, count int) (*State, *gfx.Canvas) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	for range count {
		openEmptyTab(state)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	return state, &canvas
}

func TestNewTabButtonStaysVisibleWhenTabsOverflow(t *testing.T) {
	state, canvas := manyTabs(t, 30)
	plus, ok := hitByID(canvas, hitNewTab)
	if !ok {
		t.Fatal("the new-tab button is not painted")
	}
	search, _ := hitByID(canvas, hitCommandPalette)
	if plus.Bounds.X+plus.Bounds.Width > 1400 || plus.Bounds.X < 0 {
		t.Fatalf("the new-tab button is off the window: %+v", plus.Bounds)
	}
	for _, tab := range state.workspaceTabs {
		hit, ok := hitByID(canvas, hitTabList.At(int(tab.ID)))
		if ok && hit.Bounds.X+hit.Bounds.Width > plus.Bounds.X {
			t.Fatalf("tab %d (%+v) runs under the new-tab button (%+v)", tab.ID, hit.Bounds, plus.Bounds)
		}
	}
	if plus.Bounds.X+plus.Bounds.Width > search.Bounds.X {
		t.Fatalf("the new-tab button %+v overlaps the bar's buttons %+v", plus.Bounds, search.Bounds)
	}
	if state.tabStrip.max <= 0 {
		t.Fatal("thirty tabs must overflow a 1400px window")
	}
}

func TestFewTabsDoNotScrollAndNewTabFollowsTheLastTab(t *testing.T) {
	state, canvas := manyTabs(t, 3)
	if state.tabStrip.max != 0 {
		t.Fatalf("three tabs scroll by %v", state.tabStrip.max)
	}
	last, _ := hitByID(canvas, hitTabList.At(int(state.workspaceTabs[2].ID)))
	plus, ok := hitByID(canvas, hitNewTab)
	if !ok || plus.Bounds.X < last.Bounds.X+last.Bounds.Width {
		t.Fatalf("new tab %+v should sit after the last tab %+v", plus.Bounds, last.Bounds)
	}
}

func TestWheelOverTheTabsScrollsThemAndOnlyThere(t *testing.T) {
	state, _ := manyTabs(t, 30)
	// The newest tab is active, so the strip starts scrolled to the end.
	end := state.tabStrip.offset
	if end <= 0 || end != state.tabStrip.max {
		t.Fatalf("the active (last) tab is not scrolled into view: offset %v max %v", end, state.tabStrip.max)
	}
	over := state.tabStrip.area
	result := state.Scroll(gfx.ScrollEvent{X: over.X + 40, Y: over.Y + 14, DeltaY: -120, Viewport: state.viewport})
	if !result.Handled || !result.Repaint || state.tabStrip.offset != end-120 {
		t.Fatalf("scroll over the tabs: %+v offset %v", result, state.tabStrip.offset)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.tabStrip.offset != end-120 {
		t.Fatalf("the next paint undid the scroll: %v", state.tabStrip.offset)
	}
	state.Scroll(gfx.ScrollEvent{X: over.X, Y: over.Y, DeltaX: -100000})
	if state.tabStrip.offset != 0 {
		t.Fatalf("scrolling past the start must stop at 0, got %v", state.tabStrip.offset)
	}
	before := state.tabStrip.offset
	state.Scroll(gfx.ScrollEvent{X: 700, Y: 400, DeltaY: 100, Viewport: state.viewport})
	if state.tabStrip.offset != before {
		t.Fatal("a wheel over the page moved the tabs")
	}
}

func TestTabShowsFullTitleAsHoverHintWhenLabelIsElided(t *testing.T) {
	state := newTestState(t, testOptions(t))
	const longTitle = "production-db-replica-01 · /var/lib/postgresql/16/main/data"
	id, _, err := state.deck.Create(longTitle)
	if err != nil {
		t.Fatal(err)
	}
	state.deck.ActivateWorkspace(id)
	state.activeSessionID = 0
	state.activeSection = sectionTerminal
	state.rebuildTabOptions()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 600, Height: 900})
	hit, ok := hitByID(&canvas, hitTabList.At(int(id)))
	if !ok {
		t.Fatal("the tab is not painted")
	}
	if hit.Hint != longTitle {
		t.Fatalf("hovering the tab should show its full title, got hint %q want %q", hit.Hint, longTitle)
	}
}

func TestActivatingATabScrollsItIntoView(t *testing.T) {
	state, _ := manyTabs(t, 30)
	state.Scroll(gfx.ScrollEvent{X: state.tabStrip.area.X, Y: 10, DeltaX: -100000})
	first := state.workspaceTabs[0].ID
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitTabList.At(int(first)))
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitTabList.At(int(first)))
	if !ok || hit.Bounds.X < state.tabStrip.area.X || hit.Bounds.X+hit.Bounds.Width > state.tabStrip.area.X+state.tabStrip.area.Width+0.5 {
		t.Fatalf("the activated tab is not fully visible: %+v in %+v (%v)", hit.Bounds, state.tabStrip.area, ok)
	}
}

func TestTabClickRegionReachesTheWindowTopEdge(t *testing.T) {
	state, canvas := manyTabs(t, 3)
	for _, tab := range state.workspaceTabs {
		hit, ok := hitByID(canvas, hitTabList.At(int(tab.ID)))
		if !ok {
			t.Fatalf("tab %d is not painted", tab.ID)
		}
		if hit.Bounds.Y > 0 || hit.Bounds.Y+hit.Bounds.Height < windowBarHeight {
			t.Fatalf("tab %d click region %+v leaves a gap against the top edge or the content below the bar", tab.ID, hit.Bounds)
		}
	}
}

func TestNewTabButtonSitsOnTheTabsCentreLine(t *testing.T) {
	state, canvas := manyTabs(t, 2)
	plus, _ := hitByID(canvas, hitNewTab)
	tab, _ := hitByID(canvas, hitTabList.At(int(state.workspaceTabs[0].ID)))
	if plus.Bounds.Y+plus.Bounds.Height/2 != tab.Bounds.Y+tab.Bounds.Height/2 {
		t.Fatalf("new-tab button %+v is off the tabs' centre line %+v", plus.Bounds, tab.Bounds)
	}
}
