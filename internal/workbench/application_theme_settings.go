package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var applicationThemeHits = tree.Feature("application-theme")

var hitApplicationOpacity = tree.ID(applicationThemeHits, "opacity")

type applicationThemeControls struct {
	mode     appearanceMode
	pickers  [len(applicationColors)]ui.ColorPickerState
	values   [len(applicationColors)]gfx.Color
	opacity  ui.SliderState
	advanced bool
}

func (state *State) paintApplicationThemeSettings(page *settingsPage) {
	if state.appearanceMode != appearanceCustom {
		return
	}
	page.group(state.messages.Themes.InterfaceColors, func() {
		for index := range applicationPrimaryColors {
			state.paintApplicationColor(page, index)
		}
		page.lineWith(state.messages.Themes.ThemeActions, "", 260, func() {
			ui.Row(page.c).Gap(8).Children(func() {
				advanced := state.messages.Themes.AdvancedColors
				if state.applicationThemeUI.advanced {
					advanced = state.messages.Themes.HideAdvancedColors
				}
				if ui.Button(page.c, advanced).Secondary().Shrink(0).Clicked() {
					state.applicationThemeUI.advanced = !state.applicationThemeUI.advanced
				}
				if ui.Button(page.c, state.messages.Themes.Reset).Secondary().Shrink(0).Clicked() {
					page.c.After(state.resetApplicationColors)
				}
			})
		})
		if state.applicationThemeUI.advanced {
			for index := applicationPrimaryColors; index < len(applicationColors); index++ {
				state.paintApplicationColor(page, index)
			}
		}
	})
}

func (state *State) paintApplicationColor(page *settingsPage, index int) {
	token := applicationColors[index]
	label, m := token.label(state.messages.Themes), state.messages
	id := tree.ID(applicationThemeHits, token.key)
	picker, target := &state.applicationThemeUI.pickers[index], &state.applicationThemeUI.values[index]
	if picker.Applied() {
		state.setApplicationColor(token.key, *target)
	}
	if !picker.Open() {
		*target = *token.color(state.pal)
	}
	shown := *target
	if draft, ok := picker.Draft(); ok {
		shown = draft
	}
	hex := applicationColorHex(shown)
	page.lineFor(id, label, "", 132, func() {
		ui.Row(page.c).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(page.c, hex).Mono().FontSize(metrics.FontSizeS).Color(state.pal.muted).Grow(1)
			ui.ColorPicker(page.c, picker, target, label, ui.WithID(id)).
				PanelLabels(components.ColorPickerLabels{
					Color: label, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue, Hex: m.FieldColorCustom,
					Clear: m.FindSettings.Reset, Cancel: m.Cancel, Apply: m.ColorPickerApply,
				}).Size(34, 34).Shrink(0)
		})
	})
}

func applicationColorHex(color gfx.Color) string {
	return string(components.ColorPickerValue{}.WithColor(color).AppendHex(nil, false))
}
