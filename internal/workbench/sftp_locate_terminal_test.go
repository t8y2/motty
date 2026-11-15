package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/shellkind"
	"motty/internal/sshconn"
)

func TestLocateTerminalToolbarUsesDisplayedDirectoryOnce(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	shell := fake.shell(t)
	state.sftpFollowOff = true
	// A shell-looking path is always one literal argument to builtin cd.
	target := "/home/test/中文 'b%#;$(touch nope)"
	state.openSFTPDirectory(session.files, target)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == target })
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	reads := remote.readCount(target)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPLocateTerminal)); !ok {
		t.Fatal("the remote files toolbar has no locate button")
	}
	state.Click(contextID(hitSFTPLocateTerminal))
	if got := folderSyncInput(shell); got != "" {
		t.Fatalf("cd ran before a fresh directory read: %q", got)
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	want := "builtin cd -- '/home/test/中文 '\\''b%#;$(touch nope)'\r"
	if got := folderSyncInput(shell); got != want {
		t.Fatalf("terminal input = %q, want %q", got, want)
	}
	if remote.readCount(target) != reads+1 || state.sftpTerminalFollow || !state.sftpFollowOff {
		t.Fatal("locate did not validate the directory once or changed a follow preference")
	}
	if session.files.terminalPath != "" || session.files.terminalLocate {
		t.Fatal("locate left a pending command")
	}
	// A new ready prompt and ordinary navigation must not repeat the action.
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	state.navigateSFTPDirectory(session.files, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if got := folderSyncInput(shell); got != want {
		t.Fatalf("one-shot locate enabled automatic synchronization: %q", got)
	}
}

func TestLocateTerminalRejectsUnsafeOrUnrelatedContexts(t *testing.T) {
	for _, mode := range []string{"unknown", "dirty", "running", "alt-screen", "ai-exec", "local", "detached", "container", "disconnected", "busy", "invalid-path"} {
		t.Run(mode, func(t *testing.T) {
			state, session, fake, _ := openTestSFTP(t)
			shell := fake.shell(t)
			view := session.files
			if mode != "unknown" {
				shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
				state.Wake()
			}
			switch mode {
			case "dirty":
				session.term.send([]byte("echo unfinished"))
			case "running":
				session.term.send([]byte("read value\r"))
			case "alt-screen":
				shell.say("\x1b[?1049h")
				state.Wake()
			case "ai-exec":
				session.term.exec = &aiExec{}
			case "local":
				view.local = true
			case "detached":
				view.detached = &sftpWindow{}
			case "container":
				session.containerCommand = "docker exec -it test sh"
			case "disconnected":
				session.phase = sshconn.PhaseClosed
			case "busy":
				view.busy = true
			case "invalid-path":
				view.path = "/tmp/invalid\npath"
			}
			before := folderSyncInput(shell)
			state.locateSFTPTerminal(view)
			if got := folderSyncInput(shell); got != before || view.terminalPath != "" || view.terminalLocate {
				t.Fatalf("unsafe locate sent or queued a command: %q, %q", got, view.terminalPath)
			}
			if mode == "dirty" && view.status != state.messages.Tools.FilesLocateTerminalSkipped {
				t.Fatal("an unfinished command was skipped without actionable feedback")
			}
			if mode == "unknown" && view.status != state.messages.Tools.FilesLocateTerminalSkipped {
				t.Fatal("an unrecognized prompt was skipped without explanation")
			}
		})
	}
}

func TestLocateTerminalRechecksPromptAfterDirectoryRead(t *testing.T) {
	for _, change := range []string{"typing", "new-prompt", "cancel", "listing-error"} {
		t.Run(change, func(t *testing.T) {
			state, session, fake, remote := openTestSFTP(t)
			shell := fake.shell(t)
			shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
			state.Wake()
			remote.mu.Lock()
			remote.readDelay = 30 * time.Millisecond
			if change == "listing-error" {
				remote.unreadable = map[string]bool{session.files.path: true}
			}
			remote.mu.Unlock()
			state.Click(hitSFTPLocateTerminal)
			switch change {
			case "typing":
				session.term.send([]byte("unfinished"))
			case "new-prompt":
				shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
			case "cancel":
				state.cancelSFTPNavigation(session.files)
			}
			want := folderSyncInput(shell)
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
			if got := folderSyncInput(shell); got != want || session.files.terminalPath != "" || session.files.terminalLocate {
				t.Fatalf("invalidated locate sent or retained a command: %q, pending %q", got, session.files.terminalPath)
			}
			if (change == "typing" || change == "new-prompt") && session.files.status != state.messages.Tools.FilesLocateTerminalSkipped {
				t.Fatal("invalidated locate did not ask the user to retry")
			}
		})
	}
}

func TestLocateTerminalUsesTheOwningSession(t *testing.T) {
	state, original, fake, _ := openTestSFTP(t)
	id := original.ID
	shell := fake.shell(t)
	shell.say(folderSyncMarker(original, "C") + folderSyncMarker(original, "A") + folderSyncMarker(original, "B"))
	state.Wake()
	otherShell := &fakeShell{done: make(chan struct{})}
	other := Session{ID: id + 1000, host: original.host, phase: sshconn.PhaseConnected, term: newTerminal(80, 24)}
	other.term.shell = otherShell
	state.sessions = append(state.sessions, other)
	state.activeSessionID = other.ID
	session, _ := state.sessionByIDPointer(id)
	state.locateSFTPTerminal(session.files)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if got := folderSyncInput(shell); got != "builtin cd -- '/home/test'\r" {
		t.Fatalf("owning terminal received %q", got)
	}
	if got := folderSyncInput(otherShell); got != "" {
		t.Fatalf("unrelated active terminal received %q", got)
	}
}

func TestLocateTerminalNativeClickIgnoresPathDraftAndSelectedChild(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	shell := fake.shell(t)
	shell.say(folderSyncMarker(session, "C") + folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
	state.Wake()
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, sftpListRowID(t, session.files.active(), sftpRowIndex(t, session.files, "docs")))
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPPathEdit))
	typeSFTPPathDraft(t, driver, state, "/unsubmitted-folder")
	follow := hitOf(t, driver, contextID(hitSFTPFollowToggle))
	locate := hitOf(t, driver, contextID(hitSFTPLocateTerminal))
	if locate.Label != state.messages.Tools.FilesLocateTerminal || locate.Disabled || locate.Bounds.Y != follow.Bounds.Y || locate.Bounds.X < follow.Bounds.X+follow.Bounds.Width {
		t.Fatalf("locate must be available beside follow: %+v", locate)
	}
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPLocateTerminal))
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if got := folderSyncInput(shell); got != "builtin cd -- '/home/test'\r" {
		t.Fatalf("locate used the path draft or selected child: %q", got)
	}
}

func TestLocateTerminalWorksOnInteractiveOnlyHostWithoutReporting(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	shell := fake.shell(t)
	session.host.InteractiveOnly = true
	session.term.integrated = false
	shell.say("\r\nroot@box:~# ")
	state.Wake()
	// Reproduce the screenshot: a manually submitted cd has returned to an
	// ordinary Bash prompt, with no OSC markers to acknowledge the command.
	session.term.send([]byte("cd /etc/\r"))
	shell.say("cd /etc/\r\nroot@box:/etc# ")
	state.Wake()
	before := folderSyncInput(shell)
	// The ordinary prompt permits only the explicit click. Automatic sync
	// still requires this session's trusted command/prompt reports.
	state.sftpTerminalFollow = true
	state.requestTerminalDirectory(session.files, session.files.path)
	if session.files.terminalPath != "" || folderSyncInput(shell) != before {
		t.Fatal("the plain prompt authorized automatic reverse sync")
	}
	state.sftpTerminalFollow = false
	driver := sftpPathFocusDriver(t, state)
	clickSFTPPathFocusHit(t, driver, contextID(hitSFTPLocateTerminal))
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if got := folderSyncInput(shell); got != before+"command cd -- '/home/test'\r" {
		t.Fatalf("ordinary Bash prompt could not locate the terminal: %q, status %q", got, session.files.status)
	}
	if !session.host.InteractiveOnly || state.sftpTerminalFollow || session.term.commandReporting || session.term.folderIntegrationSeen {
		t.Fatal("manual locate changed the connection policy or enabled automatic synchronization")
	}
	// A stale screen before the cd echo must not authorize a second click.
	state.locateSFTPTerminal(session.files)
	if session.files.terminalPath != "" || session.files.busy {
		t.Fatal("a repeated click reused a prompt whose command has not echoed yet")
	}
}

func TestLocateTerminalPlainShellRequiresAnEmptyUnchangedPrompt(t *testing.T) {
	for _, mode := range []string{"dirty", "unacknowledged-input", "running", "output-during-read", "nested-integrated", "missing-command-marker", "repl", "password", "continuation", "text-after-cursor", "other-shell", "alt-screen", "unsafe-path"} {
		t.Run(mode, func(t *testing.T) {
			state, session, fake, remote := openTestSFTP(t)
			shell := fake.shell(t)
			session.host.InteractiveOnly = true
			session.term.integrated = false
			shell.say("\r\nroot@box:/etc# ")
			state.Wake()
			switch mode {
			case "dirty":
				session.term.send([]byte("echo unfinished"))
			case "unacknowledged-input":
				session.term.send([]byte("sleep 10\r"))
			case "running":
				session.term.send([]byte("sleep 10\r"))
				shell.say("sleep 10\r\n")
			case "output-during-read":
				remote.mu.Lock()
				remote.readDelay = 30 * time.Millisecond
				remote.mu.Unlock()
			case "nested-integrated":
				shell.say(folderSyncMarker(session, "C"))
			case "missing-command-marker":
				shell.say(folderSyncMarker(session, "A") + folderSyncMarker(session, "B"))
			case "repl":
				shell.say("\r\npostgres=# ")
			case "password":
				shell.say("\r\n[sudo] password for root: ")
			case "continuation":
				shell.say("\r\n> ")
			case "text-after-cursor":
				shell.say("echo unfinished\x1b[15D")
			case "other-shell":
				session.term.shell = kindShell{fakeShell: shell, kind: shellkind.PowerShell}
			case "alt-screen":
				shell.say("\x1b[?1049h")
			case "unsafe-path":
				session.files.path = "/tmp/bad\npath"
			}
			state.Wake()
			before := folderSyncInput(shell)
			state.locateSFTPTerminal(session.files)
			if mode == "output-during-read" {
				if !session.files.busy {
					t.Fatal("the plain prompt did not start directory validation")
				}
				// Even returning to visually identical text is a new screen.
				shell.say("\r\nbackground output\r\nroot@box:/etc# ")
			}
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
			if got := folderSyncInput(shell); got != before || session.files.terminalPath != "" {
				t.Fatalf("%s plain prompt received or queued a cd: %q, %q", mode, got, session.files.terminalPath)
			}
		})
	}
}

func TestLocateTerminalPlainPromptRecognition(t *testing.T) {
	for _, prompt := range []string{"root@box:/etc# ", "user@box:~/中文 dir$ ", "[root@box etc]# ", "bash-5.2$ ", "sh$ ", "# ", "$ "} {
		if !locateShellPrompt.MatchString(prompt) {
			t.Errorf("ordinary POSIX prompt was not recognized: %q", prompt)
		}
	}
	for _, prompt := range []string{"root@box:/etc# ls", "postgres=# ", "mysql> ", "> ", "Password: ", "PS C:\\> ", "root@box:/etc# echo # "} {
		if locateShellPrompt.MatchString(prompt) {
			t.Errorf("nonempty or foreign prompt was recognized: %q", prompt)
		}
	}
}
