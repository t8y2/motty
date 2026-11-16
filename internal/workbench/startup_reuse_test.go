package workbench

import (
	"context"
	"testing"

	"motty/internal/localshell"
)

// TestStartupSessionsReuseOpenOnes checks the reuse switch: with it on, a
// launch switches to a terminal that is already open instead of adding another;
// with it off, a new one opens. The switch itself flips and is saved negated.
func TestStartupSessionsReuseOpenOnes(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.startupLocal = true
	state.startupReuse = true
	restored := state.openLocalTerminal()
	before := len(state.sessions)
	state.openStartupSessions()
	if len(state.sessions) != before {
		t.Fatalf("sessions %d -> %d: an open local terminal must be reused", before, len(state.sessions))
	}
	if state.activeSessionID != restored.ID {
		t.Fatalf("session %d is on screen: the reused terminal must be the one shown", state.activeSessionID)
	}

	state.startupReuse = false
	state.openStartupSessions()
	if len(state.sessions) != before+1 {
		t.Fatalf("sessions %d: with reuse off a new local terminal must open", len(state.sessions))
	}

	state.startupReuse = true
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsStartupReuse)
	if state.startupReuse {
		t.Fatal("the switch must turn off")
	}
	if !state.snapshot().StartupNoReuse {
		t.Fatal("the off switch must be saved")
	}
}

// TestStartupFeatureSwitch checks the switch of the launch feature itself: off,
// a launch opens nothing — the chooser stays closed and the choice is kept.
func TestStartupFeatureSwitch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.startupLocal = true
	state.startupEnabled = true
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsStartupEnable)
	if state.startupEnabled {
		t.Fatal("the switch must turn off")
	}
	if !state.snapshot().StartupDisabled {
		t.Fatal("the off switch must be saved")
	}
	if state.startupSummary() != state.messages.SettingsStartupDisabled {
		t.Fatalf("summary %q: the disabled feature must say so", state.startupSummary())
	}
	if len(state.startupHosts) != 0 {
		t.Fatal("turning off must keep the choice")
	}
	before := len(state.sessions)
	state.openStartupSessions()
	if len(state.sessions) != before {
		t.Fatalf("sessions %d -> %d: a launch must open nothing while off", before, len(state.sessions))
	}
	state.Click(hitSettingsStartupHosts)
	if state.startupPicker.open {
		t.Fatal("the chooser must stay closed while off")
	}
	state.Click(hitSettingsStartupEnable)
	if !state.startupEnabled || !state.startupLocal {
		t.Fatal("turning on again must find the kept choice")
	}
}

// TestResolveLocalShell checks what a local terminal runs when nothing was
// picked: the machine's detected default, the one the shell setting shows —
// not whatever a PATH lookup happens to find first.
func TestResolveLocalShell(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.localShellMenu.shells = []localshell.Choice{
		{Name: "CMD", Path: `C:\Windows\system32\cmd.exe`},
		{Name: "Windows PowerShell", Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, Default: true},
	}
	state.localShellMenu.rebuild()
	state.localShellMenu.loaded = true // the test's list, not a fresh detection
	state.localShell = ""              // nothing was picked in the settings
	host := localHost(state.messages.LocalTerminalTitle)
	if shell := state.resolveLocalShell(host); shell != state.localShellMenu.choices[1].Path {
		t.Fatalf("shell %q: an unpicked terminal must run the detected default", shell)
	}
	state.localShell = state.localShellMenu.choices[0].Path
	if shell := state.resolveLocalShell(host); shell != state.localShell {
		t.Fatalf("shell %q: the settings' default must win over the detected one", shell)
	}
	host.localShell = state.localShellMenu.choices[1].Path
	if shell := state.resolveLocalShell(host); shell != host.localShell {
		t.Fatalf("shell %q: the tab's own shell must win over the settings", shell)
	}
}

// TestStartupShellChoice checks that the launch menu lists this machine's
// detected shells: picking one opens that shell instead of the default, and
// picking the default again clears the shell.
func TestStartupShellChoice(t *testing.T) {
	state := newTestState(t, testOptions(t))
	// Opening the menu detects the shells again: the test machine offers these
	// two, with the command interpreter as the default.
	shells := []localshell.Choice{
		{Name: "CMD", Path: `C:\Windows\system32\cmd.exe`, Default: true},
		{Name: "Windows PowerShell", Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
	}
	state.localShellMenu.discover = func() []localshell.Choice { return shells }
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice { return nil }
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsStartupHosts)
	state.Click(hitSettingsStartupShells.At(1))
	if state.startupShell != state.localShellMenu.choices[1].Path || state.startupLocal {
		t.Fatalf("shell %q local %v: picking a shell must choose it instead of the default", state.startupShell, state.startupLocal)
	}
	if state.startupSummary() != "Windows PowerShell" {
		t.Fatalf("summary %q: the picked shell must show by name", state.startupSummary())
	}
	state.openStartupSessions()
	if len(state.sessions) == 0 {
		t.Fatal("a launch with a picked shell must open it")
	}
	local := state.sessions[len(state.sessions)-1]
	if local.host.localShell != state.localShellMenu.choices[1].Path {
		t.Fatalf("opened shell %q: the picked shell must run", local.host.localShell)
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	state.Click(hitSettingsStartupHosts)
	state.Click(hitSettingsStartupLocal)
	if !state.startupLocal || state.startupShell != "" {
		t.Fatalf("shell %q local %v: picking the default must clear the shell", state.startupShell, state.startupLocal)
	}
	state.Click(hitSettingsStartupDismiss)
	state.Click(hitSettingsStartupHosts)
	state.Click(hitSettingsStartupShells.At(1))
	if state.startupShell != state.localShellMenu.choices[1].Path || state.startupLocal {
		t.Fatal("reopening the menu must offer the shells again")
	}
	state.Click(hitSettingsStartupShells.At(1)) // the menu stays open: the same row clears it
	if state.startupShell != "" || state.startupLocal {
		t.Fatal("clicking the picked shell again must clear the choice")
	}
}
