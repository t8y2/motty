package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReverseFolderSyncIsOffByDefaultAndPersistsIndependently(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	if state.sftpTerminalFollow {
		t.Fatal("reverse sync defaulted on")
	}
	state.Click(hitSettingsTerminalFollowToggle)
	state.Click(hitSettingsFollowToggle)
	restarted := newTestState(t, state.options)
	if !restarted.sftpTerminalFollow || !restarted.sftpFollowOff {
		t.Fatal("the two independent preferences did not survive restart")
	}
	if len(folderSyncInput(fake.shell(t))) != 0 || session.files.path != "/home/test" {
		t.Fatal("changing settings performed a navigation")
	}
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 1100})
	if _, ok := hitByID(&canvas, hitSettingsTerminalFollowToggle); !ok {
		t.Fatal("reverse sync has no visible settings switch")
	}
}

func TestFolderSyncCanOptOutOfShellIntegration(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.sftpFollowOff, state.sftpTerminalFollow = true, false
	session.term.shell.Close()
	session.term.shell = nil
	state.openShell(session)
	fake.mu.Lock()
	config := fake.configs[len(fake.configs)-1]
	fake.mu.Unlock()
	if config.IntegrateShell {
		t.Fatal("both synchronization directions are off, but startup still changes")
	}
}

func TestReverseFolderSyncRejectsNestedShellMarkers(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	session.term.send([]byte("ssh elsewhere\r"))
	shell.say("\x1b]133;A\a\x1b]7;file://elsewhere/another-host\a\x1b]133;B\a")
	state.Wake()
	if session.files.busy || session.term.promptReady {
		t.Fatal("a nested shell took over the outer SSH file panel or readiness")
	}
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "" {
		t.Fatal("a nested login accepted an outer-server cd")
	}
}

func TestReverseFolderSyncRejectsCompleteSubmittedTypeahead(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	session.term.send([]byte("sleep 1\rread value\r"))
	// The shell finishes sleep and briefly prints a prompt before consuming
	// the complete read command that is already in its input queue.
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "" {
		t.Fatal("an intermediate prompt authorized a cd with a complete command still queued")
	}
	shell.say(folderSyncMarker(session, "C")) // read is now actually running
	state.Wake()
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "" {
		t.Fatal("a running read accepted an automatic cd as its input")
	}
}

func TestReverseFolderSyncRequiresCommandStartReportingAndCanClearInputDebt(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "" {
		t.Fatal("a shell without command-start reporting authorized reverse sync")
	}
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	session.term.send([]byte("read value\r"))
	shell.say(folderSyncMarker(session, "C"))
	state.Wake()
	session.term.send([]byte("program input\r"))
	shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	if session.term.promptReady {
		t.Fatal("unacknowledged program input was treated as an empty shell line")
	}
	session.term.send([]byte{3}) // explicit user interrupt at an empty prompt
	shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "/home/test/docs" {
		t.Fatal("an explicit input reset could not restore verified prompt readiness")
	}
}

func TestReverseFolderSyncReconnectRequiresFreshShellAuthorization(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + "\x1b]7;file://box/home/test/docs\a" + folderSyncMarker(session, "B"))
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	oldToken := session.term.folderToken
	shell.Close()
	state.closeSFTP(session)
	state.finishStartingConnection(session, false)
	session.phase = sshconn.PhaseConnected
	state.openShell(session)
	state.openSFTP(session)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if session.term.folderToken == oldToken || session.term.promptReady || session.term.promptDirty || session.term.followPath != "" {
		t.Fatal("reconnect retained old shell authorization or pending state")
	}
	if _, ok := session.term.screen.WorkingDirectory(); ok {
		t.Fatal("reconnect retained the old shell working directory")
	}
	state.requestTerminalDirectory(session.files, "/home/test/docs")
	if session.files.terminalPath != "" {
		t.Fatal("a shell still starting received an automatic cd")
	}
}

func TestReverseFolderSyncCannotExecuteAfterTypingDuringListing(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	remote.mu.Lock()
	remote.readDelay = 30 * time.Millisecond
	remote.mu.Unlock()
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	session.term.send([]byte("unfinished"))
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if got := folderSyncInput(shell); got != "unfinished" {
		t.Fatalf("late cd corrupted the input line: %q", got)
	}
	if session.files.status != state.messages.Tools.FilesTerminalFollowSkipped {
		t.Fatal("unsafe navigation was skipped without feedback")
	}
}

// A shell that reports its prompt but never the start of a command cannot be
// proven idle. The panel must say that, not ask the user
// to finish a command that is not running.
func TestReverseFolderSyncExplainsAShellThatCannotReportCommandStarts(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if got := folderSyncInput(shell); got != "" {
		t.Fatalf("a terminal that could not be proven idle received %q", got)
	}
	if session.files.status != state.messages.Tools.FilesTerminalFollowUnsupported {
		t.Fatalf("status = %q", session.files.status)
	}
}

type folderSyncErrorRemote struct{ *workbenchSFTPRemote }

func (r folderSyncErrorRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	if directory == "/home/test/docs" {
		return nil, os.ErrNotExist
	}
	return r.workbenchSFTPRemote.ReadDir(directory)
}

func TestReverseFolderSyncFailedPrefetchCannotLeaveACommandQueued(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	state.closeSFTP(session)
	fake.mu.Lock()
	fake.sftp = folderSyncErrorRemote{remote}
	fake.mu.Unlock()
	state.openSFTP(session)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && len(v.rows) == 2 })
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	clickSFTPRow(t, state, session.files, "docs") // starts a prefetch
	state.Click(hitSFTPOpenEntry)                 // joins that same read
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.errorText != "" })
	if got := folderSyncInput(shell); got != "" || session.files.terminalPath != "" {
		t.Fatalf("failed listing left a cd queued: input=%q pending=%q", got, session.files.terminalPath)
	}
}

func TestReverseFolderSyncValidatesCachedDirectoriesBeforeExecuting(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	state.closeSFTP(session)
	fake.mu.Lock()
	fake.sftp = folderSyncErrorRemote{remote}
	fake.mu.Unlock()
	state.openSFTP(session)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && len(v.rows) == 2 })
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	session.files.remember("/home/test/docs", nil) // an old successful reading
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	if got := folderSyncInput(shell); got != "" {
		t.Fatalf("an unvalidated cached directory executed %q", got)
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.errorText != "" })
	if got := folderSyncInput(shell); got != "" {
		t.Fatalf("a removed directory executed %q", got)
	}
}

func TestReverseFolderSyncUsesTheOwningSessionNotTheActiveTab(t *testing.T) {
	state, original, fake, _ := openTestSFTP(t)
	id := original.ID
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(original, "C") + folderSyncMarker(original, "A") + folderSyncMarker(original, "B"))
	state.Wake()
	otherShell := &fakeShell{done: make(chan struct{})}
	other := Session{ID: id + 1000, host: original.host, phase: sshconn.PhaseConnected, term: newTerminal(80, 24)}
	other.term.shell = otherShell
	state.sessions = append(state.sessions, other)
	state.activeSessionID = other.ID
	session, _ := state.sessionByIDPointer(id)
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if got := folderSyncInput(shell); got != "builtin cd -- '/home/test/docs'\r" {
		t.Fatalf("owning terminal received %q", got)
	}
	if got := folderSyncInput(otherShell); got != "" {
		t.Fatalf("active unrelated terminal received %q", got)
	}
}

func folderSyncInput(shell *fakeShell) string {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return string(shell.input)
}

func TestReverseFolderSyncExcludesStandaloneAndInvalidPaths(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	view := session.files
	for _, directory := range []string{"relative", "/tmp/bad\npath", "/tmp/bad\x00path", "/tmp/bad\x7fpath"} {
		state.requestTerminalDirectory(view, directory)
		if view.terminalPath != "" {
			t.Fatalf("unsafe directory %q queued", directory)
		}
	}
	view.detached = &sftpWindow{}
	state.requestTerminalDirectory(view, "/home/test/docs")
	view.detached = nil
	if view.terminalPath != "" || folderSyncInput(shell) != "" {
		t.Fatal("a standalone browser controlled a terminal")
	}
}

func TestReverseFolderSyncSendsOneQuotedCommandAfterExplicitNavigation(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.Click(hitSettingsTerminalFollowToggle)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	view := session.files
	target := "/home/test/a 'b%#;$(touch nope)"
	*view.pathInput() = *text.NewEditor(target)
	state.submitSFTPPath(view)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == target })
	want := "builtin cd -- '/home/test/a '\\''b%#;$(touch nope)'\r"
	if got := folderSyncInput(shell); got != want {
		t.Fatalf("reverse input = %q, want literal %q", got, want)
	}
	// Reporting the resulting cwd and refreshing it must not echo another cd.
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + "\x1b]7;file://box/home/test/a%20%27b%25%23%3B%24%28touch%20nope%29\x07" + folderSyncMarker(session, "B"))
	state.Wake()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == target })
	if got := folderSyncInput(shell); got != want {
		t.Fatalf("forward report/refresh formed a loop: %q", got)
	}
}

func TestReverseFolderSyncNeverWritesToUnsafePrompts(t *testing.T) {
	for _, mode := range []string{"off", "unknown", "dirty", "redraw-dirty", "running", "type-ahead", "alt-screen", "container", "local", "ai-exec"} {
		t.Run(mode, func(t *testing.T) {
			state, session, fake, _ := openTestSFTP(t)
			shell := fake.shell(t)
			if mode != "off" {
				state.Click(hitSettingsTerminalFollowToggle)
			}
			if mode != "unknown" {
				shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
				state.Wake()
			}
			switch mode {
			case "dirty", "redraw-dirty":
				session.term.send([]byte("echo unfinished"))
				if mode == "redraw-dirty" {
					shell.say(folderSyncMarker(session, "B")) // readline redraws PS1 without a new prompt
					state.Wake()
				}
			case "running":
				session.term.send([]byte("read value\r"))
			case "type-ahead":
				session.term.send([]byte("sleep 1\rpartial"))
				shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
				state.Wake()
			case "alt-screen":
				shell.say("\x1b[?1049h")
				state.Wake()
			case "container":
				session.containerCommand = "docker exec -it test sh"
			case "local":
				session.files.local = true
			case "ai-exec":
				session.term.exec = &aiExec{}
			}
			before := folderSyncInput(shell)
			state.navigateSFTPDirectory(session.files, "/home/test/docs")
			// The guard is evaluated at request time too, not just after I/O.
			session.term.exec = nil
			session.files.local = false
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
			if got := folderSyncInput(shell); got != before {
				t.Fatalf("%s prompt received an automatic command: %q", mode, strings.TrimPrefix(got, before))
			}
		})
	}
}

func folderSyncMarker(session *Session, kind string) string {
	return "\x1b]133;" + kind + ";motty=" + session.term.folderToken + "\x07"
}
