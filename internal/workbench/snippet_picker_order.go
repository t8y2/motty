package workbench

import "github.com/vivaui/viva/gfx"

// snippetPickerNeighbor 按选择器的可见顺序查找同组命令，不使用设置页筛选。
func (state *State) snippetPickerNeighbor(source, direction int) int {
	palette := &state.commandPalette
	row := -1
	for at, entry := range palette.visible {
		if palette.entries[entry].target == hitSnippetSendList.At(source) {
			row = at
			break
		}
	}
	if row < 0 || state.snippetSend.target == sendChosen {
		return -1
	}
	for at := row + direction; at >= 0 && at < len(palette.visible); at += direction {
		index, ok := palette.entries[palette.visible[at]].target.In(hitSnippetSendList)
		if ok && (!state.snippets.pickerGrouped || sameSnippetGroup(state.snippets.list[index].Group, state.snippets.list[source].Group)) {
			return index
		}
	}
	return -1
}

// reorderSnippetPicker 为拖拽、菜单和键盘共用持久化及选中项恢复逻辑。
func (state *State) reorderSnippetPicker(source, to int) bool {
	if !state.moveSnippetInOrder(source, to) {
		return false
	}
	selected := to
	if source < to {
		selected--
	}
	palette := &state.commandPalette
	scroll := palette.scroll
	state.rebuildCommandPalette()
	palette.scroll = min(scroll, palette.maxScroll())
	for row, index := range palette.visible {
		if palette.entries[index].target == hitSnippetSendList.At(selected) {
			palette.selected = row
			break
		}
	}
	palette.follow()
	return true
}

func (state *State) moveSnippetPicker(source, direction int) bool {
	target := state.snippetPickerNeighbor(source, direction)
	if target < 0 {
		return false
	}
	if target > source {
		target++
	}
	return state.reorderSnippetPicker(source, target)
}

func (state *State) snippetPickerOrderKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	palette := &state.commandPalette
	if !palette.snippets || event.Release || event.Modifiers != gfx.ModifierAlt ||
		(event.Key != gfx.KeyUp && event.Key != gfx.KeyDown) || state.snippetSend.target == sendChosen {
		return gfx.EventResult{}, false
	}
	row := palette.selected
	if row >= 0 && row < len(palette.visible) {
		if source, ok := palette.entries[palette.visible[row]].target.In(hitSnippetSendList); ok {
			direction := -1
			if event.Key == gfx.KeyDown {
				direction = 1
			}
			state.moveSnippetPicker(source, direction)
		}
	}
	focus := hitCommandPaletteSearch
	if _, inItems := event.Target.In(hitCommandPaletteItems); inItems {
		focus = hitCommandPaletteItems.At(palette.selected)
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}, true
}
