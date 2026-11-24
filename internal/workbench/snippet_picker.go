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

// groupAncestorsExpanded reports whether every ancestor folder of path is open.
// A top-level group has no ancestors and is always visible in the picker tree.
func (palette *commandPalette) groupAncestorsExpanded(path string) bool {
	for parent := snippetGroupParent(path); parent != ""; parent = snippetGroupParent(parent) {
		if !palette.groupExpanded(strings.ToLower(parent)) {
			return false
		}
	}
	return true
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
	// The bar's show/hide checkbox is chrome of the picker, declared above the
	// search field. Nothing here belongs to the searchable list: the entries
	// are snippets only, so a query never has to account for a setting.
	// folded is set while a group or any of its ancestors is closed: its
	// snippets still answer a search without appearing in the unfiltered tree.
	folded := false
	add := func(index int, depth uint8) {
		entry := &state.snippets.list[index]
		palette.addDetailed(entry.Name, snippetDetail(entry), hitSnippetSendList.At(index))
		last := &palette.entries[len(palette.entries)-1]
		last.search += " " + strings.ToLower(entry.Command)
		last.searchOnly = folded
		last.depth = depth
	}
	header := func(label, key string, count int, depth uint8) {
		palette.addHeader(label, strconv.Itoa(count))
		last := &palette.entries[len(palette.entries)-1]
		last.foldKey, last.expanded, last.depth = key, palette.groupExpanded(key), depth
		folded = !last.expanded
	}
	form := &state.snippets
	if len(form.groups) == 0 {
		for index := range form.list {
			add(index, 0)
		}
	} else {
		// With groups, the unfiltered list is a collapsible tree in pre-order,
		// the ungrouped last; a search drops the headings and still matches
		// every snippet and its full group path.
		for _, group := range form.groups {
			key := strings.ToLower(group)
			depth := snippetGroupDepth(group)
			if palette.groupAncestorsExpanded(group) {
				header(snippetGroupLeaf(group), key, form.groupCounts[group], depth)
			} else {
				folded = true
			}
			for index := range form.list {
				if sameSnippetGroup(form.list[index].Group, group) {
					add(index, depth+1)
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
			header(state.messages.Tools.SnippetFilterNone, ungroupedFoldKey, count, 0)
			for index := range form.list {
				if form.list[index].Group == "" {
					add(index, 1)
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
