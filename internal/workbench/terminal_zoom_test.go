package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func zoomChord(key string, shift bool) gfx.KeyEvent {
	modifiers := keymap.Primary
	if shift {
		modifiers |= gfx.ModifierShift
	}
	return gfx.KeyEvent{Modifiers: modifiers, Text: key}
}

func TestZoomShortcutsStepTheFontSizeWithinItsRangeAndAreKept(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	for _, test := range []struct {
		event gfx.KeyEvent
		want  float32
	}{
		{zoomChord("=", false), 14},
		{zoomChord("+", true), 15}, // Command+Shift+= types a plus
		{zoomChord("-", false), 14},
		{zoomChord("0", false), 13},
	} {
		if result := state.Shortcut(test.event); !result.Handled || state.terminalFontSize != test.want {
			t.Fatalf("%q: handled %v, size %v, want %v", test.event.Text, result.Handled, state.terminalFontSize, test.want)
		}
	}
	for range 40 {
		state.Shortcut(zoomChord("=", false))
	}
	if state.terminalFontSize != terminalFontSizeMax {
		t.Fatalf("zoomed in without end: %v", state.terminalFontSize)
	}
	for range 40 {
		state.Shortcut(zoomChord("-", false))
	}
	if state.terminalFontSize != terminalFontSizeMin {
		t.Fatalf("zoomed out without end: %v", state.terminalFontSize)
	}
	if state.status != "Terminal font size 9." {
		t.Fatalf("status = %q", state.status)
	}
	if restored := newTestState(t, options); restored.terminalFontSize != terminalFontSizeMin {
		t.Fatalf("restored size %v", restored.terminalFontSize)
	}
}

func TestAZoomChangeRefitsTheTerminalToTheNewGrid(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.fitTerminals()
	before, _ := state.sessions[0].term.screen.Size()
	state.Shortcut(zoomChord("=", false))
	state.Shortcut(zoomChord("=", false))
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	after, _ := state.sessions[0].term.screen.Size()
	if after >= before {
		t.Fatalf("columns %d then %d: a larger font should fit fewer", before, after)
	}
}

func TestFontSizeFromADamagedFileFallsBackAndWholePointsAreKept(t *testing.T) {
	for value, want := range map[float32]float32{0: 13, 5: 13, 9: 9, 12.4: 12, 28: 28, 29: 13, 1e9: 13} {
		if got := normalizeTerminalFontSize(value); got != want {
			t.Errorf("normalize(%v) = %v, want %v", value, got, want)
		}
	}
}

func TestPaddingChangesHowManyCellsFitAndIsKept(t *testing.T) {
	options := freshOptions(t)
	options.Keystore = &memKeys{}
	state := newTestState(t, options)
	size := gfx.Size{Width: 1000, Height: 700}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	normal := terminalLayout(size, 8, 16, state.padding())
	clickSettingsChoiceInRow(t, state, state.messages.Tools.PaddingRow, state.settingsPaddingLabel(padCompact))
	compact := terminalLayout(size, 8, 16, state.padding())
	clickSettingsChoiceInRow(t, state, state.messages.Tools.PaddingRow, state.settingsPaddingLabel(padRoomy))
	roomy := terminalLayout(size, 8, 16, state.padding())
	if !(compact.cols >= normal.cols && normal.cols > roomy.cols) || !(compact.left < normal.left && normal.left < roomy.left) {
		t.Fatalf("compact %+v normal %+v roomy %+v", compact, normal, roomy)
	}
	if restored := newTestState(t, options); restored.terminalPad != padRoomy {
		t.Fatalf("restored padding %v", restored.terminalPad)
	}
	if terminalPadFromString("nonsense") != padNormal {
		t.Fatal("an unknown padding is not the normal one")
	}
}

func TestZoomShortcutsAreCustomisableLikeTheOthers(t *testing.T) {
	state := newTestState(t, testOptions(t))
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: '='}
	if conflict, ok := state.shortcuts.bind(shortcutZoomIn, custom); !ok {
		t.Fatalf("binding zoom in failed (conflict %d)", conflict)
	}
	if got := chordLabel(state.shortcuts.chords.Chord(shortcutZoomIn)); got == "—" {
		t.Fatalf("label = %q", got)
	}
	if _, ok := state.shortcuts.bind(shortcutZoomOut, keymap.Chord{Modifiers: keymap.Primary, Char: '='}); !ok {
		t.Fatal("zoom out could not take the freed chord")
	}
}

// Ctrl+wheel resizes the terminal's font, the same change the Ctrl +/- shortcuts
// make: a wheel notch is one point, a trackpad gesture accumulates before it
// steps, Ctrl+Shift stays with the terminal's scrolling, and the ends of the
// range and the saved state are shared with the keyboard path.
func TestCtrlWheelResizesTheTerminalFont(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	session := &state.sessions[0]
	start := state.terminalFontSize

	if result := state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: -120}); !result.Handled || !result.Repaint {
		t.Fatalf("wheel up: %+v", result)
	}
	if state.terminalFontSize != start+1 {
		t.Fatalf("after wheel up = %v, want %v", state.terminalFontSize, start+1)
	}
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: 120})
	if state.terminalFontSize != start {
		t.Fatalf("after wheel down = %v, want %v", state.terminalFontSize, start)
	}

	// Half a precise unit does nothing; a full one steps a single point.
	state.terminalZoomWheel = 0
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: -30, Precise: true})
	if state.terminalFontSize != start {
		t.Fatalf("half a unit already zoomed to %v", state.terminalFontSize)
	}
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: -30, Precise: true})
	if state.terminalFontSize != start+1 {
		t.Fatalf("a full precise unit did not zoom: %v, want %v", state.terminalFontSize, start+1)
	}

	// Ctrl+Shift+wheel scrolls the terminal, it does not zoom.
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl | gfx.ModifierShift, DeltaY: -120})
	if state.terminalFontSize != start+1 {
		t.Fatalf("Ctrl+Shift changed the size to %v, want %v", state.terminalFontSize, start+1)
	}

	options := freshOptions(t)
	options.Keystore = &memKeys{}
	bounded := newTestState(t, options)
	bounded.sessions = append(bounded.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	bounded.activeSessionID = 1
	var frame gfx.Canvas
	bounded.Paint(&frame, gfx.Size{Width: 1400, Height: 900})
	for range 40 {
		bounded.terminalScroll(&bounded.sessions[0], gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: -120})
	}
	if bounded.terminalFontSize != terminalFontSizeMax {
		t.Fatalf("zoomed in without end: %v", bounded.terminalFontSize)
	}
	if restored := newTestState(t, options); restored.terminalFontSize != terminalFontSizeMax {
		t.Fatalf("restored size %v", restored.terminalFontSize)
	}
}

// A Ctrl+wheel zoom reports what it did: the pane shows its current font size
// as a transient badge over the grid, one that is absent until a gesture and
// clears once its linger deadline passes.
func TestCtrlWheelShowsTheFontSizeBadge(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	session := &state.sessions[0]
	grid := gfx.Rect{Width: 800, Height: 600}

	// Before a gesture the pane has nothing to show.
	if texts := badgeTexts(state, session, grid, time.Second); len(texts) != 0 {
		t.Fatalf("a badge was drawn before any zoom: %v", texts)
	}

	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: gfx.ModifierControl, DeltaY: -120})
	want := fmt.Sprintf("%d px", int(state.terminalFontSize))
	if session.term.zoomLabel != want {
		t.Fatalf("badge = %q, want %q", session.term.zoomLabel, want)
	}
	// The badge shows while it lingers after the last notch,
	if texts := badgeTexts(state, session, grid, session.term.zoomUntil-1); !contains(texts, want) {
		t.Fatalf("zoom badge %q was not drawn: %v", want, texts)
	}
	// ...and clears itself, and its label, once the deadline passes.
	if texts := badgeTexts(state, session, grid, session.term.zoomUntil+1); len(texts) != 0 {
		t.Fatalf("badge outlived its deadline: %v", texts)
	}
	if session.term.zoomLabel != "" {
		t.Fatalf("badge label not cleared after its deadline: %q", session.term.zoomLabel)
	}
}

// badgeTexts paints the zoom badge as it is seen at the given frame time and
// returns the label text it drew.
func badgeTexts(state *State, session *Session, grid gfx.Rect, now time.Duration) []string {
	var canvas gfx.Canvas
	canvas.SetClock(func() time.Duration { return now })
	state.paintTerminalZoomBadge(&canvas, session, grid)
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return texts
}

// narrowFontEngine measures a fixed-pitch font narrower than the reference cell, as the
// platform's text engine does for Consolas.
type narrowFontEngine struct{ gfx.TextEngine }

func (narrowFontEngine) MonoCell(size float32, _ gfx.MonoFace) gfx.MonoCellMetrics {
	return gfx.MonoCellMetrics{Advance: size * 0.5, Ascent: size * 0.9, Descent: size * 0.3}
}

// The terminal's columns come from the font that draws it, as the window's text engine
// measures it: a narrower font fits more columns, and the open terminal takes them with the
// frame that first sees the font, not on the next resize.
func TestTheTerminalCellFollowsTheFontTheWindowMeasures(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	size := gfx.Size{Width: 1400, Height: 900}
	var reference gfx.Canvas
	state.Paint(&reference, size)
	referenceColumns, _ := state.sessions[0].term.screen.Size()

	var measured gfx.Canvas
	measured.SetTextEngine(narrowFontEngine{gfx.ReferenceTextEngine()})
	state.Paint(&measured, size)
	width, height := measured.MonoCellSize(state.terminalFontSize, state.terminalFontFace)
	if state.cellWidth != width || state.cellHeight != height*state.terminalLineHeight.factor() {
		t.Fatalf("cell %vx%v, want the canvas's %vx%v", state.cellWidth, state.cellHeight, width, height)
	}
	if columns, _ := state.sessions[0].term.screen.Size(); columns <= referenceColumns {
		t.Fatalf("columns %d then %d: a narrower font should fit more", referenceColumns, columns)
	}
}
