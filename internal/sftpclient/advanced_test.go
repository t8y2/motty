package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

func queueResult(t *testing.T, id uint64, queued bool, worker *Worker) Result {
	t.Helper()
	if !queued || id == 0 {
		t.Fatal("transfer was not queued")
	}
	result := waitResult(t, worker)
	if result.TransferID != id {
		t.Fatalf("result transfer = %d, want %d", result.TransferID, id)
	}
	return result
}

func TestWorkerTransfersDirectoriesOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	localRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(localRoot, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(localRoot, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte("directory payload\n")
	if err := os.WriteFile(filepath.Join(localRoot, "nested", "data.txt"), want, 0o640); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUpload(localRoot, "uploaded", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != 1 || result.Bytes != int64(len(want)) {
		t.Fatalf("upload result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(root, "uploaded", "nested", "data.txt"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("uploaded file = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(root, "uploaded", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty directory = %+v, %v", info, err)
	}

	downloadRoot := filepath.Join(t.TempDir(), "copy")
	id, queued = worker.QueueDownload("uploaded", downloadRoot, true, ExistingFail)
	result = queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != 1 || result.Bytes != int64(len(want)) {
		t.Fatalf("download result = %+v", result)
	}
	got, err = os.ReadFile(filepath.Join(downloadRoot, "nested", "data.txt"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("downloaded file = %q, %v", got, err)
	}
	if info, err := os.Stat(filepath.Join(downloadRoot, "empty")); err != nil || !info.IsDir() {
		t.Fatalf("downloaded empty directory = %+v, %v", info, err)
	}
}

func TestWorkerRequiresOverwriteAndRetriesSafely(t *testing.T) {
	worker, root := newProtocolWorker(t)
	remotePath := filepath.Join(root, "replace.txt")
	if err := os.WriteFile(remotePath, []byte("new content"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "replace.txt")
	if err := os.WriteFile(target, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueDownload("replace.txt", target, false, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if !errors.Is(result.Err, ErrDestinationExists) {
		t.Fatalf("conflict result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "keep me" {
		t.Fatalf("destination changed before confirmation: %q, %v", got, err)
	}
	if !worker.Retry(id, ExistingResume) {
		t.Fatal("confirmed retry was not queued")
	}
	result = waitResult(t, worker)
	if result.Err != nil {
		t.Fatalf("confirmed result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "new content" {
		t.Fatalf("confirmed destination = %q, %v", got, err)
	}
}

func TestWorkerResumesOnlyMatchingPartialDownload(t *testing.T) {
	worker, root := newProtocolWorker(t)
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	if err := os.WriteFile(filepath.Join(root, "resume.bin"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "resume.bin")
	partial := localPartialPath(target)
	prefix := content[:13]
	if err := os.WriteFile(partial, prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueDownload("resume.bin", target, false, ExistingResume)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != int64(len(prefix)) || result.Bytes != int64(len(content)) {
		t.Fatalf("resume result = %+v", result)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("resumed content = %q, %v", got, err)
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed partial remains: %v", err)
	}

	badTarget := filepath.Join(t.TempDir(), "resume.bin")
	badPartial := localPartialPath(badTarget)
	if err := os.WriteFile(badTarget, []byte("stable final"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(badPartial, []byte("wrong prefix"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued = worker.QueueDownload("resume.bin", badTarget, false, ExistingResume)
	result = queueResult(t, id, queued, worker)
	if !errors.Is(result.Err, ErrResumeMismatch) {
		t.Fatalf("mismatch result = %+v", result)
	}
	if got, err := os.ReadFile(badTarget); err != nil || string(got) != "stable final" {
		t.Fatalf("final changed after mismatch: %q, %v", got, err)
	}
}

func TestWorkerUploadRequiresConfirmationAndResumesMatchingPartial(t *testing.T) {
	worker, root := newProtocolWorker(t)
	localPath := filepath.Join(t.TempDir(), "upload.bin")
	content := []byte("upload resume payload")
	if err := os.WriteFile(localPath, content, 0o640); err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(root, "upload.bin")
	if err := os.WriteFile(finalPath, []byte("stable remote"), 0o600); err != nil {
		t.Fatal(err)
	}
	partialPath := filepath.Join(root, remotePartialPath("upload.bin"))
	prefix := content[:7]
	if err := os.WriteFile(partialPath, prefix, 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueUpload(localPath, "upload.bin", false, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if !errors.Is(result.Err, ErrDestinationExists) {
		t.Fatalf("upload conflict = %+v", result)
	}
	if got, err := os.ReadFile(finalPath); err != nil || string(got) != "stable remote" {
		t.Fatalf("remote final changed before confirmation: %q, %v", got, err)
	}
	if !worker.Retry(id, ExistingResume) {
		t.Fatal("upload retry was not queued")
	}
	result = waitResult(t, worker)
	if result.Err != nil || result.Resumed != int64(len(prefix)) || result.Bytes != int64(len(content)) {
		t.Fatalf("upload resume result = %+v", result)
	}
	if got, err := os.ReadFile(finalPath); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("remote final = %q, %v", got, err)
	}
	if _, err := os.Stat(partialPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote partial remains: %v", err)
	}
}

func TestRecursiveTransferRejectsSymbolicLinksBeforeWriting(t *testing.T) {
	worker, root := newProtocolWorker(t)
	localRoot := filepath.Join(t.TempDir(), "tree")
	if err := os.Mkdir(localRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("outside", filepath.Join(localRoot, "link")); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("Windows account lacks symbolic-link privilege: %v", err)
		}
		t.Fatal(err)
	}
	id, queued := worker.QueueUpload(localRoot, "unsafe", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if !errors.Is(result.Err, ErrSymbolicLink) {
		t.Fatalf("symlink result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "unsafe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote root was written before preflight: %v", err)
	}

	if err := os.Symlink("missing", filepath.Join(root, "remote-link")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "remote-link")
	id, queued = worker.QueueDownload("remote-link", target, true, ExistingFail)
	result = queueResult(t, id, queued, worker)
	if !errors.Is(result.Err, ErrSymbolicLink) {
		t.Fatalf("remote symlink result = %+v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local target was written for symlink: %v", err)
	}
}

type traversalRemote struct {
	infos []os.FileInfo
}

func (remote *traversalRemote) Getwd() (string, error)                { return "/", nil }
func (remote *traversalRemote) ReadDir(string) ([]os.FileInfo, error) { return remote.infos, nil }
func (remote *traversalRemote) Open(string) (io.ReadCloser, error)    { return nil, os.ErrNotExist }
func (remote *traversalRemote) Close() error                          { return nil }
func (remote *traversalRemote) Lstat(name string) (os.FileInfo, error) {
	if name == "/root" {
		return fakeInfo{name: "root", mode: os.ModeDir | 0o755}, nil
	}
	return nil, os.ErrNotExist
}
func (remote *traversalRemote) OpenTransferFile(string, int) (TransferFile, error) {
	return nil, os.ErrNotExist
}
func (remote *traversalRemote) Mkdir(string) error               { return nil }
func (remote *traversalRemote) Rename(string, string) error      { return nil }
func (remote *traversalRemote) PosixRename(string, string) error { return nil }
func (remote *traversalRemote) Remove(string) error              { return nil }
func (remote *traversalRemote) Chmod(string, os.FileMode) error  { return nil }

func TestRemoteDirectoryRejectsTraversalNames(t *testing.T) {
	remote := &traversalRemote{infos: []os.FileInfo{fakeInfo{name: "../escape", size: 5, mode: 0o600}}}
	if _, _, err := scanRemote(remote, "/root", true); !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("scan error = %v", err)
	}
}

type renameFallbackRemote struct {
	*traversalRemote
	renameCalled bool
}

func (remote *renameFallbackRemote) PosixRename(string, string) error {
	return errors.New("posix rename unavailable")
}

func (remote *renameFallbackRemote) Rename(string, string) error {
	remote.renameCalled = true
	return nil
}

func TestRemoteReplacementNeverFallsBackAfterExistingDestination(t *testing.T) {
	remote := &renameFallbackRemote{traversalRemote: &traversalRemote{}}
	if err := commitRemote(remote, ".part", "final", true); err == nil || remote.renameCalled {
		t.Fatalf("existing replacement = %v, rename called = %t", err, remote.renameCalled)
	}
	if err := commitRemote(remote, ".part", "new", false); err != nil || !remote.renameCalled {
		t.Fatalf("new-file fallback = %v, rename called = %t", err, remote.renameCalled)
	}
}

type extensionRenameRemote struct {
	*traversalRemote
	advertised  bool
	posixCalls  int
	renameCalls int
}

func (remote *extensionRenameRemote) HasExtension(name string) (string, bool) {
	if name == posixRenameExtension && remote.advertised {
		return "1", true
	}
	return "", false
}

func (remote *extensionRenameRemote) PosixRename(string, string) error {
	remote.posixCalls++
	return nil
}

func (remote *extensionRenameRemote) Rename(string, string) error {
	remote.renameCalls++
	return nil
}

func TestRemoteCommitUsesPosixRenameOnlyWhenAdvertised(t *testing.T) {
	plain := &extensionRenameRemote{traversalRemote: &traversalRemote{}}
	if err := commitRemote(plain, ".part", "new", false); err != nil {
		t.Fatal(err)
	}
	if plain.posixCalls != 0 || plain.renameCalls != 1 {
		t.Fatalf("unadvertised commit: posix = %d, rename = %d", plain.posixCalls, plain.renameCalls)
	}

	advertised := &extensionRenameRemote{traversalRemote: &traversalRemote{}, advertised: true}
	if err := commitRemote(advertised, ".part", "final", true); err != nil {
		t.Fatal(err)
	}
	if advertised.posixCalls != 1 || advertised.renameCalls != 0 {
		t.Fatalf("advertised commit: posix = %d, rename = %d", advertised.posixCalls, advertised.renameCalls)
	}
}

type stalledRenameRemote struct {
	remote
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type noPosixRenameRemote struct {
	remote
	posixCalls  int
	renameCalls int
}

func (remote *noPosixRenameRemote) HasExtension(name string) (string, bool) {
	if name == posixRenameExtension {
		return "", false
	}
	return remote.remote.HasExtension(name)
}

func (remote *noPosixRenameRemote) PosixRename(string, string) error {
	remote.posixCalls++
	return errors.New("posix rename was sent without an announcement")
}

func (remote *noPosixRenameRemote) Rename(oldPath, newPath string) error {
	remote.renameCalls++
	return remote.remote.Rename(oldPath, newPath)
}

func TestUploadCompletesWhenServerDoesNotAdvertisePosixRename(t *testing.T) {
	noPosix := &noPosixRenameRemote{}
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		noPosix.remote = base
		return noPosix
	})
	content := []byte("upload without the renaming extension\n")
	source := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueUpload(source, "payload.txt", false, ExistingFail)
	if !queued {
		t.Fatal("upload was not queued")
	}
	result := resultWithin(t, worker, 10*time.Second)
	if result.TransferID != id || result.Err != nil {
		t.Fatalf("result = %+v", result)
	}
	if noPosix.posixCalls != 0 || noPosix.renameCalls != 1 {
		t.Fatalf("posix calls = %d, rename calls = %d", noPosix.posixCalls, noPosix.renameCalls)
	}
	if got, err := os.ReadFile(filepath.Join(root, "payload.txt")); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("uploaded content = %q, err %v", got, err)
	}
	requireExists(t, filepath.Join(root, remotePartialPath("payload.txt")), false)
}

func (remote *stalledRenameRemote) PosixRename(string, string) error {
	remote.once.Do(func() { close(remote.entered) })
	<-remote.release
	return os.ErrClosed
}

func (remote *stalledRenameRemote) Close() error {
	remote.once.Do(func() { close(remote.release) })
	return remote.remote.Close()
}

func TestStalledUploadRenameTimesOutAndKeepsPartial(t *testing.T) {
	stalled := &stalledRenameRemote{entered: make(chan struct{}), release: make(chan struct{})}
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		stalled.remote = base
		return stalled
	})
	worker.finalizeTimeout = 50 * time.Millisecond
	content := []byte("payload that must survive a stalled rename\n")
	source := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUpload(source, "payload.txt", false, ExistingFail)
	if !queued {
		t.Fatal("upload was not queued")
	}
	result := resultWithin(t, worker, 10*time.Second)
	if result.TransferID != id || !errors.Is(result.Err, ErrUploadFinalizeTimeout) {
		t.Fatalf("result = %+v, want finalization timeout", result)
	}
	if errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("timeout was reported as cancellation: %v", result.Err)
	}
	select {
	case <-stalled.entered:
	default:
		t.Fatal("posix rename was not attempted")
	}

	requireExists(t, filepath.Join(root, "payload.txt"), false)
	partial := filepath.Join(root, remotePartialPath("payload.txt"))
	requireExists(t, partial, true)
	if kept, err := os.ReadFile(partial); err != nil || !bytes.Equal(kept, content) {
		t.Fatalf("partial = %q, err %v", kept, err)
	}
}

type progressFile struct {
	data   []byte
	offset int64
	opened chan struct{}
	closed <-chan struct{}
	once   sync.Once
}

func (file *progressFile) Read(destination []byte) (int, error) {
	if file.offset < int64(len(file.data)) {
		count := copy(destination, file.data[file.offset:])
		file.offset += int64(count)
		return count, nil
	}
	file.once.Do(func() { close(file.opened) })
	<-file.closed
	return 0, os.ErrClosed
}
func (file *progressFile) Write([]byte) (int, error) { return 0, os.ErrPermission }
func (file *progressFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		file.offset = offset
	case io.SeekCurrent:
		file.offset += offset
	case io.SeekEnd:
		file.offset = int64(len(file.data)) + offset
	}
	return file.offset, nil
}
func (file *progressFile) Close() error { return nil }
func (file *progressFile) Stat() (os.FileInfo, error) {
	return fakeInfo{name: "large.bin", size: int64(len(file.data) + 64*1024), mode: 0o600}, nil
}
func (file *progressFile) Sync() error          { return nil }
func (file *progressFile) Truncate(int64) error { return os.ErrPermission }

type progressRemote struct {
	closed chan struct{}
	once   sync.Once
	file   *progressFile
}

func newProgressRemote() *progressRemote {
	closed := make(chan struct{})
	return &progressRemote{
		closed: closed,
		file:   &progressFile{data: bytes.Repeat([]byte("x"), 32*1024), opened: make(chan struct{}), closed: closed},
	}
}
func (remote *progressRemote) Getwd() (string, error)                { return "/", nil }
func (remote *progressRemote) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (remote *progressRemote) Open(string) (io.ReadCloser, error)    { return remote.file, nil }
func (remote *progressRemote) Close() error {
	remote.once.Do(func() { close(remote.closed) })
	return nil
}
func (remote *progressRemote) Lstat(string) (os.FileInfo, error) { return remote.file.Stat() }
func (remote *progressRemote) OpenTransferFile(string, int) (TransferFile, error) {
	return remote.file, nil
}
func (remote *progressRemote) Mkdir(string) error               { return nil }
func (remote *progressRemote) Rename(string, string) error      { return nil }
func (remote *progressRemote) PosixRename(string, string) error { return nil }
func (remote *progressRemote) Remove(string) error              { return nil }
func (remote *progressRemote) Chmod(string, os.FileMode) error  { return nil }

func TestTransferProgressCancelAndDisconnectCleanup(t *testing.T) {
	remote := newProgressRemote()
	notify := make(chan struct{}, 16)
	worker := NewWorker(func() (Remote, error) { return remote, nil }, func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	})
	defer worker.Close()
	target := filepath.Join(t.TempDir(), "large.bin")
	id, queued := worker.QueueDownload("large.bin", target, false, ExistingResume)
	if !queued {
		t.Fatal("download was not queued")
	}
	select {
	case <-remote.file.opened:
	case <-time.After(time.Second):
		t.Fatal("transfer did not reach blocked read")
	}
	var snapshots []TransferSnapshot
	for range 16 {
		snapshots = worker.Transfers(snapshots)
		if len(snapshots) == 1 && snapshots[0].BytesDone == 32*1024 && snapshots[0].State == TransferRunning {
			break
		}
		select {
		case <-notify:
		case <-time.After(time.Second):
			t.Fatal("progress notification did not arrive")
		}
	}
	if len(snapshots) != 1 || snapshots[0].BytesDone != 32*1024 {
		t.Fatalf("progress snapshot = %+v", snapshots)
	}
	if !worker.CancelTransfer(id) {
		t.Fatal("active transfer was not canceled")
	}
	if worker.Retry(id, ExistingResume) {
		t.Fatal("transfer retried before cancellation cleanup completed")
	}
	result := waitResult(t, worker)
	if !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("cancel result = %+v", result)
	}
	select {
	case <-remote.closed:
	default:
		t.Fatal("disconnect did not close remote subsystem")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial transfer replaced final target: %v", err)
	}
	partial := localPartialPath(target)
	if info, err := os.Stat(partial); err != nil || info.Size() != 32*1024 {
		t.Fatalf("resumable partial = %+v, %v", info, err)
	}
	if !worker.Retry(id, ExistingResume) {
		t.Fatal("settled canceled transfer was not retryable")
	}
}

func TestTransferQueueIsBounded(t *testing.T) {
	remote := newProgressRemote()
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.SetParallel(1) // the fake has one shared file
	for index := range maxQueuedRequests {
		if id, queued := worker.QueueDownload("large.bin", filepath.Join(t.TempDir(), "file"), false, ExistingResume); !queued || id == 0 {
			t.Fatalf("queue item %d rejected", index)
		}
	}
	if id, queued := worker.QueueDownload("overflow", filepath.Join(t.TempDir(), "overflow"), false, ExistingResume); queued || id != 0 {
		t.Fatalf("overflow transfer = %d, %t", id, queued)
	}
}

func BenchmarkTransferSnapshots(b *testing.B) {
	worker := &Worker{transfers: make([]transferRecord, maxQueuedRequests)}
	for index := range worker.transfers {
		worker.transfers[index].snapshot = TransferSnapshot{
			ID: uint64(index + 1), Operation: OperationDownload, State: TransferQueued,
			Source: "remote/file", Destination: "local/file", BytesTotal: 1 << 20,
		}
	}
	destination := make([]TransferSnapshot, 0, maxQueuedRequests)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		destination = worker.Transfers(destination)
		if len(destination) != maxQueuedRequests {
			b.Fatal(len(destination))
		}
	}
}

func TestLargeFilesTransferThroughThePipelinedPath(t *testing.T) {
	worker, root := newProtocolWorker(t)
	payload := bytes.Repeat([]byte("0123456789abcdef"), 32*1024) // 512 KiB
	local := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueUpload(local, "large.bin", false, ExistingFail)
	if result := queueResult(t, id, queued, worker); result.Err != nil || result.Bytes != int64(len(payload)) {
		t.Fatalf("upload result = %+v", result)
	}
	if got, err := os.ReadFile(filepath.Join(root, "large.bin")); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("uploaded %d bytes, err %v", len(got), err)
	}

	copyPath := filepath.Join(t.TempDir(), "copy.bin")
	id, queued = worker.QueueDownload("large.bin", copyPath, false, ExistingFail)
	if result := queueResult(t, id, queued, worker); result.Err != nil || result.Bytes != int64(len(payload)) {
		t.Fatalf("download result = %+v", result)
	}
	if got, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded %d bytes, err %v", len(got), err)
	}
}
