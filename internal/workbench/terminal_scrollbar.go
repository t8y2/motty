package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// terminalBarLinger is how long the scrollbar stays after the view last moved.
const terminalBarLinger = 1000 * time.Millisecond

// terminalScrollbarShown reports whether the terminal has history to scroll
// through. The alternate screen has none, and a synchronized update is a frozen
// picture of the screen, not a place to move in.
func (t *terminal) terminalScrollbarShown() bool {
	if t.sync != nil || t.presentedAltScreen() {
		return false
	}
	_, rows := t.screen.Size()
	total, _ := t.displayScroll(rows)
	return total > rows
}

// terminalScrollArea is the terminal's text as a scroll area: the history above
// the screen and the screen itself, one cell height per line. It paints only the
// bar, over the right edge of the grid, in the terminal's own colors since the
// application theme has no say in what the terminal's ground looks like. A nil
// canvas gives the geometry alone. fade scales the bar's opacity, 0 being
// hidden.
func (state *State) terminalScrollArea(canvas *gfx.Canvas, session *Session, area gfx.Rect, fade float32) components.ScrollArea {
	t := session.term
	_, rows := t.screen.Size()
	total, offset := t.displayScroll(rows)
	history := float32(max(total-rows, 0)) * state.cellHeight
	theme := state.Theme()
	theme.Border = withAlpha(state.termPal.termFG, 0.22*fade)
	theme.MutedForeground = withAlpha(state.termPal.termFG, 0.55*fade)
	session.term.pageIdentity(session.ID)
	id := session.term.parts.At(terminalPartScrollbar)
	return components.BeginScrollArea(canvas, theme, area, gfx.Size{Width: area.Width, Height: area.Height + history}).
		Offset(0, float32(offset)*state.cellHeight).
		VerticalScrollbar(id, state.messages.Tools.ScrollbarLabel, "")
}

// terminalScrollGeometry is the bar geometry for callers that only need the
// track. It uses the same scroll area as painting, so folded rows keep their
// visible-row geometry.
func (state *State) terminalScrollGeometry(session *Session, area gfx.Rect) base.ScrollAreaGeometry {
	return state.terminalScrollArea(nil, session, area, 1).Geometry()
}

// terminalBarFade is how opaque the scrollbar is in this frame. The bar is lit
// while the pointer is anywhere over the terminal or its bar and while it is
// dragged, and fades out when the pointer leaves. A view moved by the keyboard
// lights it for terminalBarLinger, since the pointer may be elsewhere. It asks
// for the frame that starts the fade.
func (state *State) terminalBarFade(canvas *gfx.Canvas, session *Session) float32 {
	t := session.term
	session.term.pageIdentity(session.ID)
	id := session.term.parts.At(terminalPartScrollbar)
	now := canvas.Now()
	offset := t.displayOffset
	if t.sync != nil || !t.foldsActive() || !t.hasFolds() {
		offset = t.screen.ViewOffset()
		t.displayOffset = offset
	}
	hovered := canvas.Hovered()
	over := t.barDragging || canvas.Pressed() == id || hovered == id || hovered == session.term.controlID
	if offset != t.barOffset {
		t.barOffset, t.barSince, t.barLit = offset, now, true
	}
	target := float32(0)
	if over {
		target = 1
	} else if t.barLit {
		if remaining := terminalBarLinger - (now - t.barSince); remaining > 0 {
			target = 1
			canvas.RequestFrameIn(remaining)
		} else {
			t.barLit = false
		}
	}
	return canvas.Animate(id, 0, target, gfx.DefaultMotion())
}

func (state *State) paintTerminalScrollbar(canvas *gfx.Canvas, session *Session, geometry terminalGeometry) {
	if !session.term.terminalScrollbarShown() || state.cellHeight <= 0 {
		return
	}
	fade := state.terminalBarFade(canvas, session)
	area := state.terminalScrollArea(canvas, session, geometry.area, fade)
	area.EndContent()
}

// terminalScrollbarPointer drags the thumb and pages when the track beside it is
// pressed. The scrollbar is a part of the terminal control, so the session is
// the control's own.
func (state *State) terminalScrollbarPointer(session *Session, event gfx.PointerEvent) gfx.EventResult {
	if session == nil || session.term == nil || state.cellHeight <= 0 {
		return gfx.EventResult{}
	}
	t := session.term
	if state.modal != modalNone || state.activeSection != sectionTerminal || !t.terminalScrollbarShown() {
		t.barDragging = false
		return gfx.EventResult{}
	}
	layout := state.terminalWindowLayout(session.ID)
	_, rows := t.screen.Size()
	geometry := state.terminalScrollArea(nil, session, layout.area, 1).Geometry()
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}
		}
		thumb := geometry.VerticalThumb
		switch {
		case event.Y < thumb.Y:
			return state.scrollTerminalView(t, layout.rows-1)
		case event.Y >= thumb.Y+thumb.Height:
			return state.scrollTerminalView(t, -(layout.rows - 1))
		}
		t.barDragging = true
		t.barGrab = event.Y - thumb.Y
		return gfx.EventResult{Handled: true, Repaint: true}
	case gfx.PointerMove, gfx.PointerUp:
		if !t.barDragging {
			return gfx.EventResult{}
		}
		offset := geometry.VerticalOffsetForThumb(event.Y - t.barGrab)
		line := int(math.Round(float64(offset / state.cellHeight)))
		total, _ := t.displayScroll(rows)
		target := max(max(total-rows, 0)-line, 0)
		result := state.scrollTerminalView(t, target-t.displayOffset)
		if event.Kind == gfx.PointerUp {
			t.barDragging = false
		}
		return result
	case gfx.PointerExit:
		if !t.barDragging {
			return gfx.EventResult{}
		}
		t.barDragging = false
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{}
}

// scrollTerminalView moves the view by lines, positive going back into history.
func (state *State) scrollTerminalView(t *terminal, lines int) gfx.EventResult {
	if lines == 0 {
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{Handled: true, Repaint: t.scrollDisplay(lines)}
}
