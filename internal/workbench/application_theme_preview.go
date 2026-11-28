package workbench

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintApplicationOpacity(page *settingsPage) {
	c, slider := page.c, &state.applicationThemeUI.opacity
	label := state.themeText("Interface background opacity", "界面背景不透明度", "介面背景不透明度")
	description := state.themeText("Applies over a background image or on supported transparent windows.", "在背景图片上或支持透明的窗口中生效。", "在背景圖片上或支援透明的視窗中生效。")
	page.lineFor(hitApplicationOpacity, label, description, 182, func() {
		ui.Row(c).Grow(1).Gap(8).Items(ui.Center).Children(func() {
			wasDragging := slider.Dragging()
			control := ui.Slider(c, slider).Range(0, 100).Step(1).Label(label).Hit(hitApplicationOpacity, label).Grow(1).Shrink(1)
			slider.ID = hitApplicationOpacity
			changed := control.Changed()
			opacity := int(slider.Value)
			if opacity != state.applicationOpacity() {
				state.applicationTheme.Opacity = &opacity
				if opacity == 100 {
					state.applicationTheme.Opacity = nil
				}
			}
			if !slider.Dragging() && (wasDragging || changed) {
				state.save()
			}
			ui.Text(c, strconv.Itoa(opacity)+"%").FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(36).Shrink(0).TextEnd()
		})
	})
}

func (state *State) applicationOpacity() int {
	if state.appearanceFollowTerminal && state.appearanceMode != appearanceCustom {
		if scheme, ok := state.schemeByID(state.activeScheme()); ok && scheme.opacity != nil {
			return min(max(*scheme.opacity, 0), 100)
		}
		return 100
	}
	if state.appearanceMode != appearanceCustom {
		return 100
	}
	if opacity := state.applicationTheme.Opacity; opacity != nil {
		return min(max(*opacity, 0), 100)
	}
	return 100
}

func (state *State) applicationGround(color gfx.Color) gfx.Color {
	if state.terminalTransparent() || state.showsBackground() {
		color.Alpha *= float32(state.applicationOpacity()) / 100
	}
	return color
}
