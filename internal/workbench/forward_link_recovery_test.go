package workbench

import (
	"errors"
	"testing"
	"time"

	"motty/internal/linkwatch"
	"motty/internal/sshconn"
)

func addResumingForward(t *testing.T, state *State, label string) *forwardRecord {
	t.Helper()
	addForwardRule(t, state, label, freePort(t), "database.internal", 5432)
	state.Click(hitForwardRowEditList.At(int(state.selectedForwardID)))
	state.Click(hitForwardDialogResume)
	state.Click(hitForwardDialogSubmit)
	forward, ok := state.forwardByIDPointer(state.selectedForwardID)
	if !ok || !forward.Resume {
		t.Fatalf("forward after enabling resume: ok=%v forward=%+v", ok, forward)
	}
	return forward
}

func TestBackgroundForwardTunnelStartsLinkWatcherAndVerifiesOnLinkChange(t *testing.T) {
	state, world, watcher := linkTestState(t)
	forward := addResumingForward(t, state, "Database")

	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	if len(world.conns) != 1 || len(state.forwardTunnels) != 1 {
		t.Fatalf("dials=%d tunnels=%d, want 1 each", len(world.conns), len(state.forwardTunnels))
	}
	if starts, _, active := watcher.counts(); starts != 1 || active != 1 {
		t.Fatalf("watcher after starting tunnel: starts=%d active=%d, want 1/1", starts, active)
	}

	fake := world.conns[0]
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner == nil {
		t.Fatal("forward did not start over the tunnel")
	}

	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()
	waitForVerifies(t, fake, 1)

	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	state.Wake()
	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 1 {
		t.Fatalf("watcher after stopping tunnel: starts=%d stops=%d, want 1/1", starts, stops)
	}
	watcher.waitInactive(t)
}

func TestBackgroundForwardTunnelWaitingRetryRestartsOnNetworkRecovery(t *testing.T) {
	state, world, watcher := linkTestState(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectForever
	forward := addResumingForward(t, state, "Database")

	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	world.conns[0].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	world.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = errors.Join(sshconn.ErrDisconnected, errors.New("network lost"))
	})
	state.Wake()
	if !forward.pending || len(state.forwardTunnels) != 1 {
		t.Fatalf("forward after drop = %+v tunnels=%d", *forward, len(state.forwardTunnels))
	}
	tunnel := state.forwardTunnels[0]
	tunnel.retryAttempt = 5
	tunnel.retryDue = now + 30*time.Second
	if _, _, active := watcher.counts(); active != 1 {
		t.Fatalf("watcher active = %d while tunnel waits, want 1", active)
	}

	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()
	if len(world.conns) != 2 {
		t.Fatalf("dials after network recovery = %d, want 2", len(world.conns))
	}
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner == nil || forward.pending {
		t.Fatalf("forward after recovered dial = %+v", *forward)
	}
}

func TestBackgroundForwardTunnelRevivesAfterRetriesExhaustedWhenNetworkRecovers(t *testing.T) {
	state, world, watcher := linkTestState(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectThree
	forward := addResumingForward(t, state, "Database")

	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	world.conns[0].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	// Screen off / sleep: connection drops and all 3 retries fail while network is down.
	world.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = errors.Join(sshconn.ErrDisconnected, errors.New("network lost"))
	})
	state.Wake()
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		now += delay
		state.Wake()
		world.conns[len(world.conns)-1].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("network unreachable")
		})
		state.Wake()
	}

	if forward.runner != nil || forward.pending || len(state.forwardTunnels) != 0 {
		t.Fatalf("forward after exhausting retries = %+v tunnels=%d", *forward, len(state.forwardTunnels))
	}
	if _, _, active := watcher.counts(); active != 1 {
		t.Fatalf("watcher active = %d after exhausting retries, want 1", active)
	}

	// Screen wakes up and network comes back.
	dialedBefore := len(world.conns)
	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()
	if len(world.conns) != dialedBefore+1 || !forward.pending || len(state.forwardTunnels) != 1 {
		t.Fatalf("after network recovery: dials=%d want %d, forward=%+v tunnels=%d",
			len(world.conns), dialedBefore+1, *forward, len(state.forwardTunnels))
	}
	world.conns[len(world.conns)-1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner == nil || forward.pending {
		t.Fatalf("forward did not resume after tunnel reconnected: %+v", *forward)
	}
}

func TestSessionForwardRevivesAfterRetriesExhaustedWhenNetworkRecovers(t *testing.T) {
	state, world, watcher := linkTestState(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectThree

	connection := world.connectFirst(t, state)
	forward := addResumingForward(t, state, "Database")
	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	if forward.runner == nil || forward.sessionID != state.sessions[0].ID {
		t.Fatalf("started forward = %+v", *forward)
	}

	// Screen off / sleep: tab drops and exhausts all 3 retries.
	disconnectEstablished(connection, state)
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		now += delay
		state.Wake()
		world.conns[len(world.conns)-1].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("network unreachable")
		})
		state.Wake()
	}
	if !state.sessions[0].reconnectExhausted {
		t.Fatal("session retries did not exhaust")
	}
	if forward.runner != nil || forward.pending || forward.lastError != state.messages.ForwardConnectionClosed {
		t.Fatalf("forward after exhausted retries = %+v", *forward)
	}

	// Network returns: tab revives and reconnects, and the forward resumes.
	dialedBefore := len(world.conns)
	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()
	if !state.sessions[0].reconnectPending {
		t.Fatal("session did not revive on network recovery")
	}
	if !forward.pending || forward.sessionID != state.sessions[0].ID {
		t.Fatalf("forward did not re-attach to reviving session: %+v", *forward)
	}
	now += time.Second
	state.Wake()
	if len(world.conns) != dialedBefore+1 {
		t.Fatalf("dials = %d, want %d", len(world.conns), dialedBefore+1)
	}
	world.conns[len(world.conns)-1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if forward.runner == nil || forward.pending {
		t.Fatalf("forward did not resume after session reconnected: %+v", *forward)
	}
}

func TestForwardHeldByAReconnectRevivesWhenAStricterPolicyGivesUp(t *testing.T) {
	state, world, watcher := linkTestState(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectForever

	connection := world.connectFirst(t, state)
	forward := addResumingForward(t, state, "Database")
	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	if forward.runner == nil {
		t.Fatalf("started forward = %+v", *forward)
	}

	// The tab keeps retrying, so the forward waits for it.
	disconnectEstablished(connection, state)
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		now += delay
		state.Wake()
		world.conns[len(world.conns)-1].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("network unreachable")
		})
		state.Wake()
	}
	if !forward.pending || !state.sessions[0].reconnectPending {
		t.Fatalf("forward should wait for the retrying tab: %+v", *forward)
	}

	// A stricter policy gives the tab up; the forward stops with it.
	state.setReconnectPolicy(reconnectThree)
	if !state.sessions[0].reconnectExhausted || forward.pending {
		t.Fatalf("after the policy change: exhausted=%v forward=%+v", state.sessions[0].reconnectExhausted, *forward)
	}

	// The network returns: the tab revives and the forward comes back with it.
	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	state.Wake()
	if !state.sessions[0].reconnectPending || !forward.pending || forward.sessionID != state.sessions[0].ID {
		t.Fatalf("forward did not re-attach to the reviving tab: %+v", *forward)
	}
}

func TestLinkChangeDispatchesThroughPostWhenAvailable(t *testing.T) {
	world, options := newSharingWorld(t)
	watcher := &linkWatcher{}
	options.WatchNetworkChanges = watcher.start
	var posted []func()
	options.Post = func(fn func()) { posted = append(posted, fn) }
	state := newTestState(t, options)
	fake := world.connectFirst(t, state)
	posted = nil

	watcher.fire(linkwatch.Change{At: time.Now(), Up: true})
	if len(posted) != 1 {
		t.Fatalf("posted callbacks = %d, want 1", len(posted))
	}
	posted[0]()
	waitForVerifies(t, fake, 1)
}

func TestTurningReconnectOffClearsRevivingForwardAndStopsWatcher(t *testing.T) {
	state, world, watcher := linkTestState(t)
	now := 10 * time.Second
	state.options.Now = func() time.Duration { return now }
	state.connectionReconnect = reconnectThree
	forward := addResumingForward(t, state, "Database")

	state.Click(hitForwardRowToggleList.At(int(forward.ID)))
	world.conns[0].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	world.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = errors.Join(sshconn.ErrDisconnected, errors.New("network lost"))
	})
	state.Wake()
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		now += delay
		state.Wake()
		world.conns[len(world.conns)-1].set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("network unreachable")
		})
		state.Wake()
	}
	if _, _, active := watcher.counts(); active != 1 {
		t.Fatalf("watcher active = %d before turning reconnect off, want 1", active)
	}

	state.setReconnectPolicy(reconnectOff)
	if starts, stops, _ := watcher.counts(); starts != 1 || stops != 1 {
		t.Fatalf("watcher after turning reconnect off: starts=%d stops=%d, want 1/1", starts, stops)
	}
	watcher.waitInactive(t)
	if forward.reviveTunnel {
		t.Fatal("reviveTunnel remained set after turning reconnect off")
	}
}
