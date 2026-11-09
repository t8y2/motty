package workbench

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestNoteSplitScrollFollowsContentAndKeepsDriver(t *testing.T) {
	s, _, _, _ := liveSession(t)
	s.Click(hitNotes)
	s.Click(hitNoteAdd)
	var body strings.Builder
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&body, "## Section %d\n\nA paragraph with **bold** text and 中文内容.\n\n```go\nvar value = %d\n```\n\n", i, i)
	}
	s.notes.body.Insert(body.String())
	s.notes.view.mode = noteSplit
	var c gfx.Canvas
	size := gfx.Size{Width: 1600, Height: 850}
	paint := func() { c.Reset(); s.Paint(&c, size) }
	paint()
	n := &s.notes
	if n.code.MaxScrollY() == 0 || !n.view.splitScroll.active {
		t.Fatal("split has no source layout")
	}
	// Different heights must use the native content mapping, not whole-page percentages.
	n.code.SetScrollY(450)
	s.noteScrollDriver(false)
	paint()
	offset, _ := n.code.OffsetAtY(n.code.ScrollY())
	expected, _ := n.rendered.YAtSource(n.body.String(), offset)
	if math.Abs(float64(n.previewScroll-expected-6)) > .1 {
		t.Fatalf("source mapping: got %v want %v", n.previewScroll, expected+6)
	}
	sy, py := n.code.ScrollY(), n.previewScroll
	paint()
	paint()
	if n.code.ScrollY() != sy || n.previewScroll != py || n.view.splitScroll.preview {
		t.Fatal("programmatic scroll fed back")
	}
	b := n.previewBounds
	s.notesScroll(gfx.ScrollEvent{X: b.X + 20, Y: b.Y + 20, DeltaY: 210})
	paint()
	paint()
	offset, _ = n.rendered.SourceAtY(n.body.String(), max(0, n.previewScroll-6))
	expected, _ = n.code.YAtOffset(offset)
	if math.Abs(float64(n.code.ScrollY()-expected)) > .1 || !n.view.splitScroll.preview {
		t.Fatal("preview did not drive source")
	}
	// A resize retains the driving side's source anchor.
	anchor := n.view.splitScroll.anchor
	size.Width = 1450
	paint()
	paint()
	if delta := n.view.splitScroll.anchor - anchor; delta < -8 || delta > 8 {
		t.Fatalf("resize lost anchor: %d -> %d", anchor, n.view.splitScroll.anchor)
	}
	// Loaded images have no source block. Keep the source at EOF and retain
	// the preview's position inside that appended region across reflow.
	n.imageSource = n.body.String()
	for range 4 {
		n.images = append(n.images, noteImage{bitmap: gfx.Bitmap{Width: 1, Height: 1, Stride: 4, Pix: []byte{0, 0, 0, 255}}})
	}
	paint()
	n.previewScroll = n.rendered.Height() + 106
	s.noteScrollDriver(true)
	paint()
	paint()
	if n.code.ScrollY() != n.code.MaxScrollY() {
		t.Fatal("image region did not map to source EOF")
	}
	size.Width = 1550
	paint()
	paint()
	if math.Abs(float64(n.previewScroll-n.rendered.Height()-106)) > 1 {
		t.Fatal("image region position lost on resize")
	}
	// Both endpoints, including the preview's additional image area.
	n.previewScroll = n.previewHeight - n.previewBounds.Height
	s.noteScrollDriver(true)
	paint()
	paint()
	if n.code.ScrollY() != n.code.MaxScrollY() {
		t.Fatal("preview bottom did not reach source bottom")
	}
	n.code.SetScrollY(0)
	s.noteScrollDriver(false)
	paint()
	if n.previewScroll != 0 {
		t.Fatal("source top did not reach preview top")
	}
	// Caret movement can make the editor reveal a new location without a wheel event.
	n.body.MoveTo(0, false)
	paint()
	n.body.MoveTo(n.body.RuneCount(), false)
	paint()
	paint()
	if n.previewScroll != max(0, n.previewHeight-n.previewBounds.Height) || n.view.splitScroll.preview {
		t.Fatal("caret reveal did not drive preview to bottom")
	}
	// Temporary single-column mode does not link scrolls; returning starts at source.
	size.Width = 850
	paint()
	if n.view.splitScroll.active {
		t.Fatal("narrow source fallback still synchronizes")
	}
	n.previewScroll = 0
	size.Width = 1600
	paint()
	if n.previewScroll == 0 {
		t.Fatal("restored split did not initialize from source")
	}
}
