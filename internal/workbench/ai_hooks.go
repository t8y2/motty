package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// The assistant's panel (ai_panel.go) and its settings section
// (ai_settings.go) join the window's event handling here, and only here: the
// functions below are called from Click, Pointer, Scroll, Key and EditorFor
// before the rest of the window looks at the event.

func (state *State) aiClick(id gfx.ID) bool {
	return state.aiPanelClick(id) || state.aiSettingsClick(id)
}

func (state *State) aiPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if result, handled := state.aiPanelPointer(event); handled {
		return result, true
	}
	return state.aiSettingsPointer(event)
}

func (state *State) aiScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if result, handled := state.aiPanelScroll(event); handled {
		return result, true
	}
	return state.aiSettingsScroll(event)
}

func (state *State) aiKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if result, handled := state.aiPanelKey(event); handled {
		return result, true
	}
	return state.aiSettingsKey(event)
}

func (state *State) aiEditor(hit gfx.ID) *text.Editor {
	if editor := state.aiPanelEditor(hit); editor != nil {
		return editor
	}
	return state.aiSettingsEditor(hit)
}

// aiSettingsChanged tells the application an assistant setting was written, so
// it can start or stop what depends on it (the MCP server).
func (state *State) aiSettingsChanged() {
	if state.options.AISettingsChanged != nil {
		state.options.AISettingsChanged()
	}
}
