package workbench

import (
	"math"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// hitTerminalBar is the scrollbar of a terminal that has scrollback. It is only
// drawn while the view moves or the pointer is over it, but its track always
// takes hits, so moving to the right edge brings it back.
var hitTerminalBar = tree.Sub(shellHits, tree.Name("terminal-bar"))

// terminalBarLinger is how long the scrollbar stays after the view last moved.
const terminalBarLinger = 1000 * time.Millisecond

// scrollbarSession finds the session whose terminal owns a scrollbar hit.
func (state *State) scrollbarSession(hit gfx.ID) *Session {
	if hit.Scope() != hitTerminalBar {
		return nil
	}
	for index := range state.sessions {
		if state.sessions[index].ID == uint32(item(hit)) {
			return &state.sessions[index]
		}
	}
	return nil
}

// terminalOrBarSession finds the session whose terminal, or whose terminal's
// scrollbar, owns a hit: the places a pointer or wheel over the terminal lands.
func (state *State) terminalOrBarSession(hit gfx.ID) *Session {
	if session := state.terminalSession(hit); session != nil {
		return session
	}
	return state.scrollbarSession(hit)
}

// terminalScrollbarShown reports whether the terminal has history to scroll
// through. The alternate screen has none, and a synchronized update is a frozen
// picture of the screen, not a place to move in.
func (t *terminal) terminalScrollbarShown() bool {
	return t.sync == nil && !t.presentedAltScreen() && t.screen.Scrollback() > 0
}

// terminalScrollArea is the terminal's text as a scroll area: the history above
// the screen and the screen itself, one cell height per line. It paints only the
// bar, over the right edge of the grid, in the terminal's own colors since the
// application theme has no say in what the terminal's ground looks like. A nil
// canvas gives the geometry alone. fade scales the bar's opacity, 0 being
// hidden.
func (state *State) terminalScrollArea(canvas *gfx.Canvas, session *Session, area gfx.Rect, fade float32) components.ScrollArea {
	screen := session.term.screen
	history := float32(screen.Scrollback()) * state.cellHeight
	theme := state.Theme()
	theme.Border = withAlpha(state.termPal.termFG, 0.22*fade)
	theme.MutedForeground = withAlpha(state.termPal.termFG, 0.55*fade)
	id := hitTerminalBar.At(int(session.ID))
	return components.BeginScrollArea(canvas, theme, area, gfx.Size{Width: area.Width, Height: area.Height + history}).
		Offset(0, float32(screen.Scrollback()-screen.ViewOffset())*state.cellHeight).
		VerticalScrollbar(id, state.messages.Tools.ScrollbarLabel, "")
}

// terminalBarFade is how opaque the scrollbar is in this frame. The bar is lit
// while the pointer is anywhere over the terminal or its bar and while it is
// dragged, and fades out when the pointer leaves. A view moved by the keyboard
// lights it for terminalBarLinger, since the pointer may be elsewhere. It asks
// for the frame that starts the fade.
func (state *State) terminalBarFade(canvas *gfx.Canvas, session *Session) float32 {
	t := session.term
	id := hitTerminalBar.At(int(session.ID))
	now := canvas.Now()
	offset := t.screen.ViewOffset()
	hovered := canvas.Hovered()
	over := t.barDragging || canvas.Pressed() == id || hovered == id || hovered == hitTerminalList.At(int(session.ID))
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
// pressed.
func (state *State) terminalScrollbarPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	session := state.scrollbarSession(event.Target)
	if session == nil || session.term == nil || state.cellHeight <= 0 {
		return gfx.EventResult{}, false
	}
	t := session.term
	if state.modal != modalNone || state.activeSection != sectionTerminal || !t.terminalScrollbarShown() {
		t.barDragging = false
		return gfx.EventResult{}, false
	}
	layout := state.terminalLayoutForEvent(session.ID, event.Viewport)
	geometry := state.terminalScrollArea(nil, session, layout.area, 1).Geometry()
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}, false
		}
		thumb := geometry.VerticalThumb
		switch {
		case event.Y < thumb.Y:
			return state.scrollTerminalView(t, layout.rows-1), true
		case event.Y >= thumb.Y+thumb.Height:
			return state.scrollTerminalView(t, -(layout.rows - 1)), true
		}
		t.barDragging = true
		t.barGrab = event.Y - thumb.Y
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.PointerMove, gfx.PointerUp:
		if !t.barDragging {
			return gfx.EventResult{}, false
		}
		offset := geometry.VerticalOffsetForThumb(event.Y - t.barGrab)
		line := int(math.Round(float64(offset / state.cellHeight)))
		result := state.scrollTerminalView(t, t.screen.Scrollback()-line-t.screen.ViewOffset())
		if event.Kind == gfx.PointerUp {
			t.barDragging = false
		}
		return result, true
	case gfx.PointerExit:
		if !t.barDragging {
			return gfx.EventResult{}, false
		}
		t.barDragging = false
		return gfx.EventResult{Handled: true}, true
	}
	return gfx.EventResult{}, false
}

// scrollTerminalView moves the view by lines, positive going back into history.
func (state *State) scrollTerminalView(t *terminal, lines int) gfx.EventResult {
	if lines == 0 {
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{Handled: true, Repaint: t.screen.Scroll(lines)}
}
