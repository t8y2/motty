//go:build darwin || windows || linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"motty/internal/buildinfo"
)

const claimHelperEnv = "MOTTY_CLAIM_HELPER"

// TestClaimSubprocessHelper is not a test: it is the child side of
// TestSecondLaunchHandsOffWithoutOpeningStorage.
func TestClaimSubprocessHelper(t *testing.T) {
	if os.Getenv(claimHelperEnv) != "1" {
		t.Skip("helper process")
	}
	dir := os.Getenv("MOTTY_DATA_DIR")
	primary, claim, err := claimLaunch(dir, nil, func([]string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !primary {
		explainSecondaryLaunch(os.Stderr)
		return
	}
	defer claim.Close()
	path := filepath.Join(dir, "storage-opened")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("pid=%d", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestSecondLaunchHandsOffWithoutOpeningStorage(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("TMPDIR", runtimeDir)
	t.Setenv("TEMP", runtimeDir)
	t.Setenv("TMP", runtimeDir)
	dataDir := filepath.Join(dir, "data")
	first := exec.Command(os.Args[0], "-test.run=^TestClaimSubprocessHelper$")
	first.Env = append(os.Environ(), claimHelperEnv+"=1", "MOTTY_DATA_DIR="+dataDir)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = first.Process.Kill()
		_ = first.Wait()
	}()
	waitForFile(t, filepath.Join(dataDir, "storage-opened"))

	second := exec.Command(os.Args[0], "-test.run=^TestClaimSubprocessHelper$")
	second.Env = append(os.Environ(), claimHelperEnv+"=1", "MOTTY_DATA_DIR="+dataDir)
	output, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("second launch: %v\n%s", err, output)
	}
	announced := strings.Contains(string(output), "motty: secondary launch handed off")
	if buildinfo.Development != announced {
		t.Fatalf("development build = %t, second launch output = %q", buildinfo.Development, output)
	}
	if first.ProcessState != nil && first.ProcessState.Exited() {
		t.Fatal("the first process exited")
	}
	marker := filepath.Join(dataDir, "storage-opened")
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != fmt.Sprintf("pid=%d", first.Process.Pid) {
		t.Fatalf("the second launch changed the first process's storage marker: %q, %v", content, err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s was not written", path)
}
