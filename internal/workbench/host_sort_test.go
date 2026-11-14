package workbench

import (
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// beginHostSortCase enters the transient drag a press drag starts, then
// finishes the pickup so a test can press the handles directly.
func beginHostSortCase(t *testing.T, state *State) {
	t.Helper()
	if len(state.hosts) == 0 {
		t.Fatal("no hosts to sort")
	}
	state.beginHostPressDrag(state.hosts[0].ID, gfx.Point{})
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.hostSort.pickup = hostSortPickup{}
}

func TestAltUpDownMovesTheFocusedHostAndSwitchesToManual(t *testing.T) {
	state := manyHostsState(t, 3)
	state.setHostOrder(hostOrderIP)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	row, ok := hostListHostHit(&canvas, state, state.hosts[0].ID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	result := state.Key(gfx.KeyEvent{
		Key: gfx.KeyDown, Modifiers: gfx.ModifierAlt, Target: row.ID,
	})
	if !result.Handled {
		t.Fatal("Alt+Down was not handled over a host row")
	}
	if state.hostOrder != hostOrderManual {
		t.Fatal("the keyboard move did not switch to manual order")
	}
	if state.hosts[0].Label != "Host 01" || state.hosts[1].Label != "Host 00" {
		t.Fatalf("keyboard order = %q, %q", state.hosts[0].Label, state.hosts[1].Label)
	}
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Hosts[0].Label != "Host 01" || saved.Hosts[1].Label != "Host 00" {
		t.Fatalf("saved keyboard order = %q, %q", saved.Hosts[0].Label, saved.Hosts[1].Label)
	}
}

func TestHostOrderButtonKeepsItsMenuAndTheOldSortButtonIsGone(t *testing.T) {
	state := manyHostsState(t, 4)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	if _, ok := hitByID(&canvas, tree.ID(hostGridHits, "host-sort")); ok {
		t.Fatal("the old sort button is still painted")
	}
	if _, ok := hitByID(&canvas, hitHostOrder); !ok {
		t.Fatal("the display-order control is not painted")
	}
	state.Click(hitHostOrder)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitHostOrderManual); !ok {
		t.Fatal("the display-order menu no longer opens")
	}
}

func TestHostPressDragPicksUpAHostAndDragsIt(t *testing.T) {
	state := manyHostsState(t, 4)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	firstID, thirdID := state.hosts[0].ID, state.hosts[2].ID
	first, ok := hostListHostHit(&canvas, state, firstID)
	if !ok {
		t.Fatal("the first host row is not painted")
	}
	third, ok := hostListHostHit(&canvas, state, thirdID)
	if !ok {
		t.Fatal("the third host row is not painted")
	}
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: first.ID,
		X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2, Viewport: size,
	})
	if state.hostSort.enabled {
		t.Fatal("a press alone entered sorting")
	}
	// Moving past the slop turns the press into a drag.
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: first.ID, Captured: true,
		X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2 + hostDragSlop + 2, Viewport: size,
	})
	if !state.hostSort.enabled {
		t.Fatal("dragging the row did not enter sorting")
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size) // the handles this drag needs
	if _, ok := hitByID(&canvas, hitHostSortHandles.At(int(firstID))); !ok {
		t.Fatal("sorting mode has no drag handle")
	}

	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: first.ID, Captured: true,
		X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2, Viewport: size,
	})
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: first.ID, Captured: true,
		X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2, Viewport: size,
	})
	if state.hostSort.enabled {
		t.Fatal("releasing the drag left sorting mode on")
	}
	got := make([]string, len(state.hosts))
	for index := range state.hosts {
		got[index] = state.hosts[index].Label
	}
	if want := []string{"Host 01", "Host 02", "Host 00", "Host 03"}; !slices.Equal(got, want) {
		t.Fatalf("long-press order = %v, want %v", got, want)
	}
}

func TestHostSortCardHandleUsesCheckboxFootprint(t *testing.T) {
	state := manyHostsState(t, 4)
	state.Click(hitHostViewGrid)

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	checkID := hitHostCheckList.At(int(state.hosts[0].ID))
	check, ok := hitByID(&canvas, checkID)
	if !ok {
		t.Fatal("card checkbox is not painted")
	}

	beginHostSortCase(t, state)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)

	handleID := hitHostSortHandles.At(int(state.hosts[0].ID))
	handle, ok := hitByID(&canvas, handleID)
	if !ok {
		t.Fatal("card sort handle is not painted")
	}

	checkCenterX := check.Bounds.X + check.Bounds.Width/2
	handleCenterX := handle.Bounds.X + handle.Bounds.Width/2
	checkCenterY := check.Bounds.Y + check.Bounds.Height/2
	handleCenterY := handle.Bounds.Y + handle.Bounds.Height/2
	if dx := checkCenterX - handleCenterX; dx < -0.01 || dx > 0.01 {
		t.Fatalf("card leading control moved horizontally by %v: checkbox=%+v handle=%+v", dx, check.Bounds, handle.Bounds)
	}
	if dy := checkCenterY - handleCenterY; dy < -0.01 || dy > 0.01 {
		t.Fatalf("card leading control moved vertically by %v: checkbox=%+v handle=%+v", dy, check.Bounds, handle.Bounds)
	}
}

func TestHostSortDragReordersCardsAndPersists(t *testing.T) {
	state := manyHostsState(t, 5)
	state.Click(hitHostViewGrid)
	beginHostSortCase(t, state)

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	firstID := state.hosts[0].ID
	thirdID := state.hosts[2].ID
	firstHandle := hitHostSortHandles.At(int(firstID))
	thirdHandle := hitHostSortHandles.At(int(thirdID))
	first, ok := hitByID(&canvas, firstHandle)
	if !ok {
		t.Fatal("first card handle is not painted")
	}
	third, ok := hitByID(&canvas, thirdHandle)
	if !ok {
		t.Fatal("third card handle is not painted")
	}

	down := gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: firstHandle,
		X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2, Viewport: size,
	}
	if result := pointer(state, down); !result.Handled {
		t.Fatal("sort drag did not capture the handle press")
	}
	move := gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: firstHandle,
		X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2, Viewport: size, Captured: true,
	}
	if result := pointer(state, move); !result.Handled {
		t.Fatal("sort drag did not handle movement")
	}
	beforeDrop := make([]string, len(state.hosts))
	for index := range state.hosts {
		beforeDrop[index] = state.hosts[index].Label
	}
	if want := []string{"Host 00", "Host 01", "Host 02", "Host 03", "Host 04"}; !slices.Equal(beforeDrop, want) {
		t.Fatalf("drag reordered before drop: %v", beforeDrop)
	}
	up := move
	up.Kind = gfx.PointerUp
	if result := pointer(state, up); !result.Handled {
		t.Fatal("sort drag did not handle release")
	}

	got := make([]string, len(state.hosts))
	for index := range state.hosts {
		got[index] = state.hosts[index].Label
	}
	want := []string{"Host 01", "Host 02", "Host 00", "Host 03", "Host 04"}
	if !slices.Equal(got, want) {
		t.Fatalf("card order = %v, want %v", got, want)
	}

	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, host := range saved.Hosts {
		got = append(got, host.Label)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("saved order = %v, want %v", got, want)
	}
}

func TestHostSortListHandleUsesCheckboxFootprint(t *testing.T) {
	state := manyHostsState(t, 4)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	checkID := hitHostCheckList.At(int(state.hosts[0].ID))
	check, ok := hitByID(&canvas, checkID)
	if !ok {
		t.Fatal("checkbox is not painted")
	}

	beginHostSortCase(t, state)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	handleID := hitHostSortHandles.At(int(state.hosts[0].ID))
	handle, ok := hitByID(&canvas, handleID)
	if !ok {
		t.Fatal("sort handle is not painted")
	}

	// The ReorderGrid deliberately expands the handle's hit area, so compare
	// centers: both controls must occupy the same layout slot even if the grip
	// is easier to grab.
	checkCenter := check.Bounds.X + check.Bounds.Width/2
	handleCenter := handle.Bounds.X + handle.Bounds.Width/2
	if delta := checkCenter - handleCenter; delta < -0.01 || delta > 0.01 {
		t.Fatalf("leading control center moved by %v: checkbox=%+v handle=%+v", delta, check.Bounds, handle.Bounds)
	}
}

func TestHostSortDisablesSearchWhileActive(t *testing.T) {
	state := manyHostsState(t, 4)
	beginHostSortCase(t, state)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	search, ok := hitByID(&canvas, hitSearch)
	if !ok || !search.Disabled {
		t.Fatalf("search hit=%+v found=%v", search, ok)
	}
	if editor := state.EditorFor(hitSearch); editor != nil {
		t.Fatal("sorting mode still exposes the search editor")
	}
}

func TestHostSortKeyboardMovesAndPersists(t *testing.T) {
	state := manyHostsState(t, 4)
	beginHostSortCase(t, state)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	firstID := state.hosts[0].ID
	handle := hitHostSortHandles.At(int(firstID))
	result := state.Key(gfx.KeyEvent{Target: handle, Key: gfx.KeyDown})
	if !result.Handled || !result.FocusVisible || result.Focus != handle {
		t.Fatalf("keyboard result=%+v", result)
	}
	want := []string{"Host 01", "Host 00", "Host 02", "Host 03"}
	got := make([]string, len(state.hosts))
	for index := range state.hosts {
		got[index] = state.hosts[index].Label
	}
	if !slices.Equal(got, want) {
		t.Fatalf("keyboard order=%v want=%v", got, want)
	}

	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	got = got[:0]
	for _, host := range saved.Hosts {
		got = append(got, host.Label)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("saved keyboard order=%v want=%v", got, want)
	}
}

func TestHostSortDragReordersListRows(t *testing.T) {
	state := manyHostsState(t, 5)
	beginHostSortCase(t, state)

	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	firstID := state.hosts[0].ID
	thirdID := state.hosts[2].ID
	firstHandle := hitHostSortHandles.At(int(firstID))
	thirdHandle := hitHostSortHandles.At(int(thirdID))
	first, ok := hitByID(&canvas, firstHandle)
	if !ok {
		t.Fatal("first row handle is not painted")
	}
	third, ok := hitByID(&canvas, thirdHandle)
	if !ok {
		t.Fatal("third row handle is not painted")
	}

	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: firstHandle,
		X: first.Bounds.X + first.Bounds.Width/2, Y: first.Bounds.Y + first.Bounds.Height/2, Viewport: size,
	})
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: firstHandle,
		X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2, Viewport: size, Captured: true,
	})
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: firstHandle,
		X: third.Bounds.X + third.Bounds.Width/2, Y: third.Bounds.Y + third.Bounds.Height/2, Viewport: size, Captured: true,
	})

	got := make([]string, len(state.hosts))
	for index := range state.hosts {
		got[index] = state.hosts[index].Label
	}
	want := []string{"Host 01", "Host 02", "Host 00", "Host 03", "Host 04"}
	if !slices.Equal(got, want) {
		t.Fatalf("list order = %v, want %v", got, want)
	}
}

func TestHostSortDragAutoScrollsNearTheBottomEdge(t *testing.T) {
	state := manyHostsState(t, 40)
	beginHostSortCase(t, state)
	size := gfx.Size{Width: 1400, Height: 700}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	firstID := state.hosts[0].ID
	handleID := hitHostSortHandles.At(int(firstID))
	handle, ok := hitByID(&canvas, handleID)
	if !ok {
		t.Fatal("first row handle is not painted")
	}
	pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: handleID,
		X: handle.Bounds.X + handle.Bounds.Width/2, Y: handle.Bounds.Y + handle.Bounds.Height/2, Viewport: size,
	})
	result := pointer(state, gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: handleID, Captured: true,
		X:        handle.Bounds.X + handle.Bounds.Width/2,
		Y:        state.chrome.hostList.Y + state.chrome.hostList.Height - 1,
		Viewport: size,
	})
	if !result.Handled || state.hostsScroll <= 0 {
		t.Fatalf("edge drag result=%+v scroll=%v", result, state.hostsScroll)
	}
	afterPointer := state.hostsScroll

	var frame gfx.Canvas
	frame.BeginFrame()
	state.Paint(&frame, size)
	frame.EndFrame()
	if state.hostsScroll <= afterPointer {
		t.Fatalf("resting edge drag did not keep scrolling: before=%v after=%v", afterPointer, state.hostsScroll)
	}
	if _, scheduled := frame.NextFrame(); !scheduled {
		t.Fatal("resting edge drag did not schedule another frame")
	}
	state.hostSort.grid.Cancel()
}

func TestHostSortDoesNotMoveAcrossGroups(t *testing.T) {
	state := stateWithHosts(t, 4)
	state.hosts[0].Group = "A"
	state.hosts[1].Group = "B"
	state.hosts[2].Group = "A"
	state.hosts[3].Group = "B"
	state.visible = nil
	state.syncVisible()
	state.refreshHostGroups()

	source := state.hosts[0].ID
	target := state.hosts[1].ID
	move := components.ReorderGridMove{
		ID: hitHostSortHandles.At(int(source)), Target: hitHostSortHandles.At(int(target)), From: 0, To: 1,
	}
	if state.applyHostSortMove(move) {
		t.Fatal("sorting moved a host across groups")
	}
	if state.hosts[0].ID != source || state.hosts[1].ID != target {
		t.Fatal("cross-group sort changed host order")
	}
}

func TestHostSortEscapeDoesNotInterceptOutsideHosts(t *testing.T) {
	state := manyHostsState(t, 3)
	beginHostSortCase(t, state)
	state.activeSection = sectionTerminal

	result, handled := state.hostManagementKey(gfx.KeyEvent{Key: gfx.KeyEscape})
	if handled || result.Handled {
		t.Fatal("sort mode intercepted Escape outside Hosts")
	}
	if !state.hostSort.enabled {
		t.Fatal("leaving Hosts unexpectedly disabled sort mode")
	}
}

func TestHostSortEscapeReleaseAndRepeatDoNotExitAfterCancel(t *testing.T) {
	state := manyHostsState(t, 3)
	beginHostSortCase(t, state)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)

	handleID := hitHostSortHandles.At(int(state.hosts[0].ID))
	handle, ok := hitByID(&canvas, handleID)
	if !ok {
		t.Fatal("sort handle is not painted")
	}
	state.hostSort.grid.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: handleID,
		X: handle.Bounds.X + handle.Bounds.Width/2, Y: handle.Bounds.Y + handle.Bounds.Height/2,
	})
	if _, handled := state.hostManagementKey(gfx.KeyEvent{Target: handleID, Key: gfx.KeyEscape}); !handled {
		t.Fatal("Escape did not cancel held drag")
	}
	if !state.hostSort.enabled {
		t.Fatal("canceling a held drag also exited sort mode")
	}

	if _, handled := state.hostManagementKey(gfx.KeyEvent{Target: handleID, Key: gfx.KeyEscape, Release: true}); handled {
		t.Fatal("Escape release was handled as a second sort-mode action")
	}
	if _, handled := state.hostManagementKey(gfx.KeyEvent{Target: handleID, Key: gfx.KeyEscape, Repeat: true}); handled {
		t.Fatal("Escape repeat was handled as a second sort-mode action")
	}
	if !state.hostSort.enabled {
		t.Fatal("Escape release/repeat exited sort mode")
	}

	if _, handled := state.hostManagementKey(gfx.KeyEvent{Target: handleID, Key: gfx.KeyEscape}); !handled || state.hostSort.enabled {
		t.Fatal("a fresh Escape press did not leave sort mode")
	}
}

func TestHostPressDragIsUnavailableWhileSearching(t *testing.T) {
	state := manyHostsState(t, 4)
	state.search.Insert("Host 0")
	state.syncVisible()
	state.hostPressPointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitHostList.At(int(state.hosts[0].ID)), X: 10, Y: 10,
	})
	if state.hostSort.press.active || state.hostSort.enabled {
		t.Fatal("a filtered list armed the press")
	}
}
