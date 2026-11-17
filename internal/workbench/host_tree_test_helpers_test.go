package workbench

import (
	"github.com/vivaui/viva/gfx"
)

// hostListGroupHit finds a group's row in the list view's Tree. A group row and
// its chevron share a key; only the row itself is not expandable.
func hostListGroupHit(canvas *gfx.Canvas, state *State, groupIndex int) (gfx.Hit, bool) {
	return hostListGroupHitIn(canvas.Hits(), state, groupIndex)
}

func hostListGroupHitIn(hits []gfx.Hit, state *State, groupIndex int) (gfx.Hit, bool) {
	key := 2*groupIndex + 1
	for _, hit := range hits {
		if k, ok := state.hostListHitKey(hit.ID); ok && k == key && !hit.Expandable {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hostListGroupChevron finds the disclosure control of a group row.
func hostListGroupChevronIn(hits []gfx.Hit, state *State, groupIndex int) (gfx.Hit, bool) {
	key := 2*groupIndex + 1
	for _, hit := range hits {
		if k, ok := state.hostListHitKey(hit.ID); ok && k == key && hit.Expandable {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// hostListHostHit finds a host's row in the list view's Tree.
func hostListHostHit(canvas *gfx.Canvas, state *State, hostID uint32) (gfx.Hit, bool) {
	return hostListHostHitIn(canvas.Hits(), state, hostID)
}

func hostListHostHitIn(hits []gfx.Hit, state *State, hostID uint32) (gfx.Hit, bool) {
	for _, hit := range hits {
		entry, ok := state.hostListRowFromHit(hit.ID)
		if ok && entry.Kind == hostTreeHost && state.hosts[entry.HostIndex].ID == hostID {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// dragHostGroup starts a drag on a group row and moves it onto target. The
// canvas returned is the paint the last move produced, where the drop indicator
// is.
func dragHostGroup(state *State, size gfx.Size, from, to gfx.Hit) *gfx.Canvas {
	canvas := &gfx.Canvas{}
	state.Paint(canvas, size)
	x, y := from.Bounds.X+from.Bounds.Width/2, from.Bounds.Y+from.Bounds.Height/2
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height/2
	canvas.DeliverPointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x, Y: y, Viewport: size}, size, state.Paint)
	canvas.DeliverPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: x + 20, Y: y + 20, Viewport: size}, size, state.Paint)
	canvas.DeliverPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size}, size, state.Paint)
	// The paint after the move onto the row is the one that promotes the target
	// this paint found, so move once more to show it.
	canvas.DeliverPointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size}, size, state.Paint)
	return canvas
}

// releaseHostGroup lets go of a drag at target's row.
func releaseHostGroup(state *State, size gfx.Size, canvas *gfx.Canvas, from, to gfx.Hit) {
	tx, ty := to.Bounds.X+to.Bounds.Width/2, to.Bounds.Y+to.Bounds.Height/2
	canvas.DeliverPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: from.ID, X: tx, Y: ty, Viewport: size}, size, state.Paint)
}
