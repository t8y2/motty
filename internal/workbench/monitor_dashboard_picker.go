package workbench

import (
	"slices"
	"strings"
)

var (
	hitDashboardSelectedOnly = tree.ID(dashboardHits, "selected-only")
	hitDashboardGroupList    = tree.Sub(dashboardHits, tree.Name("group-option"))
)

type dashboardPickerGroup struct {
	name    string
	total   int
	hosts   []Host
	missing []int
}

// Filtering changes only the displayed catalog; the selection and its order
// remain shared with the order tab, including hosts outside the active filter.
func (s *State) dashboardPickerGroups() ([]dashboardPickerGroup, int, int) {
	e := &s.dashboard.editor
	query := strings.ToLower(strings.TrimSpace(e.query.String()))
	byName := make(map[string]*dashboardPickerGroup)
	total, shown := 0, 0
	selected := make(map[string]bool, len(e.selected))
	known := make(map[string]bool, len(s.hosts))
	for _, id := range e.selected {
		selected[id] = true
	}
	for _, h := range s.hosts {
		known[h.CatalogID] = dashboardHostEligible(h)
		if !dashboardHostEligible(h) {
			continue
		}
		name := strings.TrimSpace(h.Group)
		group := byName[name]
		if group == nil {
			group = &dashboardPickerGroup{name: name}
			byName[name] = group
		}
		group.total++
		total++
		if e.groupFilter && name != e.group {
			continue
		}
		if e.selectedOnly && !selected[h.CatalogID] {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(h.Label+" "+h.Address+" "+h.User+" "+h.Group+" "+h.Tags), query) {
			continue
		}
		group.hosts = append(group.hosts, h)
		shown++
	}
	e.groups = e.groups[:0]
	for name := range byName {
		e.groups = append(e.groups, name)
	}
	slices.Sort(e.groups)
	groups := make([]dashboardPickerGroup, 0, len(e.groups)+1)
	for _, name := range e.groups {
		if g := byName[name]; len(g.hosts) > 0 {
			groups = append(groups, *g)
		}
	}
	missing := dashboardPickerGroup{name: s.messages.Dashboard.UnavailableHosts}
	for i, id := range e.selected {
		if known[id] {
			continue
		}
		total++
		missing.total++
		searchText := s.messages.Dashboard.Missing + " " + s.messages.Dashboard.UnavailableHosts + " " + id
		if host, exists := s.dashboardHost(id); exists {
			searchText += " " + host.Label + " " + host.Address + " " + host.User + " " + host.Group + " " + host.Tags
		}
		if !e.groupFilter && (query == "" || strings.Contains(strings.ToLower(searchText), query)) {
			missing.missing = append(missing.missing, i)
			shown++
		}
	}
	if len(missing.missing) > 0 {
		groups = append(groups, missing)
	}
	return groups, total, shown
}

func dashboardPickerColumns(width float32) int {
	return max(1, min(3, int((width+8)/218)))
}

func dashboardPickerHeight(groups []dashboardPickerGroup, columns int) float32 {
	var height float32
	for _, g := range groups {
		rows := (len(g.hosts) + len(g.missing) + columns - 1) / columns
		height += 28 + float32(rows)*44
	}
	return max(0, height-8)
}

func (s *State) dashboardGroupLabel(name string) string {
	if name == "" {
		return s.messages.Dashboard.Ungrouped
	}
	return name
}
