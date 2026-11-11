package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// A click on a host row of the list view selects it, and a click on a group row
// toggles the group, through the rows the Tree names.
func TestHostListClickSelectsAHostAndTogglesAGroup(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	host := state.hosts[0]
	row, ok := hostListHostHit(canvas, state, host.ID)
	if !ok {
		t.Fatal("the host row is not painted")
	}
	clickRow(state, size, row.ID)
	if state.selectedHostID != host.ID {
		t.Fatalf("click selected %d, want %d", state.selectedHostID, host.ID)
	}

	group := state.hostGroupIndex("Team/Prod")
	expanded := state.hostGroups[group].Expanded
	canvas, _ = paintAt(state, size)
	header, ok := hostListGroupHit(canvas, state, group)
	if !ok {
		t.Fatal("the group row is not painted")
	}
	clickRow(state, size, header.ID)
	if state.hostGroups[group].Expanded == expanded {
		t.Fatal("clicking the group row did not toggle it")
	}
}

// The arrows move between the list's rows, and Enter on a focused host row
// connects it, as the keyboard paths always did.
func TestHostListArrowsAndEnterUseTheTreeRows(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	first, ok := hostListHostHit(canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	second, ok := hostListHostHit(canvas, state, state.hosts[1].ID)
	if !ok {
		t.Fatal("the second host row is not painted")
	}
	result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: first.ID})
	if !result.Handled || result.Focus != second.ID {
		t.Fatalf("Down moved to %v (handled %v), want %v", result.Focus, result.Handled, second.ID)
	}
	before := len(world.conns)
	result = windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEnter, Target: second.ID})
	if !result.Handled || len(world.conns) != before+1 {
		t.Fatalf("Enter on a row dialed %d connections (handled %v)", len(world.conns)-before, result.Handled)
	}
}

// clickRow delivers a click on a page-named row the way the runtime does: the
// paint runs as the click's event pass, and what it deferred runs after it.
func clickRow(state *State, size gfx.Size, id gfx.ID) {
	var canvas gfx.Canvas
	canvas.BeginEventPass(id, gfx.ClickInfo{Clicks: 1})
	state.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

// The hosts list is the workbench page's: its room is the workspace slot's, and
// the Tree's viewport is the bar's viewport, so rows and bar line up.
func TestTheHostListRoomIsTheWorkspaceSlot(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 600, Height: 700}} {
		state := nestedHostGroupState(t)
		state.Paint(&gfx.Canvas{}, size)
		workspace := state.chrome.workspaceSlot.Rect().Inset(gfx.All(workspaceBorder))
		headerHeight := state.hostHeaderHeight(workspace.Width, len(state.hostSelections) > 0)
		if got, want := state.hostList.header.Rect().Height, headerHeight; got != want {
			t.Fatalf("%v: header height %v, want %v", size, got, want)
		}
		wantList := gfx.Rect{X: workspace.X + 18, Y: workspace.Y + headerHeight,
			Width: max(workspace.Width-36, 0), Height: max(workspace.Height-headerHeight-18, 0)}
		if got := state.hostList.bounds; got != wantList {
			t.Fatalf("%v: list room %v, want %v", size, got, wantList)
		}
		_, geometry := hostListScrollArea(nil, state.Theme(), wantList, state.hostsContent, state.hostsScroll)
		if got := state.hostList.area.Rect(); got != geometry.Viewport {
			t.Fatalf("%v: tree room %v, bar viewport %v", size, got, geometry.Viewport)
		}
	}
}

// The wheel belongs to the workbench page, so the window's scroll route moves
// the Tree over the rows; over the bar's strip the list moves the same state.
func TestTheHostListTakesTheWheelThroughTheWindowPage(t *testing.T) {
	state := manyHostsState(t, 40)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	row, ok := hostListHostHit(&canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	at := row.Bounds
	result := windowScrollRoutes(state, gfx.ScrollEvent{X: at.X + at.Width/2, Y: at.Y + at.Height/2, DeltaY: 120, Precise: true, Viewport: size})
	if !result.Repaint || state.hostList.scroll == nil || state.hostList.scroll.Offset <= 0 {
		t.Fatalf("wheel over the rows: %+v scroll %v", result, state.hostList.scroll)
	}
	state.Paint(&canvas, size)
	before := state.hostsScroll
	if before <= 0 {
		t.Fatalf("the paint did not keep the wheel's scroll: %v", before)
	}
	strip := state.hostList.bounds
	result = windowScrollRoutes(state, gfx.ScrollEvent{X: strip.X + strip.Width - 3, Y: strip.Y + 40, DeltaY: 120, Precise: true, Viewport: size})
	if !result.Repaint || state.hostsScroll <= before {
		t.Fatalf("wheel over the bar strip: %+v scroll %v -> %v", result, before, state.hostsScroll)
	}
}

// A secondary press on a row of the list reaches the host menu through the
// window's pointer route, which finds the row the page's Tree named.
func TestHostListContextMenuThroughTheWindowPage(t *testing.T) {
	state := manyHostsState(t, 3)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	target := state.hosts[1]
	row, ok := hostListHostHit(&canvas, state, target.ID)
	if !ok {
		t.Fatal("the host row is not painted")
	}
	windowPointerRoutes(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary,
		Target: row.ID, X: row.Bounds.X + 20, Y: row.Bounds.Y + row.Bounds.Height/2, Viewport: size})
	if !state.hostMenu.open || state.hostMenu.host != target.ID {
		t.Fatalf("menu = %+v, want host %d", state.hostMenu, target.ID)
	}
}

// Tab follows the order the window page declares its hits in. The hosts Tree is
// now part of that page and is declared before the hand-painted header, so from
// a row Tab moves to the header's first control, the search field. (The list
// was a page of its own before, painted after the header, which put the rows
// last.) The test drives the real key through the window, so a later change to
// the declaration order shows up here rather than only in a snapshot.
func TestHostListTabOrderFollowsThePageDeclaration(t *testing.T) {
	state := manyHostsState(t, 3)
	size := gfx.Size{Width: 1400, Height: 900}
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Hosts", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	row, ok := hostListHostHit(&canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	driver := view.Driver()
	if err := driver.Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if focused := view.Snapshot().Focused; focused != row.ID {
		t.Fatalf("the click did not focus the row: %s, want %s", tree.Text(focused), tree.Text(row.ID))
	}
	// The rows and the controls painted inside them come before the header the
	// window paints after the page, so Tab walks the row, its checkbox, the next
	// row, and so on, then reaches the header's first control.
	order := []gfx.ID{
		hitHostCheckList.At(int(state.hosts[0].ID)),
		state.hostList.tree.RowID(1),
		hitHostCheckList.At(int(state.hosts[1].ID)),
		state.hostList.tree.RowID(2),
		hitHostCheckList.At(int(state.hosts[2].ID)),
		hitSearch,
	}
	for _, want := range order {
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
		if focused := view.Snapshot().Focused; focused != want {
			t.Fatalf("Tab focused %s, want %s", tree.Text(focused), tree.Text(want))
		}
	}
}
