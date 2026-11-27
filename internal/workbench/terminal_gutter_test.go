package workbench

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func gutterTexts(canvas *gfx.Canvas) []string {
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return texts
}

func anyContains(texts []string, want string) bool {
	for _, text := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

func TestGutterShowsNumbersAndTimesAndHidesOnTheAlternateScreen(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	state.wallClock = func() int64 { return stamp }
	state.setTerminalGutter(gutterBoth)
	geometry := state.layoutForSession(session.ID)
	session.term.write([]byte("alpha\r\nbeta\r\n"))
	want := time.Unix(stamp, 0).Format("15:04:05")

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, geometry)
	texts := gutterTexts(&canvas)
	if !anyContains(texts, want) || !slices.Contains(texts, "2") {
		t.Fatalf("no gutter for line 2 in %q", texts)
	}

	canvas.Reset()
	session.term.write([]byte("\x1b[?1049h"))
	state.paintGrid(&canvas, session, geometry)
	if texts := gutterTexts(&canvas); anyContains(texts, want) {
		t.Fatalf("gutter still painted on the alternate screen: %q", texts)
	}
}

// A program that repaints a row in place (top, a progress bar) moves that
// row's time forward; the gutter must not keep showing when the row first
// appeared.
func TestGutterFollowsRepaintedLines(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	state.wallClock = func() int64 { return stamp }
	state.setTerminalGutter(gutterTime)
	geometry := state.layoutForSession(session.ID)
	session.term.write([]byte("alpha"))

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, geometry)
	first := time.Unix(stamp, 0).Format("15:04:05")
	if !anyContains(gutterTexts(&canvas), first) {
		t.Fatalf("gutter = %q, want %s", gutterTexts(&canvas), first)
	}

	stamp += 60
	canvas.Reset()
	session.term.write([]byte("\x1b[HALPHA"))
	state.paintGrid(&canvas, session, geometry)
	second := time.Unix(stamp, 0).Format("15:04:05")
	texts := gutterTexts(&canvas)
	if !anyContains(texts, second) || anyContains(texts, first) {
		t.Fatalf("gutter after repaint = %q, want %s and not %s", texts, second, first)
	}
}

func TestGutterTakesColumnsAndCopyLeavesItOut(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterOff)
	before := state.layoutForSession(session.ID)
	state.setTerminalGutter(gutterBoth)
	geometry := state.layoutForSession(session.ID)
	if geometry.cols >= before.cols || geometry.left <= before.left {
		t.Fatalf("gutter did not take room: %+v -> %+v", before, geometry)
	}
	if cols, _ := session.term.screen.Size(); cols != geometry.cols {
		t.Fatalf("terminal has %d columns, layout %d: the remote would be told the wrong size", cols, geometry.cols)
	}

	session.term.write([]byte("alpha beta\r\n"))
	hit := terminalTestHit(state, session)
	startX, startY := cellPoint(geometry, 0, 2, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 0, 7, state.cellWidth, state.cellHeight)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	if copied := state.Shortcut(gfx.KeyEvent{Target: hit, Modifiers: keymap.Primary, Text: "c"}).ClipboardText; copied != "pha be" {
		t.Fatalf("copied %q", copied)
	}
}

func TestGutterSettingPersists(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	clickSettingsChoice(t, state, state.settingsGutterLabel(gutterTime))
	if restored := newTestState(t, options); restored.terminalGutter != gutterTime {
		t.Fatalf("restored gutter %v", restored.terminalGutter)
	}
}

func TestGutterNumberDigitsBoundaries(t *testing.T) {
	cases := []struct {
		line uint64
		want int
	}{
		{0, gutterMinNumberChars},
		{1, gutterMinNumberChars},
		{9, gutterMinNumberChars},
		{10, 2},
		{12, 2},
		{99, 2},
		{100, 3},
		{999, 3},
		{1000, 4},
		{9999, 4},
		{10000, 5},
		{99999, 5},
		{100000, 6},
		{999999, 6},
		{1000000, 6},
	}
	for _, tc := range cases {
		if got := gutterNumberDigits(tc.line); got != tc.want {
			t.Errorf("gutterNumberDigits(%d) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestGutterAutoSizesNumberColumn(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterNumbers)

	// Fresh session with 12 lines stays at the 2-digit minimum width instead of reserving 6 digits.
	session.term.write([]byte(strings.Repeat("line\r\n", 11)))
	state.fitTerminal(session)
	twoDigit := state.layoutForSession(session.ID)
	wantTwoWidth := snap(float32(gutterMinNumberChars)*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(session.term); got != wantTwoWidth {
		t.Fatalf("12-line gutter width = %v, want %v (2 digits)", got, wantTwoWidth)
	}

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, twoDigit)
	found12 := false
	for _, text := range canvas.Texts() {
		if text.Value == "12" {
			found12 = true
			wantBox := float32(gutterMinNumberChars) * state.cellWidth
			if text.Bounds.Width != wantBox {
				t.Fatalf("line 12 label width = %v, want %v", text.Bounds.Width, wantBox)
			}
		}
	}
	if !found12 {
		t.Fatalf("line 12 not painted in %q", gutterTexts(&canvas))
	}

	// Up to line 99 keeps the same 2-digit width with no resize.
	session.term.write([]byte(strings.Repeat("line\r\n", 87)))
	state.fitTerminal(session)
	if got := state.layoutForSession(session.ID); got != twoDigit {
		t.Fatalf("line 99 resized gutter: %+v -> %+v", twoDigit, got)
	}

	// Crossing line 100 expands the gutter to 3 digits and shrinks the grid by 1 column.
	session.term.write([]byte("line\r\n"))
	state.fitTerminal(session)
	threeDigit := state.layoutForSession(session.ID)
	wantThreeWidth := snap(3*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(session.term); got != wantThreeWidth {
		t.Fatalf("100-line gutter width = %v, want %v (3 digits)", got, wantThreeWidth)
	}
	if threeDigit.left != twoDigit.left+state.cellWidth {
		t.Fatalf("3-digit left = %v, want %v", threeDigit.left, twoDigit.left+state.cellWidth)
	}
	if cols, _ := session.term.screen.Size(); cols != threeDigit.cols {
		t.Fatalf("screen cols = %d, want layout cols %d", cols, threeDigit.cols)
	}

	canvas.Reset()
	state.paintGrid(&canvas, session, threeDigit)
	if !slices.Contains(gutterTexts(&canvas), "100") {
		t.Fatalf("line 100 not painted in %q", gutterTexts(&canvas))
	}

	// Crossing line 1000 expands the gutter to 4 digits.
	session.term.write([]byte(strings.Repeat("line\r\n", 900)))
	state.fitTerminal(session)
	wantFourWidth := snap(4*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(session.term); got != wantFourWidth {
		t.Fatalf("1000-line gutter width = %v, want %v (4 digits)", got, wantFourWidth)
	}
}

func TestGutterAutoWidthInBothModeAndPerSession(t *testing.T) {
	state, first, _ := selectionState()
	state.viewport.Width = 700
	state.setTerminalGutter(gutterBoth)

	secondTerm := newTerminal(80, 24)
	second := Session{ID: first.ID + 1, Title: "second", term: secondTerm}
	state.sessions = append(state.sessions, second)
	first = &state.sessions[0]
	secondPtr := &state.sessions[1]

	// Both mode starts with 2 digits + 1 space + 8 time chars = 11 cells.
	wantInitial := snap(float32(gutterMinNumberChars+1+gutterTimeChars)*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(first.term); got != wantInitial {
		t.Fatalf("initial both-mode width = %v, want %v", got, wantInitial)
	}

	// Push only the first session past 1000 lines; the second session keeps its narrow gutter.
	first.term.write([]byte(strings.Repeat("x\r\n", 1050)))
	secondPtr.term.write([]byte("short\r\n"))
	state.fitTerminals()

	firstLayout := state.layoutForSession(first.ID)
	secondLayout := state.layoutForSession(secondPtr.ID)
	if firstLayout.left != secondLayout.left+2*state.cellWidth {
		t.Fatalf("per-session gutter mismatch: first.left=%v second.left=%v cellWidth=%v", firstLayout.left, secondLayout.left, state.cellWidth)
	}
	if firstCols, _ := first.term.screen.Size(); firstCols != firstLayout.cols {
		t.Fatalf("first screen cols = %d, want %d", firstCols, firstLayout.cols)
	}
	if secondCols, _ := secondPtr.term.screen.Size(); secondCols != secondLayout.cols {
		t.Fatalf("second screen cols = %d, want %d", secondCols, secondLayout.cols)
	}
}

func TestGutterAutoWidthStaysMonotonicAcrossScrollAltScreenAndCursorMove(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport = gfx.Size{Width: 600, Height: 600}
	state.setTerminalGutter(gutterNumbers)
	state.fitTerminal(session)

	// Advance to line 80 (still 2 digits), then enter alternate screen and move
	// cursor to the bottom row: main scrolled + alt row would exceed 100 if
	// alternate screen were not ignored.
	session.term.write([]byte(strings.Repeat("x\r\n", 79)))
	state.fitTerminal(session)
	beforeAlt := state.layoutForSession(session.ID)
	if got := session.term.gutterNumberChars(); got != 2 {
		t.Fatalf("80-line digits = %d, want 2", got)
	}
	session.term.write([]byte("\x1b[?1049h\x1b[40;1H"))
	state.fitTerminal(session)
	if afterAlt := state.layoutForSession(session.ID); afterAlt != beforeAlt {
		t.Fatalf("entering alt screen resized gutter: %+v -> %+v", beforeAlt, afterAlt)
	}
	session.term.write([]byte("\x1b[?1049l"))

	// Now cross line 100 on the main screen -> grows to 3 digits.
	session.term.write([]byte(strings.Repeat("x\r\n", 40)))
	state.fitTerminal(session)
	threeDigit := state.layoutForSession(session.ID)
	if got := session.term.gutterNumberChars(); got != 3 {
		t.Fatalf("120-line digits = %d, want 3", got)
	}

	// Scrolling back to line 1 must not shrink the gutter or resize the grid.
	session.term.screen.Scroll(session.term.screen.Scrollback())
	state.fitTerminal(session)
	if got := state.layoutForSession(session.ID); got != threeDigit {
		t.Fatalf("scrolling to top changed layout: %+v -> %+v", threeDigit, got)
	}

	// Moving cursor to home row must not shrink the gutter either.
	session.term.screen.ScrollToBottom()
	session.term.write([]byte("\x1b[H"))
	state.fitTerminal(session)
	if got := state.layoutForSession(session.ID); got != threeDigit {
		t.Fatalf("cursor home changed layout: %+v -> %+v", threeDigit, got)
	}
}

func TestGutterAutoWidthResizesLiveShellOnWake(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterNumbers)
	shell := &fakeShell{done: make(chan struct{})}
	session.term.shell = shell
	state.fitTerminal(session)
	before := state.layoutForSession(session.ID)
	resizesBefore := shell.resizeCount()

	shell.say(strings.Repeat("out\r\n", 105))
	state.Wake()

	after := state.layoutForSession(session.ID)
	if after.cols >= before.cols || after.left <= before.left {
		t.Fatalf("Wake did not widen gutter for 105 lines: %+v -> %+v", before, after)
	}
	if cols, _ := session.term.screen.Size(); cols != after.cols {
		t.Fatalf("screen cols = %d, want %d", cols, after.cols)
	}
	if shell.resizeCount() <= resizesBefore {
		t.Fatalf("live shell was not notified of auto-width resize")
	}
	if got := shell.lastResize(); got[0] != after.cols || got[1] != after.rows {
		t.Fatalf("shell resize = %v, want [%d %d]", got, after.cols, after.rows)
	}
}
