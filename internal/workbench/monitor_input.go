package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func (state *State) monitorScrollArea(p *monitorView) components.ScrollArea {
	b := state.monitorRect
	b.Y += infoBarHeight + monitorControlsHeight
	b.Height = max(0, b.Height-infoBarHeight-monitorControlsHeight-24)
	return components.BeginScrollArea(nil, state.Theme(), b, ui.Size{Width: b.Width, Height: p.contentHeight}).Offset(0, p.scroll)
}

func (state *State) monitorPointer(e ui.PointerEvent) (ui.EventResult, bool) {
	if state.monitorResizing && (state.monitorResizeWorkspace != state.deck.ActiveWorkspace() || state.monitorResizeSession != state.activeSessionID) {
		state.monitorResizing = false
	}
	if state.modal != modalNone || state.activeSection != sectionTerminal || !state.monitorShown(state.deck.ActiveWorkspace()) {
		return ui.EventResult{}, false
	}
	if e.Kind == ui.PointerDown && e.Target == hitMonitorDivider && e.Button == ui.PointerButtonPrimary {
		state.monitorResizing = true
		state.monitorResizeWorkspace = state.deck.ActiveWorkspace()
		state.monitorResizeSession = state.activeSessionID
		state.monitorResizeGrab = e.X - state.monitorDividerX()
		return ui.EventResult{Handled: true, Focus: hitMonitorDivider}, true
	}
	if state.monitorResizing {
		switch e.Kind {
		case ui.PointerMove, ui.PointerUp:
			width := state.monitorRect.X + state.monitorRect.Width - 1 - e.X + state.monitorResizeGrab
			if c, _ := state.deck.Context(state.deck.ActiveWorkspace()); c.MonitorLeft {
				width = e.X - state.monitorResizeGrab - state.monitorRect.X
			}
			state.resizeMonitor(width)
			if e.Kind == ui.PointerUp {
				state.monitorResizing = false
				state.save()
			}
			return ui.EventResult{Handled: true, Repaint: true}, true
		case ui.PointerExit:
			state.monitorResizing = false
			state.save()
			return ui.EventResult{Handled: true}, true
		}
		return ui.EventResult{Handled: true}, true
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return ui.EventResult{}, false
	}
	p := &s.stats.panel
	if e.Kind == ui.PointerDown && e.Target == hitMonitorThumb {
		if e.Button != ui.PointerButtonPrimary {
			return ui.EventResult{}, false
		}
		p.dragging = true
		p.grab = e.Y - state.monitorScrollArea(p).Geometry().VerticalThumb.Y
		return ui.EventResult{Handled: true, Focus: hitMonitorScroll}, true
	}
	if !p.dragging {
		return ui.EventResult{}, false
	}
	if e.Kind == ui.PointerExit {
		p.dragging = false
		return ui.EventResult{Handled: true}, true
	}
	if e.Kind == ui.PointerMove || e.Kind == ui.PointerUp {
		g := state.monitorScrollArea(p).Geometry()
		p.scroll = g.VerticalOffsetForThumb(e.Y - p.grab)
		if e.Kind == ui.PointerUp {
			p.dragging = false
		}
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{Handled: true}, true
}

func (state *State) monitorKey(e ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalNone || state.activeSection != sectionTerminal || e.Release {
		return ui.EventResult{}, false
	}
	if !state.monitorShown(state.deck.ActiveWorkspace()) {
		return ui.EventResult{}, false
	}
	if state.monitorMenu && state.monitorMenuSession != state.activeSessionID {
		state.monitorMenu = false
		return ui.EventResult{Handled: true, Repaint: true}, true
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
		case ui.KeyLeft:
			width += step
		case ui.KeyRight:
			width -= step
		case ui.KeyHome:
			width = layoutcore.MinMonitorWidth
		case ui.KeyEnd:
			width = layoutcore.MaxMonitorWidth
		default:
			return ui.EventResult{}, false
		}
		state.resizeMonitor(width)
		state.save()
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if state.monitorMenu {
		if e.Key == ui.KeyTab {
			state.monitorMenu = false
			return ui.EventResult{Handled: true, Repaint: true, ContinueTraversal: true}, true
		}
		if id, ok := state.selectScroll(true, hitMonitorMenuDismiss).Navigate(e.Key, e.Target, true); ok {
			return ui.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
		}
	} else if e.Target == hitMonitorInterval && e.Key == ui.KeyDown {
		state.monitorMenu = true
		state.monitorMenuSession = state.activeSessionID
		return ui.EventResult{Handled: true, Repaint: true, Focus: hitMonitorEvery1, FocusVisible: true}, true
	}
	if e.Target == hitMonitorOverview || e.Target == hitMonitorDiskPage || e.Target == hitMonitorNetworkPage {
		opts := state.monitorPageOptions()
		next, ok := components.TabsKeyDown(e.Target, opts[:], components.TabsHorizontal, components.DirectionLTR, e.Key)
		if ok {
			return ui.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
		}
	}
	if state.monitorMenu && e.Key == ui.KeyEscape {
		state.monitorMenu = false
		return ui.EventResult{Handled: true, Repaint: true, Focus: hitMonitorInterval}, true
	}
	if e.Target != hitMonitorScroll {
		return ui.EventResult{}, false
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return ui.EventResult{}, false
	}
	p := &s.stats.panel
	_, y, ok := state.monitorScrollArea(p).ScrollForKey(0, p.scroll, e.Key, 28)
	if !ok {
		return ui.EventResult{}, false
	}
	p.scroll = y
	return ui.EventResult{Handled: true, Repaint: true}, true
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
	if state.deck.SetContext(id, c) != nil {
		return
	}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
}
