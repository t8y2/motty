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

// A local console shell asks for win32 input records through ConPTY, which is
// how Git Bash and Cygwin receive their keys on Windows. That is a transport,
// not a claim on the terminal's own suggestion key: Right at a live suggestion
// still accepts it, the badge still names the key, and the program is not sent
// the release of a press the terminal consumed.
func TestSuggestionRightIsAcceptedWhileWin32InputIsOn(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.type_("git ch")
	h.shell.say("\x1b[?9001h")
	h.state.Wake()
	if !h.state.sessions[0].term.screen.Modes().Win32Input {
		t.Fatal("win32 input mode was not set")
	}
	if rest, ok := h.ghost(); !ok || rest != "eckout main" {
		t.Fatalf("suggestion = %q, %v", rest, ok)
	}
	if _, _, ok := paintGhostGrid(t, h.state, &h.state.sessions[0]); !ok {
		t.Fatal("the accept badge is missing while win32 input is on")
	}
	before := len(h.shell.input)
	press := gfx.KeyEvent{Key: gfx.KeyRight, Win32: gfx.Win32Key{VirtualKey: 39, ScanCode: 0x4d}}
	h.key(press)
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the shell got %q, want the rest of the command and no key packet", got)
	}
	h.key(gfx.KeyEvent{Key: gfx.KeyRight, Release: true, Win32: gfx.Win32Key{VirtualKey: 39, ScanCode: 0x4d}})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the release of the accepted press reached the shell: %q", got)
	}
}
