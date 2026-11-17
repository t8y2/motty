package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// TestSettingsWheelScrollEasesAndTrackpadFollows pins how the page gets to the
// offset the wheel asks for: a mouse wheel notch is eased so a burst glides,
// while a trackpad's precise delta stays under the fingers. A programmatic jump
// is not part of this test; it leaves the zero motion and lands in one paint.
func TestSettingsWheelScrollEasesAndTrackpadFollows(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	size := gfx.Size{Width: 1000, Height: 520}
	var canvas gfx.Canvas
	now := time.Duration(0)
	paint := func() {
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		state.Paint(&canvas, size)
		canvas.EndFrame()
	}
	paint()
	if state.settingsContent <= state.settings.pageView {
		t.Fatalf("the page (%v) fits its view (%v), so the test would prove nothing", state.settingsContent, state.settings.pageView)
	}

	// A wheel notch sets the target; the next frame is on its way there.
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 48, Viewport: size})
	target := state.settingsScroll
	if target <= 0 {
		t.Fatal("the wheel did not scroll the settings page")
	}
	now = 40 * time.Millisecond
	paint() // the frame the new target reaches: the motion starts here
	now = 110 * time.Millisecond
	paint()
	if shown := state.settings.pageShown; shown <= 0 || shown >= target {
		t.Fatalf("70ms into the notch the page shows %v of %v, want a value between", shown, target)
	}
	now = time.Second
	paint()
	if state.settings.pageShown != target {
		t.Fatalf("the notch settled at %v, want %v", state.settings.pageShown, target)
	}

	// A precise event reports the finger's own distance and is not eased.
	state.scrollSettings(gfx.ScrollEvent{DeltaY: 30, Precise: true, Viewport: size})
	target = state.settingsScroll
	if target == state.settings.pageShown {
		t.Fatal("the precise delta did not move the target")
	}
	paint()
	if state.settings.pageShown != target {
		t.Fatalf("a trackpad delta shows %v, want %v in the frame it arrives", state.settings.pageShown, target)
	}
}
