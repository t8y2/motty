package main

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/workbench"
)

// trayIconItem counts what the status area was asked to do.
type trayIconItem struct{ removed int }

func (item *trayIconItem) SetIcon(app.StatusIcon) error { return nil }
func (item *trayIconItem) SetTooltip(string)            {}
func (item *trayIconItem) Remove()                      { item.removed++ }

// trayIconHost is a desktop with a working status area, unlike minSizeHost.
type trayIconHost struct {
	minSizeHost
	items []*trayIconItem
}

func (host *trayIconHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	item := &trayIconItem{}
	host.items = append(host.items, item)
	return item, nil
}

// hiddenTrayWindow stands for a window the user hid into the status area.
type hiddenTrayWindow struct{ shows int }

func (window *hiddenTrayWindow) Show()        { window.shows++ }
func (window *hiddenTrayWindow) Hidden() bool { return true }

// The General settings' choice drives the real status item: turning the icon
// off removes it and brings back any window hiding there; turning it on puts
// one icon back, and repeating either choice is harmless.
func TestTheTrayIconSettingAddsAndRemovesTheStatusItem(t *testing.T) {
	host := &trayIconHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { app.UseApplicationRunner(nil) })
	err := app.RunApplication(func(application *app.Application) error {
		window, err := application.OpenWindow(app.Config{Width: 800, Height: 600}, nil)
		if err != nil {
			return err
		}
		defer window.Close()
		status := newTray(application)
		if status.hasStatusArea() {
			t.Fatal("an icon before the first show")
		}
		status.show(application)
		status.show(application) // showing twice keeps one icon
		if !status.hasStatusArea() || len(host.items) != 1 {
			t.Fatalf("items %d, area %v", len(host.items), status.hasStatusArea())
		}
		dir := t.TempDir()
		state := workbench.New(workbench.Options{
			StatePath:      filepath.Join(dir, "state.json"),
			KnownHostsPath: filepath.Join(dir, "known_hosts"),
		})
		defer state.Close()
		hidden := &hiddenTrayWindow{}
		status.list.Add(state, hidden)
		status.setVisible(application, false)
		if status.hasStatusArea() || host.items[0].removed != 1 || hidden.shows != 1 {
			t.Fatalf("area %v, removed %d, shows %d", status.hasStatusArea(), host.items[0].removed, hidden.shows)
		}
		status.setVisible(application, false) // hiding twice is harmless
		if host.items[0].removed != 1 {
			t.Fatalf("removed %d", host.items[0].removed)
		}
		status.setVisible(application, true)
		if !status.hasStatusArea() || len(host.items) != 2 {
			t.Fatalf("items %d, area %v", len(host.items), status.hasStatusArea())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
