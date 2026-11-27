package workbench

// snippetPickerGroupDrop 将可见行位置映射为同级分组之前或之后的插入位置。
func (state *State) snippetPickerGroupDrop(source string, row int) (string, bool, bool) {
	palette := &state.commandPalette
	parent := snippetGroupParent(source)
	candidate := func(at int) string {
		if at < 0 || at >= len(palette.visible) {
			return ""
		}
		entry := palette.entries[palette.visible[at]]
		group := entry.foldKey
		if index, ok := entry.target.In(hitSnippetSendList); ok {
			group = state.snippets.list[index].Group
		}
		if group == ungroupedFoldKey {
			return ""
		}
		for group != "" && !sameSnippetGroup(snippetGroupParent(group), parent) {
			group = snippetGroupParent(group)
		}
		if sameSnippetGroup(group, source) {
			return ""
		}
		return group
	}
	if group := candidate(row); group != "" {
		return group, false, true
	}
	if group := candidate(row - 1); group != "" {
		return group, true, true
	}
	return "", false, false
}

// 分组拖动的插入线对齐整棵子树，避免显示在目标组的子项之间。
func (state *State) snippetPickerGroupDropRow(target string, after bool) int {
	palette := &state.commandPalette
	row := 0
	for at, index := range palette.visible {
		entry := palette.entries[index]
		group := entry.foldKey
		if snippet, ok := entry.target.In(hitSnippetSendList); ok {
			group = state.snippets.list[snippet].Group
		}
		if snippetGroupWithin(group, target) {
			if !after {
				return at
			}
			row = at + 1
		}
	}
	return row
}
