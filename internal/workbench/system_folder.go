package workbench

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// openSystemFolder 进入目录本身，不在父目录中选中它，也不执行 shell 命令。
func openSystemFolder(directory string) error {
	if !filepath.IsAbs(directory) || strings.IndexByte(directory, 0) >= 0 {
		return fmt.Errorf("file manager: invalid directory")
	}
	directory = filepath.Clean(directory)
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("file manager: path is not a directory")
	}
	command := systemFolderCommand(runtime.GOOS, directory)
	if command == nil {
		return fmt.Errorf("file manager: unsupported platform")
	}
	// Explorer 可能在成功复用现有窗口后返回非零状态，启动成功即视为已交接。
	if runtime.GOOS == "windows" {
		if err := command.Start(); err != nil {
			return err
		}
		go command.Wait()
		return nil
	}
	return command.Run()
}

func systemFolderCommand(goos, directory string) *exec.Cmd {
	switch goos {
	case "windows":
		return exec.Command("explorer.exe", directory)
	case "darwin":
		return exec.Command("open", directory)
	case "linux":
		return exec.Command("xdg-open", directory)
	default:
		return nil
	}
}
