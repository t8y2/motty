package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestTerminalUsesTheKittyKeyboardProtocolTheProgramAsksFor(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[=1u")
	state.Wake()
	shell.input = nil

	key(state, 0, gfx.KeyEvent{Key: gfx.KeyEscape})
	key(state, 0, gfx.KeyEvent{Modifiers: gfx.ModifierAlt, Text: "x"})
	want := "\x1b[27u" + "\x1b[120;3u"
	if got := string(shell.input); got != want {
		t.Fatalf("kitty input = %q, want %q", got, want)
	}

	hit := terminalHit(state, 0)
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input == nil || !input.KeyReleases {
		t.Fatalf("terminal input does not ask for raw keys: %+v", input)
	}

	shell.say("\x1b[=0u")
	state.Wake()
	shell.input = nil
	key(state, 0, gfx.KeyEvent{Key: gfx.KeyEscape})
	key(state, 0, gfx.KeyEvent{Modifiers: gfx.ModifierAlt, Text: "x"})
	if got := string(shell.input); got != "\x1b\x1bx" {
		t.Fatalf("legacy input = %q", got)
	}
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input == nil || input.KeyReleases {
		t.Fatalf("terminal input still asks for raw keys: %+v", input)
	}
}
