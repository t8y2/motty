package cli

import (
	"encoding/json"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// piUserDir writes a user agent directory and points Pi at it, so the command
// builder never reads the developer's real login or settings.
func piUserDir(t *testing.T, settings string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"anthropic":{"type":"api_key","key":"private"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	return dir
}

func TestPiCommandKeepsOnlyTheScopedMCPTools(t *testing.T) {
	piUserDir(t, `{"defaultModel":"claude-opus-5-5","defaultProvider":"anthropic","defaultTools":["bash"],"defaultProjectTrust":"always"}`)
	dir := t.TempDir()
	token := "PRIVATE_TOKEN_123456789012345678901234"
	req := llm.Request{Model: "default", Tools: []llm.ToolSpec{{Name: "list_sessions"}, {Name: "terminal_exec"}}}
	cmd, err := buildCommand(store.KindPiCLI, command{program: "pi"}, req, dir, "http://127.0.0.1:1/mcp", token, nil, "motty_run")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.args, " ")
	for _, want := range []string{"--print", "--mode json", "--no-extensions", "--extension builtin:mcp",
		"--no-builtin-tools", "--no-skills", "--no-prompt-templates", "--no-context-files", "--no-approve", "--no-session"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %v", want, cmd.args)
		}
	}
	if strings.Contains(args, token) {
		t.Fatal("token in argv")
	}
	agent := piAgentPath(dir)
	wantEnv := "PI_CODING_AGENT_DIR=" + agent
	if !strings.Contains(strings.Join(cmd.env, "\n"), wantEnv) {
		t.Fatalf("agent directory not redirected: %v", cmd.env)
	}

	raw, err := os.ReadFile(filepath.Join(agent, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		AutoEnableCodemode bool `json:"autoEnableCodemode"`
		Servers            map[string]struct {
			URL      string            `json:"url"`
			Exposure string            `json:"exposure"`
			Timeout  int               `json:"timeout"`
			Headers  map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	entry, ok := config.Servers["motty_run"]
	// One server only: a server inherited from the user's mcp.json would expose
	// tools the approval gate never sees.
	if !ok || len(config.Servers) != 1 || config.AutoEnableCodemode {
		t.Fatalf("private MCP configuration = %+v", config)
	}
	if entry.URL != "http://127.0.0.1:1/mcp" || entry.Exposure != "direct" || entry.Timeout != 900 ||
		entry.Headers["Authorization"] != "Bearer "+token {
		t.Fatalf("server entry = %+v", entry)
	}

	raw, err = os.ReadFile(filepath.Join(agent, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if string(settings["defaultModel"]) != `"claude-opus-5-5"` {
		t.Fatalf("model choice not carried over: %s", raw)
	}
	if _, ok := settings["defaultTools"]; ok {
		t.Fatal("the user's tool selection was copied into the run")
	}
	if _, ok := settings["defaultProjectTrust"]; ok {
		t.Fatal("the user's project trust was copied into the run")
	}

	auth, err := os.ReadFile(filepath.Join(agent, "auth.json"))
	if err != nil || !strings.Contains(string(auth), "private") {
		t.Fatal("the login was not shared with the private directory", err)
	}
}

func TestPiMessageEndAddsTextOnlyWhenNothingStreamed(t *testing.T) {
	p := newParser("pi-cli")
	var text string
	for _, line := range []string{
		`{"type":"session","version":3,"id":"s"}`,
		// A user message carries its text as a string, not as content blocks.
		`{"type":"message_start","message":{"role":"user","content":"hello"}}`,
		`{"type":"message_end","message":{"role":"user","content":"hello"}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[]}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"only snapshot"}]}}`,
		`{"type":"agent_settled","aborted":false}`,
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
	if text != "only snapshot" {
		t.Fatalf("snapshot text = %q", text)
	}
	if !p.done {
		t.Fatal("agent_settled did not finish the run")
	}
}

// TestPiMCPHelper stands in for `pi mcp list --json` so the listing contract is
// checked without an installed CLI.
func TestPiMCPHelper(t *testing.T) {
	if os.Getenv("MOTTY_TEST_PI_MCP") != "yes" {
		return
	}
	tools := strings.Split(os.Getenv("MOTTY_TEST_PI_TOOLS"), ",")
	if len(tools) == 1 && tools[0] == "" {
		tools = nil
	}
	raw, err := json.Marshal(map[string]any{"servers": []map[string]any{{
		"name":  os.Getenv("MOTTY_TEST_PI_SERVER"),
		"state": os.Getenv("MOTTY_TEST_PI_STATE"),
		"tools": tools,
	}}})
	if err != nil {
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(raw)
	os.Exit(0)
}

func TestPiMCPVerificationRequiresScopedTools(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := command{program: exe, args: []string{"-test.run=^TestPiMCPHelper$"}, env: []string{"MOTTY_TEST_PI_MCP=yes", "MOTTY_TEST_PI_SERVER=motty_run"}}
	run := func(tools, state string) error {
		c := base
		c.env = append(append([]string{}, base.env...), "MOTTY_TEST_PI_TOOLS="+tools, "MOTTY_TEST_PI_STATE="+state)
		return verifyPiMCP(t.Context(), c, "motty_run", []string{"list_sessions", "terminal_exec"})
	}
	if err := run("terminal_exec,list_sessions", "connected"); err != nil {
		t.Fatal(err)
	}
	if err := run("terminal_exec", "connected"); err == nil {
		t.Fatal("a missing tool was accepted")
	}
	if err := run("terminal_exec,list_sessions,other", "connected"); err == nil {
		t.Fatal("a foreign tool was accepted")
	}
	if err := run("terminal_exec,list_sessions", "failed"); err == nil {
		t.Fatal("an unconnected server was accepted")
	}
}

func TestPiStreamErrorIsClassified(t *testing.T) {
	if _, err := newParser("pi-cli").line([]byte(`{"type":"message_update","assistantMessageEvent":{"type":"error","error":"Not logged in. Please log in."}}`)); err == nil || err.Error() != "cli: login_required" {
		t.Fatal(err)
	}
}
