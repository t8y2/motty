package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
)

// steerQueue hands out the scripted interjections one batch per call, the way
// the workbench's queue does: each call takes what has been added so far.
type steerQueue struct {
	mu      sync.Mutex
	pending []string
}

func (q *steerQueue) add(text string) {
	q.mu.Lock()
	q.pending = append(q.pending, text)
	q.mu.Unlock()
}

func (q *steerQueue) take() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.pending
	q.pending = nil
	return out
}

// A steer added while a tool runs joins the conversation once the round's
// results are in, so the next request reads it right after them.
func TestSteerJoinsTheNextRequestAfterToolResults(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{call("c1", tools.NameTerminalExec, lsArgs), done(1, 1)}},
		{events: []llm.Event{text("checked"), done(1, 1)}},
	}}
	var queue steerQueue
	queue.add("also check the disk")
	cfg := newCfg(p, &fakeBackend{}, true)
	cfg.Steer = queue.take
	evs, emit := collect()
	got, err := Run(context.Background(), cfg, nil, "go", emit)
	if err != nil {
		t.Fatal(err)
	}
	second := p.reqs[1].Messages
	if n := len(second); n != 4 || second[3].Role != llm.RoleUser || second[3].Text != "also check the disk" {
		t.Fatalf("second request did not carry the steer after the tool results: %+v", second)
	}
	if n := len(got); n != 5 || got[4].Role != llm.RoleAssistant || got[4].Text != "checked" {
		t.Fatalf("returned history %+v", got)
	}
	if got[3].Role != llm.RoleUser || got[3].Text != "also check the disk" {
		t.Fatalf("steer is not part of the returned history: %+v", got[3])
	}
	var steers []string
	for _, ev := range *evs {
		if ev.Kind == EventSteer {
			steers = append(steers, ev.Text)
		}
	}
	if !reflect.DeepEqual(steers, []string{"also check the disk"}) {
		t.Fatalf("steer events %v", steers)
	}
}

// A steer that arrives while the model is answering is not dropped when the
// answer ends: the answer stays, and the run goes on with the steer.
func TestSteerArrivingDuringAnswerContinuesTheRun(t *testing.T) {
	p := &fakeProvider{turns: []turn{
		{events: []llm.Event{text("partial"), done(1, 1)}},
		{events: []llm.Event{text("adjusted"), done(1, 1)}},
	}}
	calls := 0
	cfg := newCfg(p, &fakeBackend{}, true)
	cfg.Steer = func() []string {
		calls++
		if calls == 1 {
			return []string{"wait, use ls"}
		}
		return nil
	}
	evs, emit := collect()
	got, err := Run(context.Background(), cfg, nil, "go", emit)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(p.reqs[1].Messages); n != 3 || p.reqs[1].Messages[1].Text != "partial" || p.reqs[1].Messages[2].Text != "wait, use ls" {
		t.Fatalf("second request %+v", p.reqs[1].Messages)
	}
	if n := len(got); n != 4 || got[3].Text != "adjusted" || got[2].Text != "wait, use ls" {
		t.Fatalf("returned history %+v", got)
	}
	if last := (*evs)[len(*evs)-1]; last.Kind != EventDone {
		t.Fatalf("run ended with %v, want done once after the steer", last.Kind)
	}
}

// Without a Steer hook nothing changes: the run ends at the answer.
func TestNoSteerHookLeavesTheRunUnchanged(t *testing.T) {
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("answer"), done(1, 1)}}}}
	got, err := Run(context.Background(), newCfg(p, &fakeBackend{}, true), nil, "go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Text != "answer" {
		t.Fatalf("history %+v", got)
	}
}

// sessionOnly stands in for a provider that runs its own loop.
type sessionOnly struct{ fakeProvider }

func (*sessionOnly) RunSession(context.Context, llm.Request, func(context.Context, llm.ToolCall) gate.Result, func(llm.Event)) error {
	return nil
}

func TestSteerableIsFalseForSessionProviders(t *testing.T) {
	if !Steerable(&fakeProvider{}) {
		t.Fatal("a native loop should take steers")
	}
	if Steerable(&sessionOnly{}) {
		t.Fatal("a session provider cannot take steers")
	}
}
