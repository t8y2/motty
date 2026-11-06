package workbench

import "strings"

// fitChipGroups fills state.hostChips with the groups that get a chip, as
// indexes into hostGroups, in the order they are drawn. Nested groups are not
// all strung along one row: the row is the path to the level in view (each
// ancestor a chip that goes back up to it) followed by that level's groups,
// with only the last segment of a name on its chip. With nothing chosen, or a
// top-level group, that level is the top; choosing a group that has groups of
// its own opens it, and choosing one that has none stays on its siblings.
func (state *State) fitChipGroups() {
	chips := state.hostChips[:0]
	level := ""
	if state.hostFilterOn && state.hostGroupIndex(state.hostFilter) >= 0 {
		level = state.hostFilter
		if !state.hostGroupHasChildren(level) {
			level = hostGroupParent(level)
		}
	}
	for at := 0; level != "" && at < len(level); {
		next := strings.IndexByte(level[at:], '/')
		if next < 0 {
			next = len(level) - at
		}
		at += next
		if index := state.hostGroupIndex(level[:at]); index >= 0 {
			chips = append(chips, index)
		}
		at++
	}
	for index, group := range state.hostGroups {
		if state.hostGroupChipParent(group.Name) == level && group.Name != level {
			chips = append(chips, index)
		}
	}
	state.hostChips = chips
}

// hostGroupChipParent is the group a chip's group sits under. A group whose
// parent does not exist as a group is shown with the top level.
func (state *State) hostGroupChipParent(name string) string {
	parent := hostGroupParent(name)
	if parent != "" && state.hostGroupIndex(parent) < 0 {
		return ""
	}
	return parent
}

func (state *State) hostGroupHasChildren(name string) bool {
	if name == "" {
		return false
	}
	for _, group := range state.hostGroups {
		if group.Name != name && state.hostGroupChipParent(group.Name) == name {
			return true
		}
	}
	return false
}

// hostGroupChipLabel is a chip's text: the last segment of the group's path.
func (state *State) hostGroupChipLabel(group hostGroup) string {
	if group.Name == "" {
		return state.messages.HostGroupDefault
	}
	if at := strings.LastIndexByte(group.Name, '/'); at >= 0 {
		if last := strings.TrimSpace(group.Name[at+1:]); last != "" {
			return last
		}
	}
	return group.Name
}
