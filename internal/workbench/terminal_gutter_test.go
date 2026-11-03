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
	if copied := state.Shortcut(ui.KeyEvent{Target: hit, Modifiers: defaultShortcutModifier(), Text: "c"}).ClipboardText; copied != "pha be" {
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
