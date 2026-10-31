package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
)

// sftpEditorItem is the more-menu item that opens one folder in the chosen
// editor: its label, and whether it is disabled.
func (state *State) sftpEditorItem(view *sftpView, folder *sftpFolder) (string, bool) {
	m := state.messages.Tools
	name := state.defaultEditor.name(m)
	if name == "" {
		return "", false
	}
	disabled := folder.path == "" || view.editorResults != nil
	if folder != &view.lfolder {
		session, ok := state.sftpSession(view)
		disabled = disabled || !ok || !session.hasServer()
	}
	return fmt.Sprintf(m.FilesEditorOpenFmt, name), disabled
}

// editorRequestFor copies what the request needs out of the window.
func (state *State) editorRequestFor(view *sftpView, folder *sftpFolder, session *Session) editorRequest {
	request := editorRequest{
		editor:    state.defaultEditor,
		directory: folder.path,
		portFmt:   state.messages.Tools.FilesEditorPortFmt,
	}
	if folder == &view.lfolder {
		return request
	}
	host := session.host
	request.host = editorHost{address: host.Address, user: state.hostCatalog.login(host), port: host.Port}
	request.alias = state.sshAlias(host)
	return request
}

// editorResult is one answer from the background.
type editorResult struct {
	editor    ExternalEditor
	directory string
	err       error
}

// openSFTPInEditor hands the folder the panel shows to the chosen editor, in the
// background, so a slow opener does not hold a paint.
func (state *State) openSFTPInEditor(view *sftpView) {
	folder := view.active()
	label, disabled := state.sftpEditorItem(view, folder)
	session, ok := state.sftpSession(view)
	if label == "" || disabled || !ok {
		return
	}
	request := state.editorRequestFor(view, folder, session)
	open := state.options.OpenExternalURI
	results, notify := make(chan editorResult, 1), state.notifyForView(session, view)
	view.editorResults = results
	go func() {
		results <- editorResult{editor: request.editor, directory: request.directory, err: request.open(open)}
		notify()
	}()
}

// pumpSFTPEditor takes the background's answer, if it has arrived.
func (state *State) pumpSFTPEditor(session *Session, view *sftpView) {
	if view.editorResults == nil {
		return
	}
	select {
	case result := <-view.editorResults:
		view.editorResults = nil
		if result.err != nil && session.files == view {
			m := state.messages.Tools
			title := fmt.Sprintf(m.FilesEditorOpenFailedTitleFmt, result.editor.name(m))
			state.fileOpenPathReason(view.detached, title, m.FilesEditorOpenFailedFmt, result.directory,
				firstLines(result.err.Error(), 3), components.ToastError)
		}
	default:
	}
}
