package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestMonitorTrendStaysInsideItsBounds(t *testing.T) {
	var canvas gfx.Canvas
	bounds := gfx.Rect{X: 120, Y: 80, Width: 100, Height: 40}
	paintMonitorTrend(&canvas, bounds, monitorChartHits[monitorCPU], []float32{0, 1, 2}, []monitorChartSeries{{
		color:  gfx.RGB(10, 120, 150),
		label:  "CPU",
		values: []float32{10, 50, 20},
	}}, monitorChartOptions{
		xMin: 0, xMax: 2, yMin: 0, yMax: 100, fixedY: true, hiddenAxes: true,
	})

	triangles := canvas.Triangles()
	if len(triangles) == 0 {
		t.Fatal("trend produced no geometry")
	}
	const strokeSlack = 4
	for _, triangle := range triangles {
		for _, point := range triangle.Points {
			if point.X < bounds.X-strokeSlack || point.X > bounds.X+bounds.Width+strokeSlack ||
				point.Y < bounds.Y-strokeSlack || point.Y > bounds.Y+bounds.Height+strokeSlack {
				t.Fatalf("trend point %+v escaped bounds %+v", point, bounds)
			}
		}
	}
	// A translated chart keeps the same gradient and geometry. Path and
	// gradient coordinates are both local to the plot's bounds.
	var origin gfx.Canvas
	paintMonitorTrend(&origin, gfx.Rect{Width: bounds.Width, Height: bounds.Height}, monitorChartHits[monitorCPU], []float32{0, 1, 2}, []monitorChartSeries{{
		color: gfx.RGB(10, 120, 150), label: "CPU", values: []float32{10, 50, 20},
	}}, monitorChartOptions{xMin: 0, xMax: 2, yMin: 0, yMax: 100, fixedY: true, hiddenAxes: true})
	if len(origin.Triangles()) != len(triangles) {
		t.Fatal("translating the chart changed its geometry")
	}
	for i, triangle := range origin.Triangles() {
		if triangle.Color != triangles[i].Color || triangle.Shade != triangles[i].Shade {
			t.Fatalf("translating the chart changed its gradient at triangle %d", i)
		}
	}
}
