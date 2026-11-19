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
		layout := state.hostsLayout(workspace.Width, workspace.Height)
		wantList := gfx.Rect{X: workspace.X + layout.list.X, Y: workspace.Y + layout.list.Y,
			Width: layout.list.Width, Height: layout.list.Height}
		if got := state.hostList.bounds; got != wantList {
			t.Fatalf("%v: list room %v, want %v", size, got, wantList)
		}
		_, geometry := hostListScrollArea(nil, state.Theme(), wantList, state.hostsContent, state.hostsScroll, false)
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

// Dropping a host on a group row puts it in that group; the group's count and
// the row order follow the move.
func TestHostListDragMovesAHostIntoAGroup(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	host := state.hosts[1]
	row, ok := hostListHostHit(canvas, state, host.ID)
	if !ok {
		t.Fatal("the host row is not painted")
	}
	group := state.hostGroupIndex("Team/Prod")
	header, ok := hostListGroupHit(canvas, state, group)
	if !ok {
		t.Fatal("the target group row is not painted")
	}
	canvas = dragHostGroup(state, size, row, header)
	releaseHostGroup(state, size, canvas, row, header)
	for _, moved := range state.hosts {
		if moved.ID == host.ID {
			if moved.Group != "Team/Prod" {
				t.Fatalf("the dragged host is in %q, want Team/Prod", moved.Group)
			}
			return
		}
	}
	t.Fatal("the dragged host disappeared")
}

// Dropping a host on another host puts it beside that host, in that host's
// group: the list's one drag both moves between groups and orders the rows.
func TestHostListDragOrdersAHostBesideAnother(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := state.hosts[1]
	target := state.hosts[0]
	from, ok := hostListHostHit(canvas, state, source.ID)
	if !ok {
		t.Fatal("the source row is not painted")
	}
	to, ok := hostListHostHit(canvas, state, target.ID)
	if !ok {
		t.Fatal("the target row is not painted")
	}
	canvas = dragHostGroup(state, size, from, to)
	releaseHostGroup(state, size, canvas, from, to)
	sourceAt, targetAt := state.hostIndex(source.ID), state.hostIndex(target.ID)
	if sourceAt != targetAt+1 {
		t.Fatalf("source is at %d, target at %d: want the source right after the target", sourceAt, targetAt)
	}
	for _, moved := range state.hosts {
		if moved.ID == source.ID && moved.Group != "Team/Prod/EU" {
			t.Fatalf("the dropped host is in %q, want the target's group", moved.Group)
		}
	}
}

// A secondary press on the list's own ground offers the catalog's top-level
// actions; a press on a row or an open menu's backdrop does not.
func TestHostsPageBlankPressOffersTopLevelActions(t *testing.T) {
	state := nestedHostGroupState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	paintAt(state, size)
	area := state.chrome.hostList
	if area.Height <= 0 {
		t.Fatal("the list has no room")
	}
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: gfx.ID{},
		X: area.X + 20, Y: area.Y + area.Height - 10, Viewport: size,
	})
	if !result.Handled || !state.hostBlankMenu.open {
		t.Fatalf("blank press result=%+v menu=%v", result, state.hostBlankMenu.open)
	}
	canvas, _ := paintAt(state, size)
	for _, id := range []gfx.ID{hitHostBlankNewHost, hitHostBlankNewGroup} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("blank menu row %s is not painted", tree.Text(id))
		}
	}
}

// The group tree is the whole catalog, not just its folders: hosts sit under
// their groups as rows and choosing one selects it.
func TestHostTreePaneShowsAndSelectsHosts(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostSide.open = true
	state.setHostFilter(true, "Team")
	size := gfx.Size{Width: 1400, Height: 900}
	_, texts := paintAt(state, size)
	if !state.hostSide.visible {
		t.Fatal("the tree pane is not shown")
	}
	hostRow, groupRow := -1, -1
	for i, row := range state.hostSideRows {
		if row.Kind == hostSideRowHost && hostRow < 0 {
			hostRow = i
		}
		if row.Kind == hostSideRowGroup && state.hostGroups[row.GroupIndex].Name == "Team" {
			groupRow = i
		}
	}
	if hostRow < 0 || groupRow < 0 {
		t.Fatalf("rows = %+v, want a host under Team", state.hostSideRows)
	}
	if state.hostSideRows[hostRow].Depth <= state.hostSideRows[groupRow].Depth {
		t.Fatalf("the host row is not under its group: %+v", state.hostSideRows)
	}
	host := state.hosts[state.hostSideRows[hostRow].HostIndex]
	if !contains(texts, host.Label) {
		t.Fatalf("the tree does not paint %q: %v", host.Label, texts)
	}
	clickRow(state, size, state.hostSide.tree.RowID(hostRow))
	if state.selectedHostID != host.ID {
		t.Fatalf("choosing the host selected %d, want %d", state.selectedHostID, host.ID)
	}
}

// A host dragged onto a group row in the tree moves into that group.
func TestHostTreePaneDragsAHostIntoAGroup(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostSide.open = true
	state.setHostFilter(true, "Team")
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	hostRow, groupRow := -1, -1
	for i, row := range state.hostSideRows {
		if row.Kind == hostSideRowHost && hostRow < 0 {
			hostRow = i
		}
		if row.Kind == hostSideRowGroup && state.hostGroups[row.GroupIndex].Name == "Team/Prod" {
			groupRow = i
		}
	}
	if hostRow < 0 || groupRow < 0 {
		t.Fatalf("rows = %+v, want a host under the tree", state.hostSideRows)
	}
	host := state.hosts[state.hostSideRows[hostRow].HostIndex]
	from, ok := hitByID(canvas, state.hostSide.tree.RowID(hostRow))
	if !ok {
		t.Fatal("the host row is not painted")
	}
	to, ok := hitByID(canvas, state.hostSide.tree.RowID(groupRow))
	if !ok {
		t.Fatal("the group row is not painted")
	}
	canvas = dragHostGroup(state, size, from, to)
	releaseHostGroup(state, size, canvas, from, to)
	for _, moved := range state.hosts {
		if moved.ID == host.ID && moved.Group != "Team/Prod" {
			t.Fatalf("the dragged host is in %q, want Team/Prod", moved.Group)
		}
	}
}
