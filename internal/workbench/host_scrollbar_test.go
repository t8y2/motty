package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// roundedRects returns the rounded rectangles drawn onto the canvas.
func roundedRects(canvas *ui.Canvas) []ui.Rectangle {
	var out []ui.Rectangle
	for _, rect := range canvas.Rectangles() {
		if rect.Radius > 0 {
			out = append(out, rect)
		}
	}
	return out
}

// TestHostScrollbarIsHiddenWhenTheListFits pins the non-intrusive case: a host
// list shorter than its window paints no scrollbar at all, so the default view
// is unchanged.
func TestHostScrollbarIsHiddenWhenTheListFits(t *testing.T) {
	state := newTestState(t, testOptions(t))
	bounds := ui.Rect{X: 10, Y: 20, Width: 400, Height: 300}
	state.hostsContent = 200 // shorter than the window
	state.hostsScroll = 0
	var canvas ui.Canvas
	state.paintHostScrollbar(&canvas, bounds)
	if got := roundedRects(&canvas); len(got) != 0 {
		t.Fatalf("a list that fits painted %d scrollbar rects, want 0", len(got))
	}
}

// TestHostScrollbarShowsTrackAndThumb checks that an overflowing list paints a
// track and a thumb, and that scrolling down moves the thumb down and leaves the
// track where it was.
func TestHostScrollbarShowsTrackAndThumb(t *testing.T) {
	state := newTestState(t, testOptions(t))
	bounds := ui.Rect{X: 10, Y: 20, Width: 400, Height: 300}
	state.hostsContent = 900
	state.hostsScroll = 0

	var top ui.Canvas
	state.paintHostScrollbar(&top, bounds)
	rects := roundedRects(&top)
	if len(rects) != 2 {
		t.Fatalf("painted %d scrollbar rects, want 2 (track + thumb)", len(rects))
	}
	track, thumb := rects[0], rects[1]
	if track.Bounds.Height <= thumb.Bounds.Height {
		t.Fatalf("track height %v is not taller than thumb %v", track.Bounds.Height, thumb.Bounds.Height)
	}
	// The indicator hugs the right edge of the host list.
	if want := bounds.X + bounds.Width - track.Bounds.Width - 2; track.Bounds.X != want {
		t.Fatalf("track x = %v, want %v", track.Bounds.X, want)
	}

	// Scrolling to the very bottom moves the thumb down but keeps the track fixed.
	state.hostsScroll = state.hostsContent - bounds.Height
	var bottom ui.Canvas
	state.paintHostScrollbar(&bottom, bounds)
	bottomRects := roundedRects(&bottom)
	if len(bottomRects) != 2 {
		t.Fatalf("bottom painted %d rects, want 2", len(bottomRects))
	}
	if bottomRects[0].Bounds != track.Bounds {
		t.Fatal("the track moved while scrolling")
	}
	if bottomRects[1].Bounds.Y <= thumb.Bounds.Y {
		t.Fatalf("bottom thumb y = %v did not move below top thumb y = %v", bottomRects[1].Bounds.Y, thumb.Bounds.Y)
	}
	// The thumb stays inside the track at both ends.
	bottomThumb := bottomRects[1].Bounds
	if bottomThumb.Y+bottomThumb.Height > track.Bounds.Y+track.Bounds.Height+0.01 {
		t.Fatal("thumb extends past the bottom of the track")
	}
}
