package workbench

import (
	"context"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
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
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
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

// TestStartupShellOpensWithoutOpeningTheSettings checks the launch path that
// never visits the settings page: the shell list must be detected before the
// chosen shell is resolved, or the launch silently runs the default shell.
func TestStartupShellOpensWithoutOpeningTheSettings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	shells := []localshell.Choice{
		{Name: "sh", Path: "/bin/sh", Default: true},
		{Name: "bash", Path: "/bin/bash"},
	}
	state.localShellMenu.discover = func() []localshell.Choice { return shells }
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice { return nil }
	state.localShellMenu.loaded = false
	state.startupEnabled = true
	state.startupShell = shells[1].Path
	state.startupHosts = nil

	state.openStartupSessions()

	if len(state.sessions) == 0 {
		t.Fatal("a launch with only a picked shell opened nothing")
	}
	local := state.sessions[len(state.sessions)-1]
	if local.host.localShell != shells[1].Path {
		t.Fatalf("opened shell %q, want %q", local.host.localShell, shells[1].Path)
	}
}

// TestStartupWantedWithOnlyAPickedShell checks the first-Wake guard: a picked
// shell, with no local terminal and no hosts, still asks the launch to run.
func TestStartupWantedWithOnlyAPickedShell(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.restoreConnect = restoreConnectOff
	state.startupEnabled = true
	state.startupLocal = false
	state.startupHosts = nil
	state.startupShell = "/bin/sh"
	if !state.startupWanted() {
		t.Fatal("a picked shell alone must count as startup work")
	}
	state.startupShell = ""
	if state.startupWanted() {
		t.Fatal("no launch choice must not count as startup work")
	}
}
