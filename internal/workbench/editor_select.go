package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/ui"
)

// editorSelect is the setting that picks the editor a folder is handed to.
type editorSelect struct {
	// choices is the system default followed by every editor this build knows.
	// Known editors stay visible so a URL scheme that registers on first launch
	// does not make the editor appear unsupported.
	choices []ExternalEditor
	loaded  bool
}

// refreshEditors lists every editor this build knows. Some applications, such as
// VS Code on Windows, register their URL scheme only after they have started;
// hiding them until then makes a supported editor look unavailable.
func (state *State) refreshEditors() {
	menu := &state.editorMenu
	menu.choices, menu.loaded = []ExternalEditor{editorNone}, true
	for _, info := range editors {
		menu.choices = append(menu.choices, info.id)
	}
	if customEditorChoice() || state.defaultEditor == editorCustom {
		menu.choices = append(menu.choices, editorCustom)
	}
}

func (state *State) editorSelected() int {
	if _, known := editorByID(state.defaultEditor); !known && state.defaultEditor != editorNone && state.defaultEditor != editorCustom {
		return -1
	}
	return slices.Index(state.editorMenu.choices, state.defaultEditor)
}

// editorChoiceName is how the select calls a choice.
func (state *State) editorChoiceName(editor ExternalEditor) string {
	if editor == editorNone {
		return state.messages.Tools.FilesEditorNone
	}
	return editor.name(state.messages.Tools)
}

// editorInstalled is false for a saved editor that the system has no application
// for.
func (state *State) editorInstalled(editor ExternalEditor) bool {
	info, known := editorByID(editor)
	if !known {
		return true
	}
	available, err := state.options.SchemeHandlerAvailable(info.scheme)
	return available || err != nil
}

func (state *State) settingsEditorDescription() string {
	if state.defaultEditor == editorCustom {
		return state.messages.Tools.FilesEditorCustomDescription
	}
	if _, known := editorByID(state.defaultEditor); known || state.defaultEditor == editorNone {
		return state.messages.Tools.FilesEditorDescription
	}
	return fmt.Sprintf(state.messages.Tools.FilesEditorUnknownFmt, state.defaultEditor)
}

func (state *State) paintEditorSetting(page *settingsPage) {
	m, menu := state.messages.Tools, &state.editorMenu
	if !menu.loaded {
		state.refreshEditors()
	}
	previous, previousPath := state.defaultEditor, state.customEditorPath
	hint := ""
	if state.editorSelected() >= 0 && !state.editorInstalled(state.defaultEditor) {
		hint = m.FilesEditorNotFound
	}
	settingsSelectRow(page, m.FilesEditor, state.settingsEditorDescription(), &state.defaultEditor, menu.choices, state.editorChoiceName,
		func(selector ui.Box) ui.Box {
			if state.editorSelected() < 0 {
				selector = selector.Placeholder(string(state.defaultEditor))
			}
			return selector.Hint(hint)
		},
		func() {
			// The custom editor asks for a program first; without one the choice is undone.
			if state.defaultEditor == editorCustom && previous != editorCustom {
				state.chooseCustomEditor()
				if state.defaultEditor != editorCustom {
					state.defaultEditor, state.customEditorPath = previous, previousPath
				}
				return
			}
			if state.defaultEditor != previous {
				state.save()
			}
		})
}
