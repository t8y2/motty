//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
)

func TestTerminalCommandKeysNegotiation(t *testing.T) {
	state, _, _, shell := liveSession(t)
	// Bind explicitly so the same shortcut contract is checked on every OS.
	if _, ok := state.shortcuts.bind(shortcutCopy, keymap.Chord{Modifiers: gfx.ModifierMeta, Char: 'c'}); !ok {
		t.Fatal("could not bind Command+C")
	}
	// The files path default is the platform's primary modifier; bind it to
	// Command here too, so the bound-chord rule is checked on every OS.
	if _, ok := state.shortcuts.bind(shortcutFilesPath, keymap.Chord{Modifiers: gfx.ModifierMeta, Char: 'l'}); !ok {
		t.Fatal("could not bind Command+L")
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
	optionRight, optionLeft := "\x1b[1;3C", "\x1b[1;3D"
	if runtime.GOOS == "darwin" {
		optionRight, optionLeft = "\x1bf", "\x1bb"
	}
	for _, test := range []struct {
		name  string
		flags uint32
		chord string
		want  string
	}{
		{"legacy", 0, "cmd+shift+left", ""},
		{"legacy option right", 0, "alt+right", optionRight},
		{"legacy option left", 0, "alt+left", optionLeft},
		{"kitty option right", 1, "alt+right", "\x1b[1;3C"},
		{"kitty option left", 1, "alt+left", "\x1b[1;3D"},
		{"kitty", 1, "cmd+shift+left", "\x1b[1;10D"},
		{"all keys", 8, "cmd+shift+right", "\x1b[1;10C"},
		{"printable", 1, "cmd+j", "\x1b[106;9u"},
		{"copy stays local", 8, "cmd+c", ""},
		{"unbound modified copy", 1, "cmd+alt+c", "\x1b[99;11u"},
		{"negotiation disabled", 0, "cmd+j", ""},
		{"bound but not applicable", 1, "cmd+l", ""},
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
	// One chord that asks for event types is reported as a press/release pair, so
	// the wiring that hands releases back to the encoder stays covered. The
	// release only exists once the terminal has the keyboard: the tab click puts
	// focus on the tab, and a text input asked for releases only while focused.
	t.Run("press and release", func(t *testing.T) {
		shell.say("\x1b[=3u")
		state.Wake()
		if err := session.Click(700, 400); err != nil {
			t.Fatal(err)
		}
		if err := session.Wait(20 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		driver := session.Driver()
		shell.input = nil
		if err := driver.KeyDown(gfx.KeyMetaLeft, ""); err != nil {
			t.Fatal(err)
		}
		if err := driver.KeyDown(gfx.KeyUnknown, "j"); err != nil {
			t.Fatal(err)
		}

		if err := driver.KeyUp(gfx.KeyUnknown, "j"); err != nil {
			t.Fatal(err)
		}

		if err := driver.KeyUp(gfx.KeyMetaLeft, ""); err != nil {
			t.Fatal(err)
		}
		if got, want := string(shell.input), "\x1b[106;9:1u\x1b[106;9:3u"; got != want {
			t.Fatalf("Command+J press/release sent %q, want %q", got, want)
		}
	})

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
