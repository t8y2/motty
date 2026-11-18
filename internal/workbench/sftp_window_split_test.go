package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The panes meet at the one-pixel line the painted Resizable drew, not at an
// eight-pixel strip, and a double click returns them to half each.
func TestSFTPWindowSplitterKeepsTheHairlineAndHalfOnDoubleClick(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	w := &sftpWindow{owner: state, session: *session, view: session.files}
	w.ui.body = gfx.Rect{Width: 1000, Height: 600}
	w.ui.split = ui.SplitterState{Pos: (w.ui.body.Width - sftpWindowSplitExtent) / 2, Ratio: .5}

	span := w.ui.body.Width - sftpWindowSplitExtent
	if sftpWindowSplitExtent != 1 {
		t.Fatalf("splitter takes %v pixels, want the painted hairline", sftpWindowSplitExtent)
	}
	if got := w.paneSplitWidth(w.ui.body.Width); math.Abs(float64(got-span/2)) > .01 {
		t.Fatalf("half split = %v, want %v", got, span/2)
	}

	before := w.paneSplitWidth(w.ui.body.Width)
	if result, ok := w.moveSplitter(gfx.KeyEvent{Key: gfx.KeyRight, Target: hitSFTPWindowSplit}); !ok || !result.Handled {
		t.Fatal("Right did not move the splitter")
	}
	after := w.paneSplitWidth(w.ui.body.Width)
	if got, want := after-before, float32(.05*span); math.Abs(float64(got-want)) > .01 {
		t.Fatalf("Right moved %v pixels, want five percent (%v)", got, want)
	}

	// Leave it off center, then double click the divider.
	w.ui.split.Ratio = .8
	w.ui.split.Pos = .8 * span
	w.pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitSFTPWindowSplit, ClickCount: 2,
	})
	if got := w.paneSplitWidth(w.ui.body.Width); math.Abs(float64(got-span/2)) > .01 {
		t.Fatalf("double click left the split at %v, want %v", got, span/2)
	}
}
