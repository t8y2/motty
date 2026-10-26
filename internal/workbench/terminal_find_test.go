package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

func openFindForTest(t testing.TB, state *State) (*Session, *terminalFind) {
	t.Helper()
	result := state.Shortcut(ui.KeyEvent{Modifiers: defaultShortcutModifier(), Text: "f"})
	if !result.Handled || !result.Repaint || result.Focus != hitTerminalFindField || !result.FocusVisible {
		t.Fatalf("find shortcut result = %+v", result)
	}
	session := &state.sessions[0]
	if !session.term.find.open {
		t.Fatal("find shortcut did not open the bar")
	}
	return session, &session.term.find
}

func typeTerminalFind(t testing.TB, state *State, value string) {
	t.Helper()
	input := state.TerminalFindInput(app.TextInputContext{ID: hitTerminalFindField})
	if input == nil || input.OnEvent == nil {
		t.Fatal("find field has no native text input")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: value}); err != nil {
		t.Fatalf("typing find query: %v", err)
	}
}

func TestTerminalFindSearchesHistoryAndNavigatesMatches(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Resize(ui.Size{Width: 420, Height: 260})
	for line := 0; line < 30; line++ {
		if line == 12 || line == 24 {
			shell.say("target 你e\u0301 line\r\n")
		} else {
			shell.say("ordinary output\r\n")
		}
	}
	state.Wake()

	session, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "TARGET 你E\u0301")
	if len(find.matches) != 2 || find.active != 0 || find.truncated {
		t.Fatalf("find = matches %+v active %d truncated %t", find.matches, find.active, find.truncated)
	}
	_, rows := session.term.screen.Size()
	if got := session.term.screen.ViewLine(rows / 2); got != find.matches[0].Start.Line {
		t.Fatalf("first match line = %d, centered line = %d", find.matches[0].Start.Line, got)
	}
	if find.count != "1 of 2" {
		t.Fatalf("count = %q", find.count)
	}

	result := state.Key(ui.KeyEvent{Target: hitTerminalFindField, Key: ui.KeyEnter})
	if !result.Handled || find.active != 1 || find.count != "2 of 2" {
		t.Fatalf("next result = %+v active %d count %q", result, find.active, find.count)
	}
	state.Key(ui.KeyEvent{Target: hitTerminalFindField, Key: ui.KeyEnter, Modifiers: ui.ModifierShift})
	if find.active != 0 || find.count != "1 of 2" {
		t.Fatalf("previous result = active %d count %q", find.active, find.count)
	}

	// Moving away and receiving output updates the result set without pulling the
	// view back to the active match.
	session.term.screen.ScrollToBottom()
	shell.say("target 你e\u0301 newest\r\n")
	state.Wake()
	if len(find.matches) != 3 || session.term.screen.ViewOffset() != 0 {
		t.Fatalf("output refresh = %d matches, view offset %d", len(find.matches), session.term.screen.ViewOffset())
	}
}

func TestTerminalFindInputIsBoundedSingleLineAndCloseRestoresTerminalFocus(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, find := openFindForTest(t, state)
	value := strings.Repeat("a", terminalFindMaxQuery+20) + "\nignored"
	input := state.TerminalFindInput(app.TextInputContext{ID: hitTerminalFindField})
	input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: value})
	if find.editor.RuneCount() != terminalFindMaxQuery || strings.ContainsAny(find.editor.String(), "\r\n") {
		t.Fatalf("bounded query = %d runes, %q", find.editor.RuneCount(), find.editor.String())
	}
	if state.EditorFor(hitTerminalFindField) != &find.editor {
		t.Fatal("EditorFor did not resolve the open find field")
	}

	find.matches = make([]term.SearchMatch, 1, terminalFindKeep+1)
	result := state.Key(ui.KeyEvent{Target: hitTerminalFindField, Key: ui.KeyEscape})
	if !result.Handled || result.Focus != hitTerminalList.At(int(session.ID)) || find.open || len(find.matches) != 0 || cap(find.matches) != 0 {
		t.Fatalf("close result=%+v open=%t matches=%d/%d", result, find.open, len(find.matches), cap(find.matches))
	}
	if find.editor.RuneCount() != terminalFindMaxQuery {
		t.Fatal("closing find discarded the reusable query")
	}
	if state.EditorFor(hitTerminalFindField) != nil || state.TerminalFindInput(app.TextInputContext{ID: hitTerminalFindField}) != nil {
		t.Fatal("closed find still exposes a text editor")
	}

	openFindForTest(t, state)
	pointer := state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: hitTerminalFindClose})
	if !pointer.Handled || pointer.Focus != hitTerminalList.At(int(session.ID)) || find.open {
		t.Fatalf("pointer close = %+v open=%t", pointer, find.open)
	}
	openFindForTest(t, state)
	if result := state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: hitTerminalFindClose}); result.Handled || !find.open {
		t.Fatalf("secondary close = %+v open=%t", result, find.open)
	}
}

func TestTerminalFindPaintsAccessibleControlsAndDistinctHighlights(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("needle and needle\r\n")
	state.Wake()
	_, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")

	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	field, ok := hitByID(&canvas, hitTerminalFindField)
	if !ok || field.Role != ui.RoleTextField || field.Value != "needle" || field.Label != state.messages.TerminalFindLabel {
		t.Fatalf("find field = %+v, present=%t", field, ok)
	}
	for _, id := range []ui.ID{hitTerminalFindPrevious, hitTerminalFindNext, hitTerminalFindClose} {
		hit, ok := hitByID(&canvas, id)
		if !ok || hit.Role != ui.RoleButton || hit.Disabled {
			t.Fatalf("find action %d = %+v, present=%t", id, hit, ok)
		}
	}
	regular, active := 0, 0
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color.Red != state.pal.warning.Red || rectangle.Color.Green != state.pal.warning.Green || rectangle.Color.Blue != state.pal.warning.Blue {
			continue
		}
		switch rectangle.Color.Alpha {
		case 0.28:
			regular++
		case 0.62:
			active++
		}
	}
	if len(find.matches) != 2 || regular == 0 || active == 0 {
		t.Fatalf("matches=%d highlight rectangles regular=%d active=%d", len(find.matches), regular, active)
	}
}

func TestPaintingAnUnchangedTerminalFindAllocatesNothing(t *testing.T) {
	state, _, _, shell := liveSession(t)
	for line := 0; line < 80; line++ {
		shell.say("needle output with 你好\r\n")
	}
	state.Wake()
	openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")

	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	paint()
	paint()
	if allocations := testing.AllocsPerRun(50, paint); allocations != 0 {
		t.Fatalf("painting unchanged find allocates %v times", allocations)
	}
	if _, scheduled := canvas.NextFrame(); scheduled {
		t.Fatal("an idle find bar scheduled another frame")
	}
}

func BenchmarkPaintTerminalFindUnchanged(b *testing.B) {
	state, _, _, shell := liveSession(b)
	for line := 0; line < 200; line++ {
		shell.say("needle output with 你好\r\n")
	}
	state.Wake()
	openFindForTest(b, state)
	typeTerminalFind(b, state, "needle")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	canvas.Reset()
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestTerminalFindThrottlesStreamingRescansAndCatchesUp(t *testing.T) {
	state, _, _, shell := liveSession(t)
	clock := time.Second
	state.options.Now = func() time.Duration { return clock }
	shell.say("needle one\r\n")
	state.Wake()
	session, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")
	if len(find.matches) != 1 {
		t.Fatalf("matches after opening = %d", len(find.matches))
	}

	// The first change after the query is scanned at once; output arriving inside
	// the interval is deferred instead of rescanning the whole buffer.
	shell.say("needle two\r\n")
	state.Wake()
	if len(find.matches) != 2 {
		t.Fatalf("matches after the first update = %d", len(find.matches))
	}
	scanned := find.lastScan
	shell.say("needle three\r\n")
	state.Wake()
	if !find.pending || find.lastScan != scanned || len(find.matches) != 2 {
		t.Fatalf("second update: pending=%t lastScan=%v (was %v) matches=%d", find.pending, find.lastScan, scanned, len(find.matches))
	}

	// The frame the find bar asks for runs after the interval, and the trailing
	// change is then scanned.
	clock += terminalFindRefreshInterval
	state.Wake()
	if find.pending || find.lastScan == scanned || len(find.matches) != 3 {
		t.Fatalf("trailing update: pending=%t lastScan=%v (was %v) matches=%d", find.pending, find.lastScan, scanned, len(find.matches))
	}
	if session.term.screen.ViewOffset() != 0 {
		t.Fatalf("find refresh moved the view to %d", session.term.screen.ViewOffset())
	}
}

func TestADeferredFindAsksForTheFrameThatCatchesUp(t *testing.T) {
	state, _, _, _ := liveSession(t)
	openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")
	find := &state.sessions[0].term.find
	find.lastScan = state.now()
	find.pending = true
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	delay, ok := canvas.NextFrame()
	if !ok || delay <= 0 || delay > terminalFindRefreshInterval {
		t.Fatalf("deferred find scheduled frame delay=%v ok=%t, want within %v", delay, ok, terminalFindRefreshInterval)
	}
}
