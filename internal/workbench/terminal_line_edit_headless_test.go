//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

// Command+Left, Command+Right and Command+Backspace used to be swallowed as
// application chords, so a Mac user could not jump to either end of the command
// line or clear it. They now reach the program as the Control chords they stand
// for (Ctrl+A, Ctrl+E, Ctrl+U), in whichever keyboard mode the program is in,
// full-screen programs included. The chords that drive tabs and copying stay
// local.
func TestTerminalCommandLineEditKeysReachTheProgram(t *testing.T) {
	const alternateScreen = "\x1b[?1049h"
	legacy := [3]string{"\x01", "\x05", "\x15"}
	kitty := [3]string{"\x1b[97;5u", "\x1b[101;5u", "\x1b[117;5u"}
	kittyPress := [3]string{"\x1b[97;5:1u", "\x1b[101;5:1u", "\x1b[117;5:1u"}
	for _, test := range []struct {
		name   string
		say    string // what the program asks of the terminal before the keys
		screen bool   // whether that leaves the alternate screen active
		flags  uint32 // the kitty keyboard flags that leaves active
		want   [3]string
	}{
		{"shell with the legacy keyboard", "", false, 0, legacy},
		{"shell with kitty disambiguation", "\x1b[=1u", false, 1, kitty},
		{"shell with kitty event types", "\x1b[=3u", false, 3, kittyPress},
		{"full-screen program with the legacy keyboard", alternateScreen, true, 0, legacy},
		{"full-screen program with kitty disambiguation", alternateScreen + "\x1b[=1u", true, 1, kitty},
		{"full-screen program that reports every key", alternateScreen + "\x1b[=8u", true, 8, kitty},
		{"full-screen program that reports every key and its event types", alternateScreen + "\x1b[=11u", true, 11, kittyPress},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			if test.say != "" {
				shell.say(test.say)
				state.Wake()
			}
			screen := state.sessions[0].term.screen
			if screen.Modes().AltScreen != test.screen || screen.KeyboardFlags() != test.flags {
				t.Fatalf("alternate screen = %v, keyboard flags = %d; want %v and %d", screen.Modes().AltScreen, screen.KeyboardFlags(), test.screen, test.flags)
			}
			session, err := headless.Open(app.Config{
				Title: "terminal line edit", Width: 1400, Height: 900,
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
			for i, chord := range []string{"cmd+left", "cmd+right", "cmd+backspace"} {
				shell.input = nil
				if err := session.Keys(chord); err != nil {
					t.Fatalf("%s: %v", chord, err)
				}
				if got := string(shell.input); got != test.want[i] {
					t.Errorf("%s sent %q, want %q", chord, got, test.want[i])
				}
			}
			// Further modifiers and other keys are not line edits: nothing is sent.
			for _, chord := range []string{"cmd+shift+left", "cmd+alt+backspace", "cmd+up", "cmd+delete"} {
				shell.input = nil
				if err := session.Keys(chord); err != nil {
					t.Fatalf("%s: %v", chord, err)
				}
				if got := string(shell.input); got != "" {
					t.Errorf("%s sent %q, want nothing", chord, got)
				}
			}
		})
	}
}
