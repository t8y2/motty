package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
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
	partial := filepath.Join(root, remotePartialPath("trusted.bin", local))
	if err := os.WriteFile(partial, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "trusted.bin", trusted, int64(len(payload)), sourceModTime(t, local))
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
	partial := filepath.Join(root, remotePartialPath("untrusted.bin", local))
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
	partial := filepath.Join(root, remotePartialPath("overflow.bin", local))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "overflow.bin", prefix+1024, int64(len(payload)), sourceModTime(t, local))
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
	partial := filepath.Join(root, remotePartialPath("changed.bin", local))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "changed.bin", prefix, int64(len(payload))+1, sourceModTime(t, local))
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != prefix || result.Bytes != int64(len(payload)) {
		t.Fatalf("resume result = %+v, want resumed %d of %d", result, prefix, len(payload))
	}
	if reads.Load() <= 0 {
		t.Fatal("a trust recorded against another size did not verify the partial")
	}
}

// A source edited in place at the same size is not the file the trusted prefix
// was measured on: the size matches, so only the modification time can tell,
// and the partial must be verified instead of adopted. The old prefix would
// otherwise stay on the server and the new tail would be appended to it.
func TestATrustFromASameSizeEditStillVerifies(t *testing.T) {
	worker, root, reads := countingWorker(t)
	payload := bytes.Repeat([]byte("motty same size edit "), 1<<13)
	local := filepath.Join(t.TempDir(), "edited.bin")
	if err := os.WriteFile(local, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	prefix := int64(32 << 10)
	partial := filepath.Join(root, remotePartialPath("edited.bin", local))
	if err := os.WriteFile(partial, payload[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}
	before := sourceModTime(t, local)

	file, err := os.OpenFile(local, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("X"), 10); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after := before.Add(2 * time.Second)
	if err := os.Chtimes(local, after, after); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUploadResume(local, "edited.bin", prefix, int64(len(payload)), before)
	result := queueResult(t, id, queued, worker)
	if result.Err == nil || !errors.Is(result.Err, ErrResumeMismatch) {
		t.Fatalf("same-size edit resume = %+v, want ErrResumeMismatch", result)
	}
	if reads.Load() <= 0 {
		t.Fatal("a same-size edited source did not verify the partial")
	}
}

// sourceModTime returns a file's modification time, the identity a resume
// records for the source.
func sourceModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// The partial belongs to the source that started it: another source uploading
// to the same destination gets its own partial instead of adopting these bytes.
func TestAnUploadDoesNotAdoptAnotherSourcesPartial(t *testing.T) {
	worker, root := newProtocolWorker(t)
	sourceA := filepath.Join(t.TempDir(), "a.bin")
	sourceB := filepath.Join(t.TempDir(), "b.bin")
	payloadA := bytes.Repeat([]byte("source A "), 1<<12)
	payloadB := bytes.Repeat([]byte("source B "), 1<<12)
	if len(payloadA) != len(payloadB) {
		t.Fatal("the sources must have the same size for this test")
	}
	if err := os.WriteFile(sourceA, payloadA, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceB, payloadB, 0o600); err != nil {
		t.Fatal(err)
	}
	partialA := filepath.Join(root, filepath.FromSlash(remotePartialPath("dest.bin", sourceA)))
	if err := os.WriteFile(partialA, payloadA[:16<<10], 0o600); err != nil {
		t.Fatal(err)
	}

	id, queued := worker.QueueUpload(sourceB, "dest.bin", false, ExistingResume)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Resumed != 0 || result.Bytes != int64(len(payloadB)) {
		t.Fatalf("upload from another source = %+v, want a fresh %d-byte send", result, len(payloadB))
	}
	final, err := os.ReadFile(filepath.Join(root, "dest.bin"))
	if err != nil || !bytes.Equal(final, payloadB) {
		t.Fatalf("destination = %d bytes, err %v, want source B", len(final), err)
	}
	if _, err := os.Stat(partialA); err != nil {
		t.Fatalf("the other source's partial was removed: %v", err)
	}
}
