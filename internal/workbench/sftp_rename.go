package workbench

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var hitSFTPInlineRename = tree.ID(sftpWindowHits, "inline-rename")

// The draft identifies a file by directory and name. Sorting or a background
// refresh can change the index of the row without changing what Enter renames.
type sftpInlineRename struct {
	directory, original string
	editor              text.Editor
	field               ui.Field
	row                 gfx.ID
	focus               bool
}

func (state *State) beginSFTPRename(view *sftpView) bool {
	if view.rename != nil || view.local || view.worker == nil || view.busy || view.path == "" || view.searching() ||
		view.dialog != sftpDialogNone || view.editPath != "" {
		return false
	}
	if w := view.detached; w != nil {
		if w.closed || !w.connected() || w.navigationBusy() {
			return false
		}
		w.cancelDrag()
	}
	view.cancelPanelDrag()
	entry, ok := state.selectedSFTPEntry(view)
	if !ok {
		return false
	}
	edit := &sftpInlineRename{directory: view.path, original: entry.Name, focus: true}
	edit.editor.Insert(entry.Name)
	stem := entry.Name
	if !entry.IsDir() {
		if dot := strings.LastIndexByte(stem, '.'); dot > 0 && dot < len(stem)-1 {
			stem = stem[:dot]
		}
	}
	edit.editor.SetSelection(text.Range{End: utf8.RuneCountInString(stem)})
	edit.field.Bind(&edit.editor)
	view.rename = edit
	view.list.ScrollIntoView(sftpRowNameIndex(view.rows, entry.Name))
	return true
}

func (view *sftpView) reconcileSFTPRename(canvas *gfx.Canvas, visible bool) {
	edit := view.rename
	if edit != nil && (!visible || view.worker == nil || view.busy || view.dialog != sftpDialogNone ||
		edit.directory != view.path || view.searching() || (!edit.focus && canvas.Focused() != hitSFTPInlineRename) ||
		sftpRowNameIndex(view.rows, edit.original) < 0) {
		view.rename = nil
	}
}

func (state *State) declareSFTPRename(c ui.C, view *sftpView, row gfx.ID, entry sftpclient.Entry) bool {
	edit := view.rename
	if edit == nil || edit.original != entry.Name {
		return false
	}
	edit.row = row
	field := ui.TextFieldID(c, hitSFTPInlineRename, &edit.field, state.messages.SFTPNewName, "").
		Grow(1).Height(26).OnKey(func(event gfx.KeyEvent) gfx.EventResult {
		return state.sftpRenameKey(view, event)
	})
	if field.Changed() {
		edit.field.Invalid = false
	}
	if edit.focus {
		c.Canvas().RequestFocusOnce(hitSFTPInlineRename)
		edit.focus = false
	}
	return true
}

func (state *State) sftpRenameKey(view *sftpView, event gfx.KeyEvent) gfx.EventResult {
	edit := view.rename
	if edit == nil || event.Target != hitSFTPInlineRename || event.Release || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	if event.Key != gfx.KeyEnter && event.Key != gfx.KeyEscape {
		return gfx.EventResult{}
	}
	result := gfx.EventResult{Handled: true, Repaint: true}
	if _, composing := edit.editor.Composition(); composing || event.Repeat {
		return result
	}
	if event.Key == gfx.KeyEnter && !state.submitSFTPRename(view, edit) {
		result.Focus = hitSFTPInlineRename
		return result
	}
	view.rename = nil
	result.Focus, result.FocusVisible = edit.row, true
	return result
}

// submitSFTPRename reports whether the field can close. Validation and a busy
// queue leave the draft available for correction. Only a server result can
// announce a successful rename.
func (state *State) submitSFTPRename(view *sftpView, edit *sftpInlineRename) bool {
	name := edit.editor.String()
	if name == edit.original || view.path != edit.directory || sftpRowNameIndex(view.rows, edit.original) < 0 {
		return true
	}
	fail := func(detail string) bool {
		edit.field.Invalid = true
		state.fileOpenNotice(view.detached, state.messages.SFTPRename, detail, components.ToastError)
		return false
	}
	if strings.TrimSpace(name) == "" || !validRemoteBaseName(name) || strings.ContainsAny(name, "\r\n") {
		return fail(state.messages.SFTPErrRenameInvalid)
	}
	if sftpRowNameIndex(view.allRows, name) >= 0 {
		return fail(state.messages.SFTPErrDirectoryExists)
	}
	if view.worker == nil || view.busy || !view.worker.Rename(path.Join(edit.directory, edit.original), path.Join(edit.directory, name)) {
		return fail(state.messages.SFTPUnavailable)
	}
	view.busy, view.operation = true, sftpclient.OperationRename
	view.moveAfterRename = false
	view.status = fmt.Sprintf(state.messages.SFTPRenamingFmt, edit.original)
	view.errorText = ""
	return true
}
