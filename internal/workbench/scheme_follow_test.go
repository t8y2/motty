package workbench

import "testing"

func TestAppearanceFollowTerminalUpdatesWindowPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalSchemes[state.appearance] = colorSchemes[1].id
	state.rebuildTerminalLook()
	defaultShell := state.pal.shell
	defaultWorkspace := state.pal.workspace

	state.openSettings()
	state.selectSettingsCategory(settingsAppearance)
	state.Click(hitSettingsAppearanceFollowTerminal)
	if !state.appearanceFollowTerminal {
		t.Fatal("clicking toggle did not enable appearanceFollowTerminal")
	}
	if state.pal.workspace != state.termPal.terminal {
		t.Fatalf("workspace = %v, want terminal background %v", state.pal.workspace, state.termPal.terminal)
	}
	if state.pal.shell == defaultShell && state.pal.workspace == defaultWorkspace {
		t.Fatal("window palette did not update to follow terminal scheme")
	}

	state.Click(hitSettingsAppearanceFollowTerminal)
	if state.appearanceFollowTerminal {
		t.Fatal("clicking toggle again did not disable appearanceFollowTerminal")
	}
	if state.pal.shell != defaultShell || state.pal.workspace != defaultWorkspace {
		t.Fatalf("window palette = (%v, %v), want default (%v, %v)", state.pal.shell, state.pal.workspace, defaultShell, defaultWorkspace)
	}
}

func TestAppearanceFollowTerminalPersistsRoundTrip(t *testing.T) {
	opts := testOptions(t)
	state := newTestState(t, opts)
	state.terminalSchemes[state.appearance] = colorSchemes[1].id
	state.appearanceFollowTerminal = true
	state.rebuildTerminalLook()
	state.save()
	state.Close()

	reloaded := newTestState(t, opts)
	if !reloaded.appearanceFollowTerminal {
		t.Fatal("appearanceFollowTerminal did not persist across reload")
	}
	if reloaded.pal.workspace != reloaded.termPal.terminal {
		t.Fatalf("reloaded workspace = %v, want %v", reloaded.pal.workspace, reloaded.termPal.terminal)
	}
}
