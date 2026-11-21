//go:build darwin || windows || linux

package workbench

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

// The log command reads the first unreadable path as missing unless it walks
// up to the component that denied the lookup. On a server the account cannot
// search, that branch is the difference between "Permission denied" and a
// wrong report that the log does not exist.
func TestRemoteLogCommandNamesThePermissionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the server log command is a POSIX shell script, which a Windows test cannot run")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can search any directory, so no permission failure can be created here")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(locked, "app.log")
	if err := os.WriteFile(secret, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	run := func(path string) string {
		t.Helper()
		output, _ := exec.Command("/bin/sh", "-c", remoteLogCommand(path)).CombinedOutput()
		return string(output)
	}
	if got := run(secret); !strings.Contains(got, "Permission denied.") {
		t.Fatalf("a log in an unsearchable directory reported %q, want a permission failure", got)
	}
	if got := run(filepath.Join(root, "absent.log")); !strings.Contains(got, "Log path is missing") {
		t.Fatalf("a missing log reported %q", got)
	}
	if got := run(root); !strings.Contains(got, "not a regular file") {
		t.Fatalf("a directory reported %q", got)
	}
}

func TestRemoteLogsRunWithTheSessionsPrivilege(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	connection := &sudoRunnerTestConn{fakeConn: fake}
	session.conn = connection
	plain := make(chan string, 1)
	fake.streamCommand = func(ctx context.Context, command string, _ int, line func([]byte) bool) error {
		plain <- command
		line([]byte("\n"))
		<-ctx.Done()
		return ctx.Err()
	}
	privileged := make(chan string, 1)
	connection.stream = func(ctx context.Context, command string, _ int, line func([]byte) bool) error {
		privileged <- command
		line([]byte("\n"))
		line([]byte("privileged log line\n"))
		<-ctx.Done()
		return ctx.Err()
	}
	path := remoteLogPath{Name: "Audit", Path: "/var/log/audit/audit.log"}
	view := &remoteLogView{follow: true, height: 360, pathSplit: ui.SplitterState{Pos: 280}}
	session.remoteLogs = view
	start := func() {
		t.Helper()
		state.followRemoteLog(session, view, path)
	}
	expect := func(channel chan string, what string) string {
		t.Helper()
		select {
		case command := <-channel:
			return command
		case <-time.After(time.Second):
			t.Fatalf("the log stream did not start %s", what)
			return ""
		}
	}
	start()
	expect(plain, "without privilege")
	session.stopRemoteLogs()
	session.sudoCommands, session.sudoPassword = true, "secret"
	start()
	if command := expect(privileged, "with the session's privilege"); !strings.Contains(command, "audit.log") {
		t.Fatalf("privileged stream command = %q", command)
	}
	connection.mu.Lock()
	password := connection.password
	connection.mu.Unlock()
	if password != "secret" {
		t.Fatalf("privileged stream password = %q", password)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var snapshot remoteLogSnapshot
		view.buffer.snapshot(&snapshot)
		if len(snapshot.lines) == 1 && snapshot.lines[0] == "privileged log line" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("privileged stream reached the panel with %q", snapshot.lines)
		}
		time.Sleep(time.Millisecond)
	}
	session.stopRemoteLogs()
}

func TestRemoteLogSudoControlRestartsTheStreamWithPrivilege(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	path := remoteLogPath{Name: "Audit", Path: "/var/log/audit/audit.log"}
	index := state.hostIndex(session.HostID)
	if index < 0 {
		t.Fatal("the session host is not in the catalog")
	}
	state.hosts[index].RemoteLogPaths = []remoteLogPath{path}
	plain := make(chan string, 4)
	fake.streamCommand = func(ctx context.Context, command string, _ int, _ func([]byte) bool) error {
		plain <- command
		return errors.New("permission denied")
	}
	privileged := make(chan string, 4)
	connection := &sudoRunnerTestConn{fakeConn: fake}
	connection.stream = func(ctx context.Context, command string, _ int, line func([]byte) bool) error {
		privileged <- command
		line([]byte("\n"))
		line([]byte("privileged audit line\n"))
		<-ctx.Done()
		return ctx.Err()
	}
	session.conn = connection
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Server logs", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
		OnResize: state.Resize, OnWake: state.Wake,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	clickLabel := func(label string) {
		t.Helper()
		if err := view.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	clickID := func(id gfx.ID) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID != id {
				continue
			}
			x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
			if err := view.Click(x, y); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			return
		}
		t.Fatalf("missing control %v", id)
	}
	clickLabel(state.messages.RemoteLogs.Title)
	clickLabel(path.Name + " " + path.Path)
	select {
	case <-plain:
	case <-time.After(time.Second):
		t.Fatal("selecting the path did not start the account's stream")
	}
	if session.sudoCommands {
		t.Fatal("the log panel started with privilege")
	}
	clickID(hitRemoteLogSudo)
	if state.modal != modalSudoSFTP {
		t.Fatal("the log panel's lock did not open the privilege dialog")
	}
	if !state.sudoSFTP.commands || !state.sudoSFTP.logs {
		t.Fatalf("the dialog would apply elsewhere: %+v", state.sudoSFTP)
	}
	state.sudoSFTP.password.Insert("secret")
	state.clickSudoSFTP(hitSudoSFTPEnable)
	select {
	case <-privileged:
	case <-time.After(time.Second):
		t.Fatal("confirming the password did not restart the stream with privilege")
	}
	if !session.sudoCommands || session.sudoPassword != "secret" {
		t.Fatalf("privilege = %v, password = %q", session.sudoCommands, session.sudoPassword)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if view := session.remoteLogs; view != nil {
			var snapshot remoteLogSnapshot
			view.buffer.snapshot(&snapshot)
			if len(snapshot.lines) == 1 && snapshot.lines[0] == "privileged audit line" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the privileged line did not reach the panel")
		}
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	// Turning the switch off reopens the path without privilege: a root tail
	// must not outlive the switch, and the panel must say what it now reads.
	clickID(hitRemoteLogSudo)
	select {
	case <-plain:
	case <-time.After(time.Second):
		t.Fatal("turning the switch off did not restart the plain stream")
	}
	if session.sudoCommands {
		t.Fatal("the switch stayed on")
	}
}
