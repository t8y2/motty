package rdp

import (
	"testing"

	"motty/internal/desktop"
	rdpcore "motty/internal/rdpcore"
)

// TestPointerShapeSwitchesKinds feeds the pointer callback the four statements
// a server makes and checks what the session tells the workbench.
func TestPointerShapeSwitchesKinds(t *testing.T) {
	session := &Session{}
	if kind, _, version := session.PointerShape(); kind != desktop.PointerNone || version != 0 {
		t.Fatalf("before any statement: kind %v, version %d", kind, version)
	}

	session.onPointer(&rdpcore.PointerUpdate{Type: rdpcore.PointerDefault})
	if kind, _, version := session.PointerShape(); kind != desktop.PointerSystem || version != 1 {
		t.Fatalf("default: kind %v, version %d", kind, version)
	}

	session.onPointer(&rdpcore.PointerUpdate{Type: rdpcore.PointerNull})
	if kind, _, version := session.PointerShape(); kind != desktop.PointerHidden || version != 2 {
		t.Fatalf("null: kind %v, version %d", kind, version)
	}

	// A 2x1 shape, red then green in RGBA, hotspot on the green pixel.
	session.onPointer(&rdpcore.PointerUpdate{
		Type: rdpcore.PointerShape, Width: 2, Height: 1, HotSpotX: 1,
		Data: []byte{255, 0, 0, 255, 0, 255, 0, 128},
	})
	kind, shape, version := session.PointerShape()
	if kind != desktop.PointerBitmap || version != 3 {
		t.Fatalf("shape: kind %v, version %d", kind, version)
	}
	if shape.Width != 2 || shape.Height != 1 || shape.HotX != 1 || shape.HotY != 0 {
		t.Fatalf("shape geometry: %+v", shape)
	}
	// The presenter takes BGRA rows.
	want := []byte{0, 0, 255, 255, 0, 255, 0, 128}
	if string(shape.BGRA) != string(want) {
		t.Fatalf("BGRA = %v, want %v", shape.BGRA, want)
	}

	// A shape that decoded to nothing hides rather than painting garbage.
	session.onPointer(&rdpcore.PointerUpdate{Type: rdpcore.PointerShape})
	if kind, _, _ := session.PointerShape(); kind != desktop.PointerHidden {
		t.Fatalf("empty shape: kind %v", kind)
	}
}

// A cached shape the client has no copy of names nothing the session could show, so the pointer
// the remote last named stays.
func TestAnUnknownCachedPointerLeavesThePointerAlone(t *testing.T) {
	session := &Session{}
	session.onPointer(&rdpcore.PointerUpdate{Type: rdpcore.PointerDefault})
	session.onPointer(&rdpcore.PointerUpdate{Type: rdpcore.PointerCached, CacheIndex: 9})
	if kind, _, version := session.PointerShape(); kind != desktop.PointerSystem || version != 1 {
		t.Fatalf("after an unknown cached pointer: kind %v, version %d", kind, version)
	}
}
