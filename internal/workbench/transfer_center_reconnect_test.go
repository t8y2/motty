package workbench

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/sftpclient"
)

// reconnectGateRemote stops an upload after a known number of bytes, so a test
// can take the connection away in the middle of the file. Writes go through the
// test remote's sequential path, which keeps the kept partial a prefix of the
// source, as a real resume needs.
type reconnectGateRemote struct {
	*workbenchSFTPRemote
	after   int64
	written atomic.Int64
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func newReconnectGateRemote(remote *workbenchSFTPRemote, after int64) *reconnectGateRemote {
	return &reconnectGateRemote{
		workbenchSFTPRemote: remote, after: after,
		reached: make(chan struct{}), release: make(chan struct{}),
	}
}

func (remote *reconnectGateRemote) OpenTransferFile(remotePath string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.workbenchSFTPRemote.OpenTransferFile(remotePath, flags)
	if err != nil || flags == os.O_RDONLY {
		return file, err
	}
	return &reconnectGateFile{TransferFile: file, remote: remote}, nil
}

type reconnectGateFile struct {
	sftpclient.TransferFile
	remote *reconnectGateRemote
}

func (file *reconnectGateFile) Write(source []byte) (int, error) {
	if file.remote.written.Add(int64(len(source))) > file.remote.after {
		file.remote.once.Do(func() { close(file.remote.reached) })
		<-file.remote.release
	}
	return file.TransferFile.Write(source)
}

// waitTransfer waits for the one entry of a test's transfer center to reach a
// state, pumping the window the way a frame would.
func waitTransfer(t *testing.T, state *State, want sftpclient.TransferState) sftpclient.TransferSnapshot {
	t.Helper()
	deadline := time.Now().Add(waitBudget())
	for time.Now().Before(deadline) {
		state.Wake()
		if len(state.transfers.entries) == 1 {
			snapshot := state.transfers.entries[0].snapshot
			if snapshot.State == want {
				return snapshot
			}
		}
		time.Sleep(time.Millisecond)
	}
	if len(state.transfers.entries) == 1 {
		t.Fatalf("transfer = %+v, want state %v", state.transfers.entries[0].snapshot, want)
	}
	t.Fatalf("transfer center has %d entries, want one at state %v", len(state.transfers.entries), want)
	return sftpclient.TransferSnapshot{}
}

// A connection that goes away in the middle of an upload keeps the partial and
// the row; opening the host's files again continues the upload from there.
func TestTransferCenterResumesInterruptedUploadAfterReconnect(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	payload := bytes.Repeat([]byte("motty resume "), 1<<16)
	local := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	remotePath := path.Join(session.files.path, "payload.bin")

	view := session.files
	view.worker.Close()
	gated := newReconnectGateRemote(remote, 64<<10)
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, state.notifyFor(session))
	state.configureSFTPWorker(view)
	interruptedWorker := view.worker
	remoteDir := view.path

	state.queueSFTPUpload(view, local, false)
	select {
	case <-gated.reached:
	case <-time.After(waitBudget()):
		t.Fatal("the upload did not reach the gate")
	}
	state.closeSFTP(session)
	close(gated.release)
	select {
	case <-interruptedWorker.Done():
	case <-time.After(waitBudget()):
		t.Fatal("the interrupted worker did not stop")
	}
	if len(state.transfers.entries) != 1 {
		t.Fatalf("transfer center has %d entries, want one", len(state.transfers.entries))
	}
	entry := state.transfers.entries[0]
	if entry.snapshot.State != sftpclient.TransferInterrupted || entry.owner != nil {
		t.Fatalf("entry = %+v owner=%v, want an interrupted row without a view", entry.snapshot, entry.owner != nil)
	}
	partial := testUploadPartial(t, remote, remoteDir)
	if len(partial) == 0 || len(partial) >= len(payload) || !bytes.Equal(partial, payload[:len(partial)]) {
		t.Fatalf("kept partial = %d bytes, want a prefix of the source", len(partial))
	}

	state.openSFTP(session)
	snapshot := waitTransfer(t, state, sftpclient.TransferCompleted)
	if snapshot.Resumed != int64(len(partial)) {
		t.Fatalf("resumed %d bytes, want the kept %d", snapshot.Resumed, len(partial))
	}
	remote.mu.Lock()
	content := append([]byte(nil), remote.files[remotePath]...)
	remote.mu.Unlock()
	if !bytes.Equal(content, payload) {
		t.Fatalf("uploaded file is %d bytes and differs from the source", len(content))
	}
}

// testUploadPartial returns the kept partial an upload left in a folder, named
// by the worker rather than by the test.
func testUploadPartial(t *testing.T, remote *workbenchSFTPRemote, dir string) []byte {
	t.Helper()
	remote.mu.Lock()
	defer remote.mu.Unlock()
	for name, content := range remote.files {
		base := path.Base(name)
		if path.Dir(name) == dir && strings.HasPrefix(base, ".motty.") && strings.HasSuffix(base, ".part") {
			return append([]byte(nil), content...)
		}
	}
	return nil
}

var errWorkbenchWrite = errors.New("injected write failure")

// failOnceWriteRemote refuses the first write past after bytes, then works, so
// a failed upload leaves a partial the card's Retry can continue.
type failOnceWriteRemote struct {
	*workbenchSFTPRemote
	after   int64
	written atomic.Int64
	failed  atomic.Bool
}

func (remote *failOnceWriteRemote) OpenTransferFile(remotePath string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.workbenchSFTPRemote.OpenTransferFile(remotePath, flags)
	if err != nil || flags == os.O_RDONLY {
		return file, err
	}
	return &failOnceWriteFile{TransferFile: file, remote: remote}, nil
}

type failOnceWriteFile struct {
	sftpclient.TransferFile
	remote *failOnceWriteRemote
}

func (file *failOnceWriteFile) Write(source []byte) (int, error) {
	if !file.remote.failed.Load() && file.remote.written.Add(int64(len(source))) > file.remote.after {
		file.remote.failed.Store(true)
		return 0, errWorkbenchWrite
	}
	return file.TransferFile.Write(source)
}

// Retry on a network failure continues the kept partial instead of sending the
// whole file again.
func TestTransferCenterRetryContinuesAFailedUpload(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	payload := bytes.Repeat([]byte("motty retry "), 1<<16)
	local := filepath.Join(t.TempDir(), "retry.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	remotePath := path.Join(session.files.path, "retry.bin")

	view := session.files
	view.worker.Close()
	failing := &failOnceWriteRemote{workbenchSFTPRemote: remote, after: 64 << 10}
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return failing, nil }, state.notifyFor(session))
	state.configureSFTPWorker(view)

	state.queueSFTPUpload(view, local, false)
	failed := waitTransfer(t, state, sftpclient.TransferFailed)
	if !failed.Retryable {
		t.Fatal("a failed upload is not retryable")
	}
	state.Click(hitTransfersRetry.At(state.transfers.entries[0].id))
	snapshot := waitTransfer(t, state, sftpclient.TransferCompleted)
	if snapshot.Resumed <= 0 || snapshot.Resumed >= int64(len(payload)) {
		t.Fatalf("retry resumed %d of %d bytes, want the kept prefix", snapshot.Resumed, len(payload))
	}
	remote.mu.Lock()
	content := append([]byte(nil), remote.files[remotePath]...)
	remote.mu.Unlock()
	if !bytes.Equal(content, payload) {
		t.Fatalf("retried upload is %d bytes and differs from the source", len(content))
	}
}
