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

// dragGridGroup starts a drag on a group header and moves onto the middle of
// target's header, where a drop joins the group. It returns nothing; the caller
// releases or cancels through the same config.
func dragGridGroup(t *testing.T, state *State, canvas *gfx.Canvas, config app.Config, from, to gfx.Hit, size gfx.Size) {
	t.Helper()
	dragGridGroupAt(t, state, canvas, config, from, to, size, 0.5)
}

// dragGridGroupAt starts the drag and moves onto target's header at down, the
// share of the header's height the pointer rests at: its top and bottom quarters
// order the group beside the target, its middle joins it.
func dragGridGroupAt(t *testing.T, state *State, canvas *gfx.Canvas, config app.Config, from, to gfx.Hit, size gfx.Size, down float32) {
	t.Helper()
	x, y := from.Bounds.X+from.Bounds.Width/2, from.Bounds.Y+from.Bounds.Height/2
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height*down
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x, Y: y, Viewport: size})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x + 20, Y: y + 20, Viewport: size})
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size})
}

func releaseGridGroup(config app.Config, from, to gfx.Hit, size gfx.Size) {
	releaseGridGroupAt(config, from, to, size, 0.5)
}

// releaseGridGroupAt lets go at the same point dragGridGroupAt moved to.
func releaseGridGroupAt(config app.Config, from, to gfx.Hit, size gfx.Size, down float32) {
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height*down
	config.OnPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size})
}

// gridDragMarks reads the marks a group header's drag paints at target: the line
// above or below its header, and the wash and edge around it a join paints.
func gridDragMarks(state *State, canvas *gfx.Canvas, target gfx.Hit) (above, below, join bool) {
	wash := mixColor(state.pal.surfaceSoft, state.pal.accent, hostGridDropWash)
	top, bottom := target.Bounds.Y, target.Bounds.Y+target.Bounds.Height-1
	for _, rect := range canvas.Rectangles() {
		y := rect.Bounds.Y
		switch {
		case rect.Color == state.pal.accent && rect.Bounds.Height == 2 && y >= top-1 && y <= top+1:
			above = true
		case rect.Color == state.pal.accent && rect.Bounds.Height == 2 && y >= bottom-1 && y <= bottom+1:
			below = true
		case rect.Color == wash && rect.Radius > 0 &&
			rect.Bounds.Y >= target.Bounds.Y &&
			rect.Bounds.Y+rect.Bounds.Height <= target.Bounds.Y+target.Bounds.Height &&
			rect.Bounds.Height >= target.Bounds.Height*0.75:
			join = true
		}
	}
	return above, below, join
}

// A group dragged onto the middle of another in the card grid joins it, and the
// target is marked as the group it would join: a wash around its whole header,
// never a line above it, which would read as an insertion there.
func TestHostGridGroupDragMovesSubtreeAndMarksTheGroupItJoins(t *testing.T) {
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
	above, below, join := gridDragMarks(state, highlight, target)
	if !join || above || below {
		t.Fatalf("a join marked above=%v below=%v join=%v", above, below, join)
	}
	releaseGridGroup(config, source, target, size)
	if state.hosts[0].Group != "Team2/Prod/EU" {
		t.Fatalf("grid drag left the subtree at %q", state.hosts[0].Group)
	}
}

// Dropping a group on the strip above or below another header orders it beside
// that group in the target's folder, instead of joining it. The line the drag
// paints is on the edge the group lands.
func TestHostGridGroupDroppedOnHeaderEdgeReordersInsteadOfJoining(t *testing.T) {
	for _, drop := range []struct {
		name string
		down float32
		want bool // want the dragged group after the target
	}{
		{"above", hostGridDropShare / 2, false},
		{"below", 1 - hostGridDropShare/2, true},
	} {
		t.Run(drop.name, func(t *testing.T) {
			state := nestedHostGroupState(t)
			state.hostView = hostViewGrid
			size := gfx.Size{Width: 1400, Height: 900}
			canvas, _ := paintAt(state, size)
			source := gridGroupHit(t, canvas, state, "Team2")
			target := gridGroupHit(t, canvas, state, "Team")
			config := gridDragConfig(state)
			dragGridGroupAt(t, state, canvas, config, source, target, size, drop.down)

			zone := hitHostGroupDropBeforeList.At(state.hostGroupIndex("Team"))
			if drop.want {
				zone = hitHostGroupDropAfterList.At(state.hostGroupIndex("Team"))
			}
			if state.hostGridDrag.state.Target() != zone {
				t.Fatalf("the drag over the %s strip targets %s, want %s", drop.name, tree.Text(state.hostGridDrag.state.Target()), tree.Text(zone))
			}
			marked, _ := paintAt(state, size)
			above, below, join := gridDragMarks(state, marked, target)
			if above != !drop.want || below != drop.want || join {
				t.Fatalf("the %s drop marked above=%v below=%v join=%v", drop.name, above, below, join)
			}
			releaseGridGroupAt(config, source, target, size, drop.down)
			sourceAt, targetAt := state.hostGroupIndex("Team2"), state.hostGroupIndex("Team")
			if sourceAt < 0 || targetAt < 0 {
				t.Fatalf("the drop lost a group: groups=%+v", state.hostGroups)
			}
			if wantAfter := sourceAt > targetAt; wantAfter != drop.want {
				t.Fatalf("the %s drop left Team2 at %d and Team at %d", drop.name, sourceAt, targetAt)
			}
			if hostGroupParent("Team2") != "" {
				t.Fatalf("the %s drop joined Team2 instead of ordering it", drop.name)
			}
			if state.hosts[2].Group != "Team2" {
				t.Fatalf("the %s drop moved a host into %q", drop.name, state.hosts[2].Group)
			}
		})
	}
}

// A nested group dropped on the edge of a root group leaves its folder: the line
// the drag paints above a root header is the way back out that the middle of a
// header never was.
func TestHostGridGroupDroppedOnHeaderEdgeLeavesItsParent(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := gridGroupHit(t, canvas, state, "Team/Prod")
	target := gridGroupHit(t, canvas, state, "Team2")
	config := gridDragConfig(state)
	strip := hostGridDropShare / 2
	dragGridGroupAt(t, state, canvas, config, source, target, size, strip)
	releaseGridGroupAt(config, source, target, size, strip)

	if state.hosts[0].Group != "Prod/EU" {
		t.Fatalf("the edge drop left the subtree at %q, want it out of Team", state.hosts[0].Group)
	}
	moved, tail := state.hostGroupIndex("Prod"), state.hostGroupIndex("Team2")
	if moved < 0 || tail < 0 || moved > tail {
		t.Fatalf("the edge drop left Prod at %d and Team2 at %d", moved, tail)
	}
}

// The grid takes exactly the drops the list takes: not into the source or what
// is inside it, and not beside a group of its own name, whatever part of the
// target's header the pointer is over.
func TestHostGridGroupDragRejectsSelfDescendantAndCollision(t *testing.T) {
	state := nestedHostGroupState(t)
	state.hostView = hostViewGrid
	// "Prod" collides with the leaf a drop beside Team2 would create, and
	// "Team2/Prod" with the one a drop into Team2 would.
	state.hostGroups = append(state.hostGroups,
		hostGroup{Name: "Prod", Expanded: true, Explicit: true},
		hostGroup{Name: "Team2/Prod", Expanded: true, Explicit: true})
	state.refreshHostGroups()
	size := gfx.Size{Width: 1400, Height: 900}
	canvas, _ := paintAt(state, size)
	source := gridGroupHit(t, canvas, state, "Team/Prod")
	config := gridDragConfig(state)
	for _, path := range []string{"Team/Prod", "Team/Prod/EU", "Team2"} {
		for _, down := range []float32{hostGridDropShare / 2, 0.5, 1 - hostGridDropShare/2} {
			target := gridGroupHit(t, canvas, state, path)
			dragGridGroupAt(t, state, canvas, config, source, target, size, down)
			releaseGridGroupAt(config, source, target, size, down)
			if state.hosts[0].Group != "Team/Prod/EU" || state.hostGroupIndex("Team2/Prod") < 0 {
				t.Fatalf("grid accepted a drop on %q at %v: hosts=%+v groups=%+v", path, down, state.hosts, state.hostGroups)
			}
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
