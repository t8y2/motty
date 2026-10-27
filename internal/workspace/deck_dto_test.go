package workspace

import (
	"errors"
	"reflect"
	"testing"
)

func TestDeckDTORoundTripPreservesTabsTreesEmptyPanesAndBindings(t *testing.T) {
	var deck Deck
	production, first, _ := deck.Create("Production")
	second, _ := deck.Split(production, first, Horizontal)
	_, _ = deck.Split(production, second, Vertical)
	if err := deck.Bind(production, first, 10); err != nil {
		t.Fatal(err)
	}
	database, databasePane, _ := deck.Create("Database")
	if err := deck.Bind(database, databasePane, 20); err != nil {
		t.Fatal(err)
	}
	deck.ActivateWorkspace(production)
	deck.ActivatePane(production, second)
	if err := deck.SetContext(production, ContextState{Kind: ContextSFTP, Open: true, Width: 412, Side: ContextRight}); err != nil {
		t.Fatal(err)
	}

	want := deck.DTO()
	restored, err := RestoreDeck(want)
	if err != nil {
		t.Fatal(err)
	}
	if got := restored.DTO(); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if context, ok := restored.Context(production); !ok || context != (ContextState{Kind: ContextSFTP, Open: true, Width: 412, Side: ContextRight}) {
		t.Fatalf("restored context = %+v, %v", context, ok)
	}
}

func TestDeckDTORejectsCorruptionWithoutPartialRestore(t *testing.T) {
	valid := DeckDTO{
		Version: DeckDTOVersion, ActiveWorkspace: 1,
		Workspaces: []WorkspaceDTO{{
			ID: 1, Title: "One", ActivePane: 1,
			Layout: DTO{Version: DTOVersion, Root: 1, Nodes: []NodeDTO{{ID: 1, Kind: KindLeaf, Session: 1}}},
			Panes:  []PaneDTO{{ID: 1, SessionID: 7}},
		}},
	}
	tests := []struct {
		name string
		dto  DeckDTO
	}{
		{"version", DeckDTO{Version: 9}},
		{"missing active workspace", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 2, Workspaces: valid.Workspaces}},
		{"missing active pane", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{{ID: 1, ActivePane: 2, Layout: valid.Workspaces[0].Layout, Panes: valid.Workspaces[0].Panes}}}},
		{"pane absent from tree", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{{ID: 1, ActivePane: 2, Layout: valid.Workspaces[0].Layout, Panes: []PaneDTO{{ID: 2}}}}}},
		{"duplicate workspace", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{valid.Workspaces[0], valid.Workspaces[0]}}},
		{"open context without kind", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{{ID: 1, ActivePane: 1, Layout: valid.Workspaces[0].Layout, Panes: valid.Workspaces[0].Panes, Context: ContextStateDTO{Open: true}}}}},
		{"context width", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{{ID: 1, ActivePane: 1, Layout: valid.Workspaces[0].Layout, Panes: valid.Workspaces[0].Panes, Context: ContextStateDTO{Kind: ContextSFTP, Width: 900}}}}},
		{"context side", DeckDTO{Version: DeckDTOVersion, ActiveWorkspace: 1, Workspaces: []WorkspaceDTO{{ID: 1, ActivePane: 1, Layout: valid.Workspaces[0].Layout, Panes: valid.Workspaces[0].Panes, Context: ContextStateDTO{Side: ContextRight + 1}}}}},
	}
	if _, err := RestoreDeck(valid); err != nil {
		t.Fatalf("valid deck: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deck, err := RestoreDeck(test.dto)
			if !errors.Is(err, ErrInvalidLayout) || len(deck.workspaces) != 0 {
				t.Fatalf("restore = %+v, %v", deck, err)
			}
		})
	}
}
