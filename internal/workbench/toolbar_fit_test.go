package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The hosts toolbar keeps its buttons apart however wide the platform's text
// turns out: on Windows, GDI's semibold label is wider than the regular-weight
// measurement the layout once used, and Connect ended up under Import.
func TestHostsToolbarButtonsNeverOverlapWhateverTheTextWidth(t *testing.T) {
	for _, factor := range []float32{0.5, 0.56, 0.62, 0.7} {
		state := newTestState(t, testOptions(t))
		var canvas gfx.Canvas
		canvas.SetMeasurer(func(value string, size, weight float32) float32 {
			return float32(len([]rune(value))) * size * factor * (1 + weight)
		})
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		ids := []gfx.ID{hitSearch, hitConnect, hitImportMenu, hitAddHost}
		var rects []gfx.Rect
		for _, id := range ids {
			found := false
			for _, hit := range canvas.Hits() {
				if hit.ID == id {
					rects, found = append(rects, hit.Bounds), true
					break
				}
			}
			if !found {
				t.Fatalf("factor %.2f: no hit for %d", factor, id)
			}
		}
		for index := 1; index < len(rects); index++ {
			if rects[index-1].X+rects[index-1].Width > rects[index].X {
				t.Errorf("factor %.2f: control %d (%+v) overlaps control %d (%+v)", factor, ids[index-1], rects[index-1], ids[index], rects[index])
			}
		}
		if last := rects[len(rects)-1]; last.X+last.Width > 1400 {
			t.Errorf("factor %.2f: the last button leaves the window: %+v", factor, last)
		}
		if rects[0].Width < 100 {
			t.Errorf("factor %.2f: the search box shrank to %.0f", factor, rects[0].Width)
		}
	}
}
