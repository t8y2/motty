package workbench

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/sshconn"
)

// separateConnectionRemote is the fixture a file panel sees over its own
// connection: only that transport serves these files.
func separateConnectionRemote() *workbenchSFTPRemote {
	return &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {
				workbenchFileInfo{name: "alpha.txt", size: 5, mode: 0o644, when: time.Unix(1_700_000_000, 0)},
				workbenchFileInfo{name: "docs", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_100, 0)},
			},
		},
		files: map[string][]byte{"/home/test/alpha.txt": []byte("alpha")},
	}
}

// refusedSessionChannel is how a server that binds a connection to its first
// service answers a second session: the channel open itself is refused.
func refusedSessionChannel() error {
	return fmt.Errorf("sftp: start subsystem: %w", &ssh.OpenChannelError{Reason: ssh.Prohibited, Message: "open failed"})
}

// A server that allows one session channel per connection gives the terminal's
// transport to the shell. The file panel must not report that as broken: it
// opens an authenticated connection of its own, asks the same dialogs, lists the
// files there and ends that connection with the panel.
func TestFilePanelOpensItsOwnConnectionWhenTheServerRefusesAnotherSession(t *testing.T) {
	state, world, fake, _ := liveSession(t)
	fake.mu.Lock()
	fake.sftpErr = refusedSessionChannel()
	fake.mu.Unlock()
	state.Click(hitSFTPOpen)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil {
		t.Fatal("the file panel did not open")
	}

	waitUntil(t, state, "the panel's own connection", func() bool { return len(world.configs) == 2 })
	config := world.configs[1]
	if config.PrimaryShell || !config.NoProbes {
		t.Fatalf("the panel's connection = %+v, want no shell and no probes", config)
	}
	if config.Host != session.host.Address || config.User != state.login(session.host) {
		t.Fatalf("the panel's connection was dialed for %s@%s", config.User, config.Host)
	}
	dedicated := world.conns[1]

	// Its questions reach the ordinary dialogs, answered with the host's
	// account and vault like any other connection.
	dedicated.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhasePasswordPrompt
		status.Attempt = 1
		status.Questions = []sshconn.Question{{Secret: true}}
	})
	waitUntil(t, state, "the panel connection's password question", func() bool { return state.modal == modalPassword })
	if prompt := state.promptSession(); prompt == nil || prompt.ID != session.files.dedicated.session.ID {
		t.Fatalf("the password dialog belongs to %+v, want the panel's own connection", prompt)
	}
	state.passwordFields[0].Insert("secret")
	state.Click(hitPasswordSubmit)
	dedicated.mu.Lock()
	passwords := append([]string(nil), dedicated.passwords...)
	dedicated.mu.Unlock()
	if len(passwords) != 1 || passwords[0] != "secret" {
		t.Fatalf("the panel connection received %q, want the typed password", passwords)
	}

	dedicated.mu.Lock()
	dedicated.sftp = separateConnectionRemote()
	dedicated.mu.Unlock()
	dedicated.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "alpha.txt") >= 0
	})
	if !session.fileConnectionSeparate {
		t.Fatal("the session did not remember that its transport refused a session channel")
	}

	state.closeSFTP(session)
	dedicated.mu.Lock()
	canceled := dedicated.canceled
	dedicated.mu.Unlock()
	if canceled != 1 {
		t.Fatalf("the panel's connection was canceled %d times after the panel closed", canceled)
	}
}

// Once the server refused a session channel, opening the panel again must not
// ask the terminal's transport the same question: the panel reconnects for
// itself until the session dials a new terminal connection.
func TestFilePanelReconnectsSeparatelyWithoutAskingTheTerminalTransportAgain(t *testing.T) {
	state, world, fake, _ := liveSession(t)
	fake.mu.Lock()
	fake.sftpErr = refusedSessionChannel()
	fake.mu.Unlock()
	state.Click(hitSFTPOpen)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the panel's own connection", func() bool { return len(world.configs) == 2 })
	dedicated := world.conns[1]
	dedicated.mu.Lock()
	dedicated.sftp = separateConnectionRemote()
	dedicated.mu.Unlock()
	dedicated.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "alpha.txt") >= 0
	})
	fake.mu.Lock()
	terminalOpens := fake.sftpOpens
	fake.mu.Unlock()

	state.releaseIdleSFTP(session)
	state.openSFTP(session)
	waitUntil(t, state, "the reopened panel's own connection", func() bool { return len(world.configs) == 3 })
	fake.mu.Lock()
	after := fake.sftpOpens
	fake.mu.Unlock()
	if after != terminalOpens {
		t.Fatalf("the terminal transport was asked for files again: %d then %d opens", terminalOpens, after)
	}
	reopened := world.conns[2]
	reopened.mu.Lock()
	reopened.sftp = separateConnectionRemote()
	reopened.mu.Unlock()
	reopened.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "alpha.txt") >= 0
	})
	if view := session.files; view.dedicated == nil || !view.dedicated.ready {
		t.Fatal("the reopened panel is not using its own connection")
	}
}

// The panel's backend still reports an ordinary failure when the server is not
// the one refusing channels: no second connection is dialed for it.
func TestFilePanelDoesNotDialSeparatelyForOtherFailures(t *testing.T) {
	state, world, fake, _ := liveSession(t)
	fake.mu.Lock()
	fake.sftpErr = fmt.Errorf("sftp: start subsystem: %w", errors.New("connection reset by peer"))
	fake.mu.Unlock()
	state.Click(hitSFTPOpen)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
	if len(world.configs) != 1 {
		t.Fatalf("a failed file backend dialed %d connections, want the terminal's only", len(world.configs))
	}
}
