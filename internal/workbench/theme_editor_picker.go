package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// 组件实时修改编辑器草稿，持久化由编辑器的保存操作统一完成。
func (state *State) declareThemeColorPicker(c ui.C, index int, label string) {
	e, m := &state.themeEditor, state.messages
	picker := &e.pickers[index]
	ui.ColorPicker(c, picker, &e.colors[index], label, ui.WithID(hitThemeColors.At(index))).
		Live(true).RoundTrigger().PanelLabels(components.ColorPickerLabels{
		Color: label, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue,
		Hex: m.FieldColorCustom, InvalidColor: m.Themes.HexHint,
	}).Size(32, 32)
	if picker.Open() && e.colorIndex != index {
		e.pickers[e.colorIndex].ClosePanel()
		e.colorIndex = index
		e.syncColorFields()
	}
	if picker.Applied() {
		e.syncColorFields()
	}
}
