package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func nativeSpaceKey() ui.KeyEvent {
	return ui.KeyEvent{Key: ui.KeySpace, Win32: ui.Win32Key{
		VirtualKey: 32, ScanCode: 0x39, Unicode: ' ', Repeat: 1,
	}}
}

func TestNativeSpaceIsSentOnceAcrossTerminalProtocols(t *testing.T) {
	for _, test := range []struct {
		name, mode, press, release string
	}{
		{name: "legacy", press: " "},
		{name: "kitty text", mode: "\x1b[=1u", press: " "},
		{name: "kitty all keys", mode: "\x1b[=10u", press: "\x1b[32;1:1u", release: "\x1b[32;1:3u"},
		{name: "win32", mode: "\x1b[?9001h", press: "\x1b[32;57;32;1;0;1_", release: "\x1b[32;57;32;0;0;1_"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			shell.say(test.mode)
			state.Wake()
			shell.input = nil
			input := state.TerminalInput(app.TextInputContext{ID: terminalHit(state, 0), Terminal: true})
			for range 2 {
				key(state, 0, nativeSpaceKey())
				input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
				if input.KeyReleases {
					release := nativeSpaceKey()
					release.Release = true
					key(state, 0, release)
				}
			}
			want := test.press + test.release + test.press + test.release
			if got := shell.typed(); got != want {
				t.Fatalf("two Space presses sent %q, want %q", got, want)
			}
			if test.name == "legacy" {
				shell.say("a" + shell.typed() + "b")
				state.Wake()
				if cursor := state.sessions[0].term.screen.Cursor(); cursor.X != 4 {
					t.Fatalf("echo cursor column = %d, want 4 for a  b", cursor.X)
				}
			}
		})
	}
}

func TestNativeSpaceKeepsIndependentTextInput(t *testing.T) {
	for _, test := range []struct {
		name string
		next func(*State, *app.TextInput)
		want string
	}{
		{"consecutive text spaces", func(_ *State, input *app.TextInput) {
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
		}, "  "},
		{"paste", func(_ *State, input *app.TextInput) {
			input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: "  "})
		}, "  "},
		{"IME commit", func(_ *State, input *app.TextInput) {
			input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "x"})
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
		}, " "},
		{"fullwidth space", func(_ *State, input *app.TextInput) {
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "\u3000"})
		}, "\u3000"},
		{"another key", func(state *State, input *app.TextInput) {
			key(state, 0, ui.KeyEvent{Key: ui.KeyEnter})
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
		}, "\r "},
		{"key release", func(state *State, input *app.TextInput) {
			release := nativeSpaceKey()
			release.Release = true
			key(state, 0, release)
			input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
		}, " "},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			key(state, 0, nativeSpaceKey())
			input := state.TerminalInput(app.TextInputContext{ID: terminalHit(state, 0), Terminal: true})
			if test.name == "consecutive text spaces" {
				input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: " "})
			}
			shell.input = nil
			test.next(state, input)
			if got := shell.typed(); got != test.want {
				t.Fatalf("independent input sent %q, want %q", got, test.want)
			}
		})
	}
}

func TestSpaceChordsKeepTheirTerminalMeaning(t *testing.T) {
	state, _, _, shell := liveSession(t)
	for _, test := range []struct {
		name string
		mods ui.Modifiers
		want string
	}{
		{"Shift", ui.ModifierShift, " "},
		{"Control", ui.ModifierControl, "\x00"},
		{"Alt", ui.ModifierAlt, "\x1b "},
	} {
		t.Run(test.name, func(t *testing.T) {
			shell.input = nil
			event := nativeSpaceKey()
			event.Modifiers = test.mods
			key(state, 0, event)
			if test.mods == ui.ModifierShift {
				state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: " "})
			}
			if got := shell.typed(); got != test.want {
				t.Fatalf("%s+Space sent %q, want %q", test.name, got, test.want)
			}
		})
	}
}

func TestRejectedSpaceKeyKeepsItsTextFallback(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.refuse = true
	key(state, 0, nativeSpaceKey())
	shell.refuse = false
	state.terminalText(state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: " "})
	if got := shell.typed(); got != " " {
		t.Fatalf("Space after a rejected key sent %q, want one space", got)
	}
}
