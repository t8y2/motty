package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
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

func TestAShellOpeningDuringLayoutChangesReceivesTheLatestSize(t *testing.T) {
	state, slow := connectSlow(t)
	state.Wake()
	session := &state.sessions[0]
	if session.term.opening == nil {
		t.Fatal("the shell is not opening in the background")
	}

	// Layout can change while SSH is waiting for the server. The local screen
	// already has the new size when the shell eventually attaches.
	state.Resize(gfx.Size{Width: 1000, Height: 700})
	state.Resize(gfx.Size{Width: 1100, Height: 750})
	wantCols, wantRows := session.term.screen.Size()
	close(slow.release)
	deadline := time.Now().Add(2 * time.Second)
	for session.term.shell == nil && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if session.term.shell == nil {
		t.Fatal("the shell never attached once it opened")
	}
	shell := slow.shell(t)
	if config := slow.configs[0]; config.Cols == wantCols && config.Rows == wantRows {
		t.Fatal("fixture did not change the size while the shell was opening")
	}
	if len(shell.resizes) == 0 || shell.resizes[len(shell.resizes)-1] != [2]int{wantCols, wantRows} {
		t.Fatalf("attached shell resizes = %v, want latest size %dx%d", shell.resizes, wantCols, wantRows)
	}
	resizes := len(shell.resizes)
	state.Wake()
	if len(shell.resizes) != resizes {
		t.Fatal("an unchanged layout sent another resize")
	}
}
