package workspace

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPaneMaximizePreservesTreeAndIndependentTabs(t *testing.T) {
	var deck Deck
	a, first, _ := deck.Create("A")
	second, _ := deck.Split(a, first, Horizontal)
	_, _ = deck.Split(a, second, Vertical)
	b, other, _ := deck.Create("B")
	_, _ = deck.Split(b, other, Horizontal)
	layout, _ := deck.Layout(a)
	before := layout.DTO()
	revision, _ := deck.Revision(a)
	if !deck.SetPaneMaximized(a, true) || !deck.PaneMaximized(a) || deck.PaneMaximized(b) {
		t.Fatal("workspace view modes were not independent")
	}
	deck.ActivatePane(a, first)
	if current, _ := deck.Revision(a); current <= revision {
		t.Fatal("view did not invalidate geometry")
	}
	if !deck.SetPaneMaximized(a, false) {
		t.Fatal("restore did not change view")
	}
	if !reflect.DeepEqual(before, layout.DTO()) {
		t.Fatal("maximizing changed the split tree")
	}
}

func TestPaneMaximizePersistsAndTransfersActivePane(t *testing.T) {
	var deck Deck
	a, first, _ := deck.Create("A")
	second, _ := deck.Split(a, first, Horizontal)
	_, _ = deck.Split(a, second, Vertical)
	deck.Bind(a, first, 10)
	deck.Bind(a, second, 20)
	deck.ActivatePane(a, second)
	deck.SetPaneMaximized(a, true)
	encoded, err := json.Marshal(deck.DTO())
	if err != nil {
		t.Fatal(err)
	}
	var dto DeckDTO
	if err := json.Unmarshal(encoded, &dto); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreDeck(dto)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck.DTO(), restored.DTO()) {
		t.Fatal("restore lost workspace view")
	}
	var target Deck
	target.Create("Existing")
	entry, _ := restored.ExportWorkspace(a)
	imported, err := target.Import(entry, map[SessionID]SessionID{10: 30, 20: 40}, 1)
	if err != nil {
		t.Fatal(err)
	}
	active, _ := target.Pane(imported, target.ActivePane(imported))
	if !target.PaneMaximized(imported) || active.SessionID != 40 {
		t.Fatal("import lost active maximized pane")
	}
	if _, err := restored.ClosePane(a, first); err != nil {
		t.Fatal(err)
	}
	if !restored.PaneMaximized(a) {
		t.Fatal("closing one of three panes restored the view")
	}
}
