//go:build darwin || windows || linux

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"motty/internal/selfupdate"
)

// runApplyUpdate is the helper an update leaves behind: it waits for the
// application to quit, swaps in the new version and starts it. It opens no
// window and claims nothing, so the new version finds the single-instance
// socket free.
func runApplyUpdate(args []string) int {
	job, err := selfupdate.ParseJob(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "motty apply-update:", err)
		return 2
	}
	fmt.Println(selfupdate.Ready)
	if err := job.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "motty apply-update:", err)
		return 1
	}
	return 0
}

// updatesDir is where downloads and the helper are kept: beside the saved data,
// in the data directory when one is set.
func updatesDir(dataDir string) string {
	if dataDir == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		dataDir = filepath.Join(config, "motty")
	}
	return filepath.Join(dataDir, "updates")
}
