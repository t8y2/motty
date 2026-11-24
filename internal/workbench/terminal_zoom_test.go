package workbench

import (
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

// A primary+wheel (Command on macOS, Control on Windows and Linux) resizes the
// terminal's font, the same change the primary+/- shortcuts make: a wheel notch is
// one point, a trackpad gesture accumulates before it steps, primary+Shift stays
// with the terminal's scrolling, and the ends of the range and the saved state are
// shared with the keyboard path.
func TestPrimaryWheelResizesTheTerminalFont(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	session := &state.sessions[0]
	start := state.terminalFontSize

	if result := state.terminalScroll(session, gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: -120}); !result.Handled || !result.Repaint {
		t.Fatalf("wheel up: %+v", result)
	}
	if state.terminalFontSize != start+1 {
		t.Fatalf("after wheel up = %v, want %v", state.terminalFontSize, start+1)
	}
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: 120})
	if state.terminalFontSize != start {
		t.Fatalf("after wheel down = %v, want %v", state.terminalFontSize, start)
	}

	// Half a precise unit does nothing; a full one steps a single point.
	session.term.zoomWheel = 0
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: -30, Precise: true})
	if state.terminalFontSize != start {
		t.Fatalf("half a unit already zoomed to %v", state.terminalFontSize)
	}
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: -30, Precise: true})
	if state.terminalFontSize != start+1 {
		t.Fatalf("a full precise unit did not zoom: %v, want %v", state.terminalFontSize, start+1)
	}

	// Primary+Shift+wheel scrolls the terminal, it does not zoom.
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: keymap.Primary | gfx.ModifierShift, DeltaY: -120})
	if state.terminalFontSize != start+1 {
		t.Fatalf("primary+Shift changed the size to %v, want %v", state.terminalFontSize, start+1)
	}

	options := freshOptions(t)
	options.Keystore = &memKeys{}
	bounded := newTestState(t, options)
	bounded.sessions = append(bounded.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	bounded.activeSessionID = 1
	var frame gfx.Canvas
	bounded.Paint(&frame, gfx.Size{Width: 1400, Height: 900})
	saved := bounded.terminalFontSize
	for range 40 {
		bounded.terminalScroll(&bounded.sessions[0], gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: -120})
	}
	if bounded.terminalFontSize != terminalFontSizeMax {
		t.Fatalf("zoomed in without end: %v", bounded.terminalFontSize)
	}
	// The gesture is still going. A burst writes once the wheel settles rather than
	// on every notch, so forty notches have cost the store nothing yet.
	if restored := newTestState(t, options); restored.terminalFontSize != saved {
		t.Fatalf("a wheel burst wrote mid-gesture: %v, want the saved %v", restored.terminalFontSize, saved)
	}
	// Once it settles the single write lands, with the window still open.
	deadline := time.Now().Add(5 * time.Second)
	for bounded.terminalZoomSaveTimer != nil {
		if time.Now().After(deadline) {
			t.Fatal("the wheel never released its pending save")
		}
		bounded.Wake()
		time.Sleep(20 * time.Millisecond)
	}
	if restored := newTestState(t, options); restored.terminalFontSize != terminalFontSizeMax {
		t.Fatalf("the settled size never reached the store: %v", restored.terminalFontSize)
	}
}

// The wheel is keyed on the application's own modifier alone, so the other one is
// left to whatever the platform does with it: a bare Control+wheel on macOS stays
// the system's zoom gesture, and a Command+wheel on Windows or Linux stays the
// shell's. Neither one resizes this terminal's font.
func TestWheelWithTheOtherModifierDoesNotResize(t *testing.T) {
	other := gfx.ModifierMeta
	if keymap.Primary == gfx.ModifierMeta {
		other = gfx.ModifierControl
	}
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	session := &state.sessions[0]
	start := state.terminalFontSize

	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: other, DeltaY: -120})
	state.terminalScroll(session, gfx.ScrollEvent{Modifiers: other, DeltaY: -120, Precise: true})
	if state.terminalFontSize != start {
		t.Fatalf("%v+wheel resized the font to %v, want %v", other, state.terminalFontSize, start)
	}
	// Nothing of the zoom happened: no write pending.
	if state.terminalZoomSaveTimer != nil {
		t.Fatalf("%v+wheel scheduled a save", other)
	}
}

// The trackpad's leftover travel belongs to the pane it was made in: half a step in
// one pane and half in another make no step in either.
func TestTrackpadZoomTravelDoesNotCarryToAnotherPane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sessions = append(state.sessions, Session{ID: 1, term: newTerminal(80, 24)}, Session{ID: 2, term: newTerminal(80, 24)})
	state.activeSessionID = 1
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	start := state.terminalFontSize
	half := gfx.ScrollEvent{Modifiers: keymap.Primary, DeltaY: -terminalZoomTrackpadUnit / 2, Precise: true}

	state.terminalScroll(&state.sessions[0], half)
	state.terminalScroll(&state.sessions[1], half)
	if state.terminalFontSize != start {
		t.Fatalf("half a step in each of two panes zoomed to %v, want %v", state.terminalFontSize, start)
	}
	state.terminalScroll(&state.sessions[1], half)
	if state.terminalFontSize != start+1 {
		t.Fatalf("a full step in one pane zoomed to %v, want %v", state.terminalFontSize, start+1)
	}
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
