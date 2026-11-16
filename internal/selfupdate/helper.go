package selfupdate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// stageHelper puts the program that will carry out a job outside the
// installation it replaces and returns the path to run. Its copy is what the
// replacement of the installation cannot disturb, and it has to start on its own.
func stageHelper(install Install, dir string) (string, error) {
	if install.Kind == KindMacApp {
		return stageMacHelper(install, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	helper := filepath.Join(dir, "motty-update")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	if err := copyExecutable(install.Exe, helper); err != nil {
		return "", err
	}
	return helper, nil
}

// stageMacHelper copies the whole bundle. A Developer ID signature covers the
// bundle's Info.plist, so the executable alone no longer verifies once it is
// taken out of its bundle, and the system kills it as it starts.
func stageMacHelper(install Install, dir string) (string, error) {
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	bundle := filepath.Join(dir, filepath.Base(install.Root))
	if output, err := exec.Command("/usr/bin/ditto", install.Root, bundle).CombinedOutput(); err != nil {
		return "", fmt.Errorf("copying %s: %w %s", filepath.Base(install.Root), err, firstLine(string(output)))
	}
	relative, err := filepath.Rel(install.Root, install.Exe)
	if err != nil {
		return "", err
	}
	return filepath.Join(bundle, relative), nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
