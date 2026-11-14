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

	"github.com/pkg/sftp"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	icon                 *gfx.Icon     // the file-type glyph, chosen when the listing arrives
	mono                 bool          // the glyph has no colors of its own and is tinted
	appIcon              *fileIconSlot // the system's icon for a local application bundle, once it has answered
	typeIcon             *fileIconSlot // the system's icon for this file type, shared by every row with the extension
	hidden               bool
	matchStart, matchEnd int // byte range highlighted by the current folder filter
	// where is the folder a search result is in, relative to where the search
	// began; "" for a row of the folder itself.
	where string
}

type sftpDialogKind uint8

const (
	sftpDialogNone sftpDialogKind = iota
	sftpDialogCreateDirectory
	sftpDialogRename
	sftpDialogMove
	sftpDialogDelete
	sftpDialogChmod
	sftpDialogChown
	sftpDialogDiscardEdit
	sftpDialogReloadEdit
	sftpDialogRemoteChanged
	sftpDialogSymlink
	sftpDialogExtract
	sftpDialogCompress
	sftpDialogBrowseCreateDirectory
)

// sftpDialogShowsField reports whether the dialog edits a field: a folder, a
// name, a destination, an octal mode, an owner and a group, or a link.
func sftpDialogShowsField(kind sftpDialogKind) bool {
	switch kind {
	case sftpDialogCreateDirectory, sftpDialogRename, sftpDialogMove, sftpDialogChmod,
		sftpDialogChown, sftpDialogSymlink, sftpDialogExtract, sftpDialogCompress:
		return true
	}
	return false
}

// sftpFolder is a folder on show: where it is, its rows and the user's place in
// them. The panel has one for the server and one for this computer.
type sftpFolder struct {
	history      sftpHistory
	navigation   sftpNavigation
	path         string
	allRows      []sftpRow
	rows         []sftpRow
	crumbs       []sftpCrumb
	selected     int
	anchor       int // where a Shift-click range starts: the row last chosen without Shift
	first        int
	visibleRows  int     // rows the last paint had room for
	wheel        float32 // wheel travel not yet worth a whole row
	scroll       components.ScrollAreaGeometry
	scrollOffset float32
	// The row list is a Viva List: it keeps the scroll offset, the row the
	// keys are on and the multi-selection. selected/anchor/row.selected stay
	// the model the actions read; they are seeded into the List and mirrored
	// back out of it.
	list             ui.ListState
	selection        ui.Selection
	listRowsScope    gfx.Scope // the identity scope of its row elements, for events
	listNeedsSeed    bool      // the model's selection changed without the List
	listPendingFirst int       // a row the List should put at its top next paint, or -1
	listHeight       float32   // the list's height in the last paint
	listScroll       *ui.ScrollState
	listReady        bool
	pathField        sftpPathField
	filterEditor     text.Editor
	filterInput      app.TextInput
	filterReady      bool
	filterOpen       bool
	showHidden       bool
	pathEditing      bool
	headerMenuOpen   bool
	sortOrder        sftpWindowSort
	sortChosen       bool
	localCoverage    localListingCoverage
	coverageLabel    string // bounded scope, formatted when a listing/filter changes
	coverageText     string // full semantic warning, independent of ordinary feedback
	coverageEmpty    string
	hiddenMatch      bool // the query matches rows this folder hides
	// deep widens the filter to the folders below this one; search is the
	// reading of them, and searchShown says the list shows its results rather
	// than rows (see sftp_search.go). reveal is the entry to select in the
	// folder the next navigation lands in.
	deep        bool
	search      *sftpSearch
	searchShown bool
	reveal      sftpReveal
	cancelSize  context.CancelFunc
}

type sftpView struct {
	detached                        *sftpWindow // optional native dual-pane owner; transport/navigation stay per view
	worker                          *sftpclient.Worker
	openFiles                       func() (sftpclient.Remote, error) // the server's files again, outside the worker's queue; nil when unavailable
	sudo                            bool
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
	localBusy           bool // a local deletion is walking the disk; the server side stays usable
	approximate         bool // the listing came from `ls` over SCP
	operation           sftpclient.Operation
	status              string
	// listingStatus is the listing message status still holds, so a completed
	// listing can retire it without dropping the feedback that replaced it.
	listingStatus      string
	localListingStatus bool // the shared listing message belongs to a pending local read
	errorText          string
	refreshStatus      string
	dialog             sftpDialogKind
	dialogEditor       text.Editor
	dialogEditor2      text.Editor
	// dialogFieldLabel is the field title the open dialog's body declared; the
	// window paints the field under it after the body, inside the dialog's focus
	// scope. dialogDraw is that paint, kept once so a repaint makes no closure.
	// The footer's labels and draw are kept the same way: the dialog can stand
	// beside an application modal, so it does not share the window's footer
	// scratch. The boxes are where that body laid out the parts painted by hand:
	// the one or two fields and the permission grid.
	dialogFieldLabel    string
	dialogDraw          func(*ui.DrawContext)
	dialogCancelLabel   string
	dialogSubmitLabel   string
	dialogSubmitVariant components.ButtonVariant
	dialogFooterDraw    func(*ui.DrawContext)
	dialogFirst         ui.Box
	dialogSecond        ui.Box
	dialogChmod         [9]ui.Box
	// dialogViewport is the room the owning window gives the open dialog, used
	// to keep a field's suggestion list on screen.
	dialogViewport gfx.Rect
	ownership      sftpOwnershipUI
	bookmarksOpen  bool
	places         sftpPlaceMenu
	dropping       bool // files are being dragged over the panel
	dropRow        int  // the folder row they are over, or -1
	scrollDrag     bool
	scrollGrab     float32
	// columnDragging and columnDrag are the divider of this panel the pointer
	// holds; columnFitPending asks the next paint to fit that divider's left
	// column to its content after a double click.
	columnDragging        bool
	columnDrag            int
	columnFitPending      bool
	columnFitDivider      int
	details               sftpDetails
	extracting            bool
	archiveSequence       uint64
	archiveDirectory      string
	archiveResults        chan archiveResult
	editorResults         chan editorResult
	cancelExtract         context.CancelFunc
	dialogEntry           sftpclient.Entry
	dialogEntries         []sftpclient.Entry
	deleteRemaining       []sftpclient.Entry
	deleteCompleted       int
	dialogError           string
	moveAfterRename       bool
	transfers             []sftpclient.TransferSnapshot
	refreshAfterTransfers bool
	// remoteCopies names the source of each server-to-server copy this view
	// owns, so the transfer center can describe both ends.
	remoteCopies map[uint64]remoteCopyOrigin
	// outgoingCopies are the server-to-server copies reading from this view, by
	// the view that owns each task. They keep this connection open and are what
	// closing this view cancels.
	outgoingCopies []remoteCopyLink
	editPath       string
	editName       string
	editLocation   string
	editOriginal   string
	editRevision   sftpclient.Revision
	editUTF8BOM    bool
	editEditor     text.Editor
	editLayout     components.TextareaLayout // the editor's rows and scroll
	editRect       gfx.Rect                  // where the editor was painted, for the wheel

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
	view.openFiles = func() (sftpclient.Remote, error) { return openRemoteFiles(connection, transfer) }
	view.worker = sftpclient.NewWorker(view.openFiles, state.notifyFor(session))
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
	if state.modal == modalFileProperties && session.ID == state.activeSessionID {
		state.clickFileProperties(hitFilePropertiesClose)
	}
	session.files.localSeq++
	session.files.remoteSeq++
	session.files.navigation, session.files.lfolder.navigation = sftpNavigation{}, sftpNavigation{}
	stopSFTPSearch(&session.files.sftpFolder)
	stopSFTPSearch(&session.files.lfolder)
	session.files.sftpFolder.stopSizeCalculation()
	session.files.lfolder.stopSizeCalculation()
	state.cancelRemoteCopiesFrom(session.files)
	if session.files.cancelExtract != nil {
		session.files.cancelExtract()
	}
	if session.files.worker != nil {
		session.files.worker.Close()
		state.refreshSFTPTransfers(session.files)
		state.archiveTransfers(session.files)
	}
	if session.files.sudo {
		state.forgetSFTPDocumentAccess(session.files)
	}
	session.files.openFiles = nil
	session.files.sudo = false
	state.forgetTerminalUploads(session.files)
	session.files = nil
}

// filesPanelShown reports whether the file panel is open in the active
// workspace.
func (state *State) filesPanelShown() bool {
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	return context.Open && context.Kind == layoutcore.ContextSFTP
}

// showFilesPanel opens the file panel and its view. It does nothing when the
// view is already there, so callers that merely need the panel open can call it
// without toggling the panel shut.
func (state *State) showFilesPanel(session *Session) {
	workspace := state.deck.ActiveWorkspace()
	context, _ := state.deck.Context(workspace)
	context.Kind = layoutcore.ContextSFTP
	context.Open = true
	if state.sftpPanelWidth > 0 && (context.Width <= 0 || context.Width == layoutcore.DefaultContextWidth) {
		context.Width = state.sftpPanelWidth
	}
	_ = state.deck.SetContext(workspace, context)
	state.openSFTP(session)
	state.fitWorkspaceTerminals(workspace)
	state.save()
}

// hideFilesPanel closes the file panel and lets an idle connection go.
func (state *State) hideFilesPanel(session *Session) {
	workspace := state.deck.ActiveWorkspace()
	state.releaseIdleSFTP(session)
	context, _ := state.deck.Context(workspace)
	context.Open = false
	_ = state.deck.SetContext(workspace, context)
	state.fitWorkspaceTerminals(workspace)
	state.save()
}

// cancelSFTPOperation cancels whatever the files view is doing, with the status
// the file list and the transfer center report.
func (state *State) cancelSFTPOperation(view *sftpView) {
	if !view.busy {
		return
	}
	if view.operation == sftpclient.OperationList && !view.extracting {
		state.cancelSFTPNavigation(view)
		view.status = state.messages.SFTPCanceled
		return
	}
	if view.extracting && view.cancelExtract != nil {
		view.cancelExtract()
	} else {
		view.worker.Cancel()
	}
	view.status = state.messages.SFTPCanceling
}

func (state *State) clickSFTP(id gfx.ID) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone {
		return
	}
	if id == hitSFTPOpen {
		if state.filesPanelShown() {
			state.hideFilesPanel(session)
			return
		}
		state.showFilesPanel(session)
		return
	}
	if id == hitSFTPDetach {
		state.openSFTPWindow(session)
		return
	}
	if id == hitSFTPClose || id == hitSFTPDrawerClose {
		state.hideFilesPanel(session)
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
	if id == hitSFTPMenuSudo {
		state.toggleSudoSFTP(session, view)
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
	if id == hitSFTPCopyPath {
		if err := state.copySFTPPaths(view.active(), view.local); err != nil {
			state.showActionError(state.messages.SFTPWindow.Title, err.Error())
		}
		return
	}
	if id == hitSFTPCopyFiles {
		if _, err := state.copySFTPSelection(view); err != nil {
			state.showActionError(state.messages.SFTPWindow.Title, err.Error())
		}
		return
	}
	if id == hitSFTPPasteFiles {
		state.pasteSFTPClipboard(view)
		return
	}
	if id == hitSFTPProperties {
		if state.openSFTPProperties(view.active(), view.local) && canCalculateSFTPFolderSize(view) {
			state.calculateSFTPFolderSize(view, &state.fileProperties)
		}
		return
	}
	if id == hitSFTPCalculateDirectorySize {
		view.active().headerMenuOpen = false
		state.calculateSFTPDirectorySize(view)
		return
	}
	folder := view.active()
	if folder.headerMenuOpen {
		if state.clickSFTPHistory(view, view.local, id, hitSFTPHistoryBack, hitSFTPHistoryForward, sftpRecentHits) {
			folder.headerMenuOpen = false
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
		case hitSFTPHeaderFollow:
			folder.headerMenuOpen = false
			state.toggleSFTPFollow(session)
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
	case id == hitSFTPRefresh && !view.busy:
		view.forgetListings() // refresh means: trust nothing remembered
		stopSFTPSearch(&view.sftpFolder)
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
	case id == hitSFTPMove && !view.busy:
		state.openSFTPDialog(view, sftpDialogMove)
	case id == hitSFTPDelete && !view.busy:
		state.openSFTPDialog(view, sftpDialogDelete)
	case id == hitSFTPChmod && !view.busy:
		state.openSFTPDialog(view, sftpDialogChmod)
	case id == hitSFTPChown && !view.busy:
		state.openSFTPDialog(view, sftpDialogChown)
	case id.Scope() == hitSFTPBreadcrumbList && !view.busy:
		index := item(id)
		if index < len(view.crumbs) {
			state.navigateSFTPDirectory(view, view.crumbs[index].path)
		}
	case id.Scope() == hitSFTPEntryList && view.searching():
		state.pressSFTPSearchResult(view, &view.sftpFolder, view.first+item(id), 1)
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
			folder.listNeedsSeed = true
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

func (state *State) sftpEditor(hit gfx.ID) *text.Editor {
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
	if hit == hitSFTPDialogField2 && (session.files.dialog == sftpDialogSymlink || session.files.dialog == sftpDialogChown) {
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

func (folder *sftpFolder) hasEntry(name string) bool {
	for _, row := range folder.allRows {
		if row.entry.Name == name {
			return true
		}
	}
	for _, row := range folder.rows {
		if row.entry.Name == name {
			return true
		}
	}
	return false
}

// selectRow applies a press on a row the way a file list does: a plain press
// selects that row alone, Command or Control toggles it among the others, and
// Shift selects every row from the one last chosen to it.
func (folder *sftpFolder) selectRow(index int, modifiers gfx.Modifiers) {
	if index < 0 || index >= len(folder.rows) {
		return
	}
	switch {
	case modifiers&gfx.ModifierShift != 0:
		anchor := min(max(folder.anchor, 0), len(folder.rows)-1)
		from, to := min(anchor, index), max(anchor, index)
		for row := range folder.rows {
			folder.rows[row].selected = row >= from && row <= to
		}
	case modifiers&(gfx.ModifierMeta|gfx.ModifierControl) != 0:
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
	folder.listNeedsSeed = true
}

func (folder *sftpFolder) clearSelection() {
	for i := range folder.rows {
		if folder.rows[i].selected {
			folder.rows[i].selected = false
			folder.rows[i].cells[0] = sftpSelectionCell(false)
		}
	}
	folder.selected, folder.anchor = -1, -1
	folder.listNeedsSeed = true
}

// prepareSFTPList gives the folder's List the row label and multi-selection it
// needs, once. Label names a row for typeahead and assistive technology.
func (folder *sftpFolder) prepareSFTPList() {
	if folder.listReady {
		return
	}
	folder.listReady = true
	folder.list.Selection = &folder.selection
	folder.list.Label = func(i int) string {
		rows := folder.shownRows()
		if i < 0 || i >= len(rows) {
			return ""
		}
		return rows[i].entry.Name
	}
}

// seedSFTPListChoice makes the List's choice the rows the model marks selected.
// It runs before the List declares its rows, so their Selected state is right
// in the paint that follows a model change.
func (folder *sftpFolder) seedSFTPListChoice() {
	if !folder.listNeedsSeed || folder.list.Selection == nil {
		return
	}
	folder.selection.Clear()
	rows := folder.shownRows()
	for i := range rows {
		if rows[i].selected {
			folder.selection.Add(i)
		}
	}
}

// seedSFTPListCurrent makes the row the keys are on the model's current row. It
// runs after the List has declared its rows, so the row number is current.
func (folder *sftpFolder) seedSFTPListCurrent() {
	if !folder.listNeedsSeed || folder.list.Selection == nil {
		return
	}
	if folder.searching() {
		folder.selected = -1
		if folder.search != nil {
			folder.list.Select(folder.search.selected)
		}
		folder.listNeedsSeed = false
		return
	}
	folder.list.Select(folder.selected)
	folder.listNeedsSeed = false
}

// takeSFTPListChoice mirrors the choice the List delivered into the model the
// actions, status line and transfers read.
func (folder *sftpFolder) takeSFTPListChoice() {
	if folder.list.Selection == nil {
		return
	}
	rows := folder.shownRows()
	for i := range rows {
		selected := folder.list.Selection.Has(i)
		if rows[i].selected != selected {
			rows[i].selected = selected
			rows[i].cells[0] = sftpSelectionCell(selected)
		}
	}
	index := folder.list.Selected()
	if folder.searching() {
		if folder.search != nil {
			folder.search.selected = index
			folder.selected = -1
		}
		return
	}
	folder.selected = index
}

// listRow is the row of the folder's List an identity names, if it names one.
func (folder *sftpFolder) listRow(id gfx.ID) (int, bool) {
	if folder.listRowsScope.IsZero() || id.Scope() != folder.listRowsScope {
		return 0, false
	}
	index, ok := id.Name().Index()
	if !ok || index < 0 || index >= len(folder.shownRows()) {
		return 0, false
	}
	return index, true
}

// onListRow reports whether id names one of the folder's List rows; unlike
// listRow it does not need the row to still be in the list.
func (folder *sftpFolder) onListRow(id gfx.ID) bool {
	return !folder.listRowsScope.IsZero() && id.Scope() == folder.listRowsScope
}

// onSFTPListRow is onListRow for the active session's folder.
func (state *State) onSFTPListRow(id gfx.ID) bool {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return ok && session.files != nil && session.files.active().onListRow(id)
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

// sftpFolderRemovalStopped follows a folder removal that failed or was canceled
// part-way: the walk deletes as it goes, so what it already removed is gone for
// good. Nothing remembered about the folder can be trusted, and the notice
// (unlike the cut-off status line) says so with the whole reason.
func (state *State) sftpFolderRemovalStopped(view *sftpView, err error) {
	view.forgetListings()
	messages := state.messages
	if errors.Is(err, sftpclient.ErrCanceled) {
		state.fileOpenNotice(view.detached, messages.SFTPDeleteDirectoryTitle, messages.SFTPDeletePartialHint, components.ToastInfo)
		return
	}
	detail := fmt.Sprintf(messages.SFTPErrorFmt, err) + " " + messages.SFTPDeletePartialHint
	state.fileOpenNotice(view.detached, messages.SFTPDeleteDirectoryTitle, detail, components.ToastError)
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
	state.loadTypeIcons(view, rows)
	view.crumbs = prepareSFTPCrumbs(view.crumbs, remotePath)
	if view.detached != nil {
		entries := make([]sftpclient.Entry, len(rows))
		for index := range rows {
			entries[index] = rows[index].entry
		}
		view.detached.observeDirectoryTreeListing(false, remotePath, entries)
		view.detached.syncDirectoryTreeToPath(false, remotePath)
	}
	state.applySFTPFilter(view, &view.sftpFolder, true)
	state.finishSFTPNavigation(view, false, place, restore, nav)
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
				view.errorText = state.formatSFTPError(result.Err)
			}
		}
		return
	}
	view.remember(result.Path, result.Entries)
	if view.detached != nil {
		view.detached.observeDirectoryTreeListing(false, result.Path, result.Entries)
	}
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
	if view.detached != nil {
		view.detached.observeDirectoryTreeListing(false, view.path, entries)
	}
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
	if !validSFTPDownloadDirectory(directory) || prepareDownloadDirectory(directory) != nil {
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
	state.queueSFTPDownloadFrom(view, view.path, entry, localPath)
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

// remoteCopyOrigin is where one server-to-server copy reads from. source is
// cleared when the copy settles; the labels stay for the transfer history.
type remoteCopyOrigin struct {
	server, address string
	source          *sftpView
}

// remoteCopyLink is one outgoing server-to-server copy: the view whose worker
// owns it, and that worker's transfer id.
type remoteCopyLink struct {
	state *State
	dest  *sftpView
	id    uint64
}

// safeSFTPName is a name a listing entry may contribute to a source path: one
// component, never the folder itself or its parent.
func safeSFTPName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsRune(name, '/') && !strings.ContainsRune(name, '\x00')
}

// queueRemoteCopies copies entries from source's folder sourceDir into dest's
// folder targetDir without routing them through this computer's disk. The batch
// is one transfer: one source subsystem and one card in the transfer center.
func (state *State) queueRemoteCopies(dest *sftpView, source remoteCopySource, sourceDir, targetDir string, entries []sftpclient.Entry) (queued, rejected int) {
	if dest == nil || dest.worker == nil || source.open == nil ||
		sourceDir == "" || targetDir == "" || dest.busy {
		return 0, len(entries)
	}
	sources := make([]string, 0, len(entries))
	recursive := false
	for _, entry := range entries {
		if entry.LinkBroken || !safeSFTPName(entry.Name) {
			rejected++
			continue
		}
		sources = append(sources, path.Join(sourceDir, entry.Name))
		recursive = recursive || entry.IsDir()
	}
	if len(sources) == 0 {
		return 0, rejected
	}
	// One file is described by its own name; a batch by the folder it came from.
	sourceRoot := sourceDir
	if len(sources) == 1 {
		sourceRoot = sources[0]
	}
	id, ok := dest.worker.QueueRemoteCopy(sftpclient.RemoteCopyRequest{
		Source:      source.open,
		SourceRoot:  sourceRoot,
		Sources:     sources,
		Destination: targetDir,
		Recursive:   recursive,
		Policy:      sftpclient.ExistingFail,
	})
	if !ok {
		return 0, len(entries)
	}
	if dest.remoteCopies == nil {
		dest.remoteCopies = make(map[uint64]remoteCopyOrigin)
	}
	dest.remoteCopies[id] = remoteCopyOrigin{server: source.server, address: source.address, source: source.view}
	link := remoteCopyLink{state: state, dest: dest, id: id}
	if source.view != nil {
		source.view.outgoingCopies = append(source.view.outgoingCopies, link)
	}
	dest.status = fmt.Sprintf(state.messages.SFTPQueuedFmt, targetDir)
	dest.errorText = ""
	state.transfers.open, state.transfers.filter, state.transfers.scroll = true, 0, 0
	state.transfers.host = nil
	state.refreshSFTPTransfers(dest)
	return len(entries) - rejected, rejected
}

// releaseRemoteCopySource lets go of the source view of a copy that settled.
// A source window closed while the copy ran releases its connection now.
func (state *State) releaseRemoteCopySource(dest *sftpView, id uint64) {
	origin, ok := dest.remoteCopies[id]
	if !ok || origin.source == nil {
		return
	}
	src := origin.source
	for index := range src.outgoingCopies {
		if link := src.outgoingCopies[index]; link.dest == dest && link.id == id {
			src.outgoingCopies = append(src.outgoingCopies[:index], src.outgoingCopies[index+1:]...)
			break
		}
	}
	origin.source = nil
	dest.remoteCopies[id] = origin
	if len(src.outgoingCopies) == 0 && src.detached != nil && src.detached.closed {
		src.detached.releaseConnection()
	}
}

// cancelRemoteCopiesFrom cancels every copy still reading from view, for a
// window that its user agreed to close.
func (state *State) cancelRemoteCopiesFrom(view *sftpView) {
	links := append([]remoteCopyLink(nil), view.outgoingCopies...)
	for _, link := range links {
		if link.dest == nil || link.dest.worker == nil {
			continue
		}
		link.dest.worker.CancelTransfer(link.id)
		if link.state != nil {
			link.state.refreshSFTPTransfers(link.dest)
		}
		link.dest.invalidateDetached()
	}
}

func (state *State) openSFTPDialog(view *sftpView, dialog sftpDialogKind) {
	view.dialogEditor = text.Editor{}
	view.dialogEntry = sftpclient.Entry{}
	view.dialogEntries = nil
	view.dialogError = ""
	if dialog != sftpDialogCreateDirectory {
		if dialog == sftpDialogDelete || dialog == sftpDialogMove {
			for _, row := range selectedSFTPRows(view) {
				view.dialogEntries = append(view.dialogEntries, row.entry)
			}
		}
		entry, ok := state.selectedSFTPEntry(view)
		if !ok && len(view.dialogEntries) == 0 {
			return
		}
		if len(view.dialogEntries) > 0 {
			view.dialogEntry = view.dialogEntries[0]
		} else {
			view.dialogEntry = entry
		}
	}
	switch dialog {
	case sftpDialogRename:
		view.dialogEditor.Insert(view.dialogEntry.Name)
	case sftpDialogMove:
		view.dialogEditor.Insert(view.path)
	case sftpDialogChmod:
		view.dialogEditor.Restrict(func(value rune) bool { return value >= '0' && value <= '7' }, 3)
		view.dialogEditor.Insert(fmt.Sprintf("%03o", view.dialogEntry.Mode.Perm()))
	case sftpDialogChown:
		// A name is what the listing shows; the number stands in when the
		// server's account files did not name the owner. Typing is not filtered
		// here: submit validates it, so a rejected character is explained
		// instead of quietly disappearing.
		view.dialogEditor.Restrict(acceptSFTPOwnershipRune, sftpOwnershipMaxRunes)
		view.dialogEditor2.Restrict(acceptSFTPOwnershipRune, sftpOwnershipMaxRunes)
		ids := sftpclient.IDNames{}
		if view.worker != nil {
			ids = view.worker.Accounts()
		}
		view.ownership.load(ids)
		owner := sftpOwnershipValue(view.dialogEntry.Owner, view.dialogEntry.UID, view.dialogEntry.HasOwner)
		group := sftpOwnershipValue(view.dialogEntry.Group, view.dialogEntry.GID, view.dialogEntry.HasOwner)
		view.dialogEditor.Insert(owner)
		view.dialogEditor2.Insert(group)
		view.ownership.lastQuery = [2]string{owner, group}
	}
	view.dialog = dialog
}

func (state *State) closeSFTPDialog(view *sftpView) {
	if view.dialog == sftpDialogBrowseCreateDirectory && view.detached != nil {
		view.detached.cancelBrowseDirectoryPrompt()
	}
	view.ownership.reset()
	view.dialog = sftpDialogNone
	view.dialogEditor, view.dialogEditor2 = text.Editor{}, text.Editor{}
	view.dialogEntry = sftpclient.Entry{}
	view.dialogEntries = nil
	view.dialogError = ""
}

func (state *State) queueNextSFTPDelete(view *sftpView) bool {
	entry := view.deleteRemaining[0]
	remotePath := path.Join(view.path, entry.Name)
	var queued bool
	if entry.IsDir() && !entry.IsLink() {
		queued = view.worker.RemoveDirectory(remotePath)
		view.operation = sftpclient.OperationRemoveDirectory
	} else {
		queued = view.worker.RemoveFile(remotePath)
		view.operation = sftpclient.OperationRemoveFile
	}
	if queued {
		view.deleteRemaining = view.deleteRemaining[1:]
		view.busy = true
		view.status = fmt.Sprintf(state.messages.SFTPDeletingFmt, entry.Name)
	}
	return queued
}

func (state *State) finishSFTPDelete(view *sftpView, result sftpclient.Result) {
	if result.Err == nil {
		view.deleteCompleted++
	}
	if result.Err == nil && len(view.deleteRemaining) > 0 && state.queueNextSFTPDelete(view) {
		return
	}
	remaining := len(view.deleteRemaining)
	completed := view.deleteCompleted
	failedPath := result.Path
	if result.Err == nil && remaining > 0 {
		failedPath = path.Join(view.path, view.deleteRemaining[0].Name)
	}
	view.deleteRemaining = nil
	view.deleteCompleted = 0
	view.forgetListings()
	if result.Err != nil || remaining > 0 {
		detail := state.messages.SFTPDeleteSelectionPartial
		if result.Err != nil {
			detail = fmt.Sprintf("%s: %s %s", failedPath, fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err), detail)
		} else {
			detail = fmt.Sprintf("%s: %s %s", failedPath, state.messages.SFTPUnavailable, detail)
		}
		state.fileOpenNotice(view.detached, state.messages.SFTPDeleteSelectionTitle, detail, components.ToastError)
	} else if completed > 1 {
		view.refreshStatus = fmt.Sprintf(state.messages.SFTPDeletedCountFmt, completed)
	} else {
		view.refreshStatus = fmt.Sprintf(state.messages.SFTPDeletedFmt, result.Path)
	}
	state.listSFTP(view, view.path)
}

func (state *State) clickSFTPDialog(view *sftpView, id gfx.ID) {
	if view.dialog == sftpDialogChown {
		if state.clickSFTPOwnership(view, id) {
			return
		}
	}
	if view.dialog == sftpDialogChmod && id.Scope() == hitSFTPChmodBits {
		// A permission checkbox flips one bit of the octal field, so the typed
		// value and the checkboxes always agree (issue #392).
		index := item(id)
		if index < 0 || index > 8 {
			return
		}
		mode, ok := sftpChmodFromDialog(view)
		if !ok {
			return
		}
		mask := uint32(1) << uint(8-index)
		state.setSFTPChmodDialog(view, mode^mask)
	}
	switch id {
	case hitSFTPDialogClose, hitSFTPDialogCancel:
		state.closeSFTPDialog(view)
	case hitSFTPDialogSubmit:
		state.submitSFTPDialog(view)
	}
}

// clickSFTPOwnership handles the ownership dialog's fields and suggestion
// lists. It reports whether it took the click.
func (state *State) clickSFTPOwnership(view *sftpView, id gfx.ID) bool {
	switch {
	case id == hitSFTPChownDismiss:
		view.ownership.closePopups()
		return true
	case id == hitSFTPDialogField || id == hitSFTPDialogField2:
		field := 0
		if id == hitSFTPDialogField2 {
			field = 1
		}
		state.openSFTPOwnershipList(view, field)
		return true
	}
	for field := range 2 {
		if index, ok := id.In(ownershipItemScope(field)); ok {
			state.chooseSFTPOwnership(view, field, index)
			return true
		}
	}
	return false
}

// openSFTPOwnershipList opens one field's suggestions and closes the other's.
func (state *State) openSFTPOwnershipList(view *sftpView, field int) {
	view.ownership.open = [2]bool{}
	view.ownership.open[field] = true
	view.ownership.highlighted[field] = 0
	view.ownership.lastQuery[field] = view.ownershipEditor(field).String()
}

// chooseSFTPOwnership puts one suggested account in the field. The list closes;
// the text stays editable, so an account the server did not list can still be
// typed.
func (state *State) chooseSFTPOwnership(view *sftpView, field, index int) {
	matches := view.ownership.matches(field, view.ownershipEditor(field).String())
	if index < 0 || index >= len(matches) {
		return
	}
	account := matches[index]
	editor := view.ownershipEditor(field)
	editor.SetSelection(text.Range{Start: 0, End: editor.RuneCount()})
	editor.Insert(account.name)
	editor.SetSelection(text.Range{Start: editor.RuneCount(), End: editor.RuneCount()})
	view.ownership.open[field] = false
	view.ownership.lastQuery[field] = account.name
}

// sftpOwnershipKey drives an ownership field's suggestion list before the
// dialog's own Enter and Escape. handled is false when the key belongs to the
// dialog.
func (state *State) sftpOwnershipKey(view *sftpView, event gfx.KeyEvent) (gfx.EventResult, bool) {
	if view.dialog != sftpDialogChown {
		return gfx.EventResult{}, false
	}
	field := -1
	switch event.Target {
	case hitSFTPDialogField:
		field = 0
	case hitSFTPDialogField2:
		field = 1
	}
	if field < 0 {
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	text := view.ownershipEditor(field).String()
	matches := view.ownership.matches(field, text)
	switch event.Key {
	case gfx.KeyUp, gfx.KeyDown:
		if !view.ownership.open[field] {
			state.openSFTPOwnershipList(view, field)
			view.ownership.highlighted[field] = 0
			if event.Key == gfx.KeyUp && len(matches) > 0 {
				view.ownership.highlighted[field] = len(matches) - 1
			}
			return handled, true
		}
		if len(matches) == 0 {
			view.ownership.highlighted[field] = -1
			return handled, true
		}
		index := view.ownership.highlighted[field]
		if event.Key == gfx.KeyDown {
			index = min(max(index+1, 0), len(matches)-1)
		} else {
			index = min(max(index-1, 0), len(matches)-1)
		}
		view.ownership.highlighted[field] = index
		return handled, true
	case gfx.KeyEnter:
		if !view.ownership.open[field] {
			return gfx.EventResult{}, false
		}
		if len(matches) > 0 {
			index := min(max(view.ownership.highlighted[field], 0), len(matches)-1)
			state.chooseSFTPOwnership(view, field, index)
			return handled, true
		}
		view.ownership.open[field] = false
		return handled, true
	case gfx.KeyEscape:
		if view.ownership.open[field] {
			view.ownership.open[field] = false
			return handled, true
		}
	case gfx.KeyTab:
		view.ownership.closePopups()
	}
	return gfx.EventResult{}, false
}

// sftpOwnershipAvailable reports whether the panel can change an owner and a
// group: a remote SFTP listing that carries POSIX ids. An SCP listing carries
// none, and a Windows host has none to carry.
func sftpOwnershipAvailable(session *Session, view *sftpView) bool {
	return !view.local && !view.approximate &&
		session.host.System != sshconn.SystemWindows && session.host.Transfer != transferSCP
}

// sftpOwnershipValue is what the ownership dialog starts a field with: the name
// the listing showed, or the number when the account files had no name.
func sftpOwnershipValue(name string, id uint32, hasOwner bool) string {
	if !hasOwner {
		return ""
	}
	if name != "" {
		return name
	}
	return strconv.FormatUint(uint64(id), 10)
}

// sftpOwnershipRune keeps an account field to what can name a user or group.
func sftpOwnershipRune(value rune) bool {
	return value > ' ' && value != ':' && value != '\x7f'
}

const sftpOwnershipMaxRunes = 64

// acceptSFTPOwnershipRune only bounds the field's length. submit checks the
// characters, so a rejected one is explained instead of quietly dropped.
func acceptSFTPOwnershipRune(rune) bool { return true }

func validSFTPOwnershipValue(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > sftpOwnershipMaxRunes {
		return false
	}
	for _, character := range value {
		if !sftpOwnershipRune(character) {
			return false
		}
	}
	return true
}

// sftpAccountOption is one user or group the server's account files name.
type sftpAccountOption struct {
	name string
	id   uint32
}

// sftpOwnershipMatches is how many suggestions a field shows at once. The
// query narrows the list; a longer menu would cover the dialog.
const sftpOwnershipMatches = 8

// sftpOwnershipUI is the ownership dialog's suggestion state: the account
// files' names, sorted once, and which field's list is open.
type sftpOwnershipUI struct {
	users, groups []sftpAccountOption
	open          [2]bool
	highlighted   [2]int
	lastQuery     [2]string
}

// load replaces the suggestions with the names the worker's listing read.
func (ui *sftpOwnershipUI) load(ids sftpclient.IDNames) {
	ui.users = sortedSFTPAccounts(ids.Users)
	ui.groups = sortedSFTPAccounts(ids.Groups)
	ui.open = [2]bool{}
	ui.highlighted = [2]int{}
	ui.lastQuery = [2]string{}
}

// reset forgets the suggestions: a closed dialog keeps no account names.
func (ui *sftpOwnershipUI) reset() {
	*ui = sftpOwnershipUI{}
}

func (ui *sftpOwnershipUI) closePopups() {
	ui.open = [2]bool{}
}

// matches returns the accounts a field offers for query. Exact names come
// first, then names that start with the query, then names that contain it;
// each group keeps name order. A numeric query matches ids the same way. The
// caller still accepts text that is not in the list.
func (ui *sftpOwnershipUI) matches(field int, query string) []sftpAccountOption {
	accounts := ui.users
	if field == 1 {
		accounts = ui.groups
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return accounts[:min(len(accounts), sftpOwnershipMatches)]
	}
	var tiers [3][]sftpAccountOption
	for _, account := range accounts {
		name := strings.ToLower(account.name)
		id := strconv.FormatUint(uint64(account.id), 10)
		tier := -1
		switch {
		case name == query || id == query:
			tier = 0
		case strings.HasPrefix(name, query) || strings.HasPrefix(id, query):
			tier = 1
		case strings.Contains(name, query) || strings.Contains(id, query):
			tier = 2
		}
		if tier >= 0 && len(tiers[tier]) < sftpOwnershipMatches {
			tiers[tier] = append(tiers[tier], account)
		}
	}
	matches := make([]sftpAccountOption, 0, sftpOwnershipMatches)
	for _, tier := range tiers {
		matches = append(matches, tier...)
		if len(matches) >= sftpOwnershipMatches {
			return matches[:sftpOwnershipMatches]
		}
	}
	return matches
}

func sortedSFTPAccounts(names map[uint32]string) []sftpAccountOption {
	accounts := make([]sftpAccountOption, 0, len(names))
	for id, name := range names {
		accounts = append(accounts, sftpAccountOption{name: name, id: id})
	}
	slices.SortFunc(accounts, func(left, right sftpAccountOption) int {
		if order := strings.Compare(left.name, right.name); order != 0 {
			return order
		}
		if left.id < right.id {
			return -1
		}
		if left.id > right.id {
			return 1
		}
		return 0
	})
	return accounts
}

func (view *sftpView) ownershipEditor(field int) *text.Editor {
	if field == 0 {
		return &view.dialogEditor
	}
	return &view.dialogEditor2
}

func ownershipItemScope(field int) gfx.Scope {
	if field == 0 {
		return hitSFTPChownUserItem
	}
	return hitSFTPChownGroupItem
}

// sftpChmodFromDialog reads the dialog's octal field; ok is false while the
// text is not a usable mode.
func sftpChmodFromDialog(view *sftpView) (uint32, bool) {
	mode, err := strconv.ParseUint(strings.TrimSpace(view.dialogEditor.String()), 8, 12)
	if err != nil || mode > 0o777 {
		return 0, false
	}
	return uint32(mode), true
}

// setSFTPChmodDialog replaces the octal field with mode, under the same
// restrictions the dialog opened with.
func (state *State) setSFTPChmodDialog(view *sftpView, mode uint32) {
	view.dialogEditor = text.Editor{}
	view.dialogEditor.Restrict(func(value rune) bool { return value >= '0' && value <= '7' }, 3)
	view.dialogEditor.Insert(fmt.Sprintf("%03o", mode))
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
	view.moveAfterRename = false
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
		if !validRemoteBaseName(value) {
			view.dialogError = state.messages.SFTPErrRenameInvalid
			return
		}
		remotePath := sftpRemotePath(view.path, value)
		queued = view.worker.CreateDirectory(remotePath)
		operation = sftpclient.OperationCreateDirectory
		status = fmt.Sprintf(state.messages.SFTPCreatingDirectoryFmt, remotePath)
	case sftpDialogRename:
		if !validRemoteBaseName(value) {
			view.dialogError = state.messages.SFTPErrRenameInvalid
			return
		}
		source := path.Join(view.path, view.dialogEntry.Name)
		destination := path.Join(view.path, value)
		if source == destination {
			view.dialogError = state.messages.SFTPErrRenameSame
			return
		}
		queued = view.worker.Rename(source, destination)
		operation = sftpclient.OperationRename
		status = fmt.Sprintf(state.messages.SFTPRenamingFmt, view.dialogEntry.Name)
	case sftpDialogMove:
		if value == "" {
			view.dialogError = state.messages.SFTPErrMoveRequired
			return
		}
		if strings.ContainsRune(value, '\x00') {
			view.dialogError = state.messages.SFTPErrMoveInvalid
			return
		}
		folder := sftpRemotePath(view.path, value)
		if path.Clean(folder) == path.Clean(view.path) {
			view.dialogError = state.messages.SFTPErrMoveSame
			return
		}
		sources := make([]string, 0, len(view.dialogEntries))
		for _, entry := range view.dialogEntries {
			source := path.Join(view.path, entry.Name)
			if entry.IsDir() && !entry.IsLink() && (folder == source || strings.HasPrefix(folder, source+"/")) {
				view.dialogError = state.messages.SFTPErrMoveInside
				return
			}
			sources = append(sources, source)
		}
		if len(sources) > 1 {
			queued = view.worker.MoveSelection(sources, folder)
			operation = sftpclient.OperationMoveSelection
			status = fmt.Sprintf(state.messages.SFTPMovingCountFmt, len(sources))
			break
		}
		source := path.Join(view.path, view.dialogEntry.Name)
		destination := path.Join(folder, view.dialogEntry.Name)
		queued = view.worker.Rename(source, destination)
		operation = sftpclient.OperationRename
		status = fmt.Sprintf(state.messages.SFTPMovingFmt, view.dialogEntry.Name)
		view.moveAfterRename = queued
	case sftpDialogDelete:
		if view.local {
			state.submitLocalDelete(view)
			return
		}
		if len(view.dialogEntries) > 1 {
			view.deleteRemaining = slices.Clone(view.dialogEntries)
			view.deleteCompleted = 0
			if !state.queueNextSFTPDelete(view) {
				view.deleteRemaining = nil
				view.dialogError = state.messages.SFTPUnavailable
				return
			}
			state.closeSFTPDialog(view)
			view.errorText = ""
			return
		}
		remotePath := path.Join(view.path, view.dialogEntry.Name)
		if view.dialogEntry.IsDir() && !view.dialogEntry.IsLink() {
			// A folder is removed with its contents; the worker walks the tree.
			// A link is only the link: what it points at is not ours to empty.
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
	case sftpDialogChown:
		owner := value
		group := strings.TrimSpace(view.dialogEditor2.String())
		if owner == "" || group == "" {
			view.dialogError = state.messages.SFTPErrOwnershipRequired
			return
		}
		if !validSFTPOwnershipValue(owner) || !validSFTPOwnershipValue(group) {
			view.dialogError = state.messages.SFTPErrOwnershipInvalid
			return
		}
		remotePath := path.Join(view.path, view.dialogEntry.Name)
		queued = view.worker.Chown(remotePath, owner, group)
		operation = sftpclient.OperationChown
		status = fmt.Sprintf(state.messages.SFTPChangingOwnershipFmt, view.dialogEntry.Name)
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

func validRemoteBaseName(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`+"\x00")
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
			if result.Operation == sftpclient.OperationList && result.Background && view.detached != nil && view.detached.acceptDirectoryTreeResult(result) {
				continue
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
						view.errorText = state.formatSFTPError(result.Err)
					}
				} else if result.Operation == sftpclient.OperationUpload {
					view.refreshAfterTransfers = true
					view.refreshStatus = fmt.Sprintf(state.messages.SFTPUploadedFmt, formatBytes(result.Bytes), result.Path)
					view.status = view.refreshStatus
				} else if result.Operation == sftpclient.OperationRemoteCopy {
					view.refreshAfterTransfers = true
					view.refreshStatus = fmt.Sprintf(state.messages.SFTPCopiedFmt, formatBytes(result.Bytes), result.Path)
					view.status = view.refreshStatus
				} else {
					view.status = fmt.Sprintf(state.messages.SFTPDownloadedFmt, formatBytes(result.Bytes), result.LocalPath)
				}
				state.refreshSFTPTransfers(view)
				continue
			}
			if (result.Operation == sftpclient.OperationRemoveFile || result.Operation == sftpclient.OperationRemoveDirectory) && (view.deleteRemaining != nil || view.deleteCompleted > 0) {
				state.finishSFTPDelete(view, result)
				continue
			}
			if view.detached != nil && view.detached.browseDirectoryCreated(result) {
				continue
			}
			view.busy = false
			if result.Operation == sftpclient.OperationMoveSelection {
				if result.Err != nil {
					detail := fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err)
					if errors.Is(result.Err, sftpclient.ErrCanceled) {
						detail = state.messages.SFTPCanceled
					}
					state.fileOpenNotice(view.detached, state.messages.SFTPMoveTitle, detail, components.ToastError)
				}
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPMovedCountFmt, result.Files, result.Destination)
				state.listSFTP(view, view.path)
				continue
			}
			if result.Operation == sftpclient.OperationRename && result.Err != nil {
				view.moveAfterRename = false
			}
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
					view.errorText = state.formatSFTPError(result.Err)
				}
				if result.Operation == sftpclient.OperationRemoveDirectory {
					state.sftpFolderRemovalStopped(view, result.Err)
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
				if view.moveAfterRename {
					view.refreshStatus = fmt.Sprintf(state.messages.SFTPMovedFmt, result.Path, result.Destination)
				} else {
					view.refreshStatus = fmt.Sprintf(state.messages.SFTPRenamedFmt, result.Path, result.Destination)
				}
				view.moveAfterRename = false
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
			case sftpclient.OperationChown:
				view.forgetListings()
				view.refreshStatus = fmt.Sprintf(state.messages.SFTPOwnershipChangedFmt, result.Owner, result.Group, result.Path)
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
func (state *State) scrollSFTP(view *sftpView, event gfx.ScrollEvent) gfx.EventResult {
	if view == nil {
		return gfx.EventResult{Handled: true}
	}
	if view.bookmarksOpen {
		return gfx.EventResult{Handled: true, Repaint: view.places.scroll.Wheel(event)}
	}
	if view.editPath != "" && pointInRect(event.X, event.Y, view.editRect) {
		view.editLayout.ScrollBy(event.DeltaY)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	folder := view.active()
	// The List owns the offset and is part of the workbench page: WindowConfig's
	// wheel routing finds it under the pointer with the event's own coordinates.
	// One over the panel's other chrome still scrolls the List, as it did before
	// the List existed.
	if scroll := folder.listScroll; scroll != nil && len(folder.shownRows()) > folder.visibleRows {
		result := scroll.Wheel(event)
		state.syncSFTPListOffset(folder)
		return result
	}
	if folder.visibleRows <= 0 || len(folder.shownRows()) <= folder.visibleRows {
		return gfx.EventResult{Handled: true}
	}
	return scrollFolder(folder, event)
}

// syncSFTPListOffset copies the List's offset back into the fields the drop
// target, the legacy row identities and the tests read between paints.
func (state *State) syncSFTPListOffset(folder *sftpFolder) {
	if folder.listScroll == nil {
		return
	}
	folder.scrollOffset = folder.listScroll.Offset
	folder.first = max(0, int(folder.scrollOffset/sftpRowHeight))
}

// sftpListActivateKey opens the row Enter is pressed on. The List's key route
// would take Enter as its own submission first, so this runs before it.
func (state *State) sftpListActivateKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Key != gfx.KeyEnter || event.Repeat || event.Modifiers != 0 ||
		state.activeSection != sectionTerminal || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		return gfx.EventResult{}, false
	}
	view := session.files
	if view.editPath != "" || view.dialog != sftpDialogNone {
		return gfx.EventResult{}, false
	}
	folder := view.active()
	index, ok := folder.listRow(event.Target)
	if !ok || view.busy && !view.local {
		return gfx.EventResult{}, false
	}
	if folder.searching() {
		state.pressSFTPSearchResult(view, folder, index, 2)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if index >= len(folder.rows) {
		return gfx.EventResult{}, false
	}
	folder.selected = index
	if view.local {
		state.openLocalSelected(view)
	} else {
		state.openSelectedSFTP(view)
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// fitSFTP keeps every list's first row inside its rows after the layout moved.
func (state *State) fitSFTP(size gfx.Size) {
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil {
			view.first = min(view.first, max(0, len(view.shownRows())-view.visibleRows))
			view.lfolder.first = min(view.lfolder.first, max(0, len(view.lfolder.shownRows())-view.lfolder.visibleRows))
		}
	}
}

func (state *State) formatSFTPError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, sftpclient.ErrDestinationExists) || errors.Is(err, os.ErrExist) {
		return fmt.Sprintf(state.messages.SFTPErrorFmt, state.messages.SFTPErrDirectoryExists)
	}
	var status *sftp.StatusError
	if errors.As(err, &status) {
		switch status.FxCode() {
		case sftp.ErrSSHFxPermissionDenied:
			return fmt.Sprintf(state.messages.SFTPErrorFmt, state.messages.SFTPErrPermissionDenied)
		}
	}
	return fmt.Sprintf(state.messages.SFTPErrorFmt, err)
}

func (state *State) sftpKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files != nil && state.modal == modalNone {
		if result, handled := state.sftpOwnershipKey(session.files, event); handled {
			return result, true
		}
	}
	if result, handled := state.sftpListActivateKey(event); handled {
		return result, true
	}
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
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	if !ok || session.files == nil || state.modal != modalNone || event.Repeat {
		return gfx.EventResult{}, false
	}
	view := session.files
	if view.dialog != sftpDialogNone {
		switch event.Key {
		case gfx.KeyEscape:
			state.closeSFTPDialog(view)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.KeyEnter:
			if event.Target == hitSFTPDialogField || event.Target == hitSFTPDialogField2 || event.Target == hitSFTPDialogSubmit {
				if view.dialog == sftpDialogCompress || view.dialog == sftpDialogExtract {
					if editor := state.sftpEditor(event.Target); editor != nil {
						if _, composing := editor.Composition(); composing {
							return gfx.EventResult{Handled: true}, true
						}
					}
				}
				state.submitSFTPDialog(view)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
		return gfx.EventResult{}, false
	}
	// A terminal keeps its own keys: an open editor or panel is no reason to take
	// Escape away from the shell the user is typing into.
	if state.terminalSession(event.Target) != nil || state.terminalPageSession(event.Target) != nil {
		return gfx.EventResult{}, false
	}
	if view.editPath != "" {
		if event.Key == gfx.KeyEscape {
			state.closeSFTPEditor(view)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEscape:
		if folder := view.active(); folder.filterOpen && event.Target == hitSFTPFilter {
			// Escape leaves the filter, not the file panel: the first press drops
			// the query, the next closes the row, as a find bar does.
			if folder.filterInputEditor().String() != "" {
				folder.rememberSelection()
				folder.clearFilter()
				state.applySFTPFilter(view, folder, false)
				return gfx.EventResult{Handled: true, Repaint: true, Focus: hitSFTPFilter, FocusVisible: true}, true
			}
			folder.filterOpen = false
			result := gfx.EventResult{Handled: true, Repaint: true}
			if len(folder.rows) > 0 && !folder.listRowsScope.IsZero() {
				result.Focus, result.FocusVisible = folder.listRowsScope.At(0), true
			}
			return result, true
		}
		if view.bookmarksOpen {
			view.bookmarksOpen = false
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		state.clickSFTP(hitSFTPClose)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.KeyEnter:
		if event.Target.Scope() == hitSFTPEntryList {
			folder := view.active()
			index := folder.first + item(event.Target)
			if folder.searching() {
				// Enter on a result is a double click on it.
				state.pressSFTPSearchResult(view, folder, index, 2)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			if index < len(folder.rows) {
				folder.selected = index
				if view.local {
					state.openLocalSelected(view)
				} else {
					state.openSelectedSFTP(view)
				}
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}

func (state *State) sftpPathPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonPrimary || event.Target != hitSFTPPathEdit {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || state.activeSection != sectionTerminal || state.modal != modalNone || session.files.editPath != "" || session.files.dialog != sftpDialogNone || !session.files.local && session.files.busy {
		return gfx.EventResult{}, false
	}
	result := state.startSFTPPathEditing(session.files)
	if !result.Handled {
		return gfx.EventResult{}, false
	}
	return result, true
}

// sftpRowPointer opens the row that was double-clicked: a folder is entered, a
// file is opened for editing. The first click of the pair has already selected
// the row, so what opens is what was pointed at.
func (state *State) sftpRowPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button == gfx.PointerButtonSecondary || event.ClickCount < 1 {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil {
		return gfx.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	index, listRow := folder.listRow(event.Target)
	if !listRow {
		if event.Target.Scope() != hitSFTPEntryList {
			return gfx.EventResult{}, false
		}
		index = folder.first + item(event.Target)
	}
	if view.dialog != sftpDialogNone || index >= len(folder.shownRows()) || view.busy && !view.local {
		return gfx.EventResult{Handled: true}, true
	}
	if listRow && event.ClickCount == 1 {
		// The List's embedded canvas does not receive ClickInfo, so the panel
		// keeps applying the click modifiers itself, as before the List.
		if folder.searching() {
			state.pressSFTPSearchResult(view, folder, index, 1)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		folder.selectRow(index, event.Modifiers)
		view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
		if row := folder.rows[index]; row.selected && row.entry.IsDir() && !view.local && selectedSFTPCount(view) == 1 {
			state.prefetchSFTP(view, path.Join(view.path, row.entry.Name))
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if folder.searching() {
		state.pressSFTPSearchResult(view, folder, index, event.ClickCount)
		return gfx.EventResult{Handled: true, Repaint: true}, true
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
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	folder.selectRow(index, 0)
	if view.local {
		state.openLocalSelected(view)
	} else {
		state.openSelectedSFTP(view)
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
