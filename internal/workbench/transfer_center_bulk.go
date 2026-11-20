package workbench

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

var (
	hitTransfersRetryFailed     = tree.ID(transferCenterHits, "retry-failed")
	hitTransfersSkipVisible     = tree.ID(transferCenterHits, "skip-visible")
	hitTransfersReplaceVisible  = tree.ID(transferCenterHits, "replace-visible")
	hitTransfersResumeVisible   = tree.ID(transferCenterHits, "resume-visible")
	hitTransfersBulkConfirm     = tree.ID(transferCenterHits, "bulk-confirm")
	hitTransfersBulkCancel      = tree.ID(transferCenterHits, "bulk-cancel")
	hitTransfersScopeLabel      = tree.ID(transferCenterHits, "scope-label")
	hitTransfersBulkDescription = tree.ID(transferCenterHits, "bulk-description")
	transferConnectionIDs       atomic.Uint64
)

// Capture host identity at connection time, not from a mutable catalog or a tab's
// suffixed title. This token survives moves/archive without retaining the worker.
type transferHost struct {
	catalog, address, label string
	unknown                 uint64
}

func newTransferHost(server, address string) *transferHost {
	id := transferConnectionIDs.Add(1)
	return &transferHost{unknown: id, label: fmt.Sprintf("%s · %s · #%d", server, address, id)}
}

func transferHostFor(session *Session) *transferHost {
	server := session.host.Label
	if server == "" {
		server = session.Title
	}
	if session.Address == "" {
		return newTransferHost(server, session.Address)
	}
	label := server + " · " + session.Address
	if session.host.CatalogID != "" {
		label += " · " + session.host.CatalogID
	}
	return &transferHost{catalog: session.host.CatalogID, address: session.Address, label: label}
}

func sameTransferHost(a, b *transferHost) bool {
	return a == b || a != nil && b != nil && a.catalog == b.catalog && a.address == b.address && a.unknown == b.unknown
}

type transferTarget struct {
	entry   int
	owner   *sftpView
	worker  *sftpclient.Worker
	id      uint64
	attempt uint64
	queued  time.Time
}

type transferBatch struct {
	targets                    []transferTarget
	policy                     sftpclient.ExistingPolicy
	host                       *transferHost
	filter                     int
	view                       *sftpView // a window's own queue, not the center's filtered one
	title, description, action string
}

func transferFilterShows(filter int, snapshot sftpclient.TransferSnapshot) bool {
	return filter == transferFilterAll || filter == transferGroupOf(snapshot.State)
}

func transferGroupOf(phase sftpclient.TransferState) int {
	switch phase {
	case sftpclient.TransferRunning:
		return transferFilterRunning
	case sftpclient.TransferQueued, sftpclient.TransferPaused:
		return transferFilterWaiting
	case sftpclient.TransferFailed:
		return transferFilterFailed
	default:
		return transferFilterEnded
	}
}

// Leave the badge to report progress when a saved status or an explicit
// search would otherwise open the center with the newly queued job hidden.
func (state *State) openTransferCenterForQueued() {
	center := &state.transfers
	if center.open || center.query != "" || center.direction != transferAnyDirection ||
		center.groups[transferFilterWaiting].collapsed ||
		!transferFilterShows(center.filter, sftpclient.TransferSnapshot{State: sftpclient.TransferQueued}) {
		return
	}
	center.open, center.scroll = true, 0
}

func (state *State) showTransferConflict() {
	center := &state.transfers
	center.filter, center.host, center.direction = transferFilterFailed, nil, transferAnyDirection
	center.query = ""
	center.search.SetText("")
	center.details.dismiss()
	center.groups[transferFilterFailed].collapsed = false
	center.open, center.scroll = true, 0
	state.describeTransferScope()
}

func transferRetryEligible(entry *transferEntry, conflict bool) bool {
	return !entry.removed && entry.owner != nil && entry.owner.worker != nil && entry.snapshot.State == sftpclient.TransferFailed && entry.snapshot.Retryable && entry.conflict == conflict
}

// Labels and counts change with transfer/filter events, never in steady paint.
func (state *State) describeTransferScope() {
	center, m := &state.transfers, state.messages.Transfers
	center.hosts = center.hosts[:0]
	center.hasEntries = false
	center.count, center.visibleActive, center.visibleEnded, center.retryCount, center.conflictCount = 0, 0, 0, 0, 0
	center.pausable, center.paused = 0, 0
	for _, group := range transferGroups {
		center.groups[group].members = center.groups[group].members[:0]
	}
	matched := 0
	for index := range center.entries {
		entry := &center.entries[index]
		if entry.removed {
			continue
		}
		center.hasEntries = true
		if entry.host != nil && !slices.ContainsFunc(center.hosts, func(host *transferHost) bool { return sameTransferHost(host, entry.host) }) {
			center.hosts = append(center.hosts, entry.host)
		}
		if !center.matches(entry) {
			continue
		}
		matched++
		group := transferGroupOf(entry.snapshot.State)
		center.groups[group].members = append(center.groups[group].members, index)
		if center.filter != transferFilterAll && center.filter != group {
			continue
		}
		center.count++
		if transferActive(entry.snapshot) {
			center.visibleActive++
			if entry.snapshot.State == sftpclient.TransferPaused {
				center.paused++
			} else {
				center.pausable++
			}
		} else {
			center.visibleEnded++
		}
		if transferRetryEligible(entry, false) {
			center.retryCount++
		}
		if transferRetryEligible(entry, true) {
			center.conflictCount++
		}
	}
	if center.host != nil && !slices.ContainsFunc(center.hosts, func(host *transferHost) bool { return sameTransferHost(host, center.host) }) {
		center.hosts = append(center.hosts, center.host)
	}
	center.hostChoices = append(center.hostChoices[:0], nil)
	center.hostChoices = append(center.hostChoices, center.hosts...)
	center.hostLabel = m.AllHosts
	if center.host != nil {
		center.hostLabel = center.host.label
	}
	labels := state.transferFilterLabels()
	center.groups[transferFilterAll].count = strconv.Itoa(matched)
	for _, group := range transferGroups {
		section := &center.groups[group]
		section.count = strconv.Itoa(len(section.members))
		section.label = labels[group] + " (" + section.count + ")"
		slices.SortFunc(section.members, func(a, b int) int {
			return compareTransferOrder(group, &center.entries[a], &center.entries[b])
		})
	}
	scope := center.hostLabel
	if center.direction != transferAnyDirection {
		scope += " · " + state.transferDirectionLabel(center.direction)
	}
	if center.query != "" {
		scope += " · " + strings.TrimSpace(center.search.Text())
	}
	center.scopeLabel = fmt.Sprintf(m.ScopeFmt, scope, labels[center.filter], center.count)
	center.cancelLabel, center.clearLabel = m.CancelAll, m.Clear
	center.pauseLabel = m.PauseAll
	if center.pausable == 0 && center.paused > 0 {
		center.pauseLabel = m.ResumeAll
	}
	if center.host != nil || center.filter != transferFilterAll || center.direction != transferAnyDirection || center.query != "" {
		center.cancelLabel, center.clearLabel = m.CancelVisible, m.ClearVisible
		center.pauseLabel = m.PauseVisible
		if center.pausable == 0 && center.paused > 0 {
			center.pauseLabel = m.ResumeVisible
		}
	}
	center.retryLabel = fmt.Sprintf(m.RetryFailedFmt, center.retryCount)
	center.conflictLabel = fmt.Sprintf(m.ConflictCountFmt, center.conflictCount)
	center.rebuildRows()
}

func compareTransferOrder(group int, a, b *transferEntry) int {
	left, right := transferOrderTime(group, &a.snapshot), transferOrderTime(group, &b.snapshot)
	// Unknown times should not jump ahead of records with real times in either order.
	if left.IsZero() != right.IsZero() {
		if left.IsZero() {
			return 1
		}
		return -1
	}
	order := left.Compare(right)
	if order == 0 && group == transferFilterWaiting {
		order = cmp.Compare(a.snapshot.QueueOrder, b.snapshot.QueueOrder)
	}
	if order == 0 {
		order = cmp.Compare(a.id, b.id)
	}
	if group == transferFilterWaiting {
		return order
	}
	return -order
}

func transferOrderTime(group int, snapshot *sftpclient.TransferSnapshot) time.Time {
	if (group == transferFilterEnded || group == transferFilterFailed) && !snapshot.FinishedAt.IsZero() {
		return snapshot.FinishedAt
	}
	if group != transferFilterWaiting && !snapshot.StartedAt.IsZero() {
		return snapshot.StartedAt
	}
	return snapshot.QueuedAt
}

func (center *transferCenter) rebuildRows() {
	center.rows = center.rows[:0]
	for _, group := range transferGroups {
		section := &center.groups[group]
		if len(section.members) == 0 || center.filter != transferFilterAll && center.filter != group {
			continue
		}
		center.rows = append(center.rows, transferRow{group: group, entry: -1})
		if !section.collapsed {
			for _, index := range section.members {
				center.rows = append(center.rows, transferRow{group: group, entry: index})
			}
		}
	}
}

func (state *State) transferFilterLabels() [transferFilterCount]string {
	m := state.messages.Transfers
	return [transferFilterCount]string{m.All, m.Active, m.Ended, m.Failed, m.Waiting}
}

func (state *State) transferDirectionLabel(direction transferDirection) string {
	m := state.messages.Transfers
	return [transferDirectionCount]string{m.AllDirections, m.Upload, m.Download, m.RemoteCopy}[direction]
}

func (state *State) updateTransferSearch() {
	center := &state.transfers
	query := strings.ToLower(strings.TrimSpace(center.search.Text()))
	if center.query != query {
		center.query, center.scroll = query, 0
		center.details.dismiss()
		state.describeTransferScope()
	}
}

// captureViewTransferBatch captures the conflicting transfers of one transfer
// window, so its queue can offer the same overwrite/skip choice as the center
// without depending on the center's filters or another window's tasks.
func (state *State) captureViewTransferBatch(view *sftpView, policy sftpclient.ExistingPolicy) transferBatch {
	center := &state.transfers
	batch := transferBatch{policy: policy, filter: transferFilterAll, view: view}
	for index := range center.entries {
		entry := &center.entries[index]
		if entry.owner != view {
			continue
		}
		if transferRetryEligible(entry, policy != sftpclient.ExistingFail) {
			batch.targets = append(batch.targets, transferTarget{
				entry.id, entry.owner, entry.owner.worker, entry.snapshot.ID,
				entry.snapshot.QueueOrder, entry.snapshot.QueuedAt,
			})
		}
	}
	m := state.messages.Transfers
	batch.action = m.Replace
	if policy == sftpclient.ExistingResume {
		batch.action = m.Resume
	}
	batch.title = fmt.Sprintf(m.ConfirmBatchFmt, batch.action, len(batch.targets))
	batch.description = fmt.Sprintf(m.BatchScopeFmt, state.messages.SFTPTransferQueue)
	return batch
}

func (state *State) captureTransferBatch(policy sftpclient.ExistingPolicy) transferBatch {
	center := &state.transfers
	batch := transferBatch{policy: policy, host: center.host, filter: center.filter}
	for index := range center.entries {
		entry := &center.entries[index]
		if center.shows(entry) && transferRetryEligible(entry, policy != sftpclient.ExistingFail) {
			batch.targets = append(batch.targets, transferTarget{
				entry.id, entry.owner, entry.owner.worker, entry.snapshot.ID,
				entry.snapshot.QueueOrder, entry.snapshot.QueuedAt,
			})
		}
	}
	m := state.messages.Transfers
	batch.action = m.Replace
	if policy == sftpclient.ExistingResume {
		batch.action = m.Resume
	}
	batch.title = fmt.Sprintf(m.ConfirmBatchFmt, batch.action, len(batch.targets))
	batch.description = fmt.Sprintf(m.BatchScopeFmt, center.scopeLabel)
	return batch
}

func (state *State) dispatchTransferBatch(batch transferBatch) {
	// Each captured worker is refreshed once before eligibility is checked. A
	// stale UI failure must never revive a canceled task or another retry attempt.
	refreshed := make(map[*sftpView]bool)
	for _, target := range batch.targets {
		if state.transfers.capturedEntry(target) == nil || refreshed[target.owner] {
			continue
		}
		state.refreshSFTPTransfers(target.owner)
		refreshed[target.owner] = true
	}
	for _, target := range batch.targets {
		entry := state.transfers.capturedEntry(target)
		if entry == nil || entry.snapshot.QueueOrder != target.attempt ||
			!transferRetryEligible(entry, batch.policy != sftpclient.ExistingFail) {
			continue
		}
		if batch.view != nil {
			if entry.owner != batch.view {
				continue
			}
		} else if !state.transfers.shows(entry) || !transferFilterShows(batch.filter, entry.snapshot) ||
			batch.host != nil && !sameTransferHost(entry.host, batch.host) {
			continue
		}
		policy := batch.policy
		if policy == sftpclient.ExistingFail {
			policy = transferRetryPolicy(entry.snapshot)
		}
		if target.worker.Retry(target.id, policy) {
			target.owner.errorText = ""
			if target.owner.detached != nil && entry.snapshot.Operation == sftpclient.OperationDownload {
				target.owner.detached.localRefresh = true
			}
			target.owner.invalidateDetached()
		}
	}
	for owner := range refreshed {
		state.refreshSFTPTransfers(owner)
	}
	state.describeTransferScope()
}

func (center *transferCenter) capturedEntry(target transferTarget) *transferEntry {
	if target.owner == nil || target.owner.worker != target.worker {
		return nil
	}
	index, exists := center.byTask[transferKey{target.owner, target.id}]
	if !exists || index < 0 || index >= len(center.entries) {
		return nil
	}
	entry := &center.entries[index]
	if entry.id != target.entry || entry.owner != target.owner || entry.removed {
		return nil
	}
	return entry
}

func (state *State) clickTransferBulk(id gfx.ID) bool {
	center := &state.transfers
	if len(center.batch.targets) > 0 {
		switch id {
		case hitTransfersBulkCancel:
			center.batch = transferBatch{}
		case hitTransfersBulkConfirm:
			batch := center.batch
			center.batch = transferBatch{}
			state.dispatchTransferBatch(batch)
		case hitTransfersDismiss, hitTransfersClose, hitTransferCenter:
			center.close()
		}
		return true
	}
	switch id {
	case hitTransfersRetryFailed, hitTransfersSkipVisible, hitTransfersReplaceVisible, hitTransfersResumeVisible:
		policy := sftpclient.ExistingFail
		switch id {
		case hitTransfersSkipVisible:
			policy = sftpclient.ExistingSkip
		case hitTransfersReplaceVisible:
			policy = sftpclient.ExistingReplace
		case hitTransfersResumeVisible:
			policy = sftpclient.ExistingResume
		}
		batch := state.captureTransferBatch(policy)
		if len(batch.targets) == 0 {
			return true
		}
		center.details.dismiss()
		if policy == sftpclient.ExistingReplace || policy == sftpclient.ExistingResume {
			center.batch = batch
		} else {
			state.dispatchTransferBatch(batch)
		}
		return true
	}
	return false
}

func transferSnapshotConflict(snapshot sftpclient.TransferSnapshot) bool {
	return snapshot.State == sftpclient.TransferFailed && strings.Contains(snapshot.Error, sftpclient.ErrDestinationExists.Error())
}
