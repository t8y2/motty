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

// sftpClipboard is the server files the user copied, from any server. A server
// path cannot name a file on the system clipboard, so the workbench keeps the
// rows itself. open is captured at copy time, so a paste can still name the
// server after its window moved; view is only compared, and read for a worker
// that may still be alive, never used to open the files. Copying local files
// clears it: a paste follows the copy the user made last.
type sftpClipboard struct {
	view      *sftpView
	sourceDir string
	server    string
	address   string
	open      func() (sftpclient.Remote, error)
	entries   []sftpclient.Entry
}

func (clip sftpClipboard) empty() bool { return len(clip.entries) == 0 }

// remoteCopySource is where a batch of server-to-server copies reads from. view
// is nil when the clipboard outlived its window.
type remoteCopySource struct {
	view    *sftpView
	open    func() (sftpclient.Remote, error)
	server  string
	address string
}

func copySourceFromView(view *sftpView) remoteCopySource {
	if view == nil {
		return remoteCopySource{}
	}
	return remoteCopySource{view: view, open: view.openFiles, server: view.transferServer, address: view.transferAddress}
}

func (clip sftpClipboard) copySource() remoteCopySource {
	return remoteCopySource{view: clip.view, open: clip.open, server: clip.server, address: clip.address}
}

// remoteClipboard is the server files the user copied last, shared by every
// window of the process.
func (state *State) sftpRemoteClipboard() sftpClipboard {
	if group := state.options.Group; group != nil {
		return group.remoteClipboard()
	}
	return state.sftpClip
}

func (state *State) setSFTPRemoteClipboard(clip sftpClipboard) {
	if group := state.options.Group; group != nil {
		group.setRemoteClipboard(clip)
		return
	}
	state.sftpClip = clip
	state.invalidateSFTPWindows()
}

// clearSFTPRemoteClipboardFrom drops the clipboard when view copied it, so a paste
// never holds a closed window's files.
func (state *State) clearSFTPRemoteClipboardFrom(view *sftpView) {
	if group := state.options.Group; group != nil {
		group.clearRemoteClipboardFrom(view)
		return
	}
	if state.sftpClip.view == view {
		state.sftpClip = sftpClipboard{}
		state.invalidateSFTPWindows()
	}
}

// copySFTPSelection copies the active side's selected rows. Local rows go on
// the system clipboard as files, as a file manager puts them there; server rows
// stay with the workbench, since the system clipboard cannot name them. It
// reports whether the selection could be copied, and the clipboard's error, if
// any. Search results span folders and are left out until a copy can name them.
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
		state.setSFTPRemoteClipboard(sftpClipboard{
			view: view, sourceDir: folder.path, server: view.transferServer, address: view.transferAddress,
			open: view.openFiles, entries: selected,
		})
		return true, nil
	}
	state.setSFTPRemoteClipboard(sftpClipboard{})
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

// pasteRemoteClipboardToLocal queues the clipboard's server files into a local
// folder, reading from the server they were copied from.
func (state *State) pasteRemoteClipboardToLocal(clip sftpClipboard, localDir string) bool {
	if clip.view == nil || clip.view.worker == nil || localDir == "" {
		return false
	}
	for _, entry := range clip.entries {
		state.queueSFTPDownloadFrom(clip.view, clip.sourceDir, entry, filepath.Join(localDir, safeLocalName(entry.Name)))
	}
	return true
}

// pasteSFTPClipboard pastes onto the side the docked panel shows: server files
// another panel copied become a server-to-server copy into this server's
// folder, and files the system clipboard holds are uploaded. On the local side
// the copied server files are downloaded. It reports whether there was anything
// to paste.
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
	clip := state.sftpRemoteClipboard()
	if view.local {
		return state.pasteRemoteClipboardToLocal(clip, folder.path)
	}
	if !clip.empty() && clip.open != nil {
		if queued, _ := state.queueRemoteCopies(view, clip.copySource(), clip.sourceDir, folder.path, clip.entries); queued > 0 {
			return true
		}
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
	state.transfers.host = nil
	state.openTransferCenterForQueued()
	state.refreshSFTPTransfers(view)
}

// pasteClipboard pastes onto the side the transfer window last worked in: server
// files another window copied become a server-to-server copy, files the system
// clipboard holds are uploaded, and on the local side the copied server files
// are downloaded.
func (w *sftpWindow) pasteClipboard() {
	view := w.view
	clip := w.owner.sftpRemoteClipboard()
	if view.local {
		target := w.folder(true).path
		if clip.empty() || target == "" {
			return
		}
		if w.owner.pasteRemoteClipboardToLocal(clip, target) {
			w.localRefresh = true
		}
		return
	}
	target := w.folder(false).path
	if target == "" {
		return
	}
	if !clip.empty() && clip.open != nil {
		if queued, _ := w.owner.queueRemoteCopies(view, clip.copySource(), clip.sourceDir, target, clip.entries); queued > 0 {
			w.notices.show(w.owner.messages.SFTPWindow.Title,
				fmt.Sprintf(w.owner.messages.SFTPCopiedCountFmt, queued, target), components.ToastSuccess)
			return
		}
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
