//go:build linux

package systemproxy

import (
	"context"
	"os/exec"
)

// Lookup is the manual proxy GNOME's settings name, read with one gsettings
// run; another desktop, an automatic (PAC) proxy or no gsettings is the zero
// Settings.
func Lookup() Settings {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "gsettings", "list-recursively", "org.gnome.system.proxy").Output()
	if err != nil {
		return Settings{}
	}
	return gnomeSettings(string(output))
}
