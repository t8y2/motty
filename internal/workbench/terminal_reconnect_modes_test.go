package workbench

import (
	"motty/internal/term"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A connection that drops takes the running program's teardown with it, so the
// modes it armed are still on the screen the next shell inherits: the wheel
// walks the line editor's history and every pointer move types at the prompt.
// The shell the reconnect opens is a new process and never asked for any of
// them, which is the proof available even when the session goes straight into
// tmux and draws no prompt marker of its own.
func TestAReconnectedShellTakesBackTheModesALostConnectionLeft(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	session := &state.sessions[0]
	screen := session.term.screen
	_, rows := screen.Size()
	for line := 0; line < rows+10; line++ {
		screen.Write([]byte("line\r\n"))
	}
	// A full-screen program had the screen and the mouse when the link dropped.
	screen.Write([]byte("\x1b[?1049h\x1b[?1003h\x1b[?1006h\x1b[?1004h\x1b[?1h\x1b[?25l"))
	if modes := screen.Modes(); !modes.AltScreen || modes.Mouse != term.MouseAny {
		t.Fatalf("the program's modes were not taken: %+v", modes)
	}
	// The connection is gone, and reconnecting opens a shell in the same tab.
	shell.Close()
	session.term.shell = nil
	state.openShell(session)
	reconnected := fake.shell(t)
	if reconnected == shell {
		t.Fatal("the reconnect reused the dead shell")
	}
	if modes := screen.Modes(); modes.AltScreen || modes.Mouse != term.MouseOff {
		t.Fatalf("the new shell inherited a dead program's modes: %+v", modes)
	}

	hit := hitTerminalList.At(int(session.ID))
	geometry := state.layout()
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: x, Y: y, Target: hit, Viewport: state.viewport,
	}); reconnected.typed() != "" {
		t.Fatalf("moving the pointer after a reconnect typed %q (result=%+v)", reconnected.typed(), result)
	}
	result := state.Scroll(gfx.ScrollEvent{
		X: x, Y: y, DeltaY: -state.cellHeight,
		Viewport: state.viewport, Target: hit, Precise: true,
	})
	if !result.Handled || !result.Repaint || reconnected.typed() != "" || screen.ViewOffset() == 0 {
		t.Fatalf("wheel after a reconnect: result=%+v input=%q offset=%d",
			result, reconnected.typed(), screen.ViewOffset())
	}
}
