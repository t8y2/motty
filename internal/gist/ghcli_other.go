//go:build !windows

package gist

import "os/exec"

func hideWindow(*exec.Cmd) {}
