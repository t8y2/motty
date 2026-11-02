package workbench

import (
	"testing"
	"time"

	"motty/internal/sshconn"
)

// slowConn is a connection whose shell takes as long as the test says, as one
// over a distant network does.
type slowConn struct {
	*fakeConn
	release chan struct{}
}

func (*slowConn) opensShellSlowly() bool { return true }

func (conn *slowConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	<-conn.release
	return conn.fakeConn.OpenShell(config)
}

func connectSlow(t *testing.T) (*State, *slowConn) {
	t.Helper()
	world, options := newWorld(t)
	slow := &slowConn{release: make(chan struct{})}
	options.Dial = func(config sshconn.Config) Connection {
		slow.fakeConn = &fakeConn{}
		world.conns = append(world.conns, slow.fakeConn)
		return slow
	}
	woken := make(chan struct{}, 8)
	options.Invalidate = func() {
		select {
		case woken <- struct{}{}:
		default:
		}
	}
	state := newTestState(t, options)
	openHost(state)
	slow.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	return state, slow
}

func TestAShellOpenedOverTheNetworkDoesNotBlockTheWindow(t *testing.T) {
	state, slow := connectSlow(t)
	returned := make(chan struct{})
	go func() {
		state.Wake()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		close(slow.release)
		t.Fatal("Wake waited for the shell to open")
	}
	session := &state.sessions[0]
	if session.term.shell != nil || !state.showsProgress(session) {
		t.Fatal("the connection page should stay up while the shell is opening")
	}

	close(slow.release)
	deadline := time.Now().Add(2 * time.Second)
	for session.term.shell == nil && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if session.term.shell == nil {
		t.Fatal("the shell never attached once it opened")
	}
}

func TestClosingATabWhileItsShellOpensClosesTheShellWhenItArrives(t *testing.T) {
	state, slow := connectSlow(t)
	state.Wake()
	state.closeSession(state.sessions[0].ID)
	close(slow.release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		slow.fakeConn.mu.Lock()
		shells := slow.fakeConn.shells
		slow.fakeConn.mu.Unlock()
		if len(shells) == 1 {
			shells[0].mu.Lock()
			closed := shells[0].closed
			shells[0].mu.Unlock()
			if closed {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the shell opened for a closed tab was left open")
}
