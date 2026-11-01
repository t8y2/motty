package workbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

type sftpCrumb struct {
	label string
	path  string
}

type sftpRow struct {
	entry                sftpclient.Entry
	sortName             string
	selected             bool
	cells                [5]string
	icon                 *ui.Icon      // the file-type glyph, chosen when the listing arrives
	mono                 bool          // the glyph has no colors of its own and is tinted
	appIcon              *fileIconSlot // the system's icon for a local application bundle, once it has answered
	hidden               bool
	matchStart, matchEnd int // byte range highlighted by the current folder filter
}

type sftpDialogKind uint8

const (
	sftpDialogNone sftpDialogKind = iota
	sftpDialogCreateDirectory
	sftpDialogRename
	sftpDialogDelete
	sftpDialogChmod
	sftpDialogDiscardEdit
	sftpDialogReloadEdit
	sftpDialogRemoteChanged
	sftpDialogSymlink
	sftpDialogExtract
	sftpDialogCompress
	sftpDialogBrowseCreateDirectory
)

// sftpFolder is a folder on show: where it is, its rows and the user's place in
// them. The panel has one for the server and one for this computer.
type sftpFolder struct {
	history        sftpHistory
	navigation     sftpNavigation
	path           string
	allRows        []sftpRow
	rows           []sftpRow
	crumbs         []sftpCrumb
	selected       int
	anchor         int // where a Shift-click range starts: the row last chosen without Shift
	first          int
	visibleRows    int     // rows the last paint had room for
	wheel          float32 // wheel travel not yet worth a whole row
	scroll         components.ScrollAreaGeometry
	scrollOffset   float32
	pathField      sftpPathField
	filterEditor   text.Editor
	filterInput    app.TextInput
	filterReady    bool
	filterOpen     bool
	showHidden     bool
	pathEditing    bool
	headerMenuOpen bool
	sortOrder      sftpWindowSort
	sortChosen     bool
	localCoverage  localListingCoverage
	coverageLabel  string // bounded scope, formatted when a listing/filter changes
	coverageText   string // full semantic warning, independent of ordinary feedback
	coverageEmpty  string
	hiddenMatch    bool // the query matches rows this folder hides
}

type sftpView struct {
	detached                        *sftpWindow // optional native dual-pane owner; transport/navigation stay per view
	worker                          *sftpclient.Worker
	transferScan                    uint64
	transferHost                    *transferHost
	transferServer, transferAddress string
	// sftpFolder is the server's folder; lfolder is this computer's, shown
	// instead of it while local is set.
	sftpFolder
	lfolder           sftpFolder
	local             bool
	localOnly         bool   // a local terminal's panel: no server, no worker, no other side to switch to
	localSeq          uint64 // the latest local listing; an older answer is dropped
	remoteSeq         uint64 // the latest remote listing prepared; older work is dropped
	navigationID      uint64
	waitingID         uint64
	waitingNavigation sftpNavigation
	preparingID       uint64
	acceptedListID    uint64
	preparingPath     string // the large listing whose rows are being prepared
	// refreshAfterListing names the folder to read again once the foreground
	// listing that started before a save lands.
	refreshAfterListing string
	followUpPath        string // a follow-up that arrived before those rows were ready
	followUpEntries     []sftpclient.Entry
	busy                bool
	approximate         bool // the listing came from `ls` over SCP
	operation           sftpclient.Operation
	status              string
	// listingStatus is the listing message status still holds, so a completed
	// listing can retire it without dropping the feedback that replaced it.
	listingStatus         string
	localListingStatus    bool // the shared listing message belongs to a pending local read
	errorText             string
	refreshStatus         string
	dialog                sftpDialogKind
	dialogEditor          text.Editor
	dialogEditor2         text.Editor
	bookmarksOpen         bool
	places                sftpPlaceMenu
	dropping              bool // files are being dragged over the panel
	dropRow               int  // the folder row they are over, or -1
	scrollDrag            bool
	scrollGrab            float32
	details               sftpDetails
	extracting            bool
	archiveSequence       uint64
	archiveDirectory      string
	archiveResults        chan archiveResult
	editorResults         chan editorResult
	cancelExtract         context.CancelFunc
	dialogEntry           sftpclient.Entry
	dialogError           string
	transfers             []sftpclient.TransferSnapshot
	refreshAfterTransfers bool
	editPath              string
	editName              string
	editLocation          string
	editOriginal          string
	editRevision          sftpclient.Revision
	editUTF8BOM           bool
	editEditor            text.Editor
	editLayout            components.TextareaLayout // the editor's rows and scroll
	editRect              ui.Rect                   // where the editor was painted, for the wheel

	// listings remembers the folders already read, oldest first, so going back
	// up or into a folder seen before shows at once; prefetching holds folders
	// being read ahead of a click and waiting the one the user already asked for.
	listings     []sftpListing
	listingBytes int // approximate bytes held by listings
	// listingLimitBytes overrides maxSFTPListingBytes in tests.
	listingLimitBytes     int
	prefetching           map[string]uint64
	waiting               string
	pathRequest           string // the directory submitted through the path input, awaiting its listing
	abandonedPath         string // a path request the user canceled while it was in flight
	terminalPath          string // an explicit navigation, never a shell report or background refresh
	terminalPromptVersion uint64
	terminalSyncSkipped   bool
}

// sftpListing is one folder as it was last read.
type sftpListing struct {
	path    string
	entries []sftpclient.Entry
	bytes   int
}

const (
	maxSFTPListings       = 16       // folders remembered per panel
	maxSFTPListingEntries = 20000    // a larger folder is not worth the memory
	maxSFTPListingBytes   = 32 << 20 // the whole cache, in approximate entry bytes
	// sftpPrepInlineEntries is where preparing a remote listing's rows moves off
	// the window thread. Each row formats a size, a local time and a file icon;
	// a few hundred rows are not worth a goroutine and a frame of latency.
	sftpPrepInlineEntries = 512
)

func (state *State) openSFTP(session *Session) {
	if session != nil && session.isLocal() && session.files == nil {
		view := &sftpView{local: true, localOnly: true}
		view.adoptHistory(session.fileHistory)
		view.selected, view.lfolder.selected = -1, -1
		session.files = view
		start := session.fileHistory[0].path
		if start == "" || session.term != nil && session.term.followPath != "" {
			start = state.localPanelStart(session)
		}
		state.listLocal(view, start)
		return
	}
	if session == nil || !session.hasServer() || session.conn == nil || session.phase != sshconn.PhaseConnected {
		return
	}
	if session.files != nil {
		return
	}
	connection, transfer := session.conn, session.host.Transfer
	view := &sftpView{busy: true, operation: sftpclient.OperationList}
	view.adoptHistory(session.fileHistory)
	view.startListingStatus(state.messages.SFTPLoading)
	view.sftpFolder.showHidden = true
	view.transferServer, view.transferAddress = session.Title, session.Address
	view.transferHost = transferHostFor(session)
	view.selected, view.lfolder.selected = -1, -1
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return openRemoteFiles(connection, transfer) }, state.notifyFor(session))
	state.configureSFTPWorker(view)
	session.files = view
	if session.term != nil {
		if cwd, ok := session.term.screen.WorkingDirectory(); ok {
			state.terminalDirectory(session.ID, cwd)
		}
	}
	state.listSFTP(view, session.fileHistory[1].path)
	if !view.busy {
		view.busy = false
		view.errorText = state.messages.SFTPUnavailable
	}
}

func (state *State) closeSFTP(session *Session) {
	if session == nil {
		return
	}
	session.fileHistory = [2]sftpSavedHistory{}
	if session.files == nil {
		return
	}
	session.files.localSeq++
	session.files.remoteSeq++
	session.files.navigation, session.files.lfolder.navigation = sftpNavigation{}, sftpNavigation{}
	if session.files.cancelExtract != nil {
		session.files.cancelExtract()
	}
	if session.files.worker != nil {
		session.files.worker.Close()
		state.refreshSFTPTransfers(session.files)
		state.archiveTransfers(session.files)
	}
	state.forgetTerminalUploads(session.files)
	session.files = nil
}

func (state *State) clickSFTP(id ui.ID) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone {
		return
	}
	if id == hitSFTPOpen {
		context, _ := state.deck.Context(state.deck.ActiveWorkspace())
		context.Kind = layoutcore.ContextSFTP
		context.Open = true
		_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
		state.openSFTP(session)
		state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
		state.save()
		return
	}
	if id == hitSFTPDetach {
		state.openSFTPWindow(session)
		return
	}
	if id == hitSFTPClose || id == hitSFTPDrawerClose {
		state.releaseIdleSFTP(session)
		context, _ := state.deck.Context(state.deck.ActiveWorkspace())
		context.Open = false
		_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
		state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
		state.save()
		return
	}
	if id == hitSFTPPanelSide {
		state.toggleSFTPPanelSide()
		return
	}
	view := session.files
	if view == nil {
		return
	}
	if view.dialog != sftpDialogNone {
		state.clickSFTPDialog(view, id)
		return
	}
	if view.bookmarksOpen {
		sftpPlaceBrowser{owner: state, session: session, view: view}.clickDirectoryPlace(id)
		return
	}
	folder := view.active()
	if folder.headerMenuOpen {
		if state.clickSFTPHistory(view, view.local, id, hitSFTPHistoryBack, hitSFTPHistoryForward, sftpRecentHits) {
			folder.headerMenuOpen = false
			return
		}
		if column, ok := id.In(sftpHeaderSortHits); ok && column < 3 {
			folder.headerMenuOpen = false
			state.toggleSFTPSidebarSort(folder, column)
			return
		}
		switch id {
		case hitSFTPHeaderFilter:
			folder.headerMenuOpen = false
			state.clickSFTPFilter(view, hitSFTPFilterToggle)
		case hitSFTPHeaderEditor:
			folder.headerMenuOpen = false
			state.openSFTPInEditor(view)
		case hitSFTPHeaderHidden:
			folder.headerMenuOpen = false
			state.clickSFTPFilter(view, hitSFTPHiddenToggle)
		default:
			folder.headerMenuOpen = false
		}
		return
	}
	if column, ok := id.In(hitSFTPColumnList); ok && column < int(sftpColumnCount) && state.sftpColumns.enabled(sftpColumnKey(column)) {
		state.toggleSFTPSidebarSort(folder, column)
		return
	}
	if id == hitSFTPHeaderMore {
		folder.headerMenuOpen = true
		return
	}
	if state.clickSFTPFilter(view, id) {
		return
	}
	if view.local && state.clickSFTPLocal(session, view, id) {
		return
	}
	if state.clickSFTPExtra(session, view, id) {
		return
	}
	switch {
	case id == hitSFTPEditBack && !view.busy:
		state.closeSFTPEditor(view)
		return
	case id == hitSFTPEditSave && !view.busy:
		state.saveSFTPEditor(view)
		return
	case id == hitSFTPEditReload && !view.busy:
		state.reloadSFTPEditor(view)
		return
	}
	switch {
	case id == hitSFTPCancel && view.busy:
		if view.operation == sftpclient.OperationList && !view.extracting {
			state.cancelSFTPNavigation(view)
			view.status = state.messages.SFTPCanceled
			return
		} else if view.extracting && view.cancelExtract != nil {
			view.cancelExtract()
		} else {
			view.worker.Cancel()
		}
		view.status = state.messages.SFTPCanceling
	case id == hitSFTPRefresh && !view.busy:
		view.forgetListings() // refresh means: trust nothing remembered
		state.listSFTP(view, view.path)
	case id == hitSFTPUp && !view.busy:
		state.navigateSFTPDirectory(view, path.Dir(view.path))
	case id == hitSFTPDownload && !view.busy:
		state.downloadSFTP(view)
	case id == hitSFTPUpload && !view.busy:
		state.uploadSFTP(view)
	case id == hitSFTPUploadDirectory && !view.busy:
		state.uploadSFTPDirectory(view)
	case id == hitSFTPNewDirectory && !view.busy:
		state.openSFTPDialog(view, sftpDialogCreateDirectory)
	case id == hitSFTPOpenEntry && !view.busy:
		state.openSelectedSFTP(view)
	case id == hitSFTPEditExternal && !view.busy:
		state.startExternalEdit(session, view)
	case id == hitSFTPRename && !view.busy:
		state.openSFTPDialog(view, sftpDialogRename)
	case id == hitSFTPDelete && !view.busy:
		state.openSFTPDialog(view, sftpDialogDelete)
	case id == hitSFTPChmod && !view.busy:
		state.openSFTPDialog(view, sftpDialogChmod)
	case id.Scope() == hitSFTPBreadcrumbList && !view.busy:
		index := item(id)
		if index < len(view.crumbs) {
			state.navigateSFTPDirectory(view, view.crumbs[index].path)
		}
	case id.Scope() == hitSFTPEntryList && !view.busy:
		index := view.first + item(id)
		if index < len(view.rows) {
			view.selected = index
			view.rows[index].selected = !view.rows[index].selected
			view.rows[index].cells[0] = sftpSelectionCell(view.rows[index].selected)
			view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
			if row := view.rows[index]; row.selected && row.entry.IsDir() {
				// The first click of a double-click: start reading the folder
				// now, so the second finds the work done or under way.
				state.prefetchSFTP(view, path.Join(view.path, row.entry.Name))
			}
		}
	}
}

func (state *State) toggleSFTPSidebarSort(folder *sftpFolder, column int) {
	if folder.sortChosen {
		folder.sortOrder.toggle(column)
	} else {
		folder.sortOrder = sftpWindowSort{column: column}
		folder.sortChosen = true
	}
	folder.sortRows(folder.sortOrder)
	folder.scrollOffset = float32(folder.first) * sftpRowHeight
}

func (state *State) sftpEditor(hit ui.ID) *text.Editor {
	if state.activeSection != sectionTerminal {
		return nil
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		return nil
	}
	if hit == hitSFTPFilter && session.files.dialog == sftpDialogNone && session.files.editPath == "" && session.files.active().filterOpen {
		return session.files.active().filterInputEditor()
	}
	if hit == hitSFTPPath && state.sftpPathVisible(session.files.active()) && session.files.dialog == sftpDialogNone && session.files.editPath == "" && (session.files.local || !session.files.busy) {
		return session.files.active().pathInput()
	}
	if hit == hitSFTPEditor && session.files.editPath != "" && session.files.dialog == sftpDialogNone {
		return &session.files.editEditor
	}
	if hit == hitSFTPDialogField2 && session.files.dialog == sftpDialogSymlink {
		return &session.files.dialogEditor2
	}
	if hit != hitSFTPDialogField || session.files.dialog == sftpDialogNone || session.files.dialog == sftpDialogDelete || session.files.dialog == sftpDialogDiscardEdit || session.files.dialog == sftpDialogReloadEdit || session.files.dialog == sftpDialogRemoteChanged || session.files.dialog == sftpDialogBrowseCreateDirectory {
		return nil
	}
	return &session.files.dialogEditor
}

func (state *State) selectedSFTPEntry(view *sftpView) (sftpclient.Entry, bool) {
	if selectedSFTPCount(view) != 1 {
		return sftpclient.Entry{}, false
	}
	rows := view.active().rows
	for index := range rows {
		if rows[index].selected {
			return rows[index].entry, true
		}
	}
	return sftpclient.Entry{}, false
}

// active is the folder the panel shows: the server's, or this computer's.
func (view *sftpView) active() *sftpFolder {
	if view.local {
		return &view.lfolder
	}
	return &view.sftpFolder
}

// startListingStatus shows a listing message and remembers it, so the listing
// that replaces it can tell its own message from feedback written meanwhile.
func (view *sftpView) startListingStatus(message string) {
	view.status, view.listingStatus = message, message
	view.localListingStatus = false
}

// setItemCountStatus reports a folder's count in the embedded panel's status
// line. A detached window shows counts beneath each pane instead, so its shared
// line carries operation feedback and, when idle, the drag hint: a listing only
// retires the message it started with.
func (view *sftpView) setItemCountStatus(format string, count int) {
	if view.detached == nil {
		view.status = fmt.Sprintf(format, count)
		return
	}
	if view.listingStatus != "" && view.status == view.listingStatus {
		view.status = ""
	}
	view.listingStatus = ""
}

// selectRow applies a press on a row the way a file list does: a plain press
// selects that row alone, Command or Control toggles it among the others, and
// Shift selects every row from the one last chosen to it.
func (folder *sftpFolder) selectRow(index int, modifiers ui.Modifiers) {
	if index < 0 || index >= len(folder.rows) {
		return
	}
	switch {
	case modifiers&ui.ModifierShift != 0:
		anchor := min(max(folder.anchor, 0), len(folder.rows)-1)
		from, to := min(anchor, index), max(anchor, index)
		for row := range folder.rows {
			folder.rows[row].selected = row >= from && row <= to
		}
	case modifiers&(ui.ModifierMeta|ui.ModifierControl) != 0:
		folder.rows[index].selected = !folder.rows[index].selected
		folder.anchor = index
	default:
		for row := range folder.rows {
			folder.rows[row].selected = row == index
		}
		folder.anchor = index
	}
	for row := range folder.rows {
		folder.rows[row].cells[0] = sftpSelectionCell(folder.rows[row].selected)
	}
	folder.selected = index
}

func selectedSFTPCount(view *sftpView) int {
	rows := view.active().rows
	count := 0
	for index := range rows {
		if rows[index].selected {
			count++
		}
	}
	return count
}

// selectedSFTP counts the selected rows in one pass and gives the entry of the
// last one, which is the selection when the count is one.
func selectedSFTP(view *sftpView) (count int, entry sftpclient.Entry) {
	rows := view.active().rows
	for index := range rows {
		if rows[index].selected {
			count++
			entry = rows[index].entry
		}
	}
	return count, entry
}

func selectedSFTPRows(view *sftpView) []sftpRow {
	rows := view.active().rows
	selected := make([]sftpRow, 0, selectedSFTPCount(view))
	for index := range rows {
		if rows[index].selected {
			selected = append(selected, rows[index])
		}
	}
	return selected
}

func sftpSelectionCell(selected bool) string {
	if selected {
		return "✓"
	}
	return ""
}

func (state *State) openSelectedSFTP(view *sftpView) {
	entry, ok := state.selectedSFTPEntry(view)
	if !ok {
		return
	}
	remotePath := path.Join(view.path, entry.Name)
	if entry.IsDir() {
		state.navigateSFTPDirectory(view, remotePath)
		return
	}
	if state.options.OpenContentWindow != nil {
		state.openFileWindow(view, remotePath, false)
		return
	}
	if view.worker.OpenText(remotePath) {
		view.busy = true
		view.operation = sftpclient.OperationOpenText
		view.status = fmt.Sprintf(state.messages.SFTPEditOpeningFmt, entry.Name)
		view.errorText = ""
	} else {
		view.errorText = state.messages.SFTPUnavailable
	}
}

func (state *State) closeSFTPEditor(view *sftpView) {
	if view.editPath == "" {
		return
	}
	if sftpEditorDirty(view) {
		view.dialog = sftpDialogDiscardEdit
		return
	}
	resetSFTPEditor(view)
}

func (state *State) saveSFTPEditor(view *sftpView) {
	if view.editPath == "" || !sftpEditorDirty(view) {
		return
	}
	if !view.worker.SaveText(view.editPath, view.editEditor.String(), view.editRevision, view.editUTF8BOM) {
		view.errorText = state.messages.SFTPUnavailable
		return
	}
	view.busy = true
	view.operation = sftpclient.OperationSaveText
	view.status = state.messages.SFTPEditSaving
	view.errorText = ""
}

func (state *State) reloadSFTPEditor(view *sftpView) {
	if view.editPath == "" {
		return
	}
	if sftpEditorDirty(view) {
		view.dialog = sftpDialogReloadEdit
		return
	}
	state.queueSFTPEditReload(view)
}

func (state *State) queueSFTPEditReload(view *sftpView) {
	if !view.worker.OpenText(view.editPath) {
		view.errorText = state.messages.SFTPUnavailable
		return
	}
	state.closeSFTPDialog(view)
	view.busy = true
	view.operation = sftpclient.OperationOpenText
	view.status = state.messages.SFTPEditReloading
	view.errorText = ""
}

func sftpEditorDirty(view *sftpView) bool {
	return view.editPath != "" && view.editEditor.String() != view.editOriginal
}

func resetSFTPEditor(view *sftpView) {
	view.editPath = ""
	view.editName = ""
	view.editLocation = ""
	view.editOriginal = ""
	view.editRevision = sftpclient.Revision{}
	view.editUTF8BOM = false
	view.editEditor = text.Editor{}
	view.errorText = ""
}

func (view *sftpView) listing(remotePath string) ([]sftpclient.Entry, bool) {
	for index := range view.listings {
		if view.listings[index].path == remotePath {
			return view.listings[index].entries, true
		}
	}
	return nil, false
}

// remember keeps a folder's entries, replacing an older reading of it and
// dropping the oldest folder when there are too many.
func (view *sftpView) remember(remotePath string, entries []sftpclient.Entry) {
	if len(entries) > maxSFTPListingEntries {
		return
	}
	// A newer reading of a path replaces the older one, including its bytes.
	for index := range view.listings {
		if view.listings[index].path == remotePath {
			view.listingBytes -= view.listings[index].bytes
			view.listings = slices.Delete(view.listings, index, index+1)
			break
		}
	}
	size := sftpListingSize(entries)
	limit := view.listingLimitBytes
	if limit <= 0 {
		limit = maxSFTPListingBytes
	}
	if size > limit {
		return // one folder larger than the whole budget is not cached
	}
	for len(view.listings) > 0 && (len(view.listings) >= maxSFTPListings || view.listingBytes+size > limit) {
		view.listingBytes -= view.listings[0].bytes
		view.listings = slices.Delete(view.listings, 0, 1)
	}
	view.listings = append(view.listings, sftpListing{path: remotePath, entries: entries, bytes: size})
	view.listingBytes += size
}

// sftpListingSize estimates what a listing costs, so the cache can be bounded
// by bytes as well as by folder count. The fixed part covers an Entry's fields
// and slice/string headers.
func sftpListingSize(entries []sftpclient.Entry) int {
	const entryOverhead = 128
	size := 0
	for _, entry := range entries {
		size += entryOverhead + len(entry.Name) + len(entry.Owner) + len(entry.Group) + len(entry.LinkTarget)
	}
	return size
}

// forgetListings drops everything remembered: after the remote changed, no
// reading of it can be trusted to show.
func (view *sftpView) forgetListings() {
	clear(view.listings)
	view.listings = view.listings[:0]
	view.listingBytes = 0
	// A listing already being read ahead may have observed the state before the
	// remote change. Forget its token so its late result cannot repopulate the
	// cache.
	clear(view.prefetching)
}

// openSFTPDirectory goes into a folder. One already read is shown at once and
// read again behind the scenes; one being read ahead is joined rather than read
// twice; any other is listed.
func (state *State) openSFTPDirectory(view *sftpView, remotePath string) {
	key := path.Clean(remotePath)
	nav := state.beginSFTPNavigation(view, false, key, sftpNavigationPassive, 0)
	state.openSFTPDirectoryNavigation(view, remotePath, nav)
}

func (state *State) openSFTPDirectoryNavigation(view *sftpView, remotePath string, nav sftpNavigation) {
	key := path.Clean(remotePath)
	if entries, ok := view.listing(key); ok {
		state.showSFTPListingNavigation(view, key, entries, nav, 0)
		state.prefetchSFTPRefresh(view, key)
		return
	}
	if id, inFlight := view.prefetching[key]; inFlight {
		view.busy, view.operation, view.waiting = true, sftpclient.OperationList, key
		view.waitingID, view.waitingNavigation = id, nav
		view.startListingStatus(state.messages.SFTPLoading)
		view.errorText = ""
		return
	}
	state.queueSFTPListing(view, remotePath, nav)
}

func (view *sftpView) markPrefetching(key string, id uint64) {
	if view.prefetching == nil {
		view.prefetching = make(map[string]uint64, 4)
	}
	view.prefetching[key] = id
}

// prefetchSFTP reads a folder before it is opened, so that the click that opens
// it finds the work done or under way.
func (state *State) prefetchSFTP(view *sftpView, remotePath string) {
	key := path.Clean(remotePath)
	if _, ok := view.listing(key); ok {
		return
	}
	if _, inFlight := view.prefetching[key]; inFlight {
		return
	}
	state.prefetchSFTPRefresh(view, key)
}

func (state *State) prefetchSFTPRefresh(view *sftpView, key string) {
	if _, ok := view.prefetching[key]; ok || view.worker == nil {
		return
	}
	view.navigationID++
	id := view.navigationID
	if view.worker.PrefetchWithID(key, id) {
		view.markPrefetching(key, id)
	}
}

// refreshSFTPAfterRemoteSave refreshes the directory being shown after a
// successful remote save. If the saved file belongs to another remembered
// directory, drop the cached listings so that opening it cannot show stale
// metadata.
func (state *State) refreshSFTPAfterRemoteSave(view *sftpView, remotePath string) {
	if view == nil || view.worker == nil {
		return
	}
	directory := path.Dir(remotePath)
	if view.path == directory {
		if view.busy && view.navigation.id != 0 && view.navigation.path == directory {
			// A listing that started before the save is still in flight. Read
			// the folder again after it lands, or its older metadata would be
			// what stays on screen.
			view.refreshAfterListing = directory
			return
		}
		// Force a second read even when a prefetch was already in flight. That
		// read may have started before the save and must not be allowed to put
		// the old metadata back into the cache.
		state.refreshSFTPDirectory(view, directory)
		return
	}
	view.forgetListings()
}

// refreshSFTPDirectory reads a shown folder again without disturbing the view.
func (state *State) refreshSFTPDirectory(view *sftpView, directory string) {
	view.navigationID++
	id := view.navigationID
	if view.worker.PrefetchWithID(directory, id) {
		view.markPrefetching(directory, id)
	}
}

// showSFTPListing puts a folder on screen.
func (state *State) showSFTPListing(view *sftpView, remotePath string, entries []sftpclient.Entry) {
	state.showSFTPListingNavigation(view, remotePath, entries, sftpNavigation{}, 0)
}

func (state *State) showSFTPListingNavigation(view *sftpView, remotePath string, entries []sftpclient.Entry, nav sftpNavigation, listID uint64) {
	if view.deferDraggedListing(false, func() { state.showSFTPListingNavigation(view, remotePath, entries, nav, listID) }) {
		return
	}
	if nav.id != 0 && nav.id != view.navigation.id {
		return
	}
	state.applySFTPListingNavigation(view, remotePath, prepareSFTPRows(view.allRows, entries), nav, listID)
}

// applySFTPListing puts prepared rows on screen. It is separate from the row
// preparation so a large folder can prepare them on a goroutine.
func (state *State) applySFTPListing(view *sftpView, remotePath string, rows []sftpRow) {
	state.applySFTPListingNavigation(view, remotePath, rows, sftpNavigation{}, 0)
}

func (state *State) applySFTPListingNavigation(view *sftpView, remotePath string, rows []sftpRow, nav sftpNavigation, listID uint64) {
	defer view.invalidateDetached()
	if view.deferDraggedListing(false, func() { state.applySFTPListingNavigation(view, remotePath, rows, nav, listID) }) {
		return
	}
	if nav.id != 0 && nav.id != view.navigation.id {
		return
	}
	place, restore := view.sftpFolder.acceptNavigation(remotePath, nav)
	view.acceptedListID = listID
	view.preparingID = 0
	view.remoteSeq++ // a shown listing invalidates an older one still being prepared
	view.busy = false
	view.path = remotePath
	if view.refreshAfterListing == remotePath {
		// A save landed while this listing was in flight; read the folder again
		// so the older metadata it brought back is not the last word.
		view.refreshAfterListing = ""
		state.refreshSFTPDirectory(view, remotePath)
	}
	view.allRows = rows
	view.crumbs = prepareSFTPCrumbs(view.crumbs, remotePath)
	state.applySFTPFilter(view, &view.sftpFolder, true)
	state.finishSFTPNavigation(view, false, place, restore)
	view.errorText = ""
	if view.refreshStatus != "" {
		view.status = view.refreshStatus
		view.refreshStatus = ""
	} else if view.detached != nil {
		if view.approximate {
			view.setItemCountStatus(state.messages.SCPItemsFmt, len(view.rows))
		} else {
			view.setItemCountStatus(state.messages.SFTPItemsFmt, len(view.rows))
		}
	} else {
		state.refreshSFTPFilterStatus(view, &view.sftpFolder)
	}
	state.syncTerminalDirectory(view)
	if view.detached != nil {
		view.detached.browseListingCommitted(false, remotePath)
	}
}

// finishSFTPPrefetch takes in a folder read ahead of need: it is remembered, shown
// if the user has meanwhile asked for it, and, if it is the folder on screen,
// used to correct what the screen shows from an earlier reading.
func (state *State) finishSFTPPrefetch(view *sftpView, result sftpclient.Result) {
	if result.ListID != 0 && view.prefetching[result.Path] != result.ListID {
		return
	}
	delete(view.prefetching, result.Path)
	waited := view.waiting != "" && view.waiting == result.Path && (result.ListID == 0 || result.ListID == view.waitingID)
	nav := view.waitingNavigation
	if waited {
		view.waiting = ""
		view.waitingID, view.waitingNavigation = 0, sftpNavigation{}
		view.busy = false
	}
	if result.Err != nil {
		if waited {
			view.navigation = sftpNavigation{}
			view.terminalPath = ""
			if errors.Is(result.Err, sftpclient.ErrCanceled) {
				view.status = state.messages.SFTPCanceled
			} else {
				view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err)
			}
		}
		return
	}
	view.remember(result.Path, result.Entries)
	switch {
	case waited:
		state.showSFTPListingNavigation(view, result.Path, result.Entries, nav, result.ListID)
	case view.path == result.Path && !view.busy:
		view.acceptedListID = result.ListID
		if !sameSFTPListing(view.allRows, result.Entries) {
			state.updateSFTPRows(view, result.Entries)
		}
	}
}

// updateSFTPRows swaps in a newer reading of the folder on screen without
// losing the user's place: the same names stay selected and the list keeps its
// scroll position as far as it still reaches.
func (state *State) updateSFTPRows(view *sftpView, entries []sftpclient.Entry) {
	defer view.invalidateDetached()
	if view.deferDraggedListing(false, func() { state.updateSFTPRows(view, entries) }) {
		return
	}
	var selected map[string]struct{}
	for index := range view.rows {
		if view.rows[index].selected {
			if selected == nil {
				selected = make(map[string]struct{})
			}
			selected[view.rows[index].entry.Name] = struct{}{}
		}
	}
	view.allRows = prepareSFTPRows(view.allRows, entries)
	for index := range view.allRows {
		if _, ok := selected[view.allRows[index].entry.Name]; ok {
			view.allRows[index].selected = true
			view.allRows[index].cells[0] = sftpSelectionCell(true)
		}
	}
	state.applySFTPFilter(view, &view.sftpFolder, false)
	if view.detached != nil {
		view.setItemCountStatus(state.messages.SFTPItemsFmt, len(view.rows))
	}
}

func sameSFTPListing(rows []sftpRow, entries []sftpclient.Entry) bool {
	if len(rows) != len(entries) {
		return false
	}
	for index := range rows {
		left, right := rows[index].entry, entries[index]
		if left.Name != right.Name || left.Size != right.Size || left.Mode != right.Mode || !left.ModTime.Equal(right.ModTime) {
			return false
		}
	}
	return true
}

func (state *State) listSFTP(view *sftpView, remotePath string) {
	nav := state.beginSFTPNavigation(view, false, remotePath, sftpNavigationPassive, 0)
	state.queueSFTPListing(view, remotePath, nav)
}

func (state *State) queueSFTPListing(view *sftpView, remotePath string, nav sftpNavigation) {
	if view.worker != nil && view.worker.ListWithID(remotePath, nav.id) {
		view.busy = true
		view.operation = sftpclient.OperationList
		view.startListingStatus(state.messages.SFTPLoading)
		view.errorText = ""
	} else {
		view.navigation = sftpNavigation{}
		view.busy = false
	}
}

func (state *State) downloadSFTP(view *sftpView) {
	selected := selectedSFTPRows(view)
	if len(selected) == 0 {
		return
	}
	configuredDirectory := state.sftpDownloadDirectory
	if directory, ok := state.ensureSFTPDownloadDirectory(view); ok {
		for index := range selected {
			state.queueSFTPDownload(view, selected[index].entry, filepath.Join(directory, safeLocalName(selected[index].entry.Name)))
		}
		return
	}
	if len(selected) == 1 && !selected[0].entry.IsDir() {
		entry := selected[0].entry
		extension := strings.TrimPrefix(path.Ext(entry.Name), ".")
		localPath, err := state.options.SaveFile(app.SaveFileOptions{
			Title: state.messages.SFTPSaveTitle, SuggestedName: entry.Name, DefaultExtension: extension,
		})
		if state.handleSFTPPickerError(view, err, state.messages.SFTPSaveCanceled) {
			return
		}
		if configuredDirectory != "" {
			state.rememberSFTPDownloadDirectory(filepath.Dir(localPath))
		}
		state.queueSFTPDownload(view, entry, localPath)
		return
	}
	localDirectory, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.SFTPDownloadDirectoryTitle})
	if state.handleSFTPPickerError(view, err, state.messages.SFTPSaveCanceled) {
		return
	}
	if configuredDirectory != "" {
		state.rememberSFTPDownloadDirectory(localDirectory)
	}
	for index := range selected {
		state.queueSFTPDownload(view, selected[index].entry, filepath.Join(localDirectory, safeLocalName(selected[index].entry.Name)))
	}
}

func (state *State) ensureSFTPDownloadDirectory(view *sftpView) (string, bool) {
	directory := state.sftpDownloadDirectory
	if directory == "" {
		return "", false
	}
	if !validSFTPDownloadDirectory(directory) {
		state.fileOpenNotice(
			view.detached,
			state.messages.Tools.FilesDownloadDirectory,
			fmt.Sprintf(state.messages.Tools.FilesDownloadDirectoryUnavailableFmt, directory),
			components.ToastWarning,
		)
		return "", false
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		state.fileOpenNotice(
			view.detached,
			state.messages.Tools.FilesDownloadDirectory,
			fmt.Sprintf(state.messages.Tools.FilesDownloadDirectoryUnavailableFmt, directory),
			components.ToastWarning,
		)
		return "", false
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		state.fileOpenNotice(
			view.detached,
			state.messages.Tools.FilesDownloadDirectory,
			fmt.Sprintf(state.messages.Tools.FilesDownloadDirectoryUnavailableFmt, directory),
			components.ToastWarning,
		)
		return "", false
	}
	return directory, true
}

func (state *State) rememberSFTPDownloadDirectory(directory string) {
	directory = filepath.Clean(directory)
	if !validSFTPDownloadDirectory(directory) || state.sftpDownloadDirectory == directory {
		return
	}
	state.sftpDownloadDirectory = directory
	state.save()
}

func (state *State) handleSFTPPickerError(view *sftpView, err error, canceled string) bool {
	if errors.Is(err, app.ErrCanceled) {
		view.status = canceled
		return true
	}
	if err != nil {
		view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, err)
		return true
	}
	return false
}

func (state *State) queueSFTPDownload(view *sftpView, entry sftpclient.Entry, localPath string) {
	_, queued := view.worker.QueueDownload(path.Join(view.path, entry.Name), localPath, entry.IsDir(), sftpclient.ExistingFail)
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

func (state *State) uploadSFTP(view *sftpView) {
	localPaths, err := state.options.OpenFiles(app.OpenFileOptions{Title: state.messages.SFTPUploadTitle, AllowMultiple: true})
	if state.handleSFTPPickerError(view, err, state.messages.SFTPUploadCanceled) {
		return
	}
	if len(localPaths) == 0 {
		view.status = state.messages.SFTPUploadCanceled
		return
	}
	for _, localPath := range localPaths {
		state.queueSFTPUpload(view, localPath, false)
	}
}

func (state *State) uploadSFTPDirectory(view *sftpView) {
	localPath, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.SFTPUploadDirectoryTitle})
	if state.handleSFTPPickerError(view, err, state.messages.SFTPUploadCanceled) {
		return
	}
	state.queueSFTPUpload(view, localPath, true)
}

func (state *State) queueSFTPUpload(view *sftpView, localPath string, recursive bool) {
	state.queueSFTPUploadTo(view, localPath, recursive, view.path)
}

// queueSFTPUploadTo uploads localPath into the server's folder remoteDir.
// queueSFTPUploadTo queues one upload and returns the id of its transfer, or
// zero when the worker would not take it.
func (state *State) queueSFTPUploadTo(view *sftpView, localPath string, recursive bool, remoteDir string) uint64 {
	name := filepath.Base(filepath.Clean(localPath))
	remotePath := path.Join(remoteDir, name)
	id, queued := view.worker.QueueUpload(localPath, remotePath, recursive, sftpclient.ExistingFail)
	if !queued {
		view.errorText = state.messages.SFTPUnavailable
		return 0
	}
	view.status = fmt.Sprintf(state.messages.SFTPQueuedFmt, name)
	view.errorText = ""
	state.transfers.open, state.transfers.filter, state.transfers.scroll = true, 0, 0
	state.transfers.host = nil
	state.refreshSFTPTransfers(view)
	return id
}

func (state *State) refreshSFTPTransfers(view *sftpView) {
	view.transfers = view.worker.Transfers(view.transfers)
	state.syncTransfers(view)
}

func (state *State) openSFTPDialog(view *sftpView, dialog sftpDialogKind) {
	view.dialogEditor = text.Editor{}
	view.dialogEntry = sftpclient.Entry{}
	view.dialogError = ""
	if dialog != sftpDialogCreateDirectory {
		entry, ok := state.selectedSFTPEntry(view)
		if !ok {
			return
		}
		view.dialogEntry = entry
	}
	switch dialog {
	case sftpDialogRename:
		view.dialogEditor.Insert(path.Join(view.path, view.dialogEntry.Name))
	case sftpDialogChmod:
		view.dialogEditor.Restrict(func(value rune) bool { return value >= '0' && value <= '7' }, 3)
		view.dialogEditor.Insert(fmt.Sprintf("%03o", view.dialogEntry.Mode.Perm()))
	}
	view.dialog = dialog
}

func (state *State) closeSFTPDialog(view *sftpView) {
	if view.dialog == sftpDialogBrowseCreateDirectory && view.detached != nil {
		view.detached.cancelBrowseDirectoryPrompt()
	}
	view.dialog = sftpDialogNone
	view.dialogEditor, view.dialogEditor2 = text.Editor{}, text.Editor{}
	view.dialogEntry = sftpclient.Entry{}
	view.dialogError = ""
}

func (state *State) clickSFTPDialog(view *sftpView, id ui.ID) {
	switch id {
	case hitSFTPDialogDismiss, hitSFTPDialogClose, hitSFTPDialogCancel:
		state.closeSFTPDialog(view)
	case hitSFTPDialogSubmit:
		state.submitSFTPDialog(view)
	}
}

func (state *State) submitSFTPDialog(view *sftpView) {
	value := strings.TrimSpace(view.dialogEditor.String())
	queued := false
	operation := sftpclient.Operation(0)
	status := ""
	switch view.dialog {
	case sftpDialogBrowseCreateDirectory:
		if view.detached != nil {
			view.detached.createBrowseDirectory()
		}
		return
	case sftpDialogDiscardEdit:
		state.closeSFTPDialog(view)
		resetSFTPEditor(view)
		return
	case sftpDialogRemoteChanged:
		state.queueSFTPEditReload(view)
		return
	case sftpDialogReloadEdit:
		state.queueSFTPEditReload(view)
		return
	}
	switch view.dialog {
	case sftpDialogSymlink:
		state.submitSFTPLink(view)
		return
	case sftpDialogExtract:
		state.submitSFTPExtract(view)
		return
	case sftpDialogCompress:
		state.submitSFTPCompress(view)
		return
	case sftpDialogCreateDirectory:
		if view.local {
			state.submitLocalDirectory(view, value)
			return
		}
		if value == "" {
			view.dialogError = state.messages.SFTPErrDirectoryRequired
			return
		}
		remotePath := sftpRemotePath(view.path, value)
		queued = view.worker.CreateDirectory(remotePath)
		operation = sftpclient.OperationCreateDirectory
		status = fmt.Sprintf(state.messages.SFTPCreatingDirectoryFmt, remotePath)
	case sftpDialogRename:
		if value == "" {
			view.dialogError = state.messages.SFTPErrDestinationRequired
			return
		}
		destination := sftpRemotePath(view.path, value)
		queued = view.worker.Rename(path.Join(view.path, view.dialogEntry.Name), destination)
		operation = sftpclient.OperationRename
		status = fmt.Sprintf(state.messages.SFTPRenamingFmt, view.dialogEntry.Name)
	case sftpDialogDelete:
		remotePath := path.Join(view.path, view.dialogEntry.Name)
		if view.dialogEntry.IsDir() {
			queued = view.worker.RemoveDirectory(remotePath)
			operation = sftpclient.OperationRemoveDirectory
		} else {
			queued = view.worker.RemoveFile(remotePath)
			operation = sftpclient.OperationRemoveFile
		}
		status = fmt.Sprintf(state.messages.SFTPDeletingFmt, view.dialogEntry.Name)
	case sftpDialogChmod:
		mode, err := strconv.ParseUint(value, 8, 12)
		if err != nil || mode > 0o777 {
			view.dialogError = state.messages.SFTPErrModeInvalid
			return
		}
		remotePath := path.Join(view.path, view.dialogEntry.Name)
		queued = view.worker.Chmod(remotePath, os.FileMode(mode))
		operation = sftpclient.OperationChmod
		status = fmt.Sprintf(state.messages.SFTPChangingPermissionsFmt, view.dialogEntry.Name)
	}
	if !queued {
		view.dialogError = state.messages.SFTPUnavailable
		return
	}
	state.closeSFTPDialog(view)
	view.busy = true
	view.operation = operation
	view.status = status
	view.errorText = ""
}

func sftpRemotePath(current, value string) string {
	if path.IsAbs(value) {
		return path.Clean(value)
	}
	return path.Join(current, value)
}

func (state *State) pumpSFTP(session *Session) {
	if session.files == nil {
		return
	}
	view := session.files
	state.pumpSFTPArchive(session, view)
	state.pumpSFTPEditor(session, view)
	defer func() {
		// A list or transfer reply must not unlock the panel while an archive
		// command is still using its captured source and destination.
		if view.extracting {
			view.busy = true
		}
	}()
	if view.worker == nil {
		return
	}
	state.refreshSFTPTransfers(view)
	for {
		select {
		case result, ok := <-view.worker.Results():
			if !ok {
				return
			}
			if result.Operation == sftpclient.OperationList && !result.Background && !result.FollowUp && result.ListID != 0 && result.ListID != view.navigation.id {
				if result.Path == view.abandonedPath {
					view.abandonedPath = ""
					if view.navigation.id == 0 {
						view.busy = false
					}
				}
				continue
			}
			if result.Operation == sftpclient.OperationList && result.Err == nil {
				view.approximate = result.Approximate
			}
			if result.FollowUp {
				if result.ListID != 0 {
					shown := result.ListID == view.acceptedListID && result.Path == view.path && !view.busy
					preparing := result.ListID == view.preparingID && result.Path == view.preparingPath
					if !shown && !preparing {
						continue
					}
				}
				// The links of a listing resolved after it was shown. Refresh the
				// rows without touching the selection or the scroll position; a
				// listing already in flight supersedes this one.
				view.remember(result.Path, result.Entries)
				if view.path == result.Path && !view.busy {
					state.updateSFTPRows(view, result.Entries)
				} else if view.preparingPath == result.Path && (result.ListID == 0 || result.ListID == view.preparingID) {
					// The first rows are still being prepared; the follow-up has the
					// targets and must win over the initial entries.
					view.followUpPath, view.followUpEntries = result.Path, result.Entries
				}
				continue
			}
			if result.Background {
				state.finishSFTPPrefetch(view, result)
				continue
			}
			if state.externalEditResult(session, view, result) {
				continue
			}
			if result.TransferID != 0 {
				if result.Err != nil {
					if errors.Is(result.Err, sftpclient.ErrCanceled) {
						view.status = state.messages.SFTPCanceled
					} else if errors.Is(result.Err, sftpclient.ErrDestinationExists) {
						if view.detached == nil {
							state.transfers.open = true
						}
					} else {
						view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err)
					}
				} else if result.Operation == sftpclient.OperationUpload {
					view.refreshAfterTransfers = true
					view.refreshStatus = fmt.Sprintf(state.messages.SFTPUploadedFmt, formatBytes(result.Bytes), result.Path)
					view.status = view.refreshStatus
				} else {
					view.status = fmt.Sprintf(state.messages.SFTPDownloadedFmt, formatBytes(result.Bytes), result.LocalPath)
				}
				state.refreshSFTPTransfers(view)
				continue
			}
			if view.detached != nil && view.detached.browseDirectoryCreated(result) {
				continue
			}
			view.busy = false
			if result.Operation == sftpclient.OperationList && result.ListID == 0 && view.abandonedPath != "" && result.Path == view.abandonedPath {
				// The user canceled this navigation: keep the listing they are
				// looking at, whatever the late result says.
				view.abandonedPath = ""
				continue
			}
			if result.Operation == sftpclient.OperationList && result.Err != nil && result.Path == view.terminalPath {
				view.terminalPath = ""
			}
			if result.Operation == sftpclient.OperationList && result.Err != nil && view.detached != nil && view.detached.browseListingFailed(false, result.Path, result.Err) {
				continue
			}
			if result.Operation == sftpclient.OperationList && view.pathRequest != "" && result.Path == view.pathRequest {
				view.pathRequest = ""
				if result.Err != nil && !errors.Is(result.Err, sftpclient.ErrCanceled) {
					view.navigation = sftpNavigation{}
					state.showSFTPPathErrorAt(view, result.Path, result.Err)
					view.errorText = ""
					view.setItemCountStatus(state.messages.SFTPItemsFmt, len(view.active().rows))
					continue
				}
				if result.Err == nil && !state.sftpPathInput {
					view.sftpFolder.pathEditing = false
				}
			}
			if result.Err != nil {
				if result.Operation == sftpclient.OperationList {
					view.navigation = sftpNavigation{}
				}
				view.refreshStatus = ""
				if errors.Is(result.Err, sftpclient.ErrCanceled) {
					view.status = state.messages.SFTPCanceled
				} else if errors.Is(result.Err, sftpclient.ErrRemoteChanged) {
					view.dialog = sftpDialogRemoteChanged
					view.errorText = state.messages.SFTPEditConflict
				} else {
					view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err)
				}
				continue
			}
			switch result.Operation {
			case sftpclient.OperationList:
				nav := view.navigation
				view.remember(result.Path, result.Entries)
				view.remoteSeq++
				view.preparingPath, view.followUpPath, view.followUpEntries = "", "", nil
				if len(result.Entries) > sftpPrepInlineEntries {
					view.busy = true // navigation completes only when its prepared rows are published
					seq, entries := view.remoteSeq, result.Entries
					view.preparingPath = result.Path
					view.preparingID = result.ListID
					view.startListingStatus(state.messages.SFTPLoading)
					go func() {
						rows := prepareSFTPRows(nil, entries)
						state.aiPost(func(*State) {
							if seq != view.remoteSeq {
								return
							}
							if view.followUpPath == result.Path && view.followUpEntries != nil {
								rows = prepareSFTPRows(nil, view.followUpEntries)
							}
							view.preparingPath, view.followUpPath, view.followUpEntries = "", "", nil
							state.applySFTPListingNavigation(view, result.Path, rows, nav, result.ListID)
						})
					}()
					continue
				}
				state.showSFTPListingNavigation(view, result.Path, result.Entries, nav, result.ListID)
			case sftpclient.OperationDownload:
				view.status = fmt.Sprintf(state.messages.SFTPDownloadedFmt, formatBytes(result.Bytes), result.LocalPath)
			case sftpclient.OperationUpload:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPUploadedFmt, formatBytes(result.Bytes), result.Path)
				state.listSFTP(view, view.path)
			case sftpclient.OperationCreateDirectory:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPDirectoryCreatedFmt, result.Path)
				state.listSFTP(view, view.path)
			case sftpclient.OperationRename:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPRenamedFmt, result.Path, result.Destination)
				state.listSFTP(view, view.path)
			case sftpclient.OperationRemoveFile, sftpclient.OperationRemoveDirectory:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPDeletedFmt, result.Path)
				state.listSFTP(view, view.path)
			case sftpclient.OperationSymlink:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.Tools.LinkCreatedFmt, path.Base(result.Path), result.Destination)
				state.listSFTP(view, view.path)
			case sftpclient.OperationChmod:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPPermissionsChangedFmt, result.Path, result.Mode.Perm())
				state.listSFTP(view, view.path)
			case sftpclient.OperationOpenText:
				view.editPath = result.Path
				view.editName = path.Base(result.Path)
				view.editLocation = session.Address + " · " + result.Path
				view.editOriginal = result.Document.Text
				view.editRevision = result.Document.Revision
				view.editUTF8BOM = result.Document.UTF8BOM
				view.editEditor = text.Editor{}
				view.editEditor.Insert(result.Document.Text)
				view.status = state.messages.SFTPEditReady
				view.errorText = ""
			case sftpclient.OperationSaveText:
				view.editOriginal = result.Document.Text
				view.editRevision = result.Document.Revision
				view.editUTF8BOM = result.Document.UTF8BOM
				view.status = state.messages.SFTPEditSaved
				view.errorText = ""
				state.fileOpenNotice(view.detached, view.editName, state.messages.SFTPEditSaved, components.ToastSuccess)
				state.refreshSFTPAfterRemoteSave(view, result.Path)
			}
		default:
			state.refreshSFTPTransfers(view)
			if view.refreshAfterTransfers && !view.busy && !sftpTransfersActive(view.transfers) {
				view.refreshAfterTransfers = false
				view.forgetListings()
				state.listSFTP(view, view.path)
			}
			return
		}
	}
}

func sftpTransfersActive(transfers []sftpclient.TransferSnapshot) bool {
	for index := range transfers {
		if state := transfers[index].State; state == sftpclient.TransferQueued || state == sftpclient.TransferRunning || state == sftpclient.TransferPaused {
			return true
		}
	}
	return false
}

func prepareSFTPRows(rows []sftpRow, entries []sftpclient.Entry) []sftpRow {
	if cap(rows) < len(entries) {
		rows = make([]sftpRow, len(entries))
	} else {
		rows = rows[:len(entries)]
	}
	for index, entry := range entries {
		kind := entry.Mode.String()
		size := formatBytes(entry.Size)
		if entry.IsDir() {
			size = "—"
		}
		icon, mono := sftpFileIcon(entry.Name, entry.Mode)
		rows[index] = sftpRow{
			entry: entry, icon: icon, mono: mono, hidden: strings.HasPrefix(entry.Name, "."),
			cells: [5]string{"", entry.Name, size, entry.ModTime.Local().Format("2006/01/02 15:04"), kind},
		}
	}
	return rows
}

func prepareSFTPCrumbs(crumbs []sftpCrumb, remotePath string) []sftpCrumb {
	crumbs = crumbs[:0]
	cleaned := path.Clean(remotePath)
	if strings.HasPrefix(cleaned, "/") {
		crumbs = append(crumbs, sftpCrumb{label: "/", path: "/"})
	}
	current := ""
	for _, part := range strings.Split(strings.Trim(cleaned, "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		current = path.Join(current, part)
		if strings.HasPrefix(cleaned, "/") {
			current = "/" + strings.TrimPrefix(current, "/")
		}
		crumbs = append(crumbs, sftpCrumb{label: part, path: current})
	}
	return crumbs
}

func formatBytes(size int64) string {
	if size < 1024 {
		return strconv.FormatInt(size, 10) + " B"
	}
	units := [...]string{"KB", "MB", "GB", "TB"}
	value := float64(size)
	unit := units[0]
	for index := 0; index < len(units); index++ {
		value /= 1024
		unit = units[index]
		if value < 1024 || index == len(units)-1 {
			break
		}
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + unit
}

// scrollSFTP moves the file list. As everywhere in Viva a positive delta reveals
// rows below; trackpad travel smaller than a row is kept until it adds up.
func (state *State) scrollSFTP(view *sftpView, event ui.ScrollEvent) ui.EventResult {
	if view == nil {
		return ui.EventResult{Handled: true}
	}
	if view.bookmarksOpen {
		return ui.EventResult{Handled: true, Repaint: view.places.scroll.Wheel(event)}
	}
	if view.editPath != "" && pointInRect(event.X, event.Y, view.editRect) {
		view.editLayout.ScrollBy(event.DeltaY)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	folder := view.active()
	if folder.visibleRows <= 0 || len(folder.rows) <= folder.visibleRows {
		return ui.EventResult{Handled: true}
	}
	// At the edges, continuing to scroll outward is a no-op that must not repaint
	// or jitter the layout.
	maxOffset := folder.scroll.MaxOffsetY
	if maxOffset > 0 && folder.scrollOffset >= maxOffset && event.DeltaY > 0 {
		folder.wheel = 0
		return ui.EventResult{Handled: true, Repaint: false}
	}
	if folder.first <= 0 && folder.scrollOffset > 0 && event.DeltaY < 0 {
		// A drag released inside the first row leaves a sub-row offset there;
		// the wheel moves whole rows, so scrolling up would otherwise stall.
		folder.wheel = 0
		folder.scrollOffset = 0
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if folder.scrollOffset <= 0 && folder.first <= 0 && event.DeltaY < 0 {
		folder.wheel = 0
		return ui.EventResult{Handled: true, Repaint: false}
	}
	result := scrollFolder(folder, event)
	if result.Repaint {
		folder.scrollOffset = float32(folder.first) * sftpRowHeight
		if maxOffset > 0 && folder.scrollOffset > maxOffset {
			folder.scrollOffset = maxOffset
		}
	}
	return result
}

// fitSFTP keeps every list's first row inside its rows after the layout moved.
func (state *State) fitSFTP(size ui.Size) {
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil {
			view.first = min(view.first, max(0, len(view.rows)-view.visibleRows))
			view.lfolder.first = min(view.lfolder.first, max(0, len(view.lfolder.rows)-view.lfolder.visibleRows))
		}
	}
}

func (state *State) sftpKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if result, handled := state.sftpPlacesKey(event); handled {
		return result, true
	}
	if result, handled := state.sftpHistoryKey(event); handled {
		return result, true
	}
	if result, handled := state.sftpPathKey(event); handled {
		return result, true
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if ok && session.files != nil && state.modal == modalNone && event.Target == hitSFTPEditor && session.files.editPath != "" && session.files.dialog == sftpDialogNone {
		// Up, Down, Home, End and the page keys move by the rows the editor wrapped
		// to, and repeat while held.
		if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
			view := session.files
			value := view.editEditor.String()
			to := view.editLayout.Move(value, aiByteOffset(value, view.editEditor.Caret()), movement)
			view.editEditor.MoveTo(utf8.RuneCountInString(value[:to]), extend)
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	if !ok || session.files == nil || state.modal != modalNone || event.Repeat {
		return ui.EventResult{}, false
	}
	view := session.files
	if view.dialog != sftpDialogNone {
		switch event.Key {
		case ui.KeyEscape:
			state.closeSFTPDialog(view)
			return ui.EventResult{Handled: true, Repaint: true}, true
		case ui.KeyEnter:
			if event.Target == hitSFTPDialogField || event.Target == hitSFTPDialogField2 || event.Target == hitSFTPDialogSubmit {
				if view.dialog == sftpDialogCompress || view.dialog == sftpDialogExtract {
					if editor := state.sftpEditor(event.Target); editor != nil {
						if _, composing := editor.Composition(); composing {
							return ui.EventResult{Handled: true}, true
						}
					}
				}
				state.submitSFTPDialog(view)
				return ui.EventResult{Handled: true, Repaint: true}, true
			}
		}
		return ui.EventResult{}, false
	}
	if view.editPath != "" {
		if event.Key == ui.KeyEscape {
			state.closeSFTPEditor(view)
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return ui.EventResult{}, false
	}
	switch event.Key {
	case ui.KeyEscape:
		if folder := view.active(); folder.filterOpen && event.Target == hitSFTPFilter {
			// Escape leaves the filter, not the file panel: the first press drops
			// the query, the next closes the row, as a find bar does.
			if folder.filterInputEditor().String() != "" {
				folder.rememberSelection()
				folder.clearFilter()
				state.applySFTPFilter(view, folder, false)
				return ui.EventResult{Handled: true, Repaint: true, Focus: hitSFTPFilter, FocusVisible: true}, true
			}
			folder.filterOpen = false
			result := ui.EventResult{Handled: true, Repaint: true}
			if len(folder.rows) > 0 {
				result.Focus, result.FocusVisible = hitSFTPEntryList.At(0), true
			}
			return result, true
		}
		if view.bookmarksOpen {
			view.bookmarksOpen = false
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		state.clickSFTP(hitSFTPClose)
		return ui.EventResult{Handled: true, Repaint: true}, true
	case ui.KeyEnter:
		if event.Target.Scope() == hitSFTPEntryList {
			folder := view.active()
			index := folder.first + item(event.Target)
			if index < len(folder.rows) {
				folder.selected = index
				if view.local {
					state.openLocalSelected(view)
				} else {
					state.openSelectedSFTP(view)
				}
			}
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return ui.EventResult{}, false
}

func (state *State) sftpPathPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary || event.Target != hitSFTPPathEdit {
		return ui.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || state.activeSection != sectionTerminal || state.modal != modalNone || session.files.editPath != "" || session.files.dialog != sftpDialogNone || !session.files.local && session.files.busy {
		return ui.EventResult{}, false
	}
	result := state.startSFTPPathEditing(session.files)
	if !result.Handled {
		return ui.EventResult{}, false
	}
	return result, true
}

// sftpRowPointer opens the row that was double-clicked: a folder is entered, a
// file is opened for editing. The first click of the pair has already selected
// the row, so what opens is what was pointed at.
func (state *State) sftpRowPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button == ui.PointerButtonSecondary || event.ClickCount < 1 ||
		event.Target.Scope() != hitSFTPEntryList {
		return ui.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil {
		return ui.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	index := folder.first + item(event.Target)
	if view.dialog != sftpDialogNone || index >= len(folder.rows) || view.busy && !view.local {
		return ui.EventResult{Handled: true}, true
	}
	if event.ClickCount == 1 {
		// A press, not yet half of a double click. Handling it here keeps the
		// runtime from also sending a click that would toggle the row again.
		folder.selectRow(index, event.Modifiers)
		view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
		if row := folder.rows[index]; row.selected && row.entry.IsDir() && !view.local && selectedSFTPCount(view) == 1 {
			// The first click of a double-click: start reading the folder now, so
			// the second finds the work done or under way.
			state.prefetchSFTP(view, path.Join(view.path, row.entry.Name))
		}
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	folder.selectRow(index, 0)
	if view.local {
		state.openLocalSelected(view)
	} else {
		state.openSelectedSFTP(view)
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}
