package workbench

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestHostKeyInstallCommandPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX filesystem modes and symlinks")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("requires POSIX sh: %v", err)
	}

	key := ssh.MarshalAuthorizedKey(testDeploySigner(t).PublicKey())
	run := func(t *testing.T, home string, wantExit int, wantOutput string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, sh, "-c", hostKeyInstallCommand)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = bytes.NewReader(key)
		output, err := cmd.CombinedOutput()
		if wantExit == 0 && err != nil {
			t.Fatalf("command failed: %v\n%s", err, output)
		}
		if wantExit != 0 {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != wantExit {
				t.Fatalf("command error = %v, want exit %d\n%s", err, wantExit, output)
			}
		}
		if string(output) != wantOutput {
			t.Fatalf("output = %q, want %q", output, wantOutput)
		}
	}
	write := func(t *testing.T, path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	check := func(t *testing.T, path string, want []byte) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("%s = %q, want %q", path, data, want)
		}
	}
	mode := func(t *testing.T, path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s permissions = %04o, want %04o", path, got, want)
		}
	}
	makeSSHDir := func(t *testing.T, home string) string {
		t.Helper()
		dir := filepath.Join(home, ".ssh")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("create with private permissions and repeat without duplicates", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".ssh")
		file := filepath.Join(dir, "authorized_keys")
		run(t, home, 0, "motty-key-installed\n")
		check(t, file, key)
		mode(t, dir, 0700)
		mode(t, file, 0600)
		run(t, home, 0, "motty-key-already-present\n")
		check(t, file, key)
	})

	t.Run("existing key restrictions remain untouched", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(makeSSHDir(t, home), "authorized_keys")
		restricted := append([]byte("restrict,command=\"echo only this\" "), bytes.TrimSpace(key)...)
		restricted = append(restricted, []byte(" existing comment\r\n")...)
		write(t, file, restricted)
		run(t, home, 0, "motty-key-already-present\n")
		check(t, file, restricted)
	})

	t.Run("append after a different key without final newline", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(makeSSHDir(t, home), "authorized_keys")
		original := bytes.TrimSpace(ssh.MarshalAuthorizedKey(testDeploySigner(t).PublicKey()))
		write(t, file, original)
		want := append(append(bytes.Clone(original), '\n'), key...)
		run(t, home, 0, "motty-key-installed\n")
		check(t, file, want)
		run(t, home, 0, "motty-key-already-present\n")
		check(t, file, want)
	})

	t.Run("append after a different key ending in newline adds no blank line", func(t *testing.T) {
		home := t.TempDir()
		file := filepath.Join(makeSSHDir(t, home), "authorized_keys")
		other := ssh.MarshalAuthorizedKey(testDeploySigner(t).PublicKey())
		original := append(bytes.Clone(other), '\n')
		write(t, file, original)
		want := append(bytes.Clone(original), key...)
		run(t, home, 0, "motty-key-installed\n")
		check(t, file, want)
		run(t, home, 0, "motty-key-already-present\n")
		check(t, file, want)
	})

	t.Run("stale lock directory from an older version does not block", func(t *testing.T) {
		home := t.TempDir()
		dir := makeSSHDir(t, home)
		lock := filepath.Join(dir, ".motty-authorized-keys.lock")
		if err := os.Mkdir(lock, 0700); err != nil {
			t.Fatal(err)
		}
		run(t, home, 0, "motty-key-installed\n")
		if info, err := os.Stat(lock); err == nil && info.IsDir() {
			t.Fatal("the stale lock directory was not removed")
		}
	})

	for _, linkDirectory := range []bool{true, false} {
		name := "refuse authorized_keys symlink"
		if linkDirectory {
			name = "refuse .ssh directory symlink"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "authorized_keys")
			sentinel := []byte("untouched outside the fixture HOME\n")
			write(t, target, sentinel)
			if linkDirectory {
				if err := os.Symlink(outside, filepath.Join(home, ".ssh")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(target, filepath.Join(makeSSHDir(t, home), "authorized_keys")); err != nil {
					t.Fatal(err)
				}
			}
			run(t, home, 24, "")
			check(t, target, sentinel)
		})
	}
}
