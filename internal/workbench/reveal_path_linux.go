//go:build linux

package workbench

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
)

func revealPathInFileManager(path string) error {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	var failures []error
	for _, command := range fileManagerShowCommands(uri) {
		if err := command.Run(); err == nil {
			return nil
		} else {
			failures = append(failures, err)
		}
	}
	directory := filepath.Dir(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		directory = path
	}
	if opener, err := exec.LookPath("xdg-open"); err == nil {
		if err := exec.Command(opener, directory).Run(); err == nil {
			return nil
		} else {
			failures = append(failures, err)
		}
	} else {
		failures = append(failures, err)
	}
	if opener, err := exec.LookPath("gio"); err == nil {
		if err := exec.Command(opener, "open", directory).Run(); err == nil {
			return nil
		} else {
			failures = append(failures, err)
		}
	} else {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func fileManagerShowCommands(uri string) []*exec.Cmd {
	var commands []*exec.Cmd
	if command, err := exec.LookPath("dbus-send"); err == nil {
		commands = append(commands, exec.Command(command,
			"--session",
			"--dest=org.freedesktop.FileManager1",
			"--type=method_call",
			"/org/freedesktop/FileManager1",
			"org.freedesktop.FileManager1.ShowItems",
			"array:string:"+uri,
			"string:",
		))
	}
	if command, err := exec.LookPath("gdbus"); err == nil {
		commands = append(commands, exec.Command(command,
			"call",
			"--session",
			"--dest", "org.freedesktop.FileManager1",
			"--object-path", "/org/freedesktop/FileManager1",
			"--method", "org.freedesktop.FileManager1.ShowItems",
			fmt.Sprintf("[%q]", uri),
			"",
		))
	}
	return commands
}
