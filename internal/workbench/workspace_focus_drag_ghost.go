package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// 底部标签复用分屏拖拽的浮动卡片，在窗口拖拽层绘制，避免被标签条裁剪。
func (state *State) declareWorkspaceFocusDragOverlay(c ui.C) {
	drag, ok := state.workspaceFocusGhost()
	if !ok || c.Canvas() == nil {
		return
	}
	if state.focusTabs.ghostDraw == nil {
		state.focusTabs.ghostDraw = state.drawWorkspaceFocusGhost
	}
	viewport := state.windowRect()
	width := tabGhostWidth(c.Canvas(), drag.title)
	at := gfx.Point{X: drag.at.X - width/2, Y: drag.at.Y - barTabHeight/2}
	bounds := state.popupAt(c.Canvas(), viewport, at, width, barTabHeight, 0)
	ui.Overlay(c, ui.OverlaySpec{Band: ui.LayerDrag, Bounds: viewport}).Children(func() {
		ui.Slot(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).
			Draw(state.focusTabs.ghostDraw)
	})
}

func (state *State) workspaceFocusGhost() (tabDrag, bool) {
	drag := &state.focusTabs.drag
	if !drag.active || !state.workspaceFocusDragValid() {
		return tabDrag{}, false
	}
	pane, ok := state.deck.Pane(drag.workspace, drag.pane)
	if !ok {
		return tabDrag{}, false
	}
	ghost := tabDrag{at: drag.at, title: state.messages.LabelNewTab}
	if session, exists := state.sessionByIDPointer(uint32(pane.SessionID)); exists {
		ghost.title, ghost.host, ghost.hasHost, ghost.color = session.Title, session.host, true, session.host.Color
	}
	return ghost, true
}

func (state *State) drawWorkspaceFocusGhost(d *ui.DrawContext) {
	drag, ok := state.workspaceFocusGhost()
	if !ok {
		return
	}
	shadow := d.Rect
	shadow.X += 3
	shadow.Y += 3
	d.Canvas.Rounded(shadow, radius.item, gfx.Color{Alpha: .22})
	state.drawTabGhost(d.Canvas, d.Rect, drag.title, drag.host, drag.hasHost, drag.color)
}
