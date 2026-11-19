package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestNoteWindowSizeRecordsAndSchedulesSave(t *testing.T) {
	_, options := newWorld(t)
	options.RememberWindowFrame = true
	state := newTestState(t, options)

	targetSize := gfx.Size{Width: 1200, Height: 800}
	state.Resize(targetSize)

	frame, ok := state.RestoredWindowFrame()
	if !ok || frame.Width != 1200 || frame.Height != 800 {
		t.Fatalf("RestoredWindowFrame after Resize = %+v, ok=%v; want 1200x800", frame, ok)
	}

	// Check that timer was scheduled
	if state.winChrome.saveTimer == nil {
		t.Fatal("saveTimer was not scheduled after Resize")
	}

	// Close flushes save
	state.Close()

	// New state should restore the size
	reloaded := newTestState(t, options)
	rFrame, ok := reloaded.RestoredWindowFrame()
	if !ok || rFrame.Width != 1200 || rFrame.Height != 800 {
		t.Fatalf("reloaded window frame = %+v, ok=%v; want 1200x800", rFrame, ok)
	}
}
