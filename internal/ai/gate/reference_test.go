package gate

import (
	"context"
	"strings"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

func TestReferencedCommandsFollowTheApprovalScopeAndNameTheirTarget(t *testing.T) {
	backend := &targetBackend{}
	primary := SessionInfo{ID: "1", Title: "A", Address: "a:22"}
	secondary := SessionInfo{ID: "2", Title: "B-2", Address: "b:22"}
	var asks []Request
	approved := true
	g := Gate{Backend: backend, Target: &primary, References: []SessionInfo{secondary},
		Policy: risk.Policy{Enabled: true, AutoUpTo: risk.High}, Mode: ModeAsk,
		Approver: func(_ context.Context, request Request) (bool, string, error) {
			asks = append(asks, request)
			return approved, "", nil
		}}
	// The scope, not the connection, decides what runs on its own: low and
	// medium work on a reference runs without a question under a high scope.
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"hostname"}`)); result.IsError || backend.session != primary.ID || len(asks) != 0 {
		t.Fatalf("omitted target did not resolve to A: %+v, session=%q, asks=%d", result, backend.session, len(asks))
	}
	backend.session = ""
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"hostname"}`)); result.IsError || backend.session != secondary.ID || len(asks) != 0 {
		t.Fatalf("low command on B under a high scope: %+v, session=%q, asks=%d", result, backend.session, len(asks))
	}
	backend.session = ""
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"mkdir /tmp/x"}`)); result.IsError || backend.session != secondary.ID || len(asks) != 0 {
		t.Fatalf("medium command on B under a high scope: %+v, session=%q, asks=%d", result, backend.session, len(asks))
	}
	// A scope that does not cover the risk asks, and the question names B.
	g.Policy.AutoUpTo = risk.Low
	approved = false
	backend.session = ""
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"mkdir /tmp/x"}`)); !result.Denied || backend.session != "" || len(asks) != 1 {
		t.Fatalf("medium command on B under a low scope: %+v, session=%q, asks=%d", result, backend.session, len(asks))
	}
	if asks[0].Session != secondary.ID || asks[0].SessionLabel != "B-2 · b:22" {
		t.Fatalf("approval used default instead of actual target: %+v", asks[0])
	}
	// Critical is never covered by the scope, and still needs the typed word.
	approved, g.Policy.AutoUpTo = true, risk.High
	backend.session = ""
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"rm -rf /"}`)); !result.IsError || !asks[1].NeedsText || backend.session != "" {
		t.Fatalf("critical command on B: %+v, needsText=%v, session=%q", result, asks[len(asks)-1].NeedsText, backend.session)
	}
	// A connection this question did not mention is still refused unasked.
	g.References = nil
	before := len(asks)
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"hostname"}`)); !result.IsError || len(asks) != before || backend.session != "" {
		t.Fatal("an unmentioned connection was reachable")
	}
}

func TestReferencedFileChangesShowActualTarget(t *testing.T) {
	for _, call := range []struct{ tool, args string }{
		{tools.NameSFTPWrite, `{"session":"2","path":"/tmp/preview","content":"hello"}`},
		{tools.NameSFTPRemove, `{"session":"2","path":"/tmp/preview"}`},
	} {
		t.Run(call.tool, func(t *testing.T) {
			backend := &targetBackend{}
			seen := false
			g := Gate{Backend: backend, Target: &SessionInfo{ID: "1"}, References: []SessionInfo{{ID: "2", Title: "B", Address: "b:22"}},
				Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low},
				Approver: func(_ context.Context, request Request) (bool, string, error) {
					seen = request.Session == "2" && request.SessionLabel == "B · b:22"
					return false, "", nil
				}}
			result := g.Run(context.Background(), call.tool, []byte(call.args))
			if !seen || !result.Denied || backend.writes.Load()+backend.removes.Load() != 0 {
				t.Fatalf("file operation bypassed actual-target approval: %+v", result)
			}
		})
	}
}

func TestReferencedWriteRunsUnderAHighScopeWithoutAsking(t *testing.T) {
	backend := &targetBackend{}
	g := Gate{Backend: backend, Target: &SessionInfo{ID: "1"}, References: []SessionInfo{{ID: "2", Title: "B", Address: "b:22"}},
		Policy: risk.Policy{Enabled: true, AutoUpTo: risk.High},
		Approver: func(context.Context, Request) (bool, string, error) {
			t.Fatal("a bounded change on B asked under a high scope")
			return false, "", nil
		}}
	if result := g.Run(context.Background(), tools.NameSFTPWrite, []byte(`{"session":"2","path":"/tmp/preview","content":"hello"}`)); result.IsError || backend.writes.Load() != 1 {
		t.Fatalf("referenced write under a high scope: %+v", result)
	}
}

func TestExplicitReferenceWithoutADefaultNeverFallsBack(t *testing.T) {
	backend := &targetBackend{}
	g := Gate{Backend: backend, Target: &SessionInfo{}, References: []SessionInfo{{ID: "2", Title: "B"}},
		Policy: risk.Policy{Enabled: true}, Approver: func(context.Context, Request) (bool, string, error) { return true, "", nil }}
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"hostname"}`)); !result.IsError || backend.session != "" {
		t.Fatal("missing default silently used a reference")
	}
	if result := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"hostname"}`)); result.IsError || backend.session != "2" {
		t.Fatalf("explicit reference required an unrelated default: %+v", result)
	}
}

type referenceFleet struct {
	fleetBackend
	disconnected bool
}

func (b *referenceFleet) Sessions(ctx context.Context) ([]SessionInfo, error) {
	sessions, err := b.fleetBackend.Sessions(ctx)
	for index := range sessions {
		if b.disconnected && sessions[index].ID == "w1:2" {
			sessions[index].Ready = false
		}
	}
	return sessions, err
}

func TestReferenceBatchNamesAllTargetsAndRechecksApprovalLifetimes(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "reconnected while approving"}[disconnected], func(t *testing.T) {
			backend := &referenceFleet{}
			asks := 0
			g := Gate{Backend: backend, Target: &SessionInfo{ID: "w1:1", Title: "A", Address: "a:22"},
				References: []SessionInfo{{ID: "w1:2", Title: "B", Address: "b:22"}}, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low},
				Approver: func(_ context.Context, request Request) (bool, string, error) {
					asks++
					if !strings.Contains(request.SessionLabel, "A · a:22") || !strings.Contains(request.SessionLabel, "B · b:22") {
						t.Fatalf("batch targets = %+v", request)
					}
					backend.disconnected = disconnected
					return true, "", nil
				}}
			result := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:2"],"command":"hostname"}`))
			if asks != 1 {
				t.Fatalf("asks=%d", asks)
			}
			if disconnected {
				if !result.IsError || len(backend.ran) != 0 {
					t.Fatalf("stale batch partially executed: %+v", result)
				}
			} else if result.IsError || len(backend.ran) != 2 {
				t.Fatalf("batch did not run after approval: %+v", result)
			}
		})
	}
}
