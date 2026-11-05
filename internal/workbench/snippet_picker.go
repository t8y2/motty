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
	for index, entry := range state.snippets.list {
		detail := ""
		if entry.Shortcut.valid() {
			detail = entry.Shortcut.label()
		}
		palette.addDetailed(entry.Name, detail, hitSnippetSendList.At(index))
		palette.entries[len(palette.entries)-1].search += " " + strings.ToLower(entry.Command)
	}
	if len(state.snippets.list) == 0 {
		palette.add(state.messages.Tools.SnippetPickerAdd, hitSnippets)
	}
	palette.rawQuery = ""
	palette.query = ""
	palette.filtered = false
	state.refreshCommandPalette()
}
