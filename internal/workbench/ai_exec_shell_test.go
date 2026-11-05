package workbench

import (
	"errors"
	"strings"
	"testing"

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
