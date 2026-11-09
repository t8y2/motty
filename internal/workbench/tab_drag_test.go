package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// threeTabs opens three sessions in three tabs and paints once, so the tab
// strip and the panes have their geometry. The last one is on show.
func threeTabs(t *testing.T) (*State, []layoutcore.WorkspaceID) {
	t.Helper()
	_, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	for index := 0; index < 3; index++ {
		state.activeSection = sectionHosts
		state.selectedHostID = state.hosts[index].ID
		openHost(state)
	}
	var ids []layoutcore.WorkspaceID
	for _, tab := range state.deck.Workspaces(nil) {
		ids = append(ids, tab.ID)
	}
	if len(ids) != 3 {
		t.Fatalf("%d tabs", len(ids))
	}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	return state, ids
}

func tabCenter(state *State, id layoutcore.WorkspaceID) gfx.Point {
	for index, tab := range state.workspaceTabs {
		if tab.ID == id {
			return gfx.Point{X: state.tabStrip.origin + float32(index)*state.tabStrip.pitch + state.tabStrip.tabWidth/2, Y: windowBarHeight / 2}
		}
	}
	return gfx.Point{}
}

func press(state *State, target gfx.ID, at gfx.Point) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: at.X, Y: at.Y, Viewport: state.viewport, ClickCount: 1})
}

func dragTo(state *State, target gfx.ID, at gfx.Point) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: at.X, Y: at.Y, Viewport: state.viewport})
}

func release(state *State, target gfx.ID, at gfx.Point) gfx.EventResult {
	return state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: target, Captured: true, X: at.X, Y: at.Y, Viewport: state.viewport, ClickCount: 1})
}

func tabOrder(state *State) []layoutcore.WorkspaceID {
	var ids []layoutcore.WorkspaceID
	for _, tab := range state.deck.Workspaces(nil) {
		ids = append(ids, tab.ID)
	}
	return ids
}

func TestAPressThatDoesNotTravelIsStillAClick(t *testing.T) {
	state, ids := threeTabs(t)
	from := tabCenter(state, ids[0])
	target := hitTabList.At(int(ids[0]))
	if result := press(state, target, from); result.Handled {
		t.Fatalf("the press was taken: %+v", result)
	}
	if result := dragTo(state, target, gfx.Point{X: from.X + 2, Y: from.Y}); result.Handled || state.tabDrag.active {
		t.Fatalf("a 2 px wobble started a drag: %+v", result)
	}
	if result := release(state, target, from); result.Handled {
		t.Fatalf("the release was taken, so the click would be lost: %+v", result)
	}
	if got := tabOrder(state); got[0] != ids[0] || len(got) != 3 {
		t.Fatalf("order %v", got)
	}
}

func TestDraggingATabAlongTheStripMovesIt(t *testing.T) {
	state, ids := threeTabs(t)
	target := hitTabList.At(int(ids[0]))
	from := tabCenter(state, ids[0])
	press(state, target, from)
	last := tabCenter(state, ids[2])
	last.X += state.tabStrip.tabWidth / 2 // past the last tab's middle: after it
	if result := dragTo(state, target, last); !result.Handled || !result.Repaint || state.tabDrag.drop.kind != dropReorder || state.tabDrag.drop.boundary != 3 {
		t.Fatalf("drag %+v, drop %+v", result, state.tabDrag.drop)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	found := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == tabDragLayer && rectangle.Bounds.Width == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("no insertion bar painted")
	}
	if result := release(state, target, last); !result.Handled {
		t.Fatal("the drop would be followed by a click")
	}
	if got := tabOrder(state); got[0] != ids[1] || got[1] != ids[2] || got[2] != ids[0] {
		t.Fatalf("order %v, want %v %v %v", got, ids[1], ids[2], ids[0])
	}
	if state.deck.ActiveWorkspace() != ids[0] || state.tabDrag.active {
		t.Fatalf("active %d, drag %+v", state.deck.ActiveWorkspace(), state.tabDrag)
	}
	saved, _, _ := loadState(state.options.StatePath)
	if saved.Deck == nil || saved.Deck.Workspaces[2].ID != ids[0] {
		t.Fatal("the new order was not saved")
	}
}

// The tabs are shorter than the window bar; a drag that wanders to its top or
// bottom edge is still over the strip and must not become a tear-off.
func TestDraggingAlongTheEdgesOfTheWindowBarStillReordersTabs(t *testing.T) {
	for _, y := range []float32{1, windowBarHeight - 1} {
		state, ids := threeTabs(t)
		target := hitTabList.At(int(ids[0]))
		press(state, target, tabCenter(state, ids[0]))
		last := tabCenter(state, ids[2])
		last.X += state.tabStrip.tabWidth / 2
		last.Y = y
		dragTo(state, target, last)
		if state.tabDrag.drop.kind != dropReorder || state.tabDrag.drop.boundary != 3 {
			t.Fatalf("y=%v: drop %+v", y, state.tabDrag.drop)
		}
		release(state, target, last)
		if got := tabOrder(state); got[2] != ids[0] {
			t.Fatalf("y=%v: order %v", y, got)
		}
	}
}

func TestDroppingATabBesideItselfChangesNothing(t *testing.T) {
	state, ids := threeTabs(t)
	target := hitTabList.At(int(ids[1]))
	from := tabCenter(state, ids[1])
	press(state, target, from)
	dragTo(state, target, gfx.Point{X: from.X + 30, Y: from.Y})
	if state.tabDrag.drop.kind != dropNone {
		t.Fatalf("drop %+v", state.tabDrag.drop)
	}
	release(state, target, from)
	if got := tabOrder(state); got[0] != ids[0] || got[1] != ids[1] || got[2] != ids[2] {
		t.Fatalf("order %v", got)
	}
}

func TestDraggingATabOntoAPaneEdgeDocksItThere(t *testing.T) {
	state, ids := threeTabs(t) // ids[2] is on show
	dragged := ids[0]
	sessionOf := func(id layoutcore.WorkspaceID) layoutcore.SessionID {
		return state.deck.Panes(id, nil)[0].SessionID
	}
	moved, shown := sessionOf(dragged), sessionOf(ids[2])
	target := hitTabList.At(int(dragged))
	press(state, target, tabCenter(state, dragged))
	frame := state.workspacePixelBounds(state.viewport)

	// The middle of the pane is not an edge.
	dragTo(state, target, gfx.Point{X: frame.X + frame.Width/2, Y: frame.Y + frame.Height/2})
	if state.tabDrag.drop.kind != dropNone {
		t.Fatalf("the middle is a drop: %+v", state.tabDrag.drop)
	}
	// The left edge docks to the left: the dragged tab's pane comes first.
	at := gfx.Point{X: frame.X + frame.Width*0.1, Y: frame.Y + frame.Height/2}
	dragTo(state, target, at)
	drop := state.tabDrag.drop
	if drop.kind != dropDock || drop.edge != dockLeft || drop.zone.X != frame.X || drop.zone.Width != frame.Width/2 {
		t.Fatalf("drop %+v", drop)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	highlighted := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == tabDragLayer && rectangle.Bounds == drop.zone && rectangle.Color.Alpha < 1 {
			highlighted = true
		}
	}
	if !highlighted {
		t.Fatal("the half the dock would take is not highlighted")
	}
	release(state, target, at)

	if got := tabOrder(state); len(got) != 2 || got[0] != ids[1] || got[1] != ids[2] {
		t.Fatalf("tabs after the dock: %v", got)
	}
	panes := state.deck.Panes(ids[2], nil)
	if len(panes) != 2 || panes[0].SessionID != moved || panes[1].SessionID != shown {
		t.Fatalf("panes %+v, want %d then %d", panes, moved, shown)
	}
	if state.deck.ActiveWorkspace() != ids[2] || state.activeSessionID != uint32(moved) {
		t.Fatalf("active workspace %d session %d", state.deck.ActiveWorkspace(), state.activeSessionID)
	}
	if _, ok := state.sessionByIDPointer(uint32(moved)); !ok {
		t.Fatal("docking lost the session")
	}
	var canvasAfter gfx.Canvas
	state.Paint(&canvasAfter, gfx.Size{Width: 1400, Height: 900})
	saved, _, _ := loadState(state.options.StatePath)
	if saved.Deck == nil || len(saved.Deck.Workspaces) != 2 {
		t.Fatal("the dock was not saved")
	}
}

func TestEscapeCancelsADragAndNothingChanges(t *testing.T) {
	state, ids := threeTabs(t)
	target := hitTabList.At(int(ids[0]))
	from := tabCenter(state, ids[0])
	press(state, target, from)
	dragTo(state, target, tabCenter(state, ids[2]))
	if !state.tabDrag.active {
		t.Fatal("no drag")
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled || state.tabDrag.active {
		t.Fatalf("escape: %+v, drag %+v", result, state.tabDrag)
	}
	release(state, target, tabCenter(state, ids[2]))
	if got := tabOrder(state); got[0] != ids[0] {
		t.Fatalf("order %v", got)
	}
}

func TestACrowdedTabCannotBeDockedPastTheLimit(t *testing.T) {
	state, ids := threeTabs(t)
	for state.deck.CanDock(ids[2], ids[0]) && len(state.deck.Panes(ids[2], nil)) < layoutcore.MaxPanes-1 {
		if _, err := state.deck.Split(ids[2], state.deck.Panes(ids[2], nil)[0].ID, layoutcore.Horizontal); err != nil {
			t.Fatal(err)
		}
	}
	state.deck.ActivateWorkspace(ids[2])
	if _, err := state.deck.Split(ids[2], state.deck.Panes(ids[2], nil)[0].ID, layoutcore.Horizontal); err != nil {
		t.Fatal(err)
	}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	target := hitTabList.At(int(ids[0]))
	press(state, target, tabCenter(state, ids[0]))
	frame := state.workspacePixelBounds(state.viewport)
	dragTo(state, target, gfx.Point{X: frame.X + 5, Y: frame.Y + frame.Height/2})
	if state.tabDrag.drop.kind != dropNone {
		t.Fatalf("a full workspace takes a drop: %+v", state.tabDrag.drop)
	}
}

func TestDraggingAPaneBarToTheTabsPullsItOutAsATab(t *testing.T) {
	state, ids := threeTabs(t)
	frame := state.workspacePixelBounds(state.viewport)
	// Dock tab 0 beside the tab on show so the workspace is split.
	target := hitTabList.At(int(ids[0]))
	press(state, target, tabCenter(state, ids[0]))
	at := gfx.Point{X: frame.X + frame.Width*0.1, Y: frame.Y + frame.Height/2}
	dragTo(state, target, at)
	release(state, target, at)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	panes := state.deck.Panes(ids[2], nil)
	if len(panes) != 2 {
		t.Fatalf("panes %+v", panes)
	}
	moved := panes[0].SessionID

	state.refreshWorkspaceViews()
	bar := pixelBounds(frame, state.panes[0].Bounds)
	from := gfx.Point{X: bar.X + bar.Width/2, Y: bar.Y + infoBarHeight/2}
	ground := hitPaneActivateList.At(0)
	press(state, ground, from)
	// A drag over the terminal is not a drop.
	dragTo(state, ground, gfx.Point{X: bar.X + bar.Width/2, Y: bar.Y + bar.Height/2})
	if !state.tabDrag.active || state.tabDrag.drop.kind != dropNone {
		t.Fatalf("drag %+v", state.tabDrag)
	}
	end := gfx.Point{X: state.tabStrip.origin + state.tabStrip.pitch*0.5, Y: windowBarHeight / 2}
	dragTo(state, ground, end)
	if state.tabDrag.drop.kind != dropReorder {
		t.Fatalf("no drop among the tabs: %+v", state.tabDrag.drop)
	}
	release(state, ground, end)

	if got := state.deck.Panes(ids[2], nil); len(got) != 1 {
		t.Fatalf("source keeps %+v", got)
	}
	workspace, _, ok := state.deck.PaneForSession(moved)
	if !ok || workspace == ids[2] || state.deck.ActiveWorkspace() != workspace {
		t.Fatalf("session in tab %d (ok %v), active %d", workspace, ok, state.deck.ActiveWorkspace())
	}
	if state.activeSessionID != uint32(moved) {
		t.Fatalf("active session %d, want %d", state.activeSessionID, moved)
	}
	if _, ok := state.sessionByIDPointer(uint32(moved)); !ok {
		t.Fatal("the session was lost")
	}
}
