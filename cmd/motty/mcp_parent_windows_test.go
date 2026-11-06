//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

const watchHelperEnv = "MOTTY_TEST_WATCH_PARENT"

// TestHelperSleep is not a test: it is the process the watch tests watch. It
// lives waiting on stdin, which the test keeps open, until it is killed.
func TestHelperSleep(t *testing.T) {
	if os.Getenv(watchHelperEnv) != "sleep" {
		t.Skip("helper process")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// startSleepHelper starts a process that lives until it is killed, with its
// stdin left open so nothing ends it on its own.
func startSleepHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), watchHelperEnv+"=sleep")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// Nothing may happen while the watched process lives; the context must end once
// it is gone.
func TestWatchProcessCancelsWhenTheProcessEnds(t *testing.T) {
	helper := startSleepHelper(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := watchProcess(helper.Process.Pid, cancel)
	defer stop()
	if cancelled(ctx) {
		t.Fatal("the watch cancelled while the process it watches was running")
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitCancelled(t, ctx)
}

// A process that ended before the server got to watch it must cancel at once.
func TestWatchProcessCancelsForAProcessAlreadyGone(t *testing.T) {
	helper := startSleepHelper(t)
	pid := helper.Process.Pid
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer watchProcess(pid, cancel)()
	waitCancelled(t, ctx)
}

// The parent is always older than the server it started, so a live process that
// is younger than this one is a reused id, not the parent: it must cancel at
// once instead of keeping the server alive behind a stranger.
func TestWatchOlderProcessCancelsForAProcessYoungerThanUs(t *testing.T) {
	helper := startSleepHelper(t) // started after this process, and still running
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer watchOlderProcess(helper.Process.Pid, cancel)()
	waitCancelled(t, ctx)
}

// Stopping the watch of a live process must return, and must not cancel.
func TestWatchProcessStopLeavesTheServerRunning(t *testing.T) {
	helper := startSleepHelper(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := watchProcess(helper.Process.Pid, cancel)
	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("stop did not return")
	}
	if cancelled(ctx) {
		t.Fatal("stop cancelled the server it was watching for")
	}
}

func cancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func waitCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the context was not cancelled within 10 seconds")
	}
}
