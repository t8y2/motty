package workbench

import "github.com/vivaui/viva/gfx"

type snippetPickerDrag struct {
	active, moved, valid bool
	source, to, row      int
	name                 string
	startY               float32
}

func (state *State) snippetPickerDragPointer(event gfx.PointerEvent) gfx.EventResult {
	palette := &state.commandPalette
	drag := &palette.snippetDrag
	handle, onHandle := event.Target.In(hitSnippetPickerHandle)
	if event.Kind == gfx.PointerDown {
		*drag = snippetPickerDrag{}
		if !palette.snippets || state.snippetSend.target == sendChosen || state.snippetMenu.open || state.snippets.folderMenu.open ||
			!onHandle || event.Button != gfx.PointerButtonPrimary || handle >= len(palette.visible) {
			return gfx.EventResult{}
		}
		index, ok := palette.entries[palette.visible[handle]].target.In(hitSnippetSendList)
		if !ok {
			return gfx.EventResult{}
		}
		*drag = snippetPickerDrag{active: true, source: index, name: state.snippets.list[index].Name, startY: event.Y}
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
	if event.Y-drag.startY > 4 || drag.startY-event.Y > 4 {
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
	drag.to, drag.valid = state.snippetPickerDropIndex(drag.source, drag.row)
	drag.valid = drag.valid && event.X >= palette.listRect.X && event.X <= palette.listRect.X+palette.listRect.Width
	if event.Kind == gfx.PointerUp {
		saved := *drag
		*drag = snippetPickerDrag{}
		if saved.moved && saved.valid && saved.source < len(state.snippets.list) &&
			state.snippets.list[saved.source].Name == saved.name {
			state.reorderSnippetPicker(saved.source, saved.to)
		}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

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
