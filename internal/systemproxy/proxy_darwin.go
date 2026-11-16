//go:build darwin

package systemproxy

import (
	"context"
	"os/exec"
)

// Lookup is the proxy the network settings name; no or an unreadable setting is
// the zero Settings.
func Lookup() Settings {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "scutil", "--proxy").Output()
	if err != nil {
		return Settings{}
	}
	return darwinSettings(string(output))
}
