package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// verticalTabsWindow opens the workbench window on state, for a test that has
// to drive real key and pointer traversal rather than the handlers alone.
func verticalTabsWindow(t *testing.T, state *State, size gfx.Size) *headless.Session {
	t.Helper()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

// The sidebar is a page overlay on the pane-chrome band: its rows are painted
// after the page's own hits, so a click on a row wins over the workspace, and
// Tab, which follows the bands, reaches them after the page's controls.
func TestVerticalTabsOverlayRowsComeAfterThePageInTabOrder(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	tab := hitTabList.At(int(state.deck.ActiveWorkspace()))
	row, ok := hitByID(&canvas, tab)
	if !ok {
		t.Fatal("the sidebar painted no tab row")
	}
	if row.Layer != uint16(ui.LayerPaneChrome) {
		t.Fatalf("the sidebar row is on layer %d, want the pane-chrome band %d", row.Layer, ui.LayerPaneChrome)
	}
	pageHit, rowHit := -1, -1
	for index, hit := range canvas.Hits() {
		if hit.Layer == uint16(ui.LayerContent) && hit.ID == hitPaneActivateList.At(0) {
			pageHit = index
		}
		if hit.ID == tab {
			rowHit = index
		}
	}
	if pageHit < 0 {
		t.Fatal("the workspace pane is not a hit of the page")
	}
	if rowHit <= pageHit {
		t.Fatalf("the sidebar row is hit at %d, the page's pane at %d: the page must come first", rowHit, pageHit)
	}

	// The live terminal control treats Tab as its own input, so the traversal
	// part runs on the restored-pane view, whose page has no terminal control:
	// Tab then walks from the page onto the overlay.
	restored, _, _, _ := liveSession(t)
	restored.sessions[0].restored = true
	restored.setTabOrientation(tabsVertical)
	restored.Paint(&canvas, size)
	layers := make(map[gfx.ID]uint16, len(canvas.Hits()))
	for _, hit := range canvas.Hits() {
		layers[hit.ID] = hit.Layer
	}
	view := verticalTabsWindow(t, restored, size)
	driver := view.Driver()
	sawPage, sawOverlay := false, false
	for range 60 {
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		focused := view.Snapshot().Focused
		layer, ok := layers[focused]
		if !ok {
			continue
		}
		switch layer {
		case uint16(ui.LayerContent):
			if sawOverlay {
				t.Fatalf("Tab returned to the page after reaching the pane-chrome overlay")
			}
			sawPage = true
		case uint16(ui.LayerPaneChrome):
			sawOverlay = true
		}
		if sawPage && sawOverlay {
			return
		}
	}
	t.Fatalf("Tab order reached the page %v and the overlay %v", sawPage, sawOverlay)
}

// An expanded auto-hide sidebar floats over the workspace, so its blank surface
// must swallow the press instead of letting it reach the pane under it. The
// sidebar's rows paint on the pane-chrome band, over the terminal pane and over
// a restored pane, which the window declares in the page like any other.
func TestExpandedVerticalTabsCoverTheWorkspaceAndBlockItsPresses(t *testing.T) {
	for _, restored := range []bool{false, true} {
		name := "terminal pane"
		if restored {
			name = "restored pane"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, _ := liveSession(t)
			if restored {
				state.sessions[0].restored = true
			}
			state.setTabOrientation(tabsVertical)
			state.setVerticalTabsAutoHide(true)
			size := gfx.Size{Width: 1400, Height: 900}
			var canvas gfx.Canvas
			state.Paint(&canvas, size)
			rail := state.verticalTabs.bounds.Width
			workspace := state.workspacePixelBounds(size)
			if rail <= 0 || workspace.X != rail {
				t.Fatalf("the collapsed rail is %v wide and the workspace starts at %v", rail, workspace.X)
			}
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: rail / 2, Y: 300, Viewport: size})
			canvas.Reset()
			state.Paint(&canvas, size)
			sidebar := state.verticalTabs.bounds
			if sidebar.Width <= rail || state.workspacePixelBounds(size) != workspace {
				t.Fatalf("the hovered sidebar is %v over a workspace at %v", sidebar, workspace)
			}
			row, ok := hitByID(&canvas, hitTabList.At(int(state.deck.ActiveWorkspace())))
			if !ok {
				t.Fatal("the expanded sidebar painted no row")
			}
			if row.Layer != uint16(ui.LayerPaneChrome) {
				t.Fatalf("the expanded row is on layer %d, want %d", row.Layer, ui.LayerPaneChrome)
			}
			pane, ok := hitByID(&canvas, hitPaneActivateList.At(0))
			if !ok {
				t.Fatal("the workspace pane is not a hit")
			}
			if pane.Layer != uint16(ui.LayerContent) {
				t.Fatalf("the pane is on layer %d, want the page", pane.Layer)
			}
			if pane.Bounds.X >= sidebar.X+sidebar.Width {
				t.Fatalf("the pane %+v does not lie under the sidebar %+v", pane.Bounds, sidebar)
			}
			blank := gfx.Point{X: sidebar.X + 20, Y: sidebar.Y + sidebar.Height - verticalTabsFooter - 30}
			group, ok := hitByID(&canvas, hitVerticalTabs)
			if !ok || group.Role != gfx.RoleGroup || group.Layer != uint16(ui.LayerPaneChrome) ||
				!pointInRect(blank.X, blank.Y, group.Bounds) {
				t.Fatalf("the blank sidebar at %v has no pane-chrome group: %+v", blank, group)
			}
			if group.Focusable || group.SkipTab {
				t.Fatalf("the sidebar group is a Tab stop: %+v", group)
			}

			// Drive the real pointer: the click on the blank surface must leave
			// the focus where the click on the row put it, not on the pane below.
			view := verticalTabsWindow(t, state, size)
			driver := view.Driver()
			if err := driver.Move(rail/2, 300); err != nil {
				t.Fatal(err)
			}
			if err := driver.Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if focused := view.Snapshot().Focused; focused != row.ID {
				t.Fatalf("clicking the sidebar row focused %v, want %v", focused, row.ID)
			}
			if err := driver.Click(blank.X, blank.Y); err != nil {
				t.Fatal(err)
			}
			focused := view.Snapshot().Focused
			if focused == hitPaneActivateList.At(0) || focused == terminalTestHit(state, &state.sessions[0]) {
				t.Fatalf("a press on the blank sidebar reached the workspace: focus %v", focused)
			}
		})
	}
}

// The sidebar is one accessible group on the pane-chrome band: headless
// snapshots see its role and name, it never becomes a Tab stop, and a press on
// its blank surface does not fall through to the workspace beneath.
func TestVerticalTabsExposeAGroupWithoutATabStop(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.sessions[0].restored = true
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	view := verticalTabsWindow(t, state, size)
	driver := view.Driver()

	snapshot := view.Snapshot()
	group, ok := hitByIDSnapshot(snapshot.Hits, hitVerticalTabs)
	if !ok {
		t.Fatal("the sidebar declares no accessible group")
	}
	if group.Role != gfx.RoleGroup || group.Label != state.messages.TrayTitle {
		t.Fatalf("the sidebar group is role %v named %q", group.Role, group.Label)
	}
	if group.Layer != uint16(ui.LayerPaneChrome) {
		t.Fatalf("the sidebar group is on layer %d, want %d", group.Layer, ui.LayerPaneChrome)
	}
	if group.Focusable || group.SkipTab || group.ID.IsZero() {
		t.Fatalf("the sidebar group is a Tab stop: %+v", group)
	}
	// The group is declared first in its overlay, so later controls still win
	// their own presses at the same band.
	groupIndex, pinIndex, newIndex := -1, -1, -1
	for index, hit := range snapshot.Hits {
		switch hit.ID {
		case hitVerticalTabs:
			groupIndex = index
		case hitVerticalTabsPin:
			pinIndex = index
		case hitVerticalTabsNew:
			newIndex = index
		}
	}
	if pinIndex < 0 || newIndex < 0 || groupIndex < 0 {
		t.Fatalf("the sidebar painted group %d, pin %d, new %d", groupIndex, pinIndex, newIndex)
	}
	if pinIndex <= groupIndex || newIndex <= groupIndex {
		t.Fatalf("sidebar controls do not win over the group: group %d, pin %d, new %d", groupIndex, pinIndex, newIndex)
	}

	sidebar := state.verticalTabs.bounds
	blank := gfx.Point{X: sidebar.X + 20, Y: sidebar.Y + sidebar.Height - verticalTabsFooter - 30}
	if err := driver.Click(blank.X, blank.Y); err != nil {
		t.Fatal(err)
	}
	focused := view.Snapshot().Focused
	if focused == hitPaneActivateList.At(0) || focused == terminalTestHit(state, &state.sessions[0]) {
		t.Fatalf("a press on the blank sidebar reached the workspace: focus %v", focused)
	}
	pin, ok := hitByIDSnapshot(view.Snapshot().Hits, hitVerticalTabsPin)
	if !ok {
		t.Fatal("the sidebar lost its auto-hide pin")
	}
	if err := driver.Click(pin.Bounds.X+pin.Bounds.Width/2, pin.Bounds.Y+pin.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.verticalTabs.autoHide {
		t.Fatal("a press on the sidebar pin did not reach the button")
	}
	for range 80 {
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		if view.Snapshot().Focused == hitVerticalTabs {
			t.Fatal("Tab stopped on the sidebar group")
		}
	}
}

// A wheel over the sidebar is the window's own route, not the block's: it
// scrolls an overflowing list and is consumed even when there is nothing to
// scroll, so it cannot leak into the pane under an expanded sidebar.
func TestVerticalTabsKeepTheirOwnWheelRoute(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.sessions[0].restored = true
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&gfx.Canvas{}, size)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: state.verticalTabs.bounds.Width / 2, Y: 300, Viewport: size})
	state.Paint(&gfx.Canvas{}, size)
	blank := state.verticalTabs.bounds
	point := gfx.Point{X: blank.X + 20, Y: blank.Y + blank.Height - verticalTabsFooter - 30}
	if result := state.Scroll(gfx.ScrollEvent{X: point.X, Y: point.Y, DeltaY: 120, Viewport: size}); !result.Handled {
		t.Fatal("a wheel over the sidebar was not consumed")
	}

	for range 20 {
		openEmptyTab(state)
	}
	state.setTabOrientation(tabsVertical)
	state.Paint(&gfx.Canvas{}, size)
	if state.tabStrip.max <= 0 {
		t.Fatal("the tab list does not overflow")
	}
	before := state.tabStrip.offset
	area := state.tabStrip.area
	result := state.Scroll(gfx.ScrollEvent{X: area.X + 10, Y: area.Y + 10, DeltaY: -10000, Viewport: size})
	if !result.Handled || state.tabStrip.offset == before {
		t.Fatalf("the wheel did not scroll the list: handled %v, offset %v -> %v", result.Handled, before, state.tabStrip.offset)
	}
}

// Viva's dialogs and the window's own menus paint above an application band, so
// neither the command palette nor the tab menu can be covered by the sidebar.
func TestDialogsAndMenusPaintAboveTheVerticalTabsOverlay(t *testing.T) {
	state, ids := threeTabs(t)
	state.setTabOrientation(tabsVertical)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	row, ok := hitByID(&canvas, hitTabList.At(int(ids[0])))
	if !ok {
		t.Fatal("the sidebar painted no tab row")
	}

	state.tabMenu = tabMenu{open: true, workspace: ids[0], at: gfx.Point{X: 300, Y: 100}}
	canvas.Reset()
	state.Paint(&canvas, size)
	dismiss, ok := hitByID(&canvas, hitTabMenuDismiss)
	if !ok || dismiss.Layer <= row.Layer {
		t.Fatalf("the tab menu's dismiss layer is %d, the sidebar is %d", dismiss.Layer, row.Layer)
	}

	state.tabMenu = tabMenu{}
	state.openCommandPalette()
	canvas.Reset()
	state.Paint(&canvas, size)
	search, ok := hitByID(&canvas, hitCommandPaletteSearch)
	if !ok || search.Layer <= row.Layer {
		t.Fatalf("the palette search layer is %d, the sidebar is %d", search.Layer, row.Layer)
	}
}

// A dialog or a menu that opens while a tab is held owns the pointer; the drag
// must let go instead of leaving a ghost over a page that no longer answers it.
func TestOpeningADialogOrMenuCancelsATabDrag(t *testing.T) {
	state, ids := threeTabs(t)
	size := gfx.Size{Width: 1400, Height: 900}
	startDrag := func() {
		t.Helper()
		state.setTabOrientation(tabsHorizontal)
		target := hitTabList.At(int(ids[0]))
		from := tabCenter(state, ids[0])
		press(state, target, from)
		dragTo(state, target, gfx.Point{X: from.X + 40, Y: from.Y + 20})
		if !state.tabDrag.active {
			t.Fatal("the tab drag did not start")
		}
	}
	noGhost := func(canvas *gfx.Canvas) {
		t.Helper()
		for _, text := range canvas.Texts() {
			if text.Value == state.tabDrag.title && text.Layer == uint16(ui.LayerDrag) {
				t.Fatalf("the cancelled drag still paints its ghost %q", text.Value)
			}
		}
	}

	startDrag()
	state.openCommandPalette()
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if state.tabDrag.active || state.tabDrag.pressed {
		t.Fatalf("opening a dialog left the drag %+v", state.tabDrag)
	}
	noGhost(&canvas)
	state.closeCommandPalette()

	startDrag()
	state.tabMenu = tabMenu{open: true, workspace: ids[0], at: gfx.Point{X: 300, Y: 100}}
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.tabDrag.active || state.tabDrag.pressed {
		t.Fatalf("opening a menu left the drag %+v", state.tabDrag)
	}
	noGhost(&canvas)
}

// The sidebar and the tab drag are declared every paint; a warm paint must not
// allocate, so their Draw callbacks are method values kept once.
func TestVerticalTabsAndDragOverlaysPaintWithoutAllocating(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.setTabOrientation(tabsVertical)
	state.setVerticalTabsAutoHide(true)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	rail := state.verticalTabs.bounds.Width
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, X: rail / 2, Y: 300, Viewport: size})
	state.Paint(&canvas, size)
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	if allocations := testing.AllocsPerRun(50, paint); allocations != 0 {
		t.Errorf("the vertical tabs paint allocated %v times a frame", allocations)
	}

	dragState, ids := threeTabs(t)
	dragState.setTabOrientation(tabsVertical)
	dragState.Paint(&canvas, size)
	target := hitTabList.At(int(ids[0]))
	from := gfx.Point{X: dragState.verticalTabs.bounds.X + 20, Y: dragState.verticalTabs.bounds.Y + verticalTabsHeader + 10}
	press(dragState, target, from)
	dragTo(dragState, target, gfx.Point{X: from.X + 40, Y: from.Y + 30})
	if !dragState.tabDrag.active {
		t.Fatal("the tab drag did not start")
	}
	dragState.Paint(&canvas, size)
	dragPaint := func() {
		canvas.Reset()
		dragState.Paint(&canvas, size)
	}
	if allocations := testing.AllocsPerRun(50, dragPaint); allocations != 0 {
		t.Errorf("the tab drag paint allocated %v times a frame", allocations)
	}
}
