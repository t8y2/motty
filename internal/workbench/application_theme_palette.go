package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"maps"
	"motty/internal/i18n"
)

type applicationColorToken struct {
	key   string
	label func(i18n.ThemeMessages) string
	color func(*palette) *gfx.Color
}

var applicationColors = [...]applicationColorToken{
	{"shell", func(m i18n.ThemeMessages) string { return m.ColorShell }, func(p *palette) *gfx.Color { return &p.shell }},
	{"workspace", func(m i18n.ThemeMessages) string { return m.ColorWorkspace }, func(p *palette) *gfx.Color { return &p.workspace }},
	{"surface", func(m i18n.ThemeMessages) string { return m.ColorSurface }, func(p *palette) *gfx.Color { return &p.surface }},
	{"text", func(m i18n.ThemeMessages) string { return m.ColorText }, func(p *palette) *gfx.Color { return &p.text }},
	{"muted", func(m i18n.ThemeMessages) string { return m.ColorMuted }, func(p *palette) *gfx.Color { return &p.muted }},
	{"accent", func(m i18n.ThemeMessages) string { return m.ColorAccent }, func(p *palette) *gfx.Color { return &p.accent }},
	{"border", func(m i18n.ThemeMessages) string { return m.ColorBorder }, func(p *palette) *gfx.Color { return &p.line }},
	{"hover", func(m i18n.ThemeMessages) string { return m.ColorHover }, func(p *palette) *gfx.Color { return &p.surfaceSoft }},
	{"pressed", func(m i18n.ThemeMessages) string { return m.ColorPressed }, func(p *palette) *gfx.Color { return &p.surfaceHot }},
	{"tab", func(m i18n.ThemeMessages) string { return m.ColorTab }, func(p *palette) *gfx.Color { return &p.tab }},
	{"tabHover", func(m i18n.ThemeMessages) string { return m.ColorTabHover }, func(p *palette) *gfx.Color { return &p.tabHover }},
	{"tabActive", func(m i18n.ThemeMessages) string { return m.ColorTabActive }, func(p *palette) *gfx.Color { return &p.tabActive }},
}

const applicationPrimaryColors = 7

func (state *State) resolveApplicationPalette(look appearance) *palette {
	if state.appearanceMode == appearanceCustom {
		return state.applicationTheme.palette()
	}
	return look.palette()
}

func applicationTextAgainst(background [3]uint8) gfx.Color {
	black, white := [3]uint8{}, [3]uint8{255, 255, 255}
	if contrastRatio(background, white) > contrastRatio(background, black) {
		return applicationRGB(white)
	}
	return applicationRGB(black)
}

func (config applicationTheme) palette() *palette {
	colors := maps.Clone(config.BaseColors)
	if colors == nil {
		colors = make(map[string]string)
	}
	for key, value := range config.Colors {
		colors[key] = value
	}
	pal := *config.baseAppearance().palette()
	pal.dark = config.colorAppearance() == appearanceDark
	apply := func(tokens []applicationColorToken) bool {
		overridden := false
		for _, token := range tokens {
			if color, ok := components.ParseColorHex(colors[token.key], false); ok {
				*token.color(&pal) = color
				overridden = true
			}
		}
		return overridden
	}
	if apply(applicationColors[:applicationPrimaryColors]) {
		pal.deriveApplicationColors()
	}
	// 修改基础界面颜色后重新派生标签/悬停色，仅手动高级设置可覆盖派生结果。
	for _, token := range applicationColors[:applicationPrimaryColors] {
		if _, ok := components.ParseColorHex(config.Colors[token.key], false); ok {
			colors = config.Colors
			break
		}
	}
	apply(applicationColors[applicationPrimaryColors:])
	if _, ok := components.ParseColorHex(colors["tabActive"], false); ok {
		pal.tabCloseHot = blendApplicationColor(pal.tabActive, pal.text, 0.10)
	}
	return &pal
}

func (pal *palette) deriveApplicationColors() {
	pal.customApplicationColors = true
	pal.surfaceSoft = blendApplicationColor(pal.surface, pal.text, 0.08)
	pal.surfaceHot = blendApplicationColor(pal.surface, pal.text, 0.18)
	pal.tab = blendApplicationColor(pal.shell, pal.text, 0.05)
	pal.tabHover = blendApplicationColor(pal.shell, pal.text, 0.13)
	pal.tabActive = blendApplicationColor(pal.shell, pal.text, 0.21)
	pal.tabCloseHot = blendApplicationColor(pal.tabActive, pal.text, 0.10)
	pal.faint = blendApplicationColor(pal.workspace, pal.muted, 0.76)
	pal.accentHot = blendApplicationColor(pal.accent, pal.text, 0.22)
	pal.accentSoft = blendApplicationColor(pal.surface, pal.accent, 0.13)
	pal.windowStroke = blendApplicationColor(pal.line, pal.text, 0.10)
	pal.onAccent = applicationTextAgainst(rgb8(pal.accent))
}
