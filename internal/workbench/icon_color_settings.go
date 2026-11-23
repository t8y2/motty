package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var iconColorPickerHits = tree.Feature("icon-color-picker")

var (
	hitSettingsIconColorChoose = tree.ID(settingsHits, "settings-icon-color-choose")
	hitSettingsIconColors      = tree.Sub(iconColorPickerHits, tree.Name("color"))
	hitIconColorDismiss        = tree.ID(iconColorPickerHits, "dismiss")
	hitIconColorClose          = tree.ID(iconColorPickerHits, "close")
	hitIconColorCancel         = tree.ID(iconColorPickerHits, "cancel")
	hitIconColorConfirm        = tree.ID(iconColorPickerHits, "confirm")
)

func (state *State) paintIconColorSetting(page *settingsPage) {
	m := state.messages
	page.valueActionRow(m.SettingsIconColor, m.SettingsIconColorDescription, state.iconColorLabel(state.iconColor), false,
		components.ButtonAction{ID: hitSettingsIconColorChoose, Label: m.Tools.FontChoose, Variant: components.ButtonOutline})
}

func (state *State) openIconColorPicker() {
	state.iconColorChoice = state.iconColor
	state.modal = modalIconColorPicker
}

func (state *State) confirmIconColor() {
	choice := state.iconColorChoice
	state.closeModal()
	if state.setIconColor(choice) {
		state.save()
	}
}

func (state *State) clickIconColorPicker(id gfx.ID) {
	switch id {
	case hitIconColorDismiss, hitIconColorClose, hitIconColorCancel:
		state.closeModal()
	case hitIconColorConfirm:
		state.confirmIconColor()
	}
}

func (state *State) declareIconColorPicker(c ui.C) {
	m := state.messages
	const width, bodyHeight = float32(560), float32(230)
	layout := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), width, m.SettingsIconColorDescription, bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.SettingsIconColor, Description: m.SettingsIconColorDescription, Width: width, Height: layout.Content.Height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitIconColorDismiss, CloseID: hitIconColorClose,
		InitialFocusID: hitSettingsIconColors.At(int(state.iconColorChoice)),
	})
	dialog.Body(func() {
		state.declareIconColorChoices(c, max(88, (layout.Body.Width-20)/3))
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitIconColorCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitIconColorConfirm, Label: m.ColorPickerApply, Variant: components.ButtonDefault},
		)
	})
}

func (state *State) declareIconColorChoices(c ui.C, cellWidth float32) {
	pal := state.pal
	ui.Grid(c, cellWidth).FillWidth().Gap(10).Children(func() {
		for color := iconMint; color < iconColorCount; color++ {
			label := state.iconColorLabel(color)
			selected := state.iconColorChoice == color
			border, fill, borderWidth := pal.line, pal.workspace, float32(1)
			if selected {
				border, fill, borderWidth = pal.text, pal.surfaceSoft, 2
			}
			card := ui.Column(c).Height(110).PadY(10).Gap(8).Items(ui.Center).Justify(ui.Center).
				Background(fill).Radius(radius.control).Border(borderWidth, border).
				Hover(ui.Look{}.Background(pal.surfaceSoft)).Pressed(ui.Look{}.Background(pal.surfaceHot)).
				Focus(ui.Look{}.Ring(2, pal.accent)).
				HitWith(hitSettingsIconColors.At(int(color)), label, gfx.HitOptions{
					Role: gfx.RoleRadio, Checked: selected, Selected: selected, SkipTab: !selected, Cursor: gfx.PointerCursorHand,
				})
			card.Children(func() {
				ui.Image(c, color.previewBitmap()).Size(64, 64).Shrink(0)
				ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(18).TextCenter()
				if selected {
					ui.Row(c).Absolute().Top(6).Right(6).Size(16, 16).Radius(8).Background(pal.text).Center().Children(func() {
						ui.Icon(c, lucideIcons[iconCheck]).Size(10, 10).Color(pal.workspace)
					})
				}
			})
			if card.Clicked() {
				state.iconColorChoice = color
			}
		}
	})
}

func (state *State) iconColorKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalIconColorPicker || event.Release {
		return gfx.EventResult{}, false
	}
	index, found := event.Target.In(hitSettingsIconColors)
	if event.Key == gfx.KeyEscape {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == gfx.KeyEnter && (found || event.Target.IsZero()) {
		if !event.Repeat {
			state.confirmIconColor()
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if !found || index < 0 || index >= int(iconColorCount) {
		return gfx.EventResult{}, false
	}
	var options [iconColorCount]components.RadioOption
	for color := iconMint; color < iconColorCount; color++ {
		options[color].ID = hitSettingsIconColors.At(int(color))
	}
	if next, handled := components.RadioGroupKeyDown(event.Target, options[:], components.DirectionLTR, event.Key); handled {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true, Activate: next}, true
	}
	return gfx.EventResult{}, false
}
