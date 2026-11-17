package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A window so short that the host menu's rows do not fit: the menu scrolls,
// the keyboard reaches its last item, the wheel and the scrollbar move it, and it
// starts at the top again when it is opened another time.
func TestAMenuTallerThanTheWindowScrollsAndItsLastItemIsReachable(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := gfx.Size{Width: 900, Height: 110}
	paint := func() *gfx.Canvas {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		return &canvas
	}
	inside := func(canvas *gfx.Canvas, id gfx.ID) bool {
		hit, ok := hitByID(canvas, id)
		surface := state.popups.surface
		return ok && hit.Bounds.Y >= surface.Y && hit.Bounds.Y+hit.Bounds.Height <= surface.Y+surface.Height
	}
	paint() // lays the page out, so the viewport is known
	rightClickHost(state, state.hosts[0].ID)
	canvas := paint()
	canvas = paint()
	if state.popups.scroll.Max() <= 0 {
		t.Fatalf("a 110px window left the host menu with nothing to scroll (surface %+v)", state.popups.surface)
	}
	if inside(canvas, hitHostMenuDelete) {
		t.Fatal("the last item shows before anything scrolled")
	}

	result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnd, Target: hitHostMenuConnect})
	if !result.Handled || result.Focus != hitHostMenuDelete {
		t.Fatalf("End: %+v, want focus on Delete", result)
	}
	if !inside(paint(), hitHostMenuDelete) {
		t.Fatal("End did not scroll the last item into view")
	}
	surface := state.popups.surface
	if result := state.Scroll(gfx.ScrollEvent{X: surface.X + 20, Y: surface.Y + 20, DeltaY: -1000}); !result.Handled || state.popups.scroll.Offset != 0 {
		t.Fatalf("wheel up: %+v offset %v", result, state.popups.scroll.Offset)
	}

	// The scrollbar drags: press its track low, release, and the end shows.
	bar, ok := hitByID(paint(), hitMenuScrollBar)
	if !ok {
		t.Fatal("a scrolling menu declares no scrollbar")
	}
	for _, kind := range []gfx.PointerEventKind{gfx.PointerDown, gfx.PointerMove, gfx.PointerUp} {
		if r := pointer(state, gfx.PointerEvent{Kind: kind, Button: gfx.PointerButtonPrimary, Target: hitMenuScrollBar, X: bar.Bounds.X + 2, Y: bar.Bounds.Y + bar.Bounds.Height - 1, Viewport: size}); !r.Handled {
			t.Fatalf("scrollbar %v was not taken: %+v", kind, r)
		}
	}
	if !state.hostMenu.open || !inside(paint(), hitHostMenuDelete) {
		t.Fatalf("after dragging: menu open %v", state.hostMenu.open)
	}

	// Choosing the last item works, and the next menu opens at its top.
	state.Click(hitHostMenuDelete)
	if state.modal != modalDeleteHost {
		t.Fatalf("clicking the last item: modal %v", state.modal)
	}
	state.Click(hitHostDeleteCancel)
	paint()
	rightClickHost(state, state.hosts[0].ID)
	paint()
	if state.popups.scroll.Offset != 0 {
		t.Fatalf("the reopened menu starts %v down", state.popups.scroll.Offset)
	}
}

// An open menu is modal: the wheel outside its surface does not scroll the page
// behind it, as Base UI's menu locks page scroll.
func TestWheelOutsideAnOpenMenuDoesNotScrollThePageBehindIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	size := gfx.Size{Width: 900, Height: 600}
	paint := func() {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
	}
	paint()
	rightClickHost(state, state.hosts[0].ID)
	paint()
	if state.popups.owner.IsZero() {
		t.Fatal("the menu did not claim a popup position")
	}
	result := state.Scroll(gfx.ScrollEvent{X: 20, Y: 20, DeltaY: 120})
	if !result.Handled {
		t.Fatal("the wheel outside the menu reached the page")
	}
}
