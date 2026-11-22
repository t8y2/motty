//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
)

func TestControlWReachesTheTerminalThroughTheWindow(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.shortcuts.chords.Set(shortcutCloseTab, keymap.Chord{Modifiers: gfx.ModifierControl, Char: 'w'})
	window, err := headless.Open(app.Config{
		Title: "terminal word delete", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: state.TerminalInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer window.Close()
	if err := window.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)); err != nil {
		t.Fatal(err)
	}
	shell.input = nil
	if err := window.Keys("ctrl+w"); err != nil {
		t.Fatal(err)
	}
	if got := string(shell.input); got != "\x17" {
		t.Fatalf("Ctrl+W sent %q, want the shell's word-delete key", got)
	}
	if state.modal != modalNone || len(state.sessions) != 1 {
		t.Fatalf("Ctrl+W did not leave the terminal open: modal=%v sessions=%d", state.modal, len(state.sessions))
	}
}
