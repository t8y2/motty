package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Use only system tools. PATH may contain user-controlled wrappers, and the
// graphical authorization prompt must name the actual package manager.
func registeredLinuxDeb(exe string) bool {
	if runtime.GOOS != "linux" || exe != "/usr/bin/motty" {
		return false
	}
	for _, path := range []string{"/usr/bin/pkexec", "/usr/bin/dpkg", "/usr/bin/dpkg-query", "/usr/bin/dpkg-deb"} {
		if !exists(path) {
			return false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	owner, err := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "--search", exe).Output()
	if err != nil || strings.TrimSpace(string(owner)) != "motty: /usr/bin/motty" {
		return false
	}
	status, err := exec.CommandContext(ctx, "/usr/bin/dpkg-query", "--show", "--showformat=${db:Status-Status}", "motty").Output()
	return err == nil && string(status) == "installed"
}

func verifyDebPackage(ctx context.Context, path, version, arch string) error {
	output, err := exec.CommandContext(ctx, "/usr/bin/dpkg-deb", "--field", path, "Package", "Version", "Architecture").Output()
	if err != nil {
		return fmt.Errorf("reading the DEB package metadata: %w", err)
	}
	return checkDebMetadata(string(output), version, arch)
}

func checkDebMetadata(output, version, arch string) error {
	fields := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	if fields["Package"] != "motty" || fields["Version"] != version || fields["Architecture"] != arch {
		return errors.New("the DEB package does not match Motty, the offered version and this architecture")
	}
	return nil
}

func debInstallCommand(path string) *exec.Cmd {
	return exec.Command("/usr/bin/pkexec", "/usr/bin/dpkg", "--install", path)
}

func runDebInstaller(cmd *exec.Cmd) error {
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127) {
		return errors.New("system authorization was cancelled or unavailable; the update was not installed")
	}
	return fmt.Errorf("installing the DEB package failed: %w %s", err, firstLine(string(output)))
}
