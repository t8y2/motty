package gate

import (
	"context"
	"strings"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

type targetBackend struct {
	fakeBackend
	session string
}

func (b *targetBackend) Exec(_ context.Context, args tools.TerminalExecArgs) (ExecResult, error) {
	b.session = args.Session
	return ExecResult{}, nil
}

func TestTargetResolvedBeforeApproval(t *testing.T) {
	backend := &targetBackend{}
	target := SessionInfo{ID: "1", Title: "Production", Address: "prod:22"}
	approved := false
	g := Gate{Backend: backend, Target: &target, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low},
		Approver: func(_ context.Context, request Request) (bool, string, error) {
			if request.Session != "1" || request.SessionLabel != "Production · prod:22" {
				t.Fatalf("approval lacks locked identity: %+v", request)
			}
			approved = true
			return true, ConfirmWord, nil
		}}
	result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"sudo systemctl restart nginx"}`))
	if result.IsError || !approved || backend.session != "1" {
		t.Fatalf("execution did not use the approved identity: %+v, session=%q", result, backend.session)
	}
}

func TestTargetRejectsCrossSessionToolsBeforeApproval(t *testing.T) {
	backend := &targetBackend{}
	g := Gate{Backend: backend, Target: &SessionInfo{ID: "1"}, Policy: risk.Policy{Enabled: true},
		Approver: func(context.Context, Request) (bool, string, error) {
			t.Fatal("out-of-scope operation reached approval")
			return false, "", nil
		}}
	for _, call := range []struct{ name, args string }{
		{tools.NameTerminalRead, `{"session":"2"}`},
		{tools.NameTerminalExec, `{"session":"2","command":"hostname"}`},
		{tools.NameTerminalExecMany, `{"sessions":["1","2"],"command":"hostname"}`},
		{tools.NameSFTPList, `{"session":"2","path":"/"}`},
		{tools.NameSFTPRead, `{"session":"2","path":"/etc/hosts"}`},
		{tools.NameSFTPWrite, `{"session":"2","path":"/tmp/test","content":"test"}`},
		{tools.NameSFTPRemove, `{"session":"2","path":"/tmp/test"}`},
	} {
		t.Run(call.name, func(t *testing.T) {
			result := g.Run(context.Background(), call.name, []byte(call.args))
			if !result.IsError || !strings.Contains(result.Text, "locked target") {
				t.Fatalf("cross-session call was not rejected: %+v", result)
			}
		})
	}
	if backend.session != "" || backend.reads.Load()+backend.writes.Load()+backend.removes.Load() != 0 {
		t.Fatal("backend was reached")
	}
}

func TestNoTargetCannotFallBackToActiveSession(t *testing.T) {
	backend := &targetBackend{}
	g := Gate{Backend: backend, Target: &SessionInfo{}}
	for _, args := range []string{`{"command":"hostname"}`, `{"session":"1","command":"hostname"}`} {
		if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(args)); !result.IsError {
			t.Fatal("no target allowed execution")
		}
	}
	if backend.session != "" {
		t.Fatal("backend was reached")
	}
	if result := g.Run(context.Background(), tools.NameListSessions, []byte(`{}`)); result.Text != "no sessions are open" {
		t.Fatalf("leaked sessions: %+v", result)
	}
	// External MCP callers that do not opt into a target retain their contract.
	g.Target = nil
	if result := g.Run(context.Background(), tools.NameListSessions, []byte(`{}`)); !strings.Contains(result.Text, "web") {
		t.Fatalf("external MCP changed: %+v", result)
	}
}
