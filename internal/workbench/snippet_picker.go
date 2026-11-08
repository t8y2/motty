package workbench

import (
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/text"
)

// openSnippetPicker lists the saved snippets in the command palette's list, to
// send one to the active terminal. The list is searchable by name, shortcut and
// command, so it stays usable with the many snippets the Snippets page allows.
func (state *State) openSnippetPicker() {
	if state.modal != modalNone {
		return
	}
	palette := &state.commandPalette
	palette.connect, palette.snippets = false, true
	state.modal = modalCommandPalette
	palette.search = text.Editor{}
	palette.filtered = false
	palette.expandedGroups = palette.expandedGroups[:0]
	state.snippetSend.input = text.Editor{}
	state.rebuildCommandPalette()
}

// ungroupedFoldKey names the fold of the snippets that belong to no group; no
// group name can be it, because a group name holds no control characters.
const ungroupedFoldKey = "\x00"

func (palette *commandPalette) groupExpanded(key string) bool {
	return slices.Contains(palette.expandedGroups, key)
}

// foldSnippetGroup opens or closes one group of the picker and keeps the list
// where it was, with the group's row still the one selected.
func (state *State) foldSnippetGroup(key string, expand bool) {
	palette := &state.commandPalette
	if palette.groupExpanded(key) == expand {
		return
	}
	if expand {
		palette.expandedGroups = append(palette.expandedGroups, key)
	} else {
		palette.expandedGroups = slices.DeleteFunc(palette.expandedGroups, func(open string) bool { return open == key })
	}
	scroll := palette.scroll
	state.rebuildCommandPalette()
	for row, index := range palette.visible {
		if palette.entries[index].foldKey == key {
			palette.selected = row
			break
		}
	}
	palette.scroll = min(max(scroll, 0), palette.maxScroll())
}

// rebuildSnippetPicker fills the palette with the snippets; choosing one sends it
// like the palette's own snippet entries do. With none saved, the one row leads
// to the Snippets page where one is added.
func (state *State) rebuildSnippetPicker() {
	if state.snippetSend.target == sendChosen {
		state.rebuildSendTargetPicker()
		return
	}
	palette := &state.commandPalette
	// folded is set while a group is closed: its snippets then answer a search
	// without being listed.
	folded := false
	add := func(index int) {
		entry := &state.snippets.list[index]
		palette.addDetailed(entry.Name, snippetDetail(entry), hitSnippetSendList.At(index))
		last := &palette.entries[len(palette.entries)-1]
		last.search += " " + strings.ToLower(entry.Command)
		last.searchOnly = folded
	}
	header := func(label, key string, count int) {
		palette.addHeader(label, strconv.Itoa(count))
		last := &palette.entries[len(palette.entries)-1]
		last.foldKey, last.expanded = key, palette.groupExpanded(key)
		folded = !last.expanded
	}
	form := &state.snippets
	if len(form.groups) == 0 {
		for index := range form.list {
			add(index)
		}
	} else {
		// With groups, the unfiltered list is sectioned by them, the ungrouped
		// last; a search drops the headings and still matches the group name.
		for _, group := range form.groups {
			count := 0
			for index := range form.list {
				if sameSnippetGroup(form.list[index].Group, group) {
					count++
				}
			}
			header(group, strings.ToLower(group), count)
			for index := range form.list {
				if sameSnippetGroup(form.list[index].Group, group) {
					add(index)
				}
			}
		}
		if form.hasUngrouped {
			count := 0
			for index := range form.list {
				if form.list[index].Group == "" {
					count++
				}
			}
			header(state.messages.Tools.SnippetFilterNone, ungroupedFoldKey, count)
			for index := range form.list {
				if form.list[index].Group == "" {
					add(index)
				}
			}
		}
	}
	if len(state.snippets.list) == 0 {
		palette.add(state.messages.Tools.SnippetPickerAdd, hitSnippets)
	}
	palette.rawQuery = ""
	palette.query = ""
	palette.filtered = false
	state.refreshCommandPalette()
}
