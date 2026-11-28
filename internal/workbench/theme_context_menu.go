package workbench

import (
	"github.com/vivaui/viva/ui"
	"strings"
)

// 操作绑定右键命中的卡片，不改变当前正在使用的终端主题。
func (state *State) declareSchemeContextMenu(c ui.C, card ui.Box, id string) {
	ui.ContextMenu(c, card, func(menu *ui.MenuItems) {
		custom := strings.HasPrefix(id, customSchemePrefix)
		if custom && menu.Item(state.messages.Themes.Edit).Clicked() {
			c.After(func() { state.openThemeEditorFor(id, false, false) })
		}
		if menu.Item(state.messages.Themes.Duplicate).Clicked() {
			c.After(func() { state.openThemeEditorFor(id, true, false) })
		}
		if custom && menu.Item(state.messages.Themes.Delete).Destructive().Clicked() {
			c.After(func() {
				state.openThemeEditorFor(id, false, false)
				state.themeEditor.deleting = true
			})
		}
	})
}
