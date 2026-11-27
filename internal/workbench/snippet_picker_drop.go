package workbench

// 分组视图只允许组内调整；搜索结果按相邻命令映射到全局顺序。
func (state *State) snippetPickerDropIndex(source, row int) (int, bool) {
	palette := &state.commandPalette
	if source < 0 || source >= len(state.snippets.list) {
		return 0, false
	}
	candidate := func(at int) (int, bool) {
		if at < 0 || at >= len(palette.visible) {
			return 0, false
		}
		index, ok := palette.entries[palette.visible[at]].target.In(hitSnippetSendList)
		if !ok || index >= len(state.snippets.list) {
			return 0, false
		}
		if state.snippets.pickerGrouped && !sameSnippetGroup(state.snippets.list[index].Group, state.snippets.list[source].Group) {
			return 0, false
		}
		return index, true
	}
	if index, ok := candidate(row); ok {
		return index, true
	}
	if index, ok := candidate(row - 1); ok {
		return index + 1, true
	}
	return 0, false
}
