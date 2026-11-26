package workbench

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func transferPreviewState(t testing.TB) *State {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	populateTransferPreview(state)
	return state
}

func populateTransferPreview(state *State) {
	state.setLanguage(i18n.Chinese)
	when := time.Date(2026, 10, 27, 10, 30, 0, 0, time.Local)
	for index, phase := range []sftpclient.TransferState{sftpclient.TransferCompleted, sftpclient.TransferFailed, sftpclient.TransferPaused, sftpclient.TransferRunning} {
		entry := transferEntry{id: index + 1, server: "docker-ubuntu", address: "motty@127.0.0.1:2222", snapshot: sftpclient.TransferSnapshot{
			ID: 1, Operation: sftpclient.OperationDownload, State: phase, Source: fmt.Sprintf("/home/motty/测试文件-%d.zip", index), Destination: `E:\Downloads\备份.zip`,
			BytesDone: 384 << 20, BytesTotal: 1 << 30, BytesPerSecond: 12 << 20, FilesDone: 1, FilesTotal: 1,
			QueuedAt: when, StartedAt: when, FinishedAt: when.Add(32 * time.Second),
		}}
		if phase == sftpclient.TransferFailed {
			entry.snapshot.Error = "连接中断，请重试"
		}
		if phase == sftpclient.TransferCompleted {
			entry.snapshot.BytesDone = entry.snapshot.BytesTotal
		}
		if phase == sftpclient.TransferRunning {
			entry.snapshot.Operation = sftpclient.OperationUpload
			entry.server = "docker-uos"
			entry.snapshot.Source = `E:\Downloads\发布包.tar.gz`
		}
		if transferActive(entry.snapshot) {
			entry.owner = &sftpView{}
		}
		state.describeTransfer(&entry)
		state.transfers.entries = append(state.transfers.entries, entry)
	}
	state.transfers.nextID = 4
	state.transfers.open = true
	state.describeTransferSummary()
}

func TestTransferCenterUsesReadableUnitsAndKeepsOtherConnections(t *testing.T) {
	state := transferPreviewState(t)
	entry := state.transfers.entries[3]
	if !strings.Contains(entry.amount, "MB") || !strings.Contains(entry.amount, "GB") || entry.speed != "12.0 MB/秒" {
		t.Fatalf("size=%q speed=%q", entry.amount, entry.speed)
	}
	if entry.elapsed != "32s" {
		t.Fatalf("elapsed=%q", entry.elapsed)
	}
	if state.transfers.badge != "2" {
		t.Fatalf("badge=%q, want 2", state.transfers.badge)
	}
	// Worker IDs overlap, but global controls always name the intended record.
	state.Click(hitTransfersRemove.At(2))
	if len(state.transfers.entries) != 3 || state.transfers.entries[1].server != "docker-ubuntu" || state.transfers.entries[2].server != "docker-uos" {
		t.Fatal("removed another connection's record")
	}
	state.Click(hitTransfersClear)
	if len(state.transfers.entries) != 2 || state.transfers.active != 2 {
		t.Fatal("clearing history removed an active task")
	}
	for _, entry := range state.transfers.entries {
		if !transferActive(entry.snapshot) {
			t.Fatal("finished history remains")
		}
	}
	for _, size := range []int64{1024, 1 << 20, 1 << 30, 1 << 40} {
		if strings.HasSuffix(formatBytes(size), " B") {
			t.Fatalf("unscaled size %d", size)
		}
	}
}

func TestTransferCenterOnlyFillsTheBarAfterCommitCompletes(t *testing.T) {
	state := transferPreviewState(t)
	entry := &state.transfers.entries[3]
	entry.snapshot.BytesDone = entry.snapshot.BytesTotal
	state.describeTransfer(entry)
	if entry.status != state.messages.Transfers.Finalizing || transferFraction(entry.snapshot) >= 1 {
		t.Fatal("pending finalization appears completed")
	}
	entry.snapshot.State = sftpclient.TransferCompleted
	state.describeTransfer(entry)
	if entry.status != state.messages.Transfers.Completed || transferFraction(entry.snapshot) != 1 {
		t.Fatal("completed transfer does not fill the bar")
	}
}

func TestTransferCenterOrdersGroupsByTheirLifecycleTime(t *testing.T) {
	state := transferPreviewState(t)
	at := time.Unix(1_790_000_000, 0)
	tick := func(seconds int) time.Time { return at.Add(time.Duration(seconds) * time.Second) }
	center := &state.transfers
	center.entries = []transferEntry{
		{id: 1, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferQueued, QueuedAt: tick(10), QueueOrder: 40}},
		{id: 2, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferPaused, QueuedAt: tick(10), QueueOrder: 30, StartedAt: tick(19)}},
		{id: 3, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferQueued, QueuedAt: tick(11), QueueOrder: 42}},
		{id: 4, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferRunning, QueuedAt: tick(1), StartedAt: tick(20)}},
		{id: 5, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferRunning, QueuedAt: tick(2), StartedAt: tick(19)}},
		{id: 6, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferCompleted, StartedAt: tick(1), FinishedAt: tick(17)}},
		{id: 7, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferCompleted, StartedAt: tick(5), FinishedAt: tick(15)}},
		{id: 8, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferFailed, StartedAt: tick(1), FinishedAt: tick(16)}},
		{id: 9, snapshot: sftpclient.TransferSnapshot{State: sftpclient.TransferFailed, StartedAt: tick(5), FinishedAt: tick(14)}},
	}
	state.describeTransferSummary()
	ids := func() []int {
		var visible []int
		for _, row := range center.rows {
			if row.entry >= 0 {
				visible = append(visible, center.entries[row.entry].id)
			}
		}
		return visible
	}
	if got, want := ids(), []int{4, 5, 2, 1, 3, 6, 7, 8, 9}; !slices.Equal(got, want) {
		t.Fatalf("ordered transfers %v, want %v", got, want)
	}
	state.Click(hitTransfersFilter.At(transferFilterWaiting))
	center.scroll = 38
	center.entries = append(center.entries, transferEntry{id: 10, snapshot: sftpclient.TransferSnapshot{
		State: sftpclient.TransferQueued, QueuedAt: tick(12), QueueOrder: 43,
	}})
	state.openTransferCenterForQueued()
	state.describeTransferSummary()
	if got, want := ids(), []int{2, 1, 3, 10}; !slices.Equal(got, want) || center.scroll != 38 {
		t.Fatalf("new upload moved waiting jobs or scroll: jobs=%v scroll=%v want %v / 38", got, center.scroll, want)
	}
}

func TestTransferCenterRevealsOnlyCompletedDownloads(t *testing.T) {
	state := transferPreviewState(t)
	completed := &state.transfers.entries[0]
	if completed.owner != nil {
		t.Fatal("test download should represent closed-connection history")
	}
	opened := make(chan string, 3)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	state.options.RevealPath = func(path string) error {
		opened <- path
		<-release
		return nil
	}
	reveal := hitTransfersReveal.At(completed.id)
	state.Click(reveal)
	state.Key(gfx.KeyEvent{Target: reveal, Key: gfx.KeyEnter})
	state.Key(gfx.KeyEvent{Target: reveal, Key: gfx.KeySpace})
	for range 20 {
		state.Click(reveal)
	}
	waitFor(t, state, func() bool { return len(opened) == 1 })
	if got := <-opened; got != completed.snapshot.Destination {
		t.Fatalf("opened=%q want=%q", got, completed.snapshot.Destination)
	}
	if !state.revealBusy {
		t.Fatal("repeated reveal did not keep a single pending request")
	}
	close(release)
	waitFor(t, state, func() bool { return !state.revealBusy })
	state.Click(reveal)
	waitFor(t, state, func() bool { return len(opened) == 1 })
	if got := <-opened; got != completed.snapshot.Destination {
		t.Fatalf("opened=%q want=%q after first reveal completed", got, completed.snapshot.Destination)
	}
	waitFor(t, state, func() bool { return !state.revealBusy })
	for _, index := range []int{1, 2, 3} {
		state.Click(hitTransfersReveal.At(state.transfers.entries[index].id))
	}
	completedUpload := transferEntry{
		id: 5,
		snapshot: sftpclient.TransferSnapshot{
			Operation: sftpclient.OperationUpload, State: sftpclient.TransferCompleted, Destination: "/srv/upload.txt",
		},
	}
	state.transfers.entries = append(state.transfers.entries, completedUpload)
	state.Click(hitTransfersReveal.At(completedUpload.id))
	time.Sleep(10 * time.Millisecond)
	if len(opened) != 0 {
		t.Fatal("non-download records opened a local path")
	}
}

func TestTransferCenterReportsRevealFailure(t *testing.T) {
	state := transferPreviewState(t)
	state.options.RevealPath = func(string) error { return errors.New("file manager unavailable") }
	state.Click(hitTransfersReveal.At(state.transfers.entries[0].id))
	waitFor(t, state, func() bool { return state.status == state.messages.Transfers.ShowInFolderFailed })
	if len(state.fileNotices.items) != 1 {
		t.Fatalf("status=%q notices=%d", state.status, len(state.fileNotices.items))
	}
	if got := state.fileNotices.items[0]; got.title != state.messages.Transfers.ShowInFolder || got.detail != state.messages.Transfers.ShowInFolderFailed {
		t.Fatalf("unexpected notice: %+v", got)
	}
}

func TestTransferCenterExplainsWhenARevealTargetIsGone(t *testing.T) {
	state := transferPreviewState(t)
	state.options.RevealPath = func(path string) error {
		return fmt.Errorf("%w: %q", os.ErrNotExist, path)
	}
	state.Click(hitTransfersReveal.At(state.transfers.entries[0].id))
	waitFor(t, state, func() bool { return state.status == state.messages.Transfers.ShowInFolderMissing })
	if len(state.fileNotices.items) != 1 {
		t.Fatalf("status=%q notices=%d", state.status, len(state.fileNotices.items))
	}
	if got := state.fileNotices.items[0]; got.detail != state.messages.Transfers.ShowInFolderMissing {
		t.Fatalf("unexpected notice: %+v", got)
	}
}

type centerGatedRemote struct {
	*workbenchSFTPRemote
	gate   chan struct{}
	opened chan struct{}
	once   sync.Once
}

type centerGatedFile struct {
	sftpclient.TransferFile
	remote *centerGatedRemote
}

func (remote *centerGatedRemote) OpenTransferFile(name string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.workbenchSFTPRemote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	return &centerGatedFile{file, remote}, nil
}

func (file *centerGatedFile) Read(dst []byte) (int, error) {
	select {
	case file.remote.opened <- struct{}{}:
	default:
	}
	<-file.remote.gate
	return file.TransferFile.Read(dst)
}

func (remote *centerGatedRemote) Close() error {
	remote.once.Do(func() { close(remote.gate) })
	return remote.workbenchSFTPRemote.Close()
}

func TestTransferCenterQueuedCardExpandsAndClosesWithoutCanceling(t *testing.T) {
	state := transferPreviewState(t)
	state.transfers.close()
	state.openTransferCenterForQueued()
	if !state.transfers.open || !state.transfers.compact {
		t.Fatal("queued transfer did not open the compact progress card")
	}
	state.Click(hitTransfersExpand)
	if !state.transfers.open || state.transfers.compact {
		t.Fatal("expanding the card did not open the full transfer center")
	}
	state.Click(hitTransfersClose)
	if state.transfers.open || state.transfers.compact || state.transfers.active != 2 {
		t.Fatal("closing the card changed its presentation reset or active tasks")
	}
	state.Click(hitTransferCenter)
	if !state.transfers.open || state.transfers.compact {
		t.Fatal("toolbar did not reopen the full transfer center")
	}
}

func TestTransferCenterHidingAndNavigatingDoesNotCancelACopy(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	gated := &centerGatedRemote{workbenchSFTPRemote: remote, gate: make(chan struct{}), opened: make(chan struct{}, 1)}
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
	defer view.worker.Close()
	target := filepath.Join(t.TempDir(), "notes.txt")
	state.queueSFTPDownload(view, view.rows[1].entry, target)
	select {
	case <-gated.opened:
	case <-time.After(time.Second):
		t.Fatal("copy did not start")
	}
	state.refreshSFTPTransfers(view)
	id := view.transfers[0].ID
	if !state.transfers.open || !state.transfers.compact || state.transfers.badge != "1" {
		t.Fatalf("queued download: open=%v compact=%v badge=%q", state.transfers.open, state.transfers.compact, state.transfers.badge)
	}
	state.Click(hitTransfersClose)
	if state.transfers.open || state.transfers.compact || state.transfers.badge != "1" {
		t.Fatal("closing the card changed the task count or presentation reset")
	}
	state.Click(hitSFTPClose)
	state.Click(hitHome)
	state.openSettings()
	if session.files != view {
		t.Fatal("navigation detached the running worker")
	}
	if got := view.worker.Transfers(nil); len(got) != 1 || got[0].State != sftpclient.TransferRunning {
		t.Fatalf("copy was canceled: %+v", got)
	}
	gated.once.Do(func() { close(gated.gate) })
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if contents, err := os.ReadFile(target); err != nil || string(contents) != "hello world" {
		t.Fatalf("copy=%q err=%v", contents, err)
	}
	if state.transfers.badge != "" {
		t.Fatalf("completed copy still has badge %q", state.transfers.badge)
	}
	entry := state.transfers.entries[0]
	if entry.snapshot.ID != id || entry.server == "" || entry.snapshot.QueuedAt.IsZero() || entry.snapshot.FinishedAt.IsZero() {
		t.Fatalf("incomplete history: %+v", entry)
	}
	state.closeSFTP(session)
	if len(state.transfers.entries) != 1 || state.transfers.entries[0].owner != nil {
		t.Fatal("closing the session lost history or retained its view")
	}
	state.Click(hitTransfersRemove.At(entry.id))
	if len(state.transfers.entries) != 0 {
		t.Fatal("history did not clear")
	}
}

func TestTransferCenterWorkerIDsAreScopedAndRemovalDoesNotReappear(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.queueSFTPDownload(view, view.rows[1].entry, filepath.Join(t.TempDir(), "notes.txt"))
	if !state.transfers.open || !state.transfers.compact {
		t.Fatal("download did not open the compact transfer card")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	other := &sftpView{transferServer: "other", transferAddress: "other:22", transfers: append([]sftpclient.TransferSnapshot(nil), view.transfers...)}
	state.syncTransfers(other)
	if len(state.transfers.entries) != 2 || state.transfers.entries[0].id == state.transfers.entries[1].id {
		t.Fatal("worker IDs collided")
	}
	state.Click(hitTransfersRemove.At(state.transfers.entries[0].id))
	state.refreshSFTPTransfers(view)
	if len(state.transfers.entries) != 1 || state.transfers.entries[0].server != "other" {
		t.Fatal("a removed record reappeared or another connection was removed")
	}
	// Do not retain a test-only view that has no real worker during cleanup.
	state.archiveTransfers(other)
}

func TestTransferCenterPauseResumeAndCancelReachTheWorker(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	gated := &centerGatedRemote{workbenchSFTPRemote: remote, gate: make(chan struct{}), opened: make(chan struct{}, 1)}
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
	defer view.worker.Close()
	target := filepath.Join(t.TempDir(), "notes.txt")
	view.worker.QueueDownload("/home/test/notes.txt", target, false, sftpclient.ExistingFail)
	select {
	case <-gated.opened:
	case <-time.After(time.Second):
		t.Fatal("copy did not start")
	}
	state.refreshSFTPTransfers(view)
	entryID := state.transfers.entries[0].id
	state.Click(hitTransfersPause.At(entryID))
	if view.transfers[0].State != sftpclient.TransferPaused {
		t.Fatal("pause did not reach its worker")
	}
	state.Click(hitTransfersPause.At(entryID))
	if view.transfers[0].State != sftpclient.TransferRunning {
		t.Fatal("resume did not reach its worker")
	}
	state.Click(hitTransfersCancel.At(entryID))
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.transfers[0].State == sftpclient.TransferCanceled && view.transfers[0].Retryable
	})
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("canceled copy published a final file: %v", err)
	}
	state.Click(hitTransfersClear)
	state.refreshSFTPTransfers(view)
	if len(state.transfers.entries) != 0 {
		t.Fatal("canceled history reappeared after clearing")
	}
}

func TestTransferCenterUploadOpensAndPreservesTheFilter(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	local := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(local, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.transfers.filter, state.transfers.scroll = transferFilterWaiting, 100
	state.queueSFTPUpload(session.files, local, false)
	if !state.transfers.open || !state.transfers.compact || state.transfers.filter != transferFilterWaiting || state.transfers.scroll != 0 {
		t.Fatalf("upload did not preserve filter: open=%v compact=%v filter=%d scroll=%v", state.transfers.open, state.transfers.compact, state.transfers.filter, state.transfers.scroll)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
}

func TestQueuedTransferOpensCompactCardWithCollapsedWaitingGroup(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	state.transfers.filter = transferFilterWaiting
	state.transfers.groups[transferFilterWaiting].collapsed = true

	state.openTransferCenterForQueued()
	if !state.transfers.open || !state.transfers.compact || !state.transfers.groups[transferFilterWaiting].collapsed {
		t.Fatal("queued transfer did not show a compact card while preserving the collapsed group")
	}
}

func TestTransferCenterFilterSelectionPreservedAcrossReopenAndSaved(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)

	for _, chosenFilter := range []int{transferFilterRunning, transferFilterWaiting, transferFilterEnded, transferFilterFailed, transferFilterAll, transferFilterPending} {
		local := filepath.Join(t.TempDir(), fmt.Sprintf("upload-%d.txt", chosenFilter))
		if err := os.WriteFile(local, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		state.Click(hitTransfersFilter.At(chosenFilter))
		if state.transfers.filter != chosenFilter {
			t.Fatalf("clicking filter %d did not update transfers.filter: got %d", chosenFilter, state.transfers.filter)
		}
		if snap := state.snapshot(); snap.SFTPTransferFilter != chosenFilter {
			t.Fatalf("snapshot filter = %d, want %d", snap.SFTPTransferFilter, chosenFilter)
		}

		// Close transfer center
		state.Click(hitTransfersClose)
		if state.transfers.open {
			t.Fatal("close did not close transfer center")
		}

		// Reopen transfer center via toggle button
		state.Click(hitTransferCenter)
		if !state.transfers.open || state.transfers.filter != chosenFilter {
			t.Fatalf("reopening transfer center lost filter: open=%v, filter=%d, want %d", state.transfers.open, state.transfers.filter, chosenFilter)
		}
		state.Click(hitTransfersClose)

		// The compact card opens regardless of the full history's saved filter.
		state.queueSFTPUpload(session.files, local, false)
		if !state.transfers.open || !state.transfers.compact || state.transfers.filter != chosenFilter {
			t.Fatalf("upload: open=%v compact=%v, filter=%d want %d", state.transfers.open, state.transfers.compact, state.transfers.filter, chosenFilter)
		}
		if state.transfers.open && state.transfers.scroll != 0 {
			t.Fatalf("upload left the open center scrolled to %v", state.transfers.scroll)
		}

		// Simulate restoring state from saved preferences
		restoredState := &State{}
		restoredState.restoreSFTPSettings(savedState{SFTPTransferFilter: chosenFilter})
		if restoredState.transfers.filter != chosenFilter {
			t.Fatalf("restoreSFTPSettings did not restore filter: got %d, want %d", restoredState.transfers.filter, chosenFilter)
		}
	}
}

func TestTransferCenterFollowsAMovedTabWithoutCancelingOrDuplicatingTasks(t *testing.T) {
	source, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	gated := &centerGatedRemote{workbenchSFTPRemote: remote, gate: make(chan struct{}), opened: make(chan struct{}, 1)}
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, source.notifyFor(session))
	defer view.worker.Close()
	local := filepath.Join(t.TempDir(), "notes.txt")
	source.queueSFTPDownload(view, view.rows[1].entry, local)
	select {
	case <-gated.opened:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	_, options := newWorld(t)
	target := newTestState(t, options)
	old := source.transfers.entries[0]
	old.owner, old.snapshot.State = nil, sftpclient.TransferCompleted
	target.transfers.entries, target.transfers.nextID = []transferEntry{old}, old.id
	payload, ok := source.detachWorkspace(source.deck.ActiveWorkspace())
	if !ok || len(source.transfers.entries) != 0 || source.transfers.active != 0 {
		t.Fatal("old window retained transfer ownership")
	}
	if !target.attachPayload(payload, tabDrop{kind: dropReorder}) {
		t.Fatal("tab did not attach")
	}
	if len(target.transfers.entries) != 2 || target.transfers.entries[1].id == old.id {
		t.Fatal("moved task collided with target history")
	}
	source.Close()
	gated.once.Do(func() { close(gated.gate) })
	moved := &target.sessions[0]
	waitSFTP(t, target, moved, func(view *sftpView) bool { return view.transfers[0].State == sftpclient.TransferCompleted })
	if got, err := os.ReadFile(local); err != nil || string(got) != "hello world" {
		t.Fatalf("moved download=%q err=%v", got, err)
	}
	if len(target.transfers.entries) != 2 || target.transfers.active != 0 {
		t.Fatal("moved task duplicated or left an active badge")
	}
}

func TestTransferCenterSteadyPaintDoesNotAllocate(t *testing.T) {
	state := transferPreviewState(t)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	for range 3 {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	if n := testing.AllocsPerRun(50, func() { canvas.Reset(); state.Paint(&canvas, size) }); n != 0 {
		t.Fatalf("allocations per paint=%v", n)
	}
}

func BenchmarkPaintTransferCenter(b *testing.B) {
	state := transferPreviewState(b)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	for range 3 {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// The filter selects keep the 30px slot the old controls were given, and the
// list keeps its 10px scrollbar drawn whether or not the pointer is over it.
func TestTransferFiltersAndScrollbarKeepTheirOldGeometry(t *testing.T) {
	state := transferPreviewState(t)
	extendTransferPreview(state, sftpclient.TransferPaused, 20)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	m := state.messages.Transfers
	for _, label := range []string{m.Hosts, m.Direction} {
		hit, found := hitByLabelRoleLast(&canvas, label, gfx.RoleCombobox)
		if !found {
			t.Fatalf("missing filter select %q", label)
		}
		if hit.Bounds.Height != 30 {
			t.Fatalf("filter select %q is %v high, want the old 30px slot", label, hit.Bounds.Height)
		}
	}
	scrollbar, found := hitByLabelRoleLast(&canvas, "Vertical scrollbar", gfx.RoleSeparator)
	if !found || scrollbar.Bounds.Width != 10 {
		t.Fatalf("transfer scrollbar = %+v, want an always-drawn 10px bar", scrollbar.Bounds)
	}
}

// The hover preview keeps the old card's placement: its "Details" title, a 3px
// gap under the trigger, and the align offset that shifts it in from the row's
// leading edge.
func TestTransferPreviewKeepsTheOldCardPlacement(t *testing.T) {
	state := transferPreviewState(t)
	size := gfx.Size{Width: 1400, Height: 900}
	paint := func(now time.Duration, hovered gfx.ID) *gfx.Canvas {
		var canvas gfx.Canvas
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame()
		canvas.SetInteraction(hovered, gfx.ID{}, gfx.ID{}, false)
		state.Paint(&canvas, size)
		canvas.EndFrame()
		return &canvas
	}
	first := paint(time.Second, gfx.ID{})
	title := state.transfers.entries[0].title
	row, found := hitByLabelRoleLast(first, title, gfx.RoleButton)
	if !found {
		t.Fatalf("the first transfer row %q is not painted", title)
	}
	paint(time.Second, row.ID)
	canvas := paint(time.Second+transferDetailDelay+30*time.Millisecond, row.ID)
	if !state.transfers.details.open {
		t.Fatal("resting on the row did not open its details")
	}
	if !hasTextValue(canvas, state.messages.Transfers.Details) {
		t.Fatal("the preview does not carry the old Details title")
	}
	row, found = hitByLabelRoleLast(canvas, title, gfx.RoleButton)
	if !found {
		t.Fatalf("the transfer row %q is not painted in the preview's frame", title)
	}
	cardHit, found := hitByID(canvas, hitTransfersDetailCard)
	if !found {
		t.Fatal("the preview has no card")
	}
	align := max(4, min(row.Bounds.Width*0.55, 190))
	if wantLeft := row.Bounds.X + align; math.Abs(float64(cardHit.Bounds.X-wantLeft)) > 0.001 {
		t.Fatalf("preview starts at %v, want %v (the old align offset)", cardHit.Bounds.X, wantLeft)
	}
	if wantTop := row.Bounds.Y + row.Bounds.Height + 3; math.Abs(float64(cardHit.Bounds.Y-wantTop)) > 0.001 {
		t.Fatalf("preview top = %v, want %v (3px under the trigger)", cardHit.Bounds.Y, wantTop)
	}
}
