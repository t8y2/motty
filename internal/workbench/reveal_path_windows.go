//go:build windows

package workbench

import "os/exec"

func revealPathInFileManager(path string) error {
	return startDetached(exec.Command("explorer.exe", "/select,", path))
}
