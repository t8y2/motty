package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	return state
}

func TestTransferCenterUsesReadableUnitsAndKeepsOtherConnections(t *testing.T) {
	state := transferPreviewState(t)
	entry := state.transfers.entries[3]
	if !strings.Contains(entry.amount, "MB") || !strings.Contains(entry.amount, "GB") || entry.speed != "12.0 MB/秒" {
		t.Fatalf("size=%q speed=%q", entry.amount, entry.speed)
	}
	if !strings.Contains(entry.clock, "10:30:00") || !strings.Contains(entry.clock, "32s") {
		t.Fatalf("clock=%q", entry.clock)
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
	if !state.transfers.open || state.transfers.badge != "1" {
		t.Fatalf("queued download: open=%v badge=%q", state.transfers.open, state.transfers.badge)
	}
	state.Click(hitTransfersClose)
	if state.transfers.open || state.transfers.badge != "1" {
		t.Fatal("closing the popup changed the task count")
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
	if !state.transfers.open {
		t.Fatal("download did not open the transfer center")
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

func TestTransferCenterUploadAlsoOpensAndResetsTheFilter(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	local := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(local, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.transfers.filter, state.transfers.scroll = 2, 100
	state.queueSFTPUpload(session.files, local, false)
	if !state.transfers.open || state.transfers.filter != 0 || state.transfers.scroll != 0 {
		t.Fatal("upload did not reveal the new transfer")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
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
