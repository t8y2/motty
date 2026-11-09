package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/term"
)

func selectionState() (*State, *Session, terminalGeometry) {
	const cellWidth, cellHeight = float32(8), float32(16)
	size := gfx.Size{
		Width:  2*termPadX + 20*cellWidth,
		Height: windowBarHeight + infoBarHeight + termPadTop + 5*cellHeight + termPadBottom,
	}
	state := &State{
		viewport: size, cellWidth: cellWidth, cellHeight: cellHeight,
		activeSection: sectionTerminal, pal: &darkPalette,
		shortcuts: defaultShortcuts(),
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

	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		X: startX, Y: startY, Viewport: state.viewport,
	}); !result.Handled {
		t.Fatalf("down = %+v", result)
	}
	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	}); !result.Handled || !result.Repaint {
		t.Fatalf("move = %+v", result)
	}
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})

	result := state.Shortcut(gfx.KeyEvent{Target: hit, Modifiers: keymap.Primary, Text: "c"})
	if !result.Handled || result.Repaint || result.ClipboardText != "pha beta\ngam" {
		t.Fatalf("copy shortcut = %+v", result)
	}
	result = state.Key(gfx.KeyEvent{Target: hit, Modifiers: gfx.ModifierControl | gfx.ModifierShift, Text: "c"})
	if result.ClipboardText != "pha beta\ngam" {
		t.Fatalf("Control+Shift+C = %+v", result)
	}
}

func TestDoubleClickSelectsAUnicodeWord(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("name 世界 next"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 6, state.cellWidth, state.cellHeight)
	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	if !result.Handled || !result.Repaint {
		t.Fatalf("double click = %+v", result)
	}
	if copied := state.Shortcut(gfx.KeyEvent{Target: hit, Modifiers: keymap.Primary, Text: "c"}).ClipboardText; copied != "世界" {
		t.Fatalf("copied %q", copied)
	}
	from, to, selected := session.term.selection.span(session.term.screen.LineNumber(0), session.term.screen.Row(0), 20)
	if !selected || from != 5 || to != 9 {
		t.Fatalf("selection span = %d..%d, %v", from, to, selected)
	}
}

func TestWordSeparatorsApplyToDoubleClickAndWordDrag(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("path/to-file other"))
	state.terminalWordSeparators = "-"
	gesture(state, session, geometry, 2, [2]int{0, 1})
	if got := selectedText(session); got != "path/to" {
		t.Fatalf("double click selected %q", got)
	}
	gesture(state, session, geometry, 2, [2]int{0, 1}, [2]int{0, 9})
	if got := selectedText(session); got != "path/to-file" {
		t.Fatalf("word drag selected %q", got)
	}
	state.terminalWordSeparators = "-/"
	gesture(state, session, geometry, 2, [2]int{0, 1})
	if got := selectedText(session); got != "path" {
		t.Fatalf("updated separators selected %q", got)
	}
}

func TestTerminalPointerClampsToTheScreensCurrentRows(t *testing.T) {
	tests := []struct {
		name       string
		x, y       func(terminalGeometry, float32, float32) float32
		wantRow    func(int) int
		wantColumn func(int) int
	}{
		{
			name: "below",
			x: func(geometry terminalGeometry, cellWidth, _ float32) float32 {
				return geometry.left + 2.5*cellWidth
			},
			y: func(geometry terminalGeometry, _, cellHeight float32) float32 {
				return geometry.top + 19.5*cellHeight
			},
			wantRow:    func(rows int) int { return rows - 1 },
			wantColumn: func(int) int { return 2 },
		},
		{
			name: "right",
			x: func(geometry terminalGeometry, cellWidth, _ float32) float32 {
				return geometry.left + 39.5*cellWidth
			},
			y: func(geometry terminalGeometry, _, cellHeight float32) float32 {
				return geometry.top + 1.5*cellHeight
			},
			wantRow:    func(int) int { return 1 },
			wantColumn: func(cols int) int { return cols - 1 },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, session, _ := selectionState()
			state.viewport = gfx.Size{
				Width:  state.viewport.Width + 24*state.cellWidth,
				Height: state.viewport.Height + 24*state.cellHeight,
			}
			geometry := state.terminalLayoutForEvent(session.ID, state.viewport)
			hit := hitTerminalList.At(int(session.ID))
			cols, rows := session.term.screen.Size()

			result := state.Pointer(gfx.PointerEvent{
				Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
				X: test.x(geometry, state.cellWidth, state.cellHeight),
				Y: test.y(geometry, state.cellWidth, state.cellHeight), Viewport: state.viewport,
			})

			if !result.Handled {
				t.Fatalf("pointer outside the terminal = %+v", result)
			}
			want := session.term.screen.PointAt(term.Position{
				Row: test.wantRow(rows), Column: test.wantColumn(cols),
			})
			if session.term.selection.anchor != want || !session.term.selection.dragging {
				t.Fatalf("selection anchor = %+v, want %+v", session.term.selection.anchor, want)
			}
		})
	}
}

func TestTerminalPointerReturnsTheOriginForAnEmptyScreen(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.screen.Resize(0, 0)
	position := state.terminalPositionAt(session, state.viewport,
		geometry.left+5*state.cellWidth, geometry.top+3*state.cellHeight)
	if position != (term.Position{}) {
		t.Fatalf("empty screen pointer = %+v", position)
	}
}

func TestTerminalChangesClearASelection(t *testing.T) {
	tests := []struct {
		name   string
		change func(*State, *terminal)
	}{
		{"output", func(_ *State, terminal *terminal) { terminal.write([]byte("new")) }},
		{"input", func(_ *State, terminal *terminal) { terminal.shell = &fakeShell{}; terminal.send([]byte("x")) }},
		{"resize", func(state *State, _ *terminal) { state.Resize(gfx.Size{Width: 400, Height: 300}) }},
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

	down := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		Modifiers: gfx.ModifierControl, X: startX, Y: startY, Viewport: state.viewport,
	})
	if !down.Handled || shell.typed() != "\x1b[<16;3;2M" {
		t.Fatalf("down=%+v input=%q", down, shell.typed())
	}
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Target: hit, Captured: true,
		X: startX, Y: startY, Viewport: state.viewport,
	})
	if shell.typed() != "\x1b[<16;3;2M" {
		t.Fatalf("same-cell motion sent %q", shell.typed())
	}
	move := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Target: hit, Captured: true,
		Modifiers: gfx.ModifierMeta, X: endX, Y: endY, Viewport: state.viewport,
	})
	up := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
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
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit, Target: hit, Captured: true})
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

	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		Modifiers: gfx.ModifierShift, X: startX, Y: startY, Viewport: state.viewport,
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Target: hit, Captured: true,
		X: endX, Y: endY, Viewport: state.viewport,
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
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
	event := gfx.PointerEvent{Kind: gfx.PointerMove, Target: hit, X: x, Y: y, Viewport: state.viewport}
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
	result := state.Scroll(gfx.ScrollEvent{
		Modifiers: gfx.ModifierControl,
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
	result := state.Scroll(gfx.ScrollEvent{
		Modifiers: gfx.ModifierShift,
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
	drag := func() gfx.EventResult {
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
		return state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	}

	if result := drag(); result.ClipboardText != "" {
		t.Fatalf("a selection was copied without the setting: %q", result.ClipboardText)
	}
	state.terminalCopyOnSelect = true
	if result := drag(); result.ClipboardText != "alpha" {
		t.Fatalf("selected text copied as %q", result.ClipboardText)
	}
	x, y := cellPoint(geometry, 0, 7, state.cellWidth, state.cellHeight)
	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: hit, X: x, Y: y, Viewport: state.viewport})
	if result.ClipboardText != "beta" {
		t.Fatalf("a double-clicked word copied as %q", result.ClipboardText)
	}
	// A click that selects nothing leaves the clipboard alone.
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	result = state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: startY, Viewport: state.viewport})
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
	rightClick := func(modifiers gfx.Modifiers) {
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Modifiers: modifiers, Target: hit, X: x, Y: y, Viewport: state.viewport})
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
	rightClick(gfx.ModifierShift)
	if !state.terminalMenu.open {
		t.Fatal("Shift+right click no longer reaches the menu")
	}
}

func TestRightClickCopiesSelectionInsteadOfPasting(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	session.term.screen.Write([]byte("hello world"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 4}),
	)
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.options.ClipboardText = func() (string, error) { return "paste", nil }
	state.terminalRightClickPaste = true
	x, y := cellPoint(geometry, 0, 8, state.cellWidth, state.cellHeight)
	click := func(modifiers gfx.Modifiers) {
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Modifiers: modifiers,
			Target: hitTerminalList.At(int(session.ID)), X: x, Y: y, Viewport: state.viewport})
	}
	click(0)
	if copied != "hello" || len(shell.input) != 0 || state.terminalMenu.open {
		t.Fatalf("selected right click: copied=%q input=%q menu=%v", copied, shell.input, state.terminalMenu.open)
	}
	click(gfx.ModifierShift)
	if !state.terminalMenu.open || len(shell.input) != 0 {
		t.Fatal("Shift+right click with a selection did not open the menu")
	}
}

func fillHistory(t *testing.T, terminal *terminal, lines int) {
	t.Helper()
	for line := 0; line < lines; line++ {
		terminal.write([]byte(fmt.Sprintf("line %02d\r\n", line)))
	}
}

// A frame arrives when the compositor has one, not exactly on the interval the
// drag asked for. A frame a millisecond early must not cost a whole step, or a
// drag held at the edge scrolls at half the rate it asked for.
func TestFramesArrivingEarlyDoNotHalveTheDragScrollRate(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 300)
	terminal.screen.Scroll(200)
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: x, Y: y, Viewport: state.viewport})
	// Just below the grid: one line per interval, so the count of lines is the
	// rate the pointer asked for.
	belowY := geometry.top + 5*state.cellHeight + state.cellHeight/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: belowY, Viewport: state.viewport})
	if lines := terminal.selection.edge.lines; lines != 1 {
		t.Fatalf("a pointer just below the grid asks for %d lines per step", lines)
	}
	before := terminal.screen.ViewOffset()
	const frames = 21
	var at time.Duration
	for range frames {
		at += selectionScrollInterval - time.Millisecond
		state.stepSelectionScroll(terminal, at)
	}
	if scrolled := before - terminal.screen.ViewOffset(); scrolled < frames-2 {
		t.Fatalf("%d frames %v apart scrolled %d lines", frames, selectionScrollInterval-time.Millisecond, scrolled)
	}
}

func TestDraggingBelowTheGridScrollsAndSelectsHistory(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	terminal.screen.Scroll(20)
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 0, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	first := terminal.screen.RowText(0)

	belowY := geometry.top + 5*state.cellHeight + 3*state.cellHeight
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: belowY, Viewport: state.viewport})
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
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: belowY, Viewport: state.viewport})

	if terminal.selection.edge.direction != 0 {
		t.Fatal("releasing the button left the auto-scroll armed")
	}
	copied := state.Shortcut(gfx.KeyEvent{Target: hit, Modifiers: keymap.Primary, Text: "c"}).ClipboardText
	if !strings.HasPrefix(copied, strings.TrimSpace(first)) || strings.Count(copied, "\n") < 8 {
		t.Fatalf("selection covers only %q", copied)
	}
}

func TestInterruptedDragDoesNotSelectTextAboveItsFocus(t *testing.T) {
	for _, steps := range []int{0, 3} {
		t.Run(fmt.Sprintf("scrolled_%d", steps), func(t *testing.T) {
			state, session, geometry := selectionState()
			terminal := session.term
			fillHistory(t, terminal, 30)
			terminal.screen.Scroll(20)
			hit := hitTerminalList.At(int(session.ID))
			startRow := 1
			if steps == 0 {
				startRow = 3
			}
			startX, startY := cellPoint(geometry, startRow, 0, state.cellWidth, state.cellHeight)
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
			belowY := geometry.top + 8*state.cellHeight
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: startX, Y: belowY, Viewport: state.viewport})
			for step := 1; step <= steps; step++ {
				state.stepSelectionScroll(terminal, time.Duration(step)*selectionScrollInterval)
			}
			want := terminal.selection.text(terminal.screen)
			if want == "" {
				t.Fatal("drag did not select text")
			}
			state.Pointer(gfx.PointerEvent{Kind: gfx.PointerExit, Target: hit, Captured: true, X: -1, Y: -1, Viewport: state.viewport})
			if got := terminal.selection.text(terminal.screen); got != want {
				t.Fatalf("losing the pointer changed the selection from %q to %q", want, got)
			}
			if terminal.selection.dragging || terminal.selection.edge.direction != 0 {
				t.Fatalf("interrupted drag still active: %+v", terminal.selection)
			}
		})
	}
}

func TestDraggingAboveTheGridScrollsBackIntoHistory(t *testing.T) {
	state, session, geometry := selectionState()
	terminal := session.term
	fillHistory(t, terminal, 30)
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 4, 0, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: x, Y: y, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: geometry.top - 2*state.cellHeight, Viewport: state.viewport})
	for step := 1; step <= 2; step++ {
		state.stepSelectionScroll(terminal, time.Duration(step)*selectionScrollInterval)
	}
	if terminal.screen.ViewOffset() == 0 {
		t.Fatal("view did not scroll back")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: geometry.top - 2*state.cellHeight, Viewport: state.viewport})
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
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit,
		X: x, Y: y, Viewport: state.viewport,
	})
	state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true,
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
	state.Scroll(gfx.ScrollEvent{DeltaY: -3 * state.cellHeight})
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
		state.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: -1, Viewport: state.viewport, Target: hitTerminalList.At(int(session.ID))})
		if got := session.term.screen.ViewOffset(); got != notch {
			t.Fatalf("after %d slow notches the view is %d rows back", notch, got)
		}
	}
}

func TestShiftClickStretchesTheSelectionFromTheLastClick(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("alpha beta\r\ngamma delta\r\n"))
	hit := hitTerminalList.At(int(session.ID))
	press := func(row, column int, modifiers gfx.Modifiers) {
		x, y := cellPoint(geometry, row, column, state.cellWidth, state.cellHeight)
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, Modifiers: modifiers, X: x, Y: y, Viewport: state.viewport})
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, Modifiers: modifiers, X: x, Y: y, Viewport: state.viewport})
	}
	copied := func() string {
		return state.Shortcut(gfx.KeyEvent{Target: hit, Modifiers: keymap.Primary, Text: "c"}).ClipboardText
	}

	// With nothing selected, Shift-click goes from the place last clicked.
	press(0, 2, 0)
	if session.term.selection.selected {
		t.Fatal("a plain click selected something")
	}
	press(1, 4, gfx.ModifierShift)
	if got := copied(); got != "pha beta\ngamma" {
		t.Fatalf("from the last click = %q", got)
	}
	// With a selection, the end nearer the click moves and the far one stays.
	press(1, 1, gfx.ModifierShift)
	if got := copied(); got != "pha beta\nga" {
		t.Fatalf("shrunk at the end = %q", got)
	}
	press(0, 7, gfx.ModifierShift)
	if got := copied(); got != "eta\nga" {
		t.Fatalf("moved the start = %q", got)
	}
	press(0, 0, gfx.ModifierShift)
	if got := copied(); got != "alpha beta\nga" {
		t.Fatalf("grown at the start = %q", got)
	}
}

func TestShiftClickWithoutAnythingToExtendDoesNothing(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("alpha beta\r\n"))
	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 0, 3, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, Modifiers: gfx.ModifierShift, X: x, Y: y, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, Modifiers: gfx.ModifierShift, X: x, Y: y, Viewport: state.viewport})
	if session.term.selection.selected {
		t.Fatalf("selection = %+v", session.term.selection)
	}
}

// press, drag and release drive one gesture the way the runtime delivers it: a
// press with its click count, moves captured by the press, then the release.
func gesture(state *State, session *Session, geometry terminalGeometry, clicks int, down [2]int, moves ...[2]int) {
	hit := hitTerminalList.At(int(session.ID))
	at := func(cell [2]int) (float32, float32) {
		return cellPoint(geometry, cell[0], cell[1], state.cellWidth, state.cellHeight)
	}
	x, y := at(down)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: clicks, Target: hit, X: x, Y: y, Viewport: state.viewport})
	last := down
	for _, cell := range moves {
		x, y = at(cell)
		state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: y, Viewport: state.viewport})
		last = cell
	}
	x, y = at(last)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: x, Y: y, Viewport: state.viewport})
}

func selectedText(session *Session) string {
	return session.term.selection.text(session.term.screen)
}

func TestDoubleClickThenDragGrowsTheSelectionByWholeWords(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("one two three four\r\n"))

	gesture(state, session, geometry, 2, [2]int{0, 5})
	if got := selectedText(session); got != "two" {
		t.Fatalf("double click selected %q", got)
	}
	// Pressing inside the word and dragging forward ends on the word under the pointer.
	gesture(state, session, geometry, 2, [2]int{0, 5}, [2]int{0, 9})
	if got := selectedText(session); got != "two three" {
		t.Fatalf("forward drag selected %q", got)
	}
	// Dragging backward keeps the clicked word and takes the word before it.
	gesture(state, session, geometry, 2, [2]int{0, 9}, [2]int{0, 5}, [2]int{0, 1})
	if got := selectedText(session); got != "one two three" {
		t.Fatalf("backward drag selected %q", got)
	}
	// Coming back to the clicked word shrinks to just it.
	gesture(state, session, geometry, 2, [2]int{0, 9}, [2]int{0, 1}, [2]int{0, 10})
	if got := selectedText(session); got != "three" {
		t.Fatalf("returning to the clicked word selected %q", got)
	}
}

func TestTripleClickSelectsTheWholeLineAndDragsByLines(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("first line\r\nsecond line\r\nthird line\r\n"))

	gesture(state, session, geometry, 3, [2]int{1, 3})
	if got := selectedText(session); got != "second line" {
		t.Fatalf("triple click selected %q", got)
	}
	gesture(state, session, geometry, 3, [2]int{1, 3}, [2]int{2, 0})
	if got := selectedText(session); got != "second line\nthird line" {
		t.Fatalf("drag down selected %q", got)
	}
	gesture(state, session, geometry, 3, [2]int{1, 3}, [2]int{0, 9})
	if got := selectedText(session); got != "first line\nsecond line" {
		t.Fatalf("drag up selected %q", got)
	}
}

func TestTripleClickTakesEveryRowOfASoftWrappedLine(t *testing.T) {
	state, session, geometry := selectionState()
	// 45 characters at 20 columns wrap onto three rows; the next line is separate.
	long := strings.Repeat("0123456789", 4) + "abcde"
	session.term.write([]byte(long + "\r\nnext\r\n"))

	gesture(state, session, geometry, 3, [2]int{1, 4})
	if got := selectedText(session); got != long {
		t.Fatalf("triple click on a wrapped row selected %q, want the whole logical line", got)
	}
}

// A program killed before it could restore its modes must not leave them on the
// shell's prompt: moving the pointer would type escape sequences at the prompt,
// and the wheel would walk the line editor's history instead of the scrollback.
// The owning shell's prompt marker is where the terminal is taken back.
func TestTheOwnersPromptTakesBackTheModesAKilledProgramLeft(t *testing.T) {
	state, session, geometry := selectionState()
	shell := &fakeShell{}
	session.term.shell = shell
	screen := session.term.screen
	screen.SetPromptToken("tok")
	for line := 0; line < 20; line++ {
		screen.Write([]byte("line\r\n"))
	}
	// A prompt, then a full-screen program arming the modes it wants.
	screen.Write([]byte("\x1b]133;A;motty=tok\a$ \x1b]133;B;motty=tok\a\x1b]133;C;motty=tok\a"))
	screen.Write([]byte("\x1b[?1049h\x1b[?1003h\x1b[?1006h"))
	// It is killed: no teardown arrives, and its modes are still armed.
	if modes := screen.Modes(); !modes.AltScreen || modes.Mouse != term.MouseAny {
		t.Fatalf("the program's modes were not taken: %+v", modes)
	}
	screen.Write([]byte("\x1b]133;D;130;motty=tok\a\x1b]133;A;motty=tok\a$ \x1b]133;B;motty=tok\a"))

	hit := hitTerminalList.At(int(session.ID))
	x, y := cellPoint(geometry, 2, 7, state.cellWidth, state.cellHeight)
	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: x, Y: y, Target: hit, Viewport: state.viewport,
	}); shell.typed() != "" {
		t.Fatalf("moving the pointer at the prompt typed %q (result=%+v)", shell.typed(), result)
	}
	result := state.Scroll(gfx.ScrollEvent{
		X: x, Y: y, DeltaY: -state.cellHeight,
		Viewport: state.viewport, Target: hit, Precise: true,
	})
	if !result.Handled || !result.Repaint || shell.typed() != "" || screen.ViewOffset() == 0 {
		t.Fatalf("wheel at the prompt: result=%+v input=%q offset=%d", result, shell.typed(), screen.ViewOffset())
	}
}

func TestAPlainClickAfterAWordSelectionGoesBackToCells(t *testing.T) {
	state, session, geometry := selectionState()
	session.term.write([]byte("one two three\r\n"))

	gesture(state, session, geometry, 2, [2]int{0, 5})
	gesture(state, session, geometry, 1, [2]int{0, 0}, [2]int{0, 5})
	if got := selectedText(session); got != "one tw" {
		t.Fatalf("a cell drag after a word selection selected %q", got)
	}
}
