package workbench

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/sshconn"
)

// This exercises the settings actions, the actual hub and HTTP JSON-RPC gate,
// then the window's real backend. Only the remote shell/files are fixtures;
// commands are recorded, never passed to an operating-system shell.
func TestMCPHTTPConsentControlsRealToolCallsAndRevocation(t *testing.T) {
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	conn := connect(t, state, world)
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := conn.shell(t)
	shell.say("\x1b[0m")
	state.Wake()
	var executions atomic.Int32
	marker := regexp.MustCompile(`GOPAI_ ([0-9a-f]{16})`)
	shell.onWrite = func(input []byte) {
		match := marker.FindSubmatch(input)
		if len(match) == 0 {
			return
		}
		executions.Add(1)
		shell.say(string(input) + "\r\nhttp-fixture-output\r\nGOPAI_" + string(match[1]) + "0\r\n")
	}
	remote := &workbenchSFTPRemote{
		cwd: "/tmp", entries: map[string][]os.FileInfo{"/tmp": {workbenchFileInfo{name: "mcp-test", mode: 0600}}},
		files: map[string][]byte{"/tmp/mcp-test": []byte("fixture")},
	}
	conn.sftp = remote
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsMCP)
	hub := NewAIHub(options.AIStorePath)
	t.Cleanup(hub.Close)
	hub.Add(state, nil)
	state.options.AISettingsChanged = func() {
		if err := hub.Reconfigure(); err != nil {
			t.Fatal(err)
		}
	}
	state.Click(hitAISetMCPEnabled)
	state.Wake()
	call := func(name, args string, wantError bool) {
		t.Helper()
		result := mcpHTTPToolCall(t, state, hub.Address(), state.ai.mcpToken, name, args)
		if result.IsError != wantError {
			t.Fatalf("%s: error=%t, text=%s", name, result.IsError, result.Content[0].Text)
		}
	}
	checkUntouched := func() {
		t.Helper()
		remote.mu.Lock()
		_, exists := remote.files["/tmp/mcp-test"]
		remote.mu.Unlock()
		if executions.Load() != 0 || !exists {
			t.Fatal("an unconfirmed tool call reached the remote backend")
		}
	}
	state.Click(hitAISetMCPAllowAll)
	call("terminal_exec", `{"command":"rm -rf /"}`, true)
	call("sftp_remove", `{"path":"/tmp/mcp-test"}`, true)
	checkUntouched()
	state.Click(hitAISetConfirmCancel)
	call("terminal_exec", `{"command":"touch /tmp/mcp-test"}`, true)
	checkUntouched()
	state.Click(hitAISetMCPAllowAll)
	state.Click(hitAISetConfirmAction)
	state.Wake()
	call("terminal_exec", `{"command":"rm -rf /"}`, false)
	call("sftp_remove", `{"path":"/tmp/mcp-test"}`, false)
	remote.mu.Lock()
	_, exists := remote.files["/tmp/mcp-test"]
	remote.mu.Unlock()
	if executions.Load() != 1 || exists || len(state.ai.approvals) != 0 {
		t.Fatal("confirmed access did not execute both tools without approval")
	}
	state.Click(hitAISetMCPObserve)
	state.Wake()
	call("terminal_exec", `{"command":"touch /tmp/revoked"}`, true)
	call("sftp_write", `{"path":"/tmp/revoked","content":"no"}`, true)
	call("sftp_remove", `{"path":"/tmp/revoked"}`, true)
	remote.mu.Lock()
	_, exists = remote.files["/tmp/revoked"]
	remote.mu.Unlock()
	if executions.Load() != 1 || exists {
		t.Fatal("revoked access still reached the backend")
	}
}

type mcpHTTPTestResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func mcpHTTPToolCall(t *testing.T, state *State, address, token, name, args string) mcpHTTPTestResult {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	type reply struct {
		result mcpHTTPTestResult
		err    error
	}
	done := make(chan reply, 1)
	go func() {
		request, err := http.NewRequest(http.MethodPost, address, strings.NewReader(body))
		if err != nil {
			done <- reply{err: err}
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		client := http.Client{Timeout: 5 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			done <- reply{err: err}
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			done <- reply{err: err}
			return
		}
		var out struct {
			Result mcpHTTPTestResult `json:"result"`
		}
		if response.StatusCode != http.StatusOK || bytes.Contains(data, []byte(`"error":`)) {
			done <- reply{err: fmt.Errorf("HTTP %d: %s", response.StatusCode, data)}
			return
		}
		err = json.Unmarshal(data, &out)
		done <- reply{result: out.Result, err: err}
	}()
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case response := <-done:
			if response.err != nil {
				t.Fatal(response.err)
			}
			if len(response.result.Content) == 0 {
				t.Fatal("MCP returned no tool result")
			}
			return response.result
		case <-ticker.C:
			state.Wake()
		case <-deadline.C:
			t.Fatal("HTTP MCP call did not finish")
		}
	}
}
