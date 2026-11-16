package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// applyMac swaps the new bundle in beside the old one so the swap is two
// renames on one volume, and puts the old one back if the second fails.
func (job Job) applyMac() error {
	parent := filepath.Dir(job.Root)
	next := filepath.Join(parent, "."+filepath.Base(job.Root)+".new")
	old := filepath.Join(parent, "."+filepath.Base(job.Root)+".old")
	os.RemoveAll(next)
	os.RemoveAll(old)
	if err := moveTree(job.Staged, next); err != nil {
		os.RemoveAll(next)
		return fmt.Errorf("placing the new version: %w", err)
	}
	if err := os.Rename(job.Root, old); err != nil {
		os.RemoveAll(next)
		return fmt.Errorf("setting the old version aside: %w", err)
	}
	if err := os.Rename(next, job.Root); err != nil {
		rollback := os.Rename(old, job.Root)
		os.RemoveAll(next)
		return errors.Join(fmt.Errorf("installing the new version: %w", err), rollback)
	}
	os.RemoveAll(old)
	return nil
}

// moveTree renames src to dst, or copies it when they are on different volumes.
func moveTree(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// copyTree copies directories and ordinary files, keeping permissions.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, err = io.Copy(out, in)
			if closeErr := out.Close(); err == nil {
				err = closeErr
			}
			return err
		}
		return fmt.Errorf("not a file: %s", path)
	})
}

// applyWindows runs the installer silently in the detected installation's exact
// directory. The helper has already waited for the running program to quit.
func (job Job) applyWindows() error {
	return replaceWindowsExecutable(job.Root, filepath.Base(job.Exe), job.runWindowsInstaller)
}

func (job Job) runWindowsInstaller() error {
	// installerCommandLine builds the whole command line: Go's argument quoting
	// would wrap /D= and NSIS would then install to the default directory.
	cmd := exec.Command(job.Staged)
	hide(cmd)
	installerCommandLine(cmd, job.Root)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("the installer failed: %w %s", err, firstLine(string(output)))
	}
	return nil
}

// applyLinux runs the archive's own install.sh over the existing prefix; the
// script copies the program beside its target and renames it into place.
func (job Job) applyLinux() error {
	cmd := exec.Command("/bin/sh", filepath.Join(job.Staged, "install.sh"), "install", "--prefix", job.Root)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("install.sh failed: %w %s", err, firstLine(string(output)))
	}
	return nil
}
