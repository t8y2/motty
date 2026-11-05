package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// TestStartupOpensWhatTheSettingsAsk checks that the first Wake opens the local
// terminal and the chosen hosts, and drops a host that left the catalog.
func TestStartupOpensWhatTheSettingsAsk(t *testing.T) {
	state := newTabState(t)
	host := state.hosts[0]
	if host.CatalogID == "" {
		t.Fatal("the test host has no catalog id")
	}
	before := len(state.sessions)
	state.startupLocal = true
	state.startupHosts = []string{host.CatalogID, "gone-from-the-catalog"}
	state.restorePending = true
	state.Wake()
	if state.restorePending {
		t.Fatal("the pending startup must be done")
	}
	if len(state.startupHosts) != 1 || state.startupHosts[0] != host.CatalogID {
		t.Fatalf("startup hosts %v: the host that left the catalog must be dropped", state.startupHosts)
	}
	if len(state.sessions) != before+2 {
		t.Fatalf("sessions %d -> %d: a launch opens a local terminal and the host", before, len(state.sessions))
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.HostID != host.ID {
		t.Fatalf("active session ok=%v: the launch must end on the host's tab", ok)
	}
	if state.activeSection != sectionTerminal {
		t.Fatalf("section = %v: a launch that opens sessions ends on the terminal", state.activeSection)
	}
}

// TestStartupOpensNothingWithoutTheSettings checks the choice's guard.
func TestStartupOpensNothingWithoutTheSettings(t *testing.T) {
	state := newTabState(t)
	before := len(state.sessions)
	state.restorePending = true
	state.Wake()
	if state.restorePending {
		t.Fatal("the pending startup must be done")
	}
	if len(state.sessions) != before {
		t.Fatalf("sessions %d -> %d: no settings, no sessions at launch", before, len(state.sessions))
	}
}

// TestStartupSettingsRespondToClicks checks that the chooser picks the local
// terminal and the hosts, and keeps the menu open while choosing.
func TestStartupSettingsRespondToClicks(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsStartupHosts)
	if !state.startupPicker.open {
		t.Fatal("the chooser did not open")
	}
	if state.startupLocal {
		t.Fatal("nothing is chosen before a click")
	}
	state.Click(hitSettingsStartupLocal)
	if !state.startupLocal || !state.startupPicker.open {
		t.Fatal("the local terminal must be chosen and the menu stay open")
	}
	host := state.hosts[0]
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSettingsStartupHostList.At(0)); !ok {
		t.Fatal("the chooser lists no hosts")
	}
	state.Click(hitSettingsStartupHostList.At(0))
	if len(state.startupHosts) != 1 || state.startupHosts[0] != host.CatalogID {
		t.Fatalf("startup hosts %v: choosing a host must record its catalog id", state.startupHosts)
	}
	if !state.startupPicker.open {
		t.Fatal("choosing a host must keep the menu open for another choice")
	}
	state.Click(hitSettingsStartupLocal)
	if state.startupLocal {
		t.Fatal("clicking the local terminal again must unchoose it")
	}
	if state.startupSummary() == state.messages.SettingsStartupNone {
		t.Fatalf("summary %q: the chosen host must show", state.startupSummary())
	}
}
