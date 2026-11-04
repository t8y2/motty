//go:build darwin

package workbench

import "os/exec"

func revealPathInFileManager(path string) error {
	return startDetached(exec.Command("open", "-R", path))
}
