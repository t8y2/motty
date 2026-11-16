package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gate holds the server's end of a protocol worker: once `after` bytes have gone
// through it the server stops reading until the gate is opened. The client's
// writes then block on the pipe, which pins an upload at a known point without
// depending on timing.
type gate struct {
	after   int64
	seen    atomic.Int64
	reached chan struct{}
	opened  chan struct{}
	reach   sync.Once
	open    sync.Once
}

func newGate(after int64) *gate {
	return &gate{after: after, reached: make(chan struct{}), opened: make(chan struct{})}
}

func (g *gate) Open() { g.open.Do(func() { close(g.opened) }) }

type gatedConn struct {
	net.Conn
	gate *gate
}

func (c gatedConn) Read(p []byte) (int, error) {
	if c.gate.seen.Load() >= c.gate.after {
		c.gate.reach.Do(func() { close(c.gate.reached) })
		<-c.gate.opened
	}
	n, err := c.Conn.Read(p)
	c.gate.seen.Add(int64(n))
	return n, err
}

// gatedWorker is newProtocolWorker with the gate between client and server.
func gatedWorker(t *testing.T, after int64) (*Worker, string, *gate) {
	t.Helper()
	g := newGate(after)
	worker, root := newProtocolWorkerOn(t, func(conn net.Conn) net.Conn { return gatedConn{Conn: conn, gate: g} }, nil)
	t.Cleanup(g.Open) // runs before the worker's own cleanup, which waits for the server
	return worker, root, g
}

func randomFile(t *testing.T, size int) (string, []byte) {
	t.Helper()
	content := make([]byte, size)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range content {
		content[i] = byte(random.Uint32())
	}
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, content
}

func waitSignal(t *testing.T, what string, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func resultWithin(t *testing.T, worker *Worker, limit time.Duration) Result {
	t.Helper()
	select {
	case result := <-worker.Results():
		return result
	case <-time.After(limit):
		t.Fatal("the worker did not finish in time")
		return Result{}
	}
}

func requireExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if (err == nil) != want {
		t.Fatalf("%s exists=%v (%v), want %v", path, err == nil, err, want)
	}
}

// The user giving up an upload removes what it left on the server, unless they
// chose to resume: then the partial file is what the resume continues from.
func TestCancelingAnUploadKeepsThePartialOnlyForResume(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy ExistingPolicy
		keep   bool
	}{
		{"fail", ExistingFail, false},
		{"replace", ExistingReplace, false},
		{"resume", ExistingResume, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			const size = 8 << 20
			worker, root, held := gatedWorker(t, 1<<20)
			local, content := randomFile(t, size)
			id, queued := worker.QueueUpload(local, "up.bin", false, test.policy)
			if !queued {
				t.Fatal("upload not queued")
			}
			waitSignal(t, "the upload to be under way", held.reached)
			if !worker.CancelTransfer(id) {
				t.Fatal("cancel refused")
			}
			held.Open()
			if result := resultWithin(t, worker, 10*time.Second); !errors.Is(result.Err, ErrCanceled) {
				t.Fatalf("result error = %v, want ErrCanceled", result.Err)
			}
			partial := filepath.Join(root, remotePartialPath("up.bin"))
			requireExists(t, filepath.Join(root, "up.bin"), false)
			requireExists(t, partial, test.keep)
			if !test.keep {
				return
			}
			kept, err := os.ReadFile(partial)
			if err != nil || len(kept) == 0 || len(kept) >= size || !bytes.Equal(kept, content[:len(kept)]) {
				t.Fatalf("kept partial: %d bytes, err %v, want a non-empty prefix of the source", len(kept), err)
			}
			if !worker.Retry(id, ExistingResume) {
				t.Fatal("retry refused")
			}
			result := resultWithin(t, worker, 10*time.Second)
			if result.Err != nil || result.Resumed != int64(len(kept)) {
				t.Fatalf("resume: err %v, resumed %d, want %d", result.Err, result.Resumed, len(kept))
			}
			if got, err := os.ReadFile(filepath.Join(root, "up.bin")); err != nil || !bytes.Equal(got, content) {
				t.Fatalf("resumed upload differs from the source (%d bytes, err %v)", len(got), err)
			}
			requireExists(t, partial, false)
		})
	}
}

// Cancel is about the connection, not a decision about the file: when it ends an
// upload the partial file stays for a retry, whatever the policy.
func TestLosingTheConnectionKeepsThePartial(t *testing.T) {
	worker, root, held := gatedWorker(t, 1<<20)
	local, _ := randomFile(t, 8<<20)
	if _, queued := worker.QueueUpload(local, "up.bin", false, ExistingReplace); !queued {
		t.Fatal("upload not queued")
	}
	waitSignal(t, "the upload to be under way", held.reached)
	// Cancel waits for the writes blocked on the pipe, which the gate holds.
	time.AfterFunc(100*time.Millisecond, held.Open)
	worker.Cancel()
	if result := resultWithin(t, worker, 10*time.Second); !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("result error = %v, want ErrCanceled", result.Err)
	}
	requireExists(t, filepath.Join(root, remotePartialPath("up.bin")), true)
}

// A link that no longer answers must not hold a canceled upload: its writes
// cannot finish, so after a grace period the connection is dropped under them.
func TestCancelingAnUploadOnAStalledLinkEnds(t *testing.T) {
	worker, _, held := gatedWorker(t, 1<<20)
	worker.cancelGrace = 50 * time.Millisecond
	local, _ := randomFile(t, 8<<20)
	id, queued := worker.QueueUpload(local, "up.bin", false, ExistingFail)
	if !queued {
		t.Fatal("upload not queued")
	}
	waitSignal(t, "the upload to be under way", held.reached)
	if !worker.CancelTransfer(id) {
		t.Fatal("cancel refused")
	}
	// The gate stays shut: only dropping the connection can end the writes.
	if result := resultWithin(t, worker, 10*time.Second); !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("result error = %v, want ErrCanceled", result.Err)
	}
}

// holeyRemote fails the write at one offset after later offsets have been
// written, which is what a concurrent upload leaves behind when a request fails
// while others are in flight.
type holeyRemote struct {
	remote
	failAt     atomic.Int64 // -1 when nothing fails
	beyond     atomic.Int32 // writes past failAt that have landed
	needBeyond int32
}

func (r *holeyRemote) OpenTransferFile(path string, flags int) (TransferFile, error) {
	file, err := r.remote.OpenTransferFile(path, flags)
	if err != nil {
		return nil, err
	}
	return &holeyFile{TransferFile: file, remote: r}, nil
}

type holeyFile struct {
	TransferFile
	remote *holeyRemote
}

var errInjected = errors.New("injected write failure")

func (f *holeyFile) WriteAt(p []byte, off int64) (int, error) {
	r := f.remote
	failAt := r.failAt.Load()
	if failAt >= 0 && off == failAt {
		deadline := time.Now().Add(10 * time.Second)
		for r.beyond.Load() < r.needBeyond && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		return 0, errInjected
	}
	n, err := f.TransferFile.(io.WriterAt).WriteAt(p, off)
	if failAt >= 0 && off > failAt && err == nil {
		r.beyond.Add(1)
	}
	return n, err
}

// After a failed write the partial file holds exactly the bytes known to be
// there: later chunks that landed are cut off, so a resume can verify it.
func TestFailedUploadLeavesNoGapInThePartial(t *testing.T) {
	const chunk = 32 * 1024
	holey := &holeyRemote{needBeyond: 3}
	holey.failAt.Store(10 * chunk)
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		holey.remote = base
		return holey
	})
	local, content := randomFile(t, 40*chunk)
	if _, queued := worker.QueueUpload(local, "up.bin", false, ExistingResume); !queued {
		t.Fatal("upload not queued")
	}
	result := resultWithin(t, worker, 20*time.Second)
	if !errors.Is(result.Err, errInjected) {
		t.Fatalf("result error = %v, want the injected failure", result.Err)
	}
	if holey.beyond.Load() < holey.needBeyond {
		t.Fatalf("only %d writes landed past the failure; the scenario did not happen", holey.beyond.Load())
	}
	partial := filepath.Join(root, remotePartialPath("up.bin"))
	kept, err := os.ReadFile(partial)
	if err != nil || len(kept) != 10*chunk || !bytes.Equal(kept, content[:len(kept)]) {
		t.Fatalf("partial: %d bytes, err %v, want exactly the %d bytes before the failure", len(kept), err, 10*chunk)
	}

	holey.failAt.Store(-1)
	id := worker.Transfers(nil)[0].ID
	if !worker.Retry(id, ExistingResume) {
		t.Fatal("retry refused")
	}
	result = resultWithin(t, worker, 20*time.Second)
	if result.Err != nil || result.Resumed != 10*chunk {
		t.Fatalf("resume: err %v, resumed %d, want %d", result.Err, result.Resumed, 10*chunk)
	}
	if got, err := os.ReadFile(filepath.Join(root, "up.bin")); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("resumed upload differs from the source (%d bytes, err %v)", len(got), err)
	}
}

// A canceled upload's tidying requests must not outlast a link that stopped
// answering: the transfer ends after the grace period and the subsystem is
// discarded, which is what frees the request.
func TestSettlingACanceledUploadDoesNotWaitOnAStalledLink(t *testing.T) {
	worker, _, _ := gatedWorker(t, 1<<20)
	worker.cancelGrace = 50 * time.Millisecond
	generation := generationOf(worker)
	stuck, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	finished := make(chan error, 1)
	go func() {
		finished <- worker.settleCanceled(generation, 0, func() error {
			close(stuck)
			<-release
			return errors.New("late")
		})
	}()
	waitSignal(t, "the cleanup request to start", stuck)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("a request that never answered contributed %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settling a canceled upload waited on the stalled request")
	}
	if generationOf(worker) == generation {
		t.Fatal("the stalled subsystem was not discarded")
	}
}

func generationOf(worker *Worker) uint64 {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return worker.generation
}
