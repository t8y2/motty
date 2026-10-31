package workbench

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/systemproxy"
)

func localShellOutput(t *testing.T, state *State, session *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.term != nil && strings.Contains(screenText(state, 0), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("local terminal never showed %q; screen: %q", want, screenText(state, 0))
}

func openLocal(t testing.TB) (*State, *Session) {
	t.Helper()
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no local session opened")
	}
	t.Cleanup(func() {
		if session.term != nil && session.term.shell != nil {
			session.term.shell.Close()
		}
	})
	return state, session
}

func TestLocalTerminalRunsAShellOnThisComputerWithoutDialing(t *testing.T) {
	skipWithoutUnixShell(t)
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitLocalTerminal)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if session == nil || !session.isLocal() {
		t.Fatalf("session = %+v", session)
	}
	if len(world.conns) != 0 {
		t.Fatalf("a local terminal dialed %d connections", len(world.conns))
	}
	if session.phase != sshconn.PhaseConnected && session.conn.Status().Phase != sshconn.PhaseConnected {
		t.Fatal("a local terminal is not connected from the start")
	}
	t.Cleanup(func() {
		if session.term != nil && session.term.shell != nil {
			session.term.shell.Close()
		}
	})
	state.Wake()
	if session.term == nil || session.term.shell == nil {
		t.Fatal("no shell was started")
	}
	session.term.send([]byte("echo local-$((20+22))\n"))
	localShellOutput(t, state, session, "local-42")
}

func TestLocalTerminalNetworkProxyEnvironment(t *testing.T) {
	direct := newLocalConn("", networkProxyDirect)
	direct.lookupSystemProxy = func() systemproxy.Settings {
		t.Fatal("direct mode read the system proxy")
		return systemproxy.Settings{}
	}
	if got, want := direct.shellConfig(sshconn.ShellConfig{}, "windows").Env, systemproxy.Environment(systemproxy.Settings{}); !slices.Equal(got, want) {
		t.Fatalf("direct environment = %q, want %q", got, want)
	}

	system := newLocalConn("", networkProxySystem)
	called := false
	proxy := systemproxy.Settings{HTTP: "http://127.0.0.1:10808", HTTPS: "http://127.0.0.1:10808", Bypass: "localhost"}
	system.lookupSystemProxy = func() systemproxy.Settings {
		called = true
		return proxy
	}
	if got, want := system.shellConfig(sshconn.ShellConfig{}, "windows").Env, systemproxy.Environment(proxy); !called || !slices.Equal(got, want) {
		t.Fatalf("system environment = %q, want %q; lookup called = %v", got, want, called)
	}
}

func TestLocalTerminalOffersNoRemoteOnlyTools(t *testing.T) {
	state, session := openLocal(t)
	state.Wake()
	session.term.live = true
	if _, _, offered := state.remoteStatsToggle(session); offered {
		t.Fatal("the resource monitor is offered for this computer")
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitSFTPOpen); !ok || hit.Disabled {
		t.Fatalf("Files should be offered for this computer's files: %+v, %v", hit, ok)
	}
}

func TestLocalTerminalBrowsesThisComputersFilesAndTypesPathsIntoTheShell(t *testing.T) {
	dir := localFixture(t)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	state, session := openLocal(t)
	state.Wake()

	state.Click(hitSFTPOpen)
	view := session.files
	if view == nil || !view.localOnly || !view.local || view.worker != nil {
		t.Fatalf("a local terminal's panel = %+v", view)
	}
	if view.lfolder.path != dir || len(view.lfolder.rows) != 3 || view.lfolder.rows[0].entry.Name != "photos" {
		t.Fatalf("the panel shows %q with %d rows", view.lfolder.path, len(view.lfolder.rows))
	}
	state.Wake() // pumping a panel without a server must not touch a worker

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitSFTPPaneToggle, hitSFTPUpload, hitSFTPBookmarkToggle} {
		if _, ok := hitByID(&canvas, contextID(id)); ok {
			t.Fatalf("hit %d is a server tool, shown for this computer", id)
		}
	}

	// A plain press selects a row; the panel then offers to type its path.
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 1, Target: contextID(hitSFTPEntryList.At(1))})
	canvas.Reset()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPInsertPath)); !ok {
		t.Fatal("no way to type the selected path into the terminal")
	}
	state.Click(contextID(hitSFTPInsertPath))
	localShellOutput(t, state, session, "ort.txt'") // the quoted path, wrapped by the screen

	// Going into a folder and back up.
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPEntryList.At(0))})
	if view.lfolder.path != filepath.Join(dir, "photos") {
		t.Fatalf("after opening a folder: %q", view.lfolder.path)
	}
	state.Click(contextID(hitSFTPUp))
	if view.lfolder.path != dir {
		t.Fatalf("after going up: %q", view.lfolder.path)
	}

	state.Click(contextID(hitSFTPClose))
	if session.files != nil {
		t.Fatal("closing the panel kept the list")
	}
}

func TestLocalTerminalIsSavedAndRestoredAsLocalNotAsAHost(t *testing.T) {
	skipWithoutUnixShell(t)
	_, options := newWorld(t)
	first := newTestState(t, options)
	first.Click(hitLocalTerminal)
	local, _ := first.sessionByIDPointer(first.activeSessionID)
	t.Cleanup(func() {
		if local.term != nil && local.term.shell != nil {
			local.term.shell.Close()
		}
	})
	first.save()

	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sessions) != 1 || !saved.Sessions[0].Local || saved.Sessions[0].Address != "" {
		t.Fatalf("saved sessions = %+v", saved.Sessions)
	}

	second := newTestState(t, options)
	if len(second.sessions) != 1 || !second.sessions[0].isLocal() || !second.sessions[0].restored {
		t.Fatalf("restored sessions = %+v", second.sessions)
	}
	if second.sessions[0].term != nil && second.sessions[0].term.shell != nil {
		t.Fatal("restoring started a shell before it was asked for")
	}
}

func TestLocalTerminalReconnectsWithANewShellAfterItEnds(t *testing.T) {
	state, session := openLocal(t)
	state.Wake()
	session.term.send([]byte("exit 0\n"))
	deadline := time.Now().Add(5 * time.Second)
	for session.term.shell != nil && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(5 * time.Millisecond)
	}
	if session.term.shell != nil {
		t.Fatal("the shell did not end")
	}
	if !state.canReconnect(session) {
		t.Fatal("an ended local terminal cannot be started again")
	}
	state.reconnect(session)
	// A local shell can fail to start (a sandbox without pty access, for one);
	// wait for the attempt instead of assuming it succeeded by the next Wake, and
	// say why if it did not.
	deadline = time.Now().Add(5 * time.Second)
	for session.term.shell == nil && session.err == nil && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(5 * time.Millisecond)
	}
	if session.term.shell == nil {
		t.Fatalf("reconnecting did not start a new shell: %v", session.err)
	}
	session.term.send([]byte("echo again-$((1+1))\n"))
	localShellOutput(t, state, session, "again-2")
}

func TestLocalTerminalHasAFixedEntryInTheSidebarAndTheCommandPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for _, size := range []ui.Size{{Width: 1000, Height: 700}, {Width: 1400, Height: 900}, {Width: 1800, Height: 1000}} {
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, hitLocalTerminal)
		if !ok || hit.Bounds.Y+hit.Bounds.Height > size.Height {
			t.Fatalf("at %v the sidebar's Local Terminal entry is missing or off screen: %+v, %v", size, hit, ok)
		}
	}
	state.openCommandPalette()
	found := false
	palette := &state.commandPalette
	for index := 0; index < int(palette.entryCount); index++ {
		if palette.entries[index].target == hitLocalTerminal {
			found = true
		}
	}
	if !found {
		t.Fatal("the command palette does not offer Local Terminal")
	}
}

// skipWithoutUnixShell makes the tests that type POSIX commands run a POSIX
// shell, and leaves them out where there is none (Windows has its own tests in
// the localshell package, which can name cmd.exe).
func skipWithoutUnixShell(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these tests type POSIX shell commands")
	}
	t.Setenv("SHELL", "/bin/sh")
}

// The keyboard shortcut opens a shell on this computer like the sidebar,
// window bar and tray do.
func TestLocalTerminalShortcutOpensAShell(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	chord := state.shortcuts.chords[shortcutLocalTerminal]
	if !chord.valid() {
		t.Fatalf("the local terminal shortcut is not a valid chord: %+v", chord)
	}
	result := state.Shortcut(ui.KeyEvent{Modifiers: chord.Modifiers, Text: string([]byte{chord.Key})})
	if !result.Handled || !result.Repaint {
		t.Fatalf("shortcut result = %+v", result)
	}
	session, found := state.sessionByIDPointer(state.activeSessionID)
	if !found || !session.isLocal() {
		t.Fatalf("session after the shortcut = %+v", session)
	}
	t.Cleanup(func() {
		if session.term != nil && session.term.shell != nil {
			session.term.shell.Close()
		}
	})
	if state.activeSection != sectionTerminal || result.Focus != state.activeTerminalFocusHit() {
		t.Fatalf("the shortcut left section %d and focus %v, want the new terminal", state.activeSection, result.Focus)
	}
}

func TestWindowBarHasALocalTerminalButtonThatOpensAShell(t *testing.T) {
	skipWithoutUnixShell(t)
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitWindowLocalTerminal)
	if !ok || hit.Bounds.Y >= windowBarHeight || hit.Label != state.messages.LocalTerminal {
		t.Fatalf("window bar local terminal hit = %+v, ok %v", hit, ok)
	}
	state.Click(hitWindowLocalTerminal)
	session, found := state.sessionByIDPointer(state.activeSessionID)
	if !found || !session.isLocal() {
		t.Fatalf("session = %+v", session)
	}
	t.Cleanup(func() {
		if session.term != nil && session.term.shell != nil {
			session.term.shell.Close()
		}
	})
}
