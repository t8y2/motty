package sftpclient

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// countingReadRemote wraps the test remote and counts every byte read back
// through a transfer file, so a test can tell whether a resume re-downloaded
// the partial it already has.
type countingReadRemote struct {
	remote
	reads *atomic.Int64
}

func (client countingReadRemote) OpenTransferFile(remotePath string, flags int) (TransferFile, error) {
	file, err := client.remote.OpenTransferFile(remotePath, flags)
	if err != nil {
		return nil, err
	}
	return &countingReadFile{TransferFile: file, reads: client.reads}, nil
}

type countingReadFile struct {
	TransferFile
	reads *atomic.Int64
}

func (file *countingReadFile) Read(p []byte) (int, error) {
	n, err := file.TransferFile.Read(p)
	file.reads.Add(int64(n))
	return n, err
}

func (file *countingReadFile) ReadAt(p []byte, off int64) (int, error) {
	at, ok := file.TransferFile.(io.ReaderAt)
	if !ok {
		return 0, io.ErrNoProgress
	}
	n, err := at.ReadAt(p, off)
	file.reads.Add(int64(n))
	return n, err
}

func countingWorker(t *testing.T) (*Worker, string, *atomic.Int64) {
	t.Helper()
	reads := &atomic.Int64{}
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		return countingReadRemote{remote: base, reads: reads}
	})
	return worker, root, reads
}

// A resume whose trusted prefix was written by an earlier attempt does not read
// that prefix back: it drops anything past the trusted length and continues.
func TestTrustedResumeDoesNotReadThePartialBack(t *testing.T) {
	worker, root, reads := countingWorker(t)
	payload := bytes.Repeat([]byte("motty trusted resume "), 1<<13)
	local := filepath.Join(t.TempDir(), "trusted.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	trusted := int64(32 << 10)
	stale := append(append([]byte(nil), payload[:trusted]...), []byte("stale tail the failed attempt left")...)
	partial := filepath.Join(root, remotePartialPath("trusted.bin"))
	if err := os.WriteFile(partial, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "trusted.bin", trusted, int64(len(payload)))
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != trusted || result.Bytes != int64(len(payload)) {
		t.Fatalf("resume result = %+v, want resumed %d of %d", result, trusted, len(payload))
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("resume read %d bytes of the partial back, want none", got)
	}
	final := filepath.Join(root, "trusted.bin")
	if got, err := os.ReadFile(final); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("remote final = %d bytes, %v", len(got), err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("remote partial remains: %v", err)
	}
}

// Without a trusted prefix a resume has to verify the partial it found by
// reading it back, which is what an arbitrary leftover must still pay for.
func TestUntrustedResumeVerifiesThePartialBack(t *testing.T) {
	worker, root, reads := countingWorker(t)
	payload := bytes.Repeat([]byte("motty untrusted resume "), 1<<13)
	local := filepath.Join(t.TempDir(), "untrusted.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := int64(32 << 10)
	partial := filepath.Join(root, remotePartialPath("untrusted.bin"))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUpload(local, "untrusted.bin", false, ExistingResume)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != prefix || result.Bytes != int64(len(payload)) {
		t.Fatalf("resume result = %+v, want resumed %d of %d", result, prefix, len(payload))
	}
	if reads.Load() <= 0 {
		t.Fatal("an untrusted resume did not verify the partial")
	}
	final := filepath.Join(root, "untrusted.bin")
	if got, err := os.ReadFile(final); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("remote final = %d bytes, %v", len(got), err)
	}
}

// A trusted length longer than the partial cannot describe it, so the resume
// falls back to verifying what is actually on the server.
func TestATrustLongerThanThePartialStillVerifies(t *testing.T) {
	worker, root, reads := countingWorker(t)
	payload := bytes.Repeat([]byte("motty trust overflow "), 1<<13)
	local := filepath.Join(t.TempDir(), "overflow.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := int64(24 << 10)
	partial := filepath.Join(root, remotePartialPath("overflow.bin"))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "overflow.bin", prefix+1024, int64(len(payload)))
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != prefix || result.Bytes != int64(len(payload)) {
		t.Fatalf("resume result = %+v, want resumed %d of %d", result, prefix, len(payload))
	}
	if reads.Load() <= 0 {
		t.Fatal("a trust longer than the partial did not verify it")
	}
}

// A trusted prefix recorded against a different source size is not adopted:
// the source may have been replaced since, so the partial is verified.
func TestATrustFromAChangedSourceStillVerifies(t *testing.T) {
	worker, root, reads := countingWorker(t)
	payload := bytes.Repeat([]byte("motty changed source "), 1<<13)
	local := filepath.Join(t.TempDir(), "changed.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := int64(32 << 10)
	partial := filepath.Join(root, remotePartialPath("changed.bin"))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "changed.bin", prefix, int64(len(payload))+1)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != prefix || result.Bytes != int64(len(payload)) {
		t.Fatalf("resume result = %+v, want resumed %d of %d", result, prefix, len(payload))
	}
	if reads.Load() <= 0 {
		t.Fatal("a trust recorded against another size did not verify the partial")
	}
}
