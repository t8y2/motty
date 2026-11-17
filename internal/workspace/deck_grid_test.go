package workspace

import (
	"errors"
	"reflect"
	"testing"
)

func TestDeckSetLayoutPreservesBindingsAndActivePane(t *testing.T) {
	var deck Deck
	workspaceID, first, err := deck.Create("Operations")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deck.Split(workspaceID, first, Horizontal)
	if err != nil {
		t.Fatal(err)
	}
	third, err := deck.Split(workspaceID, second, Vertical)
	if err != nil {
		t.Fatal(err)
	}
	for paneID, sessionID := range map[PaneID]SessionID{first: 10, second: 20, third: 30} {
		if err := deck.Bind(workspaceID, paneID, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	if !deck.ActivatePane(workspaceID, second) {
		t.Fatal("activate second pane")
	}
	beforeRevision, _ := deck.Revision(workspaceID)

	order := []PaneID{third, first, second}
	if err := deck.SetLayout(workspaceID, order, GridSpec(2)); err != nil {
		t.Fatal(err)
	}

	panes := deck.Panes(workspaceID, nil)
	want := []Pane{
		{ID: third, SessionID: 30},
		{ID: first, SessionID: 10},
		{ID: second, SessionID: 20},
	}
	if !reflect.DeepEqual(panes, want) {
		t.Fatalf("panes = %+v, want %+v", panes, want)
	}
	if deck.ActivePane(workspaceID) != second {
		t.Fatalf("active pane = %d, want %d", deck.ActivePane(workspaceID), second)
	}
	layout, _ := deck.Layout(workspaceID)
	assertGridRects(t, layout, []gridRect{
		{0, 0, 0.5, 0.5}, {0.5, 0, 0.5, 0.5},
		{0, 0.5, 1, 0.5},
	})
	if revision, _ := deck.Revision(workspaceID); revision <= beforeRevision {
		t.Fatalf("revision = %d, want more than %d", revision, beforeRevision)
	}
	if err := deck.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDeckSetLayoutRejectsInvalidOrderWithoutMutation(t *testing.T) {
	var deck Deck
	workspaceID, first, err := deck.Create("Atomic")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deck.Split(workspaceID, first, Horizontal)
	if err != nil {
		t.Fatal(err)
	}
	third, err := deck.Split(workspaceID, second, Horizontal)
	if err != nil {
		t.Fatal(err)
	}
	for index, paneID := range []PaneID{first, second, third} {
		if err := deck.Bind(workspaceID, paneID, SessionID(index+1)); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		order   []PaneID
		columns int
		want    error
	}{
		{"duplicate", []PaneID{first, first, second}, 2, ErrInvalidLayout},
		{"missing pane", []PaneID{first, second}, 2, ErrInvalidLayout},
		{"unknown pane", []PaneID{first, second, 99}, 2, ErrPaneNotFound},
		{"zero pane", []PaneID{first, second, 0}, 2, ErrPaneNotFound},
		{"zero columns", []PaneID{first, second, third}, 0, ErrInvalidColumns},
		{"too many columns", []PaneID{first, second, third}, MaxGridColumns + 1, ErrInvalidColumns},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := deck.DTO()
			err := deck.SetLayout(workspaceID, test.order, GridSpec(uint8(test.columns)))
			if !errors.Is(err, test.want) {
				t.Fatalf("SetLayout() error = %v, want %v", err, test.want)
			}
			if got := deck.DTO(); !reflect.DeepEqual(got, before) {
				t.Fatalf("failed SetLayout mutated deck:\n got %+v\nwant %+v", got, before)
			}
		})
	}
	if err := deck.SetLayout(workspaceID+1, []PaneID{first}, GridSpec(1)); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("missing workspace error = %v", err)
	}
}
