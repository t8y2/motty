package workbench

import (
	"context"
	"strings"
	"sync"
	"testing"

	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// sessionRecorder answers every request and remembers the SessionID each
// carried.
type sessionRecorder struct {
	mu  sync.Mutex
	ids []string
}

func (r *sessionRecorder) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	r.mu.Lock()
	r.ids = append(r.ids, req.SessionID)
	r.mu.Unlock()
	out := make(chan llm.Event, 2)
	out <- llm.Event{Kind: llm.EventText, Text: "ok"}
	out <- llm.Event{Kind: llm.EventDone}
	close(out)
	return out, nil
}

func (*sessionRecorder) Models(context.Context) ([]string, error) { return nil, nil }

func (r *sessionRecorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ids) == 0 {
		return ""
	}
	return r.ids[len(r.ids)-1]
}

// One conversation keeps one session id across its questions, so OpenCode
// routes it to one cache; a new conversation gets a new one.
func TestAIConversationKeepsOneSessionID(t *testing.T) {
	model := &sessionRecorder{}
	state, _ := panelState(t, model)
	withModel(t, state)

	askAndWait := func(question string) string {
		t.Helper()
		ask(state, question, agent.ModeAsk)
		pumpUntil(t, state, "the answer to "+question, func() bool { return state.ai.run == nil })
		return model.last()
	}

	first := askAndWait("one")
	if !strings.HasPrefix(first, "ses_") {
		t.Fatalf("first question session = %q, want a ses_ id", first)
	}
	if second := askAndWait("two"); second != first {
		t.Fatalf("second question session = %q, want %q", second, first)
	}

	state.aiNewConversation()
	if third := askAndWait("three"); third == "" || third == first {
		t.Fatalf("new conversation session = %q, want a fresh id (old %q)", third, first)
	}
}
