package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var transferCenterHits = tree.Feature("transfer-center")

var (
	hitTransferCenter     = tree.ID(transferCenterHits, "toggle")
	hitTransfersDismiss   = tree.ID(transferCenterHits, "dismiss")
	hitTransfersClose     = tree.ID(transferCenterHits, "close")
	hitTransfersExpand    = tree.ID(transferCenterHits, "expand")
	hitTransfersScope     = tree.ID(transferCenterHits, "scope")
	hitTransfersList      = tree.ID(transferCenterHits, "list")
	hitTransfersClear     = tree.ID(transferCenterHits, "clear")
	hitTransfersCancelAll = tree.ID(transferCenterHits, "cancel-all")
	hitTransfersPauseAll  = tree.ID(transferCenterHits, "pause-all")
	hitTransfersSearch    = tree.ID(transferCenterHits, "search")
	hitTransfersFilter    = tree.Sub(transferCenterHits, tree.Name("filter"))
	hitTransfersGroup     = tree.Sub(transferCenterHits, tree.Name("group"))
	hitTransfersPause     = tree.Sub(transferCenterHits, tree.Name("pause"))
	hitTransfersCancel    = tree.Sub(transferCenterHits, tree.Name("cancel"))
	hitTransfersReveal    = tree.Sub(transferCenterHits, tree.Name("show-location"))
	hitTransfersRemove    = tree.Sub(transferCenterHits, tree.Name("remove"))
	hitTransfersRetry     = tree.Sub(transferCenterHits, tree.Name("retry"))
	hitTransfersReplace   = tree.Sub(transferCenterHits, tree.Name("replace"))
	hitTransfersResume    = tree.Sub(transferCenterHits, tree.Name("resume"))
	hitTransfersSkip      = tree.Sub(transferCenterHits, tree.Name("skip"))
	hitTransfersStatus    = tree.Sub(transferCenterHits, tree.Name("status"))
)

var (
	hitTransfersCompactList = tree.ID(transferCenterHits, "compact-list")
	hitTransfersCompactRow  = tree.Sub(transferCenterHits, tree.Name("compact-row"))
)

const maxTransferHistory = 500

// gfx.Index names list rows with numbers from 0, so the server headings keep
// their keys far above the task ids.
const transferHostHeaderKeyBase = 1 << 30

const (
	compactTransferVisibleRows = 3
	compactTransferRowHeight   = float32(58)
	compactTransferScrollbar   = float32(8)
)

// Keep the saved indices of the original four filters. Waiting is displayed
// between Running and Ended, regardless of its persisted value.
const (
	transferFilterAll = iota
	transferFilterRunning
	transferFilterEnded
	transferFilterFailed
	transferFilterWaiting
	transferFilterPending
	transferFilterCount
)

var transferGroups = [...]int{transferFilterPending, transferFilterRunning, transferFilterWaiting, transferFilterEnded, transferFilterFailed}

type transferDirection int

const (
	transferAnyDirection transferDirection = iota
	transferUpload
	transferDownload
	transferRemoteCopy
	transferDirectionCount
)

// transferDirections are the choices of the direction picker, in its order.
var transferDirections = [...]transferDirection{transferAnyDirection, transferUpload, transferDownload, transferRemoteCopy}

// transferEntry keeps the identity independent of worker-local IDs and tabs.
// A closed connection releases its view; its finished snapshot remains readable.
type transferEntry struct {
	id                                                int
	owner                                             *sftpView
	host                                              *transferHost
	snapshot                                          sftpclient.TransferSnapshot
	server, address                                   string
	sourceServer, sourceAddress                       string // the other end of a server-to-server copy
	title, status, amount, speed                      string
	searchTitle, size, percent, eta, elapsed, outcome string
	fileIcon                                          *gfx.Icon
	typeIcon                                          *fileIconSlot
	monoIcon                                          bool
	removed                                           bool
	conflict                                          bool
	seen                                              uint64
}

type transferKey struct {
	view *sftpView
	id   uint64
}

type transferCenter struct {
	open          bool
	filter        int
	nextID        int
	entries       []transferEntry
	revision      uint64 // changes when SFTP queues need to refresh their sorted indices
	byTask        map[transferKey]int
	summary       string
	badge         string
	buttonLabel   string
	active, ended int
	failed        int
	pending       int
	completed     int
	canceled      int
	// interrupted counts rows a lost connection stopped, so a view that comes
	// back does not scan the history when there is nothing to continue.
	interrupted int
	hasEntries  bool
	// done and total are the bytes the running transfers have moved and the
	// bytes they announced, summed the way the summary bar draws them. They
	// cannot be added up as fractions across windows, and the icon the desktop
	// shows belongs to all of them. The panel overview describes only running
	// transfers through its own overview value.
	done, total int64
	overview    transferOverview
	// Rows/sections are rebuilt when the transfer snapshots or filters change.
	// The List declares only rows near the viewport and gives them stable IDs.
	rows          []transferRow
	groups        [transferFilterCount]transferGroup
	direction     transferDirection
	search        ui.Field
	query         string
	details       transferDetails
	listState     ui.ListState
	listBox       ui.Box
	body          transferBody
	rowDecl       func(ui.ListRow)
	bodyDraw      func(*ui.DrawContext)
	listWidth     float32
	anchor        gfx.Rect
	scrollSynced  float32
	surface, list gfx.Rect
	// scroll is the offset the application asked the List for; scrollSynced is
	// the offset the List reported after the last layout.
	scroll float32
	count  int

	compact bool // a newly queued transfer opens the small progress card first

	compactRows      []int
	compactListState ui.ListState

	host                        *transferHost
	hosts                       []*transferHost
	hostChoices                 []*transferHost // nil (all hosts) followed by hosts
	hostHeaderKeys              map[string]int
	nextHostHeaderKey           int
	hostLabel, scopeLabel       string
	cancelLabel, clearLabel     string
	pauseLabel                  string
	retryLabel, conflictLabel   string
	visibleActive, visibleEnded int
	pausable, paused            int
	retryCount, conflictCount   int
	batch                       transferBatch
	// awake says this window already asked the process to stay awake for its
	// running transfers; see keepAwakeForTransfers.
	awake bool
}

type transferOverview struct {
	title, amount, rate, percent string
	fraction                     float32
}

type transferGroup struct {
	members      []int
	label, count string
	collapsed    bool
}

type transferRow struct {
	group int
	entry int // index in entries, or -1 for a group header

	// When a section lists more than one server, each server's tasks sit under
	// its own header row so the reader can tell them apart.
	host       string
	hostHeader bool
	hostKey    int
}

// transferBody keeps the page List that shows the rows.
type transferBody struct{ list ui.Box }

// handlers makes the page handlers the transfer center uses, once.
func (center *transferCenter) handlers(state *State) {
	if center.rowDecl != nil {
		return
	}
	center.listState.Key = center.keyOf
	center.compactListState.Key = center.compactKeyOf
	center.rowDecl = state.declareTransferRow
	center.bodyDraw = state.paintTransferCenterBody
}

// The low keys name section headers; all other keys follow the transfer.
func (center *transferCenter) keyOf(index int) int {
	row := center.rows[index]
	if row.hostHeader {
		return row.hostKey
	}
	if row.entry < 0 {
		return row.group
	}
	return center.entries[row.entry].id + transferFilterCount
}

// compactScrolls reports whether the compact card has more tasks than it shows.
func (center *transferCenter) compactScrolls() bool {
	return len(center.compactRows) > compactTransferVisibleRows
}

func (center *transferCenter) compactKeyOf(index int) int {
	if index < 0 || index >= len(center.compactRows) || center.compactRows[index] >= len(center.entries) {
		return -1 - index
	}
	return center.entries[center.compactRows[index]].id
}

// hostHeaderKey gives each server header a stable key, so collapsing and
// scrolling do not jump when a row is rebuilt. A server named in two sections
// needs two keys: sibling rows in one List must not share one.
func (center *transferCenter) hostHeaderKey(group int, label string) int {
	if center.hostHeaderKeys == nil {
		center.hostHeaderKeys = map[string]int{}
	}
	name := strconv.Itoa(group) + "\x00" + label
	if key, ok := center.hostHeaderKeys[name]; ok {
		return key
	}
	center.nextHostHeaderKey++
	// gfx.Index names the rows with non-negative numbers, so server headings take
	// a range far above the task ids rather than a negative number.
	key := transferHostHeaderKeyBase + center.nextHostHeaderKey
	center.hostHeaderKeys[name] = key
	return key
}

func (center *transferCenter) entryHostLabel(entry *transferEntry) string {
	if entry.host != nil {
		return entry.host.label
	}
	if entry.server != "" && entry.address != "" {
		return entry.server + " · " + entry.address
	}
	return entry.server
}

func transferActive(snapshot sftpclient.TransferSnapshot) bool {
	return snapshot.State == sftpclient.TransferQueued || snapshot.State == sftpclient.TransferRunning || snapshot.State == sftpclient.TransferPaused
}

// transferRetryPolicy is what "Retry" means for a failed transfer. An upload or
// download continues from the partial file it kept, so a network failure does
// not send the whole file again; anything else asks for the request again.
func transferRetryPolicy(snapshot sftpclient.TransferSnapshot) sftpclient.ExistingPolicy {
	switch snapshot.Operation {
	case sftpclient.OperationUpload, sftpclient.OperationDownload:
		return sftpclient.ExistingResume
	default:
		return sftpclient.ExistingFail
	}
}

func transferCanReveal(snapshot sftpclient.TransferSnapshot) bool {
	return snapshot.Operation == sftpclient.OperationDownload &&
		snapshot.State == sftpclient.TransferCompleted &&
		snapshot.Destination != ""
}

// revealResult is how a background reveal finished, with the text to report a
// failure in: the title the notice has, and what to say when the file manager
// does not start or the target is gone.
type revealResult struct {
	err                    error
	title, failed, missing string
}

// revealLocalPath shows path in the system's file manager. A native reveal can
// wait for the file manager, so it runs away from the window thread and leaves
// its outcome for pumpRevealResults. One reveal is in flight at a time: a second
// request while one runs is dropped.
func (state *State) revealLocalPath(path, title, failed, missing string) {
	state.launchFileManager(path, title, failed, missing, state.options.RevealPath)
}

// launchFileManager 在后台启动文件管理器，并复用统一的结果提示。
func (state *State) launchFileManager(path, title, failed, missing string, reveal func(string) error) {
	if state.revealBusy {
		return
	}
	state.revealBusy = true
	if state.revealResults == nil {
		state.revealResults = make(chan revealResult, 1)
	}
	go func() {
		var err error
		if reveal == nil {
			err = errors.New("reveal path: no file manager launcher is configured")
		} else {
			err = reveal(path)
		}
		state.revealResults <- revealResult{err: err, title: title, failed: failed, missing: missing}
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
}

func (state *State) revealTransferPath(path string) {
	m := state.messages.Transfers
	state.revealLocalPath(path, m.ShowInFolder, m.ShowInFolderFailed, m.ShowInFolderMissing)
}

func (state *State) pumpRevealResults() {
	if state.revealResults == nil {
		return
	}
	for {
		select {
		case result := <-state.revealResults:
			state.revealBusy = false
			if result.err == nil {
				continue
			}
			detail := result.failed
			if errors.Is(result.err, fs.ErrNotExist) {
				detail = result.missing
			}
			state.showActionFailure(result.title, result.err, detail)
		default:
			return
		}
	}
}

func (state *State) transferCenterAvailable() bool {
	return state.modal == modalNone && state.knownHosts.action == knownHostsActionNone &&
		!(state.aiSettingsVisible() && state.ai.cfg.dialog != aiDialogNone)
}

// Once it owns transfer history, a browser's worker belongs to the transfer
// center until the connection closes. Hiding it must preserve retries as well
// as running copies. A browser that never transferred can release its subsystem.
func (state *State) releaseIdleSFTP(session *Session) {
	if session == nil || session.files == nil {
		return
	}
	view := session.files
	if view.worker != nil {
		state.refreshSFTPTransfers(view)
		if len(view.transfers) > 0 {
			return
		}
	}
	// A copy reading from this view holds its connection without owning a task
	// on this worker; the last one releases it.
	if len(view.outgoingCopies) > 0 {
		return
	}
	state.closeSFTP(session)
	state.releaseKeptConnection(session)
}

func (state *State) syncTransfers(view *sftpView) {
	center := &state.transfers
	view.transferScan++
	if len(view.transfers) > 0 && center.byTask == nil {
		center.byTask = make(map[transferKey]int)
	}
	changed := false
	for _, snapshot := range view.transfers {
		key := transferKey{view, snapshot.ID}
		index, exists := center.byTask[key]
		if !exists {
			if view.transferHost == nil {
				view.transferHost = newTransferHost(view.transferServer, view.transferAddress)
			}
			center.nextID++
			entry := transferEntry{id: center.nextID, owner: view, host: view.transferHost, server: view.transferServer, address: view.transferAddress}
			if origin, ok := view.remoteCopies[snapshot.ID]; ok {
				entry.sourceServer, entry.sourceAddress = origin.server, origin.address
			}
			center.entries = append(center.entries, entry)
			index = len(center.entries) - 1
			center.byTask[key] = index
		}
		entry := &center.entries[index]
		entry.seen = view.transferScan
		if entry.removed {
			if view.worker.ForgetTransfer(snapshot.ID) {
				center.drop(index)
				changed = true
			}
			continue
		}
		if entry.snapshot != snapshot {
			entry.snapshot = snapshot
			state.describeTransfer(entry)
			changed = true
		}
		if !transferActive(snapshot) {
			state.releaseRemoteCopySource(view, snapshot.ID)
		}
	}
	// The worker bounds its own history independently of the global history.
	for index := range center.entries {
		entry := &center.entries[index]
		if entry.owner == view && entry.seen != view.transferScan {
			delete(center.byTask, transferKey{view, entry.snapshot.ID})
			entry.owner = nil
			entry.snapshot.Retryable = false
			state.describeTransfer(entry)
			state.releaseRemoteCopySource(view, entry.snapshot.ID)
			changed = true
		}
	}
	state.settleTerminalUploads(view)
	if state.sftpClearUploads {
		for index := len(center.entries) - 1; index >= 0; index-- {
			entry := &center.entries[index]
			if !entry.removed && entry.owner == view && entry.snapshot.Operation == sftpclient.OperationUpload && entry.snapshot.State == sftpclient.TransferCompleted {
				state.removeTransfer(index)
				changed = true
			}
		}
	}
	if changed {
		state.trimTransferHistory()
		state.describeTransferSummary()
	}
}

func (state *State) archiveTransfers(view *sftpView) {
	for index := range state.transfers.entries {
		entry := &state.transfers.entries[index]
		if entry.owner == view {
			delete(state.transfers.byTask, transferKey{view, entry.snapshot.ID})
			entry.owner = nil
			entry.snapshot.Retryable = false
			entry.conflict = false
			state.releaseRemoteCopySource(view, entry.snapshot.ID)
			state.describeTransfer(entry)
		}
	}
	state.describeTransferSummary()
}

// resumeInterruptedTransfers continues the uploads and downloads a lost
// connection stopped. A view for the same host is the connection they were
// waiting for: the partial files stayed, so the new worker verifies them and
// goes on from their end. The row keeps its identity and moves to the new
// worker's task; server-to-server copies are not continued.
func (state *State) resumeInterruptedTransfers(view *sftpView) {
	if view == nil || view.worker == nil || view.transferHost == nil {
		return
	}
	center := &state.transfers
	if center.interrupted == 0 {
		return
	}
	resumed := false
	for index := range center.entries {
		entry := &center.entries[index]
		if entry.removed || entry.owner != nil || entry.snapshot.State != sftpclient.TransferInterrupted {
			continue
		}
		if !sameTransferHost(entry.host, view.transferHost) {
			continue
		}
		snapshot := entry.snapshot
		var id uint64
		var queued bool
		switch snapshot.Operation {
		case sftpclient.OperationUpload:
			if !snapshot.Recursive && snapshot.BytesDone > 0 && snapshot.BytesTotal > 0 {
				id, queued = view.worker.QueueUploadResume(snapshot.Source, snapshot.Destination, snapshot.BytesDone, snapshot.BytesTotal, snapshot.SourceModTime)
			} else {
				id, queued = view.worker.QueueUpload(snapshot.Source, snapshot.Destination, snapshot.Recursive, sftpclient.ExistingResume)
			}
		case sftpclient.OperationDownload:
			id, queued = view.worker.QueueDownload(snapshot.Source, snapshot.Destination, snapshot.Recursive, sftpclient.ExistingResume)
		default:
			continue
		}
		if !queued {
			continue
		}
		if center.byTask == nil {
			center.byTask = make(map[transferKey]int)
		}
		center.byTask[transferKey{view, id}] = index
		entry.owner = view
		entry.seen = view.transferScan
		resumed = true
	}
	if resumed {
		state.describeTransferSummary()
	}
}

func (state *State) trimTransferHistory() {
	center := &state.transfers
	for len(center.entries) > maxTransferHistory {
		index := slices.IndexFunc(center.entries, func(entry transferEntry) bool { return !transferActive(entry.snapshot) })
		if index < 0 {
			break
		}
		entry := &center.entries[index]
		if entry.owner != nil && !entry.owner.worker.ForgetTransfer(entry.snapshot.ID) {
			break
		}
		center.drop(index)
	}
}

// Transfer ownership follows a tab between windows, just like its worker and
// notify relay. Keeping a live entry in the old window would leave a stale badge
// and controls that operate on another window's connection.
func (state *State) detachTransfers(view *sftpView, dst []transferEntry) []transferEntry {
	if view == nil {
		return dst
	}
	for index := 0; index < len(state.transfers.entries); {
		entry := &state.transfers.entries[index]
		if entry.owner != view {
			index++
			continue
		}
		dst = append(dst, *entry)
		state.transfers.drop(index)
	}
	state.describeTransferSummary()
	return dst
}

func (state *State) adoptTransfers(entries []transferEntry) {
	center := &state.transfers
	if len(entries) == 0 {
		return
	}
	if center.byTask == nil {
		center.byTask = make(map[transferKey]int)
	}
	for _, entry := range entries {
		center.nextID++
		entry.id = center.nextID
		state.describeTransfer(&entry)
		if entry.owner != nil {
			center.byTask[transferKey{entry.owner, entry.snapshot.ID}] = len(center.entries)
		}
		center.entries = append(center.entries, entry)
	}
	state.trimTransferHistory()
	state.describeTransferSummary()
}

func (state *State) describeTransfer(entry *transferEntry) {
	snapshot, m := entry.snapshot, state.messages.Transfers
	entry.title = filepath.Base(snapshot.Source)
	if snapshot.Operation == sftpclient.OperationDownload || snapshot.Operation == sftpclient.OperationRemoteCopy {
		entry.title = path.Base(snapshot.Source)
	}
	mode := fs.FileMode(0)
	if snapshot.Recursive {
		mode = fs.ModeDir
	}
	entry.fileIcon, entry.monoIcon = sftpFileIcon(sftpclient.Entry{Name: entry.title, Mode: mode})
	entry.typeIcon = state.attachTypeIcon(entry.title, mode.IsDir())
	entry.searchTitle = strings.ToLower(entry.title)
	entry.status = m.Queued
	switch snapshot.State {
	case sftpclient.TransferRunning:
		entry.status = m.Running
		if snapshot.BytesTotal > 0 && snapshot.BytesDone >= snapshot.BytesTotal {
			entry.status = m.Finalizing
		}
	case sftpclient.TransferPaused:
		entry.status = m.Paused
	case sftpclient.TransferCompleted:
		entry.status = m.Completed
	case sftpclient.TransferFailed:
		entry.status = m.Failed
	case sftpclient.TransferCanceled:
		entry.status = m.Canceled
	case sftpclient.TransferInterrupted:
		entry.status = m.Interrupted
	}
	total := "—"
	if snapshot.BytesTotal > 0 || snapshot.State == sftpclient.TransferCompleted {
		total = formatBytes(snapshot.BytesTotal)
	}
	entry.amount = fmt.Sprintf(m.ProgressFmt, formatBytes(snapshot.BytesDone), total)
	entry.size = total
	entry.speed = fmt.Sprintf(state.messages.Tools.TransferSpeedFmt, formatBytes(snapshot.BytesPerSecond))
	entry.percent, entry.eta = "—", ""
	if snapshot.State == sftpclient.TransferRunning && snapshot.BytesTotal > 0 {
		entry.percent = transferPercent(transferFraction(snapshot))
		remaining := snapshot.BytesTotal - max(0, snapshot.BytesDone)
		if remaining > 0 && snapshot.BytesPerSecond > 0 {
			seconds := (remaining-1)/snapshot.BytesPerSecond + 1
			if seconds <= int64(math.MaxInt64)/int64(time.Second) {
				entry.eta = fmt.Sprintf(m.RemainingFmt, (time.Duration(seconds) * time.Second).String())
			}
		}
	}
	entry.conflict = entry.owner != nil && snapshot.Retryable && transferSnapshotConflict(snapshot)
	if entry.conflict {
		entry.status = m.Pending
	}
	started := snapshot.StartedAt
	if started.IsZero() {
		started = snapshot.QueuedAt
	}
	ended := snapshot.FinishedAt
	if ended.IsZero() {
		ended = time.Now()
	}
	entry.elapsed = "—"
	if !started.IsZero() {
		elapsed := max(ended.Sub(started), 0)
		if elapsed >= time.Millisecond {
			elapsed = elapsed.Round(time.Millisecond)
		}
		entry.elapsed = elapsed.String()
	}
	entry.outcome = entry.status
	switch {
	case snapshot.State == sftpclient.TransferCompleted && entry.elapsed != "—":
		entry.outcome += " · " + fmt.Sprintf(m.DurationFmt, entry.elapsed)
	case entry.conflict:
		entry.outcome += " · " + m.Conflict
	case snapshot.State == sftpclient.TransferFailed && snapshot.Error != "":
		entry.outcome += " · " + snapshot.Error
	}
}

func transferPercent(fraction float32) string {
	return strconv.Itoa(int(fraction*100+0.001)) + "%"
}

func (state *State) rebuildTransferCenter() {
	for index := range state.transfers.entries {
		state.describeTransfer(&state.transfers.entries[index])
	}
	state.describeTransferSummary()
}

func (state *State) describeTransferSummary() {
	center := &state.transfers
	center.revision++
	center.active, center.ended, center.failed, center.completed, center.canceled, center.interrupted = 0, 0, 0, 0, 0, 0
	center.pending = 0
	var rate, done, total, runningDone, runningTotal int64
	running := 0
	knownSize := true
	for _, entry := range center.entries {
		if entry.removed {
			continue
		}
		if entry.snapshot.State == sftpclient.TransferInterrupted {
			center.interrupted++
		}
		if transferActive(entry.snapshot) {
			center.active++
			if entry.snapshot.State == sftpclient.TransferRunning {
				running++
				rate += entry.snapshot.BytesPerSecond
				runningDone += entry.snapshot.BytesDone
				runningTotal += entry.snapshot.BytesTotal
				knownSize = knownSize && entry.snapshot.BytesTotal > 0
			}
			done += entry.snapshot.BytesDone
			total += entry.snapshot.BytesTotal
		} else if entry.conflict {
			center.pending++
		} else {
			center.ended++
			if entry.snapshot.State == sftpclient.TransferFailed {
				center.failed++
			}
			if entry.snapshot.State == sftpclient.TransferCompleted {
				center.completed++
			}
			if entry.snapshot.State == sftpclient.TransferCanceled {
				center.canceled++
			}
		}
	}
	// A transfer that moves bytes keeps the machine out of idle sleep; one that
	// is queued or paused does not.
	state.keepAwakeForTransfers(running > 0)
	m := state.messages.Transfers
	center.summary = m.Idle
	center.badge = ""
	if center.active > 0 {
		center.badge = strconv.Itoa(center.active)
		if center.active > 99 {
			center.badge = "99+"
		}
		center.summary = fmt.Sprintf(m.SummaryFmt, center.active, formatBytes(rate))
	}
	center.buttonLabel = m.Title
	if center.active > 0 {
		center.buttonLabel += " · " + center.summary
	}
	center.done, center.total = done, total
	overview := transferOverview{title: m.Idle, percent: "—"}
	if running > 0 {
		overview.title = fmt.Sprintf(m.RunningCountFmt, running)
		overview.rate = fmt.Sprintf(state.messages.Tools.TransferSpeedFmt, formatBytes(rate))
		totalText := "—"
		if knownSize && runningTotal > 0 {
			totalText = formatBytes(runningTotal)
			overview.fraction = min(0.99, max(0, float32(runningDone)/float32(runningTotal)))
			overview.percent = transferPercent(overview.fraction)
		}
		overview.amount = fmt.Sprintf(m.ProgressFmt, formatBytes(runningDone), totalText)
	} else if center.active > 0 {
		overview.title = fmt.Sprintf(m.WaitingCountFmt, center.active)
	}
	center.overview = overview
	center.rebuildCompactRows()
	state.describeTransferScope()
	// Every change to a transfer ends here, so this is where the desktop hears
	// about it: the icon it draws stands for the window and for its siblings.
	state.reportTransferProgress()
}

// rebuildCompactRows keeps the corner notice independent of the full center's
// saved filters. Every live record is present; running work leads, then waiting
// and conflicted work, followed by failures and completed history.
func (center *transferCenter) rebuildCompactRows() {
	center.compactRows = center.compactRows[:0]
	for index := range center.entries {
		if !center.entries[index].removed {
			center.compactRows = append(center.compactRows, index)
		}
	}
	slices.SortFunc(center.compactRows, func(a, b int) int {
		left, right := &center.entries[a], &center.entries[b]
		leftGroup, rightGroup := transferEntryGroup(left), transferEntryGroup(right)
		if leftGroup != rightGroup {
			return compactTransferGroupRank(leftGroup) - compactTransferGroupRank(rightGroup)
		}
		return compareTransferOrder(leftGroup, left, right)
	})
}

func compactTransferGroupRank(group int) int {
	switch group {
	case transferFilterRunning:
		return 0
	case transferFilterWaiting:
		return 1
	case transferFilterPending:
		return 2
	case transferFilterFailed:
		return 3
	default:
		return 4
	}
}

func (state *State) removeTransfer(index int) {
	center := &state.transfers
	entry := &center.entries[index]
	if transferActive(entry.snapshot) {
		return
	}
	center.revision++
	if entry.owner != nil {
		entry.owner.invalidateDetached()
	}
	if entry.owner != nil && !entry.owner.worker.ForgetTransfer(entry.snapshot.ID) {
		entry.removed = true
	} else {
		center.drop(index)
	}
}

func (center *transferCenter) drop(index int) {
	entry := &center.entries[index]
	if entry.owner != nil {
		delete(center.byTask, transferKey{entry.owner, entry.snapshot.ID})
	}
	center.entries = slices.Delete(center.entries, index, index+1)
	for next := index; next < len(center.entries); next++ {
		entry := &center.entries[next]
		if entry.owner != nil && center.byTask != nil {
			center.byTask[transferKey{entry.owner, entry.snapshot.ID}] = next
		}
	}
}

func (state *State) clickTransferCenter(id gfx.ID) bool {
	center := &state.transfers
	if view := state.transferConflictView(); view != nil && state.clickTransferConflict(view, id) {
		return true
	}
	if !state.transferCenterAvailable() {
		return false
	}
	if state.clickTransferBulk(id) {
		return true
	}
	switch id {
	case hitTransferCenter:
		center.open = !center.open
		if center.open {
			center.compact = false
			state.rebuildTransferCenter()
		} else {
			center.close()
		}
		return true
	case hitTransfersExpand:
		center.compact = false
		state.rebuildTransferCenter()
		return true
	case hitTransfersDismiss, hitTransfersClose:
		center.close()
		return true
	case hitTransfersDetailDismiss:
		center.details.dismiss()
		return true
	case hitTransfersClear:
		for index := len(center.entries) - 1; index >= 0; index-- {
			if center.shows(&center.entries[index]) {
				state.removeTransfer(index)
			}
		}
		state.describeTransferSummary()
		return true
	case hitTransfersCancelAll:
		state.changeTransferQueue(true)
		return true
	case hitTransfersPauseAll:
		state.changeTransferQueue(false)
		return true
	}
	if filter, ok := id.In(hitTransfersFilter); ok {
		if filter >= transferFilterAll && filter < transferFilterCount && filter != center.filter {
			center.filter, center.scroll = filter, 0
			center.details.dismiss()
			state.describeTransferScope()
			state.save()
		}
		return true
	}
	if group, ok := id.In(hitTransfersGroup); ok {
		if group > transferFilterAll && group < transferFilterCount {
			section := &center.groups[group]
			section.collapsed = !section.collapsed
			center.details.dismiss()
			center.rebuildRows()
		}
		return true
	}
	if entry, ok := id.In(hitTransfersDetails); ok {
		if center.details.pinned == entry {
			center.details.dismiss()
		} else {
			center.details.pinned = entry
		}
		return true
	}
	return state.clickTransferTask(id, nil)
}

func (center *transferCenter) close() {
	center.open, center.compact = false, false
	center.batch = transferBatch{}
	center.details.dismiss()
}

// Bulk controls apply to the current filters, including tasks in collapsed
// groups. Refresh each affected worker only after walking the current entries.
func (state *State) changeTransferQueue(cancel bool) {
	center := &state.transfers
	resume := !cancel && center.pausable == 0
	var changed map[*sftpView]struct{}
	apply := func(index int) {
		entry := &center.entries[index]
		if !center.shows(entry) || !transferActive(entry.snapshot) || entry.owner == nil || entry.owner.worker == nil {
			return
		}
		worker, id := entry.owner.worker, entry.snapshot.ID
		var ok bool
		switch {
		case cancel:
			ok = worker.CancelTransfer(id)
		case resume:
			ok = worker.Resume(id)
		default:
			ok = worker.Pause(id)
		}
		if ok {
			if changed == nil {
				changed = make(map[*sftpView]struct{})
			}
			changed[entry.owner] = struct{}{}
		}
	}
	if resume {
		// Resume can immediately start a job. Give the older waiting jobs
		// the first chance even when a retry has reused an early record ID.
		for _, index := range center.groups[transferFilterWaiting].members {
			apply(index)
		}
	} else {
		for index := range center.entries {
			apply(index)
		}
	}
	for owner := range changed {
		if owner.worker == nil {
			continue
		}
		state.refreshSFTPTransfers(owner)
		owner.invalidateDetached()
	}
}

func (state *State) clickTransferTask(id gfx.ID, view *sftpView) bool {
	center := &state.transfers
	for index := range center.entries {
		entry := &center.entries[index]
		if item(id) != entry.id || (view != nil && entry.owner != view) {
			continue
		}
		if id.Scope() == hitTransfersReveal {
			if transferCanReveal(entry.snapshot) {
				state.revealTransferPath(entry.snapshot.Destination)
			}
			return true
		}
		if transferActive(entry.snapshot) && id.Scope() == hitTransfersRemove {
			return true
		}
		if id.Scope() == hitTransfersRemove {
			state.removeTransfer(index)
			state.describeTransferSummary()
			return true
		}
		if entry.owner == nil || entry.owner.worker == nil {
			continue
		}
		owner := entry.owner
		worker, snapshot := owner.worker, entry.snapshot
		retrying := false
		switch id.Scope() {
		case hitTransfersCancel:
			worker.CancelTransfer(snapshot.ID)
		case hitTransfersPause:
			if snapshot.State == sftpclient.TransferPaused {
				worker.Resume(snapshot.ID)
			} else {
				worker.Pause(snapshot.ID)
			}
		case hitTransfersRetry:
			retrying = worker.Retry(snapshot.ID, transferRetryPolicy(snapshot))
		case hitTransfersReplace:
			retrying = worker.Retry(snapshot.ID, sftpclient.ExistingReplace)
		case hitTransfersResume:
			retrying = worker.Retry(snapshot.ID, sftpclient.ExistingResume)
		case hitTransfersSkip:
			retrying = worker.Retry(snapshot.ID, sftpclient.ExistingSkip)
		default:
			continue
		}
		if retrying {
			owner.errorText = ""
			if center.filter == transferFilterPending {
				center.filter, center.scroll = transferFilterAll, 0
			}
			if owner.detached != nil && snapshot.Operation == sftpclient.OperationDownload {
				owner.detached.localRefresh = true
			}
		}
		state.refreshSFTPTransfers(owner)
		owner.invalidateDetached()
		return true
	}
	return id.Scope() == transferCenterHits
}

func (center *transferCenter) shows(entry *transferEntry) bool {
	return center.matches(entry) && (center.filter == transferFilterAll || center.filter == transferEntryGroup(entry))
}

func (center *transferCenter) matches(entry *transferEntry) bool {
	return !entry.removed && (center.host == nil || sameTransferHost(center.host, entry.host)) &&
		(center.direction == transferAnyDirection || center.direction == transferDirectionOf(entry.snapshot.Operation)) &&
		(center.query == "" || strings.Contains(entry.searchTitle, center.query))
}

func transferDirectionOf(operation sftpclient.Operation) transferDirection {
	switch operation {
	case sftpclient.OperationUpload:
		return transferUpload
	case sftpclient.OperationRemoteCopy:
		return transferRemoteCopy
	default:
		return transferDownload
	}
}
