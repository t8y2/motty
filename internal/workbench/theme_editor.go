package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

var themeEditorHits = tree.Feature("theme-editor")
var (
	hitThemeMaximize = tree.ID(themeEditorHits, "maximize")
	hitThemeNew      = tree.ID(themeEditorHits, "new")
	hitThemeCopy     = tree.ID(themeEditorHits, "copy")
	hitThemeEdit     = tree.ID(themeEditorHits, "edit")
	hitThemeDelete   = tree.ID(themeEditorHits, "delete")
	hitThemeClose    = tree.ID(themeEditorHits, "close")
	hitThemeSave     = tree.ID(themeEditorHits, "save")
	hitThemeApply    = tree.ID(themeEditorHits, "apply")
	hitThemeReset    = tree.ID(themeEditorHits, "reset")
	hitThemePreview  = tree.ID(themeEditorHits, "preview")
	hitThemeName     = tree.ID(themeEditorHits, "name")
	hitThemeOpacity  = tree.ID(themeEditorHits, "opacity")
	hitThemeColors   = tree.Sub(themeEditorHits, tree.Name("colors"))
)

type themeEditorState struct {
	draft, original                 colorScheme
	name                            ui.Field
	colors                          [21]gfx.Color
	pickers                         [21]ui.ColorPickerState
	colorAnchors                    [21]gfx.Rect
	liveColorHex                    ui.Field
	liveColorInput                  ui.Handlers
	liveColorLayout                 components.ColorPickerLayout
	colorIndex                      int
	hex, red, green, blue           ui.Field
	opacity                         ui.SliderState
	expanded, previewOnly, deleting bool
	maximized, openPicker           bool
	error                           string
	colorInvalid                    bool
	preview                         *terminal
	previewVersion                  uint16
}

func (state *State) themeText(en, zh, tw string) string {
	switch state.language {
	case i18n.Chinese:
		return zh
	case i18n.TraditionalChinese:
		return tw
	default:
		return en
	}
}

func (state *State) resolvedScheme(id string) colorScheme {
	if s, ok := state.schemeByID(id); ok {
		copy := *s
		copy.opacity = cloneSchemeOpacity(s.opacity)
		return copy
	}
	p := state.appearance.palette()
	return colorScheme{name: state.messages.Tools.SchemeDefault, background: rgb8(p.terminal),
		foreground: rgb8(p.termFG), cursor: rgb8(p.cursor), ansi: p.ansi}
}

func (state *State) openThemeEditor(copyTheme, newTheme bool) {
	state.openThemeEditorFor(state.activeScheme(), copyTheme, newTheme)
}

func (state *State) openThemeEditorFor(id string, copyTheme, newTheme bool) {
	s := state.resolvedScheme(id)
	if newTheme {
		s = state.resolvedScheme(defaultSchemeID)
	}
	if copyTheme || newTheme || !strings.HasPrefix(s.id, customSchemePrefix) {
		s.id = ""
		base := s.name + state.themeText(" copy", " 副本", " 副本")
		if newTheme {
			base = state.themeText("My theme", "我的主题", "我的主題")
		}
		s.name = state.uniqueThemeName(base)
	}
	state.themeEditor = themeEditorState{draft: s, original: s}
	state.themeEditor.loadDraft()
	state.schemeBrowserOpen = false
	state.modal = modalThemeEditor
}

func (state *State) uniqueThemeName(base string) string {
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s %d", base, n)
		}
		exists := false
		for _, s := range state.managedSchemes {
			exists = exists || strings.EqualFold(s.name, name)
		}
		if !exists {
			return name
		}
	}
}
