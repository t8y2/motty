package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func blinkFrame(blink *cursorBlink, at time.Duration, x, y int) (lit bool, next time.Duration, scheduled bool) {
	var canvas gfx.Canvas
	canvas.SetClock(func() time.Duration { return at })
	canvas.BeginFrame()
	lit = blink.on(&canvas, x, y)
	next, scheduled = canvas.NextFrame()
	return lit, next, scheduled
}

func TestTheCursorBlinksRestartsWhenItMovesAndSettlesWhenNothingHappens(t *testing.T) {
	var blink cursorBlink
	start := 10 * time.Second
	if lit, next, ok := blinkFrame(&blink, start, 3, 1); !lit || !ok || next != cursorBlinkHalf {
		t.Fatalf("first frame: lit=%v next=%v scheduled=%v, want lit with the turn-over due in %v", lit, next, ok, cursorBlinkHalf)
	}
	if lit, _, _ := blinkFrame(&blink, start+cursorBlinkHalf+time.Millisecond, 3, 1); lit {
		t.Error("the cursor is still lit in the second half of the period")
	}
	if lit, _, _ := blinkFrame(&blink, start+2*cursorBlinkHalf+time.Millisecond, 3, 1); !lit {
		t.Error("the cursor did not light again")
	}
	// Typing moves it: it is solid at once, whatever the phase was.
	if lit, _, _ := blinkFrame(&blink, start+cursorBlinkHalf+10*time.Millisecond, 4, 1); !lit {
		t.Error("a cursor that just moved is dark")
	}
	// After a long quiet it stops, and asks for nothing.
	lit, _, ok := blinkFrame(&blink, start+cursorBlinkSpan+time.Hour, 4, 1)
	if !lit || ok {
		t.Errorf("after the quiet: lit=%v, frame scheduled=%v; want it solid and the window idle", lit, ok)
	}
	// Entering the pane again starts it afresh.
	blink.rest(4, 1)
	if lit, _, ok := blinkFrame(&blink, start+cursorBlinkSpan+2*time.Hour, 4, 1); !lit || !ok {
		t.Errorf("a pane that got the keyboard again does not blink: lit=%v scheduled=%v", lit, ok)
	}
}
