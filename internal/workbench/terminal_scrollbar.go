package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/gfx"
)

// terminalBarLinger is how long the scrollbar stays after the view last moved.
const terminalBarLinger = 1000 * time.Millisecond

// terminalScrollbarShown reports whether the terminal has history to scroll
// through. The alternate screen has none, and a synchronized update is a frozen
// picture of the screen, not a place to move in.
func (t *terminal) terminalScrollbarShown() bool {
	return t.sync == nil && !t.presentedAltScreen() && t.screen.Scrollback() > 0
}

// paintTerminalScrollbar paints the bar over the right edge of the grid, in the
// terminal's own colors since the application theme has no say in what the
// terminal's ground looks like. fade scales the bar's opacity, 0 being hidden.
func (state *State) paintTerminalScrollbar(canvas *gfx.Canvas, session *Session, geometry terminalGeometry) {
	if !session.term.terminalScrollbarShown() || state.cellHeight <= 0 {
		return
	}
	fade := state.terminalBarFade(canvas, session)
	theme := state.Theme()
	theme.Border = withAlpha(state.termPal.termFG, 0.22*fade)
	theme.MutedForeground = withAlpha(state.termPal.termFG, 0.55*fade)
	session.term.pageIdentity(session.ID)
	id := session.term.parts.At(terminalPartScrollbar)
	bar := state.terminalScrollGeometry(session, geometry.area)
	paintScrollbarBar(canvas, theme, bar, true, id, state.messages.Tools.ScrollbarLabel, "", true)
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
	offset := t.screen.ViewOffset()
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

// terminalScrollGeometry is the terminal bar's geometry without painting it.
// The bar is as thick as the full-size scroll area the component draws.
func (state *State) terminalScrollGeometry(session *Session, area gfx.Rect) base.ScrollAreaGeometry {
	screen := session.term.screen
	history := float32(screen.Scrollback()) * state.cellHeight
	return base.LayoutScrollArea(area, gfx.Size{Width: area.Width, Height: area.Height + history},
		base.ScrollAreaLayout{
			OffsetY: float32(screen.Scrollback()-screen.ViewOffset()) * state.cellHeight,
			BarSize: base.ScrollAreaBarSize,
		})
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
	geometry := state.terminalScrollGeometry(session, layout.area)
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
		result := state.scrollTerminalView(t, t.screen.Scrollback()-line-t.screen.ViewOffset())
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
	return gfx.EventResult{Handled: true, Repaint: t.screen.Scroll(lines)}
}
