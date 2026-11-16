//go:build linux

package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"motty/internal/update"
)

// DebCommand is invoked only through Polkit, using the dpkg-owned executable.
const DebCommand = "install-deb"

func debInstalled(path string) bool {
	if path != "/usr/bin/motty" {
		return false
	}
	output, err := exec.Command("/usr/bin/dpkg-query", "-S", "--", path).Output()
	if err != nil || (strings.TrimSpace(string(output)) != "motty: "+path && strings.TrimSpace(string(output)) != "motty:"+runtime.GOARCH+": "+path) {
		return false
	}
	output, err = exec.Command("/usr/bin/dpkg-query", "-W", "-f=${db:Status-Abbrev}", "motty").Output()
	return err == nil && strings.HasPrefix(string(output), "ii ")
}

func polkitAvailable() bool {
	_, err := os.Stat("/usr/bin/pkexec")
	return err == nil
}

func (job Job) applyDeb() error {
	if job.Exe != "/usr/bin/motty" || job.Root != "/usr/bin" || !debInstalled(job.Exe) || !polkitAvailable() {
		return ErrUnsupported
	}
	cmd := debInstallCommand(job)
	output, err := cmd.CombinedOutput()
	return debInstallError(err, output)
}

func debInstallCommand(job Job) *exec.Cmd {
	return exec.Command("/usr/bin/pkexec", job.Exe, DebCommand, "--path="+job.Staged, "--version="+job.Version, "--signature="+job.Signature)
}

func debInstallError(err error, output []byte) error {
	if err == nil {
		return nil
	}
	var status *exec.ExitError
	if errors.As(err, &status) && (status.ExitCode() == 126 || status.ExitCode() == 127) {
		return errors.New("system authorization was cancelled or unavailable; the downloaded package is kept for retry")
	}
	return fmt.Errorf("package manager failed: %w: %s", err, debFailure(output))
}

// RunPrivilegedDeb copies the untrusted user's file into a root-owned private
// directory, verifies that copy, then passes only that copy to dpkg.
func RunPrivilegedDeb(path, version, signature string) error {
	if os.Geteuid() != 0 || !debInstalled("/usr/bin/motty") || !update.Newer(update.Current, version) {
		return ErrUnsupported
	}
	name := fmt.Sprintf("motty_%s_%s.deb", version, runtime.GOARCH)
	if filepath.Base(path) != name || update.StableNamePackage("linux", runtime.GOARCH, update.PackageDeb) == "" {
		return ErrUnsupported
	}
	dir, err := os.MkdirTemp("/tmp", "motty-deb-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	trusted := filepath.Join(dir, name)
	if err := copyVerifiedDeb(path, trusted, update.Release{Version: version, Asset: update.Asset{Name: name, Signature: signature}}); err != nil {
		return err
	}
	output, err := exec.Command("/usr/bin/dpkg", "--install", trusted).CombinedOutput()
	if err != nil {
		return fmt.Errorf("dpkg: %w: %s", err, debFailure(output))
	}
	return nil
}

func debFailure(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error:") || strings.Contains(lower, "failed") || strings.Contains(lower, "unable to") {
			return strings.TrimSpace(line)
		}
	}
	return firstLine(string(output))
}

func copyVerifiedDeb(path, trusted string, release update.Release) error {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	source := os.NewFile(uintptr(fd), path)
	defer source.Close()
	stat, err := source.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 1<<29 {
		return ErrUnsupported
	}
	file, err := os.OpenFile(trusted, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.CopyN(file, source, stat.Size()+1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := verifyReleasePackage(trusted, release); err != nil {
		return fmt.Errorf("package signature rejected: %w", err)
	}
	return nil
}
