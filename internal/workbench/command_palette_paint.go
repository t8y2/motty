package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

const (
	commandPaletteVisibleRows = 10
	// commandPaletteRowPitch is a row and the gap below it.
	commandPaletteRowPitch = 34
	// commandPaletteBarGap is how far the scrollbar's strip sits beyond the
	// list: in the dialog's padding, so it never covers a row's text.
	commandPaletteBarGap = 8
	// commandPaletteBarWidth is the slim bar's strip, as the scroll area's
	// overlay style draws it; the thumb is a little narrower.
	commandPaletteBarWidth = 6
)

func (state *State) paintCommandPalette(canvas *ui.Canvas, viewport ui.Rect) {
	if state.commandPalette.connect {
		state.adoptWSLDistros()
	}
	state.refreshCommandPalette()
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitCommandPaletteDismiss, CloseID: hitCommandPaletteClose, InitialFocusID: hitCommandPaletteSearch,
		ContentSize: ui.Size{Width: 620, Height: 474},
	})
	title, placeholder, empty := state.messages.CommandPaletteTitle, state.messages.CommandPalettePlaceholder, state.messages.CommandPaletteEmpty
	switch {
	case state.commandPalette.connect:
		title, placeholder, empty = state.messages.LabelNewTab, state.messages.NewTabPickerPlaceholder, state.messages.NewTabPickerEmpty
	case state.commandPalette.snippets:
		title, placeholder, empty = state.messages.Tools.SnippetsTitle, state.messages.Tools.SnippetPickerPlaceholder, state.messages.Tools.SnippetPickerEmpty
	}
	layout := dialog.Begin(title, "")
	if state.commandPalette.connect {
		state.paintConnectSort(canvas, layout)
	}
	value := state.commandPalette.search.String()
	focused := canvas.Focused() == hitCommandPaletteSearch
	palette := &state.commandPalette
	count := min(len(palette.visible)-palette.top, commandPaletteVisibleRows)

	var search flex.Box
	f := state.paletteLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		search = f.Slot().Height(36).FillWidth().Shrink(0)
		if count == 0 {
			f.Text(empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(24).TextCenter().MarginSides(36, 0, 0, 0)
			return
		}
		f.Column().Gap(4).MarginSides(12, 0, 0, 0).Children(func() {
			for visible := 0; visible < count; visible++ {
				entry := palette.entries[palette.visible[palette.top+visible]]
				if entry.header {
					f.Row().Height(30).PadX(10).Gap(6).Items(flex.Center).Shrink(0).
						HitOutset(2, 0, 2, 0).
						HitWith(hitCommandPaletteHeaders.At(visible), entry.label, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true}).
						Children(func() {
							f.Text(entry.label).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(state.pal.faint).LineHeight(20).Ellipsis().Shrink(1)
							if entry.detail != "" {
								f.Text(entry.detail).FontSize(metrics.FontSizeS).Color(state.pal.faint).LineHeight(20).Shrink(0)
							}
						})
					continue
				}
				id := hitCommandPaletteItems.At(visible)
				selected := palette.selected == palette.top+visible
				color, detailColor := state.pal.muted, state.pal.faint
				if selected {
					color, detailColor = state.pal.onAccent, state.pal.onAccent
				}
				row := f.Row().Height(30).PadX(10).Gap(12).Items(flex.Center).Shrink(0)
				if selected {
					row.Background(state.pal.accent).Radius(radius.item)
				}
				// The rows touch through the gap between them, so crossing it
				// never takes the pointer off the list and flashes the first row.
				// Rows are not tab stops: the pointer and the arrows are the one
				// way the selection moves, so Tab keeps its normal traversal.
				row.HitOutset(2, 0, 2, 0).HitWith(id, entry.label, ui.HitOptions{Role: ui.RoleButton, SkipTab: true, Hint: entry.recencyHint}).Children(func() {
					if entry.recency == "" {
						f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(color).Ellipsis().Grow(1)
					} else {
						f.Row().Grow(1).Shrink(1).Gap(8).Items(flex.Center).Children(func() {
							f.Text(entry.label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(color).Ellipsis().Shrink(1)
							f.Text(entry.recency).FontSize(metrics.FontSizeS).Color(withAlpha(color, 0.65)).Shrink(0)
						})
					}
					if entry.detail != "" {
						f.Text(entry.detail).FontSize(metrics.FontSizeS).Color(detailColor).Ellipsis().MaxWidth(layout.Body.Width * 0.45).Shrink(0)
					}
				})
			}
		})
	})
	f.End()
	palette.bar = components.ScrollAreaGeometry{}
	if total := len(palette.visible); total > commandPaletteVisibleRows {
		// The bar is a real scrollbar, drawn beside the rows in the dialog's
		// padding: the rows window by index, so its content is as tall as the
		// whole list and its offset is the first row shown.
		list := ui.Rect{X: layout.Body.X, Y: search.Rect().Y + 48, Width: layout.Body.Width + commandPaletteBarGap + commandPaletteBarWidth, Height: commandPaletteVisibleRows*commandPaletteRowPitch - 4}
		content := ui.Size{Width: list.Width, Height: float32(total)*commandPaletteRowPitch - 4}
		area := components.BeginScrollArea(canvas, state.Theme(), list, content).
			Offset(0, float32(palette.top)*commandPaletteRowPitch).
			VerticalScrollbar(hitCommandPaletteBar, title, "").HoverScrollbars(true)
		palette.bar = area.EndContent()
	}
	components.PaintInput(canvas, state.Theme(), search.Rect(), components.InputSnapshot{
		ID: hitCommandPaletteSearch, Label: title, Value: value,
		Placeholder: placeholder, Focused: focused, PlatformEditing: focused && value != "",
		NavigationKeys: true,
	})
	dialog.End()
}
