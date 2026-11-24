package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) declareLocalDirectoryPicker(c ui.C) {
	if state.modal != modalLocalDirectory {
		return
	}
	state.refreshLocalDirectories()
	picker, m := &state.localDirectoryPicker, state.messages
	ui.PopupLayer(c, dialogLayer)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.LocalDirectoryTitle, Width: 620, Height: 520, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitLocalDirectoryClose, InitialFocusID: hitLocalDirectorySearch,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(8).Children(func() {
			ui.TextFieldID(c, hitLocalDirectorySearch, &picker.field, m.LocalDirectorySearch, m.LocalDirectorySearch).Height(36).FillWidth().Shrink(0).NavigationKeys()
			ui.Button(c, m.LocalDirectoryBrowse).Icon(lucideIcons[iconFolderOpen]).Hit(hitLocalDirectoryBrowse, m.LocalDirectoryBrowse).Height(36).FillWidth().Shrink(0)
			ui.Text(c, m.LocalDirectoryHistory).FontSize(metrics.FontSizeS).Color(state.pal.muted)
			list := ui.Scroll(c).FillWidth().Grow(1).Scrollbars()
			picker.scroll = list.ScrollState()
			list.Children(func() {
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					for index, directory := range picker.visible {
						id := hitLocalDirectoryRows.At(index)
						tint := withAlpha(state.transferBlue(), 0.14)
						background := withAlpha(tint, 0)
						if index == picker.selected || c.Canvas().Hovered() == hitLocalDirectoryRemove.At(index) {
							background = tint
						}
						row := ui.Row(c).FillWidth().Height(34).Gap(4).Radius(radius.control).Background(background)
						row.HitWith(id, directory, gfx.HitOptions{Role: gfx.RoleButton, SkipTab: true, Hint: directory}).Tooltip(directory).
							Hover(ui.Look{}.Background(tint)).Transition().Children(func() {
							ui.Row(c).Grow(1).Shrink(1).MinWidth(0).Height(34).PadX(10).Gap(8).Items(ui.Center).Children(func() {
								ui.Icon(c, lucideIcons[iconFolderOpen]).Size(16, 16).Shrink(0).Color(state.pal.muted)
								ui.Text(c, directory).FontSize(metrics.FontSizeM).Ellipsis().Grow(1).Shrink(1).MinWidth(0).Color(state.pal.text)
							})
							ui.Button(c, m.LocalDirectoryRemove).Icon(lucideIcons[iconClose]).IconOnly().Ghost().Hit(hitLocalDirectoryRemove.At(index), m.LocalDirectoryRemove).Tooltip(m.LocalDirectoryRemove).Size(30, 34).Shrink(0)
						})
					}
					if len(picker.visible) == 0 {
						ui.Text(c, m.LocalDirectoryEmpty).Color(state.pal.muted)
					}
				})
			})
			if picker.errorText != "" {
				ui.Text(c, picker.errorText).Color(state.pal.muted)
			}
			ui.Text(c, m.LocalDirectoryHint).FontSize(metrics.FontSizeS).Color(state.pal.muted).Shrink(0)
		})
	})
	if dialog.Dismissed() {
		state.modal = modalNone
	}
}

func (state *State) localDirectoryKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalLocalDirectory {
		return gfx.EventResult{}, false
	}
	picker := &state.localDirectoryPicker
	state.refreshLocalDirectories()
	switch event.Key {
	case gfx.KeyEscape:
		state.modal = modalNone
	case gfx.KeyDown:
		picker.selected = min(picker.selected+1, len(picker.visible)-1)
	case gfx.KeyUp:
		picker.selected = max(0, picker.selected-1)
	case gfx.KeyEnter:
		if event.Target != hitLocalDirectorySearch && !event.Target.IsZero() {
			return gfx.EventResult{}, false
		}
		if !event.Repeat && len(picker.visible) > 0 {
			state.chooseLocalDirectory(picker.visible[picker.selected])
		}
	default:
		return gfx.EventResult{}, false
	}
	if picker.scroll != nil && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		top := float32(picker.selected) * 38
		picker.scroll.ScrollTo(max(0, top-152))
	}
	result := gfx.EventResult{Handled: true, Repaint: true}
	if state.modal == modalNone {
		result.Focus = state.activeTerminalFocusHit()
	}
	return result, true
}
