package workbench

import (
	"testing"

	"motty/internal/desktop"
)

// pointingDesktop is a session whose remote names pointer shapes.
type pointingDesktop struct {
	*followingDesktop
	kind    desktop.PointerKind
	shape   desktop.PointerShape
	version int
}

func (s *pointingDesktop) PointerShape() (desktop.PointerKind, desktop.PointerShape, int) {
	return s.kind, s.shape, s.version
}

// TestPresentPointerFollowsTheRemoteStatements checks that the view copies the
// bitmap when the version moves, resizes when the shape's size changes, and
// does nothing when the remote has said nothing new.
func TestPresentPointerFollowsTheRemoteStatements(t *testing.T) {
	remote := newFollowingDesktop(800, 600, false)
	source := &pointingDesktop{followingDesktop: remote}
	view := newDesktopView(source, false)

	// Nothing named yet: no work, no bitmap.
	view.presentPointer()
	if kind := view.pointerKind; kind != desktop.PointerNone {
		t.Fatalf("before any statement: kind %v", kind)
	}
	if width, height := view.pointerSurface.Size(); width != 0 || height != 0 {
		t.Fatalf("a bitmap exists before any statement: %dx%d", width, height)
	}

	// A system pointer: remembered, no bitmap.
	source.version, source.kind = 1, desktop.PointerSystem
	view.presentPointer()
	if view.pointerKind != desktop.PointerSystem {
		t.Fatalf("system: kind %v", view.pointerKind)
	}
	if width, _ := view.pointerSurface.Size(); width != 0 {
		t.Fatalf("system pointer painted a bitmap")
	}

	// A bitmap: copied at its size, hotspot remembered.
	source.version, source.kind = 2, desktop.PointerBitmap
	source.shape = desktop.PointerShape{Width: 3, Height: 2, HotX: 2, HotY: 1, BGRA: make([]byte, 3*2*4)}
	view.presentPointer()
	if width, height := view.pointerSurface.Size(); width != 3 || height != 2 {
		t.Fatalf("bitmap size: %dx%d", width, height)
	}
	if view.pointerHotX != 2 || view.pointerHotY != 1 {
		t.Fatalf("hotspot: %d,%d", view.pointerHotX, view.pointerHotY)
	}

	// The same statement again: the version has not moved, nothing is copied.
	source.shape = desktop.PointerShape{Width: 9, Height: 9, HotX: 0, HotY: 0, BGRA: make([]byte, 9*9*4)}
	view.presentPointer()
	if width, height := view.pointerSurface.Size(); width != 3 || height != 2 {
		t.Fatalf("unchanged statement repainted: %dx%d", width, height)
	}

	// Hidden: remembered, the bitmap stays but is not the kind to paint.
	source.version, source.kind = 3, desktop.PointerHidden
	view.presentPointer()
	if view.pointerKind != desktop.PointerHidden {
		t.Fatalf("hidden: kind %v", view.pointerKind)
	}
}
