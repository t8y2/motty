package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// The window bar leaves room for what the platform keeps of the strip's two
// ends: the traffic lights on macOS, the buttons Viva draws on Windows and Linux.
func TestWindowBarStaysClearOfTheWindowButtons(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	var canvas gfx.Canvas
	const width = 1400
	state.Paint(&canvas, gfx.Size{Width: width, Height: 900})
	leading, trailing := app.TitlebarInsets()

	var leftmost, rightmost float32 = width, 0
	for _, hit := range canvas.Hits() {
		if hit.Bounds.Y >= windowBarHeight || hit.MovesWindow {
			continue
		}
		leftmost = min(leftmost, hit.Bounds.X)
		rightmost = max(rightmost, hit.Bounds.X+hit.Bounds.Width)
	}
	if leftmost < leading {
		t.Errorf("a window-bar control starts at %.0f, inside the %.0f the platform keeps", leftmost, leading)
	}
	if rightmost > width-trailing {
		t.Errorf("a window-bar control ends at %.0f, inside the %.0f the platform keeps at the end", rightmost, trailing)
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
// that column rather than beside it, so the two right edges read as one line.
func TestTheWindowBarEndsOverTheSideRail(t *testing.T) {
	if _, trailing := app.TitlebarInsets(); trailing > 0 {
		t.Skip("the platform draws its window controls in the bar's right corner")
	}
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
