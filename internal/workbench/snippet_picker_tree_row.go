package workbench

import "github.com/vivaui/viva/ui"

func (state *State) declareSnippetPickerTreeRow(row int) {
	palette := &state.commandPalette
	c := palette.snippetTree.context
	entry := palette.entries[palette.visible[row]]
	state.openSnippetPickerMenuOn(c.Canvas(), palette.snippetTree.state.RowID(row), entry)
	color, detail := state.pal.text, state.pal.muted
	if palette.selected == row {
		color, detail = state.pal.onAccent, state.pal.onAccent
	}
	ui.Row(c).Fill().Gap(8).Items(ui.Center).PadX(6).Children(func() {
		ui.Text(c, entry.label).FontSize(metrics.FontSizeM).Color(color).Ellipsis().Grow(1)
		if entry.detail != "" {
			ui.Text(c, entry.detail).FontSize(metrics.FontSizeS).Color(detail).Ellipsis().MaxWidth(palette.bodyWidth * 0.45).Shrink(0)
		}
	})
}
