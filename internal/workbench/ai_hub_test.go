//go:build !windows

package workbench

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hubFixture is a window with a live shell (running commands in /bin/sh) and a
// hub serving it, configured the way the settings page would.
func hubFixture(t *testing.T, mode string) (*State, *AIHub, string, string) {
	t.Helper()
	return hubFixtureWith(t, mode, nil)
}

// hubFixtureWith lets a test replace the shell's behaviour before the hub starts.
func hubFixtureWith(t *testing.T, mode string, configure func(*fakeShell)) (*State, *AIHub, string, string) {
	t.Helper()
	state, shell := assistantSession(t, &scriptedModel{turn: nil})
	if configure != nil {
		configure(shell)
	}
	path := state.options.AIStorePath
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"ai.mcp.enabled": "on", "ai.mcp.mode": mode} {
		if err := db.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewAIHub(path)
	t.Cleanup(hub.Close)
	hub.Add(state, nil)
	if err := hub.Reconfigure(); err != nil {
		t.Fatal(err)
	}
	address := hub.Address()
	if address == "" {
		t.Fatal("the hub did not start")
	}
	token, _, _ := db.Setting("ai.mcp.token")
	return state, hub, address, token
}

// rpc posts one JSON-RPC request while the window thread keeps waking, as the
// event loop would.
func rpc(t *testing.T, state *State, address, token, method, params string) map[string]any {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`
	type reply struct {
		status int
		data   []byte
		err    error
	}
	done := make(chan reply, 1)
	go func() {
		request, _ := http.NewRequest(http.MethodPost, address, bytes.NewReader([]byte(body)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			done <- reply{err: err}
			return
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		done <- reply{status: response.StatusCode, data: data}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case r := <-done:
			if r.err != nil || r.status != 200 {
				t.Fatalf("%s: status %d err %v body %s", method, r.status, r.err, r.data)
			}
			var out map[string]any
			if err := json.Unmarshal(r.data, &out); err != nil {
				t.Fatalf("%s: %v: %s", method, err, r.data)
			}
			return out
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s timed out", method)
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

func toolText(t *testing.T, out map[string]any) (string, bool) {
	t.Helper()
	result, _ := out["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in %v", out)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	isError, _ := result["isError"].(bool)
	return text, isError
}

func TestAnExternalAgentSeesTheSessionsAndRunsAReadOnlyCommandWhenAllowedToAsk(t *testing.T) {
	state, _, address, token := hubFixture(t, "ask")
	out := rpc(t, state, address, token, "tools/call", `{"name":"list_sessions","arguments":{}}`)
	text, isError := toolText(t, out)
	if isError || !strings.Contains(text, "w1:") || !strings.Contains(text, "ready") {
		t.Fatalf("list_sessions = %q (error %t)", text, isError)
	}
	out = rpc(t, state, address, token, "tools/call", `{"name":"terminal_exec","arguments":{"command":"echo from-mcp"}}`)
	text, isError = toolText(t, out)
	if isError || !strings.Contains(text, "from-mcp") || !strings.Contains(text, "exit status 0") {
		t.Fatalf("terminal_exec = %q (error %t)", text, isError)
	}
	if screen := screenText(state, 0); !strings.Contains(screen, "echo from-mcp") {
		t.Fatalf("the user cannot see what the external agent ran:\n%s", screen)
	}
}

func TestObserverModeRefusesCommandsFromAnExternalAgent(t *testing.T) {
	state, _, address, token := hubFixture(t, "observe")
	out := rpc(t, state, address, token, "tools/call", `{"name":"terminal_exec","arguments":{"command":"echo nope"}}`)
	text, isError := toolText(t, out)
	if !isError || !strings.Contains(text, "read-only") {
		t.Fatalf("observer ran a command: %q (error %t)", text, isError)
	}
	if strings.Contains(state.sessions[0].term.screen.Text(), "echo nope") {
		t.Fatal("the refused command reached the terminal")
	}
	out = rpc(t, state, address, token, "tools/call", `{"name":"terminal_read","arguments":{}}`)
	if _, isError := toolText(t, out); isError {
		t.Fatalf("observer could not read: %v", out)
	}
}

func TestAMediumRiskCommandFromAnExternalAgentWaitsForTheUserInThePanel(t *testing.T) {
	state, _, address, token := hubFixture(t, "ask")
	done := make(chan map[string]any, 1)
	go func() {
		done <- rpc2(address, token, `{"name":"terminal_exec","arguments":{"command":"touch made-by-agent"}}`)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for len(state.ai.approvals) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no question reached the window")
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
	if !state.ai.open {
		t.Fatal("the question is not visible: the panel stayed closed")
	}
	if strings.Contains(state.sessions[0].term.screen.Text(), "touch made-by-agent") {
		t.Fatal("the command ran before the user answered")
	}
	state.aiAnswerApproval(false)
	for {
		select {
		case out := <-done:
			text, isError := toolText(t, out)
			if !isError || !strings.Contains(text, "declined") {
				t.Fatalf("declined call returned %q (error %t)", text, isError)
			}
			return
		default:
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

// rpc2 is rpc without the pumping, for a call the test answers from outside.
func rpc2(address, token, params string) map[string]any {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`
	request, _ := http.NewRequest(http.MethodPost, address, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(response.Body).Decode(&out)
	return out
}

func TestTurningTheSettingOffStopsTheServerAndNoOtherWindowsTokenWorks(t *testing.T) {
	state, hub, address, token := hubFixture(t, "ask")
	db, _ := state.aiStore()
	if err := db.SetSetting("ai.mcp.enabled", "off"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Reconfigure(); err != nil {
		t.Fatal(err)
	}
	if hub.Address() != "" {
		t.Fatal("the server is still running")
	}
	client := http.Client{Timeout: time.Second}
	request, _ := http.NewRequest(http.MethodPost, address, strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+token)
	if response, err := client.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("the stopped server still answers")
	}
	// Turned on again, it keeps its token and port, so a client's saved
	// configuration stays valid.
	if err := db.SetSetting("ai.mcp.enabled", "on"); err != nil {
		t.Fatal(err)
	}
	if err := hub.Reconfigure(); err != nil {
		t.Fatal(err)
	}
	if hub.Address() != address {
		t.Fatalf("address changed from %s to %s", address, hub.Address())
	}
}

func TestWithoutADatabaseTheHubOpensNothing(t *testing.T) {
	hub := NewAIHub(filepath.Join(t.TempDir(), "ai.db"))
	if err := hub.Reconfigure(); err != nil || hub.Address() != "" {
		t.Fatalf("Reconfigure = %v, address %q", err, hub.Address())
	}
}

func TestTheSettingsPageShowsTheAddressAnExternalAgentCanDial(t *testing.T) {
	state, _, address, _ := hubFixture(t, "ask")
	for index := 0; index < 20 && state.aiMCPURL() == ""; index++ {
		state.Wake()
		time.Sleep(10 * time.Millisecond)
	}
	if got := state.aiMCPURL(); got != address {
		t.Fatalf("the settings page shows %q, the server is at %q", got, address)
	}
}
