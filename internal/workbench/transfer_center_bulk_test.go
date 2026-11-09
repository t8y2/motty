package workbench

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestTransferCenterFailedFilterExcludesCanceledAndOtherStates(t *testing.T) {
	state := transferPreviewState(t)
	state.transfers.entries = append(state.transfers.entries, transferEntry{id: 5, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferCanceled}})
	state.Click(hitTransfersFilter.At(3))
	var visible []sftpclient.TransferState
	for index := range state.transfers.entries {
		if state.transfers.shows(&state.transfers.entries[index]) {
			visible = append(visible, state.transfers.entries[index].snapshot.State)
		}
	}
	if len(visible) != 1 || visible[0] != sftpclient.TransferFailed {
		t.Fatalf("Failed filter displayed %v; want only failed, not canceled or completed", visible)
	}
	if state.transfers.badge != "2" || state.transfers.active != 2 {
		t.Fatal("filter changed global summary/badge")
	}
}

// Keep the Worker and filesystem real; only the remote transport is in memory.
func bulkTestView(t testing.TB, state *State) (*sftpView, *workbenchSFTPRemote) {
	t.Helper()
	remote := &workbenchSFTPRemote{cwd: "/", entries: map[string][]os.FileInfo{"/": {}}, files: map[string][]byte{}}
	view := &sftpView{transferServer: "same host", transferAddress: "user@same:22"}
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return remote, nil }, nil)
	t.Cleanup(view.worker.Close)
	return view, remote
}

func bulkSource(remote *workbenchSFTPRemote, source, content string) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	remote.files[source] = []byte(content)
	remote.putEntryLocked(path.Dir(source), workbenchFileInfo{name: path.Base(source), size: int64(len(content)), mode: 0o600})
}

func bulkAwait(t testing.TB, state *State, view *sftpView, id uint64, phase sftpclient.TransferState) sftpclient.TransferSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.refreshSFTPTransfers(view)
		for _, snapshot := range view.transfers {
			if snapshot.ID == id && snapshot.State == phase && (!transferActive(snapshot) && (snapshot.Retryable || phase == sftpclient.TransferCompleted)) {
				return snapshot
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %d did not reach phase %v: %+v", id, phase, view.transfers)
	return sftpclient.TransferSnapshot{}
}

func bulkConflict(t testing.TB, state *State, view *sftpView, remote *workbenchSFTPRemote, name, existing string) (uint64, string) {
	t.Helper()
	bulkSource(remote, "/"+name, "hello world")
	target := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(target, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	id, ok := view.worker.QueueDownload("/"+name, target, false, sftpclient.ExistingFail)
	if !ok {
		t.Fatal("queue rejected")
	}
	bulkAwait(t, state, view, id, sftpclient.TransferFailed)
	return id, target
}

func bulkAction(name string) gfx.ID { return tree.ID(transferCenterHits, name) }
func bulkHost(index int) gfx.ID {
	return tree.Sub(transferCenterHits, tree.Name("host-item")).At(index)
}

func TestTransferCenterHostFilterScopesClearWithIdenticalWorkerIDs(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	first, remote := bulkTestView(t, state)
	second, other := bulkTestView(t, state)
	id, _ := bulkConflict(t, state, first, remote, "a", "mine")
	otherID, _ := bulkConflict(t, state, second, other, "b", "other")
	if id != otherID {
		t.Fatal("fixture must exercise colliding worker-local task IDs")
	}
	state.Click(bulkHost(1))
	state.Click(hitTransfersFilter.At(3))
	var shown int
	for index := range state.transfers.entries {
		if state.transfers.shows(&state.transfers.entries[index]) {
			shown++
			if state.transfers.entries[index].owner != first {
				t.Fatal("host filter leaked the second connection with the same address")
			}
		}
	}
	if shown != 1 {
		t.Fatalf("host scope shows %d, want 1", shown)
	}
	state.Click(hitTransfersClear)
	state.refreshSFTPTransfers(second)
	if len(state.transfers.entries) != 1 || state.transfers.entries[0].owner != second {
		t.Fatal("filtered clear removed a hidden host")
	}
}

func TestTransferCenterBulkRetryDoesNotOverwriteConflictsOrRetryHiddenCanceledTasks(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	hidden, hiddenRemote := bulkTestView(t, state)
	_, conflict := bulkConflict(t, state, view, remote, "exists", "mine")
	target := filepath.Join(t.TempDir(), "repaired")
	id, _ := view.worker.QueueDownload("/missing", target, false, sftpclient.ExistingFail)
	bulkAwait(t, state, view, id, sftpclient.TransferFailed)
	bulkSource(remote, "/missing", "recovered")
	canceled, _ := view.worker.QueueDownload("/absent", filepath.Join(t.TempDir(), "canceled"), false, sftpclient.ExistingFail)
	bulkAwait(t, state, view, canceled, sftpclient.TransferFailed)
	view.worker.CancelTransfer(canceled)
	state.refreshSFTPTransfers(view)
	hiddenID, _ := bulkConflict(t, state, hidden, hiddenRemote, "hidden", "keep")
	state.Click(bulkHost(1))
	state.Click(hitTransfersFilter.At(3))
	state.Click(bulkAction("retry-failed"))
	bulkAwait(t, state, view, id, sftpclient.TransferCompleted)
	if contents, _ := os.ReadFile(target); string(contents) != "recovered" {
		t.Fatalf("retry did not copy recovered source: %q", contents)
	}
	if contents, _ := os.ReadFile(conflict); string(contents) != "mine" {
		t.Fatalf("Retry overwrote conflict: %q", contents)
	}
	bulkAwait(t, state, view, canceled, sftpclient.TransferCanceled)
	bulkAwait(t, state, hidden, hiddenID, sftpclient.TransferFailed)
}

func TestTransferCenterBulkConflictPoliciesAreFiniteAndRequireConfirmation(t *testing.T) {
	for _, policy := range []string{"replace", "resume", "skip"} {
		t.Run(policy, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			view, remote := bulkTestView(t, state)
			existing := "mine"
			if policy == "resume" {
				existing = "hello"
			}
			id, target := bulkConflict(t, state, view, remote, "first", existing)
			state.Click(hitTransfersFilter.At(3))
			state.Click(bulkAction(policy + "-visible"))
			if policy != "skip" {
				if contents, _ := os.ReadFile(target); string(contents) != existing {
					t.Fatal("destructive action ran before confirmation")
				}
				late, lateTarget := bulkConflict(t, state, view, remote, "late", "keep late")
				state.Click(bulkAction("bulk-confirm"))
				bulkAwait(t, state, view, late, sftpclient.TransferFailed)
				if contents, _ := os.ReadFile(lateTarget); string(contents) != "keep late" {
					t.Fatal("confirmation included a later failure")
				}
			}
			bulkAwait(t, state, view, id, sftpclient.TransferCompleted)
			want := "hello world"
			if policy == "skip" {
				want = existing
			}
			if contents, _ := os.ReadFile(target); string(contents) != want {
				t.Fatalf("%s result=%q want=%q", policy, contents, want)
			}
			// No recovery policy may become the default of a later transfer.
			later, _ := bulkConflict(t, state, view, remote, "new-request", "stay")
			bulkAwait(t, state, view, later, sftpclient.TransferFailed)
		})
	}
}

func TestTransferCenterBulkRevalidatesCapturedOwnershipAndAttempt(t *testing.T) {
	for _, change := range []string{"moved", "archived", "removed", "canceled", "another retry", "worker replaced", "now hidden"} {
		t.Run(change, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			view, remote := bulkTestView(t, state)
			id, target := bulkConflict(t, state, view, remote, "first", "mine")
			state.Click(bulkAction("replace-visible"))
			switch change {
			case "moved":
				entries := state.detachTransfers(view, nil)
				other := newTestState(t, options)
				other.adoptTransfers(entries)
			case "archived":
				state.archiveTransfers(view)
			case "removed":
				state.removeTransfer(0)
			case "canceled":
				view.worker.CancelTransfer(id)
			case "another retry":
				view.worker.Retry(id, sftpclient.ExistingFail)
				bulkAwait(t, state, view, id, sftpclient.TransferFailed)
			case "worker replaced":
				replacement, _ := bulkTestView(t, state)
				view.worker = replacement.worker
			case "now hidden":
				state.transfers.filter = 1
			}
			state.Click(bulkAction("bulk-confirm"))
			if change != "removed" && change != "worker replaced" {
				// Wait for any accidentally dispatched copy, without syncing a moved
				// owner back into the source window as the test itself would do.
				deadline := time.Now().Add(time.Second)
				for time.Now().Before(deadline) {
					snapshot := view.worker.Transfers(nil)
					if len(snapshot) > 0 && transferActive(snapshot[0]) {
						time.Sleep(time.Millisecond)
						continue
					}
					break
				}
			}
			if contents, _ := os.ReadFile(target); string(contents) != "mine" {
				t.Fatalf("confirmation recovered %s task: %q", change, contents)
			}
			if change == "moved" && len(state.transfers.entries) != 0 {
				t.Fatal("confirmation resurrected task ownership in the old window")
			}
			if change == "archived" && (len(state.transfers.entries) != 1 || state.transfers.entries[0].owner != nil) {
				t.Fatal("confirmation revived archived worker ownership")
			}
		})
	}
}

func TestTransferCenterHostScopeGroupsCatalogHostAcrossWorkers(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	var views []*sftpView
	for index, catalog := range []string{"configured-one", "configured-one", "configured-two", "configured-one"} {
		view, remote := bulkTestView(t, state)
		address := "user@same:22"
		if index == 3 {
			address = "user@changed:22"
		}
		session := &Session{Title: "same host (tab)", Address: address, host: Host{CatalogID: catalog, Label: "same host"}}
		// Capture through the same constructor as both native file surfaces.
		view.transferHost = transferHostFor(session)
		bulkConflict(t, state, view, remote, fmt.Sprintf("host-%d", index), "keep")
		views = append(views, view)
	}
	if len(state.transfers.hosts) != 3 {
		t.Fatalf("host choices=%d; same catalog+endpoint must group, changed endpoint/catalog must not", len(state.transfers.hosts))
	}
	state.Click(bulkHost(1))
	if state.transfers.count != 2 {
		t.Fatalf("same configured host shows %d, want both owners", state.transfers.count)
	}
	state.Click(bulkAction("replace-visible"))
	state.Click(bulkAction("bulk-confirm"))
	for index, view := range views {
		phase := sftpclient.TransferFailed
		if index < 2 {
			phase = sftpclient.TransferCompleted
		}
		bulkAwait(t, state, view, 1, phase)
	}
}

func TestTransferCenterScopedCancelDoesNotCancelHiddenHost(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	var views []*sftpView
	for range 2 {
		view, base := bulkTestView(t, state)
		view.worker.Close()
		bulkSource(base, "/source", "copy payload")
		remote := &centerGatedRemote{workbenchSFTPRemote: base, gate: make(chan struct{}), opened: make(chan struct{}, 1)}
		view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return remote, nil }, nil)
		t.Cleanup(view.worker.Close)
		view.worker.QueueDownload("/source", filepath.Join(t.TempDir(), "copy"), false, sftpclient.ExistingFail)
		select {
		case <-remote.opened:
		case <-time.After(time.Second):
			t.Fatal("copy did not start")
		}
		state.refreshSFTPTransfers(view)
		views = append(views, view)
	}
	state.Click(bulkHost(1))
	state.Click(hitTransfersFilter.At(1))
	state.Click(hitTransfersCancelAll)
	bulkAwait(t, state, views[0], 1, sftpclient.TransferCanceled)
	if got := views[1].worker.Transfers(nil)[0].State; got != sftpclient.TransferRunning {
		t.Fatalf("filtered cancel changed hidden host to %v", got)
	}
	if state.transfers.badge != "1" {
		t.Fatal("scoped cancel changed the global meaning of the badge")
	}
}

func TestTransferCenterRetryCaptureExcludesLaterOrdinaryFailures(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	first, _ := view.worker.QueueDownload("/first", filepath.Join(t.TempDir(), "first"), false, sftpclient.ExistingFail)
	bulkAwait(t, state, view, first, sftpclient.TransferFailed)
	batch := state.captureTransferBatch(sftpclient.ExistingFail)
	late, _ := view.worker.QueueDownload("/late", filepath.Join(t.TempDir(), "late"), false, sftpclient.ExistingFail)
	bulkAwait(t, state, view, late, sftpclient.TransferFailed)
	bulkSource(remote, "/first", "repaired")
	bulkSource(remote, "/late", "later repair")
	state.dispatchTransferBatch(batch)
	bulkAwait(t, state, view, first, sftpclient.TransferCompleted)
	bulkAwait(t, state, view, late, sftpclient.TransferFailed)
}

func TestTransferCenterNativeSurfacesCaptureStableHostMetadata(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	first := session.files.transferHost
	if first == nil || first.catalog != session.host.CatalogID || first.address != session.Address {
		t.Fatal("side panel did not capture host catalog/endpoint")
	}
	window := state.newSFTPWindow(session)
	if !sameTransferHost(first, window.view.transferHost) {
		t.Fatal("same host's independent window uses another host filter")
	}
	session.host.CatalogID, session.Address, session.Title = "edited catalog", "other@changed:23", "renamed tab"
	if sameTransferHost(first, transferHostFor(session)) {
		t.Fatal("captured host identity followed a mutable catalog/endpoint")
	}
	if !sameTransferHost(first, window.view.transferHost) {
		t.Fatal("editing source host changed captured identity")
	}
	firstAdhoc := transferHostFor(&Session{Address: "user@host:22", Title: "Ad hoc 1"})
	secondAdhoc := transferHostFor(&Session{Address: "user@host:22", Title: "Ad hoc 2"})
	if !sameTransferHost(firstAdhoc, secondAdhoc) {
		t.Fatal("ad hoc sessions with the same canonical endpoint are split")
	}
}

func TestTransferCenterBulkViewsKeepSteadyPaintAllocationBudget(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	bulkConflict(t, state, view, remote, "first", "mine")
	state.transfers.open = true
	state.Click(bulkHost(1))
	state.Click(hitTransfersFilter.At(3))
	for _, mode := range []string{"filtered", "confirmation"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "confirmation" {
				state.Click(bulkAction("replace-visible"))
			}
			var canvas gfx.Canvas
			size := gfx.Size{Width: 900, Height: 640}
			for range 3 {
				canvas.Reset()
				state.Paint(&canvas, size)
			}
			if n := testing.AllocsPerRun(30, func() { canvas.Reset(); state.Paint(&canvas, size) }); n != 0 {
				t.Fatalf("%s steady paint allocations=%v", mode, n)
			}
		})
	}
}
