package workbench

import (
	"slices"
	"strconv"
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

// gutterCrossLine is the first line number that no longer fits the minimum
// width, so the gutter grows by one digit there.
func gutterCrossLine() int {
	line := 1
	for range gutterMinNumberChars {
		line *= 10
	}
	return line
}

func TestGutterNumberDigitsBoundaries(t *testing.T) {
	cases := []struct {
		line uint64
		want int
	}{
		{0, gutterMinNumberChars},
		{1, gutterMinNumberChars},
		{99, gutterMinNumberChars},
		{999, gutterMinNumberChars},
		{9999, gutterMinNumberChars},
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
	cross := gutterCrossLine()

	// A fresh session with 12 lines stays at the minimum width instead of reserving 6 digits.
	session.term.write([]byte(strings.Repeat("line\r\n", 11)))
	state.fitTerminal(session)
	minimum := state.layoutForSession(session.ID)
	wantMinWidth := snap(float32(gutterMinNumberChars)*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(session.term); got != wantMinWidth {
		t.Fatalf("12-line gutter width = %v, want %v (%d digits)", got, wantMinWidth, gutterMinNumberChars)
	}

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, minimum)
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

	// Up to the last line that fits keeps the same width with no resize.
	session.term.write([]byte(strings.Repeat("line\r\n", cross-1-12)))
	state.fitTerminal(session)
	if got := state.layoutForSession(session.ID); got != minimum {
		t.Fatalf("line %d resized gutter: %+v -> %+v", cross-1, minimum, got)
	}

	// Crossing it widens the gutter by one digit and shrinks the grid by 1 column.
	session.term.write([]byte("line\r\n"))
	state.fitTerminal(session)
	wider := state.layoutForSession(session.ID)
	wantWiderWidth := snap(float32(gutterMinNumberChars+1)*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(session.term); got != wantWiderWidth {
		t.Fatalf("line %d gutter width = %v, want %v", cross, got, wantWiderWidth)
	}
	if wider.left != minimum.left+state.cellWidth {
		t.Fatalf("wider left = %v, want %v", wider.left, minimum.left+state.cellWidth)
	}
	if cols, _ := session.term.screen.Size(); cols != wider.cols {
		t.Fatalf("screen cols = %d, want layout cols %d", cols, wider.cols)
	}

	canvas.Reset()
	state.paintGrid(&canvas, session, wider)
	if !slices.Contains(gutterTexts(&canvas), strconv.Itoa(cross)) {
		t.Fatalf("line %d not painted in %q", cross, gutterTexts(&canvas))
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

	// Both mode starts with the minimum digits + 1 space + 8 time chars = 11 cells.
	wantInitial := snap(float32(gutterMinNumberChars+1+gutterTimeChars)*state.cellWidth) + gutterRuleGap
	if got := state.gutterWidthFor(first.term); got != wantInitial {
		t.Fatalf("initial both-mode width = %v, want %v", got, wantInitial)
	}

	// Push only the first session past the minimum width; the second session keeps its narrow gutter.
	first.term.write([]byte(strings.Repeat("x\r\n", gutterCrossLine()+50)))
	secondPtr.term.write([]byte("short\r\n"))
	state.fitTerminals()

	firstLayout := state.layoutForSession(first.ID)
	secondLayout := state.layoutForSession(secondPtr.ID)
	if firstLayout.left != secondLayout.left+state.cellWidth {
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

	// Advance to 20 lines short of the crossing, then enter alternate screen and
	// move the cursor to the bottom row: main scrolled + alt row would cross it
	// if the alternate screen were not ignored.
	cross := gutterCrossLine()
	session.term.write([]byte(strings.Repeat("x\r\n", cross-21)))
	state.fitTerminal(session)
	beforeAlt := state.layoutForSession(session.ID)
	if got := session.term.gutterNumberChars(); got != gutterMinNumberChars {
		t.Fatalf("%d-line digits = %d, want %d", cross-20, got, gutterMinNumberChars)
	}
	session.term.write([]byte("\x1b[?1049h\x1b[40;1H"))
	state.fitTerminal(session)
	if afterAlt := state.layoutForSession(session.ID); afterAlt != beforeAlt {
		t.Fatalf("entering alt screen resized gutter: %+v -> %+v", beforeAlt, afterAlt)
	}
	session.term.write([]byte("\x1b[?1049l"))

	// Now cross the minimum width on the main screen -> grows by one digit.
	session.term.write([]byte(strings.Repeat("x\r\n", 40)))
	state.fitTerminal(session)
	threeDigit := state.layoutForSession(session.ID)
	if got := session.term.gutterNumberChars(); got != gutterMinNumberChars+1 {
		t.Fatalf("%d-line digits = %d, want %d", cross+20, got, gutterMinNumberChars+1)
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

	shell.say(strings.Repeat("out\r\n", gutterCrossLine()+5))
	state.Wake()

	after := state.layoutForSession(session.ID)
	if after.cols >= before.cols || after.left <= before.left {
		t.Fatalf("Wake did not widen gutter past the minimum: %+v -> %+v", before, after)
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

func countExact(texts []string, want string) int {
	count := 0
	for _, text := range texts {
		if text == want {
			count++
		}
	}
	return count
}

// A resize rewraps lines into more or fewer rows. The strip skips the rows a
// line gains, every number is painted once wherever the wrap falls, and the
// times stay what they were: a rewrap is not new output, so the clock must not
// move to the resize.
func TestGutterSkipsRewrappedRowsAndKeepsNumbersAndTimes(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	clock := stamp
	state.wallClock = func() int64 { return clock }
	state.setTerminalGutter(gutterBoth)
	cols, _ := session.term.screen.Size()
	session.term.write([]byte(strings.Repeat("ab", cols) + "\r\nbeta\r\ngamma"))
	want := time.Unix(stamp, 0).Format("15:04:05")

	var canvas gfx.Canvas
	state.paintGrid(&canvas, session, state.layoutForSession(session.ID))
	texts := gutterTexts(&canvas)
	for _, number := range []string{"1", "2", "3"} {
		if got := countExact(texts, number); got != 1 {
			t.Fatalf("number %s painted %d times in %q", number, got, texts)
		}
	}
	// Every line was written in the same second, so each of the three lines
	// shows it once on the row that begins it.
	if got := countExact(texts, want); got != 3 {
		t.Fatalf("time %s painted %d times in %q", want, got, texts)
	}

	clock += 3600
	later := time.Unix(clock, 0).Format("15:04:05")
	state.Resize(gfx.Size{Width: 600 - 4*state.cellWidth, Height: state.viewport.Height})
	canvas.Reset()
	state.paintGrid(&canvas, session, state.layoutForSession(session.ID))
	texts = gutterTexts(&canvas)
	for _, number := range []string{"1", "2", "3"} {
		if got := countExact(texts, number); got != 1 {
			t.Fatalf("number %s painted %d times after narrowing in %q", number, got, texts)
		}
	}
	if got, gotLater := countExact(texts, want), countExact(texts, later); got != 3 || gotLater != 0 {
		t.Fatalf("times after narrowing: %s painted %d times, %s painted %d times in %q", want, got, later, gotLater, texts)
	}

	state.Resize(gfx.Size{Width: 600, Height: state.viewport.Height})
	canvas.Reset()
	state.paintGrid(&canvas, session, state.layoutForSession(session.ID))
	texts = gutterTexts(&canvas)
	for _, number := range []string{"1", "2", "3"} {
		if got := countExact(texts, number); got != 1 {
			t.Fatalf("number %s painted %d times after widening in %q", number, got, texts)
		}
	}
	if got, gotLater := countExact(texts, want), countExact(texts, later); got != 3 || gotLater != 0 {
		t.Fatalf("times after widening: %s painted %d times, %s painted %d times in %q", want, got, later, gotLater, texts)
	}
}
