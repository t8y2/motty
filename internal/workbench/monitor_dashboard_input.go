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
	result := s.windowPage().Wheel(e)
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
	// Page keys scroll the page from any of its controls, so following a long
	// panel does not first require Tab-ing to the scroll viewport. A text field
	// keeps its own page keys.
	if e.Key == gfx.KeyPageUp || e.Key == gfx.KeyPageDown {
		if s.EditorFor(e.Target) != nil {
			return gfx.EventResult{}
		}
		d := &s.dashboard
		maxOffset := max(d.content-d.body.Height, 0)
		step := max(d.body.Height*0.9, 32)
		if e.Key == gfx.KeyPageUp {
			step = -step
		}
		next := min(max(d.scroll+step, 0), maxOffset)
		if next == d.scroll {
			return gfx.EventResult{Handled: true}
		}
		d.scroll = next
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}
