//go:build windows

package workbench

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

func windowsShellChoices(t *testing.T) []localshell.Choice {
	t.Helper()
	var choices []localshell.Choice
	for _, shell := range []struct{ name, program string }{{"CMD", "cmd.exe"}, {"Windows PowerShell", "powershell.exe"}} {
		path, err := exec.LookPath(shell.program)
		if err != nil {
			t.Fatal(err)
		}
		choices = append(choices, localshell.Choice{Name: shell.name, Path: path, Default: len(choices) == 0})
	}
	return choices
}

func shellSelectState(t *testing.T, choices []localshell.Choice) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.localShellMenu.discover = func() []localshell.Choice { return choices }
	// The machine's own distributions are not what these tests are about.
	state.localShellMenu.discoverWSL = func(context.Context) []localshell.Choice { return nil }
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	return state
}

func paintShellSettings(state *State) *gfx.Canvas {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	// The terminal page grows as settings are added, so the row this file is
	// about is not always in the first screenful. Reach it the way a user does
	// rather than assuming the canvas is tall enough.
	if _, visible := hitByID(&canvas, hitSettingsLocalShell); !visible {
		state.scrollSettings(gfx.ScrollEvent{DeltaY: state.settingsContent, Precise: true})
		canvas.Reset()
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	}
	return &canvas
}

func TestLocalShellDropdownSelectsAndPersists(t *testing.T) {
	choices := windowsShellChoices(t)
	state := shellSelectState(t, choices)
	canvas := paintShellSettings(state)
	hit, ok := hitByID(canvas, hitSettingsLocalShell)
	if !ok || hit.Value != "CMD" || hit.Expanded {
		t.Fatalf("default select: %+v", hit)
	}
	state.Click(hitSettingsLocalShell)
	canvas = paintShellSettings(state)
	for index, choice := range choices {
		hit, ok := hitByID(canvas, hitSettingsLocalShellItems.At(index))
		if !ok || hit.Label != choice.Name || hit.Selected != (index == 0) {
			t.Fatalf("shell option %d: %+v", index, hit)
		}
	}
	state.Click(hitSettingsLocalShellItems.At(1))
	if state.localShellMenu.open || state.localShell != choices[1].Path {
		t.Fatal("selection did not save and close")
	}
	options := state.options
	state.Close()
	restored := newTestState(t, options)
	if restored.localShell != choices[1].Path {
		t.Fatalf("restart lost the shell: %q", restored.localShell)
	}
}

func TestLocalShellDropdownKeyboardDismissAndRefresh(t *testing.T) {
	choices := windowsShellChoices(t)
	state := shellSelectState(t, choices)
	paintShellSettings(state)
	result := state.Key(gfx.KeyEvent{Target: hitSettingsLocalShell, Key: gfx.KeyDown})
	if !result.Handled || !state.localShellMenu.open {
		t.Fatal("Down did not open the dropdown")
	}
	paintShellSettings(state)
	result = state.Key(gfx.KeyEvent{Target: hitSettingsLocalShellItems.At(0), Key: gfx.KeyDown})
	if result.Focus != hitSettingsLocalShellItems.At(1) {
		t.Fatalf("Down did not focus PowerShell: %+v", result)
	}
	state.Click(hitSettingsLocalShellDismiss)
	if state.localShellMenu.open || state.localShell != "" {
		t.Fatal("dismiss changed the selection")
	}
	state.Click(hitSettingsLocalShell)
	paintShellSettings(state)
	result = state.Key(gfx.KeyEvent{Target: hitSettingsLocalShellItems.At(0), Key: gfx.KeyTab})
	if !result.ContinueTraversal || state.localShellMenu.open {
		t.Fatal("Tab did not dismiss the dropdown")
	}
	paintShellSettings(state)
	result = state.Key(gfx.KeyEvent{Target: hitSettingsLocalShell, Text: "w"})
	if !result.Handled || state.localShell != choices[1].Path {
		t.Fatal("closed-select typeahead did not choose PowerShell")
	}
	state.localShellMenu.discover = func() []localshell.Choice { return choices[:1] }
	state.Click(hitSettingsLocalShell)
	if len(state.localShellMenu.choices) != 1 {
		t.Fatal("opening the dropdown did not refresh installed shells")
	}
	state.selectSettingsCategory(settingsGeneral)
	paintShellSettings(state)
	if state.localShellMenu.open {
		t.Fatal("changing category left the dropdown open")
	}
}

func TestLocalShellDropdownHandlesNoInstalledShells(t *testing.T) {
	state := shellSelectState(t, nil)
	canvas := paintShellSettings(state)
	hit, ok := hitByID(canvas, hitSettingsLocalShell)
	if !ok || !hit.Disabled || hit.Value != state.messages.SettingsLocalShellEmpty {
		t.Fatalf("empty discovery: %+v", hit)
	}
}

func TestConfiguredShellsRunThroughTheWorkbench(t *testing.T) {
	choices := windowsShellChoices(t)
	t.Setenv("PSModulePath", filepath.Join(filepath.Dir(choices[1].Path), "Modules"))
	dir := filepath.Join(t.TempDir(), "shell with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(choices[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	choices[0].Path = filepath.Join(dir, "cmd.exe")
	if err := os.WriteFile(choices[0].Path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	state := shellSelectState(t, choices)
	paintShellSettings(state)
	state.Click(hitSettingsLocalShell)
	state.Click(hitSettingsLocalShellItems.At(0))
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term.shell == nil {
		t.Fatalf("configured CMD did not start: %+v", session)
	}
	session.term.send([]byte("set /a 6*7\r"))
	localShellOutput(t, state, session, "42")
	first := session.term.shell
	state.Click(hitSettings)
	state.Click(hitSettingsLocalShell)
	state.Click(hitSettingsLocalShellItems.At(1))
	if session.term.shell != first || session.conn.(*localConn).shell != choices[0].Path {
		t.Fatal("selection replaced a running shell")
	}
	state.reconnect(session)
	if session.term.shell == nil || session.conn.(*localConn).shell != choices[1].Path {
		t.Fatal("reconnect did not use the new selection")
	}
	session.term.send([]byte("Write-Output ('powershell-' + (6*7))\r"))
	localShellOutput(t, state, session, "powershell-42")
}
