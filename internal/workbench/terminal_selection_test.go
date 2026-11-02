package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
	chrome, _ := state.terminalChromeForSession(1)
	return state, &state.sessions[0], terminalLayoutWithChrome(size, cellWidth, cellHeight, chrome, padNormal.padding())
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
	from, to, selected := session.term.selection.span(session.term.screen.LineNumber(0), session.term.screen.Row(0), 20)
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, session, _ := selectionState()
			session.term.screen.Write([]byte("selected\r\n"))
			session.term.selection.selectRange(
				session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
				session.term.screen.PointAt(term.Position{Row: 0, Column: 3}),
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

func TestCopyOnSelectCopiesWhenTheMouseLetsGo(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("alpha beta\r\n"))
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 0, 4, state.cellWidth, state.cellHeight)
	drag := func() ui.EventResult {
		state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
		state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
		return state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	}

	if result := drag(); result.ClipboardText != "" {
		t.Fatalf("a selection was copied without the setting: %q", result.ClipboardText)
	}
	state.terminalCopyOnSelect = true
	if result := drag(); result.ClipboardText != "alpha" {
		t.Fatalf("selected text copied as %q", result.ClipboardText)
	}
	x, y := cellPoint(geometry, 0, 7, state.cellWidth, state.cellHeight)
	result := state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: hit, X: x, Y: y, Viewport: state.viewport})
	if result.ClipboardText != "beta" {
		t.Fatalf("a double-clicked word copied as %q", result.ClipboardText)
	}
	// A click that selects nothing leaves the clipboard alone.
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	result = state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: startY, Viewport: state.viewport})
	if result.ClipboardText != "" {
		t.Fatalf("a plain click copied %q", result.ClipboardText)
	}
}

func TestRightClickPastesOnlyWhenChosenAndShiftKeepsTheMenu(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	state.options.ClipboardText = func() (string, error) { return "ls -la", nil }
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 3, state.cellWidth, state.cellHeight)
	rightClick := func(modifiers ui.Modifiers) {
		state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Modifiers: modifiers, Target: hit, X: x, Y: y, Viewport: state.viewport})
	}

	rightClick(0)
	if !state.terminalMenu.open || len(shell.input) != 0 {
		t.Fatalf("by default a right click opens the menu: open=%v input=%q", state.terminalMenu.open, shell.input)
	}
	state.terminalMenu.open = false

	state.terminalRightClickPaste = true
	rightClick(0)
	if state.terminalMenu.open || string(shell.input) != "ls -la" {
		t.Fatalf("right click did not paste: open=%v input=%q", state.terminalMenu.open, shell.input)
	}
	rightClick(ui.ModifierShift)
	if !state.terminalMenu.open {
		t.Fatal("Shift+right click no longer reaches the menu")
	}
}

func fillHistory(t *testing.T, terminal *terminal, lines int) {
	t.Helper()
	for line := 0; line < lines; line++ {
		terminal.write([]byte(fmt.Sprintf("line %02d\r\n", line)))
	}
}

func TestDraggingBelowTheGridScrollsAndSelectsHistory(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	terminal.screen.Scroll(20)
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	first := terminal.screen.RowText(0)

	belowY := geometry.top + 5*state.cellHeight + 3*state.cellHeight
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: belowY, Viewport: state.viewport})
	if terminal.selection.edge.direction != -1 {
		t.Fatalf("a pointer below the grid does not ask to scroll: %+v", terminal.selection.edge)
	}
	before := terminal.screen.ViewOffset()
	for step := 1; step <= 3; step++ {
		state.stepSelectionScroll(terminal, time.Duration(step)*selectionScrollInterval)
	}
	if terminal.screen.ViewOffset() >= before {
		t.Fatalf("view stayed at %d", terminal.screen.ViewOffset())
	}
	state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: belowY, Viewport: state.viewport})

	if terminal.selection.edge.direction != 0 {
		t.Fatal("releasing the button left the auto-scroll armed")
	}
	copied := state.Key(ui.KeyEvent{Target: hit, Modifiers: ui.ModifierMeta, Text: "c"}).ClipboardText
	if !strings.HasPrefix(copied, strings.TrimSpace(first)) || strings.Count(copied, "\n") < 8 {
		t.Fatalf("selection covers only %q", copied)
	}
}

func TestDraggingAboveTheGridScrollsBackIntoHistory(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 4, 0, state.cellWidth, state.cellHeight)
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit, X: x, Y: y, Viewport: state.viewport})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: geometry.top - 2*state.cellHeight, Viewport: state.viewport})
	for step := 1; step <= 2; step++ {
		state.stepSelectionScroll(terminal, time.Duration(step)*selectionScrollInterval)
	}
	if terminal.screen.ViewOffset() == 0 {
		t.Fatal("view did not scroll back")
	}
	state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: geometry.top - 2*state.cellHeight, Viewport: state.viewport})
	copied := terminal.selection.text(terminal.screen)
	if strings.Count(copied, "\n") < 4 {
		t.Fatalf("selection lost the rows scrolled past: %q", copied)
	}
}

func TestDraggingInsideTheActiveGridDoesNotArmAutoScroll(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true,
		X: x + state.cellWidth, Y: y, Viewport: state.viewport,
	})
	if terminal.selection.edge.direction != 0 {
		t.Fatalf("a pointer inside the active grid armed auto-scroll: %+v", terminal.selection.edge)
	}
}

func TestScrollingKeepsTheSelectionOnItsText(t *testing.T) {
	state, session, _ := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	terminal.selection.selectRange(
		terminal.screen.PointAt(term.Position{Row: 1}),
		terminal.screen.PointAt(term.Position{Row: 2, Column: 5}),
	)
	want := terminal.selection.text(terminal.screen)
	state.Scroll(ui.ScrollEvent{DeltaY: -3 * state.cellHeight})
	if !terminal.selection.selected || terminal.selection.text(terminal.screen) != want {
		t.Fatalf("scrolling changed the selection: %q, want %q", terminal.selection.text(terminal.screen), want)
	}
}

// macOS reports a slow notch of a classic mouse wheel as a fraction of a line,
// which is a few pixels once scaled: every notch must still move a row.
func TestSlowMouseWheelNotchScrollsOneTerminalRow(t *testing.T) {
	state, session, geometry := selectionState()
	for line := 0; line < 40; line++ {
		session.term.screen.Write([]byte("line\r\n"))
	}
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	for notch := 1; notch <= 3; notch++ {
		state.Scroll(ui.ScrollEvent{X: x, Y: y, DeltaY: -1, Viewport: state.viewport, Target: hitTerminalList.At(int(session.ID))})
		if got := session.term.screen.ViewOffset(); got != notch {
			t.Fatalf("after %d slow notches the view is %d rows back", notch, got)
		}
	}
}
