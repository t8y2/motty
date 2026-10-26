package workbench

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func reconnectingSession(t testing.TB, policy reconnectPolicy) (*State, *world, *fakeConn, *time.Duration) {
	t.Helper()
	world, options := newWorld(t)
	now := 10 * time.Second
	options.Now = func() time.Duration { return now }
	state := newTestState(t, options)
	state.connectionReconnect = policy
	connection := connect(t, state, world)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	return state, world, connection, &now
}

func disconnectEstablished(connection *fakeConn, state *State) {
	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = errors.Join(sshconn.ErrDisconnected, errors.New("network lost"))
	})
	state.Wake()
}

func TestAutomaticReconnectOnlyStartsForEstablishedUnexpectedDisconnects(t *testing.T) {
	t.Run("default off", func(t *testing.T) {
		state, _, connection, _ := reconnectingSession(t, reconnectOff)
		disconnectEstablished(connection, state)
		if state.sessions[0].reconnectPending {
			t.Fatal("default policy scheduled a reconnect")
		}
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		if _, scheduled := canvas.NextFrame(); scheduled {
			t.Fatal("disabled reconnect scheduled a frame")
		}
	})

	world, options := newWorld(t)
	now := 10 * time.Second
	options.Now = func() time.Duration { return now }
	state := newTestState(t, options)
	state.connectionReconnect = reconnectThree
	connection := connect(t, state, world)

	connection.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = sshconn.ErrDisconnected
	})
	state.Wake()
	if state.sessions[0].reconnectPending || len(world.conns) != 1 {
		t.Fatal("a connection that never established scheduled a retry")
	}

	state, world, connection, clock := reconnectingSession(t, reconnectThree)
	disconnectEstablished(connection, state)
	session := &state.sessions[0]
	if !session.reconnectPending || session.reconnectAttempt != 1 || session.reconnectDue != *clock+time.Second {
		t.Fatalf("reconnect = pending %v attempt %d due %v", session.reconnectPending, session.reconnectAttempt, session.reconnectDue)
	}
	if state.reconnectWaiters != 1 {
		t.Fatalf("reconnect waiters = %d, want 1", state.reconnectWaiters)
	}
	*clock += time.Second - time.Millisecond
	state.Wake()
	if len(world.conns) != 1 {
		t.Fatal("reconnect started before its deadline")
	}
	*clock += time.Millisecond
	state.Wake()
	if len(world.conns) != 2 || session.reconnectPending || session.reconnectAttempt != 1 {
		t.Fatalf("at deadline conns=%d pending=%v attempt=%d", len(world.conns), session.reconnectPending, session.reconnectAttempt)
	}
	if state.reconnectWaiters != 0 {
		t.Fatalf("reconnect waiters = %d after dial", state.reconnectWaiters)
	}
}

func TestAutomaticReconnectUsesBoundedBackoffAndAttemptLimit(t *testing.T) {
	state, world, connection, clock := reconnectingSession(t, reconnectThree)
	disconnectEstablished(connection, state)
	session := &state.sessions[0]

	for attempt, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if session.reconnectAttempt != attempt+1 || session.reconnectDue != *clock+delay {
			t.Fatalf("attempt %d due=%v at now=%v", session.reconnectAttempt, session.reconnectDue, *clock)
		}
		*clock += delay
		state.Wake()
		current := world.conns[len(world.conns)-1]
		current.set(func(status *sshconn.Status) {
			status.Phase = sshconn.PhaseFailed
			status.Err = errors.New("temporary network failure")
		})
		state.Wake()
	}
	if len(world.conns) != 4 || session.reconnectPending || !session.reconnectExhausted || session.reconnectAttempt != 3 {
		t.Fatalf("conns=%d pending=%v exhausted=%v attempt=%d", len(world.conns), session.reconnectPending, session.reconnectExhausted, session.reconnectAttempt)
	}
	if reconnectDelay(99) != 30*time.Second || !reconnectForever.permits(1_000_000) {
		t.Fatal("forever policy is not delay-bounded")
	}
}

func TestAutomaticReconnectStopsForCredentialAndHostKeyErrors(t *testing.T) {
	for name, terminalError := range map[string]error{
		"authentication":    sshconn.ErrAuthFailed,
		"changed host key":  sshconn.ErrHostKeyChanged,
		"rejected host key": sshconn.ErrHostKeyRejected,
	} {
		t.Run(name, func(t *testing.T) {
			state, world, connection, clock := reconnectingSession(t, reconnectForever)
			disconnectEstablished(connection, state)
			*clock += time.Second
			state.Wake()
			retry := world.conns[len(world.conns)-1]
			retry.set(func(status *sshconn.Status) {
				status.Phase = sshconn.PhaseFailed
				status.Err = terminalError
			})
			state.Wake()
			session := &state.sessions[0]
			if session.reconnectPending || session.reconnectAttempt != 0 {
				t.Fatalf("terminal error left reconnect %+v", session)
			}
		})
	}
}

func TestCancelCloseAndManualReconnectCancelScheduledAttempt(t *testing.T) {
	t.Run("cancel button", func(t *testing.T) {
		state, _, connection, _ := reconnectingSession(t, reconnectThree)
		disconnectEstablished(connection, state)
		canvas, texts := paintedTexts(state)
		if !contains(texts, "Reconnect attempt 1 in 0:01") || !hasHit(canvas, hitConnCancelReconnect) {
			t.Fatalf("countdown page texts=%q hits=%v", texts, canvas.Hits())
		}
		state.Click(hitConnCancelReconnect)
		if state.sessions[0].reconnectPending || !state.sessions[0].showLog {
			t.Fatal("cancel did not stop the plan and reveal its result")
		}
	})

	t.Run("close tab", func(t *testing.T) {
		state, _, connection, _ := reconnectingSession(t, reconnectForever)
		disconnectEstablished(connection, state)
		state.closeSession(state.sessions[0].ID)
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		if _, scheduled := canvas.NextFrame(); scheduled {
			t.Fatal("closed reconnecting tab left a delayed frame")
		}
	})

	t.Run("manual reconnect", func(t *testing.T) {
		state, world, connection, _ := reconnectingSession(t, reconnectForever)
		disconnectEstablished(connection, state)
		session := &state.sessions[0]
		state.Click(hitConnCancelReconnect)
		result := state.Key(ui.KeyEvent{Target: terminalHit(state, 0), Key: ui.KeyEnter})
		if len(world.conns) != 2 || session.reconnectPending || session.reconnectAttempt != 0 {
			t.Fatalf("manual reconnect result=%+v conns=%d pending=%v attempt=%d", result, len(world.conns), session.reconnectPending, session.reconnectAttempt)
		}
	})
}

func TestTurningReconnectOffCancelsWaitingSessions(t *testing.T) {
	state, _, connection, _ := reconnectingSession(t, reconnectForever)
	disconnectEstablished(connection, state)
	state.activeSection = sectionSettings
	state.Click(hitSettingsReconnectOff)
	if state.connectionReconnect != reconnectOff || state.sessions[0].reconnectPending {
		t.Fatalf("policy=%v pending=%v", state.connectionReconnect, state.sessions[0].reconnectPending)
	}
}

func TestReconnectWaitingPaintAllocatesNothingAndOnlyWaitersScheduleFrames(t *testing.T) {
	state, _, connection, _ := reconnectingSession(t, reconnectThree)
	disconnectEstablished(connection, state)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if delay, scheduled := canvas.NextFrame(); !scheduled || delay <= 0 || delay > time.Second {
		t.Fatalf("waiting reconnect frame = %v, %v", delay, scheduled)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("waiting reconnect paint allocations = %v", allocations)
	}
	state.Click(hitConnCancelReconnect)
	var idle ui.Canvas
	state.Paint(&idle, size)
	if _, scheduled := idle.NextFrame(); scheduled {
		t.Fatal("canceled reconnect kept scheduling frames")
	}
}

func BenchmarkReconnectDisabledFrameCheck(b *testing.B) {
	state := State{sessions: make([]Session, 32)}
	var canvas ui.Canvas
	b.ReportAllocs()
	for b.Loop() {
		state.requestReconnectFrames(&canvas)
	}
}

func TestTheReconnectCountdownNeverRunsUnderTheButtons(t *testing.T) {
	state, _, connection, _ := reconnectingSession(t, reconnectThree)
	disconnectEstablished(connection, state)
	for _, size := range []ui.Size{{Width: 1400, Height: 900}, {Width: 360, Height: 420}} {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		var countdown ui.Rect
		for _, text := range canvas.Texts() {
			if strings.HasPrefix(text.Value, "Reconnect attempt") {
				countdown = text.Bounds
			}
		}
		if countdown.Width == 0 {
			t.Fatalf("%v: the countdown is not on the page", size)
		}
		for _, id := range []ui.ID{hitConnLog, hitConnExport, hitConnClose, hitConnCancelReconnect} {
			hit, ok := hitByID(&canvas, id)
			if !ok {
				t.Fatalf("%v: button %d is missing", size, id)
			}
			if countdown.X+countdown.Width > hit.Bounds.X+0.5 && hit.Bounds.X+hit.Bounds.Width > countdown.X {
				t.Errorf("%v: countdown %+v runs under button %d at %+v", size, countdown, id, hit.Bounds)
			}
		}
	}
}
