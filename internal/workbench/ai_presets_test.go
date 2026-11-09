package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// MiniMax's mark is a magenta-to-orange gradient; flattening it to one color
// would still compile and paint, so check the colors really vary across it.
func TestMiniMaxPresetIconKeepsItsGradient(t *testing.T) {
	var preset aiPreset
	for _, candidate := range aiPresets {
		if candidate.label == "MiniMax" {
			preset = candidate
		}
	}
	if len(preset.icon.Layers) == 0 {
		t.Fatal("the MiniMax preset has no icon")
	}
	var canvas gfx.Canvas
	canvas.DrawIcon(preset.icon, gfx.Rect{Width: 16, Height: 16}, gfx.Color{Alpha: 1})
	var left, right gfx.Color
	leftX, rightX := float32(1e9), float32(-1e9)
	for _, triangle := range canvas.Triangles() {
		colors := [3]gfx.Color{triangle.Color, triangle.Shade[0], triangle.Shade[1]}
		for index, point := range triangle.Points {
			if !triangle.Shaded && index > 0 {
				break
			}
			if point.X < leftX {
				leftX, left = point.X, colors[index]
			}
			if point.X > rightX {
				rightX, right = point.X, colors[index]
			}
		}
	}
	if left.Blue-right.Blue < 0.2 || right.Green-left.Green < 0.2 {
		t.Fatalf("the mark runs from %+v on the left to %+v on the right, want magenta to orange", left, right)
	}
}
