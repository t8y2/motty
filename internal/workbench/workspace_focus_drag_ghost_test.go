package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"testing"
)

func TestWorkspaceFocusTabGhostFollowsPointerAndEndsWithDrag(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	state.togglePaneMaximized(0)
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	panes := state.deck.Panes(state.deck.ActiveWorkspace(), nil)
	target := hitWorkspaceFocusTab.At(int(panes[0].ID))
	hit, ok := hitByID(&canvas, target)
	if !ok {
		t.Fatal("missing bottom tab")
	}
	source, _ := state.sessionByIDPointer(uint32(panes[0].SessionID))
	start := gfx.Point{X: hit.Bounds.X + 40, Y: hit.Bounds.Y + 12}
	ghost := func() gfx.Rect {
		t.Helper()
		canvas.Reset()
		state.Paint(&canvas, size)
		shadow := false
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Layer == uint16(ui.LayerDrag) && rectangle.Color == (gfx.Color{Alpha: .22}) {
				shadow = true
			}
		}
		if !shadow {
			t.Fatal("drag preview shadow is missing")
		}
		for _, text := range canvas.Texts() {
			if text.Layer == uint16(ui.LayerDrag) && text.Value == source.Title {
				return text.Bounds
			}
		}
		t.Fatal("drag preview title is missing from drag layer")
		return gfx.Rect{}
	}
	noGhost := func() {
		t.Helper()
		canvas.Reset()
		state.Paint(&canvas, size)
		for _, text := range canvas.Texts() {
			if text.Layer == uint16(ui.LayerDrag) {
				t.Fatal("preview remained after drag ended")
			}
		}
	}
	press(state, target, start)
	first := gfx.Point{X: start.X + 100, Y: start.Y - 40}
	dragTo(state, target, first)
	before := ghost()
	second := gfx.Point{X: first.X + 80, Y: first.Y - 30}
	dragTo(state, target, second)
	after := ghost()
	if after.X-before.X != 80 || after.Y-before.Y != -30 {
		t.Fatalf("preview did not follow pointer: %+v -> %+v", before, after)
	}
	// 预览不创建命中区域，鼠标事件继续由源标签捕获。
	for _, hit := range canvas.Hits() {
		if hit.Layer == uint16(ui.LayerDrag) {
			t.Fatal("preview intercepts pointer input")
		}
	}
	release(state, target, second)
	noGhost()
	press(state, target, start)
	dragTo(state, target, first)
	ghost()
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	noGhost()
}
