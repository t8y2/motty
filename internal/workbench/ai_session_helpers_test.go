package workbench

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/sshconn"
)

// scriptedModel answers each request from a script: a function of the request.
type scriptedModel struct {
	mu       sync.Mutex
	requests []llm.Request
	turn     func(index int, request llm.Request) []llm.Event
}

func (model *scriptedModel) Stream(ctx context.Context, request llm.Request) (<-chan llm.Event, error) {
	model.mu.Lock()
	index := len(model.requests)
	model.requests = append(model.requests, request)
	model.mu.Unlock()
	events := model.turn(index, request)
	out := make(chan llm.Event, len(events)+1)
	for _, event := range events {
		out <- event
	}
	out <- llm.Event{Kind: llm.EventDone}
	close(out)
	return out, nil
}

func (model *scriptedModel) Models(context.Context) ([]string, error) { return nil, nil }

func toolTurn(name, arguments string) []llm.Event {
	return []llm.Event{{Kind: llm.EventToolCall, ToolCall: &llm.ToolCall{ID: "c1", Name: name, Arguments: arguments}}}
}

func textTurn(text string) []llm.Event { return []llm.Event{{Kind: llm.EventText, Text: text}} }

// assistantTestSession is a live session with the assistant pointed at a scripted
// model and a temporary database.
func assistantTestSession(t *testing.T, model *scriptedModel) (*State, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	options.AIProvider = func(store.Provider) llm.Provider { return model }
	state := newTestState(t, options)
	t.Cleanup(state.Close)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := fake.shell(t)
	shell.say("\x1b[0m")
	state.Wake()

	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "test", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "m"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	return state, shell
}

// pumpUntil runs the window thread's wake-ups until the condition holds.
func pumpUntil(t *testing.T, state *State, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; items: %+v", what, state.ai.items)
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

func ask(state *State, question string, mode agent.Mode) {
	state.ai.mode = mode
	state.ai.input.Insert(question)
	state.aiSend()
}
