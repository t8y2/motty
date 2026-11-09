package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestKnownHostsToolbarStaysInsideANarrowWindow(t *testing.T) {
	options := freshOptions(t)
	writeKnownHostsFixture(t, options.KnownHostsPath)
	state := newTestState(t, options)
	state.Click(hitKnownHosts)
	var canvas gfx.Canvas
	canvas.SetTree(Tree())
	state.paintKnownHostsWorkspace(&canvas, gfx.Rect{X: 220, Y: 40, Width: 420, Height: 380})
	hit, _ := hitByID(&canvas, hitKnownHostsDelete)
	if hit.Bounds.X+hit.Bounds.Width > 640 {
		t.Fatalf("Delete ends at %v, outside the 640 wide window", hit.Bounds.X+hit.Bounds.Width)
	}
}

// A settings card narrower than a control and its margins keeps the control inside the
// card: the row's name has no room, but the control does not hang out of the card.
func TestSettingsControlStaysInsideANarrowCard(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	canvas.SetTree(Tree())
	for _, width := range []float32{212, 140, 600} {
		bounds := gfx.Rect{X: 100, Y: 50, Width: width, Height: 400}
		f := state.settingsPage.Begin(&canvas, state.Theme(), bounds)
		page := settingsPage{state: state, f: f, width: width, rowHeight: settingsRowHeight}
		var control ui.Box
		f.Column().Fill().Children(func() {
			page.group("", func() {
				control = page.line("A setting", "What it does, in a sentence that wraps when there is room for it.", 300)
			})
		})
		f.End()
		rect := control.Rect()
		if rect.X < bounds.X || rect.X+rect.Width > bounds.X+bounds.Width {
			t.Errorf("in a card %v wide the control spans %v to %v, outside the card (%v to %v)",
				width, rect.X, rect.X+rect.Width, bounds.X, bounds.X+bounds.Width)
		}
	}
}
