package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in check for the OpenCode 2 profile. It runs an installed V2 binary
// against a loopback model endpoint, calls the scoped MCP tool exactly once,
// and proves that a configured foreign MCP server is never contacted.
func TestInstalledOpenCodeV2Isolation(t *testing.T) {
	exe := os.Getenv("MOTTY_TEST_OPENCODE_V2")
	if exe == "" {
		t.Skip("set MOTTY_TEST_OPENCODE_V2 to an OpenCode 2 binary")
	}

	var mu sync.Mutex
	var tools []string
	var toolCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		hasResult := false
		for _, message := range req.Messages {
			if message.Role == "tool" {
				hasResult = true
			}
		}
		var callName string
		mu.Lock()
		tools = tools[:0]
		for _, tool := range req.Tools {
			tools = append(tools, tool.Function.Name)
			if strings.HasSuffix(tool.Function.Name, "_list_sessions") {
				callName = tool.Function.Name
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(delta map[string]any, finish any) {
			raw, _ := json.Marshal(map[string]any{"id": "x", "object": "chat.completion.chunk", "created": 1, "model": "fixture",
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		if !hasResult && callName != "" {
			toolCalls.Add(1)
			chunk(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": callName, "arguments": "{}"}}}}, "tool_calls")
		} else {
			chunk(map[string]any{"role": "assistant", "content": "fixture ok"}, nil)
			chunk(map[string]any{}, "stop")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()

	var foreignHits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignHits.Add(1)
		var req struct {
			ID *json.RawMessage `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"foreign","version":"1"}}}`))
	}))
	defer foreign.Close()

	home := t.TempDir()
	config := map[string]any{
		"model": "fixture/test",
		"providers": map[string]any{"fixture": map[string]any{
			"name": "Fixture", "env": []string{"FIXTURE_KEY"},
			"package":  "@opencode/ai/providers/openai-compatible",
			"settings": map[string]any{"baseURL": model.URL + "/v1"},
			"models":   map[string]any{"test": map[string]any{}},
		}},
		"mcp": map[string]any{"servers": map[string]any{"foreign": map[string]any{
			"type": "remote", "url": foreign.URL + "/mcp", "oauth": false,
		}}},
	}
	raw, _ := json.Marshal(config)
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "opencode", "opencode.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("FIXTURE_KEY", "x")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	p := New("opencode-cli", exe).(*provider)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	var text string
	var done bool
	err := p.RunSession(ctx, llm.Request{
		Model:    "fixture/test",
		System:   "Use the provided tool once.",
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "list sessions"}},
		Tools:    []llm.ToolSpec{{Name: "list_sessions", Description: "list sessions"}},
	}, func(ctx context.Context, call llm.ToolCall) gate.Result {
		return gate.Result{Text: "fixture-session"}
	}, func(event llm.Event) {
		if event.Kind == llm.EventText {
			text += event.Text
		}
		if event.Kind == llm.EventDone {
			done = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "fixture ok") || !done {
		t.Fatalf("text=%q done=%t", text, done)
	}
	if toolCalls.Load() != 1 {
		t.Fatalf("scoped tool calls = %d", toolCalls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, name := range tools {
		if !strings.HasSuffix(name, "_list_sessions") && name != "skill" {
			t.Errorf("unexpected tool exposed to the model: %s", name)
		}
	}
	if foreignHits.Load() != 0 {
		t.Errorf("disabled foreign MCP server was contacted %d times", foreignHits.Load())
	}
}
