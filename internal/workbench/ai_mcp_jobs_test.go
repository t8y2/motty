//go:build !windows

package workbench

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

// shellThatStreams runs what it is typed in the background, so output arrives
// while the command runs, and ends the running command on Ctrl-C like a tty.
func shellThatStreams(shell *fakeShell) {
	var mu sync.Mutex
	var stop context.CancelFunc
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
		mu.Unlock()
		shell.say(strings.ReplaceAll(string(p), "\n", "\r\n"))
		go func() {
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/sh", "-c", string(p))
			command.WaitDelay = 100 * time.Millisecond
			command.Stdout = writerFunc(func(out []byte) { shell.say(strings.ReplaceAll(string(out), "\n", "\r\n")) })
			command.Stdin = strings.NewReader("")
			_ = command.Run()
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
	state, _, address, token := hubFixtureWith(t, "ask", shellThatStreams)
	client := newJobClient(t, state, address, token)

	started, failed := client.call("terminal_start", map[string]any{"command": "printf 'JOB_BEGIN\\n'; sleep 1; printf 'JOB_STEP:1\\n'; sleep 1; printf 'JOB_END\\n'"})
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

func TestHTTPMCPJobStopCancelsAndFreesTheTerminalForTheNextCommand(t *testing.T) {
	state, _, address, token := hubFixtureWith(t, "ask", shellThatStreams)
	client := newJobClient(t, state, address, token)
	other := newJobClient(t, state, address, token)

	started, _ := client.call("terminal_start", map[string]any{"command": "printf 'WAITING\\n'; sleep 30; printf 'NEVER\\n'"})
	id := started["job_id"].(string)
	if _, failed := client.call("terminal_start", map[string]any{"command": "echo second"}); !failed {
		t.Fatal("a second job started in a busy terminal")
	}
	if _, failed := other.call("terminal_poll", map[string]any{"job_id": id}); !failed {
		t.Fatal("another MCP session read the job")
	}
	if _, failed := other.call("terminal_stop", map[string]any{"job_id": id}); !failed {
		t.Fatal("another MCP session stopped the job")
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		status, _ := client.call("terminal_poll", map[string]any{"job_id": id})
		if strings.Contains(fmt.Sprint(status["data"], status["pending_line"]), "WAITING") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job output never appeared: %v\n%s", status, screenText(state, 0))
		}
		time.Sleep(20 * time.Millisecond)
	}

	stopped, failed := client.call("terminal_stop", map[string]any{"job_id": id})
	if failed || stopped["state"] != "cancelled" || stopped["interrupt_sent"] != true {
		t.Fatalf("stop = %v", stopped)
	}
	// This shell reports no prompts, so readiness cannot be confirmed.
	if stopped["terminal_ready"] != "unconfirmed" {
		t.Fatalf("terminal_ready = %v", stopped["terminal_ready"])
	}
	again, _ := client.call("terminal_stop", map[string]any{"job_id": id})
	if again["interrupt_sent"] != false || again["state"] != "cancelled" {
		t.Fatalf("a second stop = %v", again)
	}
	status, _ := client.call("terminal_poll", map[string]any{"job_id": id})
	if strings.Contains(status["data"].(string), "^C") || strings.Contains(status["data"].(string), "NEVER") {
		t.Fatalf("interrupt echo leaked into the job output: %q", status["data"])
	}

	next, failed := client.call("terminal_exec", map[string]any{"command": "echo after-stop"})
	if failed {
		t.Fatalf("terminal_exec after stop failed: %v", next)
	}
	if _, failed := client.call("terminal_poll", map[string]any{"job_id": "w1:nope"}); !failed {
		t.Fatal("an unknown job id was accepted")
	}
}
