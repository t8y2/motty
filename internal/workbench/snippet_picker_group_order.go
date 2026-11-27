package workbench

import (
	"slices"
	"strings"
)

// snippetPickerGroups 保持目录前序结构，只调整同级分组的显示顺序。
func (state *State) snippetPickerGroups() []string {
	groups := slices.Clone(state.snippets.groups)
	ranks := make(map[string]int, len(state.snippets.pickerGroupOrder))
	for i, group := range state.snippets.pickerGroupOrder {
		ranks[strings.ToLower(group)] = i + 1
	}
	slices.SortStableFunc(groups, func(a, b string) int {
		left, right := strings.Split(a, "/"), strings.Split(b, "/")
		for i := 0; i < min(len(left), len(right)); i++ {
			if strings.EqualFold(left[i], right[i]) {
				continue
			}
			ar, br := ranks[strings.ToLower(strings.Join(left[:i+1], "/"))], ranks[strings.ToLower(strings.Join(right[:i+1], "/"))]
			if ar != br {
				if ar == 0 || (br != 0 && ar > br) {
					return 1
				}
				return -1
			}
			if snippetGroupLess(a, b) {
				return -1
			}
			return 1
		}
		return len(left) - len(right)
	})
	return groups
}

func (state *State) moveSnippetPickerGroup(source, target string, after bool) bool {
	if sameSnippetGroup(source, target) || !sameSnippetGroup(snippetGroupParent(source), snippetGroupParent(target)) {
		return false
	}
	var moved, kept []string
	for _, group := range state.snippetPickerGroups() {
		if snippetGroupWithin(group, source) {
			moved = append(moved, group)
		} else {
			kept = append(kept, group)
		}
	}
	at := -1
	for i, group := range kept {
		if sameSnippetGroup(group, target) || (after && snippetGroupWithin(group, target)) {
			at = i
			if after {
				at++
			}
		}
	}
	if len(moved) == 0 || at < 0 {
		return false
	}
	state.snippets.pickerGroupOrder = slices.Insert(kept, at, moved...)
	state.save()
	state.rebuildCommandPalette()
	for row, index := range state.commandPalette.visible {
		if state.commandPalette.entries[index].foldKey == strings.ToLower(source) {
			state.commandPalette.selected = row
			break
		}
	}
	state.commandPalette.follow()
	return true
}
