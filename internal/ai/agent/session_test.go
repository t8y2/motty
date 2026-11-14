package agent

import (
	"context"
	"errors"
	"fmt"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"strings"
	"testing"
)

type nativeTestProvider struct {
	run func(context.Context, llm.Request, func(context.Context, llm.ToolCall) gate.Result, func(llm.Event)) error
}

func (*nativeTestProvider) Models(context.Context) ([]string, error) { return []string{"default"}, nil }
func (*nativeTestProvider) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	return nil, errors.New("native session must not use HTTP loop")
}
func (p *nativeTestProvider) RunSession(c context.Context, r llm.Request, call func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
	return p.run(c, r, call, emit)
}

func TestNativeSessionExecutesOnceRedactsAndPersistsCompletedCalls(t *testing.T) {
	b := &fakeBackend{out: "password=secret"}
	cfg := newCfg(&fakeProvider{}, b, true)
	cfg.RedactToolResults = true
	cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
		c := llm.ToolCall{ID: "one", Name: tools.NameTerminalExec, Arguments: `{"command":"ls"}`}
		res := invoke(ctx, c)
		if strings.Contains(res.Text, "secret") {
			t.Error("unredacted result")
		}
		emit(llm.Event{Kind: llm.EventToolCall, ToolCall: &c}) // diagnostic only; not an instruction to execute again.
		emit(text("finished"))
		emit(done(2, 3))
		return nil
	}}
	var events []Event
	msgs, err := Run(t.Context(), cfg, nil, "hello", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatal(err)
	}
	if len(b.execs) != 1 || len(msgs) != 4 || msgs[2].Role != llm.RoleTool || msgs[3].Text != "finished" {
		t.Fatalf("exec=%v msgs=%+v", b.execs, msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m.Text, "secret") {
			t.Fatal("secret in history")
		}
	}
	starts, ends := 0, 0
	for _, e := range events {
		if e.Kind == EventToolStart {
			starts++
		}
		if e.Kind == EventToolDone {
			ends++
		}
	}
	if starts != 1 || ends != 1 {
		t.Fatal(starts, ends)
	}
}

func TestNativeSessionFailureKeepsToolPairButNotFalseFinal(t *testing.T) {
	b := &fakeBackend{}
	cfg := newCfg(&fakeProvider{}, b, true)
	cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
		invoke(ctx, llm.ToolCall{Name: tools.NameTerminalExec, Arguments: `{"command":"ls"}`})
		emit(text("unverified"))
		emit(done(1, 1))
		return errors.New("exit 7")
	}}
	msgs, err := Run(t.Context(), cfg, nil, "hello", nil)
	if err == nil || len(msgs) != 3 || msgs[2].Role != llm.RoleTool {
		t.Fatal(msgs, err)
	}
}

func TestNativeSessionAskEscalationNeverExecutes(t *testing.T) {
	b := &fakeBackend{}
	cfg := newCfg(&fakeProvider{}, b, true)
	cfg.Mode = ModeAsk
	cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
		if len(r.Tools) != 1 || r.Tools[0].Name != tools.NameRequestAgentMode {
			t.Error(r.Tools)
		}
		denied := invoke(ctx, llm.ToolCall{Name: tools.NameTerminalExec, Arguments: `{"command":"touch no"}`})
		if !denied.IsError {
			t.Error("Ask allowed exec")
		}
		invoke(ctx, llm.ToolCall{Name: tools.NameRequestAgentMode, Arguments: `{"reason":"inspect"}`})
		return ctx.Err()
	}}
	escalation := 0
	msgs, err := Run(t.Context(), cfg, nil, "inspect", func(e Event) {
		if e.Kind == EventAgentModeRequest {
			escalation++
		}
	})
	if err != nil || len(b.execs) != 0 || escalation != 1 || len(msgs) != 1 {
		t.Fatal(msgs, err, b.execs, escalation)
	}
}

func TestNativeSessionBudgetAndNoTools(t *testing.T) {
	for _, noTools := range []bool{false, true} {
		t.Run(map[bool]string{false: "budget", true: "declined"}[noTools], func(t *testing.T) {
			b := &fakeBackend{}
			cfg := newCfg(&fakeProvider{}, b, true)
			cfg.MaxSteps = 1
			if noTools {
				cfg.Mode = ModeAsk
				cfg.NoTools = true
			}
			cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
				if noTools && len(r.Tools) != 0 {
					t.Error("tools after decline")
				}
				invoke(ctx, llm.ToolCall{Name: tools.NameTerminalExec, Arguments: `{"command":"ls"}`})
				res := invoke(ctx, llm.ToolCall{Name: tools.NameTerminalExec, Arguments: `{"command":"pwd"}`})
				if !res.IsError {
					t.Error("budget bypass")
				}
				emit(text("summary"))
				emit(done(0, 0))
				return nil
			}}
			_, err := Run(t.Context(), cfg, nil, "question", nil)
			want := 1
			if noTools {
				want = 0
			}
			if err != nil || len(b.execs) != want {
				t.Fatal(err, b.execs)
			}
		})
	}
}

func TestNativeSessionRequiresTerminalEvent(t *testing.T) {
	cfg := Config{Provider: &nativeTestProvider{run: func(ctx context.Context, r llm.Request, call func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
		emit(text("partial"))
		return nil
	}}}
	msgs, err := Run(t.Context(), cfg, nil, "hello", nil)
	if err == nil || len(msgs) != 1 {
		t.Fatal(msgs, err)
	}
}

type nativeNoteBackend struct {
	fakeBackend
	edits int
	query tools.NoteQueryArgs
}

func (b *nativeNoteBackend) QueryNote(_ context.Context, a tools.NoteQueryArgs) (string, error) {
	b.query = a
	return `{"items":[]}`, nil
}
func (b *nativeNoteBackend) EditNote(context.Context, tools.NoteEditArgs) (string, error) {
	b.edits++
	return `{"status":"saved"}`, nil
}

func TestNativeSessionRoutesInAppNoteToolsThroughGate(t *testing.T) {
	b := &nativeNoteBackend{}
	cfg := Config{Mode: ModeAgent, Tools: tools.InApp(), Gate: gate.Gate{Backend: b, NotesEnabled: true}}
	cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
		for _, call := range []llm.ToolCall{
			{ID: "query", Name: tools.NameNoteQuery, Arguments: `{"action":"search","references":[{"kind":"host","id":"host_id","title":"alias-2","token":"note:host:alias-2"}]}`},
			{ID: "edit", Name: tools.NameNoteEdit, Arguments: `{"action":"create","title":"runbook","body":"steps"}`},
		} {
			if res := invoke(ctx, call); res.IsError {
				t.Errorf("native note call failed: %+v", res)
			}
		}
		emit(text("saved"))
		emit(done(1, 1))
		return nil
	}}
	messages, err := Run(t.Context(), cfg, nil, "save a note", nil)
	if err != nil || b.edits != 1 {
		t.Fatalf("native path failed: %v edits=%d", err, b.edits)
	}
	if len(b.query.References) != 1 || b.query.References[0].ID != "host_id" {
		t.Fatalf("native reference lost: %+v", b.query)
	}
	results := 0
	for _, m := range messages {
		if m.Role == llm.RoleTool {
			results++
		}
	}
	if results != 2 {
		t.Fatalf("native tool results not persisted: %+v", messages)
	}
}

func TestNativeAskCanQueryNotesButCannotEdit(t *testing.T) {
	for _, noTools := range []bool{false, true} {
		t.Run(fmt.Sprint("noTools=", noTools), func(t *testing.T) {
			b := &nativeNoteBackend{}
			cfg := Config{Mode: ModeAsk, NoTools: noTools, Tools: tools.InApp(), Gate: gate.Gate{Backend: b, NotesEnabled: true}}
			cfg.Provider = &nativeTestProvider{run: func(ctx context.Context, r llm.Request, invoke func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
				for _, s := range r.Tools {
					if s.Name != tools.NameNoteQuery && s.Name != tools.NameRequestAgentMode {
						t.Errorf("unexpected Ask tool: %s", s.Name)
					}
				}
				if noTools {
					if len(r.Tools) != 0 {
						t.Error("declined run exposed tools")
					}
				} else {
					if len(r.Tools) != 2 {
						t.Errorf("Ask tools: %+v", r.Tools)
					}
					res := invoke(ctx, llm.ToolCall{Name: tools.NameNoteQuery, Arguments: `{"action":"search"}`})
					if res.IsError {
						t.Errorf("query failed: %+v", res)
					}
				}
				res := invoke(ctx, llm.ToolCall{Name: tools.NameNoteEdit, Arguments: `{"action":"create","title":"n","body":"b"}`})
				if !res.IsError {
					t.Error("Ask edited a note")
				}
				emit(text("answer"))
				emit(done(1, 1))
				return nil
			}}
			messages, err := Run(t.Context(), cfg, nil, "read notes", nil)
			if err != nil || b.edits != 0 {
				t.Fatalf("Ask failed: %+v %v", messages, err)
			}
			if !noTools && b.query.Action != "search" {
				t.Fatal("native query did not reach backend")
			}
		})
	}
}
