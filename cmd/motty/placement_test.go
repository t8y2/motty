package main

import "testing"

// A torn-off window opens through a placement with no number; it must take a
// fresh one instead of falling back to the first window's, or the two windows
// write the same record and the tray names their sessions alike.
func TestAPlacementWithoutAWindowNumberTakesAFreshOne(t *testing.T) {
	first := placementWindowID(placement{})
	second := placementWindowID(placement{})
	if first == 0 || second == 0 {
		t.Fatalf("placements without a number got %d and %d, want fresh numbers", first, second)
	}
	if first == second {
		t.Fatalf("two placements without a number both got %d", first)
	}
	if got := placementWindowID(placement{windowID: first}); got != first {
		t.Fatalf("an explicit number %d became %d", first, got)
	}
}
