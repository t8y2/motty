//go:build !windows

package remotestats

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("process %d outlived its command", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The monitor's long stream has a resident child (top on a Mac). Ending the
// stream, by the caller or by the context, must take it too.
func TestLocalStreamLeavesNoChildBehind(t *testing.T) {
	const script = "sleep 60 & echo $!; echo second; wait"
	for _, test := range []struct {
		name string
		run  func(t *testing.T) int
	}{
		{"the caller stops reading", func(t *testing.T) int {
			pid := 0
			err := Local{}.StreamCommand(context.Background(), script, 64, func(line []byte) bool {
				if pid == 0 {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(line)))
					return true
				}
				return false
			})
			if err != nil {
				t.Fatalf("stopping the stream = %v", err)
			}
			return pid
		}},
		{"the context is canceled", func(t *testing.T) int {
			ctx, cancel := context.WithCancel(context.Background())
			pid := 0
			err := Local{}.StreamCommand(ctx, script, 64, func(line []byte) bool {
				if pid == 0 {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(line)))
					return true
				}
				cancel()
				return true
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled stream = %v", err)
			}
			return pid
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pid := test.run(t)
			if pid <= 1 {
				t.Fatal("the script did not report its child")
			}
			waitGone(t, pid)
		})
	}
}

func TestLocalStreamBoundsALineAndReportsAnExit(t *testing.T) {
	err := Local{}.StreamCommand(context.Background(), "head -c 5000 /dev/zero | tr '\\0' a; echo", 64, func([]byte) bool { return true })
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("a line over the limit = %v", err)
	}
	if err := (Local{}).StreamCommand(context.Background(), "echo one; exit 3", 64, func([]byte) bool { return true }); err == nil {
		t.Fatal("a failing command reported success")
	}
	if err := (Local{}).StreamCommand(context.Background(), "echo one", 64, func([]byte) bool { return true }); err != nil {
		t.Fatalf("a command that ended cleanly = %v", err)
	}
}

func TestLocalRunCommandBoundsItsOutputAndFindsSystemTools(t *testing.T) {
	data, err := Local{}.RunCommand(context.Background(), "head -c 100000 /dev/zero | tr '\\0' a", 1000)
	if !errors.Is(err, ErrOutputTooLarge) || len(data) != 1000 {
		t.Fatalf("len=%d err=%v", len(data), err)
	}
	// An application started from a launcher can have a PATH without /usr/sbin,
	// where sysctl and route are.
	t.Setenv("PATH", "/nonexistent")
	data, err = Local{}.RunCommand(context.Background(), "echo $PATH", 1000)
	if err != nil || !strings.HasPrefix(string(data), "/usr/bin:/bin:/usr/sbin:/sbin:") {
		t.Fatalf("PATH = %q, %v", data, err)
	}
}
