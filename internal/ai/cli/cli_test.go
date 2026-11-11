package cli

import (
	"context"
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
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli", "pi-cli"} {
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
