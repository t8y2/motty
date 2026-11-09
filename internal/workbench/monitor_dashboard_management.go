package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

var (
	hitDashboardSort         = tree.ID(dashboardHits, "sort")
	hitDashboardPickTab      = tree.ID(dashboardHits, "pick-tab")
	hitDashboardOrderTab     = tree.ID(dashboardHits, "order-tab")
	hitDashboardAddHosts     = tree.ID(dashboardHits, "add-hosts")
	hitDashboardAddNew       = tree.ID(dashboardHits, "add-new")
	hitDashboardAddDismiss   = tree.ID(dashboardHits, "add-dismiss")
	hitDashboardAddPanelList = tree.Sub(dashboardHits, tree.Name("add-panel"))
)

func dashboardOrderID(host string) gfx.ID { return tree.ID(dashboardHits, "order-"+host) }

func (s *State) dashboardModeTabs() [2]components.TabOption {
	m := s.messages.Dashboard
	return [2]components.TabOption{{ID: hitDashboardSimple, Label: m.Simple}, {ID: hitDashboardDetailed, Label: m.Detailed}}
}

func (s *State) dashboardEditorTabs() [2]components.TabOption {
	m := s.messages.Dashboard
	return [2]components.TabOption{{ID: hitDashboardPickTab, Label: m.Hosts, Icon: icons.Server}, {ID: hitDashboardOrderTab, Label: m.OrderTab, Icon: icons.ArrowUpDown}}
}

func (s *State) dashboardCanAddHosts() bool {
	for _, h := range s.hosts {
		if h.ID != localHostID && h.isSSH() && h.CatalogID != "" && s.hostSelected(h.ID) {
			return true
		}
	}
	return false
}

func (s *State) dashboardSelectedHosts() []string {
	var hosts []string
	for _, h := range s.hosts {
		if h.ID != localHostID && h.isSSH() && h.CatalogID != "" && s.hostSelected(h.ID) {
			hosts = append(hosts, h.CatalogID)
		}
	}
	return hosts
}

func (s *State) newDashboardFromHosts(hosts []string) {
	s.openDashboardEditor(false)
	s.dashboard.editor.selected = slices.Clone(hosts)
	s.dashboard.editor.fromHosts = true
	s.dashboard.addHosts = nil
}

func (s *State) clickDashboardManagement(id gfx.ID) bool {
	d := &s.dashboard
	if s.modal == modalDashboardAdd {
		switch {
		case id == hitDashboardAddDismiss:
			s.closeModal()
		case id == hitDashboardAddNew:
			s.newDashboardFromHosts(d.addHosts)
		case id.Scope() == hitDashboardAddPanelList:
			i := item(id)
			if i < len(d.panels) {
				p := &d.panels[i]
				for _, host := range d.addHosts {
					if h, ok := s.dashboardHost(host); ok && h.isSSH() && !slices.Contains(p.Hosts, host) {
						p.Hosts = append(p.Hosts, host)
					}
				}
				d.active = p.ID
				d.search = text.Editor{}
				d.scroll, d.scrollX = 0, 0
				s.activeSection = sectionDashboard
				s.closeModal()
				s.reconcileDashboard()
				s.save()
			}
		}
		return true
	}
	if id == hitDashboardAddHosts && s.modal == modalNone && s.activeSection == sectionHosts {
		hosts := s.dashboardSelectedHosts()
		if len(hosts) > 0 {
			if len(d.panels) == 0 {
				s.newDashboardFromHosts(hosts)
			} else {
				d.addHosts = hosts
				s.modal = modalDashboardAdd
			}
		}
		return true
	}
	return false
}

func (s *State) paintDashboardAddMenu(c *gfx.Canvas) {
	d, m := &s.dashboard, s.messages.Dashboard
	menu := components.BeginDropdownMenu(c, s.Theme(), s.popupBelow(c, d.addAnchor, 260, min(420, float32(len(d.panels)*32+85)), ui.Start),
		components.DropdownMenuOptions{Layer: dialogLayer, Scope: dialogScope, DismissID: hitDashboardAddDismiss, Viewport: s.windowRect(), Scroll: s.menuScrollFor(hitDashboardAddDismiss)})
	menu.AddLabel(fmt.Sprintf(m.AddHint, len(d.addHosts)), false)
	menu.AddItem(components.DropdownMenuItem{ID: hitDashboardAddNew, Label: m.New, Icon: icons.Plus})
	menu.AddSeparator()
	for i, p := range d.panels {
		menu.AddItem(components.DropdownMenuItem{ID: hitDashboardAddPanelList.At(i), Label: p.Name, Icon: icons.Activity})
	}
	s.menuPlaced(menu.Bounds())
	menu.End()
}

func (s *State) openDashboardOrder() {
	s.openDashboardEditor(true)
	s.dashboard.editor.page = 1
}

func (s *State) moveDashboardHost(move components.ReorderMove) {
	e := &s.dashboard.editor
	if !move.Valid() || move.From >= len(e.selected) || move.To >= len(e.selected) || dashboardOrderID(e.selected[move.From]) != move.ID {
		return
	}
	host := e.selected[move.From]
	if move.From < move.To {
		copy(e.selected[move.From:move.To], e.selected[move.From+1:move.To+1])
	} else {
		copy(e.selected[move.To+1:move.From+1], e.selected[move.To:move.From])
	}
	e.selected[move.To] = host
	e.scroll = max(0, e.scroll+e.order.RevealScroll(move.To))
}

func (s *State) paintDashboardHostOrder(c *gfx.Canvas, geo components.ScrollAreaGeometry) {
	e, m, theme := &s.dashboard.editor, s.messages.Dashboard, s.Theme()
	bounds := geo.Content
	bounds.X += 3
	bounds.Y += 3
	bounds.Width = max(0, bounds.Width-6)
	if geo.MaxOffsetY > 0 {
		bounds.Width = max(0, bounds.Width-14)
	}
	list := components.BeginReorderList(c, theme, bounds, &e.order).Viewport(geo.Viewport)
	for _, id := range e.selected {
		h, exists := s.dashboardHost(id)
		if !exists {
			h.Label, h.Address = m.Missing, id
		}
		row := list.Row(dashboardOrderID(id), fmt.Sprintf(m.SortPanel, h.Label), false)
		s.paintHostCatalogIcon(c, h, gfx.Rect{X: row.Content.X, Y: row.Content.Y + (row.Content.Height-28)/2, Width: 28, Height: 28}, theme.Card)
		color := theme.Foreground
		if !exists {
			color = s.pal.warning
		}
		monitorLabel(c, h.Label, gfx.Rect{X: row.Content.X + 40, Y: row.Content.Y, Width: max(0, row.Content.Width*.55-40), Height: row.Content.Height}, 13, color, gfx.WeightMedium)
		monitorRight(c, h.Address, gfx.Rect{X: row.Content.X + row.Content.Width*.55, Y: row.Content.Y, Width: row.Content.Width*.45 - 12, Height: row.Content.Height}, 12, theme.MutedForeground, gfx.WeightRegular)
	}
	list.End()
	if len(e.selected) == 0 {
		components.Empty(c, theme, geo.Viewport, components.EmptyData{Title: m.HostsRequired})
	}
}
