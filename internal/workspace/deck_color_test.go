package workspace

import (
	"encoding/json"
	"testing"
)

func TestWorkspaceColorSurvivesPersistenceAndTransfer(t *testing.T) {
	const blue = "#268bd2"
	for _, color := range []string{blue, "none"} {
		t.Run(color, func(t *testing.T) {
			var deck Deck
			id, _, err := deck.Create("coloured tab")
			if err != nil {
				t.Fatal(err)
			}
			if err := deck.SetColor(id, color); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(deck.DTO())
			if err != nil {
				t.Fatal(err)
			}
			var saved DeckDTO
			if err := json.Unmarshal(data, &saved); err != nil {
				t.Fatal(err)
			}
			restored, err := RestoreDeck(saved)
			if err != nil {
				t.Fatal(err)
			}
			if got := restored.Workspaces(nil)[0].Color; got != color {
				t.Fatalf("restored color = %q, want %q", got, color)
			}
			dto, ok := restored.ExportWorkspace(id)
			if !ok {
				t.Fatal("cannot export colored workspace")
			}
			var destination Deck
			if _, err := destination.Import(dto, nil, 0); err != nil {
				t.Fatal(err)
			}
			if got := destination.Workspaces(nil)[0].Color; got != color {
				t.Fatalf("transferred color = %q, want %q", got, color)
			}
		})
	}
}
