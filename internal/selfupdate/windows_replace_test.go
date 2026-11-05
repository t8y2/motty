package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsExecutableReplacement(t *testing.T) {
	refusal := errors.New("installer failed")
	for _, mode := range []string{"success", "failure", "partial", "missing", "empty"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			exe := filepath.Join(root, "motty.exe")
			if err := os.WriteFile(exe, []byte("old"), 0700); err != nil {
				t.Fatal(err)
			}
			err := replaceWindowsExecutable(root, "motty.exe", func() error {
				if _, err := os.Stat(exe); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("old image still at install path: %v", err)
				}
				if mode == "success" || mode == "partial" || mode == "empty" {
					data := []byte("new")
					if mode == "empty" {
						data = nil
					}
					if err := os.WriteFile(exe, data, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "failure" || mode == "partial" {
					return refusal
				}
				return nil
			})
			want := "old"
			if mode == "success" {
				want = "new"
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected installation failure")
			}
			if mode == "failure" || mode == "partial" {
				if !errors.Is(err, refusal) {
					t.Fatalf("lost installer error: %v", err)
				}
			}
			data, readErr := os.ReadFile(exe)
			if readErr != nil || string(data) != want {
				t.Fatalf("installed image %q, %v; want %q", data, readErr, want)
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 1 {
				t.Fatalf("unexpected leftovers: %v", entries)
			}
		})
	}
}

func TestWindowsExecutableRetirementFailureDoesNotRunInstaller(t *testing.T) {
	root := t.TempDir()
	called := false
	err := replaceWindowsExecutable(root, "missing.exe", func() error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("err=%v, installer called=%v", err, called)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("unexpected leftovers: %v", entries)
	}
}

func TestRetiredImageCleanupPreservesOtherContents(t *testing.T) {
	root := t.TempDir()
	prefix := ".motty.exe-update-"
	dir, err := os.MkdirTemp(root, prefix)
	if err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(dir, "previous.exe")
	keep := filepath.Join(dir, "keep.txt")
	for _, path := range []string{previous, keep} {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cleanRetiredExecutables(root, prefix)
	if _, err := os.Stat(previous); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous image not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}

func TestFailedRetirementPreservesEarlierRecoveryCopy(t *testing.T) {
	root := t.TempDir()
	dir, err := os.MkdirTemp(root, ".motty.exe-update-")
	if err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(dir, "previous.exe")
	if err := os.WriteFile(previous, []byte("recovery"), 0700); err != nil {
		t.Fatal(err)
	}
	err = replaceWindowsExecutable(root, "motty.exe", func() error { t.Fatal("installer called without current image"); return nil })
	if err == nil {
		t.Fatal("missing current image was accepted")
	}
	data, err := os.ReadFile(previous)
	if err != nil || string(data) != "recovery" {
		t.Fatalf("recovery copy lost: %q %v", data, err)
	}
}
