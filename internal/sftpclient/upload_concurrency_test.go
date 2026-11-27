package sftpclient

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// concurrentOpenRemote counts how many of a directory's files are open at
// once. A small file's round trips are what a folder of them costs, so the
// count is the property that keeps the transfer from being paced by one file.
type concurrentOpenRemote struct {
	remote
	hold time.Duration

	mu   sync.Mutex
	open int
	max  int
}

func (remote *concurrentOpenRemote) OpenTransferFile(name string, flags int) (TransferFile, error) {
	file, err := remote.remote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	remote.mu.Lock()
	remote.open++
	if remote.open > remote.max {
		remote.max = remote.open
	}
	remote.mu.Unlock()
	time.Sleep(remote.hold)
	return &countedTransferFile{TransferFile: file, release: remote.release}, nil
}

func (remote *concurrentOpenRemote) release() {
	remote.mu.Lock()
	remote.open--
	remote.mu.Unlock()
}

func (remote *concurrentOpenRemote) maximum() int {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	return remote.max
}

type countedTransferFile struct {
	TransferFile
	once    sync.Once
	release func()
}

func (file *countedTransferFile) Close() error {
	file.once.Do(file.release)
	return file.TransferFile.Close()
}

func TestDirectoryUploadKeepsSeveralFilesInFlight(t *testing.T) {
	measure := &concurrentOpenRemote{hold: 5 * time.Millisecond}
	worker, root := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		measure.remote = client
		return measure
	})
	local := filepath.Join(t.TempDir(), "many")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 24
	payload := []byte("payload")
	for index := 0; index < files; index++ {
		if err := os.WriteFile(filepath.Join(local, fmt.Sprintf("file-%02d.txt", index)), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	id, queued := worker.QueueUpload(local, "many", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != files || result.Bytes != int64(files*len(payload)) {
		t.Fatalf("upload result = %+v", result)
	}
	if got := measure.maximum(); got < 2 {
		t.Fatalf("the directory upload kept %d file in flight; its per-file round trips were serialized", got)
	}
	for _, index := range []int{0, files / 2, files - 1} {
		name := filepath.Join(root, "many", fmt.Sprintf("file-%02d.txt", index))
		if got, err := os.ReadFile(name); err != nil || string(got) != string(payload) {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
}

type roundTripCountingRemote struct {
	remote
	mu       sync.Mutex
	lstats   int
	readdirs int
	opens    int
	syncs    int
	writes   int
}

type roundTripCountingFile struct {
	TransferFile
	owner *roundTripCountingRemote
}

func (file roundTripCountingFile) Write(p []byte) (int, error) {
	file.owner.mu.Lock()
	file.owner.writes++
	file.owner.mu.Unlock()
	return file.TransferFile.Write(p)
}

func (file roundTripCountingFile) Sync() error {
	file.owner.mu.Lock()
	file.owner.syncs++
	file.owner.mu.Unlock()
	return file.TransferFile.Sync()
}

func (r *roundTripCountingRemote) Lstat(p string) (os.FileInfo, error) {
	r.mu.Lock()
	r.lstats++
	r.mu.Unlock()
	return r.remote.Lstat(p)
}

func (r *roundTripCountingRemote) ReadDir(p string) ([]os.FileInfo, error) {
	r.mu.Lock()
	r.readdirs++
	r.mu.Unlock()
	return r.remote.ReadDir(p)
}

func (r *roundTripCountingRemote) OpenTransferFile(p string, flags int) (TransferFile, error) {
	r.mu.Lock()
	r.opens++
	r.mu.Unlock()
	file, err := r.remote.OpenTransferFile(p, flags)
	if err != nil {
		return nil, err
	}
	return roundTripCountingFile{TransferFile: file, owner: r}, nil
}

func TestDirectoryUploadAvoidsPerFileLstatAndPerFileFsync(t *testing.T) {
	counted := &roundTripCountingRemote{}
	worker, root := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		counted.remote = client
		return counted
	})
	local := filepath.Join(t.TempDir(), ".output")
	if err := os.MkdirAll(filepath.Join(local, "public", "_nuxt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, "server", "chunks"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_123_456, 0)
	for i := range 20 {
		p := filepath.Join(local, "public", "_nuxt", fmt.Sprintf("chunk-%02d.js", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("export const c%d = %d;\n", i, i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	emptyPath := filepath.Join(local, "server", "chunks", "empty.mjs")
	if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUpload(local, ".output", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != 21 {
		t.Fatalf("fresh upload result = %+v", result)
	}
	// A brand-new destination folder needs only the single root Lstat; none of
	// the subdirectories, files, or .part paths should pay Lstat round trips,
	// and batch uploads must not fsync each file or send a write for a 0-byte file.
	if counted.lstats != 1 {
		t.Fatalf("fresh directory upload sent %d Lstat calls, want 1 (root only)", counted.lstats)
	}
	if counted.readdirs != 0 {
		t.Fatalf("fresh directory upload sent %d ReadDir calls, want 0", counted.readdirs)
	}
	if counted.syncs != 0 {
		t.Fatalf("batch directory upload sent %d Sync calls, want 0", counted.syncs)
	}
	if counted.writes != 20 {
		t.Fatalf("batch directory upload sent %d Write calls for 20 non-empty + 1 empty file, want 20", counted.writes)
	}
	if info, err := os.Stat(filepath.Join(root, ".output", "public", "_nuxt", "chunk-00.js")); err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("uploaded mtime = %v (%v), want %v", info, err, stamp)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".output", "server", "chunks", "empty.mjs")); err != nil || len(data) != 0 {
		t.Fatalf("empty file = %q, %v", data, err)
	}

	// Merging with ExistingSkip over an existing tree uses ReadDir per folder
	// rather than per-file Lstat, and opens zero files when all are present.
	counted.lstats, counted.readdirs, counted.opens = 0, 0, 0
	id, queued = worker.QueueUpload(local, ".output", true, ExistingSkip)
	result = queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != 21 {
		t.Fatalf("skip upload result = %+v", result)
	}
	if counted.lstats != 1 {
		t.Fatalf("skip upload sent %d Lstat calls, want 1 (root only)", counted.lstats)
	}
	if counted.opens != 0 {
		t.Fatalf("skip upload opened %d files, want 0", counted.opens)
	}
}

type probeRecordingDest struct {
	buf               bytes.Buffer
	writeCalls        int
	readFromCalls     int
	readFromWindowArg int
}

func (d *probeRecordingDest) Write(p []byte) (int, error) {
	d.writeCalls++
	return d.buf.Write(p)
}

func (d *probeRecordingDest) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent && offset == 0 {
		return int64(d.buf.Len()), nil
	}
	return 0, nil
}

func (d *probeRecordingDest) ReadFromWithConcurrency(r io.Reader, concurrency int) (int64, error) {
	d.readFromCalls++
	d.readFromWindowArg = concurrency
	return io.Copy(&d.buf, r)
}

func TestUploadOrderedProbeBoundary(t *testing.T) {
	worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
	defer worker.Close()

	cases := []struct {
		name         string
		size         int
		wantWrites   int
		wantReadFrom int
	}{
		{name: "empty", size: 0, wantWrites: 0, wantReadFrom: 0},
		{name: "one_byte", size: 1, wantWrites: 1, wantReadFrom: 0},
		{name: "below_chunk", size: uploadChunkSize - 1, wantWrites: 1, wantReadFrom: 0},
		{name: "exact_chunk_32768", size: uploadChunkSize, wantWrites: 1, wantReadFrom: 0},
		{name: "probe_overflow_32769", size: uploadChunkSize + 1, wantWrites: 0, wantReadFrom: 1},
		{name: "multi_chunk_65536", size: 2 * uploadChunkSize, wantWrites: 0, wantReadFrom: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := make([]byte, tc.size)
			for i := range payload {
				payload[i] = byte((i*31 + 7) % 251)
			}
			dest := &probeRecordingDest{}
			sink := newFileSink(worker, 0, 0, 0)
			written, err := worker.uploadCopyTo(dest, bytes.NewReader(payload), 0, 0, 0, sink)
			if err != nil {
				t.Fatalf("uploadCopyTo(%d) err = %v", tc.size, err)
			}
			if written != int64(tc.size) {
				t.Fatalf("written = %d, want %d", written, tc.size)
			}
			if dest.writeCalls != tc.wantWrites || dest.readFromCalls != tc.wantReadFrom {
				t.Fatalf("writeCalls=%d readFromCalls=%d, want %d / %d", dest.writeCalls, dest.readFromCalls, tc.wantWrites, tc.wantReadFrom)
			}
			if !bytes.Equal(dest.buf.Bytes(), payload) {
				t.Fatalf("uploaded bytes differ (len=%d, want=%d)", dest.buf.Len(), len(payload))
			}
		})
	}
}

type handleCapTracker struct {
	cap     int
	mu      sync.Mutex
	open    int
	maxOpen int
	rejects int
}

type handleCappedRemote struct {
	remote
	tracker *handleCapTracker
}

type handleCappedFile struct {
	TransferFile
	tracker *handleCapTracker
	once    sync.Once
}

func (f *handleCappedFile) Close() error {
	f.once.Do(func() {
		f.tracker.mu.Lock()
		f.tracker.open--
		f.tracker.mu.Unlock()
	})
	return f.TransferFile.Close()
}

func (r *handleCappedRemote) OpenTransferFile(p string, flags int) (TransferFile, error) {
	tr := r.tracker
	tr.mu.Lock()
	if tr.open >= tr.cap {
		tr.rejects++
		tr.mu.Unlock()
		return nil, fmt.Errorf("too many open handles: %w", sftp.ErrSSHFxFailure)
	}
	tr.open++
	if tr.open > tr.maxOpen {
		tr.maxOpen = tr.open
	}
	tr.mu.Unlock()

	file, err := r.remote.OpenTransferFile(p, flags)
	if err != nil {
		tr.mu.Lock()
		tr.open--
		tr.mu.Unlock()
		return nil, err
	}
	time.Sleep(3 * time.Millisecond)
	return &handleCappedFile{TransferFile: file, tracker: tr}, nil
}

func TestSharedHandleBudgetBacksOffAndBoundsMixedTransfers(t *testing.T) {
	tracker := &handleCapTracker{cap: 4}
	worker, root := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		return &handleCappedRemote{remote: client, tracker: tracker}
	})
	worker.SetParallel(2)

	// Stage a remote file for a concurrent download alongside the directory upload.
	downloadPayload := bytes.Repeat([]byte("download-check-"), 4096)
	if err := os.WriteFile(filepath.Join(root, "remote.bin"), downloadPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	localDownload := filepath.Join(t.TempDir(), "remote.bin")

	localDir := filepath.Join(t.TempDir(), "folder")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 24
	for i := range files {
		if err := os.WriteFile(filepath.Join(localDir, fmt.Sprintf("f-%02d.txt", i)), []byte(fmt.Sprintf("item-%02d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	upID, upQueued := worker.QueueUpload(localDir, "folder", true, ExistingFail)
	downID, downQueued := worker.QueueDownload("remote.bin", localDownload, false, ExistingFail)
	if !upQueued || !downQueued {
		t.Fatalf("queued upload=%t download=%t", upQueued, downQueued)
	}

	results := map[uint64]Result{}
	for range 2 {
		r := waitResult(t, worker)
		results[r.TransferID] = r
	}
	if r := results[upID]; r.Err != nil || r.Files != files {
		t.Fatalf("upload failed after backoff: %+v", r)
	}
	if r := results[downID]; r.Err != nil {
		t.Fatalf("download failed alongside capped upload: %+v", r)
	}
	tracker.mu.Lock()
	rejects, maxOpen, open := tracker.rejects, tracker.maxOpen, tracker.open
	tracker.mu.Unlock()
	if rejects == 0 {
		t.Fatal("expected initial open rejections to exercise handle backoff")
	}
	if maxOpen > tracker.cap {
		t.Fatalf("maxOpen = %d, exceeded server cap %d", maxOpen, tracker.cap)
	}
	if open != 0 {
		t.Fatalf("leaked %d open handles on remote", open)
	}
	if got := worker.handles.currentLimit(); got > tracker.cap {
		t.Fatalf("worker handle budget = %d, expected backoff <= %d", got, tracker.cap)
	}
}

type cancelLeakRemote struct {
	remote
	started    atomic.Int32
	openCount  atomic.Int32
	gateOpened chan struct{}
	gateOnce   sync.Once
	reached    chan struct{}
	reachOnce  sync.Once
}

type cancelLeakFile struct {
	TransferFile
	owner *cancelLeakRemote
	once  sync.Once
}

func (f *cancelLeakFile) Write(p []byte) (int, error) {
	if f.owner.started.Add(1) >= 4 {
		f.owner.reachOnce.Do(func() { close(f.owner.reached) })
	}
	<-f.owner.gateOpened
	return f.TransferFile.Write(p)
}

func (f *cancelLeakFile) Close() error {
	f.once.Do(func() { f.owner.openCount.Add(-1) })
	return f.TransferFile.Close()
}

func (r *cancelLeakRemote) OpenTransferFile(p string, flags int) (TransferFile, error) {
	file, err := r.remote.OpenTransferFile(p, flags)
	if err != nil {
		return nil, err
	}
	r.openCount.Add(1)
	return &cancelLeakFile{TransferFile: file, owner: r}, nil
}

func TestDirectoryUploadCancelReleasesHandlesAndGoroutines(t *testing.T) {
	leakRemote := &cancelLeakRemote{
		gateOpened: make(chan struct{}),
		reached:    make(chan struct{}),
	}
	worker, _ := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		leakRemote.remote = client
		return leakRemote
	})
	t.Cleanup(func() {
		leakRemote.gateOnce.Do(func() { close(leakRemote.gateOpened) })
	})

	localDir := filepath.Join(t.TempDir(), "cancel-many")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 24 {
		if err := os.WriteFile(filepath.Join(localDir, fmt.Sprintf("f-%02d.txt", i)), []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	baseGoroutines := runtime.NumGoroutine()
	id, queued := worker.QueueUpload(localDir, "cancel-many", true, ExistingFail)
	if !queued {
		t.Fatal("upload not queued")
	}
	waitSignal(t, "directory upload writes to start", leakRemote.reached)
	if !worker.CancelTransfer(id) {
		t.Fatal("CancelTransfer returned false")
	}
	leakRemote.gateOnce.Do(func() { close(leakRemote.gateOpened) })

	result := resultWithin(t, worker, 10*time.Second)
	if !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("cancel result = %+v, want ErrCanceled", result)
	}
	if got := leakRemote.openCount.Load(); got != 0 {
		t.Fatalf("remote open handles after cancel = %d, want 0", got)
	}
	worker.handles.mu.Lock()
	inFlight := worker.handles.inFlight
	worker.handles.mu.Unlock()
	if inFlight != 0 {
		t.Fatalf("worker handleBudget.inFlight = %d, want 0", inFlight)
	}
	if got := len(worker.largeUploads); got != 0 {
		t.Fatalf("worker largeUploads slots in use = %d, want 0", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseGoroutines+1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := runtime.NumGoroutine(); got > baseGoroutines+2 {
		t.Fatalf("goroutines leaked after cancel: before=%d after=%d", baseGoroutines, got)
	}
}

type failingOpenRemote struct {
	transferRemote
	err   error
	opens atomic.Int32
}

func (r *failingOpenRemote) OpenTransferFile(string, int) (TransferFile, error) {
	r.opens.Add(1)
	return nil, r.err
}

func TestOpenRemoteTransferFileFiltersNonLimitErrorsAndCapsRetries(t *testing.T) {
	if !isHandleLimitError(syscall.EMFILE) || !isHandleLimitError(syscall.ENFILE) {
		t.Fatal("expected EMFILE/ENFILE to match isHandleLimitError")
	}
	if !isHandleLimitError(fmt.Errorf("too many open files: %w", sftp.ErrSSHFxFailure)) {
		t.Fatal("expected SSH_FX_FAILURE with handle-limit text to match isHandleLimitError")
	}
	for _, nonLimit := range []error{
		&sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)},
		fmt.Errorf("disk quota exceeded: %w", sftp.ErrSSHFxFailure),
		fmt.Errorf("open file failed: quota exceeded: %w", sftp.ErrSSHFxFailure),
		io.EOF,
		sftp.ErrSSHFxConnectionLost,
		os.ErrPermission,
		os.ErrNotExist,
	} {
		if isHandleLimitError(nonLimit) {
			t.Fatalf("isHandleLimitError(%v) = true, want false", nonLimit)
		}
	}

	// Non-limit errors (quota, connection drop, bare SSH_FX_FAILURE) must not
	// retry or shrink the shared handle budget even when another handle is open.
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "quota", err: fmt.Errorf("disk quota exceeded: %w", sftp.ErrSSHFxFailure)},
		{name: "eof", err: io.EOF},
		{name: "connection_lost", err: sftp.ErrSSHFxConnectionLost},
		{name: "bare_fx_failure", err: &sftp.StatusError{Code: uint32(sftp.ErrSSHFxFailure)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
			defer worker.Close()
			gen := worker.snapshot()
			// Hold one active slot so active > 1 on the test open.
			hold, _, ok := worker.handles.acquire(worker, nil, gen, 0)
			if !ok {
				t.Fatal("failed to acquire initial handle slot")
			}
			defer hold(false)

			initialLimit := worker.handles.currentLimit()
			backoffCalled := false
			stub := &failingOpenRemote{err: tc.err}
			_, _, err := worker.openRemoteTransferFile(stub, "a.bin", os.O_RDWR|os.O_CREATE, nil, gen, 0, func() {
				backoffCalled = true
			})
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if got := stub.opens.Load(); got != 1 {
				t.Fatalf("opens = %d, want 1 (must not retry non-limit error)", got)
			}
			if backoffCalled {
				t.Fatal("onBackoff must not be called for non-limit error")
			}
			if got := worker.handles.currentLimit(); got != initialLimit {
				t.Fatalf("handle budget limit changed from %d to %d on non-limit error", initialLimit, got)
			}
		})
	}

	// Persistent handle-limit errors must stop retrying after maxHandleOpenRetries
	// even when other handles remain open, surfacing the error cleanly instead of
	// looping indefinitely.
	t.Run("caps_retries_on_persistent_handle_limit", func(t *testing.T) {
		worker := NewWorker(func() (Remote, error) { return nil, nil }, nil)
		defer worker.Close()
		gen := worker.snapshot()
		held := make([]func(bool), 0, 16)
		for range 16 {
			rel, _, ok := worker.handles.acquire(worker, nil, gen, 0)
			if !ok {
				t.Fatal("failed to acquire initial handle slot")
			}
			held = append(held, rel)
		}
		defer func() {
			for _, rel := range held {
				rel(false)
			}
		}()

		handleErr := fmt.Errorf("too many open handles: %w", sftp.ErrSSHFxFailure)
		stub := &failingOpenRemote{err: handleErr}
		var backoffs atomic.Int32
		_, _, err := worker.openRemoteTransferFile(stub, "a.bin", os.O_RDWR|os.O_CREATE, nil, gen, 0, func() {
			backoffs.Add(1)
			// Release enough held handles to drop below the halved limit while
			// keeping at least one handle open so every retry still has active > 1.
			target := max(1, worker.handles.currentLimit()/2)
			for len(held) > target {
				held[len(held)-1](false)
				held = held[:len(held)-1]
			}
		})
		if !errors.Is(err, handleErr) {
			t.Fatalf("err = %v, want %v", err, handleErr)
		}
		if got := stub.opens.Load(); got != int32(maxHandleOpenRetries+1) {
			t.Fatalf("opens = %d, want %d (1 initial + %d retries)", got, maxHandleOpenRetries+1, maxHandleOpenRetries)
		}
		if got := backoffs.Load(); got != int32(maxHandleOpenRetries) {
			t.Fatalf("backoffs = %d, want %d", got, maxHandleOpenRetries)
		}
		if got := worker.handles.currentLimit(); got >= defaultInitialOpenHandles {
			t.Fatalf("handle budget limit = %d, expected backoff below %d", got, defaultInitialOpenHandles)
		}
	})
}
