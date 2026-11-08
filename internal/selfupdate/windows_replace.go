package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// replaceWindowsExecutable retires the installed image on the same volume before
// NSIS runs. Windows permits renaming an executing image, but not deleting it:
// stdio MCP servers can keep their connections and the installer gets a free path.
// Only the executable is restored on failure; this is not an installer transaction.
func replaceWindowsExecutable(root, name string, install func() error) error {
	if name == "" || name == "." || filepath.Base(name) != name {
		return errors.New("invalid installed executable name")
	}
	prefix := "." + name + "-update-"
	dir, err := os.MkdirTemp(root, prefix)
	if err != nil {
		return fmt.Errorf("preparing the previous executable: %w", err)
	}
	exe, previous := filepath.Join(root, name), filepath.Join(dir, "previous.exe")
	if err := os.Rename(exe, previous); err != nil {
		os.Remove(dir)
		return fmt.Errorf("setting the previous executable aside (close other Motty processes and retry): %w", err)
	}
	err = install()
	if err == nil {
		info, statErr := os.Stat(exe)
		if statErr != nil {
			err = fmt.Errorf("the installer did not create the new executable: %w", statErr)
		} else if !info.Mode().IsRegular() || info.Size() == 0 {
			err = errors.New("the installer did not create a usable executable")
		}
	}
	if err != nil {
		// An interrupted installer can leave a partial file. Never restart it in
		// preference to the original, and retain the backup if restoring fails.
		if restoreErr := restoreExecutable(exe, previous, dir, os.Remove); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	// A running MCP server may still hold this image. Leave it in place until
	// a later update can remove it; no active process or remote command is killed.
	cleanRetiredExecutables(root, prefix)
	return nil
}

// restoreExecutable puts the retired image back at the install path. A partial
// file the installer left is removed first, but a removal that fails does not
// end the attempt: the lock may be gone by the time of the rename, and a rename
// onto the leftover would fail on its own. Both failures are reported together
// with the backup path so the only copy of the program is never silently left
// in the temporary directory.
func restoreExecutable(exe, previous, dir string, remove func(string) error) error {
	var removeErr error
	if err := remove(exe); err != nil && !errors.Is(err, os.ErrNotExist) {
		removeErr = err
	}
	if restoreErr := os.Rename(previous, exe); restoreErr != nil {
		if removeErr != nil {
			return fmt.Errorf("removing the incomplete executable and restoring %s: %w", previous, errors.Join(removeErr, restoreErr))
		}
		return fmt.Errorf("restoring the previous executable from %s: %w", previous, restoreErr)
	}
	// The image is back at the install path, so the directory holding the
	// retired copy has nothing left to keep. A removal that failed above no
	// longer matters once the restore succeeded.
	os.Remove(dir)
	return nil
}

// Only remove the known file in our retired-image directories. No recursive
// deletion, following directory symlinks, or removal of unrelated contents.
func cleanRetiredExecutables(root, prefix string) {
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		os.Remove(filepath.Join(dir, "previous.exe"))
		os.Remove(dir)
	}
}
