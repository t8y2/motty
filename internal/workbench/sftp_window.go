package workbench

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// A transfer window fixes its SSH source at creation, never at the active tab.
// The terminal entry borrows that connection; the host-menu entry owns its
// connection or authenticated share. Both own their browser and SFTP worker.
type sftpWindow struct {
	owner                       *State
	session                     Session
	window                      *app.ContentWindow
	view                        *sftpView
	closed, closeConfirm        bool
	allowClose, afterClosed     func()
	activeLocal                 bool
	localRefresh                bool
	pendingLocal                string
	phase                       sshconn.Phase
	language                    i18n.Language
	pal                         *palette
	shownPaths                  [2]string
	input                       app.TextInput
	notices                     fileNotices
	ui                          sftpWindowUI
	drag                        sftpWindowDrag
	deferredListings            [2]func()
	externalConflict            uint32
	ownsConnection              bool
	initialLocal, initialRemote string
}

func (state *State) openSFTPWindow(source *Session) {
	if source == nil || source.conn == nil || !source.hasServer() || source.phase != sshconn.PhaseConnected || state.options.OpenContentWindow == nil {
		return
	}
	if existing := state.sftpWindows[source.ID]; existing != nil && !existing.closed {
		if existing.window != nil {
			existing.window.Show()
		}
		return
	}
	w := state.newSFTPWindow(source)
	w.show()
}

func (state *State) newSFTPWindow(source *Session) *sftpWindow {
	w := &sftpWindow{owner: state, activeLocal: true, phase: source.phase, language: state.language, pal: state.pal}
	w.view = &sftpView{detached: w, busy: true, status: state.messages.SFTPLoading, operation: sftpclient.OperationList}
	w.view.transferServer, w.view.transferAddress = source.Title, source.Address
	w.view.selected, w.view.lfolder.selected = -1, -1
	w.session = Session{ID: source.ID, HostID: source.HostID, Title: source.Title, Address: source.Address, host: source.host, conn: source.conn, phase: source.phase, files: w.view}
	w.ui.fraction = .5
	w.initialLocal = localStart()
	if source.files != nil {
		w.initialRemote = source.files.path
		if source.files.lfolder.path != "" {
			w.initialLocal = source.files.lfolder.path
		}
	}
	if state.sftpWindows == nil {
		state.sftpWindows = make(map[uint32]*sftpWindow)
	}
	state.sftpWindows[source.ID] = w
	return w
}

func (w *sftpWindow) show() {
	state := w.owner
	window, err := state.options.OpenContentWindow(w.config(), nil)
	if err != nil {
		state.fileNotices.show(state.messages.SFTPWindow.Title, err.Error(), components.ToastError)
		w.onClosed()
		return
	}
	w.window = window
	if w.ownsConnection && w.session.relay != nil {
		w.session.relay.set(func() { window.Invalidate(); state.options.Invalidate() })
	}
	connection, mode := w.session.conn, w.session.host.Transfer
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return openRemoteFiles(connection, mode) }, func() { window.Invalidate(); state.options.Invalidate() })
	state.configureSFTPWorker(w.view)
	state.listLocal(w.view, w.initialLocal)
	state.listSFTP(w.view, w.initialRemote)
	w.wake()
	window.Invalidate()
}

func (w *sftpWindow) config() app.Config {
	return app.Config{Title: w.owner.messages.SFTPWindow.Title + " — " + w.session.Title + " · " + w.session.Address, Icon: AppIcon(), Width: 1200, Height: 800, Tree: Tree(), Paint: w.paint, OnWake: w.wake, OnPointer: w.pointer, OnKey: w.key, OnShortcut: w.shortcut, OnScroll: w.scroll, OnFileDrop: w.fileDrop, TextInput: w.textInput, OnCloseRequest: w.requestClose, OnClosed: w.onClosed}
}

func (w *sftpWindow) connected() bool {
	return w.session.conn != nil && w.session.conn.Status().Phase == sshconn.PhaseConnected
}

func (w *sftpWindow) wake() {
	if w.closed || w.view.worker == nil {
		return
	}
	w.owner.pumpSFTP(&w.session)
	if w.externalConflict != 0 && w.owner.editByID(w.externalConflict) == nil {
		w.externalConflict = 0
	}
	if w.view.dialog != sftpDialogNone {
		w.cancelDrag()
	}
	w.view.local = w.activeLocal
	if w.localRefresh && w.pendingLocal == "" && !sftpTransfersActive(w.view.transfers) {
		w.localRefresh = false
		w.owner.listLocal(w.view, w.view.lfolder.path)
	}
	for side, folder := range []*sftpFolder{&w.view.lfolder, &w.view.sftpFolder} {
		if folder.path != w.shownPaths[side] {
			w.ui.offset[side] = 0
			w.shownPaths[side] = folder.path
		}
	}
}

func (view *sftpView) invalidateDetached() {
	if view.detached != nil && !view.detached.closed && view.detached.window != nil {
		view.detached.window.Invalidate()
	}
}

func (state *State) syncSFTPWindows() {
	for _, w := range state.sftpWindows {
		if w.closed || w.window == nil {
			continue
		}
		phase := w.session.conn.Status().Phase
		if w.phase != phase || w.language != state.language || w.pal != state.pal {
			w.phase, w.language, w.pal = phase, state.language, state.pal
			w.window.Invalidate()
		}
	}
}

func (state *State) closeSFTPWindows() {
	for _, w := range state.sftpWindows {
		w.afterClosed = nil
		if w.window != nil {
			w.window.Close()
		} else {
			w.onClosed()
		}
		w.releaseConnection()
	}
	state.sftpWindows = nil
}

func (w *sftpWindow) requestClose(allow func()) {
	if w.hasWork() {
		w.allowClose = allow
		w.closeConfirm = true
		w.cancelDrag()
		w.window.Invalidate()
		return
	}
	allow()
}

func (w *sftpWindow) onClosed() {
	if w.closed {
		return
	}
	w.closed = true
	w.deferredListings = [2]func(){}
	w.view.localSeq++
	w.view.remoteSeq++
	if w.view.cancelExtract != nil {
		w.view.cancelExtract()
	}
	w.view.extracting = false
	w.owner.dropWindowExternalEdits(w)
	if w.view.worker != nil {
		w.view.worker.Close()
		w.owner.refreshSFTPTransfers(w.view)
		w.owner.archiveTransfers(w.view)
	}
	w.owner.options.Invalidate()
	w.view.rows = nil
	w.view.lfolder.rows = nil
	w.view.forgetListings()
	w.releaseConnection()
	if w.afterClosed != nil {
		next := w.afterClosed
		w.afterClosed = nil
		next()
	}
}

func (state *State) confirmSFTPWindows(allow func()) bool {
	for _, w := range state.sftpWindows {
		if !w.hasWork() {
			continue
		}
		w.afterClosed = func() {
			if !state.confirmSFTPWindows(allow) {
				allow()
			}
		}
		w.requestClose(w.window.Close)
		w.window.Show()
		return true
	}
	return false
}

func (w *sftpWindow) hasWork() bool {
	if w.view.extracting || w.owner.edits.opening.active && w.owner.edits.opening.origin == w {
		return true
	}
	for i := range w.owner.edits.list {
		if w.owner.edits.list[i].origin == w {
			return true
		}
	}
	return w.view.worker != nil && sftpTransfersActive(w.view.worker.Transfers(nil))
}

func (w *sftpWindow) folder(local bool) *sftpFolder {
	if local {
		return &w.view.lfolder
	}
	return &w.view.sftpFolder
}

func (w *sftpWindow) navigate(local bool, location string) {
	if w.view.dialog != sftpDialogNone || w.closeConfirm || (!local && (!w.connected() || w.view.busy)) {
		return
	}
	w.activeLocal, w.view.local = local, local
	w.cancelDrag()
	if local {
		w.pendingLocal = filepath.Clean(location)
		w.owner.listLocal(w.view, location)
		if w.view.errorText != "" {
			w.pendingLocal = ""
		}
	} else {
		w.owner.openSFTPDirectory(w.view, location)
	}
}

func (w *sftpWindow) openEntry(local bool, index int) {
	if w.closeConfirm || w.view.dialog != sftpDialogNone || local && w.pendingLocal != "" || !local && (!w.connected() || w.view.busy) {
		return
	}
	folder := w.folder(local)
	if index < 0 || index >= len(folder.rows) {
		return
	}
	entry := folder.rows[index].entry
	if entry.LinkBroken {
		return
	}
	w.activeLocal, w.view.local = local, local
	folder.selectRow(index, 0)
	w.selectionChanged(local)
	if entry.IsDir() {
		location := path.Join(folder.path, entry.Name)
		if local {
			location = filepath.Join(folder.path, entry.Name)
		}
		w.navigate(local, location)
	} else {
		location := path.Join(folder.path, entry.Name)
		if local {
			location = filepath.Join(folder.path, entry.Name)
		}
		w.owner.openFileWindow(w.view, location, local)
	}
}

func (w *sftpWindow) queueSelection(local bool) {
	var entries []sftpclient.Entry
	for _, row := range w.folder(local).rows {
		if row.selected {
			entries = append(entries, row.entry)
		}
	}
	w.queueEntries(local, entries, w.folder(local).path, w.folder(!local).path)
}

func (w *sftpWindow) queueEntries(fromLocal bool, entries []sftpclient.Entry, sourceDir, targetDir string) {
	if !w.connected() || w.view.worker == nil || w.view.busy || w.pendingLocal != "" || sourceDir == "" || targetDir == "" || w.closeConfirm || w.view.dialog != sftpDialogNone {
		return
	}
	queued := 0
	for _, entry := range entries {
		if entry.LinkBroken {
			continue
		}
		var accepted bool
		if fromLocal {
			_, accepted = w.view.worker.QueueUpload(filepath.Join(sourceDir, entry.Name), path.Join(targetDir, entry.Name), entry.IsDir() && !entry.IsLink(), sftpclient.ExistingFail)
		} else {
			_, accepted = w.view.worker.QueueDownload(path.Join(sourceDir, entry.Name), filepath.Join(targetDir, entry.Name), entry.IsDir(), sftpclient.ExistingFail)
		}
		if accepted {
			queued++
		}
	}
	if queued > 0 {
		w.localRefresh = w.localRefresh || !fromLocal
		w.view.status = fmt.Sprintf(w.owner.messages.SFTPQueuedFmt, fmt.Sprintf("%d → %s", queued, targetDir))
		w.view.errorText = ""
		w.owner.refreshSFTPTransfers(w.view)
	}
	if queued < len(entries) {
		w.notices.show(w.owner.messages.SFTPWindow.Title, fmt.Sprintf(w.owner.messages.SFTPWindow.QueueRejectedFmt, len(entries)-queued), components.ToastWarning)
	}
}
