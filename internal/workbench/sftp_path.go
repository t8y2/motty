package workbench

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var hitSFTPPath = tree.ID(sftpHits, "sftp-path")

// Each side keeps its own draft. A refreshed listing of the same folder must
// not replace text the user is still editing.
type sftpPathField struct {
	path   string
	editor text.Editor
}

func (folder *sftpFolder) pathInput() *text.Editor {
	if folder.pathField.path != folder.path {
		folder.resetPathInput()
	}
	return &folder.pathField.editor
}

func (folder *sftpFolder) resetPathInput() {
	folder.pathField.path = folder.path
	folder.pathField.editor = *text.NewEditor(folder.path)
}

func (state *State) paintSFTPPathInput(canvas *ui.Canvas, bounds ui.Rect, view *sftpView) {
	value := view.active().pathInput().String()
	focused := canvas.Focused() == hitSFTPPath
	disabled := !view.local && view.busy
	m := state.messages.Tools
	components.PaintInput(canvas, state.Theme(), bounds, components.InputSnapshot{
		ID: hitSFTPPath, Label: m.FilesPathLabel, Value: value,
		Placeholder: m.FilesPathPlaceholder, Hint: m.FilesPathPlaceholder,
		Focused: focused, PlatformEditing: focused && !disabled && value != "",
		Disabled: disabled,
	})
}

func (state *State) sftpPathKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Target != hitSFTPPath || !state.sftpPathInput || state.activeSection != sectionTerminal || state.modal != modalNone {
		return ui.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || session.files.dialog != sftpDialogNone || session.files.editPath != "" {
		return ui.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	editor := folder.pathInput()
	_, composing := editor.Composition()
	switch event.Key {
	case ui.KeyEnter:
		if event.Modifiers == 0 && !event.Repeat && !composing && (view.local || !view.busy) {
			state.submitSFTPPath(view)
		}
	case ui.KeyEscape:
		if !event.Repeat && !composing {
			folder.resetPathInput()
		}
	default:
		return ui.EventResult{}, false
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) submitSFTPPath(view *sftpView) {
	folder := view.active()
	destination := folder.pathInput().String()
	view.errorText = ""
	if destination == "" {
		state.fileNotices.show(state.messages.Tools.FilesPathErrorTitle, state.messages.Tools.FilesPathRequired, components.ToastError)
		return
	}
	view.bookmarksOpen = false
	if view.local {
		if !filepath.IsAbs(destination) {
			destination = filepath.Join(folder.path, destination)
		}
		destination = filepath.Clean(destination)
		if destination == folder.path {
			folder.resetPathInput()
		}
		if err := state.listLocal(view, destination); err != nil {
			view.errorText = ""
			state.showSFTPPathError(destination, err)
		}
		return
	}
	if !path.IsAbs(destination) {
		destination = path.Join(folder.path, destination)
	}
	destination = path.Clean(destination)
	if view.worker == nil {
		state.showSFTPPathError(destination, errors.New(state.messages.SFTPUnavailable))
		return
	}
	if destination == folder.path {
		folder.resetPathInput()
	}
	// Manual entry validates the directory on the server, even if an older
	// listing is cached. A removed cached folder must not appear to open.
	view.pathRequest = destination
	state.listSFTP(view, destination)
	if !view.busy {
		view.pathRequest = ""
		state.showSFTPPathError(destination, errors.New(state.messages.SFTPUnavailable))
	}
}

func (state *State) showSFTPPathError(destination string, err error) {
	m := state.messages.Tools
	detail := fmt.Sprintf(m.FilesPathFailedFmt, destination, err)
	var status *sftp.StatusError
	if errors.Is(err, os.ErrNotExist) || errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxNoSuchFile {
		detail = fmt.Sprintf(m.FilesPathNotFoundFmt, destination)
	}
	state.fileNotices.show(m.FilesPathErrorTitle, detail, components.ToastError)
}
