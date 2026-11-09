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

// The main window must carry the minimum into the native config, and a frame
// restored below it must open at the minimum instead of the saved size.
func TestMainWindowKeepsItsMinimumAndCorrectsARestoredTinyFrame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(datadir.Variable, dir)
	host := &minSizeHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })

	run := func(shrink bool) app.Config {
		t.Helper()
		hub := workbench.NewAIHub(filepath.Join(t.TempDir(), "ai.db"))
		defer hub.Close()
		var opened app.Config
		err := app.RunApplication(func(application *app.Application) error {
			status := newTray(application)
			open := func() error {
				return openWorkbenchWindow(application, status, hub, nil, false, placement{rememberFrame: true})
			}
			if err := open(); err != nil {
				return err
			}
			window := host.windows[len(host.windows)-1]
			opened = window.config
			if shrink {
				window.config.OnFrameChange(gfx.Rect{Width: 400, Height: 300})
			}
			window.Close()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return opened
	}

	// Record a frame far below the minimum, then open again from the store.
	run(true)
	restored := run(false)
	if restored.MinWidth != workbench.MinimumWindowWidth || restored.MinHeight != workbench.MinimumWindowHeight {
		t.Fatalf("main window minimum = %dx%d, want %dx%d", restored.MinWidth, restored.MinHeight, workbench.MinimumWindowWidth, workbench.MinimumWindowHeight)
	}
	if restored.Width != workbench.MinimumWindowWidth || restored.Height != workbench.MinimumWindowHeight {
		t.Fatalf("restored frame %dx%d opened at %dx%d, want the minimum %dx%d", 400, 300, restored.Width, restored.Height, workbench.MinimumWindowWidth, workbench.MinimumWindowHeight)
	}
}
