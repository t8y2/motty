package workbench

import (
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
	state.rebuildCommandPalette()
}

// rebuildSnippetPicker fills the palette with the snippets; choosing one sends it
// like the palette's own snippet entries do. With none saved, the one row leads
// to the Snippets page where one is added.
func (state *State) rebuildSnippetPicker() {
	palette := &state.commandPalette
	add := func(index int) {
		entry := &state.snippets.list[index]
		palette.addDetailed(entry.Name, snippetDetail(entry), hitSnippetSendList.At(index))
		palette.entries[len(palette.entries)-1].search += " " + strings.ToLower(entry.Command)
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
			palette.addHeader(group, "")
			for index := range form.list {
				if sameSnippetGroup(form.list[index].Group, group) {
					add(index)
				}
			}
		}
		if form.hasUngrouped {
			palette.addHeader(state.messages.Tools.SnippetFilterNone, "")
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
