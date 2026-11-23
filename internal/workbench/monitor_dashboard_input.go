package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/monitoring"
)

func (s *State) restoreDashboard(panels []monitoring.Panel, active string) {
	// A live local-state refresh must not reconnect or discard running panels.
	if len(s.dashboard.sources) > 0 {
		return
	}
	d := &s.dashboard
	for _, running := range d.running {
		if running {
			return
		}
	}
	previous := d.panels
	d.panels = nil
	d.running = nil
	d.active = ""
	d.rows = nil
	seen := make(map[string]bool)
	for _, p := range panels {
		if p.ID == "" || seen[p.ID] || strings.TrimSpace(p.Name) == "" {
			continue
		}
		p.Normalize()
		seen[p.ID] = true
		d.panels = append(d.panels, p)
		d.engine.Set(p, false)
		if p.ID == active {
			d.active = p.ID
		}
	}
	for _, old := range previous {
		if !seen[old.ID] {
			d.engine.Remove(old.ID)
		}
	}
	if d.active == "" && len(d.panels) > 0 {
		d.active = d.panels[0].ID
	}
}

// dashboardScroll gives the wheel to the page's own scroll areas: the host list
// in the dashboard, the picker and the order list in its dialog.
func (s *State) dashboardScroll(e gfx.ScrollEvent) gfx.EventResult {
	if s.activeSection != sectionDashboard {
		return gfx.EventResult{}
	}
	if s.modal != modalNone && s.modal != modalDashboard && s.modal != modalDashboardAdd {
		return gfx.EventResult{}
	}
	result := s.windowPage().RouteWheel(e)
	if s.modal != modalNone {
		result.Handled = true
	}
	return result
}

func (s *State) dashboardKey(e gfx.KeyEvent) gfx.EventResult {
	if s.modal == modalDashboardAdd {
		return gfx.EventResult{}
	}
	if e.Release {
		return gfx.EventResult{}
	}
	if s.modal == modalDashboard {
		if e.Key == gfx.KeyEnter && !e.Repeat && (e.Target == hitDashboardName || e.Target == hitDashboardNote) {
			s.submitDashboard()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if s.modal != modalNone || s.activeSection != sectionDashboard {
		return gfx.EventResult{}
	}
	if e.Target.Scope() == hitDashboardViewList || e.Target.Scope() == hitDashboardSimpleViewList {
		simple := e.Target.Scope() == hitDashboardSimpleViewList
		host := uint32(item(e.Target) / 3)
		options := s.dashboardViewOptions(host, simple)
		orientation := components.TabsHorizontal
		if simple {
			orientation = components.TabsVertical
		}
		if next, ok := components.TabsKeyDown(e.Target, options[:], orientation, components.DirectionLTR, e.Key); ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}
		}
	}
	// The viewport itself moves by the arrow keys, Home and End, as the
	// hand-painted scroll area did; page keys scroll it from any of the page's
	// controls, so following a long panel does not first require Tab-ing to the
	// viewport. A text field keeps its own page keys.
	d := &s.dashboard
	maxOffset := max(d.content-d.body.Height, 0)
	next, move := d.scroll, false
	if e.Target == hitDashboardScroll {
		switch e.Key {
		case gfx.KeyDown:
			next, move = d.scroll+32, true
		case gfx.KeyUp:
			next, move = d.scroll-32, true
		case gfx.KeyHome:
			next, move = 0, true
		case gfx.KeyEnd:
			next, move = maxOffset, true
		case gfx.KeyPageDown:
			next, move = d.scroll+d.body.Height, true
		case gfx.KeyPageUp:
			next, move = d.scroll-d.body.Height, true
		}
	} else if e.Key == gfx.KeyPageDown && s.EditorFor(e.Target) == nil {
		next, move = d.scroll+d.body.Height, true
	} else if e.Key == gfx.KeyPageUp && s.EditorFor(e.Target) == nil {
		next, move = d.scroll-d.body.Height, true
	}
	if !move {
		return gfx.EventResult{}
	}
	next = min(max(next, 0), maxOffset)
	if next == d.scroll {
		return gfx.EventResult{Handled: true}
	}
	d.scroll = next
	return gfx.EventResult{Handled: true, Repaint: true}
}
