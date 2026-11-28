package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type snippetPickerTree struct {
	state   ui.TreeState
	focus   gfx.ID
	rows    []snippetTreeEntry
	context ui.C
	label   func(int) string
	content func(int)
}

func (palette *commandPalette) usesSnippetTree() bool { return palette.snippets && palette.treeMode }

func (palette *commandPalette) rowID(row int) gfx.ID {
	if palette.usesSnippetTree() {
		if id := palette.snippetTree.state.RowID(row); !id.IsZero() {
			return id
		}
	}
	return hitCommandPaletteItems.At(row)
}

func (palette *commandPalette) rowIndex(id gfx.ID) (int, bool) {
	if index, ok := id.In(hitCommandPaletteItems); ok {
		return index, true
	}
	if palette.usesSnippetTree() {
		for row := range palette.visible {
			if palette.snippetTree.state.RowID(row) == id {
				return row, true
			}
		}
	}
	return 0, false
}

func (state *State) declareSnippetPickerTree(c ui.C) {
	palette := &state.commandPalette
	picker := &palette.snippetTree
	picker.context = c
	if focused := c.Canvas().FocusVisible(); focused != picker.focus {
		if row, ok := palette.rowIndex(focused); ok {
			palette.selected = row
		}
		picker.focus = focused
	}
	picker.rows = picker.rows[:0]
	for _, index := range palette.visible {
		entry := palette.entries[index]
		row := snippetTreeEntry{folder: entry.folds(), groupKey: entry.foldKey, label: entry.label, snippet: -1, depth: int(entry.depth)}
		if palette.query != "" {
			row.depth = 0
		}
		if row.folder {
			for _, group := range state.snippets.groups {
				if sameSnippetGroup(group, entry.foldKey) {
					row.group = group
					break
				}
			}
		} else if index, ok := entry.target.In(hitSnippetSendList); ok {
			row.snippet, row.group = index, state.snippets.list[index].Group
		}
		picker.rows = append(picker.rows, row)
	}
	if picker.label == nil {
		picker.label = func(i int) string { return picker.rows[i].label }
		picker.content = state.declareSnippetPickerTreeRow
		picker.state.Node = func(i int) ui.TreeNode {
			row := picker.rows[i]
			return ui.TreeNode{Depth: row.depth, Branch: row.folder, Expanded: palette.groupExpanded(row.groupKey), Selected: palette.selected == i}
		}
		picker.state.Key = func(i int) int {
			row := picker.rows[i]
			if row.folder {
				return 1<<20 + state.snippetTreeFolderToken(row.groupKey)
			}
			if row.snippet >= 0 {
				return row.snippet
			}
			return 1 << 21
		}
		picker.state.CanDrop = state.snippetPickerTreeCanDrop
		picker.state.RowHeight, picker.state.Indent = commandPaletteRowStep, snippetTreeIndent
	}
	picker.state.Select(palette.selected)
	ui.Tree(c, &picker.state, len(picker.rows), picker.label, picker.content).
		Width(palette.bodyWidth+commandPaletteBarGap+commandPaletteBarWidth).
		Height(commandPaletteListHeight).Shrink(0).MarginSides(12, 0, 0, 0).
		PadSides(0, commandPaletteBarGap+commandPaletteBarWidth, 0, 0).
		Scrollbars().TreeKeys(false).Label(state.messages.Tools.SnippetListScroll).DrawOver(palette.listOver)
	palette.scrollState = picker.state.Scroll
	if palette.scrollState != nil {
		palette.scroll = palette.scrollState.Offset
	}
	state.snippetPickerTreeChanges()
}
