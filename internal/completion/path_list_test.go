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

func TestListPathCandidatesUsesQuotedReadOnlyQuery(t *testing.T) {
	query, ok := ParsePathQuery("tail -f /tmp/logs/lo", "/home/user")
	if !ok {
		t.Fatal("no path query")
	}
	runner := availabilityRunnerFunc(func(_ context.Context, command string, limit int) ([]byte, error) {
		if limit != 4096 || !strings.Contains(command, "dir='/tmp/logs'") || !strings.Contains(command, "prefix='lo'") ||
			!strings.Contains(command, `"$dir"/"$prefix"*`) || strings.Contains(command, "rm -") {
			t.Fatalf("unsafe path command=%q limit=%d", command, limit)
		}
		return []byte("flog.txt\x00dlogs\x00flog with spaces\x00dlo;evil\x00flogs/evil\x00flog.txt\x00"), nil
	})
	candidates, err := ListPathCandidates(context.Background(), runner, query)
	if err != nil || len(candidates) != 2 || candidates[0].Text != "/tmp/logs/log.txt" || candidates[0].Insert != "g.txt" ||
		candidates[1].Text != "/tmp/logs/logs/" || candidates[1].Insert != "gs/" || !candidates[1].IsDirectory {
		t.Fatalf("path candidates=%+v err=%v", candidates, err)
	}
}

func TestListPathCandidatesBoundsOutputAndFiltersFilesForCd(t *testing.T) {
	query, _ := ParsePathQuery("cd ./lo", "/tmp")
	runner := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte("flog.txt\x00dlogs\x00dlo\nspoof\x00"), nil
	})
	candidates, err := ListPathCandidates(context.Background(), runner, query)
	if err != nil || len(candidates) != 1 || candidates[0].Text != "./logs/" {
		t.Fatalf("directory-only candidates=%+v err=%v", candidates, err)
	}
	tooMuch := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte(strings.Repeat("flog.txt\x00", 500)), nil
	})
	if _, err := ListPathCandidates(context.Background(), tooMuch, query); err == nil {
		t.Fatal("oversized directory output was accepted")
	}
}

func TestListPathCandidatesCancelsAndRejectsIncompleteRecords(t *testing.T) {
	query, _ := ParsePathQuery("cat /tmp/lo", "/home/user")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	waiting := availabilityRunnerFunc(func(ctx context.Context, _ string, _ int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if _, err := ListPathCandidates(ctx, waiting, query); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled path query returned %v", err)
	}
	incomplete := availabilityRunnerFunc(func(context.Context, string, int) ([]byte, error) {
		return []byte("flog.txt\x00dlogs"), nil
	})
	candidates, err := ListPathCandidates(context.Background(), incomplete, query)
	if err != nil || len(candidates) != 1 || candidates[0].Text != "/tmp/log.txt" {
		t.Fatalf("incomplete path output trusted: %+v err=%v", candidates, err)
	}
}

func TestListPathCandidatesRunsWithRealPOSIXShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "log.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	query, ok := ParsePathQuery("tail -f "+filepath.ToSlash(filepath.Join(directory, "lo")), "/")
	if !ok {
		t.Fatal("safe path fixture was rejected")
	}
	for _, name := range []string{"sh", "bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable", name)
			}
			if shellkind.FromProgram(program) != shellkind.POSIX {
				t.Fatal("fixture shell was not POSIX")
			}
			candidates, err := ListPathCandidates(context.Background(), localshell.AvailabilityRunner{Program: program}, query)
			if err != nil || len(candidates) != 2 {
				t.Fatalf("%s path candidates=%+v err=%v", name, candidates, err)
			}
		})
	}
}

func TestListPathCandidatesQuotesARealCWDContainingAnApostrophe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	directory := filepath.Join(t.TempDir(), "it's files")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "log.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	query, ok := ParsePathQuery("cat lo", directory)
	if !ok {
		t.Fatal("safe quoted-CWD fixture was rejected")
	}
	candidates, err := ListPathCandidates(context.Background(), localshell.AvailabilityRunner{Program: "/bin/sh"}, query)
	if err != nil || len(candidates) != 1 || candidates[0].Text != "log.txt" {
		t.Fatalf("quoted CWD path candidates=%+v err=%v", candidates, err)
	}
}
