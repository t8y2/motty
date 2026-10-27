package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	root := t.TempDir()
	clientConnection, serverConnection := net.Pipe()
	server, err := sftp.NewServer(serverConnection, sftp.WithServerWorkingDirectory(root))
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
	worker := NewWorker(func() (Remote, error) { return remote{Client: client}, nil }, nil)
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

func TestWorkerRemovesOnlyEmptyDirectoryOverSFTP(t *testing.T) {
	worker, root := newProtocolWorker(t)
	directory := filepath.Join(root, "remove-dir")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(directory, "child.txt")
	if err := os.WriteFile(child, []byte("keep nonempty"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := requireQueuedResult(t, worker.RemoveDirectory("remove-dir"), worker, OperationRemoveDirectory)
	requireProtocolError(t, result, nil)
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	result = requireQueuedResult(t, worker.RemoveDirectory("remove-dir"), worker, OperationRemoveDirectory)
	if result.Err != nil || result.Path != "remove-dir" {
		t.Fatalf("remove directory result = %+v", result)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed directory still exists: %v", err)
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
