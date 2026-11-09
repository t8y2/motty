package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var hostFormHits = tree.Feature("host-form")
var hitHostFormViewport = tree.ID(hostFormHits, "viewport")
var hitHostFormScrollbar = tree.ID(hostFormHits, "scrollbar")

type hostFormScroll struct {
	offset, grab      float32
	geometry          components.ScrollAreaGeometry
	dragging, clipped bool
	lastFocus         gfx.ID
	lastError         string
	stops             [64]hostFormStop
	count             int
}

type hostFormStop struct {
	id     gfx.ID
	bounds gfx.Rect
	body   bool
}

func (f *hostFormScroll) addStop(id gfx.ID, bounds gfx.Rect, body bool) {
	if id.IsZero() || f.count == len(f.stops) {
		return
	}
	for i := 0; i < f.count; i++ {
		if f.stops[i].id == id {
			return
		}
	}
	at := f.count
	if body {
		for at > 0 {
			p := f.stops[at-1]
			if p.body && (p.bounds.Y < bounds.Y || p.bounds.Y == bounds.Y && p.bounds.X <= bounds.X) {
				break
			}
			f.stops[at] = p
			at--
		}
	}
	f.stops[at] = hostFormStop{id, bounds, body}
	f.count++
}

// Keep logical tab order even when a control lies outside the clipped viewport.
func (state *State) rememberHostControl(id gfx.ID, bounds gfx.Rect) {
	if state.editor.form.clipped {
		state.editor.form.addStop(id, bounds, true)
	}
}

func (state *State) hostFormPopupOpen() bool {
	e := &state.editor
	return e.colorPicker.open || e.authOpen || e.identityOpen || e.credentialOpen || e.portsOpen || e.iconOpen
}

func (state *State) scrollHostForm(event gfx.ScrollEvent) gfx.EventResult {
	f := &state.editor.form
	if state.hostFormPopupOpen() || !hostFormContains(f.geometry.Viewport, event.X, event.Y) {
		return gfx.EventResult{Handled: true}
	}
	old := f.offset
	step := event.StepY(32)
	f.offset = max(0, min(f.geometry.MaxOffsetY, f.offset+step))
	return gfx.EventResult{Handled: true, Repaint: f.offset != old}
}

func hostFormContains(b gfx.Rect, x, y float32) bool {
	return x >= b.X && y >= b.Y && x < b.X+b.Width && y < b.Y+b.Height
}

func (state *State) hostFormPointer(event gfx.PointerEvent) gfx.EventResult {
	f := &state.editor.form
	if state.modal != modalNewHost || state.hostFormPopupOpen() || event.Target != hitHostFormScrollbar {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		f.dragging = true
		f.grab = event.Y - f.geometry.VerticalThumb.Y
		if !hostFormContains(f.geometry.VerticalThumb, event.X, event.Y) {
			f.grab = f.geometry.VerticalThumb.Height / 2
		}
	case gfx.PointerMove:
		if !f.dragging || !event.Captured {
			return gfx.EventResult{}
		}
	case gfx.PointerUp:
		f.dragging = false
		return gfx.EventResult{Handled: true}
	default:
		return gfx.EventResult{}
	}
	old := f.offset
	f.offset = f.geometry.VerticalOffsetForThumb(event.Y - f.grab)
	return gfx.EventResult{Handled: true, Repaint: old != f.offset}
}

func (state *State) hostFormKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalNewHost || state.hostFormPopupOpen() || event.Release {
		return gfx.EventResult{}
	}
	f := &state.editor.form
	old := f.offset
	if event.Key == gfx.KeyTab && event.Modifiers & ^gfx.ModifierShift == 0 && f.count > 0 {
		at := -1
		for i := 0; i < f.count; i++ {
			if f.stops[i].id == event.Target {
				at = i
				break
			}
		}
		if at < 0 && event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		if event.Modifiers&gfx.ModifierShift != 0 {
			if at < 0 {
				at = 0
			}
			at = (at - 1 + f.count) % f.count
		} else {
			at = (at + 1) % f.count
		}
		next := f.stops[at]
		if next.body {
			f.reveal(next.bounds)
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: next.id, FocusVisible: true}
	}
	if event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyPageDown:
		f.offset += f.geometry.Viewport.Height * .9
	case gfx.KeyPageUp:
		f.offset -= f.geometry.Viewport.Height * .9
	case gfx.KeyHome:
		if event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		f.offset = 0
	case gfx.KeyEnd:
		if event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		f.offset = f.geometry.MaxOffsetY
	default:
		return gfx.EventResult{}
	}
	f.offset = max(0, min(f.geometry.MaxOffsetY, f.offset))
	return gfx.EventResult{Handled: true, Repaint: f.offset != old}
}

func (state *State) revealHostField(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect) {
	f := &state.editor.form
	state.rememberHostControl(id, bounds)
	if !f.clipped || canvas.Focused() != id || f.lastFocus == id {
		return
	}
	f.lastFocus = id
	old := f.offset
	f.reveal(bounds)
	if f.offset != old {
		canvas.RequestFrameIn(time.Nanosecond)
	}
}

func (f *hostFormScroll) reveal(bounds gfx.Rect) {
	// Include the label above the control, not only the caret's line.
	top := bounds.Y - (metrics.FontSizeM + 4) - metrics.GapItem/2
	if top < f.geometry.Viewport.Y {
		f.offset += top - f.geometry.Viewport.Y
	} else if bottom := bounds.Y + bounds.Height; bottom > f.geometry.Viewport.Y+f.geometry.Viewport.Height {
		f.offset += bottom - f.geometry.Viewport.Y - f.geometry.Viewport.Height
	}
	f.offset = max(0, min(f.geometry.MaxOffsetY, f.offset))
}

// Host selectors paint while the body clip is open. Their popups belong to the
// window; release only the clip owned by this form, then restore it afterwards.
func (state *State) beginHostFormPopup(canvas *gfx.Canvas, open bool) bool {
	if open && state.editor.form.clipped {
		canvas.Unclip()
		return true
	}
	return false
}
func (state *State) endHostFormPopup(canvas *gfx.Canvas, portal bool) {
	if portal {
		canvas.Clip(state.editor.form.geometry.Viewport)
	}
}
