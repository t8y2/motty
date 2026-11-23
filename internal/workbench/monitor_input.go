package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

// monitorScroll is the scroll region of a hand-painted monitor panel: its
// viewport and the height of what is inside it.
type monitorScroll struct {
	body    gfx.Rect
	content float32
}

// monitorScrollGeometry is the region's geometry: where it shows and how far it
// can move.
type monitorScrollGeometry struct {
	Viewport        gfx.Rect
	MaxOffsetY      float32
	MaxOffsetX      float32
	HorizontalTrack gfx.Rect
	HorizontalThumb gfx.Rect
}

func (state *State) monitorScrollArea(p *monitorView) monitorScroll {
	b := state.monitorRect
	b.Y += infoBarHeight + monitorControlsHeight
	b.Height = max(0, b.Height-infoBarHeight-monitorControlsHeight-24)
	return monitorScroll{body: b, content: p.contentHeight}
}

func (scroll monitorScroll) Geometry() monitorScrollGeometry {
	return monitorScrollGeometry{Viewport: scroll.body, MaxOffsetY: max(scroll.content-scroll.body.Height, 0)}
}

// ScrollForKey moves offset as the key asks, reporting the offset and whether
// the key did anything.
func (scroll monitorScroll) ScrollForKey(offset float32, key gfx.Key, step float32) (float32, bool) {
	maxOffset := max(scroll.content-scroll.body.Height, 0)
	next := offset
	switch key {
	case gfx.KeyDown:
		next += step
	case gfx.KeyUp:
		next -= step
	case gfx.KeyPageDown:
		next += scroll.body.Height
	case gfx.KeyPageUp:
		next -= scroll.body.Height
	case gfx.KeyHome:
		next = 0
	case gfx.KeyEnd:
		next = maxOffset
	default:
		return offset, false
	}
	next = min(max(next, 0), maxOffset)
	return next, next != offset
}

// scrollMonitor moves the docked panel's body by the wheel.
func (state *State) scrollMonitor(e gfx.ScrollEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionTerminal || !state.monitorShown(state.deck.ActiveWorkspace()) || !pointInRect(e.X, e.Y, state.monitorRect) {
		return gfx.EventResult{}, false
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return gfx.EventResult{Handled: true}, true
	}
	p := &s.stats.panel
	next := min(max(p.scroll+e.DeltaY, 0), state.monitorScrollArea(p).Geometry().MaxOffsetY)
	changed := next != p.scroll
	p.scroll = next
	return gfx.EventResult{Handled: true, Repaint: changed}, true
}

func (state *State) monitorPointer(e gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.monitorResizing && (state.monitorResizeWorkspace != state.deck.ActiveWorkspace() || state.monitorResizeSession != state.activeSessionID) {
		state.monitorResizing = false
	}
	if state.modal != modalNone || state.activeSection != sectionTerminal || !state.monitorShown(state.deck.ActiveWorkspace()) {
		return gfx.EventResult{}, false
	}
	if e.Kind == gfx.PointerDown && e.Target == hitMonitorDivider && e.Button == gfx.PointerButtonPrimary {
		state.monitorResizing = true
		state.monitorResizeWorkspace = state.deck.ActiveWorkspace()
		state.monitorResizeSession = state.activeSessionID
		state.monitorResizeGrab = e.X - state.monitorDividerX()
		return gfx.EventResult{Handled: true, Focus: hitMonitorDivider}, true
	}
	if state.monitorResizing {
		switch e.Kind {
		case gfx.PointerMove, gfx.PointerUp:
			width := state.monitorRect.X + state.monitorRect.Width - 1 - e.X + state.monitorResizeGrab
			if c, _ := state.deck.Context(state.deck.ActiveWorkspace()); c.MonitorLeft {
				width = e.X - state.monitorResizeGrab - state.monitorRect.X
			}
			state.resizeMonitor(width)
			if e.Kind == gfx.PointerUp {
				state.monitorResizing = false
				state.save()
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.PointerExit:
			state.monitorResizing = false
			state.save()
			return gfx.EventResult{Handled: true}, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) monitorKey(e gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionTerminal || e.Release {
		return gfx.EventResult{}, false
	}
	if !state.monitorShown(state.deck.ActiveWorkspace()) {
		return gfx.EventResult{}, false
	}
	if state.monitorMenu && state.monitorMenuSession != state.activeSessionID {
		state.monitorMenu = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if e.Target == hitMonitorDivider && !state.monitorDrawer(state.viewport) {
		context, _ := state.deck.Context(state.deck.ActiveWorkspace())
		width := context.MonitorWidth
		if width == 0 {
			width = monitorWidth
		}
		step := float32(16)
		if context.MonitorLeft {
			step = -step
		}
		switch e.Key {
		case gfx.KeyLeft:
			width += step
		case gfx.KeyRight:
			width -= step
		case gfx.KeyHome:
			width = layoutcore.MinMonitorWidth
		case gfx.KeyEnd:
			width = layoutcore.MaxMonitorWidth
		default:
			return gfx.EventResult{}, false
		}
		state.resizeMonitor(width)
		state.save()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if !state.monitorMenu && e.Target == hitMonitorInterval && e.Key == gfx.KeyDown {
		state.monitorMenu = true
		state.monitorMenuSession = state.activeSessionID
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitMonitorEvery1, FocusVisible: true}, true
	}
	if e.Target == hitMonitorOverview || e.Target == hitMonitorDiskPage || e.Target == hitMonitorNetworkPage {
		opts := state.monitorPageOptions()
		next, ok := components.TabsKeyDown(e.Target, opts[:], components.TabsHorizontal, components.DirectionLTR, e.Key)
		if ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
		}
	}
	if state.monitorMenu && e.Key == gfx.KeyEscape {
		state.monitorMenu = false
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitMonitorInterval}, true
	}
	if e.Target != hitMonitorScroll {
		return gfx.EventResult{}, false
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return gfx.EventResult{}, false
	}
	p := &s.stats.panel
	if next, ok := state.monitorScrollArea(p).ScrollForKey(p.scroll, e.Key, 28); ok {
		p.scroll = next
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) monitorDividerX() float32 {
	if c, _ := state.deck.Context(state.deck.ActiveWorkspace()); c.MonitorLeft {
		return state.monitorRect.X + state.monitorRect.Width
	}
	return state.monitorRect.X - 1
}

func (state *State) resizeMonitor(width float32) {
	id := state.deck.ActiveWorkspace()
	c, _ := state.deck.Context(id)
	maximum := layoutcore.MaxMonitorWidth
	files := float32(0)
	if c.Open {
		files = state.contextPanelExtent(state.baseWorkspacePixelBounds(state.viewport), c.Width) + 1
	}
	maximum = min(maximum, max(layoutcore.MinMonitorWidth, state.baseWorkspacePixelBounds(state.viewport).Width-files-minPaneWidth-1))
	c.MonitorWidth = min(max(width, layoutcore.MinMonitorWidth), maximum)
	state.monitorWidth = c.MonitorWidth
	if state.deck.SetContext(id, c) != nil {
		return
	}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
}
