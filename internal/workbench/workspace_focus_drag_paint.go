package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) drawWorkspaceFocusTabDrag(d *ui.DrawContext, index int) {
	state.focusTabs.bounds[index] = d.Rect
	drag := &state.focusTabs.drag
	if !drag.active || drag.boundary < 0 {
		return
	}
	x := d.Rect.X
	if drag.boundary != index {
		if index != len(state.panes)-1 || drag.boundary != len(state.panes) {
			return
		}
		x += d.Rect.Width - 2
	}
	d.Canvas.Fill(gfx.Rect{X: x, Y: d.Rect.Y + 2, Width: 2, Height: d.Rect.Height - 4}, state.pal.accent)
}

// 拖到标签条边缘时连续滚动，命中位置始终使用滚动后的实际矩形。
func (state *State) scrollWorkspaceFocusDrag() {
	drag, scroll := &state.focusTabs.drag, state.focusTabs.scroll
	if !drag.active || scroll == nil || !state.workspaceFocusDragValid() {
		return
	}
	boundary := state.workspaceFocusDropBoundary(drag.at)
	changed := boundary != drag.boundary
	drag.boundary = boundary
	if drag.boundary < 0 {
		return
	}
	frame := state.workspaceFocusTabsBounds(drag.workspace, state.viewport)
	delta := float32(0)
	if drag.at.X < frame.X+24 {
		delta = -12
	}
	if drag.at.X > frame.X+frame.Width-24 {
		delta = 12
	}
	next := min(max(scroll.OffsetX+delta, 0), max(scroll.ContentWidth-scroll.ViewportWidth, 0))
	if next != scroll.OffsetX {
		scroll.ScrollToX(next)
		changed = true
	}
	if changed && state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}
