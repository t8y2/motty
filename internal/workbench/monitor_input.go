package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func (state *State) monitorScrollArea(p *monitorView) components.ScrollArea {
	b := state.monitorRect
	b.Y += infoBarHeight + monitorControlsHeight
	b.Height = max(0, b.Height-infoBarHeight-monitorControlsHeight-24)
	return components.BeginScrollArea(nil, state.Theme(), b, gfx.Size{Width: b.Width, Height: p.contentHeight}).Offset(0, p.scroll)
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
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return gfx.EventResult{}, false
	}
	p := &s.stats.panel
	if e.Kind == gfx.PointerDown && e.Target == hitMonitorThumb {
		if e.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}, false
		}
		p.dragging = true
		p.grab = e.Y - state.monitorScrollArea(p).Geometry().VerticalThumb.Y
		return gfx.EventResult{Handled: true, Focus: hitMonitorScroll}, true
	}
	if !p.dragging {
		return gfx.EventResult{}, false
	}
	if e.Kind == gfx.PointerExit {
		p.dragging = false
		return gfx.EventResult{Handled: true}, true
	}
	if e.Kind == gfx.PointerMove || e.Kind == gfx.PointerUp {
		g := state.monitorScrollArea(p).Geometry()
		p.scroll = g.VerticalOffsetForThumb(e.Y - p.grab)
		if e.Kind == gfx.PointerUp {
			p.dragging = false
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{Handled: true}, true
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
	if state.monitorMenu {
		if e.Key == gfx.KeyTab {
			state.monitorMenu = false
			return gfx.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}, true
		}
		if id, ok := state.selectScroll(true, hitMonitorMenuDismiss).Navigate(e.Key, e.Target, true); ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
		}
	} else if e.Target == hitMonitorInterval && e.Key == gfx.KeyDown {
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
	_, y, ok := state.monitorScrollArea(p).ScrollForKey(0, p.scroll, e.Key, 28)
	if !ok {
		return gfx.EventResult{}, false
	}
	p.scroll = y
	return gfx.EventResult{Handled: true, Repaint: true}, true
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
