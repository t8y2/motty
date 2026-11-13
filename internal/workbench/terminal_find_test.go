package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/term"
)

func openFindForTest(t testing.TB, state *State) (*Session, *terminalFind) {
	t.Helper()
	result := state.Shortcut(gfx.KeyEvent{Modifiers: keymap.Primary, Text: "f"})
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
	state.terminalFindPrefs.Origin = findSessionTop
	state.Resize(gfx.Size{Width: 420, Height: 260})
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

	result := state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyEnter})
	if !result.Handled || find.active != 1 || find.count != "2 of 2" {
		t.Fatalf("next result = %+v active %d count %q", result, find.active, find.count)
	}
	state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyEnter, Modifiers: gfx.ModifierShift})
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

func TestClearingTheScreenRescansAnOpenFind(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("needle in the output\r\n")
	state.Wake()
	_, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")
	if len(find.matches) != 1 {
		t.Fatalf("matches = %d, want the line just printed", len(find.matches))
	}
	shortcut := state.shortcuts.chords.Chord(shortcutClearScreen)
	state.Shortcut(gfx.KeyEvent{Target: hitTerminalFindField, Modifiers: shortcut.Modifiers, Text: string(shortcut.Char)})
	if len(find.matches) != 0 || find.active != -1 || find.count != state.messages.TerminalFindNoMatches {
		t.Fatalf("after the clear = %d matches, active %d, count %q", len(find.matches), find.active, find.count)
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
	result := state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyEscape})
	if !result.Handled || result.Focus != terminalTestHit(state, session) || find.open || len(find.matches) != 0 || cap(find.matches) != 0 {
		t.Fatalf("close result=%+v open=%t matches=%d/%d", result, find.open, len(find.matches), cap(find.matches))
	}
	if find.editor.RuneCount() != terminalFindMaxQuery {
		t.Fatal("closing find discarded the reusable query")
	}
	if state.EditorFor(hitTerminalFindField) != nil || state.TerminalFindInput(app.TextInputContext{ID: hitTerminalFindField}) != nil {
		t.Fatal("closed find still exposes a text editor")
	}

	openFindForTest(t, state)
	pointer := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitTerminalFindClose})
	if !pointer.Handled || pointer.Focus != terminalTestHit(state, session) || find.open {
		t.Fatalf("pointer close = %+v open=%t", pointer, find.open)
	}
	openFindForTest(t, state)
	if result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTerminalFindClose}); result.Handled || !find.open {
		t.Fatalf("secondary close = %+v open=%t", result, find.open)
	}
}

func TestOpeningTerminalFindTakesTheSelectionAndSelectsTheQuery(t *testing.T) {
	state, session, _ := selectionState()
	session.term.write([]byte("alpha beta gamma\r\n"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 6}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 9}),
	)
	state.openTerminalFind(session)
	find := &session.term.find
	if find.editor.String() != "beta" {
		t.Fatalf("query = %q, want the selected text", find.editor.String())
	}
	if !find.open || len(find.matches) != 1 || find.matches[0].Start.Column != 6 {
		t.Fatalf("open=%t matches=%+v", find.open, find.matches)
	}
	if selected := find.editor.Selection(); selected.Start != 0 || selected.End != find.editor.RuneCount() {
		t.Fatalf("query selection = %+v, want all %d runes", selected, find.editor.RuneCount())
	}
}

func TestReopeningTerminalFindSelectsItsRetainedQuery(t *testing.T) {
	state, session, _ := selectionState()
	state.openTerminalFind(session)
	find := &session.term.find
	find.setQuery("needle")
	if selected := find.editor.Selection(); selected.Start != selected.End {
		t.Fatalf("typing a query left %+v selected", selected)
	}
	state.closeTerminalFind(session)
	state.openTerminalFind(session)
	if find.editor.String() != "needle" {
		t.Fatalf("reopening replaced the query with %q", find.editor.String())
	}
	if selected := find.editor.Selection(); selected.Start != 0 || selected.End != find.editor.RuneCount() {
		t.Fatalf("reopened selection = %+v, want all %d runes", selected, find.editor.RuneCount())
	}
}

func TestTerminalFindLeavesAMultiLineSelectionAlone(t *testing.T) {
	state, session, _ := selectionState()
	session.term.write([]byte("first line\r\nsecond line\r\n"))
	state.openTerminalFind(session)
	find := &session.term.find
	find.setQuery("old")
	state.closeTerminalFind(session)
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 1, Column: 5}),
	)
	state.openTerminalFind(session)
	if find.editor.String() != "old" {
		t.Fatalf("query = %q: a selection across a line break is not searchable and must not replace the query", find.editor.String())
	}
}

func TestTerminalFindKeepsAWholeSelectionWithinTheField(t *testing.T) {
	state := &State{
		viewport: gfx.Size{Width: 400, Height: 200}, cellWidth: 8, cellHeight: 16,
		activeSection: sectionTerminal, pal: &darkPalette, shortcuts: defaultShortcuts(),
	}
	live := newTerminal(400, 5)
	live.live = true
	state.sessions = []Session{{ID: 1, Address: "test", term: live}}
	state.activeSessionID = 1
	session := &state.sessions[0]
	session.term.write([]byte(strings.Repeat("x", 1200)))
	// The line soft-wraps, so the selection joins the rows without a line
	// break and is exactly what a search of that line would need.
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 0}),
		session.term.screen.PointAt(term.Position{Row: 2, Column: 399}),
	)
	state.openTerminalFind(session)
	if got := session.term.find.editor.String(); got != strings.Repeat("x", terminalFindMaxQuery) {
		t.Fatalf("query = %d runes, want the field's %d", len([]rune(got)), terminalFindMaxQuery)
	}
}

func TestTerminalFindNativeFieldMirrorsTheSelectedQuery(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	session.term.write([]byte("alpha beta\r\n"))
	session.term.selection.selectRange(
		session.term.screen.PointAt(term.Position{Row: 0, Column: 6}),
		session.term.screen.PointAt(term.Position{Row: 0, Column: 9}),
	)
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "terminal find", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.TerminalFindInput(context); input != nil {
				return input
			}
			return state.TerminalInput(context)
		},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	find := state.shortcuts.chords.Chord(shortcutFind)
	if err := driver.Chord(find.Modifiers, string(find.Char)); err != nil {
		t.Fatal(err)
	}
	editor := driver.Snapshot().Editor
	if editor == nil || editor.Value != "beta" || editor.Selection != [2]int{0, len("beta")} {
		t.Fatalf("native editor = %+v, want the selected query all selected", editor)
	}
}

func TestTerminalFindPaintsAccessibleControlsAndDistinctHighlights(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("needle and needle\r\n")
	state.Wake()
	_, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")

	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	field, ok := hitByID(&canvas, hitTerminalFindField)
	if !ok || field.Role != gfx.RoleTextField || field.Value != "needle" || field.Label != state.messages.TerminalFindLabel {
		t.Fatalf("find field = %+v, present=%t", field, ok)
	}
	for _, id := range []gfx.ID{hitTerminalFindPrevious, hitTerminalFindNext, hitTerminalFindClose, hitTerminalFindSettings} {
		hit, ok := hitByID(&canvas, id)
		if !ok || hit.Role != gfx.RoleButton || hit.Disabled {
			t.Fatalf("find action %d = %+v, present=%t", id, hit, ok)
		}
	}
	ink, background := state.findHighlightColors(false)
	fills := 0
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Color == background {
			fills++
		}
	}
	recolored := false
	for _, run := range canvas.AllTextRuns() {
		recolored = recolored || run.Color == ink
	}
	if len(find.matches) != 2 || fills != 2 || !recolored {
		t.Fatalf("matches=%d highlight fills=%d recolored=%t", len(find.matches), fills, recolored)
	}
}

// TestTerminalFindReopeningWithAQueryKeepsTheConfiguredStart covers the second
// and later opens: the field still holds the previous query, and the origin
// chosen since then must still pick the first match.
func TestTerminalFindReopeningWithAQueryKeepsTheConfiguredStart(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Resize(gfx.Size{Width: 500, Height: 410})
	for line := range 100 {
		if line%15 == 0 && line < 90 {
			shell.say("needle\r\n")
		} else {
			shell.say("other\r\n")
		}
	}
	state.Wake()
	state.terminalFindPrefs.Origin = findSessionTop
	_, find := openFindForTest(t, state)
	typeTerminalFind(t, state, "needle")
	if len(find.matches) != 6 || find.active != 0 {
		t.Fatalf("first open = matches %d active %d", len(find.matches), find.active)
	}
	state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyEscape})
	if find.open || find.editor.String() != "needle" {
		t.Fatalf("close = open %t query %q", find.open, find.editor.String())
	}

	state.terminalFindPrefs.Origin = findSessionBottom
	openFindForTest(t, state)
	if find.active != len(find.matches)-1 {
		t.Fatalf("reopen with the remembered query = matches %d active %d, want the last one", len(find.matches), find.active)
	}
}

func TestTerminalFindStartsAtTheSelectedBufferEdge(t *testing.T) {
	for _, test := range []struct {
		origin terminalFindOrigin
		want   int
	}{
		{findSessionTop, 0},
		{findSessionBottom, 5},
		{findViewTop, 2},
		{findViewBottom, 3},
	} {
		t.Run(string(test.origin), func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			state.Resize(gfx.Size{Width: 500, Height: 410})
			for line := range 100 {
				if line%15 == 0 && line < 90 {
					shell.say("needle\r\n")
				} else {
					shell.say("other\r\n")
				}
			}
			state.Wake()
			screen := state.sessions[0].term.screen
			screen.Scroll(screen.Scrollback() - 28)
			state.terminalFindPrefs.Origin = test.origin
			_, find := openFindForTest(t, state)
			top := screen.ViewLine(0)
			typeTerminalFind(t, state, "needle")
			if find.active != test.want {
				t.Fatalf("view starts at %d, matches=%v, active=%d, want %d", top, find.matches, find.active, test.want)
			}
			if test.origin == findViewTop || test.origin == findViewBottom {
				screen.ScrollToBottom()
				find.setQuery("NEEDLE")
				find.refresh(screen, state.messages, true)
				if find.active != test.want {
					t.Fatalf("editing the query lost the starting viewport: active=%d, want %d", find.active, test.want)
				}
			}
		})
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

	var canvas gfx.Canvas
	// Scrolling to a match lights the scrollbar for a while; time has to pass
	// for the application to be at rest.
	var at time.Duration
	canvas.SetClock(func() time.Duration { return at })
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func() {
		at += 10 * time.Second
		canvas.Reset()
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	delay, ok := canvas.NextFrame()
	if !ok || delay <= 0 || delay > terminalFindRefreshInterval {
		t.Fatalf("deferred find scheduled frame delay=%v ok=%t, want within %v", delay, ok, terminalFindRefreshInterval)
	}
}
