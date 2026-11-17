//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// What one whole-window paint costs on the workbench, which is what an event that
// changes a little still pays today. The numbers decide whether retained view
// scopes are worth building; see docs/decisions.
func BenchmarkWholeWindowPaint(b *testing.B) {
	size := gfx.Size{Width: 1400, Height: 900}
	run := func(b *testing.B, state *State) {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			canvas.Reset()
			state.Paint(&canvas, size)
		}
		b.ReportMetric(float64(len(canvas.Rectangles())+len(canvas.Texts())+len(canvas.Hits())), "items")
	}
	b.Run("hosts-10", func(b *testing.B) { run(b, hostsState(b, 10)) })
	b.Run("hosts-1000", func(b *testing.B) { run(b, hostsState(b, 1000)) })
	b.Run("terminal-two-panes-files", func(b *testing.B) {
		state, _ := openLocal(b)
		state.Click(hitPaneSplitHorizontal)
		state.Click(hitSFTPOpen)
		run(b, state)
	})
}

// The same, through the driver: paint, reconcile with the retained scene, publish.
func BenchmarkWholeWindowPaintThroughTheDriver(b *testing.B) {
	state, _ := openLocal(b)
	state.Click(hitPaneSplitHorizontal)
	state.Click(hitSFTPOpen)
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{
		Title: "cost", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, &canvas)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	b.ReportAllocs()
	b.ResetTimer()
	// Alternate the pointer between two controls so every event changes the hover
	// and repaints: the smallest change there is, and what a window pays for it.
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
	for i := 0; b.Loop(); i++ {
		point := points[i%2]
		if err := driver.Move(point.X, point.Y); err != nil {
			b.Fatal(err)
		}
	}
}

func hostsState(b testing.TB, count int) *State {
	b.Helper()
	_, options := newWorld(b)
	hosts := make([]savedHost, count)
	for index := range hosts {
		hosts[index] = savedHost{Label: fmt.Sprintf("Host %04d", index), Address: fmt.Sprintf("10.0.%d.%d", index/250, index%250+1), User: "ops", Port: 22}
	}
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		b.Fatal(err)
	}
	return newTestState(b, options)
}

// A page laid out with github.com/vivaui/viva/flex, through the driver with the platform's real text
// measurement: a hover between two buttons repaints the page, and the page measures
// every label on each repaint.
func BenchmarkKnownHostsPageThroughTheDriver(b *testing.B) {
	options := freshOptions(b)
	writeKnownHostsFixture(b, options.KnownHostsPath)
	state := newTestState(b, options)
	state.Click(hitKnownHosts)
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{
		Title: "cost", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, &canvas)
	if err != nil {
		b.Fatal(err)
	}
	defer driver.Close()
	var points [2]gfx.Point
	for index, id := range [2]gfx.ID{hitKnownHostsRefresh, hitKnownHostsRetrust} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				points[index] = gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
				found = true
			}
		}
		if !found {
			b.Fatalf("no control %d on the page", id)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := driver.Move(points[i%2].X, points[i%2].Y); err != nil {
			b.Fatal(err)
		}
	}
}
