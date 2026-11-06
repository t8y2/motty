package agent

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// turn scripts one model response.
type turn struct {
	events []llm.Event
	block  bool  // after events, wait for ctx cancellation
	err    error // Stream itself fails
}

func text(s string) llm.Event { return llm.Event{Kind: llm.EventText, Text: s} }
func done(in, out int) llm.Event {
	return llm.Event{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: in, OutputTokens: out}}
}
func call(id, name, args string) llm.Event {
	return llm.Event{Kind: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: id, Name: name, Arguments: args}}
}

type fakeProvider struct {
	mu    sync.Mutex
	turns []turn
	reqs  []llm.Request
}

func (p *fakeProvider) Models(context.Context) ([]string, error) { return nil, nil }
func (p *fakeProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	req.Messages = append([]llm.Message(nil), req.Messages...)
	p.reqs = append(p.reqs, req)
	if len(p.turns) == 0 {
		p.mu.Unlock()
		return nil, errors.New("script exhausted")
	}
	t := p.turns[0]
	p.turns = p.turns[1:]
	p.mu.Unlock()
	if t.err != nil {
		return nil, t.err
	}
	ch := make(chan llm.Event)
	go func() {
		defer close(ch)
		for _, ev := range t.events {
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
		if t.block {
			<-ctx.Done()
		}
	}()
	return ch, nil
}

type fakeBackend struct {
	mu       sync.Mutex
	execs    []string
	out      string
	blockCtx bool
}

func (b *fakeBackend) Sessions(context.Context) ([]gate.SessionInfo, error) {
	return []gate.SessionInfo{{ID: "1", Title: "web", Ready: true}}, nil
}
func (b *fakeBackend) ReadTerminal(context.Context, tools.TerminalReadArgs) (string, error) {
	return "screen", nil
}
func (b *fakeBackend) Exec(ctx context.Context, a tools.TerminalExecArgs) (gate.ExecResult, error) {
	b.mu.Lock()
	b.execs = append(b.execs, a.Command)
	b.mu.Unlock()
	if b.blockCtx {
		<-ctx.Done()
		return gate.ExecResult{}, ctx.Err()
	}
	out := b.out
	if out == "" {
		out = "out:" + a.Command
	}
	return gate.ExecResult{Output: out}, nil
}
func (b *fakeBackend) JobStart(context.Context, tools.TerminalStartArgs) (gate.JobStatus, error) {
	return gate.JobStatus{}, errors.New("not used")
}
func (b *fakeBackend) JobPoll(context.Context, tools.TerminalPollArgs) (gate.JobStatus, error) {
	return gate.JobStatus{}, errors.New("not used")
}
func (b *fakeBackend) JobStop(context.Context, tools.TerminalStopArgs) (gate.JobStatus, error) {
	return gate.JobStatus{}, errors.New("not used")
}
func (b *fakeBackend) ListDir(context.Context, tools.SFTPListArgs) ([]gate.FileEntry, error) {
	return nil, nil
}
func (b *fakeBackend) ReadFile(context.Context, tools.SFTPReadArgs) (string, bool, error) {
	return "", false, nil
}
func (b *fakeBackend) WriteFile(context.Context, tools.SFTPWriteArgs) error { return nil }
func (b *fakeBackend) Remove(context.Context, tools.SFTPRemoveArgs) error   { return nil }

func newCfg(p *fakeProvider, b *fakeBackend, approve bool) Config {
	return Config{
		Provider: p, Model: "m", System: "sys", Mode: ModeAgent,
		Gate: gate.Gate{Backend: b, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low},
			Approver: func(context.Context, gate.Request) (bool, string, error) { return approve, "", nil }},
	}
}

func collect() (*[]Event, func(Event)) {
	var evs []Event
	return &evs, func(e Event) { evs = append(evs, e) }
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

const lsArgs = `{"command":"ls"}`

func TestPlainAnswer(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("he"), text("llo"), done(3, 2)}}}}
	evs, emit := collect()
	hist := []llm.Message{{Role: llm.RoleUser, Text: "old"}, {Role: llm.RoleAssistant, Text: "older"}}
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), hist, "hi", emit)
	if err != nil {
		t.Fatal(err)
	}
	want := []llm.Message{{Role: llm.RoleUser, Text: "hi"}, {Role: llm.RoleAssistant, Text: "hello"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if len(hist) != 2 {
		t.Fatal("history mutated")
	}
	last := (*evs)[len(*evs)-1]
	if last.Kind != EventDone || last.Usage != (llm.Usage{InputTokens: 3, OutputTokens: 2}) {
		t.Fatalf("last %+v", last)
	}
	if !reflect.DeepEqual(kinds(*evs), []EventKind{EventText, EventText, EventDone}) {
		t.Fatalf("kinds %v", kinds(*evs))
	}
	if len(p.reqs[0].Messages) != 3 || len(p.reqs[0].Tools) == 0 {
		t.Fatalf("req %+v", p.reqs[0])
	}
}

func TestToolEventsCarryReportedUsageBeforeTheNextModelTurn(t *testing.T) {
	provider := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("tool", tools.NameTerminalExec, lsArgs), done(40, 6)}},
		{events: []llm.Event{text("finished"), done(20, 4)}},
	}}
	events, emit := collect()
	_, err := Run(context.Background(), newCfg(provider, &fakeBackend{}, true), nil, "inspect", emit)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range *events {
		if event.Kind != EventToolStart && event.Kind != EventToolDone {
			continue
		}
		count++
		if event.Usage != (llm.Usage{InputTokens: 40, OutputTokens: 6}) {
			t.Fatalf("tool event %v lost reported usage: %+v", event.Kind, event.Usage)
		}
	}
	if count != 2 {
		t.Fatalf("tool events = %d, want 2", count)
	}
}

func TestToolThenAnswerShape(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{{Kind: llm.EventReasoning, Text: "think"}, text("checking"), call("c1", tools.NameTerminalExec, lsArgs), done(10, 1)}},
		{events: []llm.Event{text("all good"), done(20, 4)}},
	}}
	evs, emit := collect()
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "go", emit)
	if err != nil {
		t.Fatal(err)
	}
	calls := []llm.ToolCall{{ID: "c1", Name: tools.NameTerminalExec, Arguments: lsArgs}}
	if len(got) != 4 || got[0].Role != llm.RoleUser || got[1].Role != llm.RoleAssistant ||
		!reflect.DeepEqual(got[1].ToolCalls, calls) || got[1].Text != "checking" ||
		got[2].Role != llm.RoleTool || got[2].ToolCallID != "c1" || !strings.Contains(got[2].Text, "out:ls") ||
		got[3].Role != llm.RoleAssistant || got[3].Text != "all good" || len(got[3].ToolCalls) != 0 {
		t.Fatalf("shape %+v", got)
	}
	for _, m := range got {
		if strings.Contains(m.Text, "think") {
			t.Fatal("reasoning leaked into history")
		}
	}
	if !reflect.DeepEqual(kinds(*evs), []EventKind{EventReasoning, EventText, EventToolStart, EventToolDone, EventText, EventDone}) {
		t.Fatalf("kinds %v", kinds(*evs))
	}
	if (*evs)[2].Step != 1 || (*evs)[4].Step != 2 {
		t.Fatalf("steps %+v", *evs)
	}
	if u := (*evs)[5].Usage; u.InputTokens != 30 || u.OutputTokens != 5 {
		t.Fatalf("usage %+v", u)
	}
	// The second request carries the tool exchange.
	if n := len(p.reqs[1].Messages); n != 3 {
		t.Fatalf("second request has %d messages", n)
	}
}

func TestParallelCallsRunInOrder(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("a", tools.NameTerminalExec, `{"command":"ls one"}`), call("b", tools.NameTerminalExec, `{"command":"ls two"}`), call("c", tools.NameTerminalExec, `{"command":"ls three"}`), done(0, 0)}},
		{events: []llm.Event{text("ok"), done(0, 0)}},
	}}
	b := &fakeBackend{}
	got, err := Run(context.Background(), newCfg(p, b, true), nil, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.execs, []string{"ls one", "ls two", "ls three"}) {
		t.Fatalf("execs %v", b.execs)
	}
	if got[2].ToolCallID != "a" || got[3].ToolCallID != "b" || got[4].ToolCallID != "c" {
		t.Fatalf("result order %+v", got)
	}
}

func TestDeniedThenAdapts(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c1", tools.NameTerminalExec, `{"command":"rm -rf /tmp/x"}`), done(0, 0)}},
		{events: []llm.Event{text("understood"), done(0, 0)}},
	}}
	b := &fakeBackend{}
	evs, emit := collect()
	got, err := Run(context.Background(), newCfg(p, b, false), nil, "x", emit)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.execs) != 0 {
		t.Fatal("denied command ran")
	}
	var d Event
	for _, e := range *evs {
		if e.Kind == EventToolDone {
			d = e
		}
	}
	if !d.Result.Denied || !d.Result.IsError {
		t.Fatalf("done %+v", d)
	}
	if got[2].Role != llm.RoleTool || !strings.HasPrefix(got[2].Text, "Error: ") || got[3].Text != "understood" {
		t.Fatalf("msgs %+v", got)
	}
}

func TestUnknownTool(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("", "nope", ""), call("", "", "{}"), done(0, 0)}},
		{events: []llm.Event{text("sorry"), done(0, 0)}},
	}}
	evs, emit := collect()
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "x", emit)
	if err != nil {
		t.Fatal(err)
	}
	if got[1].ToolCalls[0].ID != "call_1" || got[1].ToolCalls[1].ID != "call_2" || got[1].ToolCalls[1].Name == "" || got[1].ToolCalls[0].Arguments != "{}" {
		t.Fatalf("calls %+v", got[1].ToolCalls)
	}
	if got[2].ToolCallID != "call_1" || !strings.Contains(got[2].Text, "unknown tool nope") || got[3].ToolCallID != "call_2" {
		t.Fatalf("results %+v", got)
	}
	starts := 0
	for _, e := range *evs {
		if e.Kind == EventToolStart {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("starts %d", starts)
	}
}

func TestToolNotOfferedIsUnknown(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}},
		{events: []llm.Event{text("k"), done(0, 0)}},
	}}
	b := &fakeBackend{}
	cfg := newCfg(p, b, true)
	cfg.Tools = []tools.Spec{mustSpec(t, tools.NameListSessions)}
	got, err := Run(context.Background(), cfg, nil, "x", nil)
	if err != nil || len(b.execs) != 0 || !strings.Contains(got[2].Text, "unknown tool") {
		t.Fatalf("%v %v %+v", err, b.execs, got)
	}
	if len(p.reqs[0].Tools) != 1 {
		t.Fatalf("offered %d", len(p.reqs[0].Tools))
	}
}

func mustSpec(t *testing.T, n string) tools.Spec {
	s, ok := tools.Lookup(n)
	if !ok {
		t.Fatal(n)
	}
	return s
}

func TestStepLimitSummarises(t *testing.T) {
	var turns []turn
	for i := 0; i < 2; i++ {
		turns = append(turns, turn{events: []llm.Event{call("", tools.NameTerminalExec, `{"command":"ls `+string(rune('a'+i))+`"}`), done(1, 1)}})
	}
	turns = append(turns, turn{events: []llm.Event{text("summary"), call("zz", tools.NameTerminalExec, lsArgs), done(1, 1)}})
	p := &fakeProvider{turns: turns}
	b := &fakeBackend{}
	cfg := newCfg(p, b, true)
	cfg.MaxSteps = 2
	evs, emit := collect()
	got, err := Run(context.Background(), cfg, nil, "x", emit)
	if err != nil {
		t.Fatal(err)
	}
	last := got[len(got)-1]
	if last.Role != llm.RoleAssistant || last.Text != "summary" || len(last.ToolCalls) != 0 {
		t.Fatalf("last %+v", last)
	}
	if len(p.reqs) != 3 || len(p.reqs[2].Tools) != 0 || !strings.Contains(p.reqs[2].System, "step limit") || strings.Contains(p.reqs[0].System, "step limit") {
		t.Fatalf("reqs %+v", p.reqs)
	}
	if len(b.execs) != 2 {
		t.Fatalf("execs %v", b.execs)
	}
	if e := (*evs)[len(*evs)-1]; e.Kind != EventDone || e.Usage.InputTokens != 3 {
		t.Fatalf("last event %+v", e)
	}
}

func TestStuckRepeatStops(t *testing.T) {
	var turns []turn
	for i := 0; i < 6; i++ {
		turns = append(turns, turn{events: []llm.Event{call("", tools.NameTerminalExec, `{"command":"rm -rf /tmp/x"}`), done(0, 0)}})
	}
	p := &fakeProvider{turns: turns}
	evs, emit := collect()
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, false), nil, "x", emit)
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("err %v", err)
	}
	if len(p.reqs) != 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	if e := (*evs)[len(*evs)-1]; e.Kind != EventError {
		t.Fatalf("last %+v", e)
	}
	// History stays valid: every assistant call has its result.
	if len(got) != 1+3*2 || got[len(got)-1].Role != llm.RoleTool {
		t.Fatalf("got %d msgs", len(got))
	}
}

func TestDifferentFailuresDoNotTrip(t *testing.T) {
	var turns []turn
	for i := 0; i < 4; i++ {
		turns = append(turns, turn{events: []llm.Event{call("", tools.NameTerminalExec, `{"command":"rm -rf /tmp/`+string(rune('a'+i))+`"}`), done(0, 0)}})
	}
	turns = append(turns, turn{events: []llm.Event{text("end"), done(0, 0)}})
	p := &fakeProvider{turns: turns}
	if _, err := Run(context.Background(), newCfg(p, &fakeBackend{}, false), nil, "x", nil); err != nil {
		t.Fatal(err)
	}
}

func settle(t *testing.T, before int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: %d > %d", runtime.NumGoroutine(), before)
}

func TestCancelMidStream(t *testing.T) {
	before := runtime.NumGoroutine()
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("par")}, block: true}}}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var evs []Event
	emit := func(e Event) {
		mu.Lock()
		evs = append(evs, e)
		mu.Unlock()
		cancel()
	}
	start := time.Now()
	got, err := Run(ctx, newCfg(p, &fakeBackend{}, true), nil, "x", emit)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != EventText {
		t.Fatalf("events %+v", evs)
	}
	if len(got) != 1 || got[0].Role != llm.RoleUser {
		t.Fatalf("got %+v", got)
	}
	settle(t, before)
}

func TestCancelMidTool(t *testing.T) {
	before := runtime.NumGoroutine()
	p := &fakeProvider{turns: []turn{{events: []llm.Event{call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}}}}
	b := &fakeBackend{blockCtx: true}
	ctx, cancel := context.WithCancel(context.Background())
	var evs []Event
	emit := func(e Event) {
		evs = append(evs, e)
		if e.Kind == EventToolStart {
			go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		}
	}
	got, err := Run(ctx, newCfg(p, b, true), nil, "x", emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if k := kinds(evs); !reflect.DeepEqual(k, []EventKind{EventToolStart}) {
		t.Fatalf("events %v", k)
	}
	if len(got) != 1 {
		t.Fatalf("incomplete round returned: %+v", got)
	}
	settle(t, before)
}

func TestToolTimeout(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}},
		{events: []llm.Event{text("ok"), done(0, 0)}},
	}}
	cfg := newCfg(p, &fakeBackend{blockCtx: true}, true)
	cfg.StepTimeout = 30 * time.Millisecond
	evs, emit := collect()
	got, err := Run(context.Background(), cfg, nil, "x", emit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[2].Text, "timed out") || !(*evs)[1].Result.IsError {
		t.Fatalf("%+v", got[2])
	}
}

func TestProviderErrors(t *testing.T) {
	boom := errors.New("boom")
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("a"), {Kind: llm.EventError, Err: boom}}}}}
	evs, emit := collect()
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "x", emit)
	if !errors.Is(err, boom) || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	if e := (*evs)[len(*evs)-1]; e.Kind != EventError || e.Text != "boom" {
		t.Fatalf("%+v", e)
	}
	p = &fakeProvider{turns: []turn{{err: boom}}}
	if _, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "x", nil); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	// Channel closed without a terminal event.
	p = &fakeProvider{turns: []turn{{events: []llm.Event{text("a")}}}}
	if _, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "x", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestAskModeOffersOnlyTheEscalationTool(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("try ls"), call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}}}}
	b := &fakeBackend{}
	cfg := newCfg(p, b, true)
	cfg.Mode = ModeAsk
	got, err := Run(context.Background(), cfg, nil, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs[0].Tools) != 1 || p.reqs[0].Tools[0].Name != tools.NameRequestAgentMode {
		t.Fatalf("Ask tools = %+v", p.reqs[0].Tools)
	}
	if len(b.execs) != 0 || len(got) != 2 {
		t.Fatalf("execs %v got %d", b.execs, len(got))
	}
	if !strings.HasSuffix(p.reqs[0].System, askNote) {
		t.Fatal("the Ask request must describe its one tool")
	}
}

func TestAskModeEscalatesWithoutRunningAnything(t *testing.T) {
	args := `{"reason":"listing files needs a terminal","command":"ls"}`
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("I can do that."), call("c", tools.NameRequestAgentMode, args), done(0, 0)}}}}
	b := &fakeBackend{}
	cfg := newCfg(p, b, true)
	cfg.Mode = ModeAsk
	var events []Event
	got, err := Run(context.Background(), cfg, nil, "list the files", func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if len(b.execs) != 0 || len(got) != 1 || got[0].Role != llm.RoleUser {
		t.Fatalf("execs %v messages %+v", b.execs, got)
	}
	var requested *Event
	for index := range events {
		if events[index].Kind == EventAgentModeRequest {
			requested = &events[index]
		}
		if events[index].Kind == EventToolStart || events[index].Kind == EventToolDone {
			t.Fatalf("escalation ran a tool: %+v", events)
		}
	}
	if requested == nil || requested.Call.Name != tools.NameRequestAgentMode || requested.Call.Arguments != args {
		t.Fatalf("events = %+v", events)
	}
}

func TestTruncateAndRedactResults(t *testing.T) {
	out := strings.Repeat("é", 4000) + "\npassword=hunter2\n" + strings.Repeat("ü", 4000)
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}},
		{events: []llm.Event{text("k"), done(0, 0)}},
	}}
	cfg := newCfg(p, &fakeBackend{out: out}, true)
	cfg.MaxToolResultBytes = 1001
	cfg.RedactToolResults = true
	got, err := Run(context.Background(), cfg, nil, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := got[2].Text
	if !strings.Contains(r, "bytes omitted") || len(r) > 1001+60 {
		t.Fatalf("len %d: %q", len(r), r)
	}
	if !isValidUTF8(r) {
		t.Fatal("invalid utf-8")
	}

	// Redaction without truncation, and off by default.
	for _, redact := range []bool{true, false} {
		p := &fakeProvider{turns: []turn{
			{events: []llm.Event{call("c", tools.NameTerminalExec, lsArgs), done(0, 0)}},
			{events: []llm.Event{text("k"), done(0, 0)}},
		}}
		cfg := newCfg(p, &fakeBackend{out: "db password=hunter2"}, true)
		cfg.RedactToolResults = redact
		got, _ := Run(context.Background(), cfg, nil, "x", nil)
		if has := strings.Contains(got[2].Text, "hunter2"); has == redact {
			t.Fatalf("redact=%v text %q", redact, got[2].Text)
		}
	}
}

func isValidUTF8(s string) bool { return strings.ToValidUTF8(s, "") == s }

func TestTruncateBoundaries(t *testing.T) {
	for _, max := range []int{3, 4, 5, 7, 10} {
		s := strings.Repeat("日本語", 20)
		r := truncate(s, max)
		if !isValidUTF8(r) {
			t.Fatalf("max %d: %q", max, r)
		}
	}
	if truncate("short", 10) != "short" {
		t.Fatal("changed")
	}
}

func TestRunCanContinueAHistoryThatAlreadyEndsWithTheUser(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("done"), done(0, 0)}}}}
	cfg := newCfg(p, &fakeBackend{}, true)
	cfg.UserInHistory = true
	history := []llm.Message{{Role: llm.RoleUser, Text: "run ls"}}
	got, err := Run(context.Background(), cfg, history, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs[0].Messages) != 1 || p.reqs[0].Messages[0].Text != "run ls" {
		t.Fatalf("request history = %+v", p.reqs[0].Messages)
	}
	if len(got) != 1 || got[0].Role != llm.RoleAssistant || got[0].Text != "done" {
		t.Fatalf("new messages = %+v", got)
	}
}

func TestAskModeAfterADeclineAnswersWithNoTools(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("The screen does not show an answer."), done(0, 0)}}}}
	cfg := newCfg(p, &fakeBackend{}, true)
	cfg.Mode = ModeAsk
	cfg.NoTools = true
	cfg.UserInHistory = true
	got, err := Run(context.Background(), cfg, []llm.Message{{Role: llm.RoleUser, Text: "run pwd"}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs[0].Tools) != 0 {
		t.Fatalf("declined Ask run offered tools: %+v", p.reqs[0].Tools)
	}
	if !strings.HasSuffix(p.reqs[0].System, noToolsAnswerNote) {
		t.Fatal("the declined run did not say why it has no tools")
	}
	if len(got) != 1 || got[0].Role != llm.RoleAssistant || got[0].Text != "The screen does not show an answer." {
		t.Fatalf("messages = %+v", got)
	}
}

func TestRunSumsCacheUsageAcrossTurns(t *testing.T) {
	lsArgs := `{"command":"ls"}`
	cached := func(in, read, write int) llm.Event {
		return llm.Event{Kind: llm.EventDone, Usage: llm.Usage{InputTokens: in, OutputTokens: 1, CacheReadTokens: read, CacheWriteTokens: write}}
	}
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c1", tools.NameTerminalExec, lsArgs), cached(3000, 0, 2900)}},
		{events: []llm.Event{text("done"), cached(3100, 2900, 100)}},
	}}
	evs, emit := collect()
	if _, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "go", emit); err != nil {
		t.Fatal(err)
	}
	want := llm.Usage{InputTokens: 6100, OutputTokens: 2, CacheReadTokens: 2900, CacheWriteTokens: 3000}
	if u := (*evs)[len(*evs)-1].Usage; u != want {
		t.Fatalf("usage = %+v, want %+v", u, want)
	}
}

// A turn with no prose and no tool calls used to end as if the model had
// answered; the panel showed an empty bubble and nothing else.
func TestEmptyAnswerIsAnError(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{done(3, 0)}}}}
	var last Event
	_, err := Run(context.Background(), Config{Provider: p, Model: "m", Mode: ModeAgent,
		Gate: gate.Gate{Policy: risk.Policy{Enabled: false}}}, nil, "hi", func(ev Event) { last = ev })
	if err == nil {
		t.Fatal("an empty answer passed as a finished turn")
	}
	if last.Kind != EventError {
		t.Fatalf("the caller was not told: %+v", last)
	}
}

// Ask mode offers only the escalation tool; a call to anything else cannot run,
// and with no prose beside it the turn has nothing to show.
func TestAskModeDroppedCallIsAnError(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{call("c1", "terminal_exec", `{"command":"uptime"}`), done(3, 0)}}}}
	_, err := Run(context.Background(), Config{Provider: p, Model: "m", Mode: ModeAsk,
		Gate: gate.Gate{Policy: risk.Policy{Enabled: false}}}, nil, "run uptime", nil)
	if err == nil || !strings.Contains(err.Error(), "terminal_exec") {
		t.Fatalf("err = %v", err)
	}
}

// The panel tells a cut-off answer apart from a finished one by this reason.
func TestDoneCarriesTheStopReason(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("half an answer"), {Kind: llm.EventDone, StopReason: "length"}}}}}
	var done Event
	_, err := Run(context.Background(), Config{Provider: p, Model: "m", Mode: ModeAgent,
		Gate: gate.Gate{Policy: risk.Policy{Enabled: false}}}, nil, "hi", func(ev Event) {
		if ev.Kind == EventDone {
			done = ev
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.StopReason != "length" {
		t.Fatalf("stop reason = %q", done.StopReason)
	}
}
