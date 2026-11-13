package main

import (
	"bytes"
	"debug/buildinfo"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func TestCheckRejectsMissingMismatchedAndDirtyBuilds(t *testing.T) {
	expected := expectedBuild{revision: "0123456789abcdef", when: "2026-10-28T07:55:15Z", target: "windows", arch: "amd64"}
	valid := []debug.BuildSetting{
		{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: expected.revision},
		{Key: "vcs.time", Value: "2026-10-28T15:55:15+08:00"}, {Key: "vcs.modified", Value: "false"},
		{Key: "GOOS", Value: expected.target}, {Key: "GOARCH", Value: expected.arch},
		{Key: "CGO_ENABLED", Value: "0"},
	}
	if err := expected.check(&debug.BuildInfo{Settings: valid}); err != nil {
		t.Fatal(err)
	}
	// The macOS program reaches Cocoa through cgo, so its CGO_ENABLED is 1 and
	// is not checked.
	mac := expected
	mac.target = "darwin"
	withCgo := append([]debug.BuildSetting(nil), valid...)
	for index := range withCgo {
		switch withCgo[index].Key {
		case "GOOS":
			withCgo[index].Value = "darwin"
		case "CGO_ENABLED":
			withCgo[index].Value = "1"
		}
	}
	if err := mac.check(&debug.BuildInfo{Settings: withCgo}); err != nil {
		t.Fatalf("a macOS build was rejected for using cgo: %v", err)
	}
	for _, test := range []struct{ key, value string }{
		{"vcs", ""}, {"vcs.revision", ""}, {"vcs.revision", "another-commit"},
		{"vcs.time", ""}, {"vcs.time", "invalid"}, {"vcs.time", "2026-10-27T07:55:15Z"},
		{"vcs.modified", "true"}, {"GOOS", "linux"}, {"GOARCH", "arm64"},
		{"CGO_ENABLED", "1"},
	} {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			settings := append([]debug.BuildSetting(nil), valid...)
			for index := range settings {
				if settings[index].Key == test.key {
					settings[index].Value = test.value
				}
			}
			if err := expected.check(&debug.BuildInfo{Settings: settings}); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("wrong or missing error: %v", err)
			}
		})
	}
	if err := run(nil, io.Discard); err == nil {
		t.Fatal("an incomplete verification request was accepted")
	}
}

// The release checkout must have a .git directory even when the source was
// itself a linked worktree. Inspect real stripped executables, including a
// foreign target that cannot be executed on this host.
func TestSharedReleaseClonePreservesExecutableBuildInformation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go.mod":  "module example.com/release-fixture\n\ngo 1.26.0\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hooks := filepath.Join(root, "empty-hooks")
	if err := os.Mkdir(hooks, 0o700); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		args = append([]string{"-c", "core.hooksPath=" + hooks, "-c", "commit.gpgsign=false", "-C", dir}, args...)
		command := exec.Command("git", args...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-10-28T07:55:15Z", "GIT_COMMITTER_DATE=2026-10-28T07:55:15Z")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(source, "init", "-q")
	git(source, "add", "go.mod", "main.go")
	git(source, "-c", "user.name=Release test", "-c", "user.email=release@example.invalid", "commit", "-qm", "fixture")
	revision := git(source, "rev-parse", "HEAD")
	linked := filepath.Join(root, "linked")
	git(source, "worktree", "add", "-q", "--detach", linked, "HEAD")
	clone := filepath.Join(root, "release")
	git(source, "clone", "-q", "--shared", "--no-checkout", linked, clone)
	git(clone, "checkout", "-q", "--detach", revision)
	if info, err := os.Stat(filepath.Join(clone, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("release .git is not a directory: %v", err)
	}
	other := "windows"
	if runtime.GOOS == other {
		other = "linux"
	}
	for _, target := range []string{runtime.GOOS, other} {
		t.Run(target, func(t *testing.T) {
			program := filepath.Join(root, "motty-"+target+".exe")
			command := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w -buildid=", "-o", program, ".")
			command.Dir = clone
			command.Env = append(os.Environ(), "GOOS="+target, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=")
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("build fixture: %v\n%s", err, out)
			}
			info, err := buildinfo.ReadFile(program)
			if err != nil {
				t.Fatal(err)
			}
			if err := (expectedBuild{revision, "2026-10-28T07:55:15Z", target, runtime.GOARCH}).check(info); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := run([]string{"-program", program, "-revision", revision, "-time", "2026-10-28T07:55:15Z", "-target", target, "-arch", runtime.GOARCH}, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), revision) {
				t.Fatal("successful verification did not report the commit")
			}
		})
	}
}
