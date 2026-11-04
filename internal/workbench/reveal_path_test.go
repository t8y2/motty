package workbench

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRevealPathRejectsInvalidDestinationsBeforeLaunchingFileManager(t *testing.T) {
	if err := revealPath(""); !errors.Is(err, errRevealPathNotAbsolute) {
		t.Fatalf("empty path error = %v", err)
	}
	if err := revealPath("relative/file.txt"); !errors.Is(err, errRevealPathNotAbsolute) {
		t.Fatalf("relative path error = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := revealPath(missing); !errors.Is(err, errRevealPathMissing) {
		t.Fatalf("missing path error = %v", err)
	}
}
