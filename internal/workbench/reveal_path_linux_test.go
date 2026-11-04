//go:build linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileManagerShowCommandPrefersDBusSend(t *testing.T) {
	directory := t.TempDir()
	dbus := writeLinuxCommand(t, directory, "dbus-send", "#!/bin/sh\nexit 0\n")
	gdbus := writeLinuxCommand(t, directory, "gdbus", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", directory)

	got := fileManagerShowCommands("file:///tmp/report%20one.txt")
	if len(got) != 2 {
		t.Fatalf("commands=%d want 2", len(got))
	}
	wantDBus := []string{
		dbus,
		"--session",
		"--dest=org.freedesktop.FileManager1",
		"--type=method_call",
		"/org/freedesktop/FileManager1",
		"org.freedesktop.FileManager1.ShowItems",
		"array:string:file:///tmp/report%20one.txt",
		"string:",
	}
	if !slices.Equal(got[0].Args, wantDBus) {
		t.Fatalf("dbus-send args=%q want=%q", got[0].Args, wantDBus)
	}
	wantGDBus := []string{
		gdbus,
		"call",
		"--session",
		"--dest", "org.freedesktop.FileManager1",
		"--object-path", "/org/freedesktop/FileManager1",
		"--method", "org.freedesktop.FileManager1.ShowItems",
		`["file:///tmp/report%20one.txt"]`,
		"",
	}
	if !slices.Equal(got[1].Args, wantGDBus) {
		t.Fatalf("gdbus args=%q want=%q", got[1].Args, wantGDBus)
	}
}

func TestRevealPathInFileManagerFallsBackThroughDesktopOpeners(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "opened")
	writeLinuxCommand(t, directory, "dbus-send", "#!/bin/sh\nexit 1\n")
	writeLinuxCommand(t, directory, "gdbus", "#!/bin/sh\nexit 1\n")
	writeLinuxCommand(t, directory, "xdg-open", "#!/bin/sh\nexit 1\n")
	writeLinuxCommand(t, directory, "gio", fmt.Sprintf("#!/bin/sh\n: > %q\n", marker))
	t.Setenv("PATH", directory)

	if err := revealPathInFileManager("/tmp/report one.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("gio fallback did not run: %v", err)
	}
}

func TestRevealPathInFileManagerReportsWhenEveryOpenerFails(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"dbus-send", "gdbus", "xdg-open", "gio"} {
		writeLinuxCommand(t, directory, name, "#!/bin/sh\nexit 1\n")
	}
	t.Setenv("PATH", directory)

	if err := revealPathInFileManager("/tmp/report one.txt"); err == nil {
		t.Fatal("all file-manager launchers failed without an error")
	}
}

func writeLinuxCommand(t *testing.T, directory, name, contents string) string {
	t.Helper()
	command := filepath.Join(directory, name)
	if err := os.WriteFile(command, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return command
}
