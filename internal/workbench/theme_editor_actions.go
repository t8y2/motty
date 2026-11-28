package workbench

import (
	"github.com/vivaui/viva/gfx"
	"strings"
)

func (state *State) clickThemeManagement(id gfx.ID) bool {
	switch id {
	case hitThemeNew:
		state.openThemeEditor(false, true)
	case hitThemeCopy:
		state.openThemeEditor(true, false)
	case hitThemeEdit:
		if strings.HasPrefix(state.activeScheme(), customSchemePrefix) {
			state.openThemeEditor(false, false)
		}
	case hitThemeDelete:
		if strings.HasPrefix(state.activeScheme(), customSchemePrefix) {
			state.openThemeEditor(false, false)
			state.themeEditor.deleting = true
		}
	default:
		return false
	}
	return true
}

func (state *State) clickThemeEditor(id gfx.ID) {
	e := &state.themeEditor
	switch id {
	case hitThemeClose:
		state.closeModal()
	case hitThemeReset:
		e.draft = e.original
		e.loadDraft()
	case hitThemeMaximize:
		e.maximized = !e.maximized
	case hitThemePreview:
		e.expanded = !e.expanded
	case hitThemeSave, hitThemeApply:
		if e.deleting {
			if err := state.deleteCustomScheme(e.draft.id); err != nil {
				e.error = err.Error()
				return
			}
			state.closeModal()
			return
		}
		if e.colorInvalid {
			return
		}
		before := state.savedCustomSchemes()
		schemes := state.terminalSchemes
		application := state.applicationTheme
		schemeID, err := state.saveCustomScheme(e.collectDraft())
		if err != nil {
			e.error = err.Error()
			return
		}
		if id == hitThemeApply {
			state.setTerminalScheme(schemeID)
		}
		state.refreshApplicationTheme()
		if err := state.saveWithResultModeRecovery(true, false, true); err != nil {
			state.restoreAfterThemeSave(before, schemes, application, err)
			e.error = err.Error()
			return
		}
		state.closeModal()
	}
}
