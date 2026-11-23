package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/datadir"
	"motty/internal/workbench"
)

type minSizeHost struct {
	windows []*minSizeWindow
}

type minSizeWindow struct {
	config app.Config
	frame  gfx.Rect
	closed func()
	done   bool
}

func (w *minSizeWindow) Close() {
	if w.done {
		return
	}
	w.done = true
	if w.config.OnClosed != nil {
		w.config.OnClosed()
	}
	if w.closed != nil {
		w.closed()
	}
}
func (w *minSizeWindow) Hide()                   {}
func (w *minSizeWindow) Show()                   {}
func (w *minSizeWindow) Frame() (gfx.Rect, bool) { return w.frame, true }

func (h *minSizeHost) OpenWindow(config app.Config, _ *gfx.Canvas, closed func()) (app.WindowControl, error) {
	w := &minSizeWindow{config: config, frame: gfx.Rect{Width: float32(config.Width), Height: float32(config.Height)}, closed: closed}
	h.windows = append(h.windows, w)
	return w, nil
}
func (h *minSizeHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("not used")
}
func (h *minSizeHost) SetDockBadge(string) {}
func (h *minSizeHost) SetProgress(float32) {}

func (h *minSizeHost) Quit() {
	for _, w := range h.windows {
		w.Close()
	}
}

// The main window must carry the minimum into the native config, and a
// minimized frame reported after a usable one must not replace it.
func TestMainWindowKeepsItsMinimumAndIgnoresAMinimizedFrame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(datadir.Variable, dir)
	// Both runs open the window the launch path opens, so they share one frame
	// record; leftovers from another test must not make it the second window.
	previousIDs := windowIDs
	windowIDs = 0
	t.Cleanup(func() { windowIDs = previousIDs })
	host := &minSizeHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })

	usable := gfx.Rect{X: 120, Y: 80, Width: 1234, Height: 777}
	run := func(minimize bool) app.Config {
		t.Helper()
		hub := workbench.NewAIHub(filepath.Join(t.TempDir(), "ai.db"))
		defer hub.Close()
		var opened app.Config
		err := app.RunApplication(func(application *app.Application) error {
			status := newTray(application)
			open := func() error {
				return openWorkbenchWindow(application, status, hub, nil, false, placement{windowID: 1, rememberFrame: true})
			}
			if err := open(); err != nil {
				return err
			}
			window := host.windows[len(host.windows)-1]
			opened = window.config
			window.config.OnFrameChange(usable)
			if minimize {
				// What Windows reports while the window is minimized: the icon's
				// corner and size, sent before OnMinimize marks the window.
				window.config.OnFrameChange(gfx.Rect{X: -32000, Y: -32000, Width: 160, Height: 28})
			}
			window.Close()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return opened
	}

	run(true)
	restored := run(false)
	if restored.MinWidth != workbench.MinimumWindowWidth || restored.MinHeight != workbench.MinimumWindowHeight {
		t.Fatalf("main window minimum = %dx%d, want %dx%d", restored.MinWidth, restored.MinHeight, workbench.MinimumWindowWidth, workbench.MinimumWindowHeight)
	}
	if restored.Width != int(usable.Width) || restored.Height != int(usable.Height) {
		t.Fatalf("restored frame %dx%d, want the last usable %v", restored.Width, restored.Height, usable)
	}
	if restored.Origin == nil || *restored.Origin != (gfx.Point{X: usable.X, Y: usable.Y}) {
		t.Fatalf("restored origin = %v, want %v", restored.Origin, gfx.Point{X: usable.X, Y: usable.Y})
	}
}

func (h *minSizeHost) Post(fn func()) {
	if fn != nil {
		fn()
	}
}
