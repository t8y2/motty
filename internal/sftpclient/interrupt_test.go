package sftpclient

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A connection that goes away is not the user giving the upload up: the worker
// keeps the state and the partial file so a worker on the same server can
// continue it (ADR 0028).
func TestInterruptKeepsAnUploadForTheNextConnection(t *testing.T) {
	const size = 8 << 20
	worker, root, held := gatedWorker(t, 1<<20)
	local, _ := randomFile(t, size)
	if _, queued := worker.QueueUpload(local, "up.bin", false, ExistingReplace); !queued {
		t.Fatal("upload not queued")
	}
	waitSignal(t, "the upload to be under way", held.reached)
	time.AfterFunc(100*time.Millisecond, held.Open)
	worker.Interrupt()
	worker.Close()
	select {
	case <-worker.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the interrupted worker did not close")
	}
	snapshots := worker.Transfers(nil)
	if len(snapshots) != 1 || snapshots[0].State != TransferInterrupted {
		t.Fatalf("transfer = %+v, want one interrupted upload", snapshots)
	}
	if snapshots[0].Error != ErrInterrupted.Error() || snapshots[0].Retryable {
		t.Fatalf("interrupted transfer reports %q retryable=%t", snapshots[0].Error, snapshots[0].Retryable)
	}
	partial := filepath.Join(root, remotePartialPath("up.bin", local))
	kept, err := os.ReadFile(partial)
	if err != nil || len(kept) == 0 || len(kept) >= size {
		t.Fatalf("partial: %d bytes, err %v, want the bytes moved before the link went", len(kept), err)
	}
}

// Interrupt is about the connection, never about a decision the user made.
func TestInterruptLeavesAUserCancellationCanceled(t *testing.T) {
	worker, _, held := gatedWorker(t, 1<<20)
	local, _ := randomFile(t, 8<<20)
	id, queued := worker.QueueUpload(local, "up.bin", false, ExistingFail)
	if !queued {
		t.Fatal("upload not queued")
	}
	waitSignal(t, "the upload to be under way", held.reached)
	time.AfterFunc(100*time.Millisecond, held.Open)
	if !worker.CancelTransfer(id) {
		t.Fatal("cancel refused")
	}
	if result := resultWithin(t, worker, 10*time.Second); !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("result error = %v, want ErrCanceled", result.Err)
	}
	worker.Interrupt()
	snapshots := worker.Transfers(nil)
	if len(snapshots) != 1 || snapshots[0].State != TransferCanceled {
		t.Fatalf("transfer = %+v, want the user's cancellation kept", snapshots)
	}
}

// A transfer that failed just before the connection was noticed is still an
// interrupted one: the failure is the reason to continue, not to forget it.
func TestInterruptCoversAFailureTheLostLinkCaused(t *testing.T) {
	const chunk = 32 * 1024
	holey := &holeyRemote{needBeyond: 3}
	holey.failAt.Store(10 * chunk)
	worker, root := newProtocolWorkerOn(t, nil, func(base remote) Remote {
		holey.remote = base
		return holey
	})
	local, _ := randomFile(t, 40*chunk)
	if _, queued := worker.QueueUpload(local, "up.bin", false, ExistingResume); !queued {
		t.Fatal("upload not queued")
	}
	if result := resultWithin(t, worker, 20*time.Second); !errors.Is(result.Err, errInjected) {
		t.Fatalf("result error = %v, want the injected failure", result.Err)
	}
	worker.Interrupt()
	snapshots := worker.Transfers(nil)
	if len(snapshots) != 1 || snapshots[0].State != TransferInterrupted {
		t.Fatalf("transfer = %+v, want the failed upload kept for a resume", snapshots)
	}
	requireExists(t, filepath.Join(root, remotePartialPath("up.bin", local)), true)
}

// A destination that already exists is a question for the user; a lost link
// must not answer it by continuing over the file.
func TestInterruptLeavesAConflictForTheUser(t *testing.T) {
	worker, root := newProtocolWorkerOn(t, nil, nil)
	if err := os.WriteFile(filepath.Join(root, "up.bin"), []byte("theirs"), 0o600); err != nil {
		t.Fatal(err)
	}
	local, _ := randomFile(t, 1<<20)
	if _, queued := worker.QueueUpload(local, "up.bin", false, ExistingFail); !queued {
		t.Fatal("upload not queued")
	}
	if result := resultWithin(t, worker, 20*time.Second); !errors.Is(result.Err, ErrDestinationExists) {
		t.Fatalf("result error = %v, want the conflict", result.Err)
	}
	worker.Interrupt()
	snapshots := worker.Transfers(nil)
	if len(snapshots) != 1 || snapshots[0].State != TransferFailed || !snapshots[0].Retryable {
		t.Fatalf("transfer = %+v, want the conflict left failed and retryable", snapshots)
	}
}
