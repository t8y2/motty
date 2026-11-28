package workbench

// 自定义外观以当前终端配色为独立快照；已有手动覆盖项优先，不随终端后续变化。
func (state *State) seedApplicationThemeFromTerminal() {
	look := state.terminalSchemeAppearance()
	pal := *look.palette()
	if scheme, ok := state.schemeByID(state.activeScheme()); ok {
		pal = paletteFromScheme(look.palette(), scheme)
		if state.applicationTheme.Opacity == nil {
			state.applicationTheme.Opacity = cloneSchemeOpacity(scheme.opacity)
		}
	}
	theme := &state.applicationTheme
	theme.Base = look.String()
	theme.BaseColors = make(map[string]string, len(applicationColors))
	for _, token := range applicationColors {
		theme.BaseColors[token.key] = applicationColorHex(*token.color(&pal))
	}
	state.applicationThemeUI.opacity.Value = float32(state.applicationOpacity())
}
