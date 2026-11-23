//go:build darwin || linux

package main

import "github.com/vivaui/viva/app"

func desktopWindowIcon(icon app.WindowIcon) app.WindowIcon { return icon }
func desktopWindowClass() string                           { return "" }
