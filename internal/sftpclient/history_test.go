package sftpclient

import (
	"path/filepath"
	"testing"
)

func TestTransferHistoryCannotForgetRunningCopyAndCanForgetEndedRecord(t *testing.T) {
	remote := newProgressRemote()
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	id, ok := worker.QueueDownload("large.bin", filepath.Join(t.TempDir(), "large.bin"), false, ExistingFail)
	if !ok {
		t.Fatal("not queued")
	}
	if worker.ForgetTransfer(id) {
		t.Fatal("forgot an active copy")
	}
	snapshot := worker.Transfers(nil)[0]
	if snapshot.QueuedAt.IsZero() || snapshot.StartedAt.IsZero() || !snapshot.FinishedAt.IsZero() {
		t.Fatalf("running timestamps=%+v", snapshot)
	}
	worker.CancelTransfer(id)
	waitResult(t, worker)
	snapshot = worker.Transfers(nil)[0]
	if snapshot.FinishedAt.IsZero() || snapshot.FinishedAt.Before(snapshot.StartedAt) {
		t.Fatalf("ended timestamps=%+v", snapshot)
	}
	if !worker.ForgetTransfer(id) || len(worker.Transfers(nil)) != 0 {
		t.Fatal("ended history did not clear")
	}
}
