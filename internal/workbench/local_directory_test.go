package workbench

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"motty/internal/term"
)

func TestLocalDirectoryAcceptsTrustedRepeatAfterIgnoringProgramReport(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.sftpFollowOff = false
	state.restoreSessions([]savedSession{{Local: true}}, 1)
	session := &state.sessions[0]
	session.term = newTerminal(80, 24)
	state.configureTerminal(session)
	first, next := t.TempDir(), t.TempDir()
	report := func(directory string) string {
		if runtime.GOOS == "windows" {
			return "\x1b]9;9;" + directory + "\a"
		}
		return "\x1b]7;" + (&url.URL{Scheme: "file", Path: directory}).String() + "\a"
	}
	prompt := func(directory string) string {
		return "\x1b]133;A;motty=" + session.term.folderToken + "\a" + report(directory) + "\x1b]133;B;motty=" + session.term.folderToken + "\a"
	}
	session.term.screen.Write([]byte(prompt(first)))
	session.term.noteShellInput([]byte("command\r"))
	session.term.screen.Write([]byte(report(next)))
	if session.localDirectory != first {
		t.Fatal("the running program replaced the startup directory")
	}
	// The real prompt can report the same path the program just reported. Its
	// report must not be lost to terminal-level duplicate suppression.
	session.term.screen.Write([]byte(prompt(next)))
	if session.localDirectory != next {
		t.Fatalf("trusted repeat left directory at %q, want %q", session.localDirectory, next)
	}
	if session.term.followPath != next {
		t.Fatal("the trusted repeat did not update the attached files panel target")
	}
	session.term.followPath = "" // the report was consumed, then the panel was browsed manually
	session.term.screen.Write([]byte(prompt(next)))
	if session.term.followPath != "" {
		t.Fatal("an unchanged prompt tried to undo manual file browsing")
	}
}

func TestLocalDirectoryRestoredShellReallyStartsInEachFolder(t *testing.T) {
	program := "bash"
	if runtime.GOOS == "windows" {
		program = "cmd.exe"
	}
	program, err := exec.LookPath(program)
	if err != nil {
		t.Skip("supported local shell not installed")
	}
	state := newTestState(t, testOptions(t))
	state.localShell, state.sftpFollowOff = program, true
	dirs := []string{t.TempDir(), t.TempDir()}
	for i := range dirs {
		// macOS's /var temporary paths may be aliases of /private/var. A new
		// shell is allowed to report the physical spelling of its startup path.
		dirs[i], err = filepath.EvalSymlinks(dirs[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	state.restoreSessions([]savedSession{{Local: true, LocalDirectory: dirs[0]}, {Local: true, LocalDirectory: dirs[1]}}, 1)
	state.ensureWorkspaceDeck()
	for i := range state.sessions {
		state.reconnect(&state.sessions[i])
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		matched := 0
		for i := range state.sessions {
			if cwd, ok := state.sessions[i].term.screen.WorkingDirectory(); ok && filepath.Clean(cwd.Path) == dirs[i] {
				matched++
			}
		}
		if matched == len(dirs) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := range state.sessions {
		cwd, _ := state.sessions[i].term.screen.WorkingDirectory()
		t.Errorf("session %d: cwd %q; want %q; screen: %q", i, cwd.Path, dirs[i], state.sessions[i].term.screen.Text())
	}
}

func TestLocalDirectoriesRestoreIndependentlyWithoutFileFollowing(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.sftpFollowOff = true
	dirs := []string{t.TempDir(), filepath.Join(t.TempDir(), "中文 空格;100%")}
	if err := os.MkdirAll(dirs[1], 0700); err != nil {
		t.Fatal(err)
	}
	state.restoreSessions([]savedSession{{Local: true, Label: "local-1"}, {Local: true, Label: "local-2"}}, 1)
	for index := range state.sessions {
		session := &state.sessions[index]
		session.term = newTerminal(80, 24)
		state.configureTerminal(session)
		session.term.promptStarted, session.term.folderIntegrationSeen = true, true
		state.terminalDirectory(session.ID, term.Cwd{Path: dirs[index]})
	}
	state.flushTerminalDirectories()
	restored := newTestState(t, options)
	if len(restored.sessions) != 2 {
		t.Fatalf("restored sessions = %d", len(restored.sessions))
	}
	for i, session := range restored.sessions {
		if session.localDirectory != dirs[i] || localStartupDirectory(session.localDirectory, "") != dirs[i] {
			t.Fatalf("session %d restored directory = %q", i, session.localDirectory)
		}
	}
	// A manually closed tab must stay closed on the next launch.
	restored.closeSession(restored.sessions[0].ID)
	last := newTestState(t, options)
	if len(last.sessions) != 1 || last.sessions[0].localDirectory != dirs[1] {
		t.Fatal("closed tab was restored or the surviving tab lost its directory")
	}
}

func TestLocalDirectoryRejectsNestedLoginAndMissingFoldersFallBack(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.restoreSessions([]savedSession{{Local: true}}, 1)
	session := &state.sessions[0]
	session.term = newTerminal(80, 24)
	session.term.folderIntegrationSeen, session.term.promptStarted = true, true
	dir := t.TempDir()
	state.terminalDirectory(session.ID, term.Cwd{Path: dir})
	for _, report := range []term.Cwd{{Host: "another-host.invalid", Path: t.TempDir()}, {Path: "relative"}, {Path: dir + "\n"}} {
		state.terminalDirectory(session.ID, report)
		if session.localDirectory != dir {
			t.Fatalf("invalid report replaced local directory: %+v", report)
		}
	}
	session.term.promptStarted = false
	state.terminalDirectory(session.ID, term.Cwd{Path: t.TempDir()})
	if session.localDirectory != dir {
		t.Fatal("a report outside the local prompt was accepted")
	}
	session.term.folderIntegrationSeen = false
	state.terminalDirectory(session.ID, term.Cwd{Path: t.TempDir()})
	if session.localDirectory != dir {
		t.Fatal("an unverified directory report changed the next startup folder")
	}
	if got := localStartupDirectory(filepath.Join(dir, "removed"), ""); got != "" {
		t.Fatalf("missing directory did not fall back: %q", got)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if localStartupDirectory(file, "") != "" {
		t.Fatal("regular file accepted as startup directory")
	}
}

func TestLocalDirectoryHostAcceptsItsLongAndShortNames(t *testing.T) {
	name, err := os.Hostname()
	if err != nil || name == "" {
		t.Skip("no hostname to compare")
	}
	short := name
	if index := strings.IndexByte(name, '.'); index > 0 {
		short = name[:index]
	}
	for _, host := range []string{"", "localhost", name, short, strings.ToUpper(name), short + ".example.invalid"} {
		if !localDirectoryHost(host) {
			t.Errorf("localDirectoryHost(%q) rejected this machine (%q)", host, name)
		}
	}
	if localDirectoryHost("another-host.invalid") {
		t.Error("localDirectoryHost accepted another machine")
	}
}
