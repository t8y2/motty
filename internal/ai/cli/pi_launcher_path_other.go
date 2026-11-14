//go:build !windows

package cli

import "path/filepath"

func piLauncherPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
