package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// TestTerminalCursorThicknessDefaultsToNormal pins the default: without a saved
// choice the bar and the underline draw exactly as they always have, so every
// existing terminal paint is unchanged (issue #299 only adds a choice).
func TestTerminalCursorThicknessDefaultsToNormal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.terminalCursorThickness != cursorNormal {
		t.Fatalf("default thickness = %v, want normal", state.terminalCursorThickness)
	}
	if state.terminalCursorThickness.bar() != 3 || state.terminalCursorThickness.underline() != 2 {
		t.Fatalf("normal sizes = bar %v underline %v, want 3 and 2", state.terminalCursorThickness.bar(), state.terminalCursorThickness.underline())
	}
	// The steps step away from the default in both directions.
	if !(cursorThin.bar() < cursorNormal.bar() && cursorNormal.bar() < cursorThick.bar()) {
		t.Fatalf("bar widths do not step: %v %v %v", cursorThin.bar(), cursorNormal.bar(), cursorThick.bar())
	}
	if !(cursorThin.underline() < cursorNormal.underline() && cursorNormal.underline() < cursorThick.underline()) {
		t.Fatalf("underline heights do not step: %v %v %v", cursorThin.underline(), cursorNormal.underline(), cursorThick.underline())
	}
}

// TestTerminalCursorThicknessPersistsRoundTrip checks a saved choice survives a
// save and load with the same sizes.
func TestTerminalCursorThicknessPersistsRoundTrip(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if !state.setTerminalCursorThickness(cursorThick) {
		t.Fatal("choosing thick reported no change")
	}
	state.save()
	reloaded := newTestState(t, options)
	if reloaded.terminalCursorThickness != cursorThick {
		t.Fatalf("reloaded thickness = %v, want thick", reloaded.terminalCursorThickness)
	}
	if reloaded.terminalCursorThickness.bar() != cursorThick.bar() || reloaded.terminalCursorThickness.underline() != cursorThick.underline() {
		t.Fatalf("reloaded sizes = %v/%v, want %v/%v", reloaded.terminalCursorThickness.bar(), reloaded.terminalCursorThickness.underline(), cursorThick.bar(), cursorThick.underline())
	}
}

// TestCursorThicknessControlMarksTheStoredStep pins the settings control's
// selected segment to the stored choice. The segments are drawn in enum order,
// so a table written in a different order silently highlights the wrong step
// while the persisted value stays correct.
func TestCursorThicknessControlMarksTheStoredStep(t *testing.T) {
	for _, step := range []cursorThickness{cursorNormal, cursorThin, cursorThick} {
		t.Run(step.String(), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.setTerminalCursorThickness(step)
			state.Click(hitSettings)
			state.selectSettingsCategory(settingsTerminal)
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 2400})
			want := map[cursorThickness]gfx.ID{
				cursorNormal: hitSettingsCursorNormal,
				cursorThin:   hitSettingsCursorThin,
				cursorThick:  hitSettingsCursorThick,
			}[step]
			for id, label := range map[gfx.ID]string{
				hitSettingsCursorThin:   "thin",
				hitSettingsCursorNormal: "normal",
				hitSettingsCursorThick:  "thick",
			} {
				hit, ok := hitByID(&canvas, id)
				if !ok {
					t.Fatalf("the %s step is not on the settings page", label)
				}
				if hit.Selected != (id == want) {
					t.Errorf("%s selected=%v, want %v", label, hit.Selected, id == want)
				}
			}
		})
	}
}
