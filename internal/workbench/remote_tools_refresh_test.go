package workbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestRemoteAutoRefreshIsOptInAndSkipsBusyOrConfirming(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	state.tickRemoteRefresh(time.Hour)
	if server.ran("LC_ALL=C ps") != 1 {
		t.Fatal("auto refresh ran before being enabled")
	}
	clickRemoteToolsInterval(t, state, 0)
	state.tickRemoteRefresh(5*time.Second - time.Millisecond)
	if state.remote.loading {
		t.Fatal("refresh ran before its interval")
	}
	selectRemoteToolsRow(state, 0)
	selected := state.remote.selected
	state.tickRemoteRefresh(5 * time.Second)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if server.ran("LC_ALL=C ps") != 2 || state.remote.selected != selected {
		t.Fatal("timed refresh lost selection or did not run")
	}
	state.remote.loading = true
	state.tickRemoteRefresh(10 * time.Second)
	state.remote.loading = false
	state.tickRemoteRefresh(10 * time.Second)
	if state.remote.loading || server.ran("LC_ALL=C ps") != 2 {
		t.Fatal("busy tick overlapped a request or was replayed")
	}
	clickRemoteToolsAction(t, state, hitRemoteKill)
	state.tickRemoteRefresh(15 * time.Second)
	if state.modal != modalForm || state.remote.loading || server.ran("kill") != 0 {
		t.Fatal("auto refresh interfered with destructive confirmation")
	}
	state.Click(hitFormCancel)
	state.tickRemoteRefresh(time.Hour) // one catch-up request, never a burst
	waitFor(t, state, func() bool { return !state.remote.loading })
	if server.ran("LC_ALL=C ps") != 3 || state.remote.selected != selected {
		t.Fatal("resuming the utility replayed ticks or lost its selected PID")
	}
	clickRemoteToolsAutoItem(t, state, remoteToolsAutoToggleLabel(state))
	state.tickRemoteRefresh(2 * time.Hour)
	if state.remote.loading {
		t.Fatal("stopping auto refresh left it running")
	}
	clickRemoteToolsInterval(t, state, 4)
	if !state.remote.auto.enabled || state.remote.auto.period() != 5*time.Minute {
		t.Fatal("selecting an interval did not restart auto refresh")
	}
	clickRemoteToolsAction(t, state, hitRemoteClose)
	state.tickRemoteRefresh(3 * time.Hour)
	if state.remote.auto.enabled || state.remote.loading {
		t.Fatal("closed dialog continued refreshing")
	}
}

func TestRemoteAutoRefreshRunsFromAnimationFramesOnCapturedConnection(t *testing.T) {
	withRemoteWindowApplication(t, func(application *app.Application, host *remoteWindowHost) {
		state, world, fake, _ := liveSession(t)
		server := &remoteServer{}
		fake.mu.Lock()
		fake.runCommand = server.run
		fake.mu.Unlock()
		state.options.OpenContentWindow = application.OpenContentWindow
		state.Click(hitRemoteToolsOpen)
		w := state.remoteWindows[0]
		host.until(t, func() bool { return !w.ui.remote.loading })
		other := liveTab(t, state, world, 1)
		otherServer := &remoteServer{}
		other.mu.Lock()
		other.runCommand = otherServer.run
		other.mu.Unlock()
		w.act(hitRemoteIntervals.At(0))
		var canvas gfx.Canvas
		now := 5 * time.Second
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame() // a plain render, not a click's event pass
		w.paint(&canvas, gfx.Size{Width: 1100, Height: 780})
		canvas.EndFrame()
		host.until(t, func() bool { return server.ran("LC_ALL=C ps") == 2 && !w.ui.remote.loading })
		if otherServer.ran("LC_ALL=C ps") != 0 {
			t.Fatal("auto refresh followed the active tab instead of its captured server")
		}
		w.close()
		w.ui.tickRemoteRefresh(time.Hour)
		if server.ran("LC_ALL=C ps") != 2 {
			t.Fatal("closed native utility continued refreshing")
		}
	})
}

func TestRemoteAutoRefreshClockSchedulingAndMenuKeyboard(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.auto.enabled = true
	state.remote.auto.interval = 10 * time.Second
	state.remote.auto.restart()
	canvas := remoteToolsPaint(t, state)
	hits := 0
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleButton && strings.HasPrefix(hit.Label, state.messages.Tools.RemoteAutoRefresh) {
			hits++
			if hit.Label == "" {
				t.Fatal("auto refresh button lacks an accessible name")
			}
		}
	}
	if hits != 1 {
		t.Fatalf("auto refresh declared %d controls for one button", hits)
	}

	theme := state.Theme()
	for _, reduced := range []bool{false, true} {
		var frame gfx.Canvas
		frame.SetReduceMotion(reduced)
		frame.SetClock(func() time.Duration { return time.Second })
		state.drawRemoteAutoControl(&ui.DrawContext{
			Canvas: &frame, Theme: &theme, Rect: gfx.Rect{Width: 174, Height: 34},
		})
		delay, scheduled := frame.NextFrame()
		if !scheduled || (reduced && delay != 9*time.Second) || (!reduced && delay > time.Second/6) {
			t.Fatalf("reduced=%v: inappropriate frame cadence %v, %v", reduced, scheduled, delay)
		}
	}

	state.remote.auto.enabled = false
	state.remote.auto.interval = 0
	openRemoteToolsAutoMenu(t, state)
	var focus gfx.ID
	for range 2 {
		result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyDown, Target: focus})
		if !result.Handled {
			t.Fatal("Down did not navigate the interval menu")
		}
		focus = result.Focus
	}
	if focus.IsZero() {
		t.Fatal("the interval menu did not move its focus")
	}
	clickRemoteToolsPageHit(t, state, gfx.Hit{ID: focus})
	if !state.remote.auto.enabled || state.remote.auto.period() != 5*time.Second {
		t.Fatalf("menu keyboard selection: enabled=%v period=%v", state.remote.auto.enabled, state.remote.auto.period())
	}
	openRemoteToolsAutoMenu(t, state)
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "remote auto refresh", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnKey: state.Key, OnShortcut: state.Shortcut, OnPointer: state.Pointer,
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.remote.auto.open || state.modal != modalRemoteTools {
		t.Fatal("Escape closed the utility instead of its interval menu")
	}
}

func TestRemoteDockerPlaceholderRecoversAndDoesNotHideOtherErrors(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.mu.Lock()
	server.failures["docker ps"] = errors.New("exit status 127")
	server.mu.Unlock()
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if len(state.remote.rows) != 0 {
		t.Fatal("initial Docker detection reused process rows")
	}
	for _, label := range canvas.Texts() {
		if label.Value == state.messages.Tools.RemoteColID {
			t.Fatal("initial Docker detection flashed a table header")
		}
	}
	waitFor(t, state, func() bool { return !state.remote.loading })
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !state.remote.noDocker || !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteDockerMissing) {
		t.Fatal("missing Docker has no illustrated empty state")
	}
	server.mu.Lock()
	requests := len(server.commands)
	server.mu.Unlock()
	for _, label := range []string{state.messages.Tools.RemoteTabImages, state.messages.Tools.RemoteTabVolumes, state.messages.Tools.RemoteTabNetworks, state.messages.Tools.RemoteTabCompose, state.messages.Tools.RemoteTabContainers} {
		clickSettingsChoice(t, state, label)
		canvas.Reset()
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		if state.remote.loading || !state.remote.noDocker || !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteDockerMissing) {
			t.Fatal("switching Docker tabs flashed a table or discarded the known empty state")
		}
	}
	server.mu.Lock()
	unchanged := len(server.commands) == requests
	server.mu.Unlock()
	if !unchanged {
		t.Fatal("Docker tabs repeated commands despite a known missing installation")
	}
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	selectRemoteToolsRow(state, 0)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !state.remote.noDocker || remoteCanvasHasText(&canvas, state.messages.Tools.RemoteDockerMissing) {
		t.Fatal("processes lost the Docker availability cache or displayed the wrong empty state")
	}
	clickRemoteToolsRow(t, state, 0)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByLabelRoleLast(&canvas, state.messages.Tools.RemoteForceKill, gfx.RoleButton); !ok || hit.Disabled {
		t.Fatal("missing Docker disabled unrelated process operations")
	}
	state.closeRemoteProcessDetails()
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	if state.remote.loading {
		t.Fatal("returning from processes did not reuse the known Docker empty state")
	}
	server.mu.Lock()
	delete(server.failures, "docker ps")
	server.mu.Unlock()
	clickRemoteToolsAction(t, state, hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.noDocker || len(state.remote.rows) != 2 {
		t.Fatal("installing Docker and refreshing did not restore the table")
	}
	fake := state.sessions[0].conn.(*fakeConn)
	fake.mu.Lock()
	fake.runCommand = func(context.Context, string, int) ([]byte, error) {
		return []byte("permission denied while trying to connect to the Docker daemon"), errors.New("exit status 1")
	}
	fake.mu.Unlock()
	clickRemoteToolsAction(t, state, hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if !state.remote.failed || state.remote.noDocker {
		t.Fatal("Docker permission failure was incorrectly called a missing installation")
	}
}

func TestRemoteToolbarAndTableHeadersAcrossScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, _, fake, _ := liveSession(t)
			server := &remoteServer{}
			fake.mu.Lock()
			fake.runCommand = server.run
			fake.mu.Unlock()
			host := &sftpNativeHost{scale: scale}
			app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
			t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
			if err := app.RunApplication(func(application *app.Application) error {
				state.options.OpenContentWindow = application.OpenContentWindow
				state.Click(hitRemoteToolsOpen)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			w, view := state.remoteWindows[0], host.windows[0].view
			deadline := time.Now().Add(3 * time.Second)
			for w.ui.remote.loading && time.Now().Before(deadline) {
				view.Driver().Pump()
				time.Sleep(time.Millisecond)
			}
			if w.ui.remote.loading {
				t.Fatal("initial remote list did not load")
			}
			for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
				state.setLanguage(language)
				for _, size := range []gfx.Size{{Width: 600, Height: 500}, {Width: 1100, Height: 780}} {
					if err := view.Resize(int(size.Width), int(size.Height)); err != nil {
						t.Fatal(err)
					}
					w.window.Invalidate()
					view.Driver().Pump()
					var canvas gfx.Canvas
					canvas.SetPixelScale(float32(scale))
					w.paint(&canvas, size)
					tab, ok := settingsChoiceIn(&canvas, state.messages.Tools.RemoteTabProcesses)
					if !ok {
						t.Fatalf("language=%v size=%v: no process tab", language, size)
					}
					for _, control := range []struct {
						label string
						id    gfx.ID
					}{
						{state.messages.Tools.RemoteRefresh, gfx.ID{}},
						{state.messages.Tools.RemoteAutoRefresh, gfx.ID{}},
						{state.messages.Tools.RemoteSortCPU, hitRemoteSortCPU},
						{state.messages.Tools.RemoteSortMemory, hitRemoteSortMemory},
						{state.messages.Tools.RemoteClose, gfx.ID{}},
					} {
						var hit gfx.Hit
						var ok bool
						if control.id.IsZero() {
							hit, ok = hitByLabelRole(&canvas, control.label, gfx.RoleButton)
							if !ok {
								hit, ok = hitByLabelPrefix(&canvas, control.label, gfx.RoleButton)
							}
						} else {
							hit, ok = hitByID(&canvas, control.id)
						}
						if !ok || hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width+.01 || hit.Bounds.Y+hit.Bounds.Height > size.Height+.01 {
							t.Fatalf("language=%v size=%v: unreachable control %q: %+v", language, size, control.label, hit)
						}
						if (control.id == hitRemoteSortCPU || control.id == hitRemoteSortMemory) && hit.Bounds.Y <= tab.Bounds.Y+tab.Bounds.Height {
							t.Fatal("sort control still belongs to the tab toolbar")
						}
						if control.id.IsZero() && hit.Bounds.Y < tab.Bounds.Y {
							t.Fatal("refresh control is above the tab row")
						}
					}
					// The window's own titlebar names the tool; what must not
					// repeat is the content header under it.
					if remoteContentHasText(&canvas, state.messages.Tools.RemoteTitle) || remoteContentHasText(&canvas, fmt.Sprintf(state.messages.Tools.RemoteDescriptionFmt, w.session.Title)) {
						t.Fatal("redundant content title/subtitle is still painted")
					}
				}
			}
			// Exercise the actual native-window event path, including keyboard
			// activation and virtual timer frames, rather than calling handlers.
			click := func(id gfx.ID) {
				t.Helper()
				box := dualHit(t, view, id)
				if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			clickChoice := func(label string) {
				t.Helper()
				hit := dualChoiceHit(t, view, label)
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			clickLabel := func(label string) {
				t.Helper()
				hit := dualLabelHit(t, view, label, gfx.RoleButton)
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
			}
			settle := func() {
				t.Helper()
				deadline := time.Now().Add(3 * time.Second)
				for w.ui.remote.loading && time.Now().Before(deadline) {
					view.Driver().Pump()
					time.Sleep(time.Millisecond)
				}
				if w.ui.remote.loading {
					t.Fatal("refresh did not settle")
				}
				view.Driver().Pump()
			}
			selectRemoteToolsRow(state, 0)
			selected := w.ui.remote.selected
			click(hitRemoteSortMemory)
			settle()
			if w.ui.remote.sort != 1 || w.ui.remote.selected != selected || w.ui.remote.rows[0].cells[0] != "1" {
				t.Fatal("table header sorting lost its selected PID or sorted incorrectly")
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if w.ui.remote.rows[0].cells[0] != "3000" {
				t.Fatal("keyboard activation did not switch the header to descending")
			}
			if err := view.Keys("Space"); err != nil {
				t.Fatal(err)
			}
			if w.ui.remote.rows[0].cells[0] != "1" {
				t.Fatal("Space did not switch the sortable header to ascending")
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			clickLabel(state.messages.Tools.RemoteAutoRefresh)
			// The first Down reaches the start/stop item, the next the first
			// interval, as a page Menu navigates from its trigger.
			for range 2 {
				if err := view.Keys("Down"); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if !w.ui.remote.auto.enabled || w.ui.remote.auto.period() != 5*time.Second {
				t.Fatalf("refresh menu keyboard selection: enabled=%v period=%v focus=%v", w.ui.remote.auto.enabled, w.ui.remote.auto.period(), view.Snapshot().Focused)
			}
			before := server.ran("LC_ALL=C ps")
			if err := view.Driver().Advance(5 * time.Second); err != nil {
				t.Fatal(err)
			}
			view.Driver().Pump()
			settle()
			if server.ran("LC_ALL=C ps") != before+1 {
				t.Fatal("timer animation frames did not trigger exactly one refresh")
			}
			if dir := os.Getenv("MOTTY_REMOTE_SCREENSHOT_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				state.setLanguage(i18n.Chinese)
				view.Resize(1100, 780)
				w.window.Invalidate()
				view.Driver().Pump()
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("remote-tools-%g.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
				clickLabel(state.messages.Tools.RemoteAutoRefresh)
				if err := view.Driver().Advance(250 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("remote-refresh-menu-%g.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
				clickChoice(state.messages.Tools.RemoteAutoStop)
				view.Driver().Advance(250 * time.Millisecond)
				server.mu.Lock()
				server.failures = map[string]error{"docker ps": errors.New("exit status 127")}
				server.mu.Unlock()
				clickChoice(state.messages.Tools.RemoteTabContainers)
				settle()
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("remote-docker-empty-%g.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
				state.setAppearance(appearanceLight)
				w.window.Invalidate()
				view.Driver().Pump()
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("remote-docker-empty-light-%g.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
			}
		})
	}
}

func remoteCanvasHasText(canvas *gfx.Canvas, text string) bool {
	for _, painted := range canvas.Texts() {
		if strings.Contains(painted.Value, text) {
			return true
		}
	}
	return false
}

// remoteContentHasText is remoteCanvasHasText for the area under the window's
// titlebar: that bar carries the tool's own name, so only a repeat inside the
// content says the redundant header is back.
func remoteContentHasText(canvas *gfx.Canvas, text string) bool {
	for _, painted := range canvas.Texts() {
		if painted.Bounds.Y < TitlebarHeight {
			continue
		}
		if strings.Contains(painted.Value, text) {
			return true
		}
	}
	return false
}

// Refreshing from another tab must forget a stale "Docker is missing" answer,
// so the next Docker tab probes again instead of reusing the empty state.
func TestRemoteDockerRefreshFromAnotherTabProbesAgain(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.mu.Lock()
	server.failures["docker ps"] = errors.New("exit status 127")
	server.mu.Unlock()
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && state.remote.noDocker })

	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	server.mu.Lock()
	delete(server.failures, "docker ps")
	server.mu.Unlock()
	clickRemoteToolsAction(t, state, hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })

	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	if state.remote.noDocker {
		t.Fatal("installed Docker was still reported missing")
	}
}
