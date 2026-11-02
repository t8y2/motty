package workbench

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
)

func gutterTexts(canvas *ui.Canvas) []string {
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
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	session.term.write([]byte("alpha\r\nbeta\r\n"))
	want := time.Unix(stamp, 0).Format("15:04:05")

	var canvas ui.Canvas
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

// gutterLabel is the bounds of the label the strip painted with this text.
func gutterLabel(canvas *ui.Canvas, value string) (ui.Rect, bool) {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return text.Bounds, true
		}
	}
	return ui.Rect{}, false
}

func TestGutterSetsTheTimeAtTheLeftEdgeAndTheNumberAgainstTheRule(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	state.wallClock = func() int64 { return stamp }
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	session.term.write([]byte("alpha\r\nbeta\r\n"))

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	texts := gutterTexts(&canvas)
	at, ok := gutterLabel(&canvas, time.Unix(stamp, 0).Format("15:04:05"))
	if !ok {
		t.Fatalf("no time in %q", texts)
	}
	number, ok := gutterLabel(&canvas, "1")
	if !ok {
		t.Fatalf("no line number in %q", texts)
	}
	if want := geometry.left - state.gutterWidth(); at.X != want {
		t.Fatalf("time starts at %v, want the strip's left edge %v", at.X, want)
	}
	if number.X <= at.X {
		t.Fatalf("number at %v does not sit right of the time at %v", number.X, at.X)
	}
	if gap := geometry.left - number.X; gap != gutterRuleGap {
		t.Fatalf("number ends %v from the rule, want %v", gap, gutterRuleGap)
	}
}

func TestGutterKeepsTheNumbersAgainstTheRuleWithoutTheTime(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterNumbers)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	session.term.write([]byte("alpha\r\nbeta\r\n"))

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	number, ok := gutterLabel(&canvas, "1")
	if !ok {
		t.Fatalf("no line number in %q", gutterTexts(&canvas))
	}
	// Hiding the time must not move the numbers: they keep the rule's side.
	if gap := geometry.left - number.X; gap != gutterRuleGap {
		t.Fatalf("number ends %v from the rule, want %v", gap, gutterRuleGap)
	}
}

// gutterStrokes counts the marks the strip painted in its wrap cell.
func gutterStrokes(canvas *ui.Canvas, state *State, geometry terminalGeometry) int {
	x := geometry.left - state.gutterWidth()
	if state.terminalGutter.times() {
		x += float32(gutterTimeChars) * state.cellWidth
	}
	strokes := 0
	for _, rect := range canvas.Rectangles() {
		if rect.Bounds.X >= x && rect.Bounds.X+rect.Bounds.Width <= x+state.cellWidth {
			strokes++
		}
	}
	return strokes
}

func TestGutterMarksTheRowsThatContinueAWrappedLine(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())

	var canvas ui.Canvas
	// Three full rows of one line: the last two continue it.
	session.term.write([]byte(strings.Repeat("x", 3*geometry.cols) + "\r\n"))
	state.paintGrid(&canvas, session, geometry)
	cursor := session.term.presentedCursor()
	continued := 0
	for row := 0; row <= cursor.Y && row < geometry.rows; row++ {
		if session.term.screen.ContinuesAbove(row) {
			continued++
		}
	}
	if continued == 0 {
		t.Fatalf("nothing wrapped on a %d column screen", geometry.cols)
	}
	if got := gutterStrokes(&canvas, state, geometry); got != 2*continued {
		t.Fatalf("%d mark strokes for %d rows continuing a line", got, continued)
	}

	canvas.Reset()
	session.term.screen.ClearScreen()
	session.term.write([]byte("alpha\r\nbeta\r\n"))
	state.paintGrid(&canvas, session, geometry)
	if got := gutterStrokes(&canvas, state, geometry); got != 0 {
		t.Fatalf("%d mark strokes on a screen that does not wrap", got)
	}
}

func TestGutterTakesColumnsAndCopyLeavesItOut(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterOff)
	before := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	if geometry.cols >= before.cols || geometry.left <= before.left {
		t.Fatalf("gutter did not take room: %+v -> %+v", before, geometry)
	}
	if cols, _ := session.term.screen.Size(); cols != geometry.cols {
		t.Fatalf("terminal has %d columns, layout %d: the remote would be told the wrong size", cols, geometry.cols)
	}

	session.term.write([]byte("alpha beta\r\n"))
	hit := hitTerminalList.At(int(session.ID))
	startX, startY := cellPoint(geometry, 0, 2, state.cellWidth, state.cellHeight)
	endX, endY := cellPoint(geometry, 0, 7, state.cellWidth, state.cellHeight)
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hit, X: startX, Y: startY, Viewport: state.viewport})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	state.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: hit, Captured: true, X: endX, Y: endY, Viewport: state.viewport})
	if copied := state.Key(ui.KeyEvent{Target: hit, Modifiers: ui.ModifierMeta, Text: "c"}).ClipboardText; copied != "pha be" {
		t.Fatalf("copied %q", copied)
	}
}

func TestGutterSettingPersists(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.Click(hitSettingsGutterTime)
	if restored := newTestState(t, options); restored.terminalGutter != gutterTime {
		t.Fatalf("restored gutter %v", restored.terminalGutter)
	}
}
