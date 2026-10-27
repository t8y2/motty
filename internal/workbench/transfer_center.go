package workbench

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var transferCenterHits = tree.Feature("transfer-center")

var (
	hitTransferCenter     = tree.ID(transferCenterHits, "toggle")
	hitTransfersDismiss   = tree.ID(transferCenterHits, "dismiss")
	hitTransfersClose     = tree.ID(transferCenterHits, "close")
	hitTransfersScope     = tree.ID(transferCenterHits, "scope")
	hitTransfersList      = tree.ID(transferCenterHits, "list")
	hitTransfersBar       = tree.ID(transferCenterHits, "scrollbar")
	hitTransfersClear     = tree.ID(transferCenterHits, "clear")
	hitTransfersCancelAll = tree.ID(transferCenterHits, "cancel-all")
	hitTransfersFilter    = tree.Sub(transferCenterHits, tree.Name("filter"))
	hitTransfersPause     = tree.Sub(transferCenterHits, tree.Name("pause"))
	hitTransfersCancel    = tree.Sub(transferCenterHits, tree.Name("cancel"))
	hitTransfersRemove    = tree.Sub(transferCenterHits, tree.Name("remove"))
	hitTransfersRetry     = tree.Sub(transferCenterHits, tree.Name("retry"))
	hitTransfersReplace   = tree.Sub(transferCenterHits, tree.Name("replace"))
	hitTransfersResume    = tree.Sub(transferCenterHits, tree.Name("resume"))
	hitTransfersSkip      = tree.Sub(transferCenterHits, tree.Name("skip"))
	hitTransfersStatus    = tree.Sub(transferCenterHits, tree.Name("status"))
)

const transferCardHeight = float32(108)
const maxTransferHistory = 500

// transferEntry keeps the identity independent of worker-local IDs and tabs.
// A closed connection releases its view; its finished snapshot remains readable.
type transferEntry struct {
	id                                         int
	owner                                      *sftpView
	snapshot                                   sftpclient.TransferSnapshot
	server, address                            string
	title, status, amount, speed, clock, paths string
	removed                                    bool
	conflict                                   bool
	seen                                       uint64
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
	byTask        map[transferKey]int
	summary       string
	badge         string
	buttonLabel   string
	active, ended int
	fraction      float32
	view          flex.View
	row           flex.View
	surface, list ui.Rect
	geometry      components.ScrollAreaGeometry
	scroll        float32
	count         int
	dragging      bool
	dragOffset    float32
}

func transferActive(snapshot sftpclient.TransferSnapshot) bool {
	return snapshot.State == sftpclient.TransferQueued || snapshot.State == sftpclient.TransferRunning || snapshot.State == sftpclient.TransferPaused
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
	state.closeSFTP(session)
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
			center.nextID++
			center.entries = append(center.entries, transferEntry{id: center.nextID, owner: view, server: view.transferServer, address: view.transferAddress})
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
	}
	// The worker bounds its own history independently of the global history.
	for index := range center.entries {
		entry := &center.entries[index]
		if entry.owner == view && entry.seen != view.transferScan {
			delete(center.byTask, transferKey{view, entry.snapshot.ID})
			entry.owner = nil
			entry.snapshot.Retryable = false
			changed = true
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
			state.describeTransfer(entry)
		}
	}
	state.describeTransferSummary()
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
	if snapshot.Operation == sftpclient.OperationDownload {
		entry.title = path.Base(snapshot.Source)
	}
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
	}
	total := "—"
	if snapshot.BytesTotal > 0 || snapshot.State == sftpclient.TransferCompleted {
		total = formatBytes(snapshot.BytesTotal)
	}
	entry.amount = fmt.Sprintf(m.ProgressFmt, formatBytes(snapshot.BytesDone), total)
	entry.speed = fmt.Sprintf(state.messages.Tools.TransferSpeedFmt, formatBytes(snapshot.BytesPerSecond))
	entry.paths = snapshot.Source + " → " + snapshot.Destination
	entry.conflict = entry.owner != nil && snapshot.State == sftpclient.TransferFailed && strings.Contains(snapshot.Error, sftpclient.ErrDestinationExists.Error())
	started := snapshot.StartedAt
	if started.IsZero() {
		started = snapshot.QueuedAt
	}
	ended := snapshot.FinishedAt
	if ended.IsZero() {
		ended = time.Now()
	}
	duration := max(ended.Sub(started), 0).Round(time.Second)
	entry.clock = fmt.Sprintf(m.TimeFmt, snapshot.QueuedAt.Local().Format("01-02 15:04:05"), duration.String())
}

func (state *State) rebuildTransferCenter() {
	for index := range state.transfers.entries {
		state.describeTransfer(&state.transfers.entries[index])
	}
	state.describeTransferSummary()
}

func (state *State) describeTransferSummary() {
	center := &state.transfers
	center.active, center.ended = 0, 0
	var rate, done, total int64
	for _, entry := range center.entries {
		if entry.removed {
			continue
		}
		if transferActive(entry.snapshot) {
			center.active++
			rate += entry.snapshot.BytesPerSecond
			done += entry.snapshot.BytesDone
			total += entry.snapshot.BytesTotal
		} else {
			center.ended++
		}
	}
	center.summary = state.messages.Transfers.Idle
	center.badge = ""
	if center.active > 0 {
		center.badge = strconv.Itoa(center.active)
		if center.active > 99 {
			center.badge = "99+"
		}
	}
	if center.active > 0 {
		center.summary = fmt.Sprintf(state.messages.Transfers.SummaryFmt, center.active, formatBytes(rate))
	}
	center.buttonLabel = state.messages.Transfers.Title
	if center.active > 0 {
		center.buttonLabel += " · " + center.summary
	}
	center.fraction = 0
	if total > 0 {
		center.fraction = min(0.99, float32(done)/float32(total))
	}
}

func (state *State) removeTransfer(index int) {
	center := &state.transfers
	entry := &center.entries[index]
	if transferActive(entry.snapshot) {
		return
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

func (state *State) clickTransferCenter(id ui.ID) bool {
	center := &state.transfers
	if !state.transferCenterAvailable() {
		return false
	}
	switch id {
	case hitTransferCenter:
		center.open = !center.open
		if center.open {
			state.rebuildTransferCenter()
		}
		return true
	case hitTransfersDismiss, hitTransfersClose:
		center.open, center.dragging = false, false
		return true
	case hitTransfersClear:
		for index := len(center.entries) - 1; index >= 0; index-- {
			state.removeTransfer(index)
		}
		state.describeTransferSummary()
		return true
	case hitTransfersCancelAll:
		for index := range center.entries {
			entry := &center.entries[index]
			if transferActive(entry.snapshot) && entry.owner != nil {
				entry.owner.worker.CancelTransfer(entry.snapshot.ID)
			}
		}
		state.refreshAllTransfers()
		return true
	}
	if filter, ok := id.In(hitTransfersFilter); ok {
		center.filter, center.scroll = min(filter, 2), 0
		return true
	}
	for index := range center.entries {
		entry := &center.entries[index]
		if item(id) != entry.id {
			continue
		}
		if transferActive(entry.snapshot) && id.Scope() == hitTransfersRemove {
			return true
		}
		if id.Scope() == hitTransfersRemove {
			state.removeTransfer(index)
			state.describeTransferSummary()
			return true
		}
		if entry.owner == nil {
			continue
		}
		worker, snapshot := entry.owner.worker, entry.snapshot
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
			retrying = worker.Retry(snapshot.ID, sftpclient.ExistingFail)
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
			entry.owner.errorText = ""
		}
		state.refreshSFTPTransfers(entry.owner)
		return true
	}
	return id.Scope() == transferCenterHits
}

func (state *State) refreshAllTransfers() {
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view.worker != nil {
			state.refreshSFTPTransfers(view)
		}
	}
}

func (center *transferCenter) shows(entry *transferEntry) bool {
	return !entry.removed && (center.filter == 0 || (center.filter == 1 && transferActive(entry.snapshot)) || (center.filter == 2 && !transferActive(entry.snapshot)))
}
