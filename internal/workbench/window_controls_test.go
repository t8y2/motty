package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// The window bar leaves room for what the window buttons cover of the strip's
// two ends, as the window reports it: the traffic lights at the start on macOS,
// the buttons Viva draws at the end on Windows and Linux, and nothing in full
// screen, where the bar takes the room back.
func TestWindowBarStaysClearOfTheWindowButtons(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	const width = 1400
	edges := func(leading, trailing float32) (leftmost, rightmost float32) {
		var canvas gfx.Canvas
		canvas.SetTitlebarInsets(leading, trailing)
		state.Paint(&canvas, gfx.Size{Width: width, Height: 900})
		leftmost = width
		for _, hit := range canvas.Hits() {
			if hit.Bounds.Y >= windowBarHeight || hit.MovesWindow {
				continue
			}
			leftmost = min(leftmost, hit.Bounds.X)
			rightmost = max(rightmost, hit.Bounds.X+hit.Bounds.Width)
		}
		return leftmost, rightmost
	}
	for _, insets := range [][2]float32{{70, 0}, {0, 3 * app.WindowControlWidth}} {
		leftmost, rightmost := edges(insets[0], insets[1])
		if leftmost < insets[0] {
			t.Errorf("insets %v: a window-bar control starts at %.0f, inside the %.0f the buttons cover", insets, leftmost, insets[0])
		}
		if rightmost > width-insets[1] {
			t.Errorf("insets %v: a window-bar control ends at %.0f, inside the %.0f the buttons cover", insets, rightmost, insets[1])
		}
	}
	covered, _ := edges(70, 0)
	if free, _ := edges(0, 0); free >= covered {
		t.Errorf("with no window buttons on the strip the bar still starts at %.0f, as it does beside them", free)
	}
}

// The side rail appears only once a session is connected. The bar's last
// control must not jump sideways when it does: the column is reserved by the
// toolbar size, not by the rail's presence.
func TestTheWindowBarDoesNotShiftWhenASessionConnects(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	before, ok := hitByID(&canvas, hitWindowSettings)
	if !ok {
		t.Fatal("no settings button before connecting")
	}
	inTerminal(t, state)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)
	after, ok := hitByID(&canvas, hitWindowSettings)
	if !ok {
		t.Fatal("no settings button after connecting")
	}
	if before.Bounds.X != after.Bounds.X || before.Bounds.Width != after.Bounds.Width {
		t.Fatalf("the bar's last control moved from %+v to %+v", before.Bounds, after.Bounds)
	}
}

// A side rail is a column of centred tools. The bar's last control sits over
// that column rather than beside it, so the two right edges read as one line,
// where no window buttons cover the bar's end: the canvas here reports none.
func TestTheWindowBarEndsOverTheSideRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	inTerminal(t, state)
	for _, size := range [...]terminalToolbarSize{terminalToolbarSmall, terminalToolbarMedium, terminalToolbarLarge} {
		state.setTerminalToolbarSize(size)
		var canvas gfx.Canvas
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		session, ok := state.sessionByIDPointer(state.activeSessionID)
		if !ok || session.term == nil || session.term.railBounds.Width == 0 {
			t.Fatalf("%v: no side rail", size)
		}
		hit, ok := hitByID(&canvas, hitWindowSettings)
		if !ok {
			t.Fatalf("%v: no settings button in the bar", size)
		}
		rail := session.term.railBounds
		barCenter := hit.Bounds.X + hit.Bounds.Width/2
		railCenter := rail.X + rail.Width/2
		if barCenter < railCenter-0.5 || barCenter > railCenter+0.5 {
			t.Fatalf("%v: the bar's last control centres at %.1f, the rail at %.1f", size, barCenter, railCenter)
		}
	}
}
