package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// gridDragConfig routes pointer and key events through the window page, where
// the card grid's DragState lives.
func gridDragConfig(state *State) app.Config {
	return state.WindowConfig(app.Config{Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key})
}

func gridGroupHit(t *testing.T, canvas *gfx.Canvas, state *State, path string) gfx.Hit {
	t.Helper()
	index := state.hostGroupIndex(path)
	if index < 0 {
		t.Fatalf("missing group %q", path)
	}
	hit, ok := hitByID(canvas, hitHostGroupOpenList.At(index))
	if !ok {
		t.Fatalf("group %q has no header", path)
	}
	return hit
}

// dragGridGroup starts a drag on a group header and moves onto target. It
// returns the config so the caller can release or cancel.
func dragGridGroup(t *testing.T, state *State, canvas *gfx.Canvas, config app.Config, from, to gfx.Hit, size gfx.Size) {
	t.Helper()
	x, y := from.Bounds.X+from.Bounds.Width/2, from.Bounds.Y+from.Bounds.Height/2
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height/2
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x, Y: y, Viewport: size})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x + 20, Y: y + 20, Viewport: size})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size})
}

func releaseGridGroup(config app.Config, from, to gfx.Hit, size gfx.Size) {
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height/2
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size})
}

// A group dragged onto another in the card grid moves there, and the target is
// highlighted while the drag is over it.
func TestHostGridGroupDragMovesSubtreeAndHighlightsTarget(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := gridGroupHit(t, canvas, state, "Team/Prod")
	target := gridGroupHit(t, canvas, state, "Team2")
	config := gridDragConfig(state)
	dragGridGroup(t, state, canvas, config, source, target, size)

	if !state.hostGridDrag.state.Dragging() || state.hostGridDrag.state.Target() != target.ID {
		t.Fatalf("grid drag source=%v target=%v dragging=%v", state.hostGridDrag.state.Source(), state.hostGridDrag.state.Target(), state.hostGridDrag.state.Dragging())
	}
	highlight, _ := paintAt(state, size)
	found := false
	for _, rect := range highlight.Rectangles() {
		if rect.Color == state.pal.accent && rect.Bounds.Height == 2 &&
			rect.Bounds.Y >= target.Bounds.Y-1 && rect.Bounds.Y <= target.Bounds.Y+1 {
			found = true
		}
	}
	if !found {
		t.Fatal("the grid drag did not highlight its target")
	}
	releaseGridGroup(config, source, target, size)
	if state.hosts[0].Group != "Team2/Prod/EU" {
		t.Fatalf("grid drag left the subtree at %q", state.hosts[0].Group)
	}
}

// The grid takes exactly the drops the list takes: not into the source or what
// is inside it, and not beside a group of its own name.
func TestHostGridGroupDragRejectsSelfDescendantAndCollision(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	state.hostGroups = append(state.hostGroups, hostGroup{Name: "Team2/Prod", Expanded: true, Explicit: true})
	state.refreshHostGroups()
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := gridGroupHit(t, canvas, state, "Team/Prod")
	config := gridDragConfig(state)
	for _, path := range []string{"Team/Prod", "Team/Prod/EU", "Team2"} {
		target := gridGroupHit(t, canvas, state, path)
		dragGridGroup(t, state, canvas, config, source, target, size)
		releaseGridGroup(config, source, target, size)
		if state.hosts[0].Group != "Team/Prod/EU" || state.hostGroupIndex("Team2/Prod") < 0 {
			t.Fatalf("grid accepted a drop on %q: hosts=%+v groups=%+v", path, state.hosts, state.hostGroups)
		}
	}
}

// Escape lets go of the grid drag without dropping it.
func TestHostGridGroupDragEscapeCancels(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := gridGroupHit(t, canvas, state, "Team/Prod")
	target := gridGroupHit(t, canvas, state, "Team2")
	config := gridDragConfig(state)
	dragGridGroup(t, state, canvas, config, source, target, size)
	if result := config.OnKey(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled {
		t.Fatal("Escape did not reach the page drag")
	}
	releaseGridGroup(config, source, target, size)
	if state.hosts[0].Group != "Team/Prod/EU" {
		t.Fatalf("a cancelled grid drag moved the subtree to %q", state.hosts[0].Group)
	}
}
