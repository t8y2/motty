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
