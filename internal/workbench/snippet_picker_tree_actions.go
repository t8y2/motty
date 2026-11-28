package workbench

import (
	"maps"
	"strings"

	"github.com/vivaui/viva/ui"
)

func (state *State) snippetPickerTreeCanDrop(from, to int, where ui.TreeDrop) bool {
	palette := &state.commandPalette
	rows := palette.snippetTree.rows
	if !palette.usesSnippetTree() || palette.query != "" || from < 0 || to < 0 || from >= len(rows) || to >= len(rows) || from == to {
		return false
	}
	if (!rows[from].folder && rows[from].snippet < 0) || (!rows[to].folder && rows[to].snippet < 0) {
		return false
	}
	return state.canDropSnippetEntries(rows[from], rows[to], where)
}

func (state *State) snippetPickerTreeChanges() {
	palette := &state.commandPalette
	picker := &palette.snippetTree
	if row, ok := picker.state.Toggled(); ok {
		entry := picker.rows[row]
		state.foldSnippetGroup(entry.groupKey, !palette.groupExpanded(entry.groupKey))
		return
	}
	if row, ok := picker.state.Chosen(); ok {
		palette.selected = row
		state.executeCommandPalette(row)
		return
	}
	if move, ok := picker.state.Dropped(); ok && state.snippetPickerTreeCanDrop(move.From, move.To, move.Where) {
		state.dropSnippetPickerTree(move)
	}
}

// 复用目录树的数据操作，同时保留设置页自己的选择和展开状态。
func (state *State) dropSnippetPickerTree(move ui.TreeMove) {
	palette, form := &state.commandPalette, &state.snippets
	src, dst := palette.snippetTree.rows[move.From], palette.snippetTree.rows[move.To]
	selected, folder := "", form.selectedFolder
	if form.selected >= 0 && form.selected < len(form.list) {
		selected = form.list[form.selected].Name
	}
	collapsed := maps.Clone(form.treeCollapsedGroups)
	group := src.group
	if src.folder {
		parent := dst.group
		if move.Where != ui.TreeDropInside {
			parent = snippetGroupParent(dst.group)
		}
		group = state.snippetFolderMoveTarget(src.group, parent)
		remapCommandGroupMap(collapsed, src.group, group)
		if snippetGroupWithin(folder, src.group) {
			folder = remapSnippetGroupPath(folder, src.group, group)
		}
	}
	state.dropSnippetEntries(src, dst, move.Where)
	for i, entry := range form.list {
		if entry.Name == selected {
			form.selected = i
			break
		}
	}
	form.selectedFolder, form.treeCollapsedGroups = folder, collapsed
	state.refreshSnippets()
	parent := snippetGroupParent(group)
	if !src.folder {
		for _, entry := range form.list {
			if entry.Name == src.label {
				parent = entry.Group
				break
			}
		}
	}
	for ; parent != ""; parent = snippetGroupParent(parent) {
		key := strings.ToLower(parent)
		if !palette.groupExpanded(key) {
			palette.expandedGroups = append(palette.expandedGroups, key)
		}
	}
	state.rebuildCommandPalette()
	for row, index := range palette.visible {
		entry := palette.entries[index]
		if (src.folder && sameSnippetGroup(entry.foldKey, group)) || (!src.folder && entry.label == src.label && !entry.header) {
			palette.selected = row
			break
		}
	}
	palette.follow()
}
