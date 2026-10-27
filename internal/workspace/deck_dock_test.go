package workspace

import (
	"errors"
	"slices"
	"testing"
)

func deckWith(t *testing.T, panes ...int) (*Deck, []WorkspaceID, SessionID) {
	t.Helper()
	deck := &Deck{}
	next := SessionID(1)
	var ids []WorkspaceID
	for index, count := range panes {
		id, pane, err := deck.Create(string(rune('A' + index)))
		if err != nil {
			t.Fatal(err)
		}
		if err := deck.Bind(id, pane, next); err != nil {
			t.Fatal(err)
		}
		next++
		last := pane
		for extra := 1; extra < count; extra++ {
			last, err = deck.Split(id, last, Horizontal)
			if err != nil {
				t.Fatal(err)
			}
			if err := deck.Bind(id, last, next); err != nil {
				t.Fatal(err)
			}
			next++
		}
		ids = append(ids, id)
	}
	return deck, ids, next
}

func titles(deck *Deck) string {
	var out string
	for _, workspace := range deck.Workspaces(nil) {
		out += workspace.Title
	}
	return out
}

func TestMoveWorkspaceReordersTabsAndKeepsTheActiveOne(t *testing.T) {
	deck, ids, _ := deckWith(t, 1, 1, 1, 1)
	deck.ActivateWorkspace(ids[1])
	for _, test := range []struct {
		id    WorkspaceID
		index int
		want  string
	}{
		{ids[0], 2, "BCAD"}, {ids[3], 0, "DBCA"}, {ids[1], 99, "DCAB"}, {ids[1], -4, "BDCA"}, {ids[1], 0, "BDCA"},
	} {
		if err := deck.MoveWorkspace(test.id, test.index); err != nil || titles(deck) != test.want {
			t.Fatalf("move %d to %d: %q, %v; want %q", test.id, test.index, titles(deck), err, test.want)
		}
	}
	if deck.ActiveWorkspace() != ids[1] {
		t.Fatalf("active %d", deck.ActiveWorkspace())
	}
	if err := deck.MoveWorkspace(999, 0); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatal(err)
	}
	if err := deck.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDockMovesEveryPaneBesideTheTargetAndRemovesTheTab(t *testing.T) {
	deck, ids, _ := deckWith(t, 2, 3)
	target := deck.Panes(ids[0], nil)[0].ID
	moved, err := deck.Dock(ids[0], ids[1], target, Horizontal, Before)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(moved, []SessionID{3, 4, 5}) {
		t.Fatalf("moved %v", moved)
	}
	if titles(deck) != "A" || deck.ActiveWorkspace() != ids[0] {
		t.Fatalf("tabs %q active %d", titles(deck), deck.ActiveWorkspace())
	}
	var order []SessionID
	for _, pane := range deck.Panes(ids[0], nil) {
		order = append(order, pane.SessionID)
	}
	if !slices.Equal(order, []SessionID{3, 4, 5, 1, 2}) {
		t.Fatalf("visual order %v", order)
	}
	if active := deck.ActivePane(ids[0]); active != deck.Panes(ids[0], nil)[0].ID {
		t.Fatalf("active pane %d is not the first docked", active)
	}
	if err := deck.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDockAfterAndVerticalPlaceTheTabOnTheOtherSides(t *testing.T) {
	deck, ids, _ := deckWith(t, 1, 2)
	target := deck.Panes(ids[0], nil)[0].ID
	if _, err := deck.Dock(ids[0], ids[1], target, Vertical, After); err != nil {
		t.Fatal(err)
	}
	var order []SessionID
	for _, pane := range deck.Panes(ids[0], nil) {
		order = append(order, pane.SessionID)
	}
	if !slices.Equal(order, []SessionID{1, 2, 3}) {
		t.Fatalf("order %v", order)
	}
	layout, _ := deck.Layout(ids[0])
	if root := layout.Root(); root == 0 {
		t.Fatal("no layout")
	}
}

func TestDockRefusesWhatDoesNotFitOrMakesNoSense(t *testing.T) {
	deck, ids, _ := deckWith(t, 5, 4)
	target := deck.Panes(ids[0], nil)[0].ID
	if deck.CanDock(ids[0], ids[1]) {
		t.Fatal("nine panes fit in a workspace of eight")
	}
	before := deck.DTO()
	if _, err := deck.Dock(ids[0], ids[1], target, Horizontal, After); !errors.Is(err, ErrPaneLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if _, err := deck.Dock(ids[0], ids[0], target, Horizontal, After); !errors.Is(err, ErrDockSelf) {
		t.Fatalf("into itself: %v", err)
	}
	if _, err := deck.Dock(ids[0], 99, target, Horizontal, After); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("missing source: %v", err)
	}
	if _, err := deck.Dock(ids[0], ids[1], 999, Horizontal, After); !errors.Is(err, ErrPaneNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	if after := deck.DTO(); len(after.Workspaces) != len(before.Workspaces) || titles(deck) != "AB" {
		t.Fatal("a refused dock changed the deck")
	}
}

func TestInsertPutsAPaneBeforeItsTarget(t *testing.T) {
	deck, ids, next := deckWith(t, 2)
	target := deck.Panes(ids[0], nil)[0].ID
	pane, err := deck.Insert(ids[0], target, Horizontal, 0.3, Before)
	if err != nil {
		t.Fatal(err)
	}
	if err := deck.Bind(ids[0], pane, next); err != nil {
		t.Fatal(err)
	}
	if got := deck.Panes(ids[0], nil)[0]; got.ID != pane {
		t.Fatalf("first pane %+v, want %d", got, pane)
	}
	if err := deck.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestExportThenImportMovesATabBetweenDecksWithFreshIdentifiers(t *testing.T) {
	source, ids, _ := deckWith(t, 3, 1)
	target, targetIDs, _ := deckWith(t, 2)
	if err := source.SetContext(ids[0], ContextState{Kind: ContextSFTP, Open: true, Width: 404, Side: ContextRight}); err != nil {
		t.Fatal(err)
	}
	dto, ok := source.ExportWorkspace(ids[0])
	if !ok {
		t.Fatal("no export")
	}
	remap := map[SessionID]SessionID{1: 101, 2: 102, 3: 103}
	id, err := target.Import(dto, remap, 0)
	if err != nil {
		t.Fatal(err)
	}
	if titles(target) != "AA" || target.ActiveWorkspace() != id || id == targetIDs[0] {
		t.Fatalf("tabs %q active %d id %d (existing %d)", titles(target), target.ActiveWorkspace(), id, targetIDs[0])
	}
	var got []SessionID
	for _, pane := range target.Panes(id, nil) {
		got = append(got, pane.SessionID)
		for _, existing := range target.Panes(targetIDs[0], nil) {
			if existing.ID == pane.ID {
				t.Fatalf("pane identifier %d collides with the tab already here", pane.ID)
			}
		}
	}
	if !slices.Equal(got, []SessionID{101, 102, 103}) {
		t.Fatalf("sessions %v", got)
	}
	if context, ok := target.Context(id); !ok || context != (ContextState{Kind: ContextSFTP, Open: true, Width: 404, Side: ContextRight}) {
		t.Fatalf("context %+v, %t", context, ok)
	}
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
	// Placed in the middle, then docked into: the imported tab behaves like any.
	again, _ := source.ExportWorkspace(ids[1])
	middle, err := target.Import(again, map[SessionID]SessionID{4: 104}, 1)
	if err != nil {
		t.Fatal(err)
	}
	tabs := target.Workspaces(nil)
	if len(tabs) != 3 || tabs[1].ID != middle {
		t.Fatalf("tabs %+v", tabs)
	}
	if _, err := target.Dock(id, middle, target.Panes(id, nil)[0].ID, Vertical, After); err != nil {
		t.Fatal(err)
	}
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestImportRefusesASessionThatIsAlreadyHereAndABrokenTab(t *testing.T) {
	source, ids, _ := deckWith(t, 1)
	target, _, _ := deckWith(t, 1)
	dto, _ := source.ExportWorkspace(ids[0])
	if _, err := target.Import(dto, map[SessionID]SessionID{1: 1}, 0); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("a duplicate session: %v", err)
	}
	broken := dto
	broken.Panes = nil
	if _, err := target.Import(broken, nil, 0); err == nil {
		t.Fatal("a tab whose layout has no panes was imported")
	}
	if titles(target) != "A" {
		t.Fatal("a refused import changed the deck")
	}
	empty := dto
	if id, err := target.Import(empty, map[SessionID]SessionID{}, 5); err != nil || target.Panes(id, nil)[0].SessionID != 0 {
		t.Fatalf("unmapped sessions leave panes empty: %v", err)
	}
}
