package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

type remoteWindowHost struct{ windows []*remoteNativeWindow }
type remoteNativeWindow struct {
	config app.Config
	closed func()
	done   bool
	shows  int
}

func (w *remoteNativeWindow) Close() {
	if !w.done {
		w.done = true
		if w.config.OnClosed != nil {
			w.config.OnClosed()
		}
		w.closed()
	}
}
func (w *remoteNativeWindow) Hide() {}
func (w *remoteNativeWindow) Show() { w.shows++ }
func (w *remoteNativeWindow) Frame() (gfx.Rect, bool) {
	return gfx.Rect{Width: float32(w.config.Width), Height: float32(w.config.Height)}, true
}
func (h *remoteWindowHost) OpenWindow(config app.Config, _ *gfx.Canvas, closed func()) (app.WindowControl, error) {
	w := &remoteNativeWindow{config: config, closed: closed}
	h.windows = append(h.windows, w)
	return w, nil
}
func (h *remoteWindowHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("not used")
}
func (h *remoteWindowHost) SetDockBadge(string) {}
func (h *remoteWindowHost) SetProgress(float32) {}

func (h *remoteWindowHost) Quit() {
	for _, w := range h.windows {
		w.Close()
	}
}
func (h *remoteWindowHost) until(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range h.windows {
			if !w.done && w.config.OnWake != nil {
				w.config.OnWake()
			}
		}
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("remote window operation did not settle")
}

func withRemoteWindowApplication(t *testing.T, run func(*app.Application, *remoteWindowHost)) {
	t.Helper()
	host := &remoteWindowHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		run(application, host)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteNativeWindowSudoPromptUsesCapturedSession(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		server := &remoteServer{failures: map[string]error{}}
		fake.runCommand = server.run
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		connection := &sudoRunnerTestConn{fakeConn: fake, run: server.run}
		session.conn = connection
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		if len(state.remoteWindows) != 1 {
			t.Fatal("native tools window did not open")
		}
		window := state.remoteWindows[0]
		host.until(t, func() bool { return !window.ui.remote.loading })
		window.click(hitRemoteSudo)
		if window.ui.modal != modalSudoSFTP || state.modal != modalNone || session.sudoCommands {
			t.Fatal("native window did not request its own authorization")
		}
		var canvas gfx.Canvas
		window.paint(&canvas, gfx.Size{Width: 1100, Height: 780})
		if _, ok := hitByID(&canvas, hitSudoSFTPPassword); !ok {
			t.Fatal("native window did not paint the sudo password field")
		}
		if input := host.windows[0].config.TextInput(app.TextInputContext{ID: hitSudoSFTPPassword}); input == nil || input.Editor != &window.ui.sudoSFTP.password {
			t.Fatal("native password field cannot receive text input")
		}
		window.click(hitSudoSFTPCancel)
		if window.ui.modal != modalRemoteTools || session.sudoCommands {
			t.Fatal("canceling native authorization changed the source")
		}
		window.click(hitRemoteSudo)
		window.ui.sudoSFTP.password.Insert("native-secret")
		window.click(hitSudoSFTPEnable)
		host.until(t, func() bool { return !window.ui.remote.loading })
		if !session.sudoCommands || session.sudoPassword != "native-secret" || !window.session.sudoCommands {
			t.Fatal("native authorization did not synchronize with captured source")
		}
		connection.mu.Lock()
		password, calls := connection.password, len(connection.commands)
		connection.mu.Unlock()
		if password != "native-secret" || calls == 0 {
			t.Fatalf("native sudo runner: password=%q calls=%d", password, calls)
		}
		window.click(hitRemoteSudo)
		if session.sudoCommands || session.sudoPassword != "" || window.session.sudoCommands || window.session.sudoPassword != "" {
			t.Fatal("native window did not discard sudo authorization")
		}
		window.click(hitRemoteSudo)
		window.ui.sudoSFTP.password.Insert("pending-secret")
		window.close()
		if window.ui.sudoSFTP.password.String() != "" || session.sudoPassword != "" {
			t.Fatal("closing native tools retained the pending sudo password")
		}
	})
}

func TestRemoteNativeWindowDoesNotBlockTheWorkbenchAndKeepsItsServer(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, world, fake, shell := liveSession(t)
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		source := state.activeSessionID
		state.Click(hitRemoteToolsOpen)
		if state.modal != modalNone || len(state.remoteWindows) != 1 || len(host.windows) != 1 {
			t.Fatal("opening native tools blocked the workbench or failed to create a window")
		}
		w := state.remoteWindows[0]
		if config := host.windows[0].config; config.MinWidth != remoteToolsMinWidth || config.MinHeight != remoteToolsMinHeight {
			t.Fatalf("remote tools window lost its native size limits: %+v", config)
		}
		if config := host.windows[0].config; config.WindowMode != app.WindowCustomTitlebar || config.TitlebarHeight != TitlebarHeight {
			t.Fatalf("remote tools window lost its custom titlebar: %+v", config)
		}
		host.until(t, func() bool { return !w.ui.remote.loading })
		var canvas gfx.Canvas
		w.paint(&canvas, gfx.Size{Width: 1100, Height: 780})
		movable := false
		for _, hit := range canvas.Hits() {
			movable = movable || hit.MovesWindow && hit.Bounds.Y == 0 && hit.Bounds.Height == TitlebarHeight
		}
		if !movable {
			t.Fatal("remote tools titlebar cannot drag the window")
		}
		if tab, ok := hitByID(&canvas, hitRemoteTabProcesses); !ok || tab.Bounds.Y < TitlebarHeight {
			t.Fatal("remote tools tabs overlap the window titlebar")
		}
		if len(w.ui.remote.rows) != 3 || !strings.Contains(host.windows[0].config.Title, state.sessions[0].Address) {
			t.Fatal("native window lost source data or identity")
		}
		state.Click(hitRemoteToolsOpen)
		if len(host.windows) != 1 || host.windows[0].shows != 1 {
			t.Fatal("same session opened a duplicate native window")
		}
		other := liveTab(t, state, world, 1)
		otherServer := &remoteServer{}
		other.mu.Lock()
		other.runCommand = otherServer.run
		other.mu.Unlock()
		state.Click(hitRemoteToolsOpen)
		if len(host.windows) != 2 || state.modal != modalNone {
			t.Fatal("different session did not get an independent non-modal window")
		}
		// Destructive confirmation belongs to the utility, while the main
		// window can still accept ordinary terminal input in its other tab.
		host.until(t, func() bool { return !w.ui.remote.loading })
		selectRemoteToolsWindowRow(w, 0)
		w.click(hitRemoteKill)
		if w.ui.modal != modalForm || state.modal != modalNone {
			t.Fatal("confirmation escaped into the workbench")
		}
		if result := key(state, len(state.sessions)-1, gfx.KeyEvent{Text: "echo free\r"}); !result.Handled {
			t.Fatal("main terminal was blocked by native confirmation")
		}
		w.click(hitFormSubmit)
		host.until(t, func() bool { return !w.ui.remote.loading })
		if server.ran("kill -KILL 2211") != 1 || otherServer.ran("kill") != 0 {
			t.Fatal("confirmation operated on the selected main tab instead of the captured server")
		}
		w.click(hitRemoteTabImages)
		host.until(t, func() bool { return !w.ui.remote.loading })
		w.click(hitRemotePull)
		input := host.windows[0].config.TextInput(app.TextInputContext{ID: hitFormFieldList.At(0)})
		if input == nil || input.Editor == nil {
			t.Fatal("native image form has no platform text input")
		}
		input.Editor.Insert("alpine:latest")
		w.key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitFormFieldList.At(0)})
		shell.mu.Lock()
		actual := string(shell.input)
		shell.mu.Unlock()
		if !strings.Contains(actual, "docker pull alpine:latest\r") || state.activeSessionID != source || w.ui.modal != modalRemoteTools || w.closed {
			t.Fatalf("pull did not target original terminal and keep tools open: %q", actual)
		}
		state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
		openHostInSplit(t, state, state.hosts[1].ID, layoutcore.Horizontal)
		peer := world.conns[len(world.conns)-1]
		peer.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
		state.Wake()
		peerShell := peer.shell(t)
		peerShell.say("\x1b[0m")
		state.Wake()
		state.broadcast = state.deck.ActiveWorkspace()
		w.click(hitRemoteTabContainers)
		host.until(t, func() bool { return !w.ui.remote.loading })
		selectRemoteToolsWindowRow(w, 0)
		w.ui.remote.auto.enabled = true
		w.ui.remote.auto.restart()
		w.click(hitRemoteLogs)
		if !strings.Contains(shell.typed(), "docker logs") || strings.Contains(peerShell.typed(), "docker logs") ||
			!w.ui.remote.auto.enabled || w.ui.modal != modalRemoteTools {
			t.Fatal("tool command was broadcast into another pane or disabled the tool surface")
		}
		w.close()
		fake.mu.Lock()
		canceled := fake.canceled
		fake.mu.Unlock()
		if canceled != 0 || !host.windows[0].done {
			t.Fatal("closing tools disconnected SSH or left native window alive")
		}
	})
}

func TestRemoteNativeWindowCloseCancelsWorkAndRejectsLateResults(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		started, stopped := make(chan struct{}), make(chan struct{})
		fake.mu.Lock()
		fake.runCommand = func(ctx context.Context, _ string, _ int) ([]byte, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return []byte(processFixture), nil
		}
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("remote listing did not start")
		}
		host.windows[0].Close() // native title-bar close, not an in-app action
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
			t.Fatal("closing native tools left the remote request running")
		}
		if !w.closed || len(state.remoteWindows) != 0 || w.window.Post(func() { t.Error("late update ran") }) {
			t.Fatal("closed utility retained state or accepted a late update")
		}
		fake.mu.Lock()
		fake.runCommand = (&remoteServer{}).run
		fake.mu.Unlock()
		state.Click(hitRemoteToolsOpen)
		state.closeSession(state.activeSessionID)
		if !host.windows[1].done || len(state.remoteWindows) != 0 {
			t.Fatal("closing the source left an orphan tool window")
		}
	})
}

func TestRemoteProcessButtonOpensProcessTabAndReusesToolsWindow(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.remote.tab = remoteContainers

		state.Click(hitProcessToolsOpen)
		if len(host.windows) != 1 || len(state.remoteWindows) != 1 {
			t.Fatal("process button did not open the shared tools window")
		}
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		if w.ui.remote.tab != remoteProcesses || state.modal != modalNone {
			t.Fatalf("process button opened tab %v with owner modal %v", w.ui.remote.tab, state.modal)
		}

		w.click(hitRemoteTabContainers)
		host.until(t, func() bool { return !w.ui.remote.loading && w.ui.remote.tab == remoteContainers })
		state.Click(hitProcessToolsOpen)
		host.until(t, func() bool { return !w.ui.remote.loading && w.ui.remote.tab == remoteProcesses })
		if len(host.windows) != 1 || host.windows[0].shows != 1 || w.ui.remote.tab != remoteProcesses {
			t.Fatal("process button duplicated the window or failed to return to its tab")
		}
	})
}

func TestRemoteNativeWindowContainerShellUsesItsOriginalConnection(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		world, options := newSharingWorld(t)
		options.OpenContentWindow = application.OpenContentWindow
		state := newTestState(t, options)
		original := world.connectFirst(t, state)
		source, _ := state.sessionByIDPointer(state.activeSessionID)
		source.term.live = true
		server := &remoteServer{}
		original.mu.Lock()
		original.runCommand = server.run
		original.mu.Unlock()
		state.openRemoteTools(source)
		w := state.remoteWindows[0]
		w.click(hitRemoteTabContainers)
		host.until(t, func() bool { return !w.ui.remote.loading })
		state.activeSessionID = 0
		selectRemoteToolsWindowRow(w, 0)
		w.ui.remote.auto.enabled = true
		w.ui.remote.auto.restart()
		w.click(hitRemoteShell)
		host.until(t, func() bool { return !w.ui.remote.loading })
		if w.ui.remote.failed || len(world.conns) != 1 || len(world.shared) != 1 || len(state.sessions) != 2 ||
			state.modal != modalNone || w.closed || !w.ui.remote.auto.enabled || w.ui.modal != modalRemoteTools {
			t.Fatal("container shell lost its original SSH connection or closed tools", w.ui.remote.status)
		}
		if len(world.shared[0].configs) != 1 || !strings.HasPrefix(world.shared[0].configs[0].Command, "docker exec -it 0f1e2d3c4b5a") {
			t.Fatal("container shell was not started in the new tab")
		}
		state.Close()
		if !w.closed || !host.windows[0].done {
			t.Fatal("closing the workbench left its utility window alive")
		}
	})
}

func TestRemoteNativeWindowFollowsItsSessionBetweenWorkbenchWindows(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		pair := newTwoWindows(t)
		fake := liveTab(t, pair.first, pair.firstW, 0)
		liveTab(t, pair.second, pair.secondW, 1) // consumes the old session ID in destination
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		pair.first.options.OpenContentWindow = application.OpenContentWindow
		pair.second.options.OpenContentWindow = application.OpenContentWindow
		pair.first.Click(hitRemoteToolsOpen)
		w := pair.first.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		payload, ok := pair.first.detachWorkspace(pair.first.deck.ActiveWorkspace())
		if !ok || !pair.second.attachPayload(payload, tabDrop{kind: dropReorder}) {
			t.Fatal("session did not move")
		}
		pair.first.Close()
		if w.closed {
			t.Fatal("old workbench closed a utility now owned by another window")
		}
		pair.second.Click(hitRemoteToolsOpen)
		if w.owner != pair.second || len(host.windows) != 1 || host.windows[0].shows != 1 {
			t.Fatal("moving a session detached or duplicated its utility window")
		}
		host.until(t, func() bool { return !w.ui.remote.loading })
		selectRemoteToolsWindowRow(w, 0)
		w.click(hitRemoteNiceLower)
		host.until(t, func() bool { return !w.ui.remote.loading })
		if server.ran("renice 10 -p 2211") != 1 {
			t.Fatal("moved source lost its tool actions")
		}
		fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseFailed })
		pair.second.Wake()
		if !w.closed || !host.windows[0].done {
			t.Fatal("disconnected source left its tool window alive")
		}
	})
}

func TestRemoteNativeWindowReopenAndRestoreReadTheHostAgain(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		server := &remoteServer{containers: containerFixture}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		w.click(hitRemoteTabContainers)
		host.until(t, func() bool { return !w.ui.remote.loading })
		if len(w.ui.remote.rows) != 2 {
			t.Fatalf("initial rows = %d", len(w.ui.remote.rows))
		}

		// A container created after the snapshot must not look deleted forever:
		// asking for the tools again reads the host again.
		server.setContainers(containerFixture + "c3c3c3c3c3c3|new|alpine:3.19|running|Up 1 minute\n")
		state.Click(hitRemoteToolsOpen)
		host.until(t, func() bool { return !w.ui.remote.loading && len(w.ui.remote.rows) == 3 })

		// Restoring the window is looking at it again too.
		server.setContainers(containerFixture)
		host.windows[0].config.OnMinimize(false)
		host.until(t, func() bool { return !w.ui.remote.loading && len(w.ui.remote.rows) == 2 })
	})
}

func TestRemoteNativeWindowResizesWithReachableActionsAndVirtualScroll(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, _, fake, _ := liveSession(t)
		fake.mu.Lock()
		fake.runCommand = (&remoteServer{}).run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		w.ui.remote.procs = append(w.ui.remote.procs, w.ui.remote.procs...)
		w.ui.remote.procs = append(w.ui.remote.procs, w.ui.remote.procs...)
		w.ui.remote.procs = append(w.ui.remote.procs, w.ui.remote.procs...)
		w.ui.rebuildRemoteRows()
		for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
			state.setLanguage(language)
			for _, size := range []gfx.Size{{Width: 600, Height: 500}, {Width: 1100, Height: 780}, {Width: 1400, Height: 900}} {
				w.ui.remote.processScroll = 0
				var canvas gfx.Canvas
				w.paint(&canvas, size)
				for _, id := range []gfx.ID{hitRemoteClose, hitRemoteRefresh, hitRemoteKill, hitRemoteNiceHigher, hitRemoteProcessScroll} {
					hit, ok := hitByID(&canvas, id)
					if !ok || hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width+.01 || hit.Bounds.Y+hit.Bounds.Height > size.Height+.01 {
						t.Fatalf("language %v viewport %+v: control %v is unreachable: %+v", language, size, id, hit)
					}
				}
				if _, ok := hitByID(&canvas, hitRemoteNext); ok {
					t.Fatal("process list still paints pagination controls")
				}
				bounds := w.ui.remote.processTableBounds
				before := w.ui.remote.processScroll
				result, handled := w.ui.remoteProcessWheel(gfx.ScrollEvent{
					X: bounds.X + bounds.Width/2, Y: bounds.Y + min(bounds.Height/2, 40),
					DeltaY: 20, Precise: true, Viewport: size,
				})
				if !handled || !result.Handled || w.ui.remote.processScroll <= before {
					t.Fatalf("language %v viewport %+v: virtual scroll did not move: handled=%v result=%+v", language, size, handled, result)
				}
			}
		}
	})
}
