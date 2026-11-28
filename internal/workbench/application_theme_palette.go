package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"maps"
)

type applicationColorToken struct {
	key, en, zh, tw string
	color           func(*palette) *gfx.Color
}

var applicationColors = [...]applicationColorToken{
	{"shell", "Title bar and sidebar", "标题栏和侧栏", "標題列和側欄", func(p *palette) *gfx.Color { return &p.shell }},
	{"workspace", "Workspace", "工作区", "工作區", func(p *palette) *gfx.Color { return &p.workspace }},
	{"surface", "Panels and controls", "面板和控件", "面板和控制項", func(p *palette) *gfx.Color { return &p.surface }},
	{"text", "Text", "正文", "正文", func(p *palette) *gfx.Color { return &p.text }},
	{"muted", "Secondary text", "次要文字", "次要文字", func(p *palette) *gfx.Color { return &p.muted }},
	{"accent", "Accent", "强调色", "強調色", func(p *palette) *gfx.Color { return &p.accent }},
	{"border", "Border", "边框", "邊框", func(p *palette) *gfx.Color { return &p.line }},
	{"hover", "Hover", "悬停", "懸停", func(p *palette) *gfx.Color { return &p.surfaceSoft }},
	{"pressed", "Pressed", "按下", "按下", func(p *palette) *gfx.Color { return &p.surfaceHot }},
	{"tab", "Tab", "标签页", "分頁", func(p *palette) *gfx.Color { return &p.tab }},
	{"tabHover", "Hovered tab", "悬停标签页", "懸停分頁", func(p *palette) *gfx.Color { return &p.tabHover }},
	{"tabActive", "Active tab", "活动标签页", "使用中分頁", func(p *palette) *gfx.Color { return &p.tabActive }},
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

func readableApplicationColor(background, color, fallback gfx.Color, ratio float32) gfx.Color {
	for _, fraction := range [...]float32{0, 0.25, 0.5, 0.75} {
		candidate := blendApplicationColor(color, fallback, fraction)
		if contrastRatio(rgb8(background), rgb8(candidate)) >= ratio {
			return candidate
		}
	}
	return fallback
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
