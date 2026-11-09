package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// realTempDir is a fresh folder spelled the way a shell started in it reports
// it: macOS's temporary folders are aliases of /private ones.
func realTempDir(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

// closeShells ends the local shells a test started.
func closeShells(t *testing.T, state *State) {
	t.Cleanup(func() {
		for index := range state.sessions {
			if session := &state.sessions[index]; session.term != nil && session.term.shell != nil {
				session.term.shell.Close()
			}
		}
	})
}

// savedLocalTabs leaves local tabs saved in the given folders, with the restore
// policy and folder choice given, and returns the options a reopened app reads.
func savedLocalTabs(t *testing.T, policy restoreConnectPolicy, start localStartPolicy, directories ...string) Options {
	t.Helper()
	options := testOptions(t)
	first := newTestState(t, options)
	first.restoreConnect, first.localStart = policy, start
	entries := make([]savedSession, len(directories))
	for index, directory := range directories {
		entries[index] = savedSession{Local: true, LocalDirectory: directory}
	}
	first.restoreSessions(entries, 1)
	first.save()
	return options
}

func localDirOf(t *testing.T, session *Session) string {
	t.Helper()
	conn, ok := session.conn.(*localConn)
	if !ok {
		t.Fatalf("session %d has no local shell (restored %v)", session.ID, session.restored)
	}
	return conn.dir
}

// Reopening the app starts every local tab in the folder it was left in, even
// though the restore policy dials nothing: a local shell is not a connection.
func TestRememberedFolderStartsRestoredLocalTabsWhereTheyWere(t *testing.T) {
	skipWithoutUnixShell(t)
	directories := []string{realTempDir(t), realTempDir(t)}
	options := savedLocalTabs(t, restoreConnectOff, localStartRemember, directories...)

	state := newTestState(t, options)
	closeShells(t, state)
	if !state.restorePending {
		t.Fatal("the first Wake has nothing to do, so the restored local tabs would stay on their pages")
	}
	state.Wake()
	if len(state.sessions) != len(directories) {
		t.Fatalf("%d sessions restored, want %d", len(state.sessions), len(directories))
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.restored {
			t.Fatalf("local tab %d still shows its restored page", index)
		}
		if got := localDirOf(t, session); got != directories[index] {
			t.Fatalf("local tab %d starts in %q, want %q", index, got, directories[index])
		}
	}
}

// With the home folder chosen, a restored local tab still starts, whatever the
// restore policy says, but forgets where it was and starts at home.
func TestHomeFolderStartsRestoredLocalTabsAtHome(t *testing.T) {
	skipWithoutUnixShell(t)
	directory := realTempDir(t)

	for _, policy := range []restoreConnectPolicy{restoreConnectOff, restoreConnectAll} {
		state := newTestState(t, savedLocalTabs(t, policy, localStartHome, directory))
		closeShells(t, state)
		state.Wake()
		session := &state.sessions[0]
		if got := localDirOf(t, session); got != "" || session.localDirectory != "" || session.restored {
			t.Fatalf("restore policy %v: tab starts in %q (remembered %q, restored %v), want a started shell at home", policy, got, session.localDirectory, session.restored)
		}
	}
}

// A new local terminal starts in the folder of the local tab in front when the
// folder is remembered, and at home when the home folder is chosen.
func TestNewLocalTerminalStartsWhereTheLocalTabInFrontIs(t *testing.T) {
	state, first := openLocal(t)
	closeShells(t, state)
	directory := realTempDir(t)
	first.localDirectory = directory

	second := state.openLocalTerminal()
	if second == nil {
		t.Fatal("no second local terminal opened")
	}
	if got := localDirOf(t, second); got != directory || second.localDirectory != directory {
		t.Fatalf("new local terminal starts in %q (remembered %q), want %q", got, second.localDirectory, directory)
	}
	if state.pendingDirectory != "" {
		t.Fatalf("pending directory %q outlived the session it was for", state.pendingDirectory)
	}

	state.setLocalStartPolicy(localStartHome)
	third := state.openLocalTerminal()
	if got := localDirOf(t, third); got != "" {
		t.Fatalf("with the home folder chosen a new local terminal starts in %q", got)
	}
}

// The folder a local shell was last in outlives its tab and the app: a local
// terminal opened after a relaunch, with no local tab in front, starts there.
func TestLastLocalFolderOutlivesTheTabAndTheApp(t *testing.T) {
	skipWithoutUnixShell(t)
	options := testOptions(t)
	first := newTestState(t, options)
	first.restoreSessions([]savedSession{{Local: true}}, 1)
	session := &first.sessions[0]
	session.term = newTerminal(80, 24)
	first.configureTerminal(session)
	session.term.promptStarted, session.term.folderIntegrationSeen = true, true
	directory := realTempDir(t)
	first.terminalDirectory(session.ID, term.Cwd{Path: directory})
	if first.lastLocalDirectory != directory {
		t.Fatalf("last local folder = %q, want %q", first.lastLocalDirectory, directory)
	}
	first.closeSession(session.ID)
	first.Close()

	again := newTestState(t, options)
	closeShells(t, again)
	if again.lastLocalDirectory != directory {
		t.Fatalf("reopened with last local folder %q, want %q", again.lastLocalDirectory, directory)
	}
	opened := again.openLocalTerminal()
	if got := localDirOf(t, opened); got != directory {
		t.Fatalf("local terminal after a relaunch starts in %q, want %q", got, directory)
	}
}

// Closing the window makes the local tab in front the last place, rather than
// whichever background shell printed a prompt last.
func TestClosingRemembersTheLocalTabInFront(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	front, back := realTempDir(t), realTempDir(t)
	state.restoreSessions([]savedSession{{Local: true, LocalDirectory: front}, {Local: true, LocalDirectory: back}}, 1)
	state.activeSessionID = state.sessions[0].ID
	state.lastLocalDirectory = back
	state.Close()
	if again := newTestState(t, options); again.lastLocalDirectory != front {
		t.Fatalf("last local folder = %q, want the front tab's %q", again.lastLocalDirectory, front)
	}
}

func TestLocalStartSettingChoosesAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	if state.localStart != localStartRemember {
		t.Fatalf("default = %v, want remember", state.localStart)
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	if _, visible := hitByID(&canvas, hitSettingsLocalStartRemember); !visible {
		// The row sits with the local terminal settings at the terminal page's
		// end, past the first screenful: reach it the way a user does.
		state.scrollSettings(gfx.ScrollEvent{DeltaY: state.settingsContent, Precise: true})
		canvas.Reset()
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1800})
	}
	for _, id := range []gfx.ID{hitSettingsLocalStartRemember, hitSettingsLocalStartHome} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("settings paint no local folder choice %d", id)
		}
	}
	for _, test := range []struct {
		hit  gfx.ID
		want localStartPolicy
	}{{hitSettingsLocalStartHome, localStartHome}, {hitSettingsLocalStartRemember, localStartRemember}} {
		state.Click(test.hit)
		if state.localStart != test.want {
			t.Fatalf("clicking %d set %v, want %v", test.hit, state.localStart, test.want)
		}
		if again := newTestState(t, options); again.localStart != test.want {
			t.Fatalf("restored %v, want %v", again.localStart, test.want)
		}
	}
	next, ok := state.settingsKey(gfx.KeyEvent{Target: hitSettingsLocalStartRemember, Key: gfx.KeyRight})
	if !ok || !next.Handled || state.localStart != localStartHome {
		t.Fatalf("arrow key on the choice = %+v %v, policy %v", next, ok, state.localStart)
	}
	searchSettings(t, state, state.messages.SettingsLocalStart)
	if state.settings.count != 1 {
		t.Fatalf("search for the row returned %d results, want 1", state.settings.count)
	}
}
