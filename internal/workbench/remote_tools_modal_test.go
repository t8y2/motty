package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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

// clickRemoteToolsRow selects a row without opening its details.
func clickRemoteToolsRow(t testing.TB, state *State, index int) {
	t.Helper()
	clickRemoteToolsRowCount(t, state, index, 1)
}

func doubleClickRemoteToolsRow(t testing.TB, state *State, index int) {
	t.Helper()
	clickRemoteToolsRowCount(t, state, index, 2)
}

func clickRemoteToolsRowCount(t testing.TB, state *State, index, clickCount int) {
	t.Helper()
	canvas := remoteToolsPaint(t, state)
	control := remoteToolsTableControl(t, canvas, state)
	x := control.Bounds.X + control.Bounds.Width/2
	y := state.remote.tableBodyBounds.Y + (float32(index-state.remote.tableBodyFirst)+0.5)*state.remote.tableRowHeight
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: clickCount,
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
	if id == hitRemoteContainerDetailsOverview || id == hitRemoteContainerDetailsEnvironment {
		clickPageLabelIn(t, state, remoteToolsPageSize, gfx.RoleTab, state.remoteContainerDetailTabLabel(id))
		return
	}
	if id == hitRemoteProcessDetailsCommand || id == hitRemoteProcessDetailsEnv {
		clickPageLabelIn(t, state, remoteToolsPageSize, gfx.RoleTab, state.remoteProcessDetailTabLabel(id))
		return
	}
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

// A precise wheel or a dragged scrollbar stops between two rows. A click must
// select the row painted under the pointer, not the row that would be there if
// the list were aligned to the top of its viewport.
func TestRemoteToolsModalRowHitTestFollowsPaintedRows(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.tab = remoteContainers
	state.remote.rows = make([]remoteRow, 40)
	for index := range state.remote.rows {
		state.remote.rows[index] = remoteRow{
			key:   fmt.Sprintf("resource-%02d", index),
			cells: [6]string{fmt.Sprintf("resource-%02d", index)},
		}
	}
	remoteToolsPaint(t, state)
	rowHeight := state.remote.tableRowHeight
	if rowHeight <= 0 {
		t.Fatal("the table has no row height")
	}
	// Stop more than half a row into the list: an offset that is not a
	// multiple of the row height, as a precise wheel or dragged scrollbar gives.
	state.remote.processScroll = 8*rowHeight + 2*rowHeight/3
	canvas := remoteToolsPaint(t, state)
	if state.remote.tableBodyRemainder == 0 {
		t.Fatalf("scroll offset %.1f landed on a row boundary", state.remote.processScroll)
	}
	viewport := state.remote.processScrollBar.Viewport
	label := "resource-09"
	var bounds gfx.Rect
	for _, painted := range canvas.Texts() {
		if painted.Value == label && pointInRect(painted.Bounds.X, painted.Bounds.Y, viewport) {
			bounds = painted.Bounds
			break
		}
	}
	if bounds.Width == 0 {
		t.Fatalf("%q is not painted after scrolling to %.1f", label, state.remote.processScroll)
	}
	control := remoteToolsTableControl(t, canvas, state)
	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1,
		X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2,
		Target: control.ID, Viewport: remoteToolsPageSize,
	}
	windowPointerRoutes(state, down)
	up := down
	up.Kind = gfx.PointerUp
	windowPointerRoutes(state, up)
	if state.remote.selected != label {
		t.Fatalf("clicking the painted %q selected %q", label, state.remote.selected)
	}
}

func TestRemoteToolsModalScrollsThroughAllDockerObjects(t *testing.T) {
	for _, tab := range []remoteTab{remoteContainers, remoteImages, remoteVolumes, remoteNetworks, remoteCompose} {
		t.Run(fmt.Sprint(tab), func(t *testing.T) {
			state, _, _ := openRemoteToolsDialog(t)
			state.remote.tab = tab
			state.remote.rows = make([]remoteRow, 40)
			for index := range state.remote.rows {
				state.remote.rows[index] = remoteRow{
					key:   fmt.Sprintf("resource-%02d", index),
					cells: [6]string{fmt.Sprintf("resource-%02d", index)},
				}
			}
			canvas := remoteToolsPaint(t, state)
			for _, label := range []string{state.messages.KnownHostsPrevious, state.messages.KnownHostsNext} {
				if _, ok := hitByLabelRoleLast(canvas, label, gfx.RoleButton); ok {
					t.Fatalf("resource list still paints %q", label)
				}
			}
			if state.remote.processScrollBar.MaxOffsetY <= 0 {
				t.Fatal("resource list has no virtual scroll range")
			}
			bounds := state.remote.processTableBounds
			result, handled := state.remoteProcessWheel(gfx.ScrollEvent{
				X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2,
				DeltaY: 100, Precise: true, Viewport: remoteToolsPageSize,
			})
			if !handled || !result.Handled || state.remote.processScroll <= 0 {
				t.Fatalf("resource wheel did not scroll: handled=%v result=%+v", handled, result)
			}
			state.remote.processScroll = state.remote.processScrollBar.MaxOffsetY
			canvas = remoteToolsPaint(t, state)
			if !remoteCanvasHasText(canvas, "resource-39") {
				t.Fatal("the last resource is not reachable by scrolling")
			}
		})
	}
}
