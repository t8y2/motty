package workbench

import (
	"errors"
	"testing"
	"time"

	"motty/internal/linkwatch"
	"motty/internal/sshconn"
)

// linkTestState is a window whose network path a test reports by hand.
func linkTestState(t *testing.T) (*State, *sharingWorld, func(linkwatch.Change)) {
	t.Helper()
	world, options := newSharingWorld(t)
	var report func(linkwatch.Change)
	options.WatchNetworkChanges = func(r func(linkwatch.Change)) func() {
		report = r
		return func() {}
	}
	state := newTestState(t, options)
	if report == nil {
		t.Fatal("the window did not watch the network path")
	}
	return state, world, report
}

// waitForVerifies waits for the link checks a change starts, which run on their
// own goroutines.
func waitForVerifies(t *testing.T, fake *fakeConn, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for fake.verifyCount() < want {
		if time.Now().After(deadline) {
			t.Fatalf("link checks = %d, want %d", fake.verifyCount(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// The transport was bound to addresses that are gone, so the connection has to
// be asked: no FIN was sent, and the next keepalive tick is an interval away.
func TestALinkChangeAsksEveryEstablishedConnection(t *testing.T) {
	state, world, _ := linkTestState(t)
	fake := world.connectFirst(t, state)

	state.linkChanged(linkwatch.Change{At: time.Now()})
	state.applyLinkChange()

	waitForVerifies(t, fake, 1)
}

func TestALinkChangeIsActedOnOnce(t *testing.T) {
	state, world, report := linkTestState(t)
	fake := world.connectFirst(t, state)

	report(linkwatch.Change{At: time.Now()})
	state.applyLinkChange()
	state.applyLinkChange()
	state.applyLinkChange()

	waitForVerifies(t, fake, 1)
	time.Sleep(20 * time.Millisecond)
	if got := fake.verifyCount(); got != 1 {
		t.Fatalf("link checks = %d for one change, want 1", got)
	}
}

// A connection that answers is left alone: the answer changed nothing, so no
// reconnect may start from a Wi-Fi switch that the server never noticed.
func TestAConnectionThatStillAnswersKeepsItsSession(t *testing.T) {
	state, world, _ := linkTestState(t)
	fake := world.connectFirst(t, state)
	state.connectionReconnect = reconnectForever

	state.linkChanged(linkwatch.Change{At: time.Now()})
	state.applyLinkChange()
	waitForVerifies(t, fake, 1)

	session := &state.sessions[0]
	if session.reconnectPending || session.phase != sshconn.PhaseConnected {
		t.Fatalf("session after an answered check: pending=%v phase=%v", session.reconnectPending, session.phase)
	}
}

// A connection that no longer answers ends the way a keepalive timeout ends, so
// the retry policy takes over without a second path to it.
func TestAConnectionThatStopsAnsweringIsRetried(t *testing.T) {
	state, world, _ := linkTestState(t)
	fake := world.connectFirst(t, state)
	fake.verifyErr = errors.New("link changed: i/o timeout")
	state.connectionReconnect = reconnectForever

	state.linkChanged(linkwatch.Change{At: time.Now()})
	state.applyLinkChange()
	waitForVerifies(t, fake, 1)

	// The real Verify records the disconnect on the transport, which ends it;
	// the fake answers the check without ending anything, so apply the ending
	// the way the connection goroutine would.
	state.updateReconnect(&state.sessions[0], sshconn.Status{Phase: sshconn.PhaseClosed, Err: sshconn.ErrDisconnected})
	if !state.sessions[0].reconnectPending {
		t.Fatal("a failed link check did not schedule a retry")
	}
}

func TestARecoveredNetworkRestartsAWaitingRetry(t *testing.T) {
	state, world, _ := linkTestState(t)
	world.connectFirst(t, state)
	var now time.Duration
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectForever
	state.updateReconnect(&state.sessions[0], sshconn.Status{Phase: sshconn.PhaseClosed, Err: sshconn.ErrDisconnected})

	session := &state.sessions[0]
	if !session.reconnectPending {
		t.Fatal("no retry was scheduled")
	}
	// A retry that has backed off to the longest delay: the network coming back
	// must not leave it waiting.
	session.reconnectAttempt = 5
	session.reconnectDue = 30 * time.Second

	state.linkChanged(linkwatch.Change{Up: true})
	state.applyLinkChange()

	if session.reconnectAttempt != 0 {
		t.Fatalf("attempt after the network came back = %d, want 0", session.reconnectAttempt)
	}
	if session.reconnectDue != now {
		t.Fatalf("due = %v, want now (%v)", session.reconnectDue, now)
	}
}

func TestARecoveredNetworkRevivesARetryThatGaveUp(t *testing.T) {
	state, world, _ := linkTestState(t)
	world.connectFirst(t, state)
	state.connectionReconnect = reconnectThree
	state.updateReconnect(&state.sessions[0], sshconn.Status{Phase: sshconn.PhaseClosed, Err: sshconn.ErrDisconnected})

	session := &state.sessions[0]
	// The three attempts the policy allows are spent without the network
	// returning: the tab gives up, and only a change of path may ask again.
	session.reconnectAttempt = 3
	session.reconnectPending = false
	session.reconnectExhausted = true
	state.reconnectWaiters = 0

	state.linkChanged(linkwatch.Change{Up: true})
	state.applyLinkChange()

	if !session.reconnectPending || session.reconnectAttempt != 1 || session.reconnectExhausted {
		t.Fatalf("after the network came back: pending=%v attempt=%d exhausted=%v",
			session.reconnectPending, session.reconnectAttempt, session.reconnectExhausted)
	}
}

func TestAChangeWithNoNetworkLeftSchedulesNothing(t *testing.T) {
	state, world, _ := linkTestState(t)
	world.connectFirst(t, state)
	state.connectionReconnect = reconnectForever
	state.updateReconnect(&state.sessions[0], sshconn.Status{Phase: sshconn.PhaseClosed, Err: sshconn.ErrDisconnected})

	session := &state.sessions[0]
	session.reconnectAttempt = 5
	session.reconnectDue = 30 * time.Second

	// Losing the network is what started the backoff: it must not reset it.
	state.linkChanged(linkwatch.Change{Up: false})
	state.applyLinkChange()

	if session.reconnectAttempt != 5 || session.reconnectDue != 30*time.Second {
		t.Fatalf("a lost network reset the backoff: attempt=%d due=%v", session.reconnectAttempt, session.reconnectDue)
	}
}
