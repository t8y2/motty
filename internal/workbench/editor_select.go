package workbench

import (
	"fmt"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var (
	hitSettingsEditor        = tree.ID(settingsHits, "settings-editor")
	hitSettingsEditorDismiss = tree.ID(settingsHits, "settings-editor-dismiss")
	hitSettingsEditorItems   = tree.Sub(settingsHits, tree.Name("settings-editor-item"))
)

// editorSelect is the setting that picks the editor a folder is handed to.
type editorSelect struct {
	// choices is the system default followed by every editor this build knows.
	// Known editors stay visible so a URL scheme that registers on first launch
	// does not make the editor appear unsupported.
	choices []ExternalEditor
	loaded  bool
	open    bool
	bounds  gfx.Rect
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
	if customEditorSupported() || state.defaultEditor == editorCustom {
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
	m := state.messages.Tools
	page.add(settingsControl{kind: controlEditorSelect, slot: page.rowFrame(m.FilesEditor, state.settingsEditorDescription())})
}

// Paint the select in the menu pass, outside the settings page's clip.
func (state *State) paintEditorSelect(canvas *gfx.Canvas) {
	menu, m := &state.editorMenu, state.messages.Tools
	bounds := menu.bounds
	if !menu.loaded && bounds.Width > 0 {
		state.refreshEditors()
	}
	if state.modal != modalNone || bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		menu.open = false
		return
	}
	selected := state.editorSelected()
	value, hint := string(state.defaultEditor), ""
	if selected >= 0 {
		value = state.editorChoiceName(menu.choices[selected])
		if !state.editorInstalled(state.defaultEditor) {
			hint = m.FilesEditorNotFound
		}
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(menu.choices)*32), max(above, below))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	opening := menu.open && state.popups.owner != hitSettingsEditorDismiss
	scroll := state.selectScroll(menu.open, hitSettingsEditorDismiss)
	if opening && selected >= 0 {
		scroll.Offset = max(0, min(float32(selected)*32, float32(len(menu.choices)*32)+8-height))
		canvas.SetInitialFocus(hitSettingsEditorItems.At(selected))
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsEditor, Label: m.FilesEditor, Value: value, Hint: hint,
		Open: menu.open, Content: content, Layer: popupLayer,
		Scope: hitSettingsEditorDismiss, DismissID: hitSettingsEditorDismiss, Viewport: state.windowRect(), Scroll: scroll,
	})
	defer control.End()
	if menu.open {
		state.menuPlaced(control.ContentBounds())
		for index, editor := range menu.choices {
			control.AddItem(components.SelectItem{ID: hitSettingsEditorItems.At(index), Label: state.editorChoiceName(editor), Selected: index == selected})
		}
	}
}

func (state *State) clickEditorSelect(id gfx.ID) bool {
	if state.settingsCategory != settingsFiles || state.settingsSearching() {
		return false
	}
	menu := &state.editorMenu
	switch id {
	case hitSettingsEditor:
		if !menu.open {
			state.refreshEditors()
		}
		menu.open = !menu.open
		return true
	case hitSettingsEditorDismiss:
		menu.open = false
		return true
	}
	index, ok := id.In(hitSettingsEditorItems)
	if !ok || index < 0 || index >= len(menu.choices) {
		return false
	}
	editor := menu.choices[index]
	menu.open = false
	if editor == editorCustom && state.defaultEditor != editorCustom {
		previousEditor, previousPath := state.defaultEditor, state.customEditorPath
		state.chooseCustomEditor()
		if state.defaultEditor != editorCustom {
			state.defaultEditor, state.customEditorPath = previousEditor, previousPath
		}
		return true
	}
	if state.defaultEditor != editor {
		state.defaultEditor = editor
		state.save()
	}
	return true
}
