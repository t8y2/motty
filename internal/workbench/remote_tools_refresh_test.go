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
	state.Click(hitRemoteIntervals.At(0))
	state.tickRemoteRefresh(5*time.Second - time.Millisecond)
	if state.remote.loading {
		t.Fatal("refresh ran before its interval")
	}
	state.Click(hitRemoteRowList.At(0))
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
	state.Click(hitRemoteKill)
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
	state.Click(hitRemoteAutoToggle)
	state.tickRemoteRefresh(2 * time.Hour)
	if state.remote.loading {
		t.Fatal("stopping auto refresh left it running")
	}
	state.Click(hitRemoteIntervals.At(4))
	if !state.remote.auto.enabled || state.remote.auto.period() != 5*time.Minute {
		t.Fatal("selecting an interval did not restart auto refresh")
	}
	state.Click(hitRemoteClose)
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
		w.click(hitRemoteIntervals.At(0))
		var canvas ui.Canvas
		now := 5 * time.Second
		canvas.SetClock(func() time.Duration { return now })
		canvas.BeginFrame() // a plain render, not a click's event pass
		w.paint(&canvas, ui.Size{Width: 1100, Height: 780})
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
	state.Click(hitRemoteIntervals.At(1))
	for _, reduced := range []bool{false, true} {
		var canvas ui.Canvas
		canvas.SetReduceMotion(reduced)
		canvas.SetClock(func() time.Duration { return time.Second })
		canvas.BeginFrame()
		state.paintRemoteRefresh(&canvas, ui.Rect{Width: 174, Height: 34})
		canvas.EndFrame()
		delay, scheduled := canvas.NextFrame()
		if !scheduled || (reduced && delay != 9*time.Second) || (!reduced && (delay <= 0 || delay > time.Second/6)) {
			t.Fatalf("reduced=%v: inappropriate frame cadence %v, %v", reduced, delay, scheduled)
		}
		hits := 0
		for _, hit := range canvas.Hits() {
			if hit.ID == hitRemoteAutoRefresh {
				hits++
				if hit.Label == "" || !hit.Expandable {
					t.Fatal("auto refresh button lacks an accessible name/state")
				}
			}
		}
		if hits != 1 {
			t.Fatalf("auto refresh declared %d controls for one button", hits)
		}
	}
	result := state.remoteRefreshKey(ui.KeyEvent{Target: hitRemoteAutoRefresh, Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitRemoteAutoToggle {
		t.Fatal("keyboard could not open the interval menu")
	}
	var canvas ui.Canvas
	state.paintRemoteRefreshMenu(&canvas)
	result = state.remoteRefreshKey(ui.KeyEvent{Target: hitRemoteAutoToggle, Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitRemoteIntervals.At(0) {
		t.Fatal("arrow keys cannot navigate refresh intervals")
	}
	result = state.remoteRefreshKey(ui.KeyEvent{Key: ui.KeyEscape})
	if !result.Handled || state.remote.auto.open || state.modal != modalRemoteTools {
		t.Fatal("Escape closed the utility instead of its interval menu")
	}
}

func TestRemoteDockerPlaceholderRecoversAndDoesNotHideOtherErrors(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.mu.Lock()
	server.failures["docker ps"] = errors.New("exit status 127")
	server.mu.Unlock()
	state.Click(hitRemoteTabContainers)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
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
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if !state.remote.noDocker || !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteDockerMissing) {
		t.Fatal("missing Docker has no illustrated empty state")
	}
	server.mu.Lock()
	requests := len(server.commands)
	server.mu.Unlock()
	for _, tab := range []ui.ID{hitRemoteTabImages, hitRemoteTabVolumes, hitRemoteTabNetworks, hitRemoteTabCompose, hitRemoteTabContainers} {
		state.Click(tab)
		canvas.Reset()
		state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
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
	state.Click(hitRemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	canvas.Reset()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if !state.remote.noDocker || remoteCanvasHasText(&canvas, state.messages.Tools.RemoteDockerMissing) {
		t.Fatal("processes lost the Docker availability cache or displayed the wrong empty state")
	}
	if hit, ok := hitByID(&canvas, hitRemoteKill); !ok || hit.Disabled {
		t.Fatal("missing Docker disabled unrelated process operations")
	}
	state.Click(hitRemoteTabContainers)
	if state.remote.loading {
		t.Fatal("returning from processes did not reuse the known Docker empty state")
	}
	server.mu.Lock()
	delete(server.failures, "docker ps")
	server.mu.Unlock()
	state.Click(hitRemoteRefresh)
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
	state.Click(hitRemoteRefresh)
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
				for _, size := range []ui.Size{{Width: 600, Height: 500}, {Width: 1100, Height: 780}} {
					if err := view.Resize(int(size.Width), int(size.Height)); err != nil {
						t.Fatal(err)
					}
					w.window.Invalidate()
					view.Driver().Pump()
					var canvas ui.Canvas
					canvas.SetPixelScale(float32(scale))
					w.paint(&canvas, size)
					tab, _ := hitByID(&canvas, hitRemoteTabProcesses)
					for _, id := range []ui.ID{hitRemoteRefresh, hitRemoteAutoRefresh, hitRemoteSortCPU, hitRemoteSortMemory, hitRemoteClose} {
						hit, ok := hitByID(&canvas, id)
						if !ok || hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width+.01 || hit.Bounds.Y+hit.Bounds.Height > size.Height+.01 {
							t.Fatalf("language=%v size=%v: unreachable control %v: %+v", language, size, id, hit)
						}
						if (id == hitRemoteSortCPU || id == hitRemoteSortMemory) && hit.Bounds.Y <= tab.Bounds.Y+tab.Bounds.Height {
							t.Fatal("sort control still belongs to the tab toolbar")
						}
						if (id == hitRemoteRefresh || id == hitRemoteAutoRefresh) && hit.Bounds.Height != tab.Bounds.Height {
							t.Fatal("refresh controls and tabs use different heights")
						}
					}
					if remoteCanvasHasText(&canvas, state.messages.Tools.RemoteTitle) || remoteCanvasHasText(&canvas, fmt.Sprintf(state.messages.Tools.RemoteDescriptionFmt, w.session.Title)) {
						t.Fatal("redundant content title/subtitle is still painted")
					}
				}
			}
			// Exercise the actual native-window event path, including keyboard
			// activation and virtual timer frames, rather than calling handlers.
			click := func(id ui.ID) {
				t.Helper()
				box := dualHit(t, view, id)
				if err := view.Click(box.X+box.Width/2, box.Y+box.Height/2); err != nil {
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
			click(hitRemoteRowList.At(0))
			selected := w.ui.remote.selected
			click(hitRemoteSortMemory)
			settle()
			if w.ui.remote.sort != 1 || w.ui.remote.selected != selected || w.ui.remote.rows[0].key != "1" {
				t.Fatal("table header sorting lost its selected PID or sorted incorrectly")
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if w.ui.remote.rows[0].key != "3000" {
				t.Fatal("keyboard activation did not switch the header to descending")
			}
			if err := view.Keys("Space"); err != nil {
				t.Fatal(err)
			}
			if w.ui.remote.rows[0].key != "1" {
				t.Fatal("Space did not switch the sortable header to ascending")
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			click(hitRemoteAutoRefresh)
			if err := view.Keys("Down"); err != nil {
				t.Fatal(err)
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
				click(hitRemoteAutoRefresh)
				if err := view.Driver().Advance(250 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("remote-refresh-menu-%g.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
				click(hitRemoteAutoToggle)
				view.Driver().Advance(250 * time.Millisecond)
				server.mu.Lock()
				server.failures = map[string]error{"docker ps": errors.New("exit status 127")}
				server.mu.Unlock()
				click(hitRemoteTabContainers)
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

func remoteCanvasHasText(canvas *ui.Canvas, text string) bool {
	for _, painted := range canvas.Texts() {
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
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && state.remote.noDocker })

	state.Click(hitRemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	server.mu.Lock()
	delete(server.failures, "docker ps")
	server.mu.Unlock()
	state.Click(hitRemoteRefresh)
	waitFor(t, state, func() bool { return !state.remote.loading })

	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	if state.remote.noDocker {
		t.Fatal("installed Docker was still reported missing")
	}
}
