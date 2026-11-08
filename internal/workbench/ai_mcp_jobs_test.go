//go:build windows || darwin || linux

package workbench

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/shellkind"
)

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

// jobCommands use only echo and sleep, which PowerShell aliases to its own
// cmdlets, so one text runs in either shell and stays low risk to the policy.
var jobCommands = struct{ progress, waiting string }{
	progress: "echo JOB_BEGIN; sleep 1; echo JOB_STEP:1; sleep 1; echo JOB_END",
	waiting:  "echo WAITING; sleep 30; echo NEVER",
}

// shellThatStreams runs what it is typed in the background, so output arrives
// while the command runs, and ends the running command on Ctrl-C like a tty.
func shellThatStreams(t *testing.T, state *State, shell *fakeShell) {
	var mu sync.Mutex
	var stop context.CancelFunc
	powershell := runtime.GOOS == "windows"
	dir, count := t.TempDir(), 0
	if powershell {
		state.sessions[0].term.shell = kindShell{shell, shellkind.PowerShell}
	}
	screenText := func(text string) string {
		text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	shell.onWrite = func(p []byte) {
		if string(p) == "\x03" {
			mu.Lock()
			if stop != nil {
				stop()
			}
			mu.Unlock()
			shell.say("^C\r\n")
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		mu.Lock()
		stop = cancel
		count++
		count := count
		mu.Unlock()
		shell.say(screenText(string(p)))
		go func() {
			defer cancel()
			script := strings.ReplaceAll(string(p), "\r", "\n")
			command := exec.CommandContext(ctx, "/bin/sh", "-c", script)
			if powershell {
				file := filepath.Join(dir, fmt.Sprintf("job%d.ps1", count))
				if err := os.WriteFile(file, []byte(script), 0o644); err != nil {
					shell.say("test shell: " + err.Error() + "\r\n")
					return
				}
				command = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file)
			}
			command.WaitDelay = 100 * time.Millisecond
			command.Stdout = writerFunc(func(out []byte) {
				shell.say(screenText(string(out)))
			})
			command.Stdin = strings.NewReader("")
			err := command.Run()
			if err != nil && ctx.Err() == nil {
				shell.say("test shell: " + err.Error() + "\r\n")
			}
		}()
	}
}

type jobClient struct {
	t       *testing.T
	state   *State
	address string
	token   string
	id      string
}

func newJobClient(t *testing.T, state *State, address, token string) *jobClient {
	t.Helper()
	_, id, status := pagedRPC(t, state, address, token, "", "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test", "version": "1"}})
	if status != 200 || id == "" {
		t.Fatal("no MCP session")
	}
	return &jobClient{t, state, address, token, id}
}

func (c *jobClient) call(tool string, arguments map[string]any) (map[string]any, bool) {
	c.t.Helper()
	out, _, status := pagedRPC(c.t, c.state, c.address, c.token, c.id, "tools/call", map[string]any{"name": tool, "arguments": arguments})
	if status != 200 {
		c.t.Fatalf("%s: HTTP %d", tool, status)
	}
	_, failed := toolText(c.t, out)
	structured, _ := out["result"].(map[string]any)["structuredContent"].(map[string]any)
	return structured, failed
}

func TestHTTPMCPJobReportsProgressWhileTheCommandRuns(t *testing.T) {
	state, _, address, token := hubFixtureWith(t, "ask", func(state *State, shell *fakeShell) { shellThatStreams(t, state, shell) })
	client := newJobClient(t, state, address, token)

	started, failed := client.call("terminal_start", map[string]any{"command": jobCommands.progress})
	if failed || started["state"] != "running" || started["job_id"] == "" {
		t.Fatalf("start = %v", started)
	}
	id := started["job_id"].(string)

	var output strings.Builder
	offset, sawRunning := 0, false
	deadline := time.Now().Add(15 * time.Second)
	for {
		status, failed := client.call("terminal_poll", map[string]any{"job_id": id, "offset": offset})
		if failed {
			t.Fatalf("poll failed: %v", status)
		}
		output.WriteString(status["data"].(string))
		offset = int(status["next_offset"].(float64))
		if status["state"] == "running" {
			sawRunning = sawRunning || strings.Contains(output.String(), "JOB_BEGIN")
			if status["terminal_ready"] != "busy" {
				t.Fatalf("a running job left the terminal %v", status["terminal_ready"])
			}
		} else {
			if status["state"] != "completed" || status["exit_code"].(float64) != 0 || status["terminal_ready"] != "ready" {
				t.Fatalf("final status = %v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawRunning {
		t.Fatal("no output was readable while the job was running")
	}
	if got, want := output.String(), "JOB_BEGIN\nJOB_STEP:1\nJOB_END"; strings.TrimRight(got, "\n") != want {
		t.Fatalf("output %q, want %q", got, want)
	}
	// Another command can follow, and the finished job stays readable.
	if again, failed := client.call("terminal_poll", map[string]any{"job_id": id}); failed || again["total_bytes"].(float64) != float64(offset) {
		t.Fatalf("finished job changed: %v", again)
	}
}
