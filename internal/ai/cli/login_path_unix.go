//go:build !windows

package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// loginPath is the PATH the user's login shell builds. An app started from the
// Finder or the Dock inherits launchd's minimal PATH, which lacks Homebrew and
// the version-manager directories, so a CLI installed there cannot run its own
// `#!/usr/bin/env node` launcher. Every CLI process needs it, so it is read once.
var loginPath = sync.OnceValue(func() string { return readLoginPath(loginShell()) })

// readLoginPath asks shell for its PATH the way a terminal would: interactive
// and login, so the profile and rc files that export PATH are sourced. Shell
// startup may print anything, so only the PATH line is taken.
func readLoginPath(shell string) string {
	ctx, cancel := context.WithTimeout(context.Background(), LoginPathTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, shell, "-ilc", "env")
	cmd.Stdout = &out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return ""
	}
	path := ""
	for _, line := range strings.Split(out.String(), "\n") {
		if value, ok := strings.CutPrefix(line, "PATH="); ok {
			path = value
		}
	}
	return path
}

func loginShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	if _, err := os.Stat("/bin/zsh"); err == nil {
		return "/bin/zsh"
	}
	return "/bin/sh"
}
