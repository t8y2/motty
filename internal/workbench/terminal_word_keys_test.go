package workbench

import (
	"runtime"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestTerminalOptionWordKeysRespectPlatformAndKeyboardModes(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    string
		event   gfx.KeyEvent
		want    string
		macWant string
	}{
		{"right", "", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt}, "\x1b[1;3C", "\x1bf"},
		{"left", "", gfx.KeyEvent{Key: gfx.KeyLeft, Modifiers: gfx.ModifierAlt}, "\x1b[1;3D", "\x1bb"},
		{"application cursor", "\x1b[?1h", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt}, "\x1b[1;3C", "\x1bf"},
		{"alternate screen", "\x1b[?1049h", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt}, "\x1b[1;3C", ""},
		{"kitty", "\x1b[=1u", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt}, "\x1b[1;3C", ""},
		{"kitty event types", "\x1b[=3u", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt}, "\x1b[1;3:1C", ""},
		{"kitty release", "\x1b[=3u", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt, Release: true}, "\x1b[1;3:3C", ""},
		{"legacy release", "", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt, Release: true}, "", ""},
		{"shift option", "", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierShift | gfx.ModifierAlt}, "\x1b[1;4C", ""},
		{"control", "", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierControl}, "\x1b[1;5C", ""},
		{"control option", "", gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierControl | gfx.ModifierAlt}, "\x1b[1;7C", ""},
		{"plain arrow", "", gfx.KeyEvent{Key: gfx.KeyRight}, "\x1b[C", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			shell.say(test.mode)
			state.Wake()
			before := len(shell.typed())
			key(state, 0, test.event)
			want := test.want
			if runtime.GOOS == "darwin" && test.macWant != "" {
				want = test.macWant
			}
			if got := shell.typed()[before:]; got != want {
				t.Fatalf("key sent %q, want %q", got, want)
			}
		})
	}
}

func TestTerminalOptionRightLeavesShellSuggestionsToTheShell(t *testing.T) {
	h := ghostWorld(t, "mvn --version")
	h.prompt("macbookpro:~ » ")
	h.type_("mv")
	h.shell.say("\x1b[90mn --version\x1b[0m\x1b[11D")
	h.state.Wake()
	if rest, offered := h.ghost(); offered {
		t.Fatalf("Motty offered %q beside the shell's own suggestion", rest)
	}
	h.echo = false
	before := len(h.shell.typed())
	h.key(gfx.KeyEvent{Key: gfx.KeyRight, Modifiers: gfx.ModifierAlt})
	want := "\x1b[1;3C"
	if runtime.GOOS == "darwin" {
		want = "\x1bf"
	}
	if got := h.shell.typed()[before:]; got != want {
		t.Fatalf("shell suggestion received %q, want word movement %q", got, want)
	}
}
