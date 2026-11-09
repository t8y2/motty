package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func (state *State) paintHostImportResult(canvas *gfx.Canvas, viewport gfx.Rect) {
	messages, palette := state.messages, state.pal
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer:          dialogLayer,
		Scope:          dialogScope,
		DismissID:      hitHostImportDismiss,
		CloseID:        hitHostImportClose,
		InitialFocusID: hitHostImportDone,
		ContentSize:    gfx.Size{Width: 640}, FitBody: true, BodyHeight: state.hostImportBody(),
	})
	result := &state.hostImportResult
	title := messages.SSHConfigResultTitle
	if result.title != "" {
		title = result.title
	}
	layout := dialog.Begin(title, result.summary)
	f := state.importLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		if result.visible == 0 {
			f.Text(messages.SSHConfigNoWarnings).FontSize(metrics.FontSizeS).Color(palette.muted).LineHeight(22).MarginSides(10, 0, 0, 0)
			return
		}
		f.Column().Gap(5).PadSides(4, 0, 0, 0).Children(func() {
			f.Text(result.warningsTitle).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(palette.warning).LineHeight(22).MarginSides(0, 0, 1, 0)
			for index := 0; index < result.visible; index++ {
				f.Text(result.warnings[index]).FontSize(metrics.FontSizeS).Color(palette.muted).LineHeight(22)
			}
			if result.more != "" {
				f.Text(result.more).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(palette.warning).LineHeight(22)
			}
		})
	})
	f.End()
	dialog.Actions(layout, components.ButtonAction{ID: hitHostImportDone, Label: messages.SSHConfigDone})
	dialog.End()
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
