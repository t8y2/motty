package workbench

import (
	"path/filepath"
	"testing"

	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// reportDirectory has the shell of a session report its prompt in directory, as
// an integrated Bash or Zsh does before each prompt.
func reportDirectory(state *State, session *Session, shell *fakeShell, directory string) {
	shell.say(folderSyncMarker(session, "A") + "\x1b]7;file://box" + directory + "\x07" + folderSyncMarker(session, "B"))
	state.Wake()
}

// A pane split off a shell that has reported its directory asks the server to
// start the new shell there, so a second service need not be cd'ed to.
func TestSplittingStartsTheNewShellInTheDirectoryOfTheOne(t *testing.T) {
	state, world, _, shell := liveSession(t)
	reportDirectory(state, &state.sessions[0], shell, "/srv/app")

	state.splitWithCurrentHost(layoutcore.Horizontal)
	second := world.conns[1]
	second.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	second.shell(t)

	second.mu.Lock()
	defer second.mu.Unlock()
	if len(second.configs) != 1 || second.configs[0].StartDirectory != "/srv/app" {
		t.Fatalf("shell configs %+v, want one that starts in /srv/app", second.configs)
	}
}

// With nothing reported there is nothing to inherit: the new shell starts where
// a new shell does, and a later split is not given a stale directory.
func TestSplittingBeforeAnyReportStartsInTheDefaultDirectory(t *testing.T) {
	state, world, _, _ := liveSession(t)

	state.splitWithCurrentHost(layoutcore.Horizontal)
	second := world.conns[1]
	second.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	second.shell(t)

	second.mu.Lock()
	defer second.mu.Unlock()
	if len(second.configs) != 1 || second.configs[0].StartDirectory != "" {
		t.Fatalf("shell configs %+v, want one without a start directory", second.configs)
	}
	if state.pendingDirectory != "" {
		t.Fatalf("pending directory %q outlived the session it was for", state.pendingDirectory)
	}
}

// A local shell remembers its folder as process startup data, so a split takes
// the folder the first one reported as the one its own shell starts in.
func TestSplittingALocalShellStartsItInTheSameFolder(t *testing.T) {
	state, first := openLocal(t)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first.localDirectory = directory

	state.splitWithCurrentHost(layoutcore.Horizontal)
	if len(state.sessions) != 2 {
		t.Fatalf("%d sessions after a split", len(state.sessions))
	}
	second := &state.sessions[1]
	t.Cleanup(func() {
		if second.term != nil && second.term.shell != nil {
			second.term.shell.Close()
		}
	})
	conn, ok := second.conn.(*localConn)
	if !ok || conn.dir != directory || second.localDirectory != directory {
		t.Fatalf("second shell starts in %q (remembered %q), want %q", conn.dir, second.localDirectory, directory)
	}
}
