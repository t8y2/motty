package workbench

import (
	"testing"

	"motty/internal/sftpclient"
	"motty/internal/zmodem"
)

// progressReport is what the desktop was told, and how often it was told it.
type progressReport struct {
	active   int
	fraction float32
	calls    int
}

func (report *progressReport) record(active int, fraction float32) {
	report.active, report.fraction = active, fraction
	report.calls++
}

// runningTransfer is one transfer a file panel is moving, as the transfer
// center keeps it.
func runningTransfer(id uint64, done, total int64) transferEntry {
	return transferEntry{id: int(id), snapshot: sftpclient.TransferSnapshot{
		ID: id, Operation: sftpclient.OperationDownload, State: sftpclient.TransferRunning,
		BytesDone: done, BytesTotal: total,
	}}
}

func TestTransferProgressAddsUpEveryWindowForOneIcon(t *testing.T) {
	group := NewGroup()
	var told progressReport
	group.SetTransferProgress(told.record)
	if told.calls != 1 || told.fraction >= 0 {
		t.Fatalf("idle: %d reports, fraction %v, want one report and no bar", told.calls, told.fraction)
	}
	first := newTestState(t, Options{Group: group})
	second := newTestState(t, Options{Group: group})

	first.transfers.entries = append(first.transfers.entries, runningTransfer(1, 100, 400))
	first.describeTransferSummary()
	if told.active != 1 || told.fraction != 0.25 {
		t.Fatalf("one window: active=%d fraction=%v, want 1 and 0.25", told.active, told.fraction)
	}
	// The second window's bytes are added to the first's: the icon stands for
	// the program, and the bar is the work of both.
	second.transfers.entries = append(second.transfers.entries, runningTransfer(2, 300, 600))
	second.describeTransferSummary()
	if told.active != 2 || told.fraction != 0.4 {
		t.Fatalf("two windows: active=%d fraction=%v, want 2 and 0.4", told.active, told.fraction)
	}
	// Numbers that have not moved do not reach the desktop again.
	calls := told.calls
	second.describeTransferSummary()
	if told.calls != calls {
		t.Fatal("an unchanged fraction reached the desktop again")
	}
	// A finished transfer leaves the bar to the one still running.
	second.transfers.entries[0].snapshot.State = sftpclient.TransferCompleted
	second.describeTransferSummary()
	if told.active != 1 || told.fraction != 0.25 {
		t.Fatalf("after one finished: active=%d fraction=%v, want 1 and 0.25", told.active, told.fraction)
	}
	// The last one finishing takes the bar away, rather than leaving it at its
	// end.
	first.transfers.entries[0].snapshot.State = sftpclient.TransferCompleted
	first.describeTransferSummary()
	if told.fraction >= 0 {
		t.Fatalf("fraction after the work ended = %v, want no bar", told.fraction)
	}
}

func TestTransferProgressCountsTheTerminalsOwnTransfer(t *testing.T) {
	group := NewGroup()
	var told progressReport
	group.SetTransferProgress(told.record)
	state := newTestState(t, Options{Group: group})
	// A transfer the terminal runs itself, with sz or rz, is not one of the
	// transfer center's, and is as much work as the panels' transfers.
	state.transfer = fileTransfer{
		phase:    transferRunning,
		progress: zmodem.Progress{Name: "release.tar.gz", Size: 2000, Done: 500},
	}
	state.reportTransferProgress()
	if told.active != 1 || told.fraction != 0.25 {
		t.Fatalf("terminal transfer: active=%d fraction=%v, want 1 and 0.25", told.active, told.fraction)
	}
	// A change the bar cannot draw is not sent.
	calls := told.calls
	state.transfer.progress.Done = 505
	state.reportTransferProgress()
	if told.calls != calls {
		t.Fatal("a fraction finer than the bar draws reached the desktop")
	}
	// A transfer whose size the other end has not announced has no bar: one at
	// its start would say the work had not begun.
	state.transfer.progress.Size = -1
	state.reportTransferProgress()
	if told.fraction >= 0 {
		t.Fatalf("fraction of an unannounced size = %v, want no bar", told.fraction)
	}
}
