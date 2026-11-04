package workbench

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	errRevealPathNotAbsolute = errors.New("reveal path: path must be absolute")
	errRevealPathMissing     = errors.New("reveal path: path does not exist")
)

// revealPath validates a local destination before handing it to the native
// file manager. Validation also prevents a transfer record from becoming a
// command or working-directory input on every platform.
func revealPath(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q", errRevealPathNotAbsolute, path)
	}
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %q", errRevealPathMissing, path)
		}
		return fmt.Errorf("reveal path: %w", err)
	}
	return revealPathInFileManager(path)
}

func startDetached(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
