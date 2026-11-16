package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCLIHelper(t *testing.T) {
	if os.Getenv("MOTTY_TEST_CLI") != "yes" {
		return
	}
	args := os.Args
	mode := args[len(args)-1]
	switch mode {
	case "success":
		fmt.Println(`{"item":{"type":"agent_message","text":"hello"},"type":"item.completed"}`)
		fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":3,"output_tokens":2}}`)
	case "failure":
		fmt.Println(`{"type":"turn.completed"}`)
		os.Exit(7)
	case "missing":
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"unfinished"}}`)
	case "sleep":
		time.Sleep(time.Minute)
	case "oversize":
		fmt.Println(strings.Repeat("x", (1<<20)+1))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("PRIVATE_TOKEN ", 20000))
		fmt.Println(`{"type":"turn.completed"}`)
	}
	os.Exit(0)
}
func helperCommand(t *testing.T, mode string) command {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return command{program: exe, args: []string{"-test.run=^TestCLIHelper$", "--", mode}, env: []string{"MOTTY_TEST_CLI=yes"}, dir: t.TempDir()}
}
func TestRuntimeRejectsMissingCompletionNonzeroAndOversize(t *testing.T) {
	for _, mode := range []string{"failure", "missing", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			p := newParser("codex-cli")
			err := streamProcess(t.Context(), helperCommand(t, mode), p, func(llm.Event) {})
			if err == nil {
				t.Fatal("false success", mode)
			}
		})
	}
}
func TestRuntimeEmitsOneDoneAfterSuccessfulExit(t *testing.T) {
	for _, mode := range []string{"success", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			p := newParser("codex-cli")
			var events []llm.Event
			err := streamProcess(t.Context(), helperCommand(t, mode), p, func(e llm.Event) { events = append(events, e) })
			if err != nil {
				t.Fatal(err)
			}
			done := 0
			for _, e := range events {
				if e.Kind == llm.EventDone {
					done++
				}
				if strings.Contains(e.Text, "PRIVATE_TOKEN") {
					t.Fatal("stderr exposed")
				}
			}
			if done != 1 || events[len(events)-1].Kind != llm.EventDone {
				t.Fatal(events)
			}
		})
	}
}
func TestRuntimeCancellationReapsPromptly(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := streamProcess(ctx, helperCommand(t, "sleep"), newParser("codex-cli"), func(llm.Event) {})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 4*time.Second {
		t.Fatal(err, time.Since(start))
	}
}
func TestParsersDeduplicateFinalSnapshotsAndClassifyErrors(t *testing.T) {
	cases := []struct {
		kind  string
		lines []string
		want  string
	}{
		{"claude-code-cli", []string{`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}`, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`, `{"type":"result","subtype":"success","result":"hello","usage":{"input_tokens":3,"output_tokens":2}}`}, "hello"},
		{"codex-cli", []string{`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}`, `{"type":"turn.completed","usage":{"input_tokens":3,"output_tokens":2}}`}, "hello"},
		{"opencode-cli", []string{`{"type":"text","part":{"text":"hello"}}`, `{"type":"step_finish","part":{"reason":"stop","tokens":{"input":3,"output":2}}}`}, "hello"},
		{"qoder-cli", []string{`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}`, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`, `{"type":"result","subtype":"success","result":"hello","usage":{"input_tokens":3,"output_tokens":2}}`}, "hello"},
		{"pi-cli", []string{
			`{"type":"session","version":3,"id":"s"}`,
			`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
			`{"type":"message_update","usage":{"input":3,"output":1},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hel"}}`,
			`{"type":"message_update","usage":{"input":3,"output":2},"assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"lo"}}`,
			`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]},"usage":{"input":3,"output":2}}`,
			`{"type":"agent_settled","aborted":false}`,
		}, "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			p := newParser(tc.kind)
			var text string
			for _, line := range tc.lines {
				es, err := p.line([]byte(line))
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range es {
					if e.Kind == llm.EventText {
						text += e.Text
					}
				}
			}
			if text != tc.want || !p.done || p.usage.InputTokens != 3 || p.usage.OutputTokens != 2 {
				t.Fatal(text, p)
			}
			if _, err := newParser(tc.kind).line([]byte(`{"type":"error","message":"PRIVATE_TOKEN"}`)); err == nil || strings.Contains(err.Error(), "PRIVATE_TOKEN") {
				t.Fatal("raw/ignored error", err)
			}
		})
	}
}
func TestResolveDoesNotInterpretShellCommands(t *testing.T) {
	for _, path := range []string{"MOTTY_X=1 codex", "codex --dangerous", "/missing/path with spaces/codex"} {
		if _, err := resolve("codex-cli", path); err == nil {
			t.Fatal("interpreted command", path)
		}
	}
	if _, err := resolve("unknown", ""); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestClaudeInformationalEventHasItsOwnFieldTypes(t *testing.T) {
	p := newParser("claude-code-cli")
	if events, err := p.line([]byte(`{"type":"system","subtype":"safe_mode","event":"initialization"}`)); err != nil || len(events) != 0 {
		t.Fatal("informational event rejected", err)
	}
}
func TestBuildersNeverPutTokenInArguments(t *testing.T) {
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli", "pi-cli", "qoder-cli"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
			dir := t.TempDir()
			req := llm.Request{Model: "default", Tools: []llm.ToolSpec{{Name: "list_sessions", Schema: []byte(`{"type":"object"}`)}}}
			cmd, err := buildCommand(kind, command{program: "agent"}, req, dir, "http://127.0.0.1:1234/mcp", "PRIVATE_TOKEN_123456789012345678901234", []string{"foreign.server"}, "motty_session")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(cmd.args, " "), "PRIVATE_TOKEN") {
				t.Fatal("token in argv")
			}
			if cmd.dir != dir {
				t.Fatal("unisolated cwd")
			}
		})
	}
}

func TestExplicitLoginErrorIsNotAnArbitraryForbiddenError(t *testing.T) {
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli"} {
		_, err := newParser(kind).line([]byte(`{"type":"error","message":"Not logged in. Please log in."}`))
		if err == nil || err.Error() != "cli: login_required" {
			t.Fatal(kind, err)
		}
		_, err = newParser(kind).line([]byte(`{"type":"error","message":"HTTP 403 model access forbidden"}`))
		if err == nil || err.Error() != "cli: provider_failed" {
			t.Fatal("misclassified provider access failure", kind, err)
		}
	}
}

func TestScopedSpecsDoNotAdvertiseExecutionAsReadOnly(t *testing.T) {
	specs := sessionTools([]llm.ToolSpec{{Name: "terminal_exec"}, {Name: "sftp_write"}, {Name: "list_sessions"}})
	if len(specs) != 3 || specs[0].Kind != tools.Exec || specs[1].Kind != tools.Write || specs[2].Kind != tools.Read {
		t.Fatal("unsafe MCP annotations", specs)
	}
}

func TestOpenCodeCapabilityProbeRequiresAppliedPermission(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"string deny", `{"permission":"deny"}`, true},
		{"map wildcard deny", `{"permission":{"*":"deny"}}`, true},
		{"string allow", `{"permission":"allow"}`, false},
		{"map wildcard allow", `{"permission":{"*":"allow"}}`, false},
		{"missing", `{}`, false},
		{"invalid", `not json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := openCodeConfigDenied([]byte(tc.raw)); got != tc.want {
				t.Fatalf("openCodeConfigDenied = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestOpenCodeV2BuilderUsesPrivateServerAndNativeConfig(t *testing.T) {
	dir := t.TempDir()
	req := llm.Request{Model: "default", Tools: []llm.ToolSpec{{Name: "list_sessions"}}}
	cmd, err := buildCommand("opencode-cli", command{program: "agent", flavor: openCodeV2}, req, dir,
		"http://127.0.0.1:1234/mcp", "PRIVATE_TOKEN_123456789012345678901234", []string{"foreign.server"}, "motty_session")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.args, " ")
	for _, want := range []string{"--standalone", "run", "--format json", "--agent motty_session", "--title motty", "--thinking"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "--pure") || strings.Contains(args, "PRIVATE_TOKEN") {
		t.Fatalf("V1 or credential argument leaked into V2 run: %q", args)
	}
	var rawConfig string
	for _, env := range cmd.env {
		if value, ok := strings.CutPrefix(env, "OPENCODE_CONFIG_CONTENT="); ok {
			rawConfig = value
		}
	}
	if rawConfig == "" {
		t.Fatal("inline V2 config missing")
	}
	var config struct {
		Permissions []openCodePermissionRule `json:"permissions"`
		Agents      map[string]struct {
			Permissions []openCodePermissionRule `json:"permissions"`
		} `json:"agents"`
		MCP struct {
			Servers map[string]struct {
				Disabled bool              `json:"disabled"`
				OAuth    *bool             `json:"oauth"`
				Codemode *bool             `json:"codemode"`
				Headers  map[string]string `json:"headers"`
			} `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
		t.Fatal(err)
	}
	agent, ok := config.Agents["motty_session"]
	if !ok {
		t.Fatal("scoped agent missing")
	}
	if effect := openCodePermissionEffect(config.Permissions, "read", "*"); effect != "deny" {
		t.Errorf("global read effect = %q", effect)
	}
	if effect := openCodePermissionEffect(agent.Permissions, "read", "*"); effect != "deny" {
		t.Errorf("agent read effect = %q", effect)
	}
	if effect := openCodePermissionEffect(agent.Permissions, "motty_session_list_sessions", "*"); effect != "allow" {
		t.Errorf("scoped tool effect = %q", effect)
	}
	server, ok := config.MCP.Servers["motty_session"]
	if !ok || server.OAuth == nil || *server.OAuth || server.Codemode == nil || *server.Codemode {
		t.Fatalf("scoped MCP server = %+v", server)
	}
	if server.Headers["Authorization"] != "Bearer PRIVATE_TOKEN_123456789012345678901234" {
		t.Fatalf("MCP authorization missing: %+v", server.Headers)
	}
	foreign, ok := config.MCP.Servers["foreign.server"]
	if !ok || !foreign.Disabled || len(foreign.Headers) != 0 {
		t.Fatalf("foreign server not inert: %+v", foreign)
	}
	for _, want := range []string{"OPENCODE_DB=:memory:", "OPENCODE_DISABLE_PROJECT_CONFIG=1"} {
		if !strings.Contains(strings.Join(cmd.env, " "), want) {
			t.Errorf("missing V2 isolation env %q", want)
		}
	}
	if strings.Contains(strings.Join(cmd.env, " "), "OPENCODE_PURE") {
		t.Error("removed V2 variable still set")
	}
}

func TestQoderBuilderScopesMCPAndDisablesPluginsAndHooks(t *testing.T) {
	dir := t.TempDir()
	req := llm.Request{Model: "default", Tools: []llm.ToolSpec{{Name: "list_sessions"}}}
	cmd, err := buildCommand("qoder-cli", command{program: "agent"}, req, dir,
		"http://127.0.0.1:1234/mcp", "PRIVATE_TOKEN_123456789012345678901234", []string{"acme@market"}, "motty_session")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.args, " ")
	for _, want := range []string{"--print", "--output-format stream-json", "--permission-mode dont_ask",
		"--strict-mcp-config", "--setting-sources user", "--tools", "--allowed-tools mcp__motty_session__list_sessions"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "PRIVATE_TOKEN") {
		t.Fatalf("token in arguments: %q", args)
	}
	var settingsPath, mcpPath string
	for index, arg := range cmd.args {
		switch arg {
		case "--settings":
			settingsPath = cmd.args[index+1]
		case "--mcp-config":
			mcpPath = cmd.args[index+1]
		}
	}
	if settingsPath == "" || mcpPath == "" {
		t.Fatalf("private config paths missing: %q", cmd.args)
	}
	var settings struct {
		DisableAllHooks bool            `json:"disableAllHooks"`
		EnabledPlugins  map[string]bool `json:"enabledPlugins"`
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil || json.Unmarshal(raw, &settings) != nil {
		t.Fatalf("settings = %s: %v", raw, err)
	}
	if !settings.DisableAllHooks || settings.EnabledPlugins["acme@market"] {
		t.Fatalf("isolating settings = %+v", settings)
	}
	var mcp struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	raw, err = os.ReadFile(mcpPath)
	if err != nil || json.Unmarshal(raw, &mcp) != nil {
		t.Fatalf("mcp config = %s: %v", raw, err)
	}
	server, ok := mcp.Servers["motty_session"]
	if !ok || server.Type != "http" || server.URL != "http://127.0.0.1:1234/mcp" ||
		server.Headers["Authorization"] != "Bearer PRIVATE_TOKEN_123456789012345678901234" {
		t.Fatalf("scoped MCP server = %+v", server)
	}
}

func TestQoderPluginIDsRejectUnlistableEntries(t *testing.T) {
	if ids, ok := qoderPluginIDs([]byte(`[{"id":"one@market"},{"id":"two@market"}]`)); !ok || strings.Join(ids, ",") != "one@market,two@market" {
		t.Fatalf("plugin IDs = %v, %t", ids, ok)
	}
	for _, raw := range []string{`not json`, `[{"name":"one@market"}]`, `{}`} {
		if _, ok := qoderPluginIDs([]byte(raw)); ok {
			t.Fatalf("unlistable plugin accepted: %s", raw)
		}
	}
}

func TestOpenCodeV2EventStreamUsesTheSameParserContract(t *testing.T) {
	p := newParser("opencode-cli")
	var text string
	for _, line := range []string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"motty_session_echo"}}`,
		`{"type":"step_finish","part":{"reason":"tool-calls","tokens":{"input":3,"output":1}}}`,
		`{"type":"text","part":{"type":"text","text":"hello"}}`,
		`{"type":"text","part":{"type":"text","text":" world"}}`,
		`{"type":"step_finish","part":{"reason":"stop","tokens":{"input":4,"output":2}}}`,
	} {
		events, err := p.line([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Kind == llm.EventText {
				text += event.Text
			}
		}
	}
	if text != "hello world" || !p.done || p.usage.InputTokens != 7 || p.usage.OutputTokens != 3 {
		t.Fatalf("V2 stream = %q done=%t usage=%+v", text, p.done, p.usage)
	}
}
