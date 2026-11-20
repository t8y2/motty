package main

import (
	"bytes"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench"
)

type iconTrayHost struct {
	minSizeHost
	initial app.StatusIcon
	updates []app.StatusIcon
}

func (host *iconTrayHost) AddStatusItem(config app.StatusItemConfig) (app.StatusItemControl, error) {
	host.initial = config.Icon
	return host, nil
}

func (host *iconTrayHost) SetIcon(icon app.StatusIcon) error {
	host.updates = append(host.updates, icon)
	return nil
}

func (*iconTrayHost) SetTooltip(string) {}
func (*iconTrayHost) Remove()           {}

func TestTrayUsesTheChosenIconBeforeAndAfterCreation(t *testing.T) {
	host := &iconTrayHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	err := app.RunApplication(func(application *app.Application) error {
		window, err := application.OpenWindow(app.Config{Width: 100, Height: 100}, &gfx.Canvas{})
		if err != nil {
			return err
		}
		defer window.Close()
		status := newTray(application)
		// A saved choice arrives before the status icon is created on startup.
		chosen := app.WindowIcon{Size: 1, RGBA: []byte{20, 100, 220, 255}}
		status.setIcon(chosen)
		status.show(application)
		if want := chosen.StatusIcon(64); !bytes.Equal(host.initial.RGBA, want.RGBA) {
			t.Fatal("the tray started with the default instead of the saved choice")
		}
		status.setIcon(workbench.AppIcon())
		if len(host.updates) != 1 {
			t.Fatal("a running tray did not update")
		}
		status.setIcon(workbench.AppIcon())
		if len(host.updates) != 1 {
			t.Fatal("an unchanged color regenerated the tray icon")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
