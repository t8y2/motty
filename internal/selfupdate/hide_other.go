//go:build !windows

package selfupdate

import "os/exec"

func hide(*exec.Cmd) {}
