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
	entry    sftpclient.Entry
	selected bool
	cells    [5]string
	icon     *ui.Icon // the file-type glyph, chosen when the listing arrives
	mono     bool     // the glyph has no colors of its own and is tinted
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
)

// sftpFolder is a folder on show: where it is, its rows and the user's place in
// them. The panel has one for the server and one for this computer.
type sftpFolder struct {
	path        string
	rows        []sftpRow
	crumbs      []sftpCrumb
	selected    int
	anchor      int // where a Shift-click range starts: the row last chosen without Shift
	first       int
	visibleRows int     // rows the last paint had room for
	wheel       float32 // wheel travel not yet worth a whole row
	pathField   sftpPathField
}

type sftpView struct {
	worker                          *sftpclient.Worker
	transferScan                    uint64
	transferServer, transferAddress string
	// sftpFolder is the server's folder; lfolder is this computer's, shown
	// instead of it while local is set.
	sftpFolder
	lfolder               sftpFolder
	local                 bool
	localOnly             bool   // a local terminal's panel: no server, no worker, no other side to switch to
	localSeq              uint64 // the latest local listing; an older answer is dropped
	remoteSeq             uint64 // the latest remote listing prepared; older work is dropped
	preparingPath         string // the large listing whose rows are being prepared
	followUpPath          string // a follow-up that arrived before those rows were ready
	followUpEntries       []sftpclient.Entry
	busy                  bool
	approximate           bool // the listing came from `ls` over SCP
	operation             sftpclient.Operation
	status                string
	errorText             string
	refreshStatus         string
	dialog                sftpDialogKind
	dialogEditor          text.Editor
	dialogEditor2         text.Editor
	bookmarksOpen         bool
	dropping              bool // files are being dragged over the panel
	dropRow               int  // the folder row they are over, or -1
	details               sftpDetails
	extracting            bool
	archiveSequence       uint64
	archiveDirectory      string
	archiveResults        chan archiveResult
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
	listingLimitBytes int
	prefetching       map[string]struct{}
	waiting           string
	pathRequest       string // the directory submitted through the path input, awaiting its listing
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
		view.selected, view.lfolder.selected = -1, -1
		session.files = view
		state.listLocal(view, localHome())
		return
	}
	if session == nil || !session.hasServer() || session.conn == nil || session.phase != sshconn.PhaseConnected {
		return
	}
	if session.files != nil {
		return
	}
	connection, transfer := session.conn, session.host.Transfer
	view := &sftpView{busy: true, operation: sftpclient.OperationList, status: state.messages.SFTPLoading}
	view.transferServer, view.transferAddress = session.Title, session.Address
	view.selected, view.lfolder.selected = -1, -1
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return openRemoteFiles(connection, transfer) }, state.notifyFor(session))
	state.configureSFTPWorker(view)
	session.files = view
	if !view.worker.List("") {
		view.busy = false
		view.errorText = state.messages.SFTPUnavailable
	}
}

func (state *State) closeSFTP(session *Session) {
	if session == nil || session.files == nil {
		return
	}
	if session.files.cancelExtract != nil {
		session.files.cancelExtract()
	}
	if session.files.worker != nil {
		session.files.worker.Close()
		state.refreshSFTPTransfers(session.files)
		state.archiveTransfers(session.files)
	}
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
	if id == hitSFTPClose {
		state.releaseIdleSFTP(session)
		context, _ := state.deck.Context(state.deck.ActiveWorkspace())
		context.Open = false
		_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
		state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
		state.save()
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
	if view.local && state.clickSFTPLocal(session, view, id) {
		return
	}
	if view.bookmarksOpen && id != hitSFTPBookmarkList && (id.Scope() != hitSFTPBookmarkItemList) {
		// Pressing anywhere else closes the list, and does nothing more.
		view.bookmarksOpen = false
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
		if view.extracting && view.cancelExtract != nil {
			view.cancelExtract()
		} else {
			view.worker.Cancel()
		}
		view.status = state.messages.SFTPCanceling
	case id == hitSFTPRefresh && !view.busy:
		view.forgetListings() // refresh means: trust nothing remembered
		state.listSFTP(view, view.path)
	case id == hitSFTPUp && !view.busy:
		state.openSFTPDirectory(view, path.Dir(view.path))
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
			state.openSFTPDirectory(view, view.crumbs[index].path)
		}
	case id.Scope() == hitSFTPEntryList && !view.busy:
		index := view.first + item(id)
		if index < len(view.rows) {
			view.selected = index
			view.rows[index].selected = !view.rows[index].selected
			view.rows[index].cells[0] = sftpSelectionCell(view.rows[index].selected)
			view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
			if row := view.rows[index]; row.selected && row.entry.IsDir() {
				// The first click of a double-click: start reading the folder
				// now, so the second finds the work done or under way.
				state.prefetchSFTP(view, path.Join(view.path, row.entry.Name))
			}
		}
	}
}

func (state *State) sftpEditor(hit ui.ID) *text.Editor {
	if state.activeSection != sectionTerminal {
		return nil
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		return nil
	}
	if hit == hitSFTPPath && state.sftpPathInput && session.files.dialog == sftpDialogNone && session.files.editPath == "" && (session.files.local || !session.files.busy) {
		return session.files.active().pathInput()
	}
	if hit == hitSFTPEditor && session.files.editPath != "" && session.files.dialog == sftpDialogNone {
		return &session.files.editEditor
	}
	if hit == hitSFTPDialogField2 && session.files.dialog == sftpDialogSymlink {
		return &session.files.dialogEditor2
	}
	if hit != hitSFTPDialogField || session.files.dialog == sftpDialogNone || session.files.dialog == sftpDialogDelete || session.files.dialog == sftpDialogDiscardEdit || session.files.dialog == sftpDialogReloadEdit || session.files.dialog == sftpDialogRemoteChanged {
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
		state.openSFTPDirectory(view, remotePath)
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
}

// openSFTPDirectory goes into a folder. One already read is shown at once and
// read again behind the scenes; one being read ahead is joined rather than read
// twice; any other is listed.
func (state *State) openSFTPDirectory(view *sftpView, remotePath string) {
	key := path.Clean(remotePath)
	if entries, ok := view.listing(key); ok {
		state.showSFTPListing(view, key, entries)
		if _, inFlight := view.prefetching[key]; !inFlight && view.worker.Prefetch(key) {
			view.markPrefetching(key)
		}
		return
	}
	if _, inFlight := view.prefetching[key]; inFlight {
		view.busy, view.operation, view.waiting = true, sftpclient.OperationList, key
		view.status = state.messages.SFTPLoading
		view.errorText = ""
		return
	}
	state.listSFTP(view, remotePath)
}

func (view *sftpView) markPrefetching(key string) {
	if view.prefetching == nil {
		view.prefetching = make(map[string]struct{}, 4)
	}
	view.prefetching[key] = struct{}{}
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
	if view.worker.Prefetch(key) {
		view.markPrefetching(key)
	}
}

// showSFTPListing puts a folder on screen.
func (state *State) showSFTPListing(view *sftpView, remotePath string, entries []sftpclient.Entry) {
	state.applySFTPListing(view, remotePath, prepareSFTPRows(view.rows, entries))
}

// applySFTPListing puts prepared rows on screen. It is separate from the row
// preparation so a large folder can prepare them on a goroutine.
func (state *State) applySFTPListing(view *sftpView, remotePath string, rows []sftpRow) {
	view.remoteSeq++ // a shown listing invalidates an older one still being prepared
	view.path = remotePath
	view.rows = rows
	view.crumbs = prepareSFTPCrumbs(view.crumbs, remotePath)
	view.selected = -1
	view.first = 0
	view.errorText = ""
	if view.refreshStatus != "" {
		view.status = view.refreshStatus
		view.refreshStatus = ""
	} else if view.approximate {
		view.status = fmt.Sprintf(state.messages.SCPItemsFmt, len(view.rows))
	} else {
		view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(view.rows))
	}
}

// finishSFTPPrefetch takes in a folder read ahead of need: it is remembered, shown
// if the user has meanwhile asked for it, and, if it is the folder on screen,
// used to correct what the screen shows from an earlier reading.
func (state *State) finishSFTPPrefetch(view *sftpView, result sftpclient.Result) {
	delete(view.prefetching, result.Path)
	waited := view.waiting != "" && view.waiting == result.Path
	if waited {
		view.waiting = ""
		view.busy = false
	}
	if result.Err != nil {
		if waited {
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
		state.showSFTPListing(view, result.Path, result.Entries)
	case view.path == result.Path && !view.busy && !sameSFTPListing(view.rows, result.Entries):
		state.updateSFTPRows(view, result.Entries)
	}
}

// updateSFTPRows swaps in a newer reading of the folder on screen without
// losing the user's place: the same names stay selected and the list keeps its
// scroll position as far as it still reaches.
func (state *State) updateSFTPRows(view *sftpView, entries []sftpclient.Entry) {
	var selected []string
	for index := range view.rows {
		if view.rows[index].selected {
			selected = append(selected, view.rows[index].entry.Name)
		}
	}
	view.rows = prepareSFTPRows(view.rows, entries)
	for index := range view.rows {
		if slices.Contains(selected, view.rows[index].entry.Name) {
			view.rows[index].selected = true
			view.rows[index].cells[0] = sftpSelectionCell(true)
		}
	}
	view.first = min(view.first, max(0, len(view.rows)-max(view.visibleRows, 1)))
	view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(view.rows))
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
	if view.worker.List(remotePath) {
		view.busy = true
		view.operation = sftpclient.OperationList
		view.status = state.messages.SFTPLoading
		view.errorText = ""
	}
}

func (state *State) downloadSFTP(view *sftpView) {
	selected := selectedSFTPRows(view)
	if len(selected) == 0 {
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
		state.queueSFTPDownload(view, entry, localPath)
		return
	}
	localDirectory, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.SFTPDownloadDirectoryTitle})
	if state.handleSFTPPickerError(view, err, state.messages.SFTPSaveCanceled) {
		return
	}
	for index := range selected {
		state.queueSFTPDownload(view, selected[index].entry, filepath.Join(localDirectory, selected[index].entry.Name))
	}
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
func (state *State) queueSFTPUploadTo(view *sftpView, localPath string, recursive bool, remoteDir string) {
	name := filepath.Base(filepath.Clean(localPath))
	remotePath := path.Join(remoteDir, name)
	_, queued := view.worker.QueueUpload(localPath, remotePath, recursive, sftpclient.ExistingFail)
	if !queued {
		view.errorText = state.messages.SFTPUnavailable
		return
	}
	view.status = fmt.Sprintf(state.messages.SFTPQueuedFmt, name)
	view.errorText = ""
	state.transfers.open, state.transfers.filter, state.transfers.scroll = true, 0, 0
	state.refreshSFTPTransfers(view)
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
			if result.Operation == sftpclient.OperationList && result.Err == nil {
				view.approximate = result.Approximate
			}
			if result.FollowUp {
				// The links of a listing resolved after it was shown. Refresh the
				// rows without touching the selection or the scroll position; a
				// listing already in flight supersedes this one.
				view.remember(result.Path, result.Entries)
				if view.path == result.Path && !view.busy {
					state.updateSFTPRows(view, result.Entries)
				} else if view.preparingPath == result.Path {
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
						state.transfers.open = true
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
			view.busy = false
			if result.Operation == sftpclient.OperationList && view.pathRequest != "" && result.Path == view.pathRequest {
				view.pathRequest = ""
				if result.Err != nil && !errors.Is(result.Err, sftpclient.ErrCanceled) {
					state.showSFTPPathError(result.Path, result.Err)
					view.errorText = ""
					view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, len(view.active().rows))
					continue
				}
			}
			if result.Err != nil {
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
				view.remember(result.Path, result.Entries)
				view.remoteSeq++
				view.preparingPath, view.followUpPath, view.followUpEntries = "", "", nil
				if len(result.Entries) > sftpPrepInlineEntries {
					seq, entries := view.remoteSeq, result.Entries
					view.preparingPath = result.Path
					view.status = state.messages.SFTPLoading
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
							state.applySFTPListing(view, result.Path, rows)
						})
					}()
					continue
				}
				state.showSFTPListing(view, result.Path, result.Entries)
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
			entry: entry, icon: icon, mono: mono,
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
	if view.editPath != "" && pointInRect(event.X, event.Y, view.editRect) {
		view.editLayout.ScrollBy(event.DeltaY)
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return scrollFolder(view.active(), event)
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
		view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
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
