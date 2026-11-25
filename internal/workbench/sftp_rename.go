package workbench

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
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
	input               app.TextInput
	row                 gfx.ID
	focus               bool
	scroll              float32
	rejectedName        string
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
	edit.input.Editor = &edit.editor
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
	ui.Slot(c).Grow(1).Height(26).Draw(func(d *ui.DrawContext) {
		edit.paint(d, state.messages.SFTPNewName)
	})
	if edit.focus {
		c.Canvas().RequestFocusOnce(hitSFTPInlineRename)
		edit.focus = false
	}
	return true
}

const sftpRenameInset = float32(4)

func (edit *sftpInlineRename) paint(d *ui.DrawContext, label string) {
	canvas, bounds := d.Canvas, d.Rect
	if edit.field.Invalid && edit.editor.String() != edit.rejectedName {
		edit.field.Invalid = false
	}
	snapshot := edit.field.Snapshot(d.Frame, hitSFTPInlineRename, label, "")
	look := components.TextEntryRecipe(*d.Theme, gfx.State{
		Focused: snapshot.Focused, Invalid: snapshot.Invalid, Hovered: canvas.Hovered() == snapshot.ID,
	})
	canvas.RoundedBorder(bounds, look.Radius, look.BorderWidth, look.Border, look.Fill)
	viewport := bounds.Inset(gfx.Insets{Left: sftpRenameInset, Right: sftpRenameInset})
	if viewport.Width <= 0 {
		return
	}

	// Keep the full shaped line at a shifted origin. Its clipped text and the
	// text bounds reported to the platform then share the same mouse geometry.
	style := snapshot.Text.TextStyle(d.Theme)
	line := viewport
	line.Width = max(viewport.Width, canvas.TextWidthStyle(snapshot.Value, style)+1)
	caret := canvas.LabelOrigin(snapshot.Value, line, style, gfx.AlignStart) - line.X +
		canvas.TextCaretStyle(snapshot.Value, style, snapshot.Caret)
	edit.scroll = min(edit.scroll, caret)
	edit.scroll = max(edit.scroll, caret-viewport.Width+1)
	edit.scroll = min(max(edit.scroll, 0), line.Width-viewport.Width)
	line.X -= edit.scroll
	canvas.Clip(viewport)
	components.PaintInputContent(canvas, *d.Theme, bounds, line, snapshot)
	canvas.Unclip()
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
		edit.rejectedName = name
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
