package workbench

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

type sudoRunnerTestConn struct {
	*fakeConn
	mu       sync.Mutex
	password string
	commands []string
	run      func(context.Context, string, int) ([]byte, error)
	stream   func(context.Context, string, int, func([]byte) bool) error
}

func (connection *sudoRunnerTestConn) RunSudoCommand(ctx context.Context, command, password string, limit int) ([]byte, error) {
	connection.mu.Lock()
	connection.commands = append(connection.commands, command)
	connection.password = password
	run := connection.run
	connection.mu.Unlock()
	if run != nil {
		return run(ctx, command, limit)
	}
	return []byte("privileged"), nil
}

func TestRemoteContainersRetryWithSudoOnlyAfterUserConfirmation(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 700, Height: 650}} {
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		if hit, ok := hitByID(&canvas, hitRemoteSudo); !ok || hit.Bounds.Width <= 0 || hit.Bounds.Height <= 0 {
			t.Fatalf("sudo control is missing in remote tools at %+v", size)
		}
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	connection := &sudoRunnerTestConn{fakeConn: session.conn.(*fakeConn)}
	connection.run = func(ctx context.Context, command string, limit int) ([]byte, error) {
		if strings.HasPrefix(command, "docker ps") {
			return []byte(containerFixture), nil
		}
		return server.run(ctx, command, limit)
	}
	session.conn = connection
	server.failures["docker ps"] = errors.New("permission denied")
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	connection.mu.Lock()
	before := len(connection.commands)
	connection.mu.Unlock()
	if before != 0 {
		t.Fatal("container listing used sudo without confirmation")
	}
	state.Click(hitRemoteSudo)
	if state.modal != modalSudoSFTP {
		t.Fatal("container privilege prompt did not open")
	}
	state.sudoSFTP.password.Insert("secret")
	state.Click(hitSudoSFTPEnable)
	waitFor(t, state, func() bool { return !state.remote.loading })
	connection.mu.Lock()
	after, password := len(connection.commands), connection.password
	connection.mu.Unlock()
	if after <= before || password != "secret" {
		t.Fatalf("privileged container calls=%d, password=%q", after, password)
	}
	state.Click(hitRemoteSudo)
	if session.sudoCommands || session.sudoPassword != "" {
		t.Fatal("container tools retained privilege after disabling sudo")
	}
}

func (connection *sudoRunnerTestConn) ExecSudo(ctx context.Context, command, password string, limit int) (sshconn.ExecResult, error) {
	output, err := connection.RunSudoCommand(ctx, command, password, limit)
	return sshconn.ExecResult{Output: output}, err
}

func (connection *sudoRunnerTestConn) StreamSudoCommand(ctx context.Context, command, password string, limit int, line func([]byte) bool) error {
	if connection.stream != nil {
		connection.mu.Lock()
		connection.commands = append(connection.commands, command)
		connection.password = password
		connection.mu.Unlock()
		return connection.stream(ctx, command, limit, line)
	}
	output, err := connection.RunSudoCommand(ctx, command, password, limit)
	if err == nil {
		line(output)
	}
	return err
}

func TestRemoteCommandsUseSudoOnlyAfterConfirmationAndForgetItOnDisconnect(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	connection := &sudoRunnerTestConn{fakeConn: fake}
	session.conn = connection
	session.stats = nil
	fake.runCommand = func(context.Context, string, int) ([]byte, error) { return []byte("ordinary"), nil }
	check := func(want string) {
		t.Helper()
		output, err := commandRunnerFor(session).RunCommand(context.Background(), "docker ps", 128)
		if err != nil || string(output) != want {
			t.Fatalf("command = %q, error = %v; want %q", output, err, want)
		}
	}
	check("ordinary")
	state.toggleSudoCommands(session, false)
	if state.modal != modalSudoSFTP {
		t.Fatal("missing privilege confirmation")
	}
	check("ordinary")
	state.clickSudoSFTP(hitSudoSFTPCancel)
	check("ordinary")
	state.toggleSudoCommands(session, false)
	state.sudoSFTP.password.Insert("secret")
	state.clickSudoSFTP(hitSudoSFTPEnable)
	check("privileged")
	connection.mu.Lock()
	password, commands := connection.password, len(connection.commands)
	connection.mu.Unlock()
	if password != "secret" || commands != 1 {
		t.Fatalf("sudo password=%q, calls=%d", password, commands)
	}
	state.toggleSudoCommands(session, false)
	check("ordinary")
	if session.sudoPassword != "" || session.sudoCommands {
		t.Fatal("turning off sudo retained the privilege")
	}
	state.toggleSudoCommands(session, false)
	state.sudoSFTP.password.Insert("secret")
	state.clickSudoSFTP(hitSudoSFTPEnable)
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if session.sudoPassword != "" || session.sudoCommands {
		t.Fatal("disconnection retained the sudo password")
	}
}

func TestMonitorRunnerUsesTheSameExplicitPrivilegeAsRemoteTools(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	connection := &sudoRunnerTestConn{fakeConn: fake}
	session.conn = connection
	session.stats = nil
	fake.runCommand = func(context.Context, string, int) ([]byte, error) { return []byte("ordinary"), nil }
	read := func() string {
		t.Helper()
		output, err := monitorRunnerFor(session).RunCommand(context.Background(), "monitor", 128)
		if err != nil {
			t.Fatal(err)
		}
		return string(output)
	}
	if read() != "ordinary" {
		t.Fatal("monitor started with privilege")
	}
	state.toggleSudoCommands(session, false)
	state.sudoSFTP.password.Insert("secret")
	state.clickSudoSFTP(hitSudoSFTPEnable)
	if read() != "privileged" {
		t.Fatal("monitor did not inherit explicit privilege")
	}
	state.toggleSudoCommands(session, false)
	if read() != "ordinary" {
		t.Fatal("monitor retained privilege after disabling sudo")
	}
}
