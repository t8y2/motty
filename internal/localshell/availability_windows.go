//go:build windows

package localshell

import "os/exec"

func prepareAvailabilityProcess(*exec.Cmd) {}
func cleanupAvailabilityProcess(*exec.Cmd) {}
