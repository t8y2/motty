//go:build !windows

package workbench

import "os/exec"

func hideSecurityKeyCommand(*exec.Cmd) {}
