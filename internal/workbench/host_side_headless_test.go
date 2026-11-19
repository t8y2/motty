//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

// The hosts divider is a page Splitter: dragging it through the real window
// widens the tree pane, and its pointer strip stays the old eight pixels.
func TestHostsSideSplitterDragsThroughThePage(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.hostSide.open = true
	state.revealHostFilterGroup()
	state.rebuildHostTreeRows()
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	bar, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostSideWidth)
	if !ok {
		t.Fatal("the hosts page has no divider")
	}
	if bar.Bounds.Width != paneDividerHitWidth {
		t.Fatalf("divider pointer strip is %v wide, want %v", bar.Bounds.Width, paneDividerHitWidth)
	}
	before := state.hostSide.width
	x, y := bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x+80, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x+80, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := state.hostSide.width; got <= before {
		t.Fatalf("dragging the divider right left the pane at %v, was %v", got, before)
	}
}
