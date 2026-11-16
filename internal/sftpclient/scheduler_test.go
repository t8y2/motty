package sftpclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const chunk = 32 * 1024

// gateRemote serves files whose reads each wait for a token from the test, so a
// test decides exactly when bytes move.
type gateRemote struct {
	gates  map[string]chan struct{}
	chunks int
	closed chan struct{}
	once   sync.Once
	// reads counts the reads that began, so a test can wait for a transfer to be
	// blocked in one.
	reads atomic.Int32
}

func newGateRemote(chunks int, names ...string) *gateRemote {
	remote := &gateRemote{gates: map[string]chan struct{}{}, chunks: chunks, closed: make(chan struct{})}
	for _, name := range names {
		remote.gates[name] = make(chan struct{}, 64)
	}
	return remote
}

func (remote *gateRemote) release(name string, tokens int) {
	for range tokens {
		remote.gates[name] <- struct{}{}
	}
}

type gateFile struct {
	remote *gateRemote
	name   string
	sent   int
}

func (file *gateFile) Read(destination []byte) (int, error) {
	if file.sent >= file.remote.chunks {
		return 0, io.EOF
	}
	file.remote.reads.Add(1)
	select {
	case <-file.remote.gates[file.name]:
	case <-file.remote.closed:
		return 0, os.ErrClosed
	}
	file.sent++
	count := min(chunk, len(destination))
	for index := range count {
		destination[index] = 'x'
	}
	return count, nil
}
func (file *gateFile) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (file *gateFile) Seek(int64, int) (int64, error) { return 0, nil }
func (file *gateFile) Close() error                   { return nil }
func (file *gateFile) Sync() error                    { return nil }
func (file *gateFile) Truncate(int64) error           { return nil }
func (file *gateFile) Stat() (os.FileInfo, error) {
	return fakeInfo{name: file.name, size: int64(file.remote.chunks * chunk), mode: 0o600}, nil
}
func (remote *gateRemote) Getwd() (string, error)                { return "/", nil }
func (remote *gateRemote) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (remote *gateRemote) Open(name string) (io.ReadCloser, error) {
	return &gateFile{remote: remote, name: filepath.Base(name)}, nil
}
func (remote *gateRemote) Close() error {
	remote.once.Do(func() { close(remote.closed) })
	return nil
}
func (remote *gateRemote) Lstat(name string) (os.FileInfo, error) {
	return fakeInfo{name: filepath.Base(name), size: int64(remote.chunks * chunk), mode: 0o600}, nil
}
func (remote *gateRemote) OpenTransferFile(name string, _ int) (TransferFile, error) {
	return &gateFile{remote: remote, name: filepath.Base(name)}, nil
}
func (remote *gateRemote) Mkdir(string) error               { return nil }
func (remote *gateRemote) Rename(string, string) error      { return nil }
func (remote *gateRemote) PosixRename(string, string) error { return nil }
func (remote *gateRemote) Remove(string) error              { return nil }
func (remote *gateRemote) Chmod(string, os.FileMode) error  { return nil }

func snapshotOf(worker *Worker, id uint64) TransferSnapshot {
	for _, snapshot := range worker.Transfers(nil) {
		if snapshot.ID == id {
			return snapshot
		}
	}
	return TransferSnapshot{}
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTransfersRunInParallelUpToTheLimit(t *testing.T) {
	remote := newGateRemote(2, "a", "b", "c")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.SetParallel(2)
	dir := t.TempDir()
	var ids []uint64
	for _, name := range []string{"a", "b", "c"} {
		id, queued := worker.QueueDownload("/"+name, filepath.Join(dir, name), false, ExistingReplace)
		if !queued {
			t.Fatal("not queued")
		}
		ids = append(ids, id)
	}
	waitFor(t, "two running", func() bool {
		return snapshotOf(worker, ids[0]).State == TransferRunning && snapshotOf(worker, ids[1]).State == TransferRunning
	})
	if state := snapshotOf(worker, ids[2]).State; state != TransferQueued {
		t.Fatalf("the third transfer is %d, want queued", state)
	}
	// Finishing one lets the third start.
	remote.release("a", 2)
	waitFor(t, "the third to start", func() bool { return snapshotOf(worker, ids[2]).State == TransferRunning })
	remote.release("b", 2)
	remote.release("c", 2)
	for range 3 {
		if result := waitResult(t, worker); result.Err != nil {
			t.Fatalf("result %+v", result)
		}
	}
}

func TestAPausedTransferHoldsItsPlaceAndGoesOnWhereItStopped(t *testing.T) {
	remote := newGateRemote(4, "a")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	target := filepath.Join(t.TempDir(), "a")
	id, _ := worker.QueueDownload("/a", target, false, ExistingReplace)
	remote.release("a", 1)
	waitFor(t, "the first chunk", func() bool { return snapshotOf(worker, id).BytesDone == chunk })

	if !worker.Pause(id) || worker.Pause(id) {
		t.Fatal("pausing once must work and pausing twice must not")
	}
	remote.release("a", 3) // the read in progress ends; nothing after it may move
	waitFor(t, "the transfer to hold", func() bool { return snapshotOf(worker, id).BytesDone == 2*chunk })
	time.Sleep(30 * time.Millisecond)
	if snapshot := snapshotOf(worker, id); snapshot.State != TransferPaused || snapshot.BytesDone != 2*chunk || snapshot.BytesPerSecond != 0 {
		t.Fatalf("paused snapshot = %+v", snapshot)
	}
	if !worker.Resume(id) || worker.Resume(id) {
		t.Fatal("resuming once must work and resuming twice must not")
	}
	result := waitResult(t, worker)
	if result.Err != nil || result.Bytes != 4*chunk {
		t.Fatalf("result = %+v", result)
	}
	if info, err := os.Stat(target); err != nil || info.Size() != 4*chunk {
		t.Fatalf("file = %v, %v", info, err)
	}
}

func TestAQueuedTransferThatIsPausedDoesNotStartUntilResumed(t *testing.T) {
	remote := newGateRemote(1, "a", "b")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.SetParallel(1)
	dir := t.TempDir()
	first, _ := worker.QueueDownload("/a", filepath.Join(dir, "a"), false, ExistingReplace)
	second, _ := worker.QueueDownload("/b", filepath.Join(dir, "b"), false, ExistingReplace)
	waitFor(t, "the first to run", func() bool { return snapshotOf(worker, first).State == TransferRunning })
	if !worker.Pause(second) {
		t.Fatal("a queued transfer could not be paused")
	}
	remote.release("a", 1)
	waitResult(t, worker)
	time.Sleep(30 * time.Millisecond)
	if state := snapshotOf(worker, second).State; state != TransferPaused {
		t.Fatalf("the paused transfer is %d", state)
	}
	worker.Resume(second)
	remote.release("b", 1)
	if result := waitResult(t, worker); result.Err != nil || result.TransferID != second {
		t.Fatalf("result = %+v", result)
	}
}

func TestCancellingOneTransferLeavesTheOthersRunning(t *testing.T) {
	remote := newGateRemote(2, "a", "b")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	dir := t.TempDir()
	a, _ := worker.QueueDownload("/a", filepath.Join(dir, "a"), false, ExistingReplace)
	b, _ := worker.QueueDownload("/b", filepath.Join(dir, "b"), false, ExistingReplace)
	waitFor(t, "both running", func() bool {
		return snapshotOf(worker, a).State == TransferRunning && snapshotOf(worker, b).State == TransferRunning
	})
	remote.release("a", 1)
	waitFor(t, "a to move", func() bool { return snapshotOf(worker, a).BytesDone == chunk })
	worker.CancelTransfer(a)
	remote.release("a", 1) // lets its blocked read return, where it sees the cancel
	if result := waitResult(t, worker); result.TransferID != a || !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("the cancelled transfer's result = %+v", result)
	}
	select {
	case <-remote.closed:
		t.Fatal("cancelling one transfer closed the subsystem the other uses")
	default:
	}
	remote.release("b", 2)
	if result := waitResult(t, worker); result.TransferID != b || result.Err != nil {
		t.Fatalf("the other transfer's result = %+v", result)
	}
}

func TestTheSpeedIsMeasuredOverAWindowAndSmoothed(t *testing.T) {
	remote := newGateRemote(4, "a")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	var offset atomic.Int64
	start := time.Unix(1_700_000_000, 0)
	worker.clock = func() time.Time { return start.Add(time.Duration(offset.Load())) }
	id, _ := worker.QueueDownload("/a", filepath.Join(t.TempDir(), "a"), false, ExistingReplace)
	waitFor(t, "the first read", func() bool { return remote.reads.Load() >= 1 })

	offset.Store(int64(time.Second)) // one chunk in one second
	remote.release("a", 1)
	waitFor(t, "the first measure", func() bool { return snapshotOf(worker, id).BytesPerSecond == chunk })

	offset.Store(int64(3 * time.Second)) // the next chunk took two seconds: 16 KiB/s
	remote.release("a", 1)
	waitFor(t, "the second measure", func() bool { return snapshotOf(worker, id).BytesDone == 2*chunk })
	// 0.6*32768 + 0.4*16384
	full := float64(chunk)
	want := int64(0.6*full + 0.4*full/2)
	if got := snapshotOf(worker, id).BytesPerSecond; got != want {
		t.Fatalf("speed = %d, want %d", got, want)
	}
	remote.release("a", 2)
	waitResult(t, worker)
	if got := snapshotOf(worker, id).BytesPerSecond; got != 0 {
		t.Fatalf("a finished transfer still has a speed of %d", got)
	}
}

func TestCanceledQueuedTaskCanRetryOrBeForgottenImmediately(t *testing.T) {
	remote := newGateRemote(1, "a", "b")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer func() { worker.Close(); <-worker.Done() }()
	worker.SetParallel(1)
	active, _ := worker.QueueDownload("/a", filepath.Join(t.TempDir(), "a"), false, ExistingFail)
	queued, _ := worker.QueueDownload("/b", filepath.Join(t.TempDir(), "b"), false, ExistingFail)
	if snapshotOf(worker, queued).State != TransferQueued {
		t.Fatal("second task did not queue")
	}
	if !worker.CancelTransfer(queued) || !snapshotOf(worker, queued).Retryable {
		t.Fatal("queued cancellation was not settled")
	}
	if !worker.Retry(queued, ExistingFail) {
		t.Fatal("queued canceled task could not retry")
	}
	if !worker.CancelTransfer(queued) || !worker.ForgetTransfer(queued) {
		t.Fatal("queued canceled task could not be removed")
	}
	if snapshotOf(worker, active).State != TransferRunning {
		t.Fatal("canceling queued work affected the active task")
	}
}

func TestFullQueueDoesNotForgetACancelUntilItsCopySettles(t *testing.T) {
	remote := newGateRemote(1, "a", "b")
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer func() { worker.Close(); <-worker.Done() }()
	dir := t.TempDir()
	first, _ := worker.QueueDownload("/a", filepath.Join(dir, "a"), false, ExistingFail)
	worker.QueueDownload("/b", filepath.Join(dir, "b"), false, ExistingFail)
	for index := 2; index < maxQueuedRequests; index++ {
		if _, ok := worker.QueueDownload("/a", filepath.Join(dir, fmt.Sprint(index)), false, ExistingFail); !ok {
			t.Fatal("queue filled early")
		}
	}
	waitFor(t, "both copies blocked in reads", func() bool { return remote.reads.Load() >= 2 })
	worker.CancelTransfer(first)
	if _, ok := worker.QueueDownload("/a", filepath.Join(dir, "extra"), false, ExistingFail); ok {
		t.Fatal("evicted a cancellation whose copy is still active")
	}
	if !worker.stopped(worker.snapshot(), first) {
		t.Fatal("eviction forgot the cancellation signal")
	}
	remote.release("a", 1)
	if result := waitResult(t, worker); result.TransferID != first || !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("result=%+v", result)
	}
	if _, ok := worker.QueueDownload("/a", filepath.Join(dir, "extra"), false, ExistingFail); !ok {
		t.Fatal("settled history did not release capacity")
	}
}
