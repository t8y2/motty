package workbench

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

// sftpClipboard is the server files a panel copied. A server path cannot name a
// file on the system clipboard, so the panel keeps the rows itself and pastes
// them into its local side. Copying local files clears it: a paste follows the
// copy the user made last, and the system clipboard then holds those files.
type sftpClipboard struct {
	sourceDir string
	entries   []sftpclient.Entry
}

// copySFTPSelection copies the active side's selected rows. Local rows go on
// the system clipboard as files, as a file manager puts them there; server rows
// stay in the view, since the system clipboard cannot name them. It reports
// whether the selection could be copied, and the clipboard's error, if any.
// Search results span folders and are left out until a copy can name them.
func (state *State) copySFTPSelection(view *sftpView) (bool, error) {
	folder := view.active()
	if folder.searching() {
		return false, nil
	}
	var selected []sftpclient.Entry
	for index := range folder.rows {
		if folder.rows[index].selected {
			selected = append(selected, folder.rows[index].entry)
		}
	}
	if len(selected) == 0 {
		return false, nil
	}
	if !view.local {
		view.clipboard = sftpClipboard{sourceDir: folder.path, entries: selected}
		return true, nil
	}
	view.clipboard = sftpClipboard{}
	paths := make([]string, len(selected))
	for index, entry := range selected {
		paths[index] = filepath.Join(folder.path, entry.Name)
	}
	return true, state.options.SetClipboardFiles(paths)
}

// sftpClipboardPaths reads the files the system clipboard holds, as a file
// manager puts them there. The read can wait on another application, so only a
// paste asks for it. The second result reports whether there is anything to
// paste.
func (state *State) sftpClipboardPaths() ([]string, bool) {
	if state.options.ClipboardFiles == nil {
		return nil, false
	}
	paths, err := state.options.ClipboardFiles()
	if err != nil || len(paths) == 0 {
		return nil, false
	}
	return paths, true
}

// pasteSFTPClipboard pastes onto the side the docked panel shows: files the
// system clipboard holds are uploaded to the server's folder, and files this
// panel copied from the server are downloaded to the local folder. It reports
// whether there was anything to paste.
func (state *State) pasteSFTPClipboard(view *sftpView) bool {
	if view.worker == nil || view.busy {
		return false
	}
	folder := view.active()
	// A search result spans folders, and the paste would land in the folder the
	// search started from, out of sight.
	if folder.searching() || folder.path == "" {
		return false
	}
	if view.local {
		clip := view.clipboard
		if len(clip.entries) == 0 {
			return false
		}
		for _, entry := range clip.entries {
			state.queueSFTPDownloadFrom(view, clip.sourceDir, entry, filepath.Join(folder.path, safeLocalName(entry.Name)))
		}
		return true
	}
	paths, ok := state.sftpClipboardPaths()
	if !ok {
		return false
	}
	for _, local := range paths {
		info, err := os.Stat(local)
		if err != nil {
			continue
		}
		state.queueSFTPUploadTo(view, local, info.IsDir(), folder.path)
	}
	return true
}

// sftpClipboardShortcut runs the copy or paste binding on the docked panel's
// file list. The binding keeps its meaning elsewhere while a row has the
// keyboard, and the path and filter fields keep their own text editing.
func (state *State) sftpClipboardShortcut(event gfx.KeyEvent, action shortcutAction) (gfx.EventResult, bool) {
	if action != shortcutCopy && action != shortcutPaste {
		return gfx.EventResult{}, false
	}
	if state.activeSection != sectionTerminal || state.modal != modalNone || !state.filesPanelShown() {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		return gfx.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	// The scopes the rows had before they were a List still name them: the
	// panel's tests and its own focus results that predate the List speak in them.
	if !folder.onListRow(event.Target) && event.Target.Scope() != hitSFTPEntryList {
		return gfx.EventResult{}, false
	}
	if view.dialog != sftpDialogNone || view.bookmarksOpen || view.editPath != "" || folder.headerMenuOpen {
		return gfx.EventResult{}, false
	}
	if event.Repeat {
		return gfx.EventResult{Handled: true}, true
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if action == shortcutCopy {
		if _, err := state.copySFTPSelection(view); err != nil {
			state.showActionError(state.messages.SFTPWindow.Title, err.Error())
		}
		return handled, true
	}
	state.pasteSFTPClipboard(view)
	return handled, true
}

// queueSFTPDownloadFrom queues one server file into localPath. The source folder
// is the one the file was listed in, which a copied selection keeps after its
// panel has moved on.
func (state *State) queueSFTPDownloadFrom(view *sftpView, remoteDir string, entry sftpclient.Entry, localPath string) {
	_, queued := view.worker.QueueDownload(path.Join(remoteDir, entry.Name), localPath, entry.IsDir(), sftpclient.ExistingFail)
	if !queued {
		view.errorText = state.messages.SFTPUnavailable
		return
	}
	view.status = fmt.Sprintf(state.messages.SFTPQueuedFmt, entry.Name)
	view.errorText = ""
	state.transfers.open, state.transfers.filter, state.transfers.scroll = true, 0, 0
	state.transfers.host = nil
	state.refreshSFTPTransfers(view)
}

// pasteClipboard pastes onto the side the transfer window last worked in: files
// the system clipboard holds are uploaded to the server's folder, and files this
// window copied from the server are downloaded to the local folder.
func (w *sftpWindow) pasteClipboard() {
	view := w.view
	if view.local {
		clip := view.clipboard
		target := w.folder(true).path
		if len(clip.entries) == 0 || target == "" {
			return
		}
		w.queueEntries(false, clip.entries, clip.sourceDir, target)
		return
	}
	target := w.folder(false).path
	if target == "" {
		return
	}
	paths, ok := w.owner.sftpClipboardPaths()
	if !ok {
		return
	}
	w.queueLocalPaths(paths, target)
}

// queueLocalPaths queues the local files and folders at paths for upload into
// targetDir, as dropping those paths on that side does.
func (w *sftpWindow) queueLocalPaths(paths []string, targetDir string) {
	for _, location := range paths {
		info, err := os.Lstat(location)
		if err != nil {
			w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
			continue
		}
		w.queueEntries(true, []sftpclient.Entry{{Name: filepath.Base(location), Mode: info.Mode()}}, filepath.Dir(location), targetDir)
	}
}
