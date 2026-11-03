package workbench

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
)

func (state *State) chooseCustomEditor() {
	if !customEditorSupported() {
		state.status = state.messages.Tools.FilesCustomEditorUnavailable
		return
	}
	request := app.OpenFileOptions{Title: state.messages.Tools.FilesCustomEditorPicker}
	if pattern := customEditorFilterPattern(); pattern != "" {
		request.Filters = []app.FileFilter{{Name: state.messages.Tools.FilesCustomEditorFilter, Pattern: pattern}}
	}
	paths, err := state.options.OpenFiles(request)
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		state.status = state.messages.Tools.FilesCustomEditorPickerFailed
		return
	}
	if len(paths) != 1 {
		return
	}
	path := strings.TrimSpace(paths[0])
	if !validCustomEditorPath(path) {
		state.status = state.messages.Tools.FilesCustomEditorInvalid
		return
	}
	state.customEditorPath = path
	state.defaultEditor = editorCustom
	state.status = state.messages.Tools.FilesCustomEditorSaved
	state.save()
}

func (state *State) clearCustomEditor() {
	wasCustom := state.defaultEditor == editorCustom
	state.customEditorPath = ""
	if wasCustom {
		state.defaultEditor = editorNone
	}
	state.save()
}

func validCustomEditorPath(value string) bool {
	if value == "" || !filepath.IsAbs(value) ||
		customEditorSupported() && !customEditorPathAllowed(value) {
		return false
	}
	for _, char := range value {
		if char < ' ' || char == '\x7f' {
			return false
		}
	}
	return true
}

func customEditorPathAllowed(value string) bool {
	return customEditorExtension() == "" || strings.EqualFold(filepath.Ext(value), customEditorExtension())
}

func (state *State) openWithConfiguredEditor(path string) error {
	if state.defaultEditor != editorCustom {
		return state.options.OpenPath(path)
	}
	if !customEditorSupported() {
		return errors.New(state.messages.Tools.FilesCustomEditorUnavailable)
	}
	if !validCustomEditorPath(state.customEditorPath) {
		return errors.New(state.messages.Tools.FilesCustomEditorInvalid)
	}
	return state.options.LaunchCustomEditor(state.customEditorPath, path)
}
