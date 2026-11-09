package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func inputClickState(t *testing.T) (*State, *Session, *fakeShell, terminalGeometry) {
	t.Helper()
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.commandReporting = true
	session.term.screen.SetPromptToken("t")
	session.term.screen.Write([]byte("\x1b]133;A;motty=t\a$ \x1b]133;B;motty=t\ahello")) // cursor at column 7
	return state, session, shell, geometry
}

func clickTerminal(state *State, session *Session, geometry terminalGeometry, column int, modifiers gfx.Modifiers) {
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, column, state.cellWidth, state.cellHeight)
	for _, kind := range []gfx.PointerEventKind{gfx.PointerDown, gfx.PointerUp} {
		state.Pointer(gfx.PointerEvent{
			Kind: kind, Button: gfx.PointerButtonPrimary, Target: hit, Captured: kind == gfx.PointerUp,
			Modifiers: modifiers, ClickCount: 1, X: x, Y: y, Viewport: state.viewport,
		})
	}
}

func TestOptionClickMovesTheShellCursorWithinTheInputLine(t *testing.T) {
	state, session, shell, geometry := inputClickState(t)
	clickTerminal(state, session, geometry, 3, gfx.ModifierAlt)
	if got, want := shell.typed(), "\x1b[D\x1b[D\x1b[D\x1b[D"; got != want {
		t.Fatalf("click left sent %q, want %q", got, want)
	}
	session.term.screen.Write([]byte("\x1b[4D\x1b[?1h")) // the cursor follows; a program wants application keys
	clickTerminal(state, session, geometry, 5, gfx.ModifierAlt)
	if got, want := shell.typed(), "\x1b[D\x1b[D\x1b[D\x1b[D\x1bOC\x1bOC"; got != want {
		t.Fatalf("click right sent %q, want %q", got, want)
	}
}

func TestPlainClickMovesTheCursorButADragDoesNot(t *testing.T) {
	state, session, shell, geometry := inputClickState(t)
	clickTerminal(state, session, geometry, 5, 0)
	if got, want := shell.typed(), "\x1b[D\x1b[D"; got != want {
		t.Fatalf("click sent %q, want %q", got, want)
	}
	hit := hitTerminalList.At(int(session.ID))
	x0, y := cellPoint(geometry, 0, 2, state.cellWidth, state.cellHeight)
	x1, _ := cellPoint(geometry, 0, 6, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, ClickCount: 1, X: x0, Y: y, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, Captured: true, X: x1, Y: y, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x1, Y: y, Viewport: state.viewport})
	if got := shell.typed(); got != "\x1b[D\x1b[D" {
		t.Fatalf("a drag sent keys: %q", got)
	}
}

func TestOptionClickLeavesTheKeyboardAloneWhereItCannotPlaceTheCursor(t *testing.T) {
	state, session, shell, geometry := inputClickState(t)
	clickTerminal(state, session, geometry, 3, gfx.ModifierAlt|gfx.ModifierShift)
	clickTerminal(state, session, geometry, 3, gfx.ModifierControl)
	clickTerminal(state, session, geometry, 0, gfx.ModifierAlt) // the prompt
	if shell.typed() != "" {
		t.Fatalf("sent %q", shell.typed())
	}
	session.term.commandReporting = false // a shell that never reports commands may be running one
	clickTerminal(state, session, geometry, 3, gfx.ModifierAlt)
	session.term.commandReporting = true
	session.term.screen.Write([]byte("\x1b]133;C;motty=t\a")) // a command is running
	clickTerminal(state, session, geometry, 3, gfx.ModifierAlt)
	if shell.typed() != "" {
		t.Fatalf("sent %q", shell.typed())
	}
}
