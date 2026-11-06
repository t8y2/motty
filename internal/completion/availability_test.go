package completion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"motty/internal/shellkind"
)

type availabilityRunnerFunc func(context.Context, string, int) ([]byte, error)

func (run availabilityRunnerFunc) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	return run(ctx, command, limit)
}

func TestProbeAvailabilityUsesOnlyValidatedNames(t *testing.T) {
	called := false
	runner := availabilityRunnerFunc(func(_ context.Context, command string, limit int) ([]byte, error) {
		called = true
		if limit != 4096 || !strings.Contains(command, `command -v "$name"`) || strings.Contains(command, "rm -rf") {
			t.Fatalf("unsafe probe: limit=%d command=%q", limit, command)
		}
		return []byte("cat\ncargo\nnot-requested\n"), nil
	})
	if _, err := ProbeAvailable(context.Background(), runner, shellkind.POSIX, []string{"cat", "$(rm -rf /)"}); err == nil || called {
		t.Fatalf("invalid name reached runner: err=%v called=%v", err, called)
	}
	available, err := ProbeAvailable(context.Background(), runner, shellkind.POSIX, []string{"cat", "cargo", "curl"})
	if err != nil || !called || !available["cat"] || !available["cargo"] || available["curl"] || available["not-requested"] {
		t.Fatalf("availability=%v err=%v called=%v", available, err, called)
	}
}

func TestProbeAvailabilityPowerShellAndUnsupportedShell(t *testing.T) {
	calls := 0
	runner := availabilityRunnerFunc(func(_ context.Context, command string, limit int) ([]byte, error) {
		calls++
		if !strings.Contains(command, "Get-Command") || !strings.Contains(command, "'Get-ChildItem'") || limit != 4096 {
			t.Fatalf("unexpected PowerShell probe: %q limit=%d", command, limit)
		}
		return []byte("Get-ChildItem\r\n"), nil
	})
	available, err := ProbeAvailable(context.Background(), runner, shellkind.PowerShell, []string{"Get-ChildItem"})
	if err != nil || !available["Get-ChildItem"] {
		t.Fatalf("PowerShell availability=%v err=%v", available, err)
	}
	if _, err := ProbeAvailable(context.Background(), runner, shellkind.Unknown, []string{"cat"}); err == nil || calls != 1 {
		t.Fatalf("unknown shell was probed: err=%v calls=%d", err, calls)
	}
}

func TestProbeAvailabilityBoundsOutputAndCancellation(t *testing.T) {
	tooMuch := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte(strings.Repeat("cat\n", 2000)), nil
	})
	if _, err := ProbeAvailable(context.Background(), tooMuch, shellkind.POSIX, []string{"cat"}); err == nil {
		t.Fatal("oversized probe response was accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	waiting := availabilityRunnerFunc(func(ctx context.Context, _ string, _ int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := ProbeAvailable(ctx, waiting, shellkind.POSIX, []string{"cat"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled probe: %v", err)
	}
}
