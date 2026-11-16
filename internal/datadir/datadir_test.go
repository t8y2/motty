package datadir

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func program(t *testing.T) (folder, executable string) {
	t.Helper()
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executable = filepath.Join(folder, "motty.exe")
	if err := os.WriteFile(executable, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return folder, executable
}

func TestExplicitDirectoryWinsOverPortableFolder(t *testing.T) {
	folder, executable := program(t)
	if err := os.Mkdir(filepath.Join(folder, PortableName), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Resolve("/tmp/profile", executable)
	if got.Dir != "/tmp/profile" || got.Portable {
		t.Fatalf("got %+v: an explicit directory is not a portable copy", got)
	}
}

func TestPortableFolderBesideTheProgram(t *testing.T) {
	folder, executable := program(t)
	want := filepath.Join(folder, PortableName)
	if err := os.Mkdir(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Resolve("", executable); got.Dir != want || !got.Portable {
		t.Fatalf("got %+v, want portable %s", got, want)
	}
}

func TestOrdinaryCopyWithoutPortableFolder(t *testing.T) {
	_, executable := program(t)
	if got := Resolve("", executable); got != (Location{}) {
		t.Fatalf("got %+v, want the platform's usual place", got)
	}
}

func TestPortableNameMustBeAFolder(t *testing.T) {
	folder, executable := program(t)
	if err := os.WriteFile(filepath.Join(folder, PortableName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Resolve("", executable); got != (Location{}) {
		t.Fatalf("got %+v: a file is not a portable folder", got)
	}
}

func TestLinkedExecutableFindsTheFolderBesideTheTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	folder, executable := program(t)
	want := filepath.Join(folder, PortableName)
	if err := os.Mkdir(want, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "motty")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	if got := Resolve("", link); got.Dir != want || !got.Portable {
		t.Fatalf("got %+v", got)
	}
}

func TestUnwritablePortableFolderFallsBack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop this user")
	}
	folder, executable := program(t)
	portable := filepath.Join(folder, PortableName)
	if err := os.Mkdir(portable, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(portable, 0o755) })
	if got := Resolve("", executable); got != (Location{}) {
		t.Fatalf("got %+v: a read-only folder cannot hold the data", got)
	}
}

func TestNoExecutableIsAnOrdinaryCopy(t *testing.T) {
	if got := Resolve("", ""); got != (Location{}) {
		t.Fatalf("got %+v", got)
	}
}
