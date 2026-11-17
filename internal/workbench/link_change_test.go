package workbench

import (
	"errors"
	"sync"
	"testing"
	"time"

	"motty/internal/linkwatch"
	"motty/internal/sshconn"
)

// linkWatcher is the injected network watcher. It counts the watchers the
// workbench starts and stops, and holds the running watcher's report so a test
// can deliver a change the way the poller would. A started watcher mirrors the
// real one: it owns a goroutine until stop is called.
type linkWatcher struct {
	mu     sync.Mutex
	starts int
	stops  int
	active int
	report func(linkwatch.Change)
}

func (watch *linkWatcher) start(report func(linkwatch.Change)) func() {
	watch.mu.Lock()
	watch.starts++
	watch.active++
	watch.report = report
	watch.mu.Unlock()
	done := make(chan struct{})
	go func() {
		<-done
		watch.mu.Lock()
		watch.active--
		watch.mu.Unlock()
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			watch.mu.Lock()
			watch.stops++
			watch.report = nil
			watch.mu.Unlock()
			close(done)
		})
	}
}

// fire delivers a change through the running watcher's report.
func (watch *linkWatcher) fire(change linkwatch.Change) {
	watch.mu.Lock()
	report := watch.report
	watch.mu.Unlock()
	if report == nil {
		panic("linkWatcher.fire with no running watcher")
	}
	report(change)
}

func (watch *linkWatcher) counts() (starts, stops, active int) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	return watch.starts, watch.stops, watch.active
}

// waitInactive waits for every watcher goroutine to exit.
func (watch *linkWatcher) waitInactive(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, _, active := watch.counts(); active == 0 {
			return
		}
		if time.Now().After(deadline) {
			_, _, active := watch.counts()
			t.Fatalf("%d watcher goroutines still running after stop", active)
		}
		time.Sleep(time.Millisecond)
	}
}

// linkTestState is a window whose network path a test reports by hand.
func linkTestState(t *testing.T) (*State, *sharingWorld, *linkWatcher) {
	t.Helper()
	world, options := newSharingWorld(t)
	watcher := &linkWatcher{}
	options.WatchNetworkChanges = watcher.start
	return newTestState(t, options), world, watcher
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
	state, world, watcher := linkTestState(t)
	fake := world.connectFirst(t, state)

	watcher.fire(linkwatch.Change{At: time.Now()})
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

// The window used to poll the interfaces for its whole life. Nothing live means
// nothing to watch: an idle window must hold no watcher, not even after a Wake.
func TestAnIdleWindowDoesNotWatchTheNetwork(t *testing.T) {
	state, _, watcher := linkTestState(t)
	if starts, stops, active := watcher.counts(); starts != 0 || stops != 0 || active != 0 {
		t.Fatalf("idle window: starts=%d stops=%d active=%d", starts, stops, active)
	}
	state.Wake()
	if starts, _, _ := watcher.counts(); starts != 0 {
		t.Fatalf("idle window started %d watchers", starts)
	}
}

// A restored tab is metadata until it is dialed, and a shell on this computer
// has no network path: neither may keep the poller alive on its own.
func TestTabsWithNothingOnTheNetworkDoNotStartTheWatcher(t *testing.T) {
	state, _, watcher := linkTestState(t)
	state.sessions = append(state.sessions,
		Session{ID: 90, HostID: 7, phase: sshconn.PhaseClosed, restored: true},
		Session{ID: 91, HostID: localHostID, conn: &fakeConn{}, phase: sshconn.PhaseConnecting},
	)
	state.syncLinkWatch()
	if starts, _, _ := watcher.counts(); starts != 0 {
		t.Fatalf("tabs without a live network connection started %d watchers", starts)
	}
}

// The first live connection starts exactly one watcher, while it is still
// dialing, and later wake-ups and sessions do not start another.
func TestTheFirstLiveSessionStartsOneWatcher(t *testing.T) {
	state, world, watcher := linkTestState(t)
	openHost(state)
	if len(world.conns) != 1 {
		t.Fatalf("dials = %d, want one", len(world.conns))
	}
	if starts, _, _ := watcher.counts(); starts != 1 {
		t.Fatalf("a connecting session left %d watchers, want 1", starts)
	}
	fake := world.conns[0]
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-Test"
		status.ConnectionGeneration = 1
		status.HostKey.Fingerprint = "SHA256:test"
	})
	state.Wake()

	// A second live session is served by the same watcher.
	state.Click(hitHosts)
	state.selectedHostID = state.hosts[1].ID
	openHost(state)
	if len(world.conns) != 2 {
		t.Fatalf("dials = %d, want two", len(world.conns))
	}
	world.conns[1].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-Test"
		status.ConnectionGeneration = 2
		status.HostKey.Fingerprint = "SHA256:test"
	})
	state.Wake()
	state.Wake()

	if starts, _, active := watcher.counts(); starts != 1 || active != 1 {
		t.Fatalf("after two sessions: starts=%d active=%d, want one watcher", starts, active)
	}
}

// Closing one of two live sessions must not stop the watcher; the last one must.
func TestTheWatcherStopsOnlyWhenTheLastSessionCloses(t *testing.T) {
	state, world, watcher := linkTestState(t)
	world.connectFirst(t, state)
	state.Click(hitPaneSplitHorizontal)
	if len(state.sessions) != 2 {
		t.Fatalf("sessions = %d, want two", len(state.sessions))
	}

	state.closeSession(state.sessions[0].ID)
	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 0 {
		t.Fatalf("after closing one session: starts=%d stops=%d", starts, stops)
	}
	if len(state.sessions) != 1 {
		t.Fatalf("sessions = %d, want one left", len(state.sessions))
	}

	state.closeSession(state.sessions[0].ID)
	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 1 {
		t.Fatalf("after closing the last session: starts=%d stops=%d", starts, stops)
	}
	watcher.waitInactive(t)
}

// A connection that ends for good, with no retry left, leaves nothing to watch.
func TestAConnectionThatEndsForGoodStopsTheWatcher(t *testing.T) {
	state, world, watcher := linkTestState(t)
	fake := world.connectFirst(t, state)
	state.connectionReconnect = reconnectOff
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = sshconn.ErrDisconnected
	})
	state.Wake()

	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 1 {
		t.Fatalf("after a final disconnect: starts=%d stops=%d", starts, stops)
	}
	watcher.waitInactive(t)
}

// A retry waiting for the network is revived by a path change, so it counts as
// live: the watcher must keep running until the tab is done with it.
func TestAWaitingRetryKeepsTheWatcher(t *testing.T) {
	state, world, watcher := linkTestState(t)
	fake := world.connectFirst(t, state)
	state.connectionReconnect = reconnectForever
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = sshconn.ErrDisconnected
	})
	state.Wake()

	if !state.sessions[0].reconnectPending {
		t.Fatal("the failed connection did not schedule a retry")
	}
	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 0 {
		t.Fatalf("while a retry waits: starts=%d stops=%d", starts, stops)
	}
}

// The report the injected watcher holds still reaches the connection through
// the window's own Wake, which is the path a real Wi-Fi switch takes.
func TestAReportedChangeIsCheckedOnTheNextWake(t *testing.T) {
	state, world, watcher := linkTestState(t)
	fake := world.connectFirst(t, state)

	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()

	waitForVerifies(t, fake, 1)
}

// A window that ends every session watches nothing, and a later session starts
// the watcher again rather than reusing a dead one.
func TestAWindowWatchesAgainAfterEverySessionEnded(t *testing.T) {
	state, world, watcher := linkTestState(t)
	world.connectFirst(t, state)
	state.closeSession(state.sessions[0].ID)
	watcher.waitInactive(t)

	openHost(state)
	if len(world.conns) != 2 {
		t.Fatalf("dials = %d, want a new one", len(world.conns))
	}
	if starts, stops, active := watcher.counts(); starts != 2 || stops != 1 || active != 1 {
		t.Fatalf("after reconnecting: starts=%d stops=%d active=%d", starts, stops, active)
	}
}

// Closing the window ends every session it holds, including the watcher.
func TestClosingTheWindowStopsTheWatcher(t *testing.T) {
	state, world, watcher := linkTestState(t)
	world.connectFirst(t, state)

	state.Close()

	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 1 {
		t.Fatalf("after closing the window: starts=%d stops=%d", starts, stops)
	}
	watcher.waitInactive(t)
}
