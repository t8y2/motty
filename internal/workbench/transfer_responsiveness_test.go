package workbench

import (
	"testing"
	"time"

	"motty/internal/sftpclient"
)

func TestTransferSleepRequestDoesNotBlockUpdates(t *testing.T) {
	started, allow, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	claim := asyncSleepClaim{prevent: func(string) (func(), error) {
		close(started)
		<-allow
		return func() { close(released) }, nil
	}}
	returned := make(chan struct{})
	go func() { claim.hold(true); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		close(allow)
		t.Fatal("sleep prevention blocked transfer updates")
	}
	<-started
	claim.hold(false)
	close(allow)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("finished transfer retained the late sleep request")
	}
}

func TestTransferElapsedPreservesSubsecondDuration(t *testing.T) {
	state := newTestState(t, testOptions(t))
	started := time.Unix(1700000000, 0)
	for _, duration := range []time.Duration{230 * time.Millisecond, 3200 * time.Millisecond, 80 * time.Microsecond} {
		entry := transferEntry{snapshot: sftpclient.TransferSnapshot{
			State: sftpclient.TransferCompleted, StartedAt: started, FinishedAt: started.Add(duration),
		}}
		state.describeTransfer(&entry)
		if entry.elapsed != duration.String() {
			t.Errorf("elapsed = %q, want %q", entry.elapsed, duration.String())
		}
	}
}
