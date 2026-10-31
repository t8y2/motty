package workbench

import (
	"testing"

	"motty/internal/sshconn"
)

// An SSH host's init script is typed into the shell once it has spoken, as if
// the user had typed it; a host without one sends nothing (#169).
func TestAnSSHHostsInitScriptIsTypedWhenTheShellSpeaks(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	conn := connect(t, state, world)
	conn.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-Test"
	})
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil || session.term.shell == nil {
		t.Fatal("the session has no shell to type into")
	}
	shell := conn.shell(t)
	session.host.InitScript = "cd /var/www"
	state.goLive(session)
	if got := shell.typed(); got != "cd /var/www\r" {
		t.Fatalf("the shell heard %q, want the init script", got)
	}
	// Going live again in the same connection does not type it twice.
	state.goLive(session)
	if got := shell.typed(); got != "cd /var/www\r" {
		t.Fatalf("the shell heard %q after going live again, want no more typing", got)
	}

	// A host without a script sends nothing.
	session.host.InitScript = ""
	session.term.initSent = false
	state.goLive(session)
	if got := shell.typed(); got != "cd /var/www\r" {
		t.Fatalf("the shell heard %q, want nothing more", got)
	}
}

// The init script runs again on a new connection: a reconnect is a new login.
func TestAnInitScriptIsTypedAgainOnAReconnectedTerminal(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	conn := connect(t, state, world)
	conn.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-Test"
	})
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no session")
	}
	session.host.InitScript = "cd /var/www"
	state.goLive(session)
	state.finishStartingConnection(session, false)
	if session.term.initSent {
		t.Fatal("the reconnected terminal already had the script sent")
	}
	shell := conn.shell(t)
	state.goLive(session)
	if got := shell.typed(); got != "cd /var/www\r" {
		t.Fatalf("the new shell heard %q, want the init script again", got)
	}
}
