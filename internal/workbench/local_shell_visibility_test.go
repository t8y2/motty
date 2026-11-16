package workbench

import (
	"runtime"
	"slices"
	"testing"

	"motty/internal/localshell"
)

func TestHiddenShellsLeaveThePickersButStayInTheSetting(t *testing.T) {
	program, second, third := "/usr/bin/zsh", "/bin/bash", "/bin/zsh"
	if runtime.GOOS == "windows" {
		program, second, third = `C:\Program Files\PowerShell\7\pwsh.exe`, `C:\Windows\System32\cmd.exe`, `C:\Program Files\PowerShell\6\pwsh.exe`
	}
	state := newTestState(t, testOptions(t))
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: program, Default: true}, {Name: "Beta", Path: second}, {Name: "Gamma", Path: third}},
		nil)
	state.localShell = program
	state.refreshLocalShells()
	if state.visibleShellCount() != 3 {
		t.Fatalf("visible shells = %d, want all three", state.visibleShellCount())
	}

	// Hiding Beta keeps it in the setting's own choices, drops the visible
	// count, and saves.
	state.hideShell(second, true)
	if len(state.localShellMenu.choices) != 3 {
		t.Fatalf("the setting lost a shell: %d", len(state.localShellMenu.choices))
	}
	if state.visibleShellCount() != 2 {
		t.Fatalf("visible shells after hiding = %d, want two", state.visibleShellCount())
	}
	path := testConfigPath(t)
	if err := saveState(path, savedState{HiddenShells: state.hiddenShells}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := loadState(path)
	if err != nil || !slices.Equal(loaded.HiddenShells, []string{second}) {
		t.Fatalf("saved hidden shells = %+v, error = %v", loaded.HiddenShells, err)
	}

	// The default shell refuses to hide.
	state.hideShell(program, true)
	if state.shellHidden(state.localShellMenu.choices[0]) {
		t.Fatal("the default shell was hidden")
	}

	// Hiding every named shell leaves one visible choice, and the pickers'
	// expansion gates follow the visible count, not the detected one.
	state.hideShell(third, true)
	if state.visibleShellCount() != 1 {
		t.Fatalf("visible shells = %d, want the default alone", state.visibleShellCount())
	}

	// Showing Beta again takes its entry back out.
	state.hideShell(second, false)
	if state.shellHidden(state.localShellMenu.choices[1]) || len(state.hiddenShells) != 1 {
		t.Fatalf("showing Beta again left %+v", state.hiddenShells)
	}
}

func TestHiddenShellEntriesSurviveAndComeBackHidden(t *testing.T) {
	state := newTestState(t, testOptions(t))
	withShellChoices(t, state,
		[]localshell.Choice{{Name: "Alpha", Path: "/bin/sh", Default: true}},
		localshell.WSLChoices([]string{"Ubuntu"}))
	state.localShell = "/bin/sh"
	state.refreshLocalShells()

	// A hidden entry whose shell this computer stops detecting stays saved:
	// nothing prunes what the user hid.
	state.hideShell("/usr/bin/gone", true)
	state.hideShell(localshell.WSLShell("Debian"), true)
	path := testConfigPath(t)
	if err := saveState(path, savedState{HiddenShells: state.hiddenShells}); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := loadState(path)
	if err != nil || !slices.Equal(loaded.HiddenShells, state.hiddenShells) {
		t.Fatalf("loaded hidden shells = %+v, error = %v", loaded.HiddenShells, err)
	}

	// The listings change between opens: Ubuntu arrives, Debian does not.
	// The saved Debian entry simply sits there, unseen and harmless.
	state.localShellMenu.distros = localshell.WSLChoices([]string{"Ubuntu"})
	state.localShellMenu.rebuild()
	hidden := localshell.Choice{Name: localshell.WSLName("Debian"), Path: localshell.WSLShell("Debian")}
	if !state.shellHidden(hidden) {
		t.Fatal("a distribution that was hidden lost its hiding")
	}

	// When the distribution comes back, it comes back hidden, and the eye
	// opens it again.
	state.localShellMenu.distros = localshell.WSLChoices([]string{"Ubuntu", "Debian"})
	state.localShellMenu.rebuild()
	if !state.shellHidden(hidden) {
		t.Fatal("a distribution that came back did not come back hidden")
	}
	state.hideShell(localshell.WSLShell("Debian"), false)
	if state.shellHidden(hidden) {
		t.Fatal("the eye did not show Debian again")
	}
}
