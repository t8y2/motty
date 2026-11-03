//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestTerminalCommandKeysNegotiation(t *testing.T) {
	state, _, _, shell := liveSession(t)
	// Bind explicitly so the same shortcut contract is checked on every OS.
	if _, ok := state.shortcuts.bind(shortcutCopy, shortcutChord{Modifiers: ui.ModifierMeta, Key: 'c'}); !ok {
		t.Fatal("could not bind Command+C")
	}
	session, err := headless.Open(app.Config{
		Title: "Command forwarding", Width: 1400, Height: 900,
		Paint: state.Paint, TextInput: state.TerminalInput,
		OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		flags uint32
		chord string
		want  string
	}{
		{"legacy", 0, "cmd+shift+left", ""},
		{"kitty", 1, "cmd+shift+left", "\x1b[1;10D"},
		{"all keys", 8, "cmd+shift+right", "\x1b[1;10C"},
		{"printable", 1, "cmd+j", "\x1b[106;9u"},
		{"copy stays local", 8, "cmd+c", ""},
		{"unbound modified copy", 1, "cmd+alt+c", "\x1b[99;11u"},
		{"negotiation disabled", 0, "cmd+j", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			shell.say(fmt.Sprintf("\x1b[=%du", test.flags))
			state.Wake()
			shell.input = nil
			if err := session.Keys(test.chord); err != nil {
				t.Fatal(err)
			}
			if got := string(shell.input); got != test.want {
				t.Fatalf("%s sent %q, want %q", test.chord, got, test.want)
			}
		})
	}
	shell.say("\x1b[=1u")
	state.Wake()
	state.shortcuts.unbind(shortcutCopy)
	shell.input = nil
	if err := session.Keys("cmd+c"); err != nil {
		t.Fatal(err)
	}
	if got, want := string(shell.input), "\x1b[99;9u"; got != want {
		t.Fatalf("unbound Command+C sent %q, want %q", got, want)
	}
}
