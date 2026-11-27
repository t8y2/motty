package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestScrollbackNormalizesToTheUsableRange(t *testing.T) {
	for _, test := range []struct {
		value int
		want  int
	}{
		{1000, 1000}, {5000, 5000}, {12345, 12345}, {100000, 100000},
		{999, scrollbackLines}, {100001, scrollbackLines}, {0, scrollbackLines}, {-1, scrollbackLines},
	} {
		if got := normalizeTerminalScrollback(test.value); got != test.want {
			t.Errorf("normalizeTerminalScrollback(%d) = %d, want %d", test.value, got, test.want)
		}
	}
}

// The scrollback row is a stepper: −, an editable number, +. The buttons move
// the count in thousands, the field accepts any count inside the range, and
// both ends clamp instead of overshooting.
func TestScrollbackStepperStepsTypesClampsAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	for _, id := range []gfx.ID{hitSettingsScrollbackMinus, hitSettingsScrollbackValue, hitSettingsScrollbackPlus} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("scrollback stepper control %d is not painted", id)
		}
	}
	state.Click(hitSettingsScrollbackPlus)
	if state.terminalScrollback != 6000 {
		t.Fatalf("+ left the count at %d", state.terminalScrollback)
	}
	state.Click(hitSettingsScrollbackMinus)
	if state.terminalScrollback != 5000 {
		t.Fatalf("- left the count at %d", state.terminalScrollback)
	}

	typeScrollback(t, state, "100000")
	if state.terminalScrollback != 100000 {
		t.Fatalf("typed 100000 left the count at %d", state.terminalScrollback)
	}
	if again := newTestState(t, options); again.terminalScrollback != 100000 {
		t.Fatalf("the typed count did not survive a restart: %d", again.terminalScrollback)
	}

	// A value past either end is clamped to the end the stepper buttons stop at.
	typeScrollback(t, state, "999999")
	if state.terminalScrollback != 100000 {
		t.Fatalf("above the maximum stored %d", state.terminalScrollback)
	}
	typeScrollback(t, state, "0")
	if state.terminalScrollback != 1000 {
		t.Fatalf("below the minimum stored %d", state.terminalScrollback)
	}

	// Escape discards what was typed and keeps the stored count.
	state.Click(hitSettingsScrollbackValue)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	input := state.WindowConfig(app.Config{}).TextInput(app.TextInputContext{ID: hitSettingsScrollbackValue})
	if input == nil {
		t.Fatal("the scrollback stepper has no page editor")
	}
	input.Editor.Insert("7")
	state.Key(gfx.KeyEvent{Target: hitSettingsScrollbackValue, Key: gfx.KeyEscape})
	if state.terminalScrollback != 1000 {
		t.Fatalf("Escape stored %d", state.terminalScrollback)
	}
}

func typeScrollback(t *testing.T, state *State, value string) {
	t.Helper()
	state.Click(hitSettingsScrollbackValue)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	input := state.WindowConfig(app.Config{}).TextInput(app.TextInputContext{ID: hitSettingsScrollbackValue})
	if input == nil {
		t.Fatal("the scrollback stepper has no page editor")
	}
	input.Editor.Insert(value)
	state.Key(gfx.KeyEvent{Target: hitSettingsScrollbackValue, Key: gfx.KeyEnter})
}
