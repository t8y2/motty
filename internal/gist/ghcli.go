package gist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Errors of the GitHub CLI credential source; test with errors.Is.
var (
	// ErrNoCLI means the GitHub CLI (gh) is not installed where Motty can find it.
	ErrNoCLI = errors.New("gist: the GitHub CLI (gh) was not found")
	// ErrCLINotSignedIn means gh has no login for the GitHub host.
	ErrCLINotSignedIn = errors.New("gist: the GitHub CLI is not signed in to that host")
)

// cliTimeout bounds one `gh auth token`; it only reads the stored login.
const cliTimeout = 15 * time.Second

// ghPath finds gh. PATH is not enough: an application started from the Finder
// or a desktop launcher inherits a minimal PATH that holds neither Homebrew
// nor the user's own bin directory, so the usual install locations are searched
// too. It is a variable so a test can stand in for the CLI.
var ghPath = findGH

func findGH() (string, error) {
	name := "gh"
	if runtime.GOOS == "windows" {
		name = "gh.exe"
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range ghInstallDirs() {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", ErrNoCLI
}

func ghInstallDirs() []string {
	var dirs []string
	if runtime.GOOS == "windows" {
		// The installer's default per-machine and per-user locations.
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				dirs = append(dirs, filepath.Join(root, "GitHub CLI"), filepath.Join(root, "Programs", "GitHub CLI"))
			}
		}
		return dirs
	}
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin", "/home/linuxbrew/.linuxbrew/bin", "/snap/bin")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, dir := range []string{".local/bin", "bin", ".nix-profile/bin"} {
			dirs = append(dirs, filepath.Join(home, filepath.FromSlash(dir)))
		}
	}
	return dirs
}

// CLIHost is the host name gh knows the API address by: github.com for the
// public service, the server itself for a GitHub Enterprise address
// (https://ghe.example.com/api/v3).
func CLIHost(apiBase string) string {
	parsed, err := url.Parse(apiBase)
	if err != nil || parsed.Hostname() == "" {
		return "github.com"
	}
	host := parsed.Hostname()
	if host == "api.github.com" {
		return "github.com"
	}
	return host
}

// readCLIToken is cliToken; tests of the programs above replace it.
var readCLIToken = cliToken

// UseGitHubCLIForTests makes the GitHub CLI answer with token(host), so a test
// of the application needs no gh on the machine. It returns the undo.
func UseGitHubCLIForTests(token func(host string) (string, error)) (restore func()) {
	previous := readCLIToken
	readCLIToken = func(_ context.Context, host string) (string, error) { return token(host) }
	return func() { readCLIToken = previous }
}

// cliToken asks the GitHub CLI for the token it holds for host. The token is
// read for every request and never kept, so a login the user refreshes or
// switches in gh applies at once, and Motty does not store a second copy.
func cliToken(ctx context.Context, host string) (string, error) {
	path, err := ghPath()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "auth", "token", "--hostname", host)
	// gh must never wait for a prompt nobody can see.
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_NO_UPDATE_NOTIFIER=1")
	hideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("gist: the GitHub CLI did not answer: %w", ctx.Err())
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			detail := strings.Join(strings.Fields(stderr.String()), " ")
			return "", fmt.Errorf("%w (%s)", ErrCLINotSignedIn, detail)
		}
		return "", fmt.Errorf("gist: the GitHub CLI could not run: %w", err)
	}
	token := strings.TrimSpace(stdout.String())
	if token == "" || strings.ContainsAny(token, " \r\n\x00") {
		return "", fmt.Errorf("%w (no token)", ErrCLINotSignedIn)
	}
	return token, nil
}
