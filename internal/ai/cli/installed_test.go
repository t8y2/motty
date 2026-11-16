package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
)

type installedFixtureBackend struct {
	gate.Backend
	calls atomic.Int32
}

func (b *installedFixtureBackend) Sessions(context.Context) ([]gate.SessionInfo, error) {
	b.calls.Add(1)
	return []gate.SessionInfo{{ID: "fixture-session", Ready: true}}, nil
}

func TestForeignCLIConfigSentinel(t *testing.T) {
	if len(os.Args) > 2 && os.Args[len(os.Args)-2] == "--foreign-sentinel" {
		_ = os.WriteFile(os.Args[len(os.Args)-1], []byte("unsafe configuration executed"), 0600)
		os.Exit(0)
	}
}

// Opt-in uses installed CLI binaries but an isolated home and a fake loopback
// model endpoint: no real account, credential, login, or inference is used.
func TestInstalledCLIIsolation(t *testing.T) {
	if os.Getenv("MOTTY_TEST_INSTALLED_CLI") != "1" {
		t.Skip("opt-in installed CLI compatibility test")
	}
	for _, kind := range []string{"codex-cli", "claude-code-cli"} {
		t.Run(kind, func(t *testing.T) {
			path, err := exec.LookPath(adapters[kind].program)
			if err != nil {
				t.Skip("CLI not installed")
			}
			home := t.TempDir()
			marker := filepath.Join(home, "foreign-executed")
			testExe, _ := os.Executable()
			foreignArgs := []string{"-test.run=^TestForeignCLIConfigSentinel$", "--", "--foreign-sentinel", marker}
			hookCommand := strconv.Quote(testExe) + " -test.run=^TestForeignCLIConfigSentinel$ -- --foreign-sentinel " + strconv.Quote(marker)
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("ANTHROPIC_API_KEY", "")
			t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-only")
			t.Setenv("NO_PROXY", "127.0.0.1,localhost")
			t.Setenv("CLAUDECODE", "")
			var mu sync.Mutex
			var names []string
			var violation string
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					http.Error(w, "not found", 404)
					return
				}
				var req struct {
					Tools []struct {
						Type, Name string
						Tools      []struct{ Type, Name string } `json:"tools"`
					} `json:"tools"`
					Model string `json:"model"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "bad fixture request", 400)
					return
				}
				mu.Lock()
				requests++
				first := requests == 1
				callName, callNamespace := "", ""
				for _, tool := range req.Tools {
					if tool.Type == "namespace" && strings.HasPrefix(tool.Name, "mcp__motty_") {
						for _, fn := range tool.Tools {
							if fn.Type != "function" || fn.Name != "list_sessions" {
								violation += "; unexpected scoped function: " + fn.Name
							} else {
								names = append(names, tool.Name+"__"+fn.Name)
								callName, callNamespace = fn.Name, tool.Name
							}
						}
						continue
					}
					if tool.Type == "function" || kind == "claude-code-cli" {
						// Codex's protocol metadata helpers can address only its
						// configured MCP servers. Our sole server exposes no resources.
						if kind == "codex-cli" && (tool.Name == "list_mcp_resources" || tool.Name == "list_mcp_resource_templates" || tool.Name == "read_mcp_resource") {
							continue
						}
						names = append(names, tool.Name)
						if strings.HasPrefix(tool.Name, "mcp__motty_") {
							callName = tool.Name
						}
						if !strings.HasPrefix(tool.Name, "mcp__motty_") || !strings.HasSuffix(tool.Name, "__list_sessions") {
							violation += "; unexpected native tool: " + tool.Name
						}
					} else {
						violation += "; unexpected tool kind: " + tool.Type
					}
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if kind == "claude-code-cli" {
					fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"fixture\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
					if first && callName != "" {
						block := map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "fixture_call", "name": callName, "input": map[string]any{}}}
						raw, _ := json.Marshal(block)
						fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", raw)
						fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
						return
					}
					fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"fixture ok\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					item := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "fixture ok", "annotations": []any{}}}}
					if first && callName != "" {
						item = map[string]any{"id": "fc_fixture", "type": "function_call", "call_id": "fixture_call", "name": callName, "arguments": "{}"}
						if callNamespace != "" {
							item["namespace"] = callNamespace
						}
					}
					emit := func(name string, data any) {
						raw, _ := json.Marshal(data)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
					}
					emit("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_fixture", "status": "in_progress", "output": []any{}}})
					emit("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
					if !first || callName == "" {
						emit("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": "fixture ok"})
					}
					emit("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
					emit("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_fixture", "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 2, "total_tokens": 3}}})
				}
			}))
			defer server.Close()
			if kind == "codex-cli" {
				os.MkdirAll(filepath.Join(home, ".codex"), 0700)
				config := fmt.Sprintf("model_provider = 'fixture'\nmodel = 'fixture'\nnotify = %s\n[features]\ncodex_hooks = true\ngoals = true\nview_image = true\n[hooks]\nSessionStart = [{hooks=[{type='command',command=%s}]}]\n[mcp_servers.foreign]\ncommand = %s\nargs = %s\n[model_providers.fixture]\nname = 'Fixture'\nbase_url = '%s/v1'\nwire_api = 'responses'\nrequires_openai_auth = false\n", jsonString(append([]string{testExe}, foreignArgs...)), strconv.Quote(hookCommand), strconv.Quote(testExe), jsonString(foreignArgs), server.URL)
				if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("ANTHROPIC_BASE_URL", server.URL)
				os.MkdirAll(filepath.Join(home, ".claude"), 0700)
				settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": hookCommand}}}}}})
				if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), settings, 0600); err != nil {
					t.Fatal(err)
				}
				mcps, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"foreign": map[string]any{"command": testExe, "args": foreignArgs}}})
				if err := os.WriteFile(filepath.Join(home, ".claude.json"), mcps, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			spec, _ := tools.Lookup(tools.NameListSessions)
			backend := &installedFixtureBackend{}
			cfg := agent.Config{Provider: New(kind, path), Model: "fixture", Mode: agent.ModeAgent, Tools: []tools.Spec{spec}, Gate: gate.Gate{Backend: backend}}
			msgs, err := agent.Run(ctx, cfg, nil, "List sessions once, then say fixture ok.", nil)
			mu.Lock()
			defer mu.Unlock()
			if violation != "" {
				t.Error(violation)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("inherited MCP/hook/notification executed outside Motty Gate")
			}
			if err != nil {
				t.Fatal("installed CLI fixture failed", err)
			}
			if len(names) == 0 {
				t.Error("scoped MCP tool never reached the model")
			}
			if backend.calls.Load() != 1 || len(msgs) != 4 {
				t.Errorf("MCP callback/audit not exactly once: calls=%d messages=%d", backend.calls.Load(), len(msgs))
			}
			if len(msgs) < 2 || !strings.Contains(msgs[len(msgs)-1].Text, "fixture ok") {
				t.Fatal("wrong final text", msgs)
			}
		})
	}
}
