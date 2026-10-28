package gate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

type fakeBackend struct {
	execs, writes, removes, reads atomic.Int32
	lastCommand                   string
}

func (b *fakeBackend) Sessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{{ID: "1", Title: "web", Address: "web:22", Active: true, Ready: true}}, nil
}
func (b *fakeBackend) ReadTerminal(context.Context, tools.TerminalReadArgs) (string, error) {
	return "screen", nil
}
func (b *fakeBackend) Exec(_ context.Context, a tools.TerminalExecArgs) (ExecResult, error) {
	b.execs.Add(1)
	b.lastCommand = a.Command
	return ExecResult{Output: "out", ExitCode: 3}, nil
}
func (b *fakeBackend) ListDir(context.Context, tools.SFTPListArgs) ([]FileEntry, error) {
	return []FileEntry{{Name: "a", Size: 1}, {Name: "d", Dir: true}}, nil
}
func (b *fakeBackend) ReadFile(context.Context, tools.SFTPReadArgs) (string, bool, error) {
	b.reads.Add(1)
	return "data", false, nil
}
func (b *fakeBackend) WriteFile(context.Context, tools.SFTPWriteArgs) error {
	b.writes.Add(1)
	return nil
}
func (b *fakeBackend) Remove(context.Context, tools.SFTPRemoveArgs) error {
	b.removes.Add(1)
	return nil
}

func yes(typed string) Approver {
	return func(_ context.Context, r Request) (bool, string, error) { return true, typed, nil }
}

func TestLowRiskRunsWithoutAskingAndOthersAsk(t *testing.T) {
	backend := &fakeBackend{}
	asked := 0
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}, Approver: func(_ context.Context, r Request) (bool, string, error) {
		asked++
		return true, "", nil
	}}
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"ls -la"}`)); res.IsError || !strings.Contains(res.Text, "[exit status 3]") || asked != 0 {
		t.Fatalf("ls: %+v asked %d", res, asked)
	}
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"mkdir /tmp/x"}`)); res.IsError || asked != 1 || backend.execs.Load() != 2 {
		t.Fatalf("mkdir: %+v asked %d", res, asked)
	}
}

func TestDeclinedActionNeverReachesTheBackend(t *testing.T) {
	backend := &fakeBackend{}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Approver: func(context.Context, Request) (bool, string, error) { return false, "", nil }}
	for name, raw := range map[string]string{
		tools.NameTerminalExec: `{"command":"rm -rf build"}`,
		tools.NameSFTPWrite:    `{"path":"/etc/x","content":"c"}`,
		tools.NameSFTPRemove:   `{"path":"/tmp/x"}`,
	} {
		res := g.Run(context.Background(), name, []byte(raw))
		if !res.Denied || !res.IsError {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if backend.execs.Load()+backend.writes.Load()+backend.removes.Load() != 0 {
		t.Fatal("a declined action ran")
	}
}

func TestCriticalNeedsTheConfirmationWord(t *testing.T) {
	backend := &fakeBackend{}
	var seen Request
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.High}, Approver: func(_ context.Context, r Request) (bool, string, error) {
		seen = r
		return true, "nope", nil
	}}
	res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"rm -rf /"}`))
	if !seen.NeedsText || seen.Confirm != ConfirmWord || !res.Denied || backend.execs.Load() != 0 {
		t.Fatalf("wrong text accepted: %+v %+v", res, seen)
	}
	g.Approver = yes(ConfirmWord)
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"rm -rf /"}`)); res.IsError || backend.execs.Load() != 1 {
		t.Fatalf("right text refused: %+v", res)
	}
}

func TestObserverModeRefusesEverythingButReads(t *testing.T) {
	backend := &fakeBackend{}
	g := Gate{Backend: backend, Mode: ModeObserve, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.High}, Approver: yes("")}
	for _, call := range []struct{ name, raw string }{
		{tools.NameTerminalExec, `{"command":"ls"}`},
		{tools.NameSFTPWrite, `{"path":"/tmp/a","content":"x"}`},
		{tools.NameSFTPRemove, `{"path":"/tmp/a"}`},
	} {
		if res := g.Run(context.Background(), call.name, []byte(call.raw)); !res.Denied {
			t.Errorf("%s was not refused: %+v", call.name, res)
		}
	}
	if res := g.Run(context.Background(), tools.NameListSessions, nil); res.IsError || !strings.Contains(res.Text, "web") {
		t.Fatalf("list: %+v", res)
	}
	if res := g.Run(context.Background(), tools.NameTerminalRead, []byte(`{}`)); res.IsError {
		t.Fatalf("read: %+v", res)
	}
}

func TestWithoutAnApproverNothingRiskyRuns(t *testing.T) {
	backend := &fakeBackend{}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}}
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"touch x"}`)); !res.Denied || backend.execs.Load() != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestAllowAllRunsEveryRiskAndFileOperationWithoutApproval(t *testing.T) {
	backend := &fakeBackend{}
	g := Gate{Backend: backend, Mode: ModeAllowAll, Policy: risk.Policy{BlockCritical: true}, Approver: func(context.Context, Request) (bool, string, error) {
		t.Error("allow-all asked for approval")
		return false, "", nil
	}}
	for _, call := range []struct{ name, raw string }{
		{tools.NameTerminalExec, `{"command":"ls"}`},
		{tools.NameTerminalExec, `{"command":"touch /tmp/test"}`},
		{tools.NameTerminalExec, `{"command":"rm /tmp/test"}`},
		{tools.NameTerminalExec, `{"command":"rm -rf /"}`},
		{tools.NameSFTPRead, `{"path":"/etc/shadow"}`},
		{tools.NameSFTPWrite, `{"path":"/etc/test","content":"x","overwrite":true}`},
		{tools.NameSFTPRemove, `{"path":"/etc/test"}`},
	} {
		if res := g.Run(context.Background(), call.name, []byte(call.raw)); res.IsError || res.Denied {
			t.Errorf("%s %s: %+v", call.name, call.raw, res)
		}
	}
	if backend.execs.Load() != 4 || backend.reads.Load() != 1 || backend.writes.Load() != 1 || backend.removes.Load() != 1 {
		t.Fatal("allowed operations did not reach the fake backend")
	}
	// Opting out of approval does not opt out of argument validation.
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{}`)); !res.IsError || backend.execs.Load() != 4 {
		t.Fatalf("invalid command reached the backend: %+v", res)
	}
}

func TestBadArgumentsAndUnknownToolsAreErrorsNotPanics(t *testing.T) {
	g := Gate{Backend: &fakeBackend{}, Policy: risk.Policy{Enabled: true}}
	for _, call := range []struct{ name, raw string }{
		{"nope", `{}`},
		{tools.NameTerminalExec, `{}`},
		{tools.NameTerminalExec, `{"command":"ls","extra":1}`},
		{tools.NameSFTPRead, `not json`},
	} {
		if res := g.Run(context.Background(), call.name, []byte(call.raw)); !res.IsError {
			t.Errorf("%s %s: %+v", call.name, call.raw, res)
		}
	}
}

func TestSensitiveReadAsksAndPlainReadDoesNot(t *testing.T) {
	backend := &fakeBackend{}
	asked := 0
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Approver: func(context.Context, Request) (bool, string, error) { asked++; return false, "", nil }}
	if res := g.Run(context.Background(), tools.NameSFTPRead, []byte(`{"path":"/var/log/syslog"}`)); res.IsError || asked != 0 {
		t.Fatalf("plain: %+v", res)
	}
	if res := g.Run(context.Background(), tools.NameSFTPRead, []byte(`{"path":"/etc/shadow"}`)); !res.Denied || asked != 1 {
		t.Fatalf("shadow: %+v asked %d", res, asked)
	}
}

func TestApproverErrorDeniesAndContextEndsApproval(t *testing.T) {
	backend := &fakeBackend{}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Approver: func(context.Context, Request) (bool, string, error) {
		return false, "", errors.New("window closed")
	}}
	if res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"command":"touch x"}`)); !res.Denied || !strings.Contains(res.Text, "window closed") {
		t.Fatalf("%+v", res)
	}
}

// fleetBackend has several sessions; one of them has no shell and one fails.
type fleetBackend struct {
	fakeBackend
	mu    sync.Mutex
	ran   map[string]string
	notOK map[string]bool
}

func (b *fleetBackend) Sessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{
		{ID: "w1:1", Title: "web", Ready: true}, {ID: "w1:2", Title: "db", Ready: true},
		{ID: "w1:3", Title: "idle", Ready: false}, {ID: "w1:4", Title: "broken", Ready: true},
	}, nil
}

func (b *fleetBackend) Exec(_ context.Context, a tools.TerminalExecArgs) (ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ran == nil {
		b.ran = map[string]string{}
	}
	b.ran[a.Session] = a.Command
	if b.notOK[a.Session] {
		return ExecResult{}, errors.New("connection lost")
	}
	return ExecResult{Output: "output of " + a.Session}, nil
}

func TestABatchIsApprovedOnceWithEveryTargetNamedAndRunsOnEach(t *testing.T) {
	backend := &fleetBackend{}
	var requests []Request
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}, Approver: func(_ context.Context, r Request) (bool, string, error) {
		requests = append(requests, r)
		return true, "", nil
	}}
	// "uptime" alone is low risk and would run unasked on one host.
	res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:2"],"command":"uptime"}`))
	if res.IsError || len(requests) != 1 {
		t.Fatalf("%+v, asked %d times", res, len(requests))
	}
	if !strings.Contains(requests[0].Summary, "web") || !strings.Contains(requests[0].Summary, "db") || !strings.Contains(requests[0].Summary, "uptime") {
		t.Errorf("the question does not name the targets and the command: %q", requests[0].Summary)
	}
	if backend.ran["w1:1"] != "uptime" || backend.ran["w1:2"] != "uptime" {
		t.Errorf("ran %v", backend.ran)
	}
	for _, want := range []string{"== w1:1 (web) ==", "output of w1:1", "== w1:2 (db) ==", "output of w1:2"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the result lacks %q:\n%s", want, res.Text)
		}
	}
	if strings.Index(res.Text, "w1:1") > strings.Index(res.Text, "w1:2") {
		t.Error("the results are not in the order the sessions were given")
	}
}

func TestADeclinedBatchRunsNowhereAndABadTargetStopsItBeforeTheQuestion(t *testing.T) {
	backend := &fleetBackend{}
	asked := 0
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Approver: func(context.Context, Request) (bool, string, error) {
		asked++
		return false, "", nil
	}}
	if res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:2"],"command":"uptime"}`)); !res.Denied || len(backend.ran) != 0 {
		t.Fatalf("declined: %+v, ran %v", res, backend.ran)
	}
	asked = 0
	for name, sessions := range map[string]string{"no shell": `["w1:1","w1:3"]`, "unknown": `["w1:1","w9:9"]`} {
		res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":`+sessions+`,"command":"uptime"}`))
		if !res.IsError || asked != 0 || len(backend.ran) != 0 || !strings.Contains(res.Text, "nothing was run") {
			t.Errorf("%s: %+v asked %d ran %v", name, res, asked, backend.ran)
		}
	}
}

func TestOneHostFailingDoesNotHideTheOthersAndACriticalBatchNeedsTheWord(t *testing.T) {
	backend := &fleetBackend{notOK: map[string]bool{"w1:4": true}}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Approver: yes("")}
	res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:4"],"command":"uptime"}`))
	if res.IsError || !strings.Contains(res.Text, "output of w1:1") || !strings.Contains(res.Text, "[error: connection lost]") {
		t.Fatalf("a partial failure: %+v", res)
	}
	var request Request
	g.Approver = func(_ context.Context, r Request) (bool, string, error) { request = r; return false, "", nil }
	g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:2"],"command":"rm -rf /"}`))
	if !request.NeedsText || request.Confirm != ConfirmWord {
		t.Errorf("a destructive command on several hosts must need the confirmation word: %+v", request)
	}
	observer := Gate{Backend: backend, Mode: ModeObserve}
	if res := observer.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["w1:1","w1:2"],"command":"uptime"}`)); !res.Denied {
		t.Errorf("a read-only connection ran a batch: %+v", res)
	}
}
