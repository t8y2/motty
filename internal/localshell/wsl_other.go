//go:build !windows

package localshell

import "context"

// InstalledWSL lists WSL distributions, which only Windows has.
func InstalledWSL(context.Context) []Choice { return nil }

// WSLExecutable is empty on a system without WSL.
func WSLExecutable() string { return "" }
