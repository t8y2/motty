//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// What a terminal full of TUI glyphs costs to paint: a bordered screen that
// does not change, the same screen with one row rewritten — what a keystroke
// or one scrolling line pays — and a plain ASCII screen with the same churn as
// the control. The box drawing, blocks, shades and braille are the cell glyphs
// Viva draws as geometry, so these numbers track its cell glyph cache and the
// line replacement behind it.
const tuiRows = 30

func tuiRow(row, tick int) string {
	return fmt.Sprintf("\x1b[%d;1H│ cpu ▁▂▃▅▂▁ %3d%% │ mem ▓▓▒░ %3d%% │ net ─────┤ ⠿ ├ io █▀▄ ┼──┼ │",
		row+1, (row*7+tick)%100, (row*13+tick)%100)
}

func asciiRow(row, tick int) string {
	return fmt.Sprintf("\x1b[%d;1H| cpu load %3d%% | mem used %3d%% | net ------ | io r/w -- |",
		row+1, (row*7+tick)%100, (row*13+tick)%100)
}

func paintTerminalCost(b *testing.B, row func(row, tick int) string, rewrite bool) {
	state, _, _, shell := liveSession(b)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	shell.say("\x1b[2J\x1b[H")
	for at := 0; at < tuiRows; at++ {
		shell.say(row(at, 0))
	}
	state.Wake()
	var canvas gfx.Canvas
	state.Paint(&canvas, size) // settle: the caches warm and the layout measures
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if rewrite {
			shell.say(row(i%tuiRows, i+1))
			state.Wake()
		}
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	b.ReportMetric(float64(len(canvas.Rectangles())+len(canvas.Texts())+len(canvas.Hits())), "items")
}

func BenchmarkPaintTerminalTUIUnchanged(b *testing.B) {
	paintTerminalCost(b, tuiRow, false)
}

func BenchmarkPaintTerminalTUIOneRowRewritten(b *testing.B) {
	paintTerminalCost(b, tuiRow, true)
}

func BenchmarkPaintTerminalASCIIOneRowRewritten(b *testing.B) {
	paintTerminalCost(b, asciiRow, true)
}

// The same TUI churn through the driver: paint, reconcile with the scene, and
// publish — the path that builds the cell glyph geometry and replaces the cell
// glyphs in the lines the font draws. A move between two controls repaints, as
// BenchmarkWholeWindowPaintThroughTheDriver does.
func BenchmarkPaintTerminalTUIOneRowRewrittenThroughTheDriver(b *testing.B) {
	state, _, _, shell := liveSession(b)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	shell.say("\x1b[2J\x1b[H")
	for at := 0; at < tuiRows; at++ {
		shell.say(tuiRow(at, 0))
	}
	state.Wake()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{
		Title: "cost", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, &canvas)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	hits := driver.Snapshot().Hits
	var points [2]gfx.Point
	found := 0
	for _, hit := range hits {
		if !hit.Block && hit.Role == gfx.RoleButton && found < 2 {
			points[found] = gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
			found++
		}
	}
	if found < 2 {
		b.Fatal("no two buttons to move between")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		shell.say(tuiRow(i%tuiRows, i+1))
		state.Wake()
		point := points[i%2]
		if err := driver.Move(point.X, point.Y); err != nil {
			b.Fatal(err)
		}
	}
}
