package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

var remoteToolsPageSize = gfx.Size{Width: 1400, Height: 900}

// remoteToolsPaint paints the workbench page with the tools modal open. The
// page owns the modal's controls, so tests find them by label and role.
func remoteToolsPaint(t testing.TB, state *State) *gfx.Canvas {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, remoteToolsPageSize)
	return &canvas
}

// clickRemoteToolsPageHit delivers a click on a page element in its event pass.
func clickRemoteToolsPageHit(t testing.TB, state *State, hit gfx.Hit) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, remoteToolsPageSize)
	canvas.BeginEventPass(hit.ID, gfx.ClickInfo{})
	state.Paint(&canvas, remoteToolsPageSize)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

// clickRemoteToolsButton clicks the last page button with label. A dialog
// declared after the tools surface owns the last one, so a details action is
// preferred over the list's button with the same label.
func clickRemoteToolsButton(t testing.TB, state *State, label string) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	hit, ok := hitByLabelRoleLast(canvas, label, gfx.RoleButton)
	if !ok {
		t.Fatalf("no page button labeled %q", label)
	}
	clickRemoteToolsPageHit(t, state, hit)
}

// clickRemoteToolsAction clicks the footer button for an action's model id.
func clickRemoteToolsAction(t testing.TB, state *State, id gfx.ID) {
	t.Helper()
	actions, count := state.remoteToolActions()
	for index := 0; index < count; index++ {
		if actions[index].ID == id {
			clickRemoteToolsButton(t, state, actions[index].Label)
			return
		}
	}
	switch id {
	case hitRemoteRefresh:
		clickRemoteToolsButton(t, state, state.messages.Tools.RemoteRefresh)
	default:
		t.Fatalf("no tools action for %v", id)
	}
}

// remoteToolsTableControl finds the page control that paints the current tab's
// table. The last paint records its body geometry.
func remoteToolsTableControl(t testing.TB, canvas *gfx.Canvas, state *State) gfx.Hit {
	t.Helper()
	label := state.remoteTabLabel(state.remote.tab)
	hit, ok := hitByLabelRole(canvas, label, gfx.RoleGroup)
	if !ok {
		t.Fatalf("no table control labeled %q", label)
	}
	return hit
}

// clickRemoteToolsRow presses and releases the primary button on a row. For the
// process tab that selects the row and opens its details.
func clickRemoteToolsRow(t testing.TB, state *State, index int) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	control := remoteToolsTableControl(t, canvas, state)
	x := control.Bounds.X + control.Bounds.Width/2
	y := state.remote.tableBodyBounds.Y + (float32(index-state.remote.tableBodyFirst)+0.5)*state.remote.tableRowHeight
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: x, Y: y, Target: control.ID, Viewport: remoteToolsPageSize,
	}
	windowPointerRoutes(state, down)
	up := down
	up.Kind = gfx.PointerUp
	windowPointerRoutes(state, up)
}

// rightClickRemoteToolsRow opens the context menu on a row.
func rightClickRemoteToolsRow(t testing.TB, state *State, index int) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	control := remoteToolsTableControl(t, canvas, state)
	x := control.Bounds.X + control.Bounds.Width/2
	y := state.remote.tableBodyBounds.Y + (float32(index-state.remote.tableBodyFirst)+0.5)*state.remote.tableRowHeight
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary,
		X: x, Y: y, Target: control.ID, Viewport: remoteToolsPageSize,
	}
	windowPointerRoutes(state, down)
	canvas.BeginPointerPass(down)
	state.Paint(canvas, remoteToolsPageSize)
	canvas.EndPointerPass()
	up := down
	up.Kind = gfx.PointerUp
	windowPointerRoutes(state, up)
}

// clickRemoteToolsMenuItem chooses an item of the open page menu by label.
func clickRemoteToolsMenuItem(t testing.TB, state *State, label string) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	hit, ok := hitByLabelAny(canvas, label)
	if !ok {
		t.Fatalf("no menu item labeled %q", label)
	}
	clickRemoteToolsPageHit(t, state, hit)
}

// clickRemoteToolsSearchClear clears the search from the field's own action.
func clickRemoteToolsSearchClear(t testing.TB, state *State) {
	clickRemoteToolsButton(t, state, state.messages.Tools.RemoteSearchClear)
}

// clickRemoteToolsHeader activates a sortable table header.
func clickRemoteToolsHeader(t testing.TB, state *State, id gfx.ID) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	canvas.BeginEventPass(id, gfx.ClickInfo{})
	state.Paint(canvas, remoteToolsPageSize)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

// clickRemoteToolsDetailTab activates a painted tab inside an open details
// dialog. The dialog's body is a painted slot, so the tab is not a page control.
func clickRemoteToolsDetailTab(t testing.TB, state *State, id gfx.ID) {
	clickRemoteToolsHeader(t, state, id)
}

// openRemoteToolsAutoMenu opens the auto-refresh interval menu from its page
// control.
func openRemoteToolsAutoMenu(t testing.TB, state *State) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	hit, ok := hitByLabelPrefix(canvas, state.messages.Tools.RemoteAutoRefresh, gfx.RoleButton)
	if !ok {
		t.Fatalf("no auto-refresh control")
	}
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2,
		Target: hit.ID, Viewport: remoteToolsPageSize,
	}
	windowPointerRoutes(state, down)
	up := down
	up.Kind = gfx.PointerUp
	windowPointerRoutes(state, up)
	// The menu is declared by the next paint; its keys are the page's from then.
	remoteToolsPaint(t, state)
}

// clickRemoteToolsAutoItem opens the auto-refresh menu and chooses label.
func clickRemoteToolsAutoItem(t testing.TB, state *State, label string) {
	t.Helper()
	openRemoteToolsAutoMenu(t, state)
	clickRemoteToolsMenuItem(t, state, label)
}

// clickRemoteToolsInterval chooses one of the auto-refresh intervals.
func clickRemoteToolsInterval(t testing.TB, state *State, index int) {
	t.Helper()
	period := remoteRefreshIntervals[index]
	clickRemoteToolsAutoItem(t, state, fmt.Sprintf(state.messages.Tools.RemoteRefreshIntervalFmt, int(period/time.Second)))
}

// remoteToolsAutoToggleLabel is the start or stop row of the auto-refresh menu.
func remoteToolsAutoToggleLabel(state *State) string {
	if state.remote.auto.enabled {
		return state.messages.Tools.RemoteAutoStop
	}
	return state.messages.Tools.RemoteAutoStart
}

// clickRemoteToolsSudo clicks the tools' sudo switch, whose label names the
// action it will take.
func clickRemoteToolsSudo(t testing.TB, state *State) {
	t.Helper()
	label := state.messages.SudoCommandsEnable
	if session, ok := state.remoteSession(); ok && session.sudoCommands {
		label = state.messages.SudoCommandsDisable
	}
	clickRemoteToolsButton(t, state, label)
}

// TestRemoteToolsModalEscapeDismisses guards the page dialog's dismiss
// identity: Escape reaches the dialog's DismissID and closes the surface.
func TestRemoteToolsModalEscapeDismisses(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "remote tools escape", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatalf("Escape left modal %d", state.modal)
	}
}

// controls of the object tabs: the first page offers only Next, and the second
// page is reachable and returns to the first.
func TestRemoteToolsModalPagesThroughDockerObjects(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.tab = remoteVolumes
	state.remote.resources = nil
	for index := 0; index < 20; index++ {
		state.remote.resources = append(state.remote.resources, remotetools.Resource{
			Key: fmt.Sprintf("volume-%02d", index), Cells: []string{fmt.Sprintf("volume-%02d", index), "local", "local"},
		})
	}
	state.rebuildRemoteRows()
	canvas := remoteToolsPaint(t, state)
	previous, ok := hitByLabelRoleLast(canvas, state.messages.KnownHostsPrevious, gfx.RoleButton)
	if !ok || !previous.Disabled {
		t.Fatalf("the first page offers Previous: %+v found=%v", previous, ok)
	}
	next, ok := hitByLabelRoleLast(canvas, state.messages.KnownHostsNext, gfx.RoleButton)
	if !ok || next.Disabled {
		t.Fatalf("the first page offers no Next: %+v found=%v", next, ok)
	}
	clickRemoteToolsButton(t, state, state.messages.KnownHostsNext)
	if state.remote.page != 1 {
		t.Fatalf("next page: page=%d", state.remote.page)
	}
	canvas = remoteToolsPaint(t, state)
	if state.remote.pageText != "2/2" {
		t.Fatalf("next page text=%q", state.remote.pageText)
	}
	previous, ok = hitByLabelRoleLast(canvas, state.messages.KnownHostsPrevious, gfx.RoleButton)
	if !ok || previous.Disabled {
		t.Fatalf("the second page offers no Previous: %+v found=%v", previous, ok)
	}
	next, ok = hitByLabelRoleLast(canvas, state.messages.KnownHostsNext, gfx.RoleButton)
	if !ok || !next.Disabled {
		t.Fatalf("the last page offers Next: %+v found=%v", next, ok)
	}
	clickRemoteToolsButton(t, state, state.messages.KnownHostsPrevious)
	if state.remote.page != 0 {
		t.Fatalf("previous page: page=%d", state.remote.page)
	}
	remoteToolsPaint(t, state)
	if state.remote.pageText != "1/2" {
		t.Fatalf("previous page text=%q", state.remote.pageText)
	}
}
