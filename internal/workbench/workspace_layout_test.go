package workbench

import (
	"reflect"
	"testing"

	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceOpenCloseSynchronizesLayout(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	firstWorkspace := state.deck.ActiveWorkspace()
	assertPanes(t, state, []Pane{{
		SessionID: state.sessions[0].ID,
		Bounds:    FractionBounds{Width: 1, Height: 1},
		Active:    true,
	}})

	state.Click(hitHosts)
	state.Click(hitHostList.At(2))
	openHost(state)
	secondWorkspace := state.deck.ActiveWorkspace()
	if firstWorkspace == secondWorkspace || len(state.workspaceTabs) != 2 {
		t.Fatalf("workspace tabs = %+v", state.workspaceTabs)
	}
	second := state.sessions[1].ID
	assertPanes(t, state, []Pane{{SessionID: second, Bounds: FractionBounds{Width: 1, Height: 1}, Active: true}})

	state.closeWorkspace(firstWorkspace)
	assertPanes(t, state, []Pane{{
		SessionID: second,
		Bounds:    FractionBounds{Width: 1, Height: 1},
		Active:    true,
	}})
	if len(state.workspaceTabs) != 1 {
		t.Fatalf("remaining workspaces = %+v", state.workspaceTabs)
	}
}

func TestWorkspaceSplitMoveRatioAndActivePane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	openHostInSplit(t, state, 3, layoutcore.Horizontal)
	first, second, third := state.sessions[0].ID, state.sessions[1].ID, state.sessions[2].ID

	if err := state.SplitPane(first, third, layoutcore.Vertical, layoutcore.Before); err != nil {
		t.Fatal(err)
	}
	assertPanes(t, state, []Pane{
		{SessionID: third, Bounds: FractionBounds{Width: 0.5, Height: 0.5}, Active: true},
		{SessionID: first, Bounds: FractionBounds{Y: 0.5, Width: 0.5, Height: 0.5}},
		{SessionID: second, Bounds: FractionBounds{X: 0.5, Width: 0.5, Height: 1}},
	})

	if err := state.MovePane(second, third, layoutcore.Horizontal, 0.25, layoutcore.Before); err != nil {
		t.Fatal(err)
	}
	assertPanes(t, state, []Pane{
		{SessionID: second, Bounds: FractionBounds{Width: 0.25, Height: 0.5}, Active: true},
		{SessionID: third, Bounds: FractionBounds{X: 0.25, Width: 0.75, Height: 0.5}},
		{SessionID: first, Bounds: FractionBounds{Y: 0.5, Width: 1, Height: 0.5}},
	})

	var horizontal PaneSplit
	for _, split := range state.PaneSplits(nil) {
		if split.Axis == layoutcore.Horizontal {
			horizontal = split
		}
	}
	if horizontal.NodeID == 0 {
		t.Fatal("horizontal split not exposed")
	}
	if err := state.SetPaneRatio(horizontal.NodeID, 0.4); err != nil {
		t.Fatal(err)
	}
	panes := state.Panes(nil)
	if panes[0].Bounds.Width != 0.4 || panes[1].Bounds.X != 0.4 || panes[1].Bounds.Width != 0.6 {
		t.Fatalf("ratio panes = %+v", panes)
	}
	if !state.ActivatePane(first) || state.activeSessionID != first || !state.Panes(nil)[2].Active {
		t.Fatalf("active pane = %d panes=%+v", state.activeSessionID, state.Panes(nil))
	}
	if state.ActivatePane(999) {
		t.Fatal("activated missing pane")
	}
	_, layout := state.activeWorkspaceLayout()
	if err := layout.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceLayoutRoundTrip(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	openHostInSplit(t, first, 3, layoutcore.Horizontal)
	ids := []uint32{first.sessions[0].ID, first.sessions[1].ID, first.sessions[2].ID}
	if err := first.SplitPane(ids[0], ids[2], layoutcore.Vertical, layoutcore.Before); err != nil {
		t.Fatal(err)
	}
	if err := first.MovePane(ids[1], ids[2], layoutcore.Horizontal, 0.3, layoutcore.After); err != nil {
		t.Fatal(err)
	}
	if !first.ActivatePane(ids[0]) {
		t.Fatal("activate first pane")
	}
	wantPanes := append([]Pane(nil), first.Panes(nil)...)
	wantDeck := first.savedWorkspaceDeck()
	first.save()

	second := newTestState(t, options)
	assertPanes(t, second, wantPanes)
	if second.activeSessionID != ids[0] {
		t.Fatalf("active pane = %d, want %d", second.activeSessionID, ids[0])
	}
	if gotDeck := second.savedWorkspaceDeck(); !reflect.DeepEqual(gotDeck, wantDeck) {
		t.Fatalf("deck roundtrip = %+v, want %+v", gotDeck, wantDeck)
	}
}

func TestMultipleWorkspacesRestoreIndependentTreesContextAndDoNotDial(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	production := first.deck.ActiveWorkspace()
	first.Click(hitPaneSplitHorizontal) // opens a second shell on the same host
	if panes := first.deck.Panes(production, nil); len(panes) != 2 || panes[1].SessionID == 0 {
		t.Fatalf("production panes = %+v", panes)
	}

	openEmptyTab(first)
	database := first.deck.ActiveWorkspace()
	first.Click(hitPaneChooseHost)
	first.Click(hitHostList.At(2))
	openHost(first)
	if database == production || len(world.conns) != 3 { // two production shells, one database
		t.Fatalf("workspaces=%d/%d dials=%d", production, database, len(world.conns))
	}
	if err := first.deck.SetContext(database, layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 404}); err != nil {
		t.Fatal(err)
	}
	first.save()

	restored := newTestState(t, options)
	if len(world.conns) != 3 {
		t.Fatalf("restore dialed %d extra connections", len(world.conns)-3)
	}
	workspaces := restored.deck.Workspaces(nil)
	if len(workspaces) != 2 || len(restored.deck.Panes(production, nil)) != 2 || len(restored.deck.Panes(database, nil)) != 1 {
		t.Fatalf("restored workspaces=%+v production=%+v database=%+v", workspaces, restored.deck.Panes(production, nil), restored.deck.Panes(database, nil))
	}
	context, ok := restored.deck.Context(database)
	if !ok || context != (layoutcore.ContextState{Kind: layoutcore.ContextSFTP, Open: true, Width: 404}) {
		t.Fatalf("restored context = %+v, %v", context, ok)
	}
	for index := range restored.sessions {
		if restored.sessions[index].conn != nil || restored.sessions[index].phase != sshconn.PhaseClosed || !restored.sessions[index].restored {
			t.Fatalf("session %d reconnected: %+v", index, restored.sessions[index])
		}
	}
}

func TestCorruptDeckDegradesToSafeDisconnectedPanes(t *testing.T) {
	world, options := newWorld(t)
	invalid := layoutcore.DeckDTO{
		Version: layoutcore.DeckDTOVersion, ActiveWorkspace: 1,
		Workspaces: []layoutcore.WorkspaceDTO{{
			ID: 1, Title: "Broken", ActivePane: 1,
			Layout:  layoutcore.DTO{Version: layoutcore.DTOVersion, Root: 1, Nodes: []layoutcore.NodeDTO{{ID: 1, Kind: layoutcore.KindLeaf, Session: 1}}},
			Panes:   []layoutcore.PaneDTO{{ID: 1, SessionID: 1}},
			Context: layoutcore.ContextStateDTO{Kind: layoutcore.ContextSFTP, Open: true, Width: 900},
		}},
	}
	if err := saveState(options.StatePath, savedState{
		Workspace: "terminal", Sessions: []savedSession{{Label: "Safe", Address: "safe.example.com", Port: 22}}, Deck: &invalid,
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(world.conns) != 0 || len(state.sessions) != 1 || state.sessions[0].conn != nil {
		t.Fatalf("corrupt restore sessions=%+v dials=%d", state.sessions, len(world.conns))
	}
	if err := state.deck.Validate(); err != nil {
		t.Fatalf("fallback deck is invalid: %v", err)
	}
	panes := state.Panes(nil)
	if len(panes) != 1 || panes[0].SessionID != state.sessions[0].ID {
		t.Fatalf("fallback panes = %+v", panes)
	}
}

func TestWorkspaceTraversalDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	openHostInSplit(t, state, 3, layoutcore.Vertical)
	panes := make([]Pane, 0, len(state.sessions))
	splits := make([]PaneSplit, 0, len(state.sessions)-1)
	panes = state.Panes(panes)
	splits = state.PaneSplits(splits)
	allocations := testing.AllocsPerRun(100, func() {
		panes = state.Panes(panes[:0])
		splits = state.PaneSplits(splits[:0])
	})
	if allocations != 0 {
		t.Fatalf("pane traversal allocations = %v", allocations)
	}
}

func assertPanes(t *testing.T, state *State, want []Pane) {
	t.Helper()
	got := state.Panes(make([]Pane, 0, len(want)))
	if len(got) != len(want) {
		t.Fatalf("panes = %+v, want %+v", got, want)
	}
	for index := range got {
		gotPane, wantPane := got[index], want[index]
		if gotPane.SessionID != wantPane.SessionID || gotPane.Active != wantPane.Active ||
			!fractionClose(gotPane.Bounds.X, wantPane.Bounds.X) ||
			!fractionClose(gotPane.Bounds.Y, wantPane.Bounds.Y) ||
			!fractionClose(gotPane.Bounds.Width, wantPane.Bounds.Width) ||
			!fractionClose(gotPane.Bounds.Height, wantPane.Bounds.Height) {
			t.Fatalf("pane %d = %+v, want %+v", index, gotPane, wantPane)
		}
	}
}

func fractionClose(first, second float32) bool {
	difference := first - second
	if difference < 0 {
		difference = -difference
	}
	return difference < 0.000001
}
