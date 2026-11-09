package workbench

import (
	"testing"

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
	normal := terminalLayout(size, 8, 16, state.padding())
	state.Click(hitSettingsPadCompact)
	compact := terminalLayout(size, 8, 16, state.padding())
	state.Click(hitSettingsPadRoomy)
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

// narrowFontEngine measures a fixed-pitch font narrower than the reference cell, as the
// platform's text engine does for Consolas.
type narrowFontEngine struct{ gfx.TextEngine }

func (narrowFontEngine) MonoCell(size float32, _ gfx.MonoFace) (float32, float32) {
	return size * 0.5, size * 1.2
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
