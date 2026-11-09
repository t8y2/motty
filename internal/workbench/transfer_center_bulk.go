package workbench

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

var (
	hitTransfersHost            = tree.ID(transferCenterHits, "host")
	hitTransfersHostDismiss     = tree.ID(transferCenterHits, "host-dismiss")
	hitTransfersHostItems       = tree.Sub(transferCenterHits, tree.Name("host-item"))
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
	entry  int
	owner  *sftpView
	worker *sftpclient.Worker
	id     uint64
	queued time.Time
}

type transferBatch struct {
	targets                    []transferTarget
	policy                     sftpclient.ExistingPolicy
	host                       *transferHost
	filter                     int
	title, description, action string
}

func transferFilterShows(filter int, snapshot sftpclient.TransferSnapshot) bool {
	return filter == 0 || filter == 1 && transferActive(snapshot) || filter == 2 && !transferActive(snapshot) || filter == 3 && snapshot.State == sftpclient.TransferFailed
}

func transferRetryEligible(entry *transferEntry, conflict bool) bool {
	return !entry.removed && entry.owner != nil && entry.owner.worker != nil && entry.snapshot.State == sftpclient.TransferFailed && entry.snapshot.Retryable && entry.conflict == conflict
}

// Labels and counts change with transfer/filter events, never in steady paint.
func (state *State) describeTransferScope() {
	center, m := &state.transfers, state.messages.Transfers
	center.hosts = center.hosts[:0]
	center.count, center.visibleActive, center.visibleEnded, center.retryCount, center.conflictCount = 0, 0, 0, 0, 0
	for index := range center.entries {
		entry := &center.entries[index]
		if !entry.removed && entry.host != nil && !slices.ContainsFunc(center.hosts, func(host *transferHost) bool { return sameTransferHost(host, entry.host) }) {
			center.hosts = append(center.hosts, entry.host)
		}
		if !center.shows(entry) {
			continue
		}
		center.count++
		if transferActive(entry.snapshot) {
			center.visibleActive++
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
	center.hostLabel = m.AllHosts
	if center.host != nil {
		center.hostLabel = center.host.label
	}
	labels := [4]string{m.All, m.Active, m.Ended, m.Failed}
	center.scopeLabel = fmt.Sprintf(m.ScopeFmt, center.hostLabel, labels[center.filter], center.count)
	center.cancelLabel, center.clearLabel = m.CancelAll, m.Clear
	if center.host != nil || center.filter != 0 {
		center.cancelLabel, center.clearLabel = m.CancelVisible, m.ClearVisible
	}
	center.retryLabel = fmt.Sprintf(m.RetryFailedFmt, center.retryCount)
	center.conflictLabel = fmt.Sprintf(m.ConflictCountFmt, center.conflictCount)
}

func (state *State) captureTransferBatch(policy sftpclient.ExistingPolicy) transferBatch {
	center := &state.transfers
	batch := transferBatch{policy: policy, host: center.host, filter: center.filter}
	for index := range center.entries {
		entry := &center.entries[index]
		if center.shows(entry) && transferRetryEligible(entry, policy != sftpclient.ExistingFail) {
			batch.targets = append(batch.targets, transferTarget{entry.id, entry.owner, entry.owner.worker, entry.snapshot.ID, entry.snapshot.QueuedAt})
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
		if entry == nil || entry.snapshot.QueuedAt != target.queued || !state.transfers.shows(entry) ||
			!transferFilterShows(batch.filter, entry.snapshot) || batch.host != nil && !sameTransferHost(entry.host, batch.host) ||
			!transferRetryEligible(entry, batch.policy != sftpclient.ExistingFail) {
			continue
		}
		if target.worker.Retry(target.id, batch.policy) {
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
			center.batch = transferBatch{}
			center.open = false
		}
		return true
	}
	switch id {
	case hitTransfersHost:
		center.hostOpen = !center.hostOpen
		return true
	case hitTransfersHostDismiss:
		center.hostOpen = false
		return true
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
		center.hostOpen = false
		if policy == sftpclient.ExistingReplace || policy == sftpclient.ExistingResume {
			center.batch = batch
		} else {
			state.dispatchTransferBatch(batch)
		}
		return true
	}
	if index, ok := id.In(hitTransfersHostItems); ok {
		if index < 0 || index > len(center.hosts) {
			return true
		}
		center.host = nil
		if index > 0 {
			center.host = center.hosts[index-1]
		}
		center.hostOpen, center.scroll = false, 0
		state.describeTransferScope()
		return true
	}
	return false
}

func transferSnapshotConflict(snapshot sftpclient.TransferSnapshot) bool {
	return snapshot.State == sftpclient.TransferFailed && strings.Contains(snapshot.Error, sftpclient.ErrDestinationExists.Error())
}
