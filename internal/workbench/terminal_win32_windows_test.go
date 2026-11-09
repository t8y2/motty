//go:build windows

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestWin32InputModeSendsTheConsolePacket(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[?9001h")
	state.Wake()
	shell.input = nil

	key(state, 0, gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', ScanCode: 0x1e, Unicode: 'a', Repeat: 2}})
	if got, want := string(shell.input), "\x1b[65;30;97;1;0;2_"; got != want {
		t.Fatalf("win32 packet = %q, want %q", got, want)
	}
	hit := terminalHit(state, 0)
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input == nil || !input.KeyReleases {
		t.Fatalf("terminal input does not ask for raw keys: %+v", input)
	}
}
