package workbench

import (
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/app/iconpng"
	"motty/assets"
)

var appIcon = sync.OnceValue(func() app.WindowIcon {
	icon, err := iconpng.Decode(assets.Icon)
	if err != nil {
		panic("workbench: the embedded icon is unusable: " + err.Error())
	}
	return icon
})

// AppIcon is the logo for the window's taskbar button, Alt+Tab and, outside an
// .app bundle, the Dock.
func AppIcon() app.WindowIcon { return appIcon() }

// TrayIcon is the logo as a status-area icon, in the form the platform expects:
// the mark alone as a template that macOS tints to its menu bar, the colored tile
// on Windows and Linux.
func TrayIcon() app.StatusIcon { return appIcon().StatusIcon(64) }
