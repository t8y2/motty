package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/ui"
)

// A terminal cell is a whole number of device pixels wide at every display scale. With a
// fractional width, two neighbouring cells shared a pixel column on Windows and the later one's
// color covered a glyph that fills its cell, such as a powerline arrow, with a line the height of
// the row (#279).
func TestTerminalCellsAreWholeDevicePixelsAtEveryScale(t *testing.T) {
	state := newTestState(t, Options{})
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2, 2.25, 3} {
		var canvas ui.Canvas
		canvas.SetPixelScale(scale)
		state.Paint(&canvas, ui.Size{Width: 1000, Height: 700})
		device := float64(state.cellWidth) * float64(scale)
		if state.cellWidth <= 0 || math.Abs(device-math.Round(device)) > 1e-3 {
			t.Errorf("at %vx a terminal cell is %v device pixels wide", scale, device)
		}
	}
}
