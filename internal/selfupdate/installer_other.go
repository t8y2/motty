//go:build !windows

package selfupdate

import "os/exec"

func installerCommandLine(*exec.Cmd, string) {}
