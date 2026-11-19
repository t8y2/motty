package workbench

import (
	"testing"
)

func TestHostMenuCopyAndPasteDuplicateTheHostInItsGroup(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuCopy)
	if len(state.hostClipboard) != 1 || state.hostClipboard[0] != target.ID {
		t.Fatalf("clipboard = %v, want host %d", state.hostClipboard, target.ID)
	}
	before := len(state.hosts)
	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuPaste)
	if len(state.hosts) != before+1 {
		t.Fatalf("paste left %d hosts, want %d (modal %v error %q status %q)", len(state.hosts), before+1, state.modal, state.dialogError, state.status)
	}
	copied := state.hosts[len(state.hosts)-1]
	if copied.Group != target.Group || copied.Address != target.Address || copied.User != target.User || copied.Port != target.Port {
		t.Fatalf("copy = %+v, want the host's connection fields in group %q", copied, target.Group)
	}
	if copied.Label == target.Label || copied.CatalogID == target.CatalogID {
		t.Fatalf("copy kept the original identity: %q %q", copied.Label, copied.CatalogID)
	}
}

func TestPastingTwiceNamesTheSecondCopyApart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuCopy)
	before := len(state.hosts)
	for range 2 {
		rightClickHost(state, target.ID)
		chooseMenu(t, state, hitHostMenuPaste)
	}
	if len(state.hosts) != before+2 {
		t.Fatalf("two pastes left %d hosts, want %d", len(state.hosts), before+2)
	}
	first, second := state.hosts[before], state.hosts[before+1]
	if first.Label == second.Label {
		t.Fatalf("both copies are named %q", first.Label)
	}
}

func TestPasteIsOfferedOnlyWhenTheClipboardHoldsAHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[0].ID)
	canvas, _ := paintedTexts(state)
	if hit, ok := menuHit(canvas, state, hitHostMenuPaste); !ok || !hit.Disabled {
		t.Fatalf("paste with an empty clipboard: found=%v disabled=%v", ok, hit.Disabled)
	}
	state.Click(hitHostMenuDismiss)
	rightClickHost(state, state.hosts[0].ID)
	chooseMenu(t, state, hitHostMenuCopy)
	rightClickHost(state, state.hosts[0].ID)
	canvas, _ = paintedTexts(state)
	if hit, ok := menuHit(canvas, state, hitHostMenuPaste); !ok || hit.Disabled {
		t.Fatalf("paste with a copied host: found=%v disabled=%v", ok, hit.Disabled)
	}
}

func TestCopyToDuplicatesTheHostIntoTheChosenGroup(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]
	destination := state.hostGroups[0].Name
	if destination == "" {
		destination = state.hostGroups[1].Name
	}
	rightClickHost(state, target.ID)
	chooseMenu(t, state, hitHostMenuCopyTo)
	if !state.hostGroupMenu || !state.hostGroupCopy {
		t.Fatalf("copy picker: open=%v copy=%v", state.hostGroupMenu, state.hostGroupCopy)
	}
	before := len(state.hosts)
	choice := hostMoveGroupChoice(t, state, destination)
	state.Click(choice)
	if state.hostGroupCopy {
		t.Fatal("the copy picker stayed in copy mode")
	}
	if len(state.hosts) != before+1 {
		t.Fatalf("copy to %q left %d hosts, want %d", destination, len(state.hosts), before+1)
	}
	copied := state.hosts[len(state.hosts)-1]
	if copied.Group != destination || copied.ID == target.ID {
		t.Fatalf("copy = %+v, want a new host in %q", copied, destination)
	}
}
