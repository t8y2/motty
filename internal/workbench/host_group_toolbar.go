package workbench

var (
	hostGroupToolbarHits     = tree.Feature("host-group-toolbar")
	hitHostGroupsExpandAll   = tree.ID(hostGroupToolbarHits, "expand-all")
	hitHostGroupsCollapseAll = tree.ID(hostGroupToolbarHits, "collapse-all")
)

func (state *State) hostGroupsExpansionActions() (expand, collapse bool) {
	// The group headers of the list and grid are the only place the controls
	// act in those views, so a flat list or a leaf filter offers nothing. The
	// tree view always has its groups to open and close.
	if !state.hostSide.visible && (state.hostFilterOn || state.hostsFlat()) {
		return false, false
	}
	for _, group := range state.hostGroups {
		expand = expand || !group.Expanded
		collapse = collapse || group.Expanded
	}
	return expand, collapse
}

// hostGroupControlsOffered says whether expand-all and collapse-all are on the
// page: beside the tree always, and over the list only while group headers show.
func (state *State) hostGroupControlsOffered() bool {
	if state.hostSide.visible {
		return true
	}
	return state.hasHostFilter() && !state.hostFilterOn
}

func (state *State) setHostGroupsExpanded(expanded bool) {
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
