package workspace

import "testing"

func TestLockSurvivesRestoreAndTransfer(t *testing.T) {
	var deck Deck
	id, _, _ := deck.Create("locked")
	_ = deck.SetLocked(id, true)
	restored, err := RestoreDeck(deck.DTO())
	if err != nil || !restored.Locked(id) || !restored.Workspaces(nil)[0].Locked {
		t.Fatalf("restored lock = %v, err = %v", restored.Locked(id), err)
	}
	dto, _ := restored.ExportWorkspace(id)
	var other Deck
	imported, err := other.Import(dto, nil, 0)
	if err != nil || !other.Locked(imported) {
		t.Fatalf("imported lock = %v, err = %v", other.Locked(imported), err)
	}
}
