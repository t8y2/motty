package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

func selectionState() (*State, *Session, terminalGeometry) {
	const cellWidth, cellHeight = float32(8), float32(16)
	size := ui.Size{
		Width:  2*termPadX + 20*cellWidth,
		Height: windowBarHeight + infoBarHeight + termPadTop + 5*cellHeight + termPadBottom,
	}
	state := &State{
		viewport: size, cellWidth: cellWidth, cellHeight: cellHeight,
		activeSection: sectionTerminal, pal: &darkPalette,
	}
	live := newTerminal(20, 5)
	live.live = true // a shell has spoken: the terminal is what the window shows
	state.sessions = []Session{{ID: 1, Address: "test", term: live}}
	state.activeSessionID = 1
	return state, &state.sessions[0], terminalLayout(size, cellWidth, cellHeight, padNormal.padding())
}

func cellPoint(geometry terminalGeometry, row, column int, cellWidth, cellHeight float32) (float32, float32) {
	return geometry.left + (float32(column)+0.5)*cellWidth, geometry.top + (float32(row)+0.5)*cellHeight
}

func TestDraggingTerminalCellsSelectsAndCopiesText(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("alpha beta\r\ngamma\r\n"))
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 2, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 1, 2, state.cellWidth, state.cellHeight)

	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit,
		X: startX, Y: startY, Viewport: state.viewport,
	}); !result.Handled {
		t.Fatalf("down = %+v", result)
	}
	if result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	}); !result.Handled || !result.Repaint {
		t.Fatalf("move = %+v", result)
	}
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})

	result := state.Key(ui.KeyEvent{Target: hit, Modifiers: ui.ModifierMeta, Text: "c"})
	if !result.Handled || result.Repaint || result.ClipboardText != "pha beta\ngam" {
		t.Fatalf("Command+C = %+v", result)
	}
	result = state.Key(ui.KeyEvent{Target: hit, Modifiers: ui.ModifierControl | ui.ModifierShift, Text: "c"})
	if result.ClipboardText != "pha beta\ngam" {
		t.Fatalf("Control+Shift+C = %+v", result)
	}
}

func TestDoubleClickSelectsAUnicodeWord(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("name 世界 next"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 6, state.cellWidth, state.cellHeight)
	result := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("double click = %+v", result)
	}
	if copied := state.Key(ui.KeyEvent{Target: hit, Modifiers: ui.ModifierMeta, Text: "c"}).ClipboardText; copied != "世界" {
		t.Fatalf("copied %q", copied)
	}
	from, to, selected := session.term.selection.span(0, session.term.screen.Row(0), 20)
	if !selected || from != 5 || to != 9 {
		t.Fatalf("selection span = %d..%d, %v", from, to, selected)
	}
}

func TestTerminalChangesClearASelection(t *testing.T) {
	tests := []struct {
		name   string
		change func(*State, *terminal)
	}{
		{"output", func(_ *State, terminal *terminal) { terminal.write([]byte("new")) }},
		{"input", func(_ *State, terminal *terminal) { terminal.shell = &fakeShell{}; terminal.send([]byte("x")) }},
		{"resize", func(state *State, _ *terminal) { state.Resize(ui.Size{Width: 400, Height: 300}) }},
		{"scroll", func(state *State, _ *terminal) { state.Scroll(ui.ScrollEvent{DeltaY: -state.cellHeight}) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, session, _ := selectionState()
			session.term.screen.Write([]byte("selected\r\n"))
			session.term.selection.selectRange(
				term.Position{Row: 0, Column: 0},
				term.Position{Row: 0, Column: 3},
			)
			test.change(state, session.term)
			if session.term.selection.selected {
				t.Fatal("selection survived the terminal change")
			}
		})
	}
}

func TestTerminalMouseReportingSendsCellEvents(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 1, 2, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 3, 5, state.cellWidth, state.cellHeight)

	down := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit,
		Modifiers: ui.ModifierControl, X: startX, Y: startY, Viewport: state.viewport,
	})
	if !down.Handled || shell.typed() != "\x1b[<16;3;2M" {
		t.Fatalf("down=%+v input=%q", down, shell.typed())
	}
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Target: hit, Captured: true,
		X: startX, Y: startY, Viewport: state.viewport,
	})
	if shell.typed() != "\x1b[<16;3;2M" {
		t.Fatalf("same-cell motion sent %q", shell.typed())
	}
	move := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Target: hit, Captured: true,
		Modifiers: ui.ModifierMeta, X: endX, Y: endY, Viewport: state.viewport,
	})
	up := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})
	if !move.Handled || !up.Handled || shell.typed() != "\x1b[<16;3;2M\x1b[<40;6;4M\x1b[<0;6;4m" {
		t.Fatalf("move=%+v up=%+v input=%q", move, up, shell.typed())
	}
}

func TestTerminalPointerExitReleasesRemoteDragAtLastCell(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 2, 4, state.cellWidth, state.cellHeight)
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	result := state.Pointer(ui.PointerEvent{Kind: ui.PointerExit, Target: hit, Captured: true})
	if !result.Handled || session.term.mouseButton != term.MouseButtonNone || shell.typed() != "\x1b[<0;5;3M\x1b[<0;5;3m" {
		t.Fatalf("result=%+v button=%d input=%q", result, session.term.mouseButton, shell.typed())
	}
}

func TestShiftBypassesTerminalMouseReportingForSelection(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.write([]byte("alpha beta"))
	session.term.screen.Write([]byte("\x1b[?1003h\x1b[?1006h"))
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 1, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 0, 4, state.cellWidth, state.cellHeight)

	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit,
		Modifiers: ui.ModifierShift, X: startX, Y: startY, Viewport: state.viewport,
	})
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})
	if shell.typed() != "" || !session.term.selection.selected {
		t.Fatalf("input=%q selection=%+v", shell.typed(), session.term.selection)
	}
}

func TestTerminalAnyMotionReportsOnlyNewCells(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1003h\x1b[?1015h"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	event := ui.PointerEvent{Kind: ui.PointerMove, Target: hit, X: x, Y: y, Viewport: state.viewport}
	if result := state.Pointer(event); !result.Handled {
		t.Fatalf("motion = %+v", result)
	}
	state.Pointer(event)
	if shell.typed() != "\x1b[67;8;3M" {
		t.Fatalf("input=%q", shell.typed())
	}
}

func TestTerminalMouseWheelPrecedesAlternateScroll(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	session.term.screen.Write([]byte("\x1b[?1049h\x1b[?1007h\x1b[?1002h\x1b[?1006h"))
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	result := state.Scroll(ui.ScrollEvent{
		Modifiers: ui.ModifierControl,
		X:         x, Y: y, DeltaX: state.cellWidth, DeltaY: -state.cellHeight,
		Viewport: state.viewport, Target: hitTerminalList.At(int(session.ID)), Precise: true,
	})
	if !result.Handled || result.Repaint || shell.typed() != "\x1b[<80;8;3M\x1b[<83;8;3M" {
		t.Fatalf("result=%+v input=%q", result, shell.typed())
	}
}

func TestShiftWheelBypassesMouseReportingForScrollback(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	for line := 0; line < 20; line++ {
		session.term.screen.Write([]byte("line\r\n"))
	}
	session.term.screen.Write([]byte("\x1b[?1003h\x1b[?1006h"))
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	result := state.Scroll(ui.ScrollEvent{
		Modifiers: ui.ModifierShift,
		X:         x, Y: y, DeltaY: -state.cellHeight,
		Viewport: state.viewport, Target: hitTerminalList.At(int(session.ID)), Precise: true,
	})
	if !result.Handled || !result.Repaint || shell.typed() != "" || session.term.screen.ViewOffset() != 1 {
		t.Fatalf("result=%+v input=%q offset=%d", result, shell.typed(), session.term.screen.ViewOffset())
	}
}
