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
	geometry := state.layoutForSession(session.ID)
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

func TestGutterSetsTheTimeBeforeTheNumber(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local).Unix()
	state.wallClock = func() int64 { return stamp }
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	session.term.write([]byte("alpha\r\nbeta\r\n"))

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	at, ok := gutterLabel(&canvas, time.Unix(stamp, 0).Format("15:04:05"))
	if !ok {
		t.Fatalf("no time in %q", gutterTexts(&canvas))
	}
	number, ok := gutterLabel(&canvas, "1")
	if !ok {
		t.Fatalf("no line number in %q", gutterTexts(&canvas))
	}
	if number.X <= at.X+at.Width-state.cellWidth {
		t.Fatalf("number at %v does not sit right of the time at %v", number.X, at.X)
	}
}

func TestGutterKeepsTheNumbersAgainstTheRuleWithoutTheTime(t *testing.T) {
	// The number's distance from the text is the same whether or not the strip
	// also shows a time.
	distance := func(mode terminalGutter) float32 {
		state, session, _ := selectionState()
		state.viewport.Width = 600
		state.setTerminalGutter(mode)
		geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
		session.term.write([]byte("alpha\r\nbeta\r\n"))
		var canvas ui.Canvas
		state.paintGrid(&canvas, session, geometry)
		number, ok := gutterLabel(&canvas, "1")
		if !ok {
			t.Fatalf("no line number in %q", gutterTexts(&canvas))
		}
		return geometry.left - number.X
	}
	if both, numbers := distance(gutterBoth), distance(gutterNumbers); both != numbers {
		t.Fatalf("number is %v from the text with a time and %v without", both, numbers)
	}
}

func TestGutterNumbersNeverWrapToTheirLowDigits(t *testing.T) {
	if got, _ := formatGutter(gutterNumbers, 1234567, 0); got != "1234567" {
		t.Fatalf("line 1234567 shown as %q", got)
	}
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterNumbers)
	before := state.gutterWidth()
	session.term.write([]byte(strings.Repeat("\n", 1_000_005)))
	if !state.growGutterNumbers(session) {
		t.Fatal("a screen past a million lines kept the six digit column")
	}
	if state.gutterWidth() <= before {
		t.Fatalf("gutter is %v wide after growing, was %v", state.gutterWidth(), before)
	}
	if cols, _ := session.term.screen.Size(); cols != terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding()).cols {
		t.Fatal("the terminal was not refitted to the wider gutter")
	}
	var canvas ui.Canvas
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	state.paintGrid(&canvas, session, geometry)
	if _, ok := gutterLabel(&canvas, "1000006"); !ok {
		t.Fatalf("no seven digit number in %q", gutterTexts(&canvas))
	}
}

// markBands are the rows of the grid that have something painted in the mark
// column.
func markBands(canvas *ui.Canvas, state *State, geometry terminalGeometry, rows int) []bool {
	width := state.gutterMarkWidth()
	x := geometry.left - gutterRuleGap - width
	bands := make([]bool, rows)
	for _, rect := range canvas.Rectangles() {
		if rect.Bounds.X < x || rect.Bounds.X+rect.Bounds.Width > x+width {
			continue
		}
		if row := int((rect.Bounds.Y - geometry.top) / state.cellHeight); row >= 0 && row < rows {
			bands[row] = true
		}
	}
	return bands
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
	bands := markBands(&canvas, state, geometry, geometry.rows)
	for row := 0; row < 4; row++ {
		if want := session.term.screen.ContinuesAbove(row); bands[row] != want {
			t.Fatalf("row %d: mark painted = %v, continues the line above = %v", row, bands[row], want)
		}
	}
	if !bands[1] || !bands[2] {
		t.Fatal("the rows a long line wrapped into carry no corner")
	}

	canvas.Reset()
	session.term.screen.ClearScreen()
	session.term.write([]byte("alpha\r\nbeta\r\n"))
	state.paintGrid(&canvas, session, geometry)
	for row, painted := range markBands(&canvas, state, geometry, geometry.rows) {
		if painted {
			t.Fatalf("row %d has a mark on a screen that does not wrap", row)
		}
	}
}

// integrated is a terminal that has the shell's markers: it expects the token
// the shell integration would carry.
func integrated(state *State, session *Session) string {
	token := "test-token"
	session.term.screen.SetPromptToken(token)
	return ";motty=" + token
}

func shellBlock(suffix, prompt, command, output string) string {
	return "\x1b]133;A" + suffix + "\x07" + prompt + "\x1b]133;B" + suffix + "\x07" + command + "\r\n\x1b]133;C" + suffix + "\x07" + output
}

func TestGutterBoxFoldsTheOutputOfACommandTheShellMarked(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	suffix := integrated(state, session)
	session.term.write([]byte(shellBlock(suffix, "$ ", "ll", "alpha\r\nbeta\r\ngamma\r\n") + "\x1b]133;A" + suffix + "\x07$ "))

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	state.declareFoldBoxes(&canvas, session)
	fold := hitTerminalFold.At(foldHitIndex(session.ID, 0))
	hits := canvas.Hits()
	if !slices.ContainsFunc(hits, func(hit ui.Hit) bool { return hit.ID == fold }) {
		t.Fatal("the command row has no fold button")
	}
	if hits[len(hits)-1].ID == hitTerminalList.At(int(session.ID)) {
		t.Fatal("the terminal's region is declared over the fold button and takes its presses")
	}

	if !session.term.screen.ToggleFold(0) {
		t.Fatal("nothing to fold")
	}
	canvas.Reset()
	state.paintGrid(&canvas, session, geometry)
	if texts := gutterTexts(&canvas); anyContains(texts, "alpha") || !anyContains(texts, "ll") {
		t.Fatalf("folded block still paints its output or lost its command: %q", texts)
	}
}

func TestTheFoldBoxLightsUnderThePointer(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	suffix := integrated(state, session)
	session.term.write([]byte(shellBlock(suffix, "$ ", "ll", "alpha\r\nbeta\r\n") + "\x1b]133;A" + suffix + "\x07$ "))
	fold := hitTerminalFold.At(foldHitIndex(session.ID, 0))

	var rest, lit ui.Canvas
	state.paintGrid(&rest, session, geometry)
	lit.SetInteraction(fold, ui.ID{}, ui.ID{}, false)
	state.paintGrid(&lit, session, geometry)
	if len(lit.Rectangles()) <= len(rest.Rectangles()) {
		t.Fatalf("hovered box painted %d rectangles, resting %d: nothing lights it", len(lit.Rectangles()), len(rest.Rectangles()))
	}
}

func TestAShellWithoutTheIntegrationHasNoFoldBoxes(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	session.term.write([]byte("$ ll\r\nalpha\r\nbeta\r\n$ "))
	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	state.declareFoldBoxes(&canvas, session)
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitTerminalFold {
			t.Fatal("a fold button for a session with no shell markers")
		}
	}
	// The numbers and times are the gutter's own and need nothing from the shell.
	if !slices.Contains(gutterTexts(&canvas), "2") {
		t.Fatalf("no line numbers without the integration: %q", gutterTexts(&canvas))
	}
}

func TestTurningTheGutterOffOpensEverythingThatWasFolded(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	suffix := integrated(state, session)
	session.term.write([]byte(shellBlock(suffix, "$ ", "ll", "alpha\r\nbeta\r\n") + "\x1b]133;A" + suffix + "\x07$ "))
	session.term.screen.ToggleFold(0)

	state.setTerminalGutter(gutterOff)
	geometry = terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	if texts := gutterTexts(&canvas); !anyContains(texts, "alpha") || !anyContains(texts, "beta") {
		t.Fatalf("output stayed hidden with no gutter to open it: %q", texts)
	}

	// The choice is remembered for when the gutter comes back.
	state.setTerminalGutter(gutterNumbers)
	geometry = terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	canvas.Reset()
	state.paintGrid(&canvas, session, geometry)
	if texts := gutterTexts(&canvas); anyContains(texts, "alpha") {
		t.Fatalf("the fold was forgotten: %q", texts)
	}
}

func TestAFullScreenProgramOverScrollbackIsPaintedWithTheGutterOn(t *testing.T) {
	state, session, _ := selectionState()
	state.viewport.Width = 600
	state.setTerminalGutter(gutterBoth)
	geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	suffix := integrated(state, session)
	session.term.write([]byte(shellBlock(suffix, "$ ", "seq 1 30", strings.Repeat("n\r\n", 30)) + "\x1b]133;A" + suffix + "\x07$ vim"))
	session.term.write([]byte("\x1b[?1049h\x1b[H\x1b[2Jvim-content"))

	var canvas ui.Canvas
	state.paintGrid(&canvas, session, geometry)
	if texts := gutterTexts(&canvas); !anyContains(texts, "vim-content") {
		t.Fatalf("the alternate screen is blank: %q", texts)
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

// BenchmarkPaintTerminalGutterBlocks is a repaint of an unchanged screen with
// the gutter on, with and without the shell's command blocks, one of them folded.
func BenchmarkPaintTerminalGutterBlocks(b *testing.B) {
	for _, blocks := range []bool{false, true} {
		name := "no markers"
		if blocks {
			name = "three blocks, one folded"
		}
		b.Run(name, func(b *testing.B) {
			state, session, _ := selectionState()
			state.viewport.Width = 600
			state.setTerminalGutter(gutterBoth)
			geometry := terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
			suffix := ""
			if blocks {
				suffix = integrated(state, session)
			}
			for _, command := range []string{"one", "two", "three"} {
				if blocks {
					session.term.write([]byte("\x1b]133;A" + suffix + "\x07$ " + "\x1b]133;B" + suffix + "\x07" + command + "\r\n\x1b]133;C" + suffix + "\x07out\r\n"))
				} else {
					session.term.write([]byte("$ " + command + "\r\nout\r\n"))
				}
			}
			if blocks {
				session.term.screen.ToggleFold(0)
			}
			var canvas ui.Canvas
			state.paintGrid(&canvas, session, geometry)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				canvas.Reset()
				state.paintGrid(&canvas, session, geometry)
			}
		})
	}
}
