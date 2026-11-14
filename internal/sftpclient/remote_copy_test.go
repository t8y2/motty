package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// startSFTPClient starts one real SFTP server rooted at root. The caller closes
// the client, which ends the server too, so every remote copy attempt gets a
// subsystem of its own as a real one does.
func startSFTPClient(root string) (*sftp.Client, error) {
	clientConnection, serverConnection := net.Pipe()
	server, err := sftp.NewServer(serverConnection, sftp.WithServerWorkingDirectory(root))
	if err != nil {
		clientConnection.Close()
		serverConnection.Close()
		return nil, err
	}
	go func() {
		_ = server.Serve()
		_ = server.Close()
	}()
	client, err := sftp.NewClientPipe(clientConnection, clientConnection)
	if err != nil {
		clientConnection.Close()
		serverConnection.Close()
		return nil, err
	}
	return client, nil
}

func remoteSourceFor(root string) RemoteSource {
	return func() (Remote, error) {
		client, err := startSFTPClient(root)
		if err != nil {
			return nil, err
		}
		return remote{Client: client}, nil
	}
}

func waitTransferResult(t *testing.T, worker *Worker) Result {
	t.Helper()
	select {
	case result := <-worker.Results():
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not complete the transfer")
		return Result{}
	}
}

func TestRemoteCopyStreamsBetweenServers(t *testing.T) {
	sourceRoot := t.TempDir()
	content := []byte("alpha\x00beta\ngamma")
	if err := os.WriteFile(filepath.Join(sourceRoot, "alpha.bin"), content, 0o640); err != nil {
		t.Fatal(err)
	}
	worker, destinationRoot := newProtocolWorker(t)

	id, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source:      remoteSourceFor(sourceRoot),
		SourceRoot:  "alpha.bin",
		Sources:     []string{"alpha.bin"},
		Destination: ".",
		Policy:      ExistingFail,
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	result := waitTransferResult(t, worker)
	if result.TransferID != id || result.Operation != OperationRemoteCopy {
		t.Fatalf("result = %+v", result)
	}
	if result.Err != nil || result.Files != 1 || result.Bytes != int64(len(content)) {
		t.Fatalf("remote copy result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(destinationRoot, "alpha.bin"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("destination content = %q, %v", got, err)
	}
	if partials, err := filepath.Glob(filepath.Join(destinationRoot, ".motty.*.part")); err != nil || len(partials) != 0 {
		t.Fatalf("partial file leaked: %v, %v", partials, err)
	}
	snapshot := snapshotOf(worker, id)
	if snapshot.State != TransferCompleted || snapshot.Source != "alpha.bin" || snapshot.Destination != "." {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestRemoteCopyDirectoryBatch(t *testing.T) {
	sourceRoot := t.TempDir()
	for name, content := range map[string]string{
		"docs/guide.txt":           "guide",
		"docs/api/deep.txt":        "deep",
		"docs/api/nested/more.txt": "more",
		"top.txt":                  "top",
	} {
		full := filepath.Join(sourceRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	worker, destinationRoot := newProtocolWorker(t)

	_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source:     remoteSourceFor(sourceRoot),
		SourceRoot: "docs",
		Sources:    []string{"docs", "top.txt"},
		// The batch lands in a folder that already exists on the server.
		Destination: ".",
		Recursive:   true,
		Policy:      ExistingFail,
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	result := waitTransferResult(t, worker)
	if result.Err != nil || result.Files != 4 {
		t.Fatalf("remote copy result = %+v", result)
	}
	for name, want := range map[string]string{
		"docs/guide.txt":           "guide",
		"docs/api/deep.txt":        "deep",
		"docs/api/nested/more.txt": "more",
		"top.txt":                  "top",
	} {
		got, err := os.ReadFile(filepath.Join(destinationRoot, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
}

func TestRemoteCopyPolicies(t *testing.T) {
	sourceRoot := t.TempDir()
	content := []byte("new content")
	if err := os.WriteFile(filepath.Join(sourceRoot, "report.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	worker, destinationRoot := newProtocolWorker(t)
	destination := filepath.Join(destinationRoot, "report.txt")
	if err := os.WriteFile(destination, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyFile := func(policy ExistingPolicy) {
		t.Helper()
		_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
			Source: remoteSourceFor(sourceRoot), SourceRoot: "report.txt",
			Sources:     []string{"report.txt"},
			Destination: ".", Policy: policy,
		})
		if !queued {
			t.Fatal("remote copy was not queued")
		}
	}
	copyFile(ExistingFail)
	if result := waitTransferResult(t, worker); !errors.Is(result.Err, ErrDestinationExists) {
		t.Fatalf("fail policy result = %+v", result)
	}
	if got, _ := os.ReadFile(destination); string(got) != "old content" {
		t.Fatalf("fail policy changed the destination: %q", got)
	}
	copyFile(ExistingSkip)
	if result := waitTransferResult(t, worker); result.Err != nil {
		t.Fatalf("skip policy result = %+v", result)
	}
	if got, _ := os.ReadFile(destination); string(got) != "old content" {
		t.Fatalf("skip policy changed the destination: %q", got)
	}
	copyFile(ExistingReplace)
	if result := waitTransferResult(t, worker); result.Err != nil {
		t.Fatalf("replace policy result = %+v", result)
	}
	if got, _ := os.ReadFile(destination); !bytes.Equal(got, content) {
		t.Fatalf("replace policy left %q", got)
	}
}

func TestRemoteCopyResumesFromAPartialFile(t *testing.T) {
	sourceRoot := t.TempDir()
	content := []byte("resumable content across two servers")
	if err := os.WriteFile(filepath.Join(sourceRoot, "partial.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	worker, destinationRoot := newProtocolWorker(t)
	prefix := content[:8]
	if err := os.WriteFile(filepath.Join(destinationRoot, ".motty.partial.txt.part"), prefix, 0o644); err != nil {
		t.Fatal(err)
	}
	_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source: remoteSourceFor(sourceRoot), SourceRoot: "partial.txt",
		Sources:     []string{"partial.txt"},
		Destination: ".", Policy: ExistingResume,
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	result := waitTransferResult(t, worker)
	if result.Err != nil || result.Resumed != int64(len(prefix)) {
		t.Fatalf("resume result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(destinationRoot, "partial.txt"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("resumed content = %q, %v", got, err)
	}
}

func TestRemoteCopyCancelClosesTheSource(t *testing.T) {
	source := newGateRemote(4, "blocked.bin")
	worker, _ := newProtocolWorker(t)
	id, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source:      func() (Remote, error) { return source, nil },
		SourceRoot:  "/",
		Sources:     []string{"/blocked.bin"},
		Destination: ".",
		Policy:      ExistingFail,
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	deadline := time.Now().Add(3 * time.Second)
	for source.reads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if source.reads.Load() == 0 {
		t.Fatal("remote copy never read from the source")
	}
	if !worker.CancelTransfer(id) {
		t.Fatal("cancel was refused")
	}
	result := waitTransferResult(t, worker)
	if result.TransferID != id || !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("canceled result = %+v", result)
	}
	select {
	case <-source.closed:
	default:
		t.Fatal("cancel did not close the source subsystem")
	}
}

// bareRemote is a Remote without the seekable write path a remote copy needs.
type bareRemote struct{}

func (bareRemote) Getwd() (string, error)                { return "/", nil }
func (bareRemote) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (bareRemote) Open(string) (io.ReadCloser, error)    { return nil, os.ErrNotExist }
func (bareRemote) Close() error                          { return nil }

func TestRemoteCopyRequiresSFTPOnBothServers(t *testing.T) {
	worker, _ := newProtocolWorker(t)
	_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source:      func() (Remote, error) { return bareRemote{}, nil },
		SourceRoot:  "/",
		Sources:     []string{"/file"},
		Destination: ".",
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	if result := waitTransferResult(t, worker); result.Err == nil {
		t.Fatalf("a source without a seekable SFTP path succeeded: %+v", result)
	}
}

func TestRemoteCopyRejectsSymbolicLinkSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need a privilege the Windows test account may not have")
	}
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(sourceRoot, "link.txt")); err != nil {
		t.Fatal(err)
	}
	worker, _ := newProtocolWorker(t)
	_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source: remoteSourceFor(sourceRoot), SourceRoot: "link.txt",
		Sources:     []string{"link.txt"},
		Destination: ".",
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	if result := waitTransferResult(t, worker); !errors.Is(result.Err, ErrSymbolicLink) {
		t.Fatalf("symbolic link source result = %+v", result)
	}
}

func TestRemoteCopyRejectsUnsafeTargetName(t *testing.T) {
	source := newGateRemote(1, "unused")
	worker, _ := newProtocolWorker(t)
	_, queued := worker.QueueRemoteCopy(RemoteCopyRequest{
		Source:      func() (Remote, error) { return source, nil },
		SourceRoot:  "..",
		Sources:     []string{".."},
		Destination: ".",
		Policy:      ExistingFail,
	})
	if !queued {
		t.Fatal("remote copy was not queued")
	}
	if result := waitTransferResult(t, worker); !errors.Is(result.Err, ErrPathTraversal) {
		t.Fatalf("unsafe source name result = %+v", result)
	}
}
