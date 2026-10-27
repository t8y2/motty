package workbench

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

const processFixture = `    1 root      0.1  0.2    10-02:03:04 /sbin/init
 2211 www-data  42.5  1.3       01:02:03 nginx: worker process
 3000 alice      7.0 30.9          05:01 /usr/bin/python3 /srv/app.py
`

const imageFixture = `d4c45f66efc5|debian:bookworm-slim|74.9MB|2 weeks ago
cf001c2f8af7|registry.example.com/library/python:<none>|1GB|3 months ago
`

const containerFixture = `a1b2c3d4e5f6|old-job|alpine:3.19|exited|Exited (0) 3 days ago
0f1e2d3c4b5a|web|nginx:latest|running|Up 2 hours
`

const composeFixture = `[{"Name":"web","Status":"running(2)","ConfigFiles":"/srv/web/compose.yml"},{"Name":"batch","Status":"exited(1)","ConfigFiles":"/srv/batch/compose.yml"}]`

type remoteServer struct {
	mu       sync.Mutex
	commands []string
	failures map[string]error
}

func (server *remoteServer) run(_ context.Context, command string, _ int) ([]byte, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.commands = append(server.commands, command)
	switch {
	case strings.Contains(command, "ps -eo"):
		return []byte(processFixture), nil
	case strings.HasPrefix(command, "docker ps"):
		if err := server.failures["docker ps"]; err != nil {
			return []byte("sh: 1: docker: not found"), err
		}
		return []byte(containerFixture), nil
	case strings.HasPrefix(command, "docker inspect --format"):
		return []byte("0f1e2d3c4b5a" + strings.Repeat("0", 52) + "\n"), nil
	case strings.HasPrefix(command, "docker images"):
		return []byte(imageFixture), nil
	case strings.HasPrefix(command, "docker volume ls"):
		return []byte("pgdata|local|local\n"), nil
	case strings.HasPrefix(command, "docker compose ls"):
		return []byte(composeFixture), nil
	case strings.HasPrefix(command, "kill -"), strings.HasPrefix(command, "renice "), strings.HasPrefix(command, "docker "):
		for fragment, err := range server.failures {
			if strings.Contains(command, fragment) {
				return []byte("denied by the test\nsecond line"), err
			}
		}
		return nil, nil
	}
	return nil, errors.New("unexpected command " + command)
}

func (server *remoteServer) ran(prefix string) int {
	server.mu.Lock()
	defer server.mu.Unlock()
	count := 0
	for _, command := range server.commands {
		if strings.HasPrefix(command, prefix) {
			count++
		}
	}
	return count
}

func openRemoteToolsDialog(t *testing.T) (*State, *remoteServer, *fakeShell) {
	t.Helper()
	state, _, fake, shell := liveSession(t)
	server := &remoteServer{failures: map[string]error{}}
	fake.runCommand = server.run
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	rightClick(state, session, 400, 300)
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	state.Click(hitTerminalMenuTools)
	if state.modal != modalRemoteTools {
		t.Fatalf("the dialog did not open: modal %d", state.modal)
	}
	waitFor(t, state, func() bool { return !state.remote.loading })
	return state, server, shell
}

func TestRemoteToolsListProcessesOnceAndNeverPoll(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	if len(state.remote.rows) != 3 || state.remote.rows[0].cells[0] != "2211" || state.remote.rows[0].cells[2] != "42.5" {
		t.Fatalf("rows %+v", state.remote.rows)
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitRemoteTabProcesses, hitRemoteSortMemory, hitRemoteRowList.At(0), hitRemoteRowList.At(2), hitRemoteTerminate, hitRemoteKill, hitRemoteRefresh} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("control %d is not painted", id)
		}
	}
	if _, ok := hitByID(&canvas, hitRemoteStop); ok {
		t.Error("a container action shows on the process tab")
	}
	for index := 0; index < 5; index++ {
		state.Wake()
		state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	}
	if server.ran("LC_ALL=C ps") != 1 {
		t.Fatalf("%d listings for one open", server.ran("LC_ALL=C ps"))
	}
	state.Click(hitRemoteSortMemory)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if server.ran("LC_ALL=C ps") != 2 || !strings.Contains(server.commands[1], "-k4,4nr") || state.remote.rows[0].cells[0] != "3000" {
		t.Fatalf("after sorting by memory: %q, first %q", server.commands, state.remote.rows[0].cells[0])
	}
}

func TestSignallingAProcessAsksFirstAndReturnsToTheList(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTerminate)
	if state.modal != modalRemoteTools || state.remote.status != state.messages.Tools.RemoteNeedSelection {
		t.Fatalf("no selection: modal %d, %q", state.modal, state.remote.status)
	}
	state.Click(hitRemoteRowList.At(0)) // the busiest, process 2211
	state.Click(hitRemoteKill)
	if state.modal != modalForm || state.form.title != state.messages.Tools.RemoteConfirmKillTitle || !strings.Contains(state.form.description, "2211") {
		t.Fatalf("no confirmation: modal %d %q %q", state.modal, state.form.title, state.form.description)
	}
	state.Click(hitFormCancel)
	if state.modal != modalRemoteTools || server.ran("kill") != 0 {
		t.Fatalf("cancelling: modal %d, %d signals", state.modal, server.ran("kill"))
	}
	state.Click(hitRemoteKill)
	state.Click(hitFormSubmit)
	if state.modal != modalRemoteTools {
		t.Fatalf("confirming left modal %d", state.modal)
	}
	waitFor(t, state, func() bool {
		return server.ran("kill -KILL 2211") == 1 && !state.remote.loading && server.ran("LC_ALL=C ps") == 2
	})
	if !strings.Contains(state.remote.status, "KILL") || state.remote.failed {
		t.Fatalf("status %q", state.remote.status)
	}
}

func TestAServerThatRefusesASignalIsReportedInTheDialog(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.failures["kill -TERM"] = errors.New("exit status 1")
	state.Click(hitRemoteRowList.At(1))
	state.Click(hitRemoteTerminate)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return state.remote.failed && !state.remote.loading })
	if !strings.Contains(state.remote.status, "denied by the test") || strings.Contains(state.remote.status, "second line") {
		t.Fatalf("status %q", state.remote.status)
	}
}

func TestRemoteToolsManageContainers(t *testing.T) {
	state, server, shell := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	if state.remote.rows[0].cells[0] != "web" || server.ran("docker ps") != 1 {
		t.Fatalf("rows %+v", state.remote.rows)
	}
	state.Click(hitRemoteRowList.At(1)) // old-job, stopped
	state.Click(hitRemoteStart)
	waitFor(t, state, func() bool {
		return server.ran("docker start a1b2c3d4e5f6") == 1 && !state.remote.loading && server.ran("docker ps") == 2
	})

	state.Click(hitRemoteRemove)
	if state.modal != modalForm || !strings.Contains(state.form.description, "old-job") {
		t.Fatalf("no confirmation before removing: modal %d", state.modal)
	}
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return server.ran("docker rm a1b2c3d4e5f6") == 1 })

	waitFor(t, state, func() bool { return !state.remote.loading })
	state.Click(hitRemoteRowList.At(0)) // web
	state.Click(hitRemoteLogs)
	if state.modal != modalNone || !strings.Contains(shell.typed(), "docker logs -f --tail 200 0f1e2d3c4b5a\r") {
		t.Fatalf("modal %d, the terminal got %q", state.modal, shell.typed())
	}
}

func TestRemoteToolsContainerShellOpensSharedTabWithoutTypingIntoOriginal(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	original := world.connectFirst(t, state)
	source, _ := state.sessionByIDPointer(state.activeSessionID)
	source.term.live = true
	server := &remoteServer{failures: map[string]error{}}
	original.runCommand = server.run
	state.openRemoteTools(source)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	state.Click(hitRemoteRowList.At(0)) // running web
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	button, painted := hitByID(&canvas, hitRemoteShell)
	if !painted || button.Disabled {
		t.Fatalf("container Shell action missing or disabled: %+v, painted=%v", button, painted)
	}
	state.Click(hitRemoteShell)
	waitFor(t, state, func() bool { return len(state.sessions) == 2 && state.modal == modalNone })
	if len(world.conns) != 1 || len(world.shared) != 1 ||
		len(original.shells) != 1 || len(world.shared[0].configs) != 1 ||
		world.shared[0].configs[0].Command != "docker exec -it 0f1e2d3c4b5a"+strings.Repeat("0", 52)+" sh" ||
		server.ran("docker exec 0f1e2d3c4b5a"+strings.Repeat("0", 52)+" sh -c :") != 1 {
		t.Fatalf("dials=%d shares=%d originalShells=%d childConfigs=%+v probes=%q", len(world.conns), len(world.shared), len(original.shells), world.shared[0].configs, server.commands)
	}
}

func TestRemoteToolsContainerShellRejectsStoppedSelection(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	state.Click(hitRemoteRowList.At(1)) // exited job
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	button, painted := hitByID(&canvas, hitRemoteShell)
	if !painted || !button.Disabled {
		t.Fatalf("stopped container offered Shell: button=%+v painted=%v", button, painted)
	}
	state.Click(hitRemoteShell)
	if server.ran("docker exec") != 0 || len(state.sessions) != 1 || state.modal != modalRemoteTools {
		t.Fatalf("stopped container opened a Shell: probes=%q tabs=%d modal=%d", server.commands, len(state.sessions), state.modal)
	}
}

func TestRemoteToolsWindowsHostNeverProbesPOSIXContainerShell(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	source, _ := state.remoteSession()
	source.host.System = sshconn.SystemWindows
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	state.Click(hitRemoteRowList.At(0))
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	button, painted := hitByID(&canvas, hitRemoteShell)
	if !painted || !button.Disabled {
		t.Fatalf("Windows SSH host offered Shell: button=%+v painted=%v", button, painted)
	}
	state.Click(hitRemoteShell)
	if server.ran("docker inspect") != 0 || server.ran("docker exec") != 0 || len(state.sessions) != 1 {
		t.Fatalf("Windows host probed a POSIX container Shell: commands=%q", server.commands)
	}
}

func TestRemoteToolsContainerShellFailureDoesNotCreateATab(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	original := world.connectFirst(t, state)
	source, _ := state.sessionByIDPointer(state.activeSessionID)
	source.term.live = true
	server := &remoteServer{failures: map[string]error{"docker exec": errors.New("exit status 1")}}
	original.runCommand = server.run
	state.openRemoteTools(source)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteShell)
	waitFor(t, state, func() bool { return !state.remote.loading && state.remote.failed })
	if len(state.sessions) != 1 || len(world.shared) != 0 || state.modal != modalRemoteTools {
		t.Fatalf("failed probe opened tab: sessions=%d shares=%d modal=%d", len(state.sessions), len(world.shared), state.modal)
	}
}

func TestRemoteToolsContainerShellIgnoresClosedDialogResult(t *testing.T) {
	world, options := newSharingWorld(t)
	notified := make(chan struct{}, 2)
	options.Invalidate = func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	}
	state := newTestState(t, options)
	original := world.connectFirst(t, state)
	source, _ := state.sessionByIDPointer(state.activeSessionID)
	source.term.live = true
	server := &remoteServer{failures: map[string]error{}}
	started, release := make(chan struct{}), make(chan struct{})
	original.runCommand = func(ctx context.Context, command string, limit int) ([]byte, error) {
		if strings.HasPrefix(command, "docker exec") {
			close(started)
			<-release
		}
		return server.run(ctx, command, limit)
	}
	state.openRemoteTools(source)
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteShell)
	<-started
	state.Click(hitRemoteClose)
	for len(notified) > 0 {
		<-notified
	}
	close(release)
	select {
	case <-notified:
		state.Wake()
	case <-time.After(2 * time.Second):
		t.Fatal("probe result was never delivered")
	}
	if state.modal != modalNone || len(state.sessions) != 1 || len(world.shared) != 0 {
		t.Fatalf("closed dialog's stale probe opened a tab: modal=%d tabs=%d shares=%d", state.modal, len(state.sessions), len(world.shared))
	}
}

func TestRemoteToolsExplainWhyThereIsNothingToShow(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.failures["docker ps"] = errors.New("exit status 127")
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if !state.remote.failed || state.remote.status != state.messages.Tools.RemoteErrNoDocker || len(state.remote.rows) != 0 {
		t.Fatalf("status %q, rows %d", state.remote.status, len(state.remote.rows))
	}
	text := state.remoteErrorText(remotetools.ErrNoDocker)
	if text != state.messages.Tools.RemoteErrNoDocker {
		t.Fatalf("text %q", text)
	}
	text = state.remoteErrorText(&remotetools.Failure{Message: "ps: bad option", Err: remotetools.ErrUnsupported})
	if !strings.Contains(text, "ps: bad option") {
		t.Fatalf("text %q", text)
	}
}

func TestAStaleAnswerDoesNotReplaceTheListTheUserIsLookingAt(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	before := len(state.remote.rows)
	state.remote.serial += 5
	state.aiPost(func(s *State) {}) // nothing to apply
	state.Wake()
	if len(state.remote.rows) != before {
		t.Fatalf("rows changed from %d to %d", before, len(state.remote.rows))
	}
}

func TestClosingRemoteToolsWhileALoadIsOnItsWayIgnoresTheAnswer(t *testing.T) {
	state, _, _, _ := liveSession(t)
	release := make(chan struct{})
	fake := state.sessions[0].conn.(*fakeConn)
	fake.runCommand = func(ctx context.Context, command string, _ int) ([]byte, error) {
		<-release
		return []byte(processFixture), nil
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openRemoteTools(session)
	state.Click(hitRemoteClose)
	close(release)
	for index := 0; index < 50; index++ {
		state.Wake()
	}
	if len(state.remote.rows) != 0 || state.modal != modalNone {
		t.Fatalf("rows %d, modal %d", len(state.remote.rows), state.modal)
	}
}

func TestRemoteToolsReadFromOldHostCannotReplaceNewHostRows(t *testing.T) {
	world, options := newSharingWorld(t)
	notified := make(chan struct{}, 16)
	options.Invalidate = func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	a := &state.sessions[0]
	a.term.live = true
	aID := a.ID
	hostB := a.host
	hostB.ID, hostB.Label, hostB.Address = 0, "Host B", "host-b.example.test"
	b := state.newHostSession(hostB)
	a, _ = state.sessionByIDPointer(aID)
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	b.term.live = true
	startA, releaseA := make(chan struct{}), make(chan struct{})
	world.conns[0].runCommand = func(_ context.Context, _ string, _ int) ([]byte, error) {
		close(startA)
		<-releaseA
		return []byte(processFixture), nil
	}
	world.conns[1].runCommand = func(_ context.Context, _ string, _ int) ([]byte, error) {
		return []byte(" 9090 bob 9.0 3.0 00:01:00 b-service\n"), nil
	}
	state.activeSessionID = a.ID
	state.openRemoteTools(a)
	<-startA
	state.Click(hitRemoteClose)
	state.activeSessionID = b.ID
	state.openRemoteTools(b)
	waitFor(t, state, func() bool {
		return !state.remote.loading && len(state.remote.procs) == 1 && state.remote.procs[0].PID == 9090
	})
	for len(notified) > 0 {
		<-notified
	}
	close(releaseA)
	select {
	case <-notified:
		state.Wake()
	case <-time.After(2 * time.Second):
		t.Fatal("old host result never returned")
	}
	if state.remote.session != b.ID || len(state.remote.procs) != 1 || state.remote.procs[0].PID != 9090 {
		t.Fatalf("old host replaced B: source=%d rows=%+v", state.remote.session, state.remote.procs)
	}
}

func TestRemoteToolsListAndRemoveImagesVolumesAndNetworks(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabImages)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	if state.remote.rows[0].cells[0] != "debian:bookworm-slim" || state.remote.rows[0].cells[1] != "d4c45f66efc5" {
		t.Fatalf("image row %+v", state.remote.rows[0])
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitRemoteRemove); !ok {
		t.Error("an image tab has no Remove")
	}
	if _, ok := hitByID(&canvas, hitRemoteStart); ok {
		t.Error("a container action shows on the images tab")
	}

	// Remove asks first, and removes a tagged image by its reference.
	state.Click(hitRemoteRemove)
	if state.remote.status != state.messages.Tools.RemoteNeedSelection {
		t.Fatalf("nothing selected, status %q", state.remote.status)
	}
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteRemove)
	if state.modal != modalForm || state.form.title != state.messages.Tools.RemoteConfirmRemoveImageTitle || !strings.Contains(state.form.description, "debian:bookworm-slim") {
		t.Fatalf("no confirmation: modal %d %q", state.modal, state.form.title)
	}
	state.Click(hitFormCancel)
	if server.ran("docker rmi") != 0 {
		t.Fatal("cancelling still removed the image")
	}
	state.Click(hitRemoteRemove)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool {
		return server.ran("docker rmi debian:bookworm-slim") == 1 && !state.remote.loading && server.ran("docker images") == 2
	})

	// An image without a tag has nothing to be addressed by but its ID.
	state.Click(hitRemoteRowList.At(1))
	state.Click(hitRemoteRemove)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return server.ran("docker rmi cf001c2f8af7") == 1 })

	waitFor(t, state, func() bool { return !state.remote.loading })
	state.Click(hitRemoteTabVolumes)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 1 })
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteRemove)
	if state.form.title != state.messages.Tools.RemoteConfirmRemoveVolumeTitle {
		t.Fatalf("a volume's confirmation is %q", state.form.title)
	}
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return server.ran("docker volume rm pgdata") == 1 })
}

func TestReniceUsesTheSelectedProcessAndShowsTheServersRefusal(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteNiceLower)
	if state.remote.status != state.messages.Tools.RemoteNeedSelection {
		t.Fatalf("nothing selected, status %q", state.remote.status)
	}
	state.Click(hitRemoteRowList.At(0)) // 2211
	state.Click(hitRemoteNiceLower)
	waitFor(t, state, func() bool {
		return server.ran("renice 10 -p 2211") == 1 && !state.remote.loading && server.ran("LC_ALL=C ps") == 2
	})
	if state.remote.failed {
		t.Fatalf("status %q", state.remote.status)
	}
	server.failures["renice -5"] = errors.New("exit status 1")
	state.Click(hitRemoteNiceHigher)
	waitFor(t, state, func() bool { return state.remote.failed && !state.remote.loading })
	if !strings.Contains(state.remote.status, "denied by the test") {
		t.Fatalf("status %q", state.remote.status)
	}
}

func TestRemoteToolsManageComposeProjects(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabCompose)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	if state.remote.rows[0].cells[0] != "batch" || state.remote.rows[1].cells[2] != "/srv/web/compose.yml" {
		t.Fatalf("rows %+v", state.remote.rows)
	}
	state.Click(hitRemoteRowList.At(0))
	state.Click(hitRemoteRestart)
	waitFor(t, state, func() bool {
		return server.ran("docker compose -p batch restart") == 1 && !state.remote.loading && server.ran("docker compose ls") == 2
	})
	// Down asks first.
	state.Click(hitRemoteDown)
	if state.modal != modalForm || !strings.Contains(state.form.description, "batch") {
		t.Fatalf("no confirmation: modal %d", state.modal)
	}
	state.Click(hitFormCancel)
	if server.ran("docker compose -p batch down") != 0 {
		t.Fatal("cancelling still ran down")
	}
	state.Click(hitRemoteDown)
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return server.ran("docker compose -p batch down") == 1 })
}

func TestAServerWithoutComposeSaysSo(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	if got := state.remoteErrorText(remotetools.ErrNoCompose); got != state.messages.Tools.RemoteErrNoCompose {
		t.Fatalf("text %q", got)
	}
}

func TestPullingAnImageTypesTheCommandIntoTheTerminal(t *testing.T) {
	state, server, shell := openRemoteToolsDialog(t)
	state.Click(hitRemoteTabImages)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitRemotePull); !ok {
		t.Fatal("the images tab has no Pull")
	}
	if _, ok := hitByID(&canvas, hitRemoteCreate); ok {
		t.Fatal("the images tab offers to create one")
	}

	state.Click(hitRemotePull)
	if state.modal != modalForm {
		t.Fatalf("no question: modal %d", state.modal)
	}
	state.form.set(0, "nginx; reboot")
	state.Click(hitFormSubmit)
	if state.form.dialogError != state.messages.Tools.RemoteBadImage || shell.typed() != "" {
		t.Fatalf("a bad reference: error %q, typed %q", state.form.dialogError, shell.typed())
	}
	state.form.set(0, "ghcr.io/org/app:1.2")
	state.Click(hitFormSubmit)
	if state.modal != modalNone || !strings.Contains(shell.typed(), "docker pull ghcr.io/org/app:1.2\r") {
		t.Fatalf("modal %d, typed %q", state.modal, shell.typed())
	}
	if server.ran("docker pull") != 0 {
		t.Fatal("the pull ran in the background instead of the terminal")
	}
}

func TestCreatingAVolumeAndANetworkFromTheirTabs(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	for _, entry := range []struct {
		tab     ui.ID
		title   string
		command string
	}{
		{hitRemoteTabVolumes, state.messages.Tools.RemoteCreateVolumeTitle, "docker volume create scratch"},
		{hitRemoteTabNetworks, state.messages.Tools.RemoteCreateNetworkTitle, "docker network create scratch"},
	} {
		state.Click(entry.tab)
		waitFor(t, state, func() bool { return !state.remote.loading })
		var canvas ui.Canvas
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
		if _, ok := hitByID(&canvas, hitRemotePull); ok {
			t.Fatal("a tab that creates offers a pull")
		}
		state.Click(hitRemoteCreate)
		if state.modal != modalForm || state.form.title != entry.title {
			t.Fatalf("modal %d, form %q, want %q", state.modal, state.form.title, entry.title)
		}
		state.form.set(0, "no good")
		state.Click(hitFormSubmit)
		if state.form.dialogError != state.messages.Tools.RemoteBadName || server.ran(entry.command) != 0 {
			t.Fatalf("a bad name: error %q", state.form.dialogError)
		}
		state.form.set(0, "scratch")
		state.Click(hitFormSubmit)
		waitFor(t, state, func() bool { return server.ran(entry.command) == 1 && !state.remote.loading })
		if state.modal != modalRemoteTools {
			t.Fatalf("after creating, modal %d, want the tools again", state.modal)
		}
	}
}
