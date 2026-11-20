package workbench

import (
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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

func (s *State) dashboardCanAddHosts() bool {
	for _, h := range s.hosts {
		if h.ID != localHostID && h.isSSH() && !h.mosh() && h.CatalogID != "" && s.hostSelected(h.ID) {
			return true
		}
	}
	return false
}

func (s *State) dashboardSelectedHosts() []string {
	var hosts []string
	for _, h := range s.hosts {
		if h.ID != localHostID && h.isSSH() && !h.mosh() && h.CatalogID != "" && s.hostSelected(h.ID) {
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
					if h, ok := s.dashboardHost(host); ok && h.isSSH() && !h.mosh() && !slices.Contains(p.Hosts, host) {
						p.Hosts = append(p.Hosts, host)
					}
				}
				d.active = p.ID
				d.search = text.Editor{}
				d.scroll = 0
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

func (s *State) openDashboardOrder() {
	s.openDashboardEditor(true)
	s.dashboard.editor.page = 1
}

// moveDashboardHost moves the rows ListState.Reorder named before the row at
// to. The editor's selection is the draft order; saving keeps it.
func (s *State) moveDashboardHost(rows []int, to int) {
	e := &s.dashboard.editor
	if len(rows) == 0 || rows[0] < 0 || rows[0] >= len(e.selected) {
		return
	}
	from := rows[0]
	if to > from {
		to--
	}
	to = min(max(to, 0), len(e.selected)-1)
	if from == to {
		return
	}
	host := e.selected[from]
	e.selected = slices.Delete(e.selected, from, from+1)
	e.selected = slices.Insert(e.selected, to, host)
	e.orderMode = hostOrderManual
}
