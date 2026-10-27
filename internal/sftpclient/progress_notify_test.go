package sftpclient

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestProgressNotificationsAreBoundedWithoutDroppingBytesOrStateChanges(t *testing.T) {
	var notifications, tick atomic.Int64
	worker := NewWorker(func() (Remote, error) { return nil, nil }, func() { notifications.Add(1) })
	defer worker.Close()
	start := time.Unix(1_700_000_000, 0)
	worker.clock = func() time.Time { return start.Add(time.Duration(tick.Load())) }
	worker.transfers = []transferRecord{
		{snapshot: TransferSnapshot{ID: 1, State: TransferRunning, BytesTotal: 128 << 20}, active: true, rateAt: start},
		{snapshot: TransferSnapshot{ID: 2, State: TransferRunning, BytesTotal: 128 << 20}, active: true, rateAt: start},
	}
	// Both concurrent transfers share one window's progress wake budget.
	for step := int64(1); step <= 1000; step++ {
		tick.Store(int64(time.Microsecond) * step)
		worker.setProgress(1, 0, step*32*1024, 0)
		worker.setProgress(2, 0, step*32*1024, 0)
	}
	if n := notifications.Load(); n != 1 {
		t.Fatalf("2000 chunk updates posted %d wakes, want 1", n)
	}
	for _, snapshot := range worker.Transfers(nil) {
		if snapshot.BytesDone != 1000*32*1024 {
			t.Fatal("throttling lost transferred bytes")
		}
	}
	tick.Store(int64(2 * progressFrameInterval))
	worker.setProgress(1, 0, 1001*32*1024, 0)
	if notifications.Load() != 2 {
		t.Fatal("the next display frame was not notified")
	}
	worker.setProgress(1, 1, 1001*32*1024, 0)
	if notifications.Load() != 3 {
		t.Fatal("file boundary waited behind progress throttling")
	}
	worker.Pause(1)
	if notifications.Load() != 4 {
		t.Fatal("pause was delayed")
	}
	worker.Resume(1)
	if notifications.Load() != 5 {
		t.Fatal("resume was delayed")
	}
	worker.setProgress(1, 1, 128<<20, 0)
	if notifications.Load() != 6 {
		t.Fatal("the final byte count was delayed")
	}
	worker.finishTransfer(1, Result{Files: 1, Bytes: 128 << 20})
	if notifications.Load() != 7 || worker.Transfers(nil)[0].State != TransferCompleted {
		t.Fatal("completion was delayed")
	}
	worker.CancelTransfer(2)
	if notifications.Load() != 8 || worker.Transfers(nil)[1].State != TransferCanceled {
		t.Fatal("cancel was delayed")
	}
}

func TestSlowTransferStillNotifiesEachArrivingChunk(t *testing.T) {
	var notifications, tick atomic.Int64
	worker := NewWorker(func() (Remote, error) { return nil, nil }, func() { notifications.Add(1) })
	defer worker.Close()
	start := time.Unix(1_700_000_000, 0)
	worker.clock = func() time.Time { return start.Add(time.Duration(tick.Load())) }
	worker.transfers = []transferRecord{{snapshot: TransferSnapshot{ID: 1, State: TransferRunning}, rateAt: start}}
	for step := int64(1); step <= 10; step++ {
		tick.Store(int64(100*time.Millisecond) * step)
		worker.setProgress(1, 0, step*32*1024, 0)
	}
	if notifications.Load() != 10 {
		t.Fatalf("slow progress notifications=%d, want 10", notifications.Load())
	}
	worker.setProgress(1, 0, 10*32*1024, 0)
	if notifications.Load() != 10 {
		t.Fatal("unchanged progress scheduled an extra redraw")
	}
}
