package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A narrow window used to push the list's status column out of sight, so a
// connected host showed no light and no words.
func TestTheListShowsEachHostsStatusInANarrowWindow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 900, Height: 600})
	found := false
	for _, text := range canvas.Texts() {
		if text.Value == state.messages.HostStateIdle {
			found = true
			if text.Bounds.Width <= 0 || text.Bounds.X+text.Bounds.Width > 900 {
				t.Fatalf("the status is painted at %+v in a 900 wide window", text.Bounds)
			}
		}
	}
	if !found {
		t.Fatal("no status is painted")
	}
}
