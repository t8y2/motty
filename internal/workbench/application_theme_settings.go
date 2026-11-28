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
	label := state.themeText
	if state.appearanceMode != appearanceCustom {
		return
	}
	page.group(label("Interface colors", "界面颜色", "介面色彩"), func() {
		for index := range applicationPrimaryColors {
			state.paintApplicationColor(page, index)
		}
		page.lineWith(label("Theme actions", "主题操作", "主題操作"), "", 260, func() {
			ui.Row(page.c).Gap(8).Children(func() {
				advanced := label("Advanced colors", "高级颜色", "進階色彩")
				if state.applicationThemeUI.advanced {
					advanced = label("Hide advanced colors", "收起高级颜色", "收合進階色彩")
				}
				if ui.Button(page.c, advanced).Secondary().Shrink(0).Clicked() {
					state.applicationThemeUI.advanced = !state.applicationThemeUI.advanced
				}
				if ui.Button(page.c, label("Reset", "重置", "重設")).Secondary().Shrink(0).Clicked() {
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
	label, m := state.themeText(token.en, token.zh, token.tw), state.messages
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
