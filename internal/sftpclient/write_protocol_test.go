package sftpclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestTransfersSupportMaximumLengthNames(t *testing.T) {
	worker, root := newProtocolWorker(t)
	for _, name := range []string{strings.Repeat("a", 250) + ".bin", strings.Repeat("配", 83) + ".conf"} {
		localDir := t.TempDir()
		source := filepath.Join(localDir, name)
		data := []byte("中文\r\n\x00binary\xff\n")
		if err := os.WriteFile(source, data, 0600); err != nil {
			t.Fatal(err)
		}
		upload := requireQueuedResult(t, worker.Upload(source, name), worker, OperationUpload)
		if upload.Err != nil {
			t.Fatal("long-name upload failed", upload.Err)
		}
		actual, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatal("upload changed bytes", err)
		}
		downloadDir := t.TempDir()
		target := filepath.Join(downloadDir, name)
		download := requireQueuedResult(t, worker.Download(name, target), worker, OperationDownload)
		if download.Err != nil {
			t.Fatal("long-name download failed", download.Err)
		}
		actual, err = os.ReadFile(target)
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatal("download changed bytes", err)
		}
		for _, directory := range []string{root, downloadDir} {
			partials, err := filepath.Glob(filepath.Join(directory, ".motty.*.part"))
			if err != nil || len(partials) != 0 {
				t.Fatal("partial file leaked", partials, err)
			}
		}
	}
}

func newProtocolWorker(t *testing.T) (*Worker, string) {
	t.Helper()
	return newProtocolWorkerOn(t, nil, nil)
}

// newProtocolWorkerOn is newProtocolWorker with a say in the two ends: serverSide
// wraps the server's end of the pipe, and wrapRemote the remote the worker uses.
func newProtocolWorkerOn(t *testing.T, serverSide func(net.Conn) net.Conn, wrapRemote func(remote) Remote) (*Worker, string) {
	t.Helper()
	root := t.TempDir()
	clientConnection, serverConnection := net.Pipe()
	var serverEnd io.ReadWriteCloser = serverConnection
	if serverSide != nil {
		serverEnd = serverSide(serverConnection)
	}
	server, err := sftp.NewServer(serverEnd, sftp.WithServerWorkingDirectory(root))
	if err != nil {
		clientConnection.Close()
		serverConnection.Close()
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() {
		served <- server.Serve()
		server.Close()
	}()
	client, err := sftp.NewClientPipe(clientConnection, clientConnection)
	if err != nil {
		clientConnection.Close()
		serverConnection.Close()
		t.Fatal(err)
	}
	worker := NewWorker(func() (Remote, error) {
		if wrapRemote != nil {
			return wrapRemote(remote{Client: client}), nil
		}
		return remote{Client: client}, nil
	}, nil)
	t.Cleanup(func() {
		worker.Close()
		select {
		case <-worker.Done():
		case <-time.After(5 * time.Second):
			t.Error("SFTP worker did not close")
		}
		client.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Error("SFTP protocol server did not observe the closed client")
		}
	})
	return worker, root
}

func requireQueuedResult(t *testing.T, queued bool, worker *Worker, operation Operation) Result {
	t.Helper()
	if !queued {
		t.Fatalf("operation %d was not queued", operation)
	}
	result := waitResult(t, worker)
	if result.Operation != operation {
		t.Fatalf("operation = %d, want %d", result.Operation, operation)
	}
	return result
}

func requireProtocolError(t *testing.T, result Result, want error) {
	t.Helper()
	if result.Err == nil {
		t.Fatalf("result unexpectedly succeeded: %+v", result)
	}
	if want != nil && !errors.Is(result.Err, want) {
		t.Fatalf("error = %T %v, want %v", result.Err, result.Err, want)
	}
}

func TestWorkerUploadsFileOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	if err := os.Mkdir(filepath.Join(root, "incoming"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("alpha\x00beta\ngamma")
	localPath := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.Upload(localPath, "incoming/data.bin"), worker, OperationUpload)
	if result.Err != nil || result.Path != "incoming/data.bin" || result.LocalPath != localPath || result.Bytes != int64(len(content)) {
		t.Fatalf("upload result = %+v", result)
	}
	uploaded, err := os.ReadFile(filepath.Join(root, "incoming", "data.bin"))
	if err != nil || !bytes.Equal(uploaded, content) {
		t.Fatalf("uploaded content = %q, %v", uploaded, err)
	}

	result = requireQueuedResult(t, worker.Upload(localPath, "missing/data.bin"), worker, OperationUpload)
	if result.Path != "missing/data.bin" || result.LocalPath != localPath {
		t.Fatalf("failed upload result = %+v", result)
	}
	requireProtocolError(t, result, os.ErrNotExist)
}

func TestWorkerCreatesDirectoryOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	if err := os.Mkdir(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.CreateDirectory("projects/new"), worker, OperationCreateDirectory)
	if result.Err != nil || result.Path != "projects/new" {
		t.Fatalf("create directory result = %+v", result)
	}
	info, err := os.Stat(filepath.Join(root, "projects", "new"))
	if err != nil || !info.IsDir() {
		t.Fatalf("created directory = %+v, %v", info, err)
	}

	result = requireQueuedResult(t, worker.CreateDirectory("projects/new"), worker, OperationCreateDirectory)
	requireProtocolError(t, result, nil)
}

func TestWorkerRenamesAndMovesOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	if err := os.Mkdir(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("move me"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.Rename("source.txt", "archive/moved.txt"), worker, OperationRename)
	if result.Err != nil || result.Path != "source.txt" || result.Destination != "archive/moved.txt" {
		t.Fatalf("rename result = %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(root, "archive", "moved.txt"))
	if err != nil || string(content) != "move me" {
		t.Fatalf("moved content = %q, %v", content, err)
	}

	result = requireQueuedResult(t, worker.Rename("source.txt", "archive/again.txt"), worker, OperationRename)
	if result.Destination != "archive/again.txt" {
		t.Fatalf("failed rename result = %+v", result)
	}
	requireProtocolError(t, result, os.ErrNotExist)
}

func TestWorkerRemovesFileOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	remotePath := filepath.Join(root, "delete.txt")
	if err := os.WriteFile(remotePath, []byte("delete me"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.RemoveFile("delete.txt"), worker, OperationRemoveFile)
	if result.Err != nil || result.Path != "delete.txt" {
		t.Fatalf("remove file result = %+v", result)
	}
	if _, err := os.Stat(remotePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed file still exists: %v", err)
	}

	result = requireQueuedResult(t, worker.RemoveFile("delete.txt"), worker, OperationRemoveFile)
	requireProtocolError(t, result, os.ErrNotExist)
}

func TestWorkerRemovesDirectoryWithItsContentsOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	directory := filepath.Join(root, "remove-dir")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "child.txt"), []byte("goes too"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "inner.txt"), []byte("goes too"), 0o600); err != nil {
		t.Fatal(err)
	}
	// What a link in the folder points at is not the folder's to delete.
	kept := filepath.Join(root, "kept.txt")
	if err := os.WriteFile(kept, []byte("stays"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kept, filepath.Join(directory, "kept-link")); err != nil {
		t.Skipf("symbolic links are not available here: %v", err)
	}

	result := requireQueuedResult(t, worker.RemoveDirectory("remove-dir"), worker, OperationRemoveDirectory)
	if result.Err != nil || result.Path != "remove-dir" {
		t.Fatalf("remove directory result = %+v", result)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed directory still exists: %v", err)
	}
	if content, err := os.ReadFile(kept); err != nil || string(content) != "stays" {
		t.Fatalf("the link's target = %q, %v", content, err)
	}

	result = requireQueuedResult(t, worker.RemoveDirectory("remove-dir"), worker, OperationRemoveDirectory)
	requireProtocolError(t, result, os.ErrNotExist)
}

// Links nested in the tree are removed as links, whether they point at an
// absolute path or climb out with "..": what they lead to is not the folder's.
func TestWorkerRemovalNeverReachesThroughNestedLinks(t *testing.T) {
	worker, root := newProtocolWorker(t)
	workspace := filepath.Join(root, "workspace")
	outsideAbsolute := filepath.Join(root, "outside-absolute")
	outsideRelative := filepath.Join(root, "outside-relative")
	for _, directory := range []string{filepath.Join(workspace, "deep", "deeper"), outsideAbsolute, outsideRelative} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{
		filepath.Join(workspace, "deep", "deeper", "own.txt"),
		filepath.Join(outsideAbsolute, "precious.txt"),
		filepath.Join(outsideRelative, "precious.txt"),
	} {
		if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outsideAbsolute, filepath.Join(workspace, "deep", "to-absolute")); err != nil {
		t.Skipf("symbolic links are not available here: %v", err)
	}
	if err := os.Symlink("../../../outside-relative", filepath.Join(workspace, "deep", "deeper", "to-relative")); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.RemoveDirectory("workspace"), worker, OperationRemoveDirectory)
	if result.Err != nil {
		t.Fatalf("remove directory result = %+v", result)
	}
	if _, err := os.Lstat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the workspace still exists: %v", err)
	}
	for _, outside := range []string{outsideAbsolute, outsideRelative} {
		if content, err := os.ReadFile(filepath.Join(outside, "precious.txt")); err != nil || string(content) != "data" {
			t.Fatalf("%s lost its content: %q, %v", outside, content, err)
		}
	}
}

// A folder that cannot be listed is reported as unreadable, not as the "not
// empty" the server answers when asked to remove it anyway.
func TestWorkerRemovalReportsAFolderItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not stop this user from listing a folder")
	}
	worker, root := newProtocolWorker(t)
	locked := filepath.Join(root, "workspace", "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	result := requireQueuedResult(t, worker.RemoveDirectory("workspace"), worker, OperationRemoveDirectory)
	requireProtocolError(t, result, os.ErrPermission)
	if !strings.Contains(result.Err.Error(), "workspace/locked") {
		t.Fatalf("the error does not name the folder: %v", result.Err)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Fatalf("the unreadable folder was touched: %v", err)
	}
}

// A link to a folder is deleted as the link it is: what it points at stays.
func TestWorkerRemovesALinkToAFolderWithoutFollowingIt(t *testing.T) {
	worker, root := newProtocolWorker(t)
	kept := filepath.Join(root, "kept-dir")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "inside.txt"), []byte("stays"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "kept-link")
	if err := os.Symlink(kept, link); err != nil {
		t.Skipf("symbolic links are not available here: %v", err)
	}

	result := requireQueuedResult(t, worker.RemoveDirectory("kept-link"), worker, OperationRemoveDirectory)
	if result.Err != nil {
		t.Fatalf("remove link result = %+v", result)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the link still exists: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(kept, "inside.txt")); err != nil || string(content) != "stays" {
		t.Fatalf("the folder the link pointed at = %q, %v", content, err)
	}
}

func TestWorkerChangesPermissionsOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	remotePath := filepath.Join(root, "script.sh")
	if err := os.WriteFile(remotePath, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	wantMode := os.FileMode(0o751)
	result := requireQueuedResult(t, worker.Chmod("script.sh", wantMode), worker, OperationChmod)
	if result.Err != nil || result.Path != "script.sh" || result.Mode != wantMode {
		t.Fatalf("chmod result = %+v", result)
	}
	// The "remote" is a directory on this machine, and Windows keeps no permission bits.
	info, err := os.Stat(remotePath)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != wantMode {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}

	result = requireQueuedResult(t, worker.Chmod("missing.sh", 0o640), worker, OperationChmod)
	if result.Mode != 0o640 {
		t.Fatalf("failed chmod result = %+v", result)
	}
	requireProtocolError(t, result, os.ErrNotExist)
}

type closeTrackingWriter struct {
	bytes.Buffer
	closed   bool
	closeErr error
}

func (writer *closeTrackingWriter) Close() error {
	writer.closed = true
	return writer.closeErr
}

type uploadRemote struct {
	*fakeRemote
	path   string
	writer *closeTrackingWriter
}

func (remote *uploadRemote) Create(path string) (io.WriteCloser, error) {
	remote.path = path
	return remote.writer, nil
}

func TestWorkerUploadClosesDestinationAndPropagatesCloseError(t *testing.T) {
	closeErr := errors.New("remote close failed")
	writer := &closeTrackingWriter{closeErr: closeErr}
	remote := &uploadRemote{
		fakeRemote: &fakeRemote{closed: make(chan struct{})},
		writer:     writer,
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	localPath := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(localPath, []byte("complete payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.Upload(localPath, "target.txt"), worker, OperationUpload)
	if !errors.Is(result.Err, closeErr) || !writer.closed {
		t.Fatalf("upload result = %+v, closed = %v", result, writer.closed)
	}
	if remote.path != "target.txt" || writer.String() != "complete payload" {
		t.Fatalf("path = %q, content = %q", remote.path, writer.String())
	}
}

// writeOrderRecorder wraps the server side of an SFTP pipe and captures every
// SSH_FXP_WRITE packet's offset and payload in wire arrival order, as a bastion
// or streaming SFTP proxy sees them.
type writeOrderRecorder struct {
	net.Conn
	mu          sync.Mutex
	buf         []byte
	offsets     []uint64
	streamedOut []byte
}

func (r *writeOrderRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.buf = append(r.buf, p[:n]...)
		for len(r.buf) >= 4 {
			pktLen := int(binary.BigEndian.Uint32(r.buf[:4]))
			if len(r.buf) < 4+pktLen {
				break
			}
			pkt := r.buf[4 : 4+pktLen]
			// SSH_FXP_WRITE = 6: type(1) + id(4) + handleLen(4) + handle + offset(8) + dataLen(4) + data
			if len(pkt) >= 1 && pkt[0] == 6 && len(pkt) >= 9 {
				handleLen := int(binary.BigEndian.Uint32(pkt[5:9]))
				offPos := 9 + handleLen
				if len(pkt) >= offPos+12 {
					offset := binary.BigEndian.Uint64(pkt[offPos : offPos+8])
					dataLen := int(binary.BigEndian.Uint32(pkt[offPos+8 : offPos+12]))
					dataPos := offPos + 12
					if len(pkt) >= dataPos+dataLen {
						r.offsets = append(r.offsets, offset)
						r.streamedOut = append(r.streamedOut, pkt[dataPos:dataPos+dataLen]...)
					}
				}
			}
			r.buf = r.buf[4+pktLen:]
		}
		r.mu.Unlock()
	}
	return n, err
}

// Bastion and streaming SFTP proxies commonly expect SSH_FXP_WRITE packets in
// ascending offset order (as FileZilla and OpenSSH sftp send them). Sending
// chunks out of order scrambles the received file and breaks archive hashes
// without returning an SFTP error (#1348).
func TestWorkerUploadDispatchesWritePacketsInOffsetOrder(t *testing.T) {
	var recorder *writeOrderRecorder
	worker, root := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn {
		recorder = &writeOrderRecorder{Conn: conn}
		return recorder
	}, nil)

	const size = 2 << 20 // 64 chunks of 32 KiB
	localPath, content := randomFile(t, size)
	result := requireQueuedResult(t, worker.Upload(localPath, "archive.zip"), worker, OperationUpload)
	if result.Err != nil {
		t.Fatalf("upload failed: %v", result.Err)
	}
	uploaded, err := os.ReadFile(filepath.Join(root, "archive.zip"))
	if err != nil || !bytes.Equal(uploaded, content) {
		t.Fatalf("uploaded file mismatch: err=%v len=%d", err, len(uploaded))
	}

	recorder.mu.Lock()
	offsets := append([]uint64(nil), recorder.offsets...)
	streamed := append([]byte(nil), recorder.streamedOut...)
	recorder.mu.Unlock()

	if len(offsets) != size/uploadChunkSize {
		t.Fatalf("captured %d write packets, want %d", len(offsets), size/uploadChunkSize)
	}
	for i, got := range offsets {
		want := uint64(i) * uploadChunkSize
		if got != want {
			t.Fatalf("SSH_FXP_WRITE packet %d arrived with offset %d, want %d (out-of-order dispatch)", i, got, want)
		}
	}
	if sha256.Sum256(streamed) != sha256.Sum256(content) {
		t.Fatal("sequentially streamed SSH_FXP_WRITE payload hash does not match source archive")
	}

	// Resuming a partial upload must likewise dispatch the remaining writes in
	// strictly ascending offset order starting from the resumed offset.
	recorder.mu.Lock()
	recorder.offsets = nil
	recorder.streamedOut = nil
	recorder.mu.Unlock()

	const resumed = int64(4 * uploadChunkSize)
	partial := filepath.Join(root, remotePartialPath("resumed.zip", localPath))
	if err := os.WriteFile(partial, content[:resumed], 0o600); err != nil {
		t.Fatal(err)
	}
	id, queued := worker.QueueUploadResume(localPath, "resumed.zip", resumed, int64(len(content)), sourceModTime(t, localPath))
	resumeResult := queueResult(t, id, queued, worker)
	if resumeResult.Err != nil || resumeResult.Resumed != resumed {
		t.Fatalf("resumed upload = %+v", resumeResult)
	}
	resumedFile, err := os.ReadFile(filepath.Join(root, "resumed.zip"))
	if err != nil || !bytes.Equal(resumedFile, content) {
		t.Fatalf("resumed file mismatch: err=%v len=%d", err, len(resumedFile))
	}
	recorder.mu.Lock()
	resumedOffsets := append([]uint64(nil), recorder.offsets...)
	resumedStreamed := append([]byte(nil), recorder.streamedOut...)
	recorder.mu.Unlock()
	wantCount := (size - int(resumed)) / uploadChunkSize
	if len(resumedOffsets) != wantCount {
		t.Fatalf("resumed upload captured %d write packets, want %d", len(resumedOffsets), wantCount)
	}
	for i, got := range resumedOffsets {
		want := uint64(resumed) + uint64(i)*uploadChunkSize
		if got != want {
			t.Fatalf("resumed SSH_FXP_WRITE packet %d arrived with offset %d, want %d", i, got, want)
		}
	}
	if sha256.Sum256(resumedStreamed) != sha256.Sum256(content[resumed:]) {
		t.Fatal("resumed sequentially streamed payload hash does not match source tail")
	}
}
