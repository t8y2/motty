package workbench

import "testing"

// A host edit the store refuses must not close as if it had been written: the
// AI agent switch lives only in this computer's own state, so a dialog that
// reports success over a refused write loses it at the next start.
func TestAHostEditTheStoreRefusesStaysInTheDialog(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	host := state.hosts[0]
	state.selectedHostID = host.ID
	state.openEditHost()
	state.Click(hitDialogAdvanced)
	state.Click(hitDialogAgentAccess)
	typeInto(state, hitDialogLabel, " edited")

	// A store that refuses every write, as an unavailable profile, a full disk
	// or a read-only volume does.
	state.saveBlocked = true
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost {
		t.Fatalf("the dialog closed over a refused write: modal=%v", state.modal)
	}
	if state.dialogError == "" {
		t.Fatal("a refused write was not explained")
	}
	if state.agentHosts[host.CatalogID] {
		t.Fatalf("the refused switch stayed on in memory: %v", state.agentHosts)
	}
	current, ok := state.hostByID(host.ID)
	if !ok || current.Label != host.Label {
		t.Fatalf("the refused edit stayed in the catalog: %+v", current)
	}

	// The same dialog writes the edit once the store takes writes again, and
	// what it kept is there at restart.
	state.saveBlocked = false
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("the saved dialog stayed open: modal=%v error=%q", state.modal, state.dialogError)
	}
	if !state.agentHosts[host.CatalogID] {
		t.Fatalf("the saved switch is not on: %v", state.agentHosts)
	}
	renamed, ok := state.hostByID(host.ID)
	if !ok || renamed.Label == host.Label {
		t.Fatalf("the saved edit is not in the catalog: %+v", renamed)
	}

	again := New(state.options)
	defer again.Close()
	if !again.agentHosts[host.CatalogID] {
		t.Fatalf("the switch did not survive a restart: %v", again.agentHosts)
	}
	if restored, ok := again.hostByID(host.ID); !ok || restored.Label != renamed.Label {
		t.Fatalf("the edit did not survive a restart: %+v", restored)
	}
}
