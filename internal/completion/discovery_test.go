package completion

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"motty/internal/localshell"
	"motty/internal/shellkind"
)

func TestDiscoverCommandsRejectsUnsafePrefixesBeforeRunning(t *testing.T) {
	called := false
	runner := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		called = true
		return nil, nil
	})
	for _, prefix := range []string{"a", "$(id)", "ab;rm", "ab cd", "../ab", "ab\n", strings.Repeat("a", 65)} {
		if _, err := DiscoverCommands(context.Background(), runner, shellkind.POSIX, prefix); err == nil {
			t.Errorf("accepted unsafe prefix %q", prefix)
		}
	}
	if called {
		t.Fatal("an unsafe prefix reached the host")
	}
}

func TestDiscoverCommandsPOSIXFiltersAndBoundsHostOutput(t *testing.T) {
	runner := availabilityRunnerFunc(func(_ context.Context, command string, limit int) ([]byte, error) {
		if limit != 4096 || !strings.Contains(command, "${path%%:*}") || !strings.Contains(command, `"$dir"/"mo"*`) ||
			!strings.Contains(command, "-x") || strings.Contains(command, "$(mo") {
			t.Fatalf("unsafe or unbounded PATH query: %q limit=%d", command, limit)
		}
		return []byte("motty-local\x00mores\x00motty-local\x00other\x00mo bad\x00mo\nspoof\x00mo;run\x00"), nil
	})
	names, err := DiscoverCommands(context.Background(), runner, shellkind.POSIX, "mo")
	if err != nil || len(names) != 2 || names[0] != "motty-local" || names[1] != "mores" {
		t.Fatalf("discovered=%q err=%v", names, err)
	}
	oversized := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte(strings.Repeat("motty\x00", 800)), nil
	})
	if _, err := DiscoverCommands(context.Background(), oversized, shellkind.POSIX, "mo"); err == nil {
		t.Fatal("oversized output was published")
	}
}

func TestDiscoverCommandsPowerShellIsBoundedAndUnsupportedShellIsSilent(t *testing.T) {
	calls := 0
	runner := availabilityRunnerFunc(func(_ context.Context, command string, limit int) ([]byte, error) {
		calls++
		if limit != 4096 || !strings.Contains(command, "Get-Command") || !strings.Contains(command, "-CommandType Application") ||
			!strings.Contains(command, "'Get-*'") && !strings.Contains(command, "'get-*'") || !strings.Contains(command, "Select-Object -First 64") {
			t.Fatalf("PowerShell query=%q limit=%d", command, limit)
		}
		return []byte("Get-Tool\r\nGet-Tool\nOther\n"), nil
	})
	names, err := DiscoverCommands(context.Background(), runner, shellkind.PowerShell, "Get-")
	if err != nil || len(names) != 1 || names[0] != "Get-Tool" {
		t.Fatalf("PowerShell names=%q err=%v", names, err)
	}
	names, err = DiscoverCommands(context.Background(), runner, shellkind.PowerShell, "get-")
	if err != nil || len(names) != 1 || names[0] != "Get-Tool" {
		t.Fatalf("case-insensitive PowerShell names=%q err=%v", names, err)
	}
	if _, err := DiscoverCommands(context.Background(), runner, shellkind.Unknown, "Get-"); err == nil || calls != 2 {
		t.Fatalf("unknown shell ran command: calls=%d err=%v", calls, err)
	}
}

func TestDiscoverCommandsCancelsSlowRunner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	runner := availabilityRunnerFunc(func(ctx context.Context, _ string, _ int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := DiscoverCommands(ctx, runner, shellkind.POSIX, "mo"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled discovery returned %v", err)
	}
}

func TestDiscoverCommandsIgnoresIncompletePOSIXRecord(t *testing.T) {
	runner := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte("motty-good\x00motty-incomplete"), nil
	})
	names, err := DiscoverCommands(context.Background(), runner, shellkind.POSIX, "mo")
	if err != nil || len(names) != 1 || names[0] != "motty-good" {
		t.Fatalf("incomplete record was trusted: %q %v", names, err)
	}
}

func TestDiscoverCommandsReadsRealLocalPATHWithoutExecutingMatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PATH fixture")
	}
	var programs []string
	for _, name := range []string{"sh", "bash", "zsh"} {
		if program, err := exec.LookPath(name); err == nil {
			programs = append(programs, program)
		}
	}
	directory := t.TempDir()
	for _, file := range []struct {
		name string
		mode os.FileMode
	}{
		{"zqmotty-executable", 0o700},
		{"zqmotty-not-executable", 0o600},
		{"zqmotty bad", 0o700},
	} {
		if err := os.WriteFile(filepath.Join(directory, file.name), []byte("exit 99\n"), file.mode); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	for _, program := range programs {
		t.Run(filepath.Base(program), func(t *testing.T) {
			names, err := DiscoverCommands(context.Background(), localshell.AvailabilityRunner{Program: program}, shellkind.POSIX, "zqmotty")
			if err != nil || len(names) != 1 || names[0] != "zqmotty-executable" {
				t.Fatalf("%s real PATH names=%q err=%v", program, names, err)
			}
		})
	}
}
