package rdp

import (
	"testing"

	"motty/internal/rdpcore/protocol/pointer"
)

// TestCachedPointerReadsTheSlotFromAnyGoroutine stores a shape the way the
// read loop does and asks for it back, including slots that hold nothing.
func TestCachedPointerReadsTheSlotFromAnyGoroutine(t *testing.T) {
	client := &Client{}
	shape := &pointer.PointerUpdate{Width: 4, Height: 4, Data: []byte{1, 2, 3, 4}}
	client.cacheAndNotifyPointer(&pointer.PointerUpdate{
		CacheIndex: 7, Width: shape.Width, Height: shape.Height, Data: shape.Data,
	})
	stored := client.CachedPointer(7)
	if stored == nil || stored.Width != 4 {
		t.Fatalf("slot 7: %v", stored)
	}
	if got := client.CachedPointer(8); got != nil {
		t.Fatalf("empty slot 8: %v", got)
	}
	if got := client.CachedPointer(128); got != nil {
		t.Fatalf("out-of-range slot: %v", got)
	}
}
