package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
)

func (state *State) openSnippetPickerMenuOn(canvas *gfx.Canvas, id gfx.ID, entry commandPaletteEntry) {
	if !state.commandPalette.snippets || state.snippetSend.target == sendChosen {
		return
	}
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary {
		return
	}
	if entry.foldKey != "" {
		for _, group := range state.snippets.groups {
			if strings.ToLower(group) == entry.foldKey {
				state.snippetMenu.open = false
				state.snippets.folderMenu = snippetFolderMenu{open: true, fromPicker: true,
					group: group, at: gfx.Point{X: event.X, Y: event.Y}}
				canvas.Consume()
				return
			}
		}
		return
	}
	index, ok := entry.target.In(hitSnippetSendList)
	if !ok {
		return
	}
	state.snippets.folderMenu.open = false
	state.snippetMenu = snippetMenu{open: true, fromPicker: true, index: index,
		name: state.snippets.list[index].Name, at: gfx.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}
