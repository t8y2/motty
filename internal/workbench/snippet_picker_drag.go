package workbench

import "github.com/vivaui/viva/gfx"

type snippetPickerDrag struct {
	active, moved, valid    bool
	source, to, row, origin int
	name, group, target     string
	after                   bool
	startX, startY          float32
}

func (state *State) snippetPickerDragPointer(event gfx.PointerEvent) gfx.EventResult {
	palette := &state.commandPalette
	drag := &palette.snippetDrag
	row, onRow := event.Target.In(hitCommandPaletteItems)
	if event.Kind == gfx.PointerDown {
		*drag = snippetPickerDrag{}
		if !palette.snippets || state.snippetSend.target == sendChosen || state.snippetMenu.open || state.snippets.folderMenu.open ||
			!onRow || event.Button != gfx.PointerButtonPrimary || row < 0 || row >= len(palette.visible) {
			return gfx.EventResult{}
		}
		entry := palette.entries[palette.visible[row]]
		index, command := entry.target.In(hitSnippetSendList)
		if !command && (entry.foldKey == "" || entry.foldKey == ungroupedFoldKey) {
			return gfx.EventResult{}
		}
		palette.selected = row
		*drag = snippetPickerDrag{active: true, source: index, origin: row, name: entry.label, group: entry.foldKey,
			startX: event.X, startY: event.Y}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if !drag.active {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerExit {
		*drag = snippetPickerDrag{}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Kind != gfx.PointerMove && event.Kind != gfx.PointerUp {
		return gfx.EventResult{Handled: true}
	}
	if event.Y-drag.startY > 4 || drag.startY-event.Y > 4 || event.X-drag.startX > 4 || drag.startX-event.X > 4 {
		drag.moved = true
	}
	if drag.moved && event.Kind == gfx.PointerMove {
		if event.Y < palette.listRect.Y+16 {
			palette.scroll = max(0, palette.scroll-commandPaletteRowStep)
		}
		if event.Y > palette.listRect.Y+palette.listRect.Height-16 {
			palette.scroll = min(palette.maxScroll(), palette.scroll+commandPaletteRowStep)
		}
		if palette.scrollState != nil {
			palette.scrollState.ScrollTo(palette.scroll)
		}
	}
	drag.row = min(max(int((event.Y-palette.listRect.Y+palette.scroll+commandPaletteRowStep/2)/commandPaletteRowStep), 0), len(palette.visible))
	if drag.group != "" {
		drag.target, drag.after, drag.valid = state.snippetPickerGroupDrop(drag.group, drag.row)
		if drag.valid {
			drag.row = state.snippetPickerGroupDropRow(drag.target, drag.after)
		}
	} else {
		drag.to, drag.valid = state.snippetPickerDropIndex(drag.source, drag.row)
	}
	drag.valid = drag.valid && pointInRect(event.X, event.Y, palette.listRect)
	if event.Kind == gfx.PointerUp {
		saved := *drag
		*drag = snippetPickerDrag{}
		if !saved.moved && pointInRect(event.X, event.Y, palette.listRect) {
			state.Click(hitCommandPaletteItems.At(saved.origin))
		} else if saved.moved && saved.valid {
			if saved.group != "" {
				state.moveSnippetPickerGroup(saved.group, saved.target, saved.after)
			} else if saved.source < len(state.snippets.list) && state.snippets.list[saved.source].Name == saved.name {
				state.reorderSnippetPicker(saved.source, saved.to)
			}
		}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
