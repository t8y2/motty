package workbench

import (
	"maps"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

type applicationTheme struct {
	BaseColors   map[string]string `json:"baseColors,omitempty"`
	Base         string            `json:"base,omitempty"`
	FollowBase   string            `json:"followBase,omitempty"`
	FollowScheme string            `json:"followScheme,omitempty"`
	Colors       map[string]string `json:"colors,omitempty"`
	Opacity      *int              `json:"opacity,omitempty"`
}

func (theme applicationTheme) snapshot() *applicationTheme {
	if theme.Base == "" && theme.FollowScheme == "" && len(theme.Colors) == 0 && len(theme.BaseColors) == 0 && theme.Opacity == nil {
		return nil
	}
	theme.Colors = maps.Clone(theme.Colors)
	theme.BaseColors = maps.Clone(theme.BaseColors)
	return &theme
}

func (theme applicationTheme) baseAppearance() appearance {
	if theme.Base == "light" {
		return appearanceLight
	}
	return appearanceDark
}

func applicationColorAppearance(rgb [3]uint8) appearance {
	if luminance(rgb) >= 0.5 {
		return appearanceLight
	}
	return appearanceDark
}

func (theme applicationTheme) colorAppearance() appearance {
	if color, ok := components.ParseColorHex(theme.Colors["workspace"], false); ok {
		return applicationColorAppearance(rgb8(color))
	}
	if color, ok := components.ParseColorHex(theme.BaseColors["workspace"], false); ok {
		return applicationColorAppearance(rgb8(color))
	}
	return theme.baseAppearance()
}

func (state *State) terminalSchemeAppearance() appearance {
	if state.appearanceMode == appearanceCustom {
		return state.applicationTheme.baseAppearance()
	}
	return state.appearance
}

func (state *State) refreshApplicationTheme() {
	state.syncNativeAppearance()
	state.setAppearance(state.resolveAppearance())
	if invalidate := state.options.Invalidate; invalidate != nil {
		invalidate()
	}
}

func (state *State) setApplicationColor(key string, color gfx.Color) {
	theme := &state.applicationTheme
	theme.setColor(key, color)
	state.refreshApplicationTheme()
	state.save()
}

func (theme *applicationTheme) setColor(key string, color gfx.Color) {
	if color.Alpha == 0 {
		delete(theme.Colors, key)
	} else {
		if theme.Colors == nil {
			theme.Colors = make(map[string]string)
		}
		theme.Colors[key] = applicationColorHex(color)
	}
}

func (state *State) resetApplicationColors() {
	state.applicationTheme.Colors = nil
	state.applicationTheme.Opacity = nil
	state.applicationThemeUI.opacity.Value = 100
	state.refreshApplicationTheme()
	state.save()
}

func blendApplicationColor(from, to gfx.Color, fraction float32) gfx.Color {
	return gfx.Color{
		Red: from.Red + (to.Red-from.Red)*fraction, Green: from.Green + (to.Green-from.Green)*fraction,
		Blue: from.Blue + (to.Blue-from.Blue)*fraction, Alpha: 1,
	}
}

func applicationRGB(color [3]uint8) gfx.Color {
	return gfx.RGB(color[0], color[1], color[2])
}
