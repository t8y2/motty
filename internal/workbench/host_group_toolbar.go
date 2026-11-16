package workbench

var (
	hostGroupToolbarHits     = tree.Feature("host-group-toolbar")
	hitHostGroupsExpandAll   = tree.ID(hostGroupToolbarHits, "expand-all")
	hitHostGroupsCollapseAll = tree.ID(hostGroupToolbarHits, "collapse-all")
)

func (state *State) hostGroupsExpansionActions() (expand, collapse bool) {
	if state.hostFilterOn || state.hostsFlat() {
		return false, false
	}
	for _, group := range state.hostGroups {
		expand = expand || !group.Expanded
		collapse = collapse || group.Expanded
	}
	return expand, collapse
}

func (state *State) setHostGroupsExpanded(expanded bool) {
	if state.hostFilterOn || state.hostsFlat() {
		return
	}
	changed := false
	for index := range state.hostGroups {
		if state.hostGroups[index].Expanded != expanded {
			state.hostGroups[index].Expanded = expanded
			changed = true
		}
	}
	if !changed {
		return
	}
	state.hostsScroll = 0
	state.rebuildHostTreeRows()
	state.save()
}
