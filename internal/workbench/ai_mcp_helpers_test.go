//go:build windows || darwin || linux

package workbench

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// hubFixtureWith lets a test give the shell its behaviour before the hub starts.
func hubFixtureWith(t *testing.T, mode string, configure func(*State, *fakeShell)) (*State, *AIHub, string, string) {
	t.Helper()
	state, shell := assistantTestSession(t, &scriptedModel{turn: nil})
	configure(state, shell)
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

func pagedRPC(t *testing.T, state *State, address, token, session, method string, params any) (map[string]any, string, int) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	type reply struct {
		data    []byte
		session string
		status  int
		err     error
	}
	done := make(chan reply, 1)
	go func() {
		request, err := http.NewRequest(http.MethodPost, address, bytes.NewReader(body))
		if err != nil {
			done <- reply{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		if session != "" {
			request.Header.Set("Mcp-Session-Id", session)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			done <- reply{err: err}
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		done <- reply{data: data, session: response.Header.Get("Mcp-Session-Id"), status: response.StatusCode, err: err}
	}()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatal(result.err)
			}
			var parsed map[string]any
			if result.status == http.StatusOK {
				if err := json.Unmarshal(result.data, &parsed); err != nil {
					t.Fatal(err)
				}
			}
			return parsed, result.session, result.status
		case <-tick.C:
			state.Wake()
		case <-deadline.C:
			t.Fatal("HTTP MCP command or page did not complete")
		}
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
