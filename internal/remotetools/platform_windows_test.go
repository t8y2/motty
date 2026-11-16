package remotetools

// These tests only build on Windows. The default CI runs Linux, so this file
// is a Windows developer-machine check; the Windows job in .github/workflows/
// tests.yml runs the package with -short, which skips the real CIM query and
// leaves the command-and-parser tests there.
import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type windowsCommandRunner struct{ shell string }

func (r windowsCommandRunner) RunCommand(ctx context.Context, c string, _ int) ([]byte, error) {
	if r.shell == "cmd" {
		return exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", c).CombinedOutput()
	}
	if r.shell == "powershell" {
		return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", c).CombinedOutput()
	}
	args := strings.Fields(c)
	return exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
}
func TestWindowsRemoteProcessScriptOnRealCIM(t *testing.T) {
	if testing.Short() {
		t.Skip("real CIM queries do not run in the short Windows CI job")
	}
	for _, shell := range []string{"cmd", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			rows, err := ReadSafeProcessSnapshot(ctx, ForSystem(windowsCommandRunner{shell: shell}, "windows"), SortCPU)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows.Processes) == 0 {
				t.Fatal("no real processes")
			}
			for _, p := range rows.Processes {
				if p.PID <= 0 || p.Command == "" || p.Elapsed == "" || p.StartIdentity != "" {
					t.Fatalf("invalid process %+v", p)
				}
			}
		})
	}
}

func TestWindowsDockerQueriesOnRealCLI(t *testing.T) {
	if os.Getenv("MOTTY_TEST_DOCKER_NATIVE") != "1" {
		t.Skip("opt-in local Docker Desktop smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := ForSystem(windowsCommandRunner{shell: "cmd"}, "windows")
	if _, err := Containers(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{Images, Volumes, Networks} {
		if _, err := Resources(ctx, r, kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Compose(ctx, r); err != nil {
		t.Fatal(err)
	}
}
