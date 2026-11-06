package localshell

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAvailabilityRunnerUsesConfiguredShellAndBoundsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX shell fixture runs on Unix")
	}
	runner := AvailabilityRunner{Program: "/bin/sh"}
	output, err := runner.RunCommand(context.Background(), "printf 'cat\\n'", 32)
	if err != nil || string(output) != "cat\n" {
		t.Fatalf("local command output=%q err=%v", output, err)
	}
	if _, err := runner.RunCommand(context.Background(), "printf '"+strings.Repeat("x", 100)+"'", 16); err == nil {
		t.Fatal("unbounded local command output was accepted")
	}
}

func TestAvailabilityRunnerBoundsInheritedPipeAfterCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process group fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := (AvailabilityRunner{Program: "/bin/sh"}).RunCommand(ctx, "sleep 10 & wait", 32)
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("local probe cancellation blocked on inherited stdout: %v in %v", err, time.Since(started))
	}
}

func TestAvailabilityRunnerRejectsUnknownShell(t *testing.T) {
	runner := AvailabilityRunner{Program: "not-a-supported-shell"}
	if _, err := runner.RunCommand(context.Background(), "echo hello", 32); err == nil {
		t.Fatal("unknown shell ran a probe")
	}
}
