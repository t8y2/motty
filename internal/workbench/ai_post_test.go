package workbench

import "testing"

// With the application's Post wired, posted work runs on the loop it was given
// to and asks for its repaint after the state it changed.
func TestAIWorkRunsThroughTheApplicationPost(t *testing.T) {
	state := newTestState(t, Options{})
	ran, invalidated := false, false
	state.options.Invalidate = func() { invalidated = true }
	state.SetPost(func(fn func()) { fn() })
	if !state.aiPost(func(*State) { ran = true }) {
		t.Fatal("the post was refused")
	}
	if !ran || !invalidated {
		t.Fatalf("ran=%v invalidated=%v", ran, invalidated)
	}
}
