package workbench

import (
	"slices"
	"testing"

	"motty/internal/sftpclient"
)

// TestTransferOrderPutsFailuresFirst pins the order every transfer list shows:
// failures, then running, then waiting or paused, then finished. Each section
// lists the newest entry first.
func TestTransferOrderPutsFailuresFirst(t *testing.T) {
	states := []sftpclient.TransferState{
		sftpclient.TransferCompleted, sftpclient.TransferQueued, sftpclient.TransferRunning,
		sftpclient.TransferFailed, sftpclient.TransferPaused, sftpclient.TransferRunning,
		sftpclient.TransferCanceled, sftpclient.TransferFailed,
	}
	entries := make([]transferEntry, len(states))
	for index, state := range states {
		entries[index] = transferEntry{id: index + 1, snapshot: sftpclient.TransferSnapshot{State: state}}
	}
	ids := func(list []*transferEntry) []int {
		var out []int
		for _, entry := range list {
			out = append(out, entry.id)
		}
		return out
	}
	got := ids(appendTransferOrder(nil, entries, func(*transferEntry) bool { return true }))
	if want := []int{8, 4, 6, 3, 5, 2, 7, 1}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	even := ids(appendTransferOrder(nil, entries, func(entry *transferEntry) bool { return entry.id%2 == 0 }))
	if want := []int{8, 4, 6, 2}; !slices.Equal(even, want) {
		t.Fatalf("filtered order = %v, want %v", even, want)
	}
}
