package workbench

import (
	"fmt"
	"path"
	"path/filepath"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// A transfer window fixes its SSH source at creation, never at the active tab.
// The terminal entry borrows that connection; the host-menu entry owns its
// connection or authenticated share. Both own their browser and SFTP worker.
type sftpWindow struct {
	owner                   *State
	session                 Session
	window                  *app.ContentWindow
	view                    *sftpView
	closed, closeConfirm    bool
	closedSignal            atomic.Bool
	allowClose, afterClosed func()
	activeLocal             bool
	// pendingFocus is the control the next paint hands the keyboard to, for a
	// focus change the model makes outside an input event.
	pendingFocus                gfx.ID
	localRefresh                bool
	pendingLocal                string
	phase                       sshconn.Phase
	language                    i18n.Language
	pal                         *palette
	tabTitle                    string
	tabTitleSource              string
	tabTitleLanguage            i18n.Language
	tabTitleReady               bool
	shownPaths                  [2]string
	input                       app.TextInput
	notices                     fileNotices
	ui                          sftpWindowUI
	drag                        sftpWindowDrag
	deferredListings            [2]func()
	externalConflict            uint32
	ownsConnection              bool
	sourceConn, boundConn       Connection
	initialLocal, initialRemote string
	browse                      sftpBrowseSync
	bookmark                    sftpBookmarkEditor
	fileProperties              sftpFileProperties
	directoryTree               [2]sftpDirectoryTree
	// frame and visible let another window find this one while a file drag is
	// outside its own content area. They are nil for a tab hosted by the main
	// window, which the drag finds through the tab strip instead.
	frame   func() (gfx.Rect, bool)
	visible func() bool
	// foreign is what another window's drag would drop here, for the highlight.
	foreign sftpForeignDrag
}

// sftpForeignDrag is a file drag from another window that would land in this
// window's pane. The highlight is cleared by the drag's source.
type sftpForeignDrag struct {
	active    bool
	fromLocal bool // the source pane is this computer's
	local     bool // the target pane is this computer's
	target    string
	row       int
}

func (state *State) openSFTPWindow(source *Session) {
	if source == nil || source.conn == nil || !source.hasServer() || source.phase != sshconn.PhaseConnected || !state.sftpManagersAvailable() {
		return
	}
	if existing := state.sftpWindows[source.ID]; existing != nil && !existing.closed {
		existing.reveal()
		return
	}
	w := state.newSFTPWindow(source)
	if state.sftpOpenMode == sftpOpenTab {
		w.openTab()
	} else {
		w.show()
	}
}

func (state *State) newSFTPWindow(source *Session) *sftpWindow {
	w := &sftpWindow{owner: state, activeLocal: true, phase: source.phase, language: state.language, pal: state.pal, sourceConn: source.conn}
	w.directoryTree[0].enabled = state.sftpDirectoryTree
	w.directoryTree[1].enabled = state.sftpDirectoryTree
	w.view = &sftpView{detached: w, busy: true, operation: sftpclient.OperationList}
	w.view.startListingStatus(state.messages.SFTPLoading)
	// A server lists its dotfiles by default, as the docked panel does (openSFTP).
	w.view.sftpFolder.showHidden = true
	w.view.transferServer, w.view.transferAddress = source.Title, source.Address
	w.view.transferHost = transferHostFor(source)
	w.view.selected, w.view.lfolder.selected = -1, -1
	w.session = Session{ID: source.ID, HostID: source.HostID, Title: source.Title, Address: source.Address, host: source.host, conn: source.conn, phase: source.phase, files: w.view}
	w.ui.split = ui.SplitterState{Ratio: .5}
	w.initialLocal = state.localSideStart()
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
	w.frame = window.Frame
	w.visible = func() bool { return !window.Closed() && !window.Minimized() }
	if w.ownsConnection && w.session.relay != nil {
		w.session.relay.set(func() { window.Invalidate(); state.options.Invalidate() })
	}
	w.startBrowser()
	window.Invalidate()
}

func (w *sftpWindow) startBrowser() {
	if w.closed || w.view.worker != nil || !w.connected() {
		return
	}
	state := w.owner
	connection, mode := w.session.conn, w.session.host.Transfer
	w.boundConn = connection
	if !w.ownsConnection {
		w.sourceConn = connection
	}
	w.clearDirectoryTreeRequests()
	w.view.openFiles = func() (sftpclient.Remote, error) { return openRemoteFiles(connection, mode) }
	// Wake both windows when a transfer changes. w.invalidate alone only
	// repaints the detached window, leaving the transfer center in the main
	// window showing a completed upload as still running until some other
	// event invalidates it (closing the file window used to be that event).
	w.view.worker = sftpclient.NewWorker(w.view.openFiles, state.notifyForView(&w.session, w.view))
	state.configureSFTPWorker(w.view)
	state.listLocal(w.view, w.initialLocal)
	state.listSFTP(w.view, w.initialRemote)
	if w.directoryTree[0].enabled {
		w.enableDirectoryTree(0, w.initialLocal)
	}
	if w.directoryTree[1].enabled {
		w.enableDirectoryTree(1, w.initialRemote)
	}
	w.invalidate()
}

func (w *sftpWindow) config() app.Config {
	base := app.Config{Title: w.owner.messages.SFTPWindow.Title + " — " + w.session.Title + " · " + w.session.Address, Icon: AppIcon(), Width: 1200, Height: 800, MinWidth: sftpWindowMinWidth, MinHeight: sftpWindowMinHeight, WindowMode: app.WindowCustomTitlebar, TitlebarHeight: TitlebarHeight, ClearColor: windowClearColor(w.pal.workspace), WindowBorderColor: func() app.Color { return windowClearColor(w.pal.workspace) }, Tree: Tree(), Paint: w.paint, OnWake: w.wake, OnPointer: w.pointer, OnKey: w.key, OnShortcut: w.shortcut, OnScroll: w.scroll, OnFileDrop: w.fileDrop, TextInput: w.textInput, OnCloseRequest: w.requestClose, OnClosed: w.onClosed}
	// The window's page routes its chrome, its panes' Lists and its connecting
	// state. The window keeps Paint (it also paints the toolbar controls, dialogs
	// and transfer queue the page only lays out) and OnScroll (a wheel notch moves
	// whole rows, and the window hands the offset to each List).
	config := ui.Configure(w.windowPage(), base)
	config.Paint = base.Paint
	config.OnScroll = w.scrollWindow
	config.OnActivate = w.activate
	return config
}

func (w *sftpWindow) connected() bool {
	return w.session.conn != nil && w.session.conn.Status().Phase == sshconn.PhaseConnected
}

func (w *sftpWindow) wake() {
	if w.closed {
		return
	}
	if w.ownsConnection {
		w.owner.pumpSFTPConnections()
		w.owner.showNextPrompt()
	} else {
		w.syncBorrowedConnection()
	}
	w.pump()
	if len(w.owner.edits.list) > 0 && !w.owner.edits.watching {
		w.owner.watchExternalEdits()
	}
}

// wakeEmbedded runs the per-browser work for a main-window tab. State.Wake has
// already pumped standalone connections and authentication prompts once for the
// whole workbench, so doing that here would make every open file tab scan all
// the others.
func (w *sftpWindow) wakeEmbedded() {
	if w.closed {
		return
	}
	w.pump()
	if len(w.owner.edits.list) > 0 && !w.owner.edits.watching {
		w.owner.watchExternalEdits()
	}
}

func (w *sftpWindow) pump() {
	if w.view.worker == nil {
		return
	}
	w.owner.pumpSFTP(&w.session)
	if !w.connected() {
		if w.view.sudo {
			w.owner.forgetSFTPDocumentAccess(w.view)
			w.view.worker.Interrupt()
			w.view.worker.Close()
			w.owner.refreshSFTPTransfers(w.view)
			w.owner.archiveTransfers(w.view)
			w.view.worker = nil
			w.view.openFiles = nil
			w.view.sudo = false
			w.boundConn = nil
		}
		w.browse.enabled, w.browse.pending = false, nil
		if w.view.dialog == sftpDialogBrowseCreateDirectory {
			w.owner.closeSFTPDialog(w.view)
		}
	}
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
	if view.detached != nil {
		view.detached.invalidate()
	}
}

func (state *State) syncSFTPWindows() {
	for _, w := range state.sftpWindows {
		if w.closed {
			continue
		}
		w.syncBorrowedConnection()
		phase := w.session.phase
		if w.session.conn != nil {
			phase = w.session.conn.Status().Phase
		}
		if w.phase != phase || w.language != state.language || w.pal != state.pal {
			w.phase, w.language, w.pal = phase, state.language, state.pal
			w.session.phase = phase
			w.invalidate()
		}
	}
}

func (w *sftpWindow) syncBorrowedConnection() {
	if w == nil || w.closed || w.ownsConnection || w.owner == nil {
		return
	}
	source, ok := w.owner.sessionByIDPointer(w.session.ID)
	if !ok {
		return
	}
	if source.conn != w.sourceConn {
		w.sourceConn = source.conn
		w.session.conn = source.conn
		w.session.phase = source.phase
		w.session.endpoint = source.endpoint
		w.session.sudoCommands, w.session.sudoPassword = false, ""
		w.phase = source.phase
		w.invalidate()
	}
	if w.connected() && (w.boundConn != w.sourceConn || w.view.worker == nil) {
		w.reconnectBrowser(source)
	}
}

func (w *sftpWindow) reconnectBrowser(source *Session) {
	if w == nil || w.closed || source == nil || !w.connected() {
		return
	}
	state := w.owner
	prevAddress := w.view.transferAddress
	if prevAddress == "" {
		prevAddress = w.session.Address
	}
	endpointChanged := source.Address != "" && prevAddress != "" && source.Address != prevAddress

	if w.view.worker != nil {
		w.view.worker.Interrupt()
		w.view.worker.Close()
		state.refreshSFTPTransfers(w.view)
		state.archiveTransfers(w.view)
	}
	for id := range w.view.remoteCopies {
		state.releaseRemoteCopySource(w.view, id)
	}
	w.view.remoteCopies = nil
	w.clearDirectoryTreeRequests()
	w.invalidateDirectoryTree(false)
	w.view.forgetListings()
	w.view.remoteSeq++
	w.view.navigation = sftpNavigation{}
	w.view.waitingNavigation = sftpNavigation{}
	w.view.waiting = ""
	w.view.preparingPath = ""
	w.view.abandonedPath = ""
	w.view.pathRequest = ""
	w.view.errorText = ""
	w.view.refreshStatus = ""
	w.view.status = ""
	w.view.sudo = false
	w.view.busy = false
	w.browse.enabled, w.browse.pending = false, nil

	if !w.ownsConnection {
		w.sourceConn = source.conn
		w.session.HostID = source.HostID
		w.session.Title = source.Title
		w.session.Address = source.Address
		w.session.host = source.host
		w.session.conn = source.conn
		w.session.phase = source.phase
		w.session.endpoint = source.endpoint
		w.session.sudoCommands, w.session.sudoPassword = false, ""
		w.phase = source.phase
		w.view.transferServer, w.view.transferAddress = source.Title, source.Address
		w.view.transferHost = transferHostFor(source)
		if w.window == nil {
			state.rebuildFileTabOptions()
		}
	} else {
		w.phase = w.session.phase
	}

	targetDir := w.view.sftpFolder.path
	if targetDir == "" {
		if w.view.restorePath != "" {
			targetDir = w.view.restorePath
		} else {
			targetDir = w.initialRemote
		}
	}
	if endpointChanged {
		targetDir = ""
		w.initialRemote = ""
		w.view.sftpFolder.history = sftpHistory{}
		w.view.sftpFolder.path = ""
		w.view.sftpFolder.allRows = nil
		w.view.sftpFolder.rows = nil
		w.view.sftpFolder.crumbs = nil
		w.view.restorePath = ""
		resetDirectoryTree(&w.directoryTree[1])
	} else {
		w.view.restorePath = targetDir
	}

	connection, mode := w.session.conn, w.session.host.Transfer
	w.boundConn = connection
	w.view.openFiles = func() (sftpclient.Remote, error) { return openRemoteFiles(connection, mode) }
	w.view.worker = sftpclient.NewWorker(w.view.openFiles, state.notifyForView(&w.session, w.view))
	state.configureSFTPWorker(w.view)

	for _, document := range state.fileWindows.documents {
		if document.view == w.view && !document.local && !document.closed && document.worker != nil {
			document.worker.Close()
			document.openRemoteWorker(w.view.openFiles)
		}
	}
	if w.view.lfolder.path == "" {
		state.listLocal(w.view, w.initialLocal)
	}
	state.listSFTP(w.view, targetDir)
	if w.directoryTree[1].enabled {
		if w.directoryTree[1].rootPath == "" {
			w.enableDirectoryTree(1, targetDir)
		} else {
			w.loadExpandedDirectoryTree(false)
		}
	}
	w.invalidate()
	state.options.Invalidate()
}

// A native transfer window receives its own OnWake callback. A manager hosted
// by a main-window tab does not, so the workbench pumps it here even while
// another tab is visible. This keeps transfers and document polling alive in
// the background without painting the hidden manager.
func (state *State) wakeEmbeddedSFTPWindows() {
	for _, w := range state.sftpWindows {
		if w != nil && !w.closed && w.window == nil {
			w.wakeEmbedded()
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
	state.sftpTabs = nil
	state.activeSFTPTab = 0
}

func (w *sftpWindow) requestClose(allow func()) {
	if w.hasWork() {
		w.allowClose = allow
		w.closeConfirm = true
		w.cancelDrag()
		w.invalidate()
		return
	}
	allow()
}

func (w *sftpWindow) closeDescription() string {
	if w.window == nil {
		return w.owner.messages.SFTPWindow.CloseTabDescription
	}
	return w.owner.messages.SFTPWindow.CloseDescription
}

func (w *sftpWindow) onClosed() {
	if w.closedSignal.Swap(true) {
		return
	}
	w.closed = true
	w.owner.cancelReconnect(&w.session)
	w.fileProperties.close()
	w.view.sftpFolder.stopSizeCalculation()
	w.view.lfolder.stopSizeCalculation()
	if w.owner.modal == modalSudoSFTP && w.owner.sudoSFTP.window == w {
		w.owner.closeModal()
	}
	w.browse.pending, w.browse.enabled = nil, false
	if w.ownsConnection && w.owner.modalSession == w.session.ID {
		w.owner.closeModal()
	}
	w.deferredListings = [2]func(){}
	w.view.localSeq++
	w.view.remoteSeq++
	w.cancelDrag()
	w.clearDirectoryTreeRequests()
	// The copied server files stay available while another terminal keeps the
	// connection; only a window that takes its own connection away ends them.
	if w.ownsConnection {
		w.owner.clearSFTPRemoteClipboardFrom(w.view)
	}
	w.owner.cancelRemoteCopiesFrom(w.view)
	if w.view.cancelExtract != nil {
		w.view.cancelExtract()
	}
	w.view.extracting = false
	w.owner.dropWindowExternalEdits(w)
	if w.view.sudo {
		w.owner.forgetSFTPDocumentAccess(w.view)
	}
	if w.view.worker != nil {
		// The window's connection is going away with it. Its unfinished uploads
		// and downloads keep their partial files and wait for a view of the same
		// host (ADR 0028).
		w.view.worker.Interrupt()
		w.view.worker.Close()
		w.owner.refreshSFTPTransfers(w.view)
		w.owner.archiveTransfers(w.view)
	}
	w.view.openFiles = nil
	w.view.sudo = false
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
		w.requestClose(w.closeHostNow)
		w.reveal()
		return true
	}
	return false
}

// invalidateSFTPWindows repaints every manager after a setting that changes
// their shared presentation. A native manager repaints itself; a tab asks the
// main window, which hosts it.
func (state *State) invalidateSFTPWindows() {
	for _, w := range state.sftpWindows {
		if !w.closed {
			w.invalidate()
		}
	}
	state.options.Invalidate()
}

func (w *sftpWindow) invalidate() {
	if w == nil || w.closedSignal.Load() || w.owner == nil {
		return
	}
	if w.window != nil {
		w.window.Invalidate()
		return
	}
	w.owner.options.Invalidate()
}

func (w *sftpWindow) reveal() {
	if w == nil || w.closed {
		return
	}
	if w.window != nil {
		w.window.Show()
		w.window.Invalidate()
		return
	}
	w.owner.activateSFTPTab(w.session.ID)
}

func (w *sftpWindow) closeHostNow() {
	if w == nil || w.closed {
		return
	}
	if w.window != nil {
		w.window.Close()
		return
	}
	w.owner.removeSFTPTab(w)
}

func (w *sftpWindow) requestHostClose() {
	if w == nil || w.closed {
		return
	}
	if w.window != nil {
		w.window.RequestClose()
		return
	}
	w.owner.requestCloseSFTPTab(w)
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
	// A copy this window is the source of still reads through its connection:
	// closing the window must ask first, and cancel it if the user agrees.
	if len(w.view.outgoingCopies) > 0 {
		return true
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
	if w.view.dialog != sftpDialogNone || w.bookmark.open || w.closeConfirm || w.browse.pending != nil || (!local && (!w.connected() || w.view.busy)) {
		return
	}
	w.activeLocal, w.view.local = local, local
	w.cancelDrag()
	if local {
		location = filepath.Clean(location)
	} else {
		location = path.Clean(location)
	}
	w.startBrowseNavigation(local, location)
}

func (w *sftpWindow) openEntry(local bool, index int) {
	if w.closeConfirm || w.view.dialog != sftpDialogNone || w.browse.pending != nil || local && w.pendingLocal != "" || !local && (!w.connected() || w.view.busy) {
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
	if !w.connected() || w.view.worker == nil || w.view.busy || w.pendingLocal != "" || sourceDir == "" || targetDir == "" ||
		w.fileProperties.open || w.closeConfirm || w.view.dialog != sftpDialogNone || len(entries) == 0 {
		return
	}
	if !fromLocal {
		requests := make([]sftpDownloadRequest, 0, len(entries))
		for _, entry := range entries {
			if entry.LinkBroken {
				continue
			}
			requests = append(requests, sftpDownloadRequest{
				view: w.view, remoteDir: sourceDir, entry: entry,
				localPath: filepath.Join(targetDir, safeLocalName(entry.Name)),
			})
		}
		queued := w.owner.queueSFTPDownloads(w.view, requests)
		if queued > 0 {
			w.localRefresh = true
		}
		if queued < len(entries) && w.view.dialog == sftpDialogNone {
			w.notices.show(w.owner.messages.SFTPWindow.Title, fmt.Sprintf(w.owner.messages.SFTPWindow.QueueRejectedFmt, len(entries)-queued), components.ToastWarning)
		}
		return
	}
	queued := 0
	for _, entry := range entries {
		if entry.LinkBroken {
			continue
		}
		if _, accepted := w.view.worker.QueueUpload(filepath.Join(sourceDir, entry.Name), path.Join(targetDir, entry.Name), entry.IsDir() && !entry.IsLink(), sftpclient.ExistingFail); accepted {
			queued++
		}
	}
	if queued > 0 {
		w.view.status = fmt.Sprintf(w.owner.messages.SFTPQueuedFmt, fmt.Sprintf("%d → %s", queued, targetDir))
		w.view.errorText = ""
		w.owner.refreshSFTPTransfers(w.view)
	}
	if queued < len(entries) {
		w.notices.show(w.owner.messages.SFTPWindow.Title, fmt.Sprintf(w.owner.messages.SFTPWindow.QueueRejectedFmt, len(entries)-queued), components.ToastWarning)
	}
}
