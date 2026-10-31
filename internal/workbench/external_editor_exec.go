//go:build darwin || linux

package workbench

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// runOpener hands the link to the system's opener and keeps what it said.
func runOpener(program, link string) error {
	output, err := exec.Command(program, link).CombinedOutput()
	if err != nil {
		// The rest of a LaunchServices refusal is a domain dump, cut to what a
		// toast can hold.
		if reason := strings.TrimSpace(string(output)); reason != "" {
			return fmt.Errorf("%s: %s", filepath.Base(program), truncateRunes(reason, 160))
		}
		return fmt.Errorf("%s: %w", filepath.Base(program), err)
	}
	return nil
}
