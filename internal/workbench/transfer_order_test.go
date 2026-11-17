package workbench

import (
	"slices"
	"testing"
	"time"

	"motty/internal/sftpclient"
)

func TestSFTPWindowQueueOrderAndOwnership(t *testing.T) {
	view := &sftpView{}
	w := &sftpWindow{owner: &State{}, view: view}
	center := &w.owner.transfers
	at := time.Unix(1000, 0)
	add := func(id int, owner *sftpView, state sftpclient.TransferState, queued, started, finished int, order uint64, conflict bool) {
		center.entries = append(center.entries, transferEntry{id: id, owner: owner, conflict: conflict,
			snapshot: sftpclient.TransferSnapshot{State: state, QueuedAt: at.Add(time.Duration(queued) * time.Second),
				StartedAt: at.Add(time.Duration(started) * time.Second), FinishedAt: at.Add(time.Duration(finished) * time.Second), QueueOrder: order}})
	}
	add(1, view, sftpclient.TransferCompleted, 1, 2, 20, 1, false)
	add(2, view, sftpclient.TransferQueued, 5, 0, 0, 9, false)
	add(3, view, sftpclient.TransferRunning, 2, 10, 0, 2, false)
	add(4, view, sftpclient.TransferFailed, 4, 5, 17, 3, false)
	add(5, view, sftpclient.TransferPaused, 5, 0, 0, 4, false)
	add(6, view, sftpclient.TransferRunning, 3, 8, 0, 5, false)
	add(7, view, sftpclient.TransferFailed, 0, 1, 9, 6, true)
	add(8, &sftpView{}, sftpclient.TransferRunning, 0, 11, 0, 7, false)
	check := func(want ...int) {
		t.Helper()
		w.queueCount()
		var got []int
		for _, i := range w.ui.queueEntries {
			got = append(got, center.entries[i].id)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
	center.revision++
	check(7, 3, 6, 5, 2, 1, 4)
	// A retry with a reused ID goes back to the end of the active queue.
	center.entries[6].snapshot.State = sftpclient.TransferQueued
	center.entries[6].snapshot.QueuedAt = at.Add(12 * time.Second)
	center.entries[6].conflict = false
	center.entries[0].removed = true
	center.revision++
	check(3, 6, 5, 2, 7, 4)
}
