package workbench

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// A tabbed workspace shows one pane at a time: the active pane takes the whole
// area, every other pane has nothing to draw, and all of them are sized for the
// whole area so that switching tabs never resizes a terminal.
func TestTabbedWorkspaceShowsOnlyTheActivePane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.openWorkspaceArrangement()
	state.workspaceLayoutDraft.layout = layoutcore.LayoutSpec{Kind: layoutcore.LayoutTabs}
	state.applyWorkspaceArrangement()
	state.refreshWorkspaceViews()

	showing := 0
	for _, pane := range state.panes {
		if pane.Bounds.Width > 0 && pane.Bounds.Height > 0 {
			showing++
			if !pane.Active || pane.Bounds != (FractionBounds{Width: 1, Height: 1}) {
				t.Fatalf("showing pane %+v is not the active one filling the area", pane)
			}
		}
	}
	if showing != 1 {
		t.Fatalf("%d panes showing, want 1", showing)
	}
	if len(state.paneSplits) != 0 {
		t.Fatalf("a tabbed workspace has dividers: %+v", state.paneSplits)
	}
	for _, pane := range state.panes {
		fraction, _, ok := state.paneFractionBounds(workspace, pane.ID)
		if !ok || fraction != (FractionBounds{Width: 1, Height: 1}) {
			t.Fatalf("pane %d is sized %+v, want the whole area", pane.ID, fraction)
		}
	}

	hidden := state.panes[0]
	if hidden.Active {
		hidden = state.panes[1]
	}
	state.Click(hitWorkspacePaneTabs.At(slices.IndexFunc(state.panes, func(p Pane) bool { return p.ID == hidden.ID })))
	state.refreshWorkspaceViews()
	if state.deck.ActivePane(workspace) != hidden.ID {
		t.Fatalf("clicking the tab made pane %d active, want %d", state.deck.ActivePane(workspace), hidden.ID)
	}
	for _, pane := range state.panes {
		if (pane.Bounds.Width > 0) != (pane.ID == hidden.ID) {
			t.Fatalf("after switching, pane %d has bounds %+v", pane.ID, pane.Bounds)
		}
	}
}

// Dropping a card on another card's edge redraws the layout around the cards,
// which then no longer matches any preset, while every session stays put.
func TestDockingACardDrawsACustomLayout(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	sessions, dials := paneSessionIDs(state.deck.Panes(workspace, nil)), len(world.configs)
	state.openWorkspaceArrangement()
	source := workspaceCardRegion{view: workspaceCardsRuntime, index: 0}
	state.dockWorkspaceCard(source, 2, zoneBottom)

	spec := state.managedWorkspaceLayouts[workspace]
	if spec.Kind != layoutcore.LayoutCustom {
		t.Fatalf("layout = %+v, want a custom one", spec)
	}
	if got := paneSessionIDs(state.deck.Panes(workspace, nil)); !slices.Equal(sortedCopy(got), sortedCopy(sessions)) {
		t.Fatalf("sessions %v became %v", sessions, got)
	}
	if len(world.configs) != dials {
		t.Fatal("redrawing the layout dialed again")
	}
	if workspacePresetIndex(state.workspaceLayoutSpec(workspace)) != -1 {
		t.Fatal("a drawn layout is offered as a preset")
	}
	// The drawn layout is what a reopened editor starts from.
	state.openWorkspaceArrangement()
	if !state.workspaceLayoutDraft.layout.Equal(spec) {
		t.Fatalf("editor opened with %+v, want %+v", state.workspaceLayoutDraft.layout, spec)
	}
}

func sortedCopy(values []uint32) []uint32 {
	values = slices.Clone(values)
	slices.Sort(values)
	return values
}

// The part of a card a drag is over decides between swapping and docking, and
// the edges are the docking sides.
func TestCardZoneFollowsTheNearestEdge(t *testing.T) {
	box := gfx.Rect{X: 100, Y: 50, Width: 200, Height: 100}
	for _, test := range []struct {
		at   gfx.Point
		want workspaceCardZone
	}{
		{gfx.Point{X: 200, Y: 100}, zoneSwap},
		{gfx.Point{X: 104, Y: 100}, zoneLeft},
		{gfx.Point{X: 296, Y: 100}, zoneRight},
		{gfx.Point{X: 200, Y: 52}, zoneTop},
		{gfx.Point{X: 200, Y: 148}, zoneBottom},
	} {
		if got := workspaceCardZoneAt(box, test.at); got != test.want {
			t.Errorf("zone at %+v = %d, want %d", test.at, got, test.want)
		}
	}
}

// Templates written before layouts existed stored a grid width in "columns".
// They load as that grid and are written back in the new form only.
func TestTemplateWithOnlyColumnsLoadsAsAGridAndSavesAsLayout(t *testing.T) {
	var old WorkspaceTemplate
	raw := `{"id":"ws_0123456789abcdef0123456789abcdef","name":"Old","host_ids":["host_a","host_b","host_c"],"columns":3}`
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		t.Fatal(err)
	}
	template, err := normalizeWorkspaceTemplate(old)
	if err != nil {
		t.Fatal(err)
	}
	if !isGrid(template.Layout, 3) {
		t.Fatalf("layout = %+v, want 3 columns", template.Layout)
	}
	out, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(out, &fields); err != nil {
		t.Fatal(err)
	}
	if _, stale := fields["columns"]; stale || fields["layout"] == nil {
		t.Fatalf("saved form = %s, want layout and no columns", out)
	}
}

// A template whose layout cannot describe its hosts is rejected rather than
// launched into a workspace that shows some of them twice or not at all.
func TestTemplateRejectsLayoutOfAnotherSize(t *testing.T) {
	two, err := layoutcore.NewLayout([]layoutcore.SessionID{1, 2}, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	custom := two.Spec()
	if _, err := NewWorkspaceTemplate("Mismatch", []string{"host_a", "host_b", "host_c"}, custom); err != ErrWorkspaceTemplateLayout {
		t.Fatalf("error = %v, want %v", err, ErrWorkspaceTemplateLayout)
	}
	if _, err := NewWorkspaceTemplate("Fits", []string{"host_a", "host_b"}, custom); err != nil {
		t.Fatal(err)
	}
}

// Dragging a pane's bar onto the edge of another pane of the same workspace
// docks it there: the tree changes, the session stays bound to its pane, and
// the editor then offers the result as a drawn layout rather than a preset.
func TestDraggingAPaneBarDocksItBesideAnotherPane(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.fitTerminals()
	dials := len(world.configs)
	before := state.deck.Panes(workspace, nil)
	source, target := before[0], before[2]
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(state.viewport)
	var box gfx.Rect
	for _, pane := range state.panes {
		if pane.ID == target.ID {
			box = pixelBounds(frame, pane.Bounds)
		}
	}
	at := gfx.Point{X: box.X + box.Width/2, Y: box.Y + box.Height - 4}
	drop := state.paneDropAt(workspace, source.ID, at)
	if drop.kind != dropDock || drop.pane != target.ID || drop.edge != dockBottom {
		t.Fatalf("drop = %+v, want a bottom dock on pane %d", drop, target.ID)
	}
	if own := state.paneDropAt(workspace, source.ID, gfx.Point{X: frame.X + 4, Y: frame.Y + frame.Height/2}); own.kind == dropDock && own.pane == source.ID {
		t.Fatal("a pane can be docked against itself")
	}
	state.movePaneBeside(workspace, source.ID, drop)

	after := state.deck.Panes(workspace, nil)
	if len(after) != 3 || len(world.configs) != dials {
		t.Fatalf("panes = %+v, dials %d -> %d", after, dials, len(world.configs))
	}
	for _, pane := range before {
		now, ok := state.deck.Pane(workspace, pane.ID)
		if !ok || now.SessionID != pane.SessionID {
			t.Fatalf("pane %d lost its session: %+v", pane.ID, now)
		}
	}
	if workspacePresetIndex(state.workspaceLayoutSpec(workspace)) != -1 {
		t.Fatal("a pane dragged into place still reads as a preset")
	}
}

func TestPaneBarDragThroughThePointerDocks(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.viewport = gfx.Size{Width: 1400, Height: 900}
	workspace := launchTemporaryWorkspace(t, state, 0, 1)
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(state.viewport)
	first, second := state.panes[0], state.panes[1]
	a, b := pixelBounds(frame, first.Bounds), pixelBounds(frame, second.Bounds)
	target := hitPaneActivateList.At(0)
	down := gfx.Point{X: a.X + 40, Y: a.Y + 10}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: target, X: down.X, Y: down.Y, Viewport: state.viewport, ClickCount: 1})
	end := gfx.Point{X: b.X + b.Width - 6, Y: b.Y + b.Height/2}
	for _, p := range []gfx.Point{{X: down.X + 30, Y: down.Y + 30}, end} {
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: target, Captured: true, X: p.X, Y: p.Y, Viewport: state.viewport})
	}
	if state.tabDrag.drop.kind != dropDock {
		t.Fatalf("drop = %+v, drag %+v", state.tabDrag.drop, state.tabDrag)
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: target, Captured: true, X: end.X, Y: end.Y, Viewport: state.viewport, ClickCount: 1})
	panes := state.deck.Panes(workspace, nil)
	if panes[0].ID != second.ID {
		t.Fatalf("panes after drop = %+v, want pane %d first", panes, second.ID)
	}
}

// Dropping a pane on the middle of another trades their places.
func TestPaneBarDroppedOnAPaneCenterSwapsThem(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.viewport = gfx.Size{Width: 1400, Height: 900}
	workspace := launchTemporaryWorkspace(t, state, 0, 1)
	before := state.deck.Panes(workspace, nil)
	state.refreshWorkspaceViews()
	b := pixelBounds(state.workspacePixelBounds(state.viewport), state.panes[1].Bounds)
	drop := state.paneDropAt(workspace, before[0].ID, gfx.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2})
	if drop.kind != dropDock || drop.edge != dockCenter {
		t.Fatalf("drop = %+v, want a swap", drop)
	}
	state.movePaneBeside(workspace, before[0].ID, drop)
	after := state.deck.Panes(workspace, nil)
	if after[0].SessionID != before[1].SessionID || after[1].SessionID != before[0].SessionID {
		t.Fatalf("before %+v after %+v", before, after)
	}
}
