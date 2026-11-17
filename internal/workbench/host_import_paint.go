package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) declareHostImportResult(c ui.C) {
	messages, palette := state.messages, state.pal
	result := &state.hostImportResult
	title := messages.SSHConfigResultTitle
	if result.title != "" {
		title = result.title
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 640, result.summary, state.hostImportBody())
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: result.summary,
		Width: 640, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitHostImportDismiss, CloseID: hitHostImportClose, InitialFocusID: hitHostImportDone,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
			if result.visible == 0 {
				ui.Text(c, messages.SSHConfigNoWarnings).FontSize(metrics.FontSizeS).Color(palette.muted).LineHeight(22).MarginSides(10, 0, 0, 0)
				return
			}
			ui.Column(c).Gap(5).PadSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, result.warningsTitle).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(palette.warning).LineHeight(22).MarginSides(0, 0, 1, 0)
				for index := 0; index < result.visible; index++ {
					ui.Text(c, result.warnings[index]).FontSize(metrics.FontSizeS).Color(palette.muted).LineHeight(22)
				}
				if result.more != "" {
					ui.Text(c, result.more).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(palette.warning).LineHeight(22)
				}
			})
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c, components.ButtonAction{ID: hitHostImportDone, Label: messages.SSHConfigDone})
	})
}

// hostImportBody is the height of the warnings: a title, a line for each one
// shown and the line saying how many more there are, or the one line saying
// there are none.
func (state *State) hostImportBody() float32 {
	result := &state.hostImportResult
	if result.visible == 0 {
		return 32
	}
	lines := result.visible
	if result.more != "" {
		lines++
	}
	return 32 + float32(lines)*27 - 5
}
