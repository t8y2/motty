package workbench

import (
	"slices"
)

var (
	hitDashboardSelectMatches = tree.ID(dashboardHits, "select-matches")
	hitDashboardOrderModes    = tree.Sub(dashboardHits, tree.Name("order-mode"))
)

func (state *State) toggleDashboardMatches() {
	groups, _, _ := state.dashboardPickerGroups()
	matching := make(map[string]bool)
	var missing []string
	for _, group := range groups {
		for _, host := range group.hosts {
			if matching[host.CatalogID] {
				continue
			}
			matching[host.CatalogID] = true
			if !slices.Contains(state.dashboard.editor.selected, host.CatalogID) {
				missing = append(missing, host.CatalogID)
			}
		}
	}
	if len(matching) == 0 {
		return
	}
	state.dashboard.editor.orderMode = hostOrderManual
	if len(missing) > 0 {
		state.dashboard.editor.selected = append(state.dashboard.editor.selected, missing...)
		return
	}
	state.dashboard.editor.selected = slices.DeleteFunc(state.dashboard.editor.selected, func(id string) bool { return matching[id] })
}

func (state *State) sortDashboardHosts(mode hostOrderMode) {
	editor := &state.dashboard.editor
	editor.orderMode = mode
	editor.scroll = 0
	if mode == hostOrderManual {
		return
	}
	hosts := make(map[string]Host, len(state.hosts))
	for _, host := range state.hosts {
		if dashboardHostEligible(host) {
			hosts[host.CatalogID] = host
		}
	}
	collator := state.hostNameCollator()
	slices.SortStableFunc(editor.selected, func(leftID, rightID string) int {
		left, leftFound := hosts[leftID]
		right, rightFound := hosts[rightID]
		if !leftFound && !rightFound {
			return 0
		}
		if !leftFound {
			return 1
		}
		if !rightFound {
			return -1
		}
		if mode == hostOrderIP {
			return compareHostAddress(collator, left, right)
		}
		return compareHostName(collator, left, right)
	})
}
