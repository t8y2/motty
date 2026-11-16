package workbench

import (
	"errors"
	"strings"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/capture"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/shellkind"
)

// kindShell is a shell that says which family it is, as real shells do.
type kindShell struct {
	*fakeShell
	kind shellkind.Kind
}

func (shell kindShell) Kind() shellkind.Kind { return shell.kind }

func TestTerminalExecTypesWhatTheShellUnderstands(t *testing.T) {
	tests := []struct {
		kind   shellkind.Kind
		prefix string
	}{
		{shellkind.POSIX, "{ hostname\n}; printf"},
		{shellkind.Unknown, "{ hostname\n}; printf"},
		{shellkind.PowerShell, "$global:LASTEXITCODE = 0;"},
	}
	for _, tc := range tests {
		state, shell := assistantTestSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }})
		term := state.sessions[0].term
		term.shell = kindShell{shell, tc.kind}
		exec, err := state.beginExec(tools.TerminalExecArgs{Command: "hostname"})
		if err != nil || exec == nil {
			t.Fatalf("%v: beginExec = %v, %v", tc.kind, exec, err)
		}
		if typed := shell.typed(); !strings.HasPrefix(typed, tc.prefix) {
			t.Errorf("%v: typed %q, want it to start with %q", tc.kind, typed, tc.prefix)
		}
	}
}

func TestTerminalExecRefusesAShellItCannotWrapWithoutTypingAnything(t *testing.T) {
	for _, kind := range []shellkind.Kind{shellkind.Cmd, shellkind.Fish, shellkind.Nushell} {
		state, shell := assistantTestSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }})
		term := state.sessions[0].term
		term.shell = kindShell{shell, kind}
		_, err := state.beginExec(tools.TerminalExecArgs{Command: "hostname"})
		var unsupported capture.ErrUnsupportedShell
		if !errors.As(err, &unsupported) || unsupported.Kind != kind {
			t.Fatalf("%v: beginExec error = %v, want ErrUnsupportedShell", kind, err)
		}
		if shell.typed() != "" {
			t.Errorf("%v: the command was typed anyway: %q", kind, shell.typed())
		}
		if term.exec != nil {
			t.Errorf("%v: a refused command left an execution waiting", kind)
		}
	}
}

// A Telnet or serial device console has no shell wrapper and no exit status. The
// command must go in exactly as a person would type it, and the prompt coming
// back must end the capture without leaking the prompt into the output.
func TestTerminalExecSendsAConsoleCommandVerbatimAndWaitsForItsPrompt(t *testing.T) {
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"hostname"}`)
		}
		return textTurn("collected")
	}}
	state, shell := assistantTestSession(t, model)
	term := state.sessions[0].term
	shell.say("Switch#")
	state.Wake()
	term.shell = kindShell{shell, shellkind.Console}
	shell.onWrite = func(p []byte) {
		command := strings.TrimRight(string(p), "\r\n")
		shell.say("Switch#" + command + "\r\nVersion 1.0\r\nSwitch#")
	}

	ask(state, "collect the device information", agent.ModeAgent)
	pumpUntil(t, state, "the console command to finish", func() bool { return state.ai.run == nil })

	if typed := shell.typed(); typed != "hostname\r" {
		t.Fatalf("typed %q; want the console command and Enter exactly", typed)
	}
	last := model.requests[len(model.requests)-1]
	result := last.Messages[len(last.Messages)-1]
	if result.Role != llm.RoleTool || !strings.Contains(result.Text, "Version 1.0") ||
		!strings.Contains(result.Text, "exit status unknown") {
		t.Fatalf("tool result sent to the model = %+v", result)
	}
	if screen := screenText(state, 0); strings.Contains(screen, "GOPAI_") ||
		strings.Contains(screen, "printf") || strings.Contains(screen, "};") {
		t.Fatalf("a shell wrapper leaked into the device console:\n%s", screen)
	}
}

func TestConsoleExecRefusesToTypeWithoutAPromptToWaitFor(t *testing.T) {
	state, shell := assistantTestSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }})
	state.sessions[0].term.shell = kindShell{shell, shellkind.Console}
	_, err := state.beginExec(tools.TerminalExecArgs{Command: "hostname"})
	if !errors.Is(err, errNoConsolePrompt) {
		t.Fatalf("beginExec error = %v; want errNoConsolePrompt", err)
	}
	if typed := shell.typed(); typed != "" {
		t.Fatalf("typed %q into a terminal with no prompt", typed)
	}
}
