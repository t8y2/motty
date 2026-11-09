package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestVerticalTabsSettingsPersistAndSupportKeyboard(t *testing.T) {
	state := newTestState(t, Options{})
	if state.tabOrientation != tabsHorizontal || tabOrientationFromString("invalid") != tabsHorizontal {
		t.Fatal("old or invalid settings must keep horizontal tabs")
	}
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	for _, id := range []gfx.ID{hitSettingsTabsHorizontal, hitSettingsTabsVertical} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("missing General setting %v", id)
		}
	}
	result := state.Key(gfx.KeyEvent{Target: hitSettingsTabsHorizontal, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != hitSettingsTabsVertical || state.tabOrientation != tabsVertical {
		t.Fatalf("keyboard choice: %+v, orientation %v", result, state.tabOrientation)
	}
	state.Click(hitSettingsVerticalTabsAutoHide)
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.TabOrientation != "vertical" || !saved.VerticalTabsAutoHide {
		t.Fatalf("saved tabs: %+v, %v", saved, err)
	}
	options := state.options
	state.Close()
	restored := newTestState(t, options)
	if restored.tabOrientation != tabsVertical || !restored.verticalTabs.autoHide || !restored.tabStrip.vertical {
		t.Fatal("tab preferences did not survive restart")
	}
}

func TestVerticalTabsFitWorkspaceAndRemainReachableFromSettings(t *testing.T) {
	state := newTestState(t, Options{})
	for range 3 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 700, Height: 560}} {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		frame := state.workspacePixelBounds(size)
		if frame.X <= 0 || frame.Width+frame.X != size.Width {
			t.Fatalf("workspace does not make room for tabs: %+v", frame)
		}
		var previous gfx.Rect
		for _, tab := range state.workspaceTabs {
			hit, ok := hitByID(&canvas, hitTabList.At(int(tab.ID)))
			if !ok || hit.Bounds.Y < windowBarHeight || hit.Bounds.X+hit.Bounds.Width > frame.X || hit.Bounds.Y < previous.Y+previous.Height {
				t.Fatalf("tab %d is not stacked in the sidebar: %+v", tab.ID, hit)
			}
			previous = hit.Bounds
		}
		close, ok := hitByID(&canvas, hitTabCloseList.At(int(state.deck.ActiveWorkspace())))
		if !ok || close.Bounds.X+close.Bounds.Width > frame.X {
			t.Fatal("active tab lost its close button")
		}
		plus, ok := hitByID(&canvas, hitNewTab)
		if !ok || plus.Bounds.X+plus.Bounds.Width > size.Width {
			t.Fatal("titlebar new-tab button is offscreen")
		}
	}
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	first := state.workspaceTabs[0].ID
	hit, ok := hitByID(&canvas, hitTabList.At(int(first)))
	if !ok || state.verticalTabs.bounds.Width <= 0 ||
		hit.Bounds.X+hit.Bounds.Width > state.verticalTabs.bounds.X+state.verticalTabs.bounds.Width {
		t.Fatal("settings must retain the session list in its navigation sidebar")
	}
	state.Click(hitTabList.At(int(first)))
	if !state.terminalPage() || state.deck.ActiveWorkspace() != first {
		t.Fatal("sidebar tab did not return to the workspace")
	}
	state.Click(hitTabCloseList.At(int(first)))
	if len(state.deck.Workspaces(nil)) != 2 {
		t.Fatal("vertical tab close did not close the workspace")
	}
}

// The bar names the session the window shows. With the tabs down the left
// sidebar the bar has nothing else on its left, so a name drawn against the
// hosts button read as part of that button group: it belongs over the workspace
// column it names, and clear of the buttons on both of its sides.
func TestVerticalTabsNameTheSessionOverTheWorkspace(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 900, Height: 640}, {Width: 820, Height: 600}} {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		label := state.windowBarTitle()
		var name gfx.Text
		found := false
		for _, text := range canvas.Texts() {
			if text.Value == label && text.Bounds.Y < windowBarHeight {
				name, found = text, true
			}
		}
		if !found {
			t.Fatalf("%vx%v: the bar does not name the session", size.Width, size.Height)
		}
		home, homeOK := hitByID(&canvas, hitHome)
		newTab, newTabOK := hitByID(&canvas, hitNewTab)
		if !homeOK || !newTabOK {
			t.Fatalf("%vx%v: the bar's buttons are missing", size.Width, size.Height)
		}
		left, right := home.Bounds.X+home.Bounds.Width, newTab.Bounds.X
		if name.Bounds.X < left || name.Bounds.X+name.Bounds.Width > right {
			t.Fatalf("%vx%v: the name overlaps the bar's buttons: %+v", size.Width, size.Height, name.Bounds)
		}
		// It sits on the workspace's centre whenever the bar leaves the room,
		// which narrow windows and platforms whose titlebar draws window
		// controls on the right do not: there it stops at the buttons instead
		// of covering them.
		reserve := state.verticalTabsReserve(size)
		want := reserve + (size.Width-reserve)/2
		if want-name.Bounds.Width/2 < left || want+name.Bounds.Width/2 > right {
			continue
		}
		if centre := name.Bounds.X + name.Bounds.Width/2; centre < want-2 || centre > want+2 {
			t.Fatalf("%vx%v: the name is centred at %v, not over the workspace at %v", size.Width, size.Height, centre, want)
		}
	}
}

func TestVerticalTabsScrollNavigateAndReorder(t *testing.T) {
	state := newTestState(t, Options{})
	for range 20 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 1000, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.tabStrip.max <= 0 || state.tabStrip.offset != state.tabStrip.max {
		t.Fatal("active tab must be revealed at the end of an overflowing sidebar")
	}
	area := state.tabStrip.area
	result := state.Scroll(gfx.ScrollEvent{X: area.X + 10, Y: area.Y + 10, DeltaY: -10000, Viewport: size})
	if !result.Handled || state.tabStrip.offset != 0 {
		t.Fatal("wheel did not scroll the sidebar to the start")
	}
	first, second := state.workspaceTabs[0].ID, state.workspaceTabs[1].ID
	state.Click(hitTabList.At(int(first)))
	canvas.Reset()
	state.Paint(&canvas, size)
	result = state.Key(gfx.KeyEvent{Target: hitTabList.At(int(first)), Key: gfx.KeyDown})
	if !result.Handled || result.Focus != hitTabList.At(int(second)) {
		t.Fatalf("vertical tabs must navigate with Up/Down: %+v", result)
	}
	hit, _ := hitByID(&canvas, hitTabList.At(int(first)))
	from := gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
	to := gfx.Point{X: from.X, Y: state.tabStrip.origin + 3*state.tabStrip.pitch}
	target := hitTabList.At(int(first))
	press(state, target, from)
	dragTo(state, target, to)
	if zone := state.tabDrag.drop.zone; state.tabDrag.drop.kind != dropReorder || zone.Width <= zone.Height {
		t.Fatalf("a vertical list needs a marker that lies across it: %+v", state.tabDrag.drop)
	}
	release(state, target, to)
	if got := tabOrder(state); got[0] != second || got[2] != first {
		t.Fatalf("vertical drag did not reorder: %v", got)
	}
}

func TestVerticalTabsHoverDoesNotResizeWorkspace(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	size := gfx.Size{Width: 1000, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	before := state.workspacePixelBounds(size)
	rail := state.verticalTabs.bounds.Width
	if rail <= 0 || before.X != rail {
		t.Fatalf("auto-hide must start as a rail the workspace leaves room for: rail %v, workspace %+v", rail, before)
	}
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: rail / 2, Y: 100, Viewport: size})
	canvas.Reset()
	state.Paint(&canvas, size)
	if !result.Repaint || state.verticalTabs.bounds.Width <= rail || state.workspacePixelBounds(size) != before {
		t.Fatal("hover expansion must overlay rather than resize the workspace")
	}
	expanded := state.verticalTabs.bounds.Width
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: size.Width - 100, Y: 300, Viewport: size})
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width != rail || state.workspacePixelBounds(size) != before {
		t.Fatal("moving away must collapse the sidebar without resizing")
	}
	state.Click(hitVerticalTabsPin)
	if state.verticalTabs.autoHide || state.workspacePixelBounds(size).X != expanded {
		t.Fatal("pinning must keep the sidebar expanded")
	}
}

// A rail of icons is no place to arrive at with the keyboard: the sidebar opens
// while keyboard focus is in it, and a mouse click that leaves focus there does
// not keep it open.
func TestVerticalTabsOpenForKeyboardFocus(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	size := gfx.Size{Width: 1000, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	rail := state.verticalTabs.bounds.Width
	tab := hitTabList.At(int(state.deck.ActiveWorkspace()))
	canvas.Reset()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, tab, false)
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width != rail {
		t.Fatal("a pointer-focused tab must not hold the sidebar open")
	}
	canvas.Reset()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, tab, true)
	state.Paint(&canvas, size)
	if state.verticalTabs.bounds.Width <= rail {
		t.Fatal("keyboard focus on a tab must open the sidebar")
	}
}

func TestTabPreferencesDefaultToHorizontalWhenTheSavedStateLacksThem(t *testing.T) {
	state := newTestState(t, Options{})
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	state.setVerticalTabsGrouping(true)
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil || saved.TabOrientation != "vertical" || !saved.VerticalTabsGrouping {
		t.Fatalf("saved: %+v, %v", saved, err)
	}
	// What an older build writes: the same state without the new fields.
	saved.TabOrientation, saved.VerticalTabsAutoHide, saved.VerticalTabsGrouping = "", false, false
	if err := saveState(state.options.StatePath, saved); err != nil {
		t.Fatal(err)
	}
	options := state.options
	state.Close()
	restored := newTestState(t, options)
	if restored.tabOrientation != tabsHorizontal || restored.verticalTabs.autoHide || restored.verticalTabs.grouping || restored.tabStrip.vertical {
		t.Fatalf("missing fields must restore the horizontal default: %v, auto-hide %v", restored.tabOrientation, restored.verticalTabs.autoHide)
	}
}

func TestAutoHideSettingIsOfferedOnlyForVerticalTabs(t *testing.T) {
	state := newTestState(t, Options{})
	state.Click(hitSettings)
	size := gfx.Size{Width: 1400, Height: 1800}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitSettingsVerticalTabsAutoHide); ok {
		t.Fatal("horizontal tabs have nothing to auto-hide")
	}
	state.Click(hitSettingsTabsVertical)
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitSettingsVerticalTabsAutoHide); !ok {
		t.Fatal("vertical tabs must offer the auto-hide switch")
	}
}

func TestVerticalTabsLeaveRoomForFilesAndAssistant(t *testing.T) {
	state := newTestState(t, Options{})
	openEmptyTab(state)
	state.setTabOrientation(tabsVertical)
	workspace := state.deck.ActiveWorkspace()
	size := gfx.Size{Width: 1600, Height: 900}
	state.ai.open = true
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		if err := state.deck.SetContext(workspace, layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Side: side, Width: layoutcore.DefaultContextWidth}); err != nil {
			t.Fatal(err)
		}
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		files, panes := state.contextRect, state.workspacePixelBounds(size)
		if files.Width <= 0 || files.X < state.verticalTabsReserve(size) || panes.X < state.verticalTabsReserve(size) ||
			files.X+files.Width > size.Width-state.aiReserve(workspace, size)+1 {
			t.Fatalf("panels overlap the tabs or assistant: files=%+v panes=%+v", files, panes)
		}
		if side == layoutcore.ContextLeft && files.X+files.Width > panes.X ||
			side == layoutcore.ContextRight && panes.X+panes.Width > files.X {
			t.Fatalf("files overlap terminal panes: files=%+v panes=%+v", files, panes)
		}
	}
}

// The sidebar scrollbar is an overlay revealed by hovering, not a permanent
// gutter: the tabs keep the full list width, the bar appears only while the
// pointer is over the sidebar, and it can be dragged to move the list.
func TestVerticalTabsRevealScrollbarOnHoverAndDragIt(t *testing.T) {
	state := newTestState(t, Options{})
	for range 20 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 1000, Height: 600}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitVerticalTabsBar); ok {
		t.Fatal("the scrollbar is painted before the pointer is over the sidebar")
	}
	list := state.tabStrip.area
	if state.tabStrip.max <= 0 {
		t.Fatal("expected an overflowing tab list")
	}
	visible := 0
	for _, tab := range state.verticalTabs.tabs {
		hit, ok := hitByID(&canvas, hitTabList.At(int(tab.workspace.ID)))
		if !ok {
			continue
		}
		visible++
		if hit.Bounds.Width != list.Width {
			t.Fatalf("tab %d is narrower than the list: %v != %v", tab.workspace.ID, hit.Bounds.Width, list.Width)
		}
	}
	if visible == 0 {
		t.Fatal("no tab is on screen")
	}

	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: list.X + 20, Y: list.Y + 20, Viewport: size})
	if !result.Repaint {
		t.Fatal("hovering the sidebar must repaint to reveal its scrollbar")
	}
	canvas.Reset()
	state.Paint(&canvas, size)
	bar, ok := hitByID(&canvas, hitVerticalTabsBar)
	if !ok || bar.Role != gfx.RoleSeparator {
		t.Fatalf("hover did not reveal the scrollbar: %+v", bar)
	}
	if bar.Bounds.Width >= 10 {
		t.Fatalf("the hover bar must be narrow: %+v", bar.Bounds)
	}
	side := state.verticalTabs.bounds
	thumb := state.verticalTabs.barGeometry.VerticalThumb
	if thumb.X < list.X+list.Width || thumb.X+thumb.Width > side.X+side.Width {
		t.Fatalf("the hover bar must float outside the rows: thumb %+v rows %+v sidebar %+v", thumb, list, side)
	}

	geometry := state.verticalTabs.barGeometry
	before := state.tabStrip.offset
	grab := gfx.Point{X: geometry.VerticalThumb.X + 1, Y: geometry.VerticalThumb.Y + geometry.VerticalThumb.Height/2}
	if result := press(state, hitVerticalTabsBar, grab); !result.Handled {
		t.Fatal("pressing the scrollbar was not handled")
	}
	dragTo(state, hitVerticalTabsBar, gfx.Point{X: grab.X, Y: grab.Y - 120})
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.tabStrip.offset >= before {
		t.Fatalf("dragging the scrollbar did not scroll the list: %v -> %v", before, state.tabStrip.offset)
	}
	if _, ok := hitByID(&canvas, hitVerticalTabsBar); !ok {
		t.Fatal("the scrollbar hid while it was being dragged")
	}
	release(state, hitVerticalTabsBar, gfx.Point{X: grab.X, Y: grab.Y - 120})

	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit, X: size.Width + 10, Y: size.Height + 10, Viewport: size})
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitVerticalTabsBar); ok {
		t.Fatal("the scrollbar stayed after the pointer left the sidebar")
	}
}
