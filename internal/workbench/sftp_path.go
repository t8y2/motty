package workbench

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

var (
	hitSFTPPath     = tree.ID(sftpHits, "sftp-path")
	hitSFTPPathEdit = tree.ID(sftpHits, "sftp-path-edit")
)

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

func (state *State) sftpPathVisible(folder *sftpFolder) bool {
	return state.sftpPathInput || folder.pathEditing
}

func (state *State) startSFTPPathEditing(view *sftpView) gfx.EventResult {
	if view == nil || !view.local && view.busy {
		return gfx.EventResult{}
	}
	folder := view.active()
	if !folder.pathEditing {
		folder.resetPathInput()
	}
	folder.pathEditing = true
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitSFTPPath, FocusVisible: true}
}

func (state *State) sftpPathShortcut(target gfx.ID) (gfx.EventResult, bool) {
	session, local, ok := state.decodePaneHit(target)
	if !ok {
		session = state.terminalSession(target)
		local = target
		ok = session != nil
	}
	if !ok || session.files == nil || session.ID != state.activeSessionID || state.activeSection != sectionTerminal || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	if local != hitSFTPPath && local != hitSFTPPathEdit && local != hitSFTPFilter &&
		local.Scope() != hitSFTPEntryList && local.Scope() != hitSFTPBreadcrumbList {
		return gfx.EventResult{}, false
	}
	context, ok := state.deck.Context(state.deck.ActiveWorkspace())
	if !ok || !context.Open || context.Kind != layoutcore.ContextSFTP || session.files.editPath != "" || session.files.dialog != sftpDialogNone || !session.files.local && session.files.busy {
		return gfx.EventResult{}, false
	}
	result := state.startSFTPPathEditing(session.files)
	if !result.Handled {
		return gfx.EventResult{}, false
	}
	result.Focus = tree.MountID(contextMount, result.Focus)
	return result, true
}

func (state *State) paintSFTPPathInput(canvas *gfx.Canvas, bounds gfx.Rect, view *sftpView) {
	value := view.active().pathInput().String()
	focused := canvas.Focused() == hitSFTPPath
	readOnly := !view.local && view.busy
	m := state.messages.Tools
	components.PaintInput(canvas, state.Theme(), bounds, components.InputSnapshot{
		ID: hitSFTPPath, Label: m.FilesPathLabel, Value: value,
		Placeholder: m.FilesPathPlaceholder, Hint: m.FilesPathPlaceholder,
		Focused: focused, PlatformEditing: focused && !readOnly && value != "",
		// A pending request must not turn server latency into focus loss.
		ReadOnly: readOnly,
	})
}

func (state *State) sftpPathKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Target != hitSFTPPath || state.activeSection != sectionTerminal || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || session.files.dialog != sftpDialogNone || session.files.editPath != "" {
		return gfx.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	if !state.sftpPathVisible(folder) {
		return gfx.EventResult{}, false
	}
	editor := folder.pathInput()
	_, composing := editor.Composition()
	switch event.Key {
	case gfx.KeyEnter:
		if event.Modifiers == 0 && !event.Repeat && !composing && (view.local || !view.busy) {
			state.submitSFTPPath(view)
		}
	case gfx.KeyEscape:
		if !event.Repeat && !composing {
			if view.local && folder.navigation.id != 0 {
				state.cancelSFTPLocalNavigation(view)
			}
			if view.busy && view.pathRequest != "" {
				// Escape cancels the navigation it started, not just the text.
				view.abandonedPath = view.pathRequest
				state.cancelSFTPNavigation(view)
			}
			folder.resetPathInput()
			if !state.sftpPathInput {
				folder.pathEditing = false
			}
		}
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) submitSFTPPath(view *sftpView) {
	folder := view.active()
	destination := folder.pathInput().String()
	view.errorText = ""
	if destination == "" {
		state.fileOpenNotice(view.detached, state.messages.Tools.FilesPathErrorTitle, state.messages.Tools.FilesPathRequired, components.ToastError)
		return
	}
	view.bookmarksOpen = false
	if view.detached != nil {
		if view.local {
			if !filepath.IsAbs(destination) {
				destination = filepath.Join(folder.path, destination)
			}
		} else if !path.IsAbs(destination) {
			destination = path.Join(folder.path, destination)
		}
		if !view.local && !view.detached.navigationBusy() {
			view.pathRequest = path.Clean(destination)
		}
		view.detached.navigate(view.local, destination)
		return
	}
	if view.local {
		if !filepath.IsAbs(destination) {
			destination = filepath.Join(folder.path, destination)
		}
		destination = filepath.Clean(destination)
		if view.detached != nil {
			view.detached.pendingLocal = destination
		}
		if destination == folder.path {
			folder.resetPathInput()
		}
		if err := state.navigateLocalDirectory(view, destination); err != nil {
			if view.detached != nil {
				view.detached.pendingLocal = ""
			}
			view.errorText = ""
			state.showSFTPPathErrorAt(view, destination, err)
			return
		}
		if !state.sftpPathInput {
			folder.pathEditing = false
		}
		return
	}
	if !path.IsAbs(destination) {
		destination = path.Join(folder.path, destination)
	}
	destination = path.Clean(destination)
	if view.worker == nil {
		state.showSFTPPathErrorAt(view, destination, errors.New(state.messages.SFTPUnavailable))
		return
	}
	if destination == folder.path {
		folder.resetPathInput()
	}
	// Manual entry validates the directory on the server, even if an older
	// listing is cached. A removed cached folder must not appear to open.
	view.pathRequest = destination
	nav := state.beginSFTPNavigation(view, false, destination, sftpNavigationManual, 0)
	state.requestTerminalDirectory(view, destination)
	state.queueSFTPListing(view, destination, nav)
	if !view.busy {
		view.pathRequest = ""
		view.terminalPath = ""
		state.showSFTPPathErrorAt(view, destination, errors.New(state.messages.SFTPUnavailable))
	}
}

func (state *State) showSFTPPathError(destination string, err error) {
	state.showSFTPPathErrorAt(nil, destination, err)
}

func (state *State) showSFTPPathErrorAt(view *sftpView, destination string, err error) {
	m := state.messages.Tools
	detail := fmt.Sprintf(m.FilesPathFailedFmt, destination, err)
	var status *sftp.StatusError
	if errors.Is(err, os.ErrNotExist) || errors.As(err, &status) && status.FxCode() == sftp.ErrSSHFxNoSuchFile {
		detail = fmt.Sprintf(m.FilesPathNotFoundFmt, destination)
	}
	var origin *sftpWindow
	if view != nil {
		origin = view.detached
	}
	state.fileOpenNotice(origin, m.FilesPathErrorTitle, detail, components.ToastError)
}
