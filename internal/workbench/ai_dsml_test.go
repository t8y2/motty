//go:build !windows

package workbench

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
)

// The exact leak of issue #225: a DeepSeek lane sends the tool call as text
// instead of tool_calls. The stream delivers it in two deltas, as a real one
// does.
const (
	leakProse = "Checking the load.\n"
	leakHead  = "<｜DSML｜ calls>\n<｜DSML｜ invoke"
	leakTail  = " name=\"terminal_exec\">\n" +
		"<｜DSML｜ parameter name=\"command\" string=\"true\">echo dsml-recovered</｜DSML｜ parameter>\n" +
		"</｜DSML｜ invoke>\n</｜DSML｜ calls>"
)

// chatDelta is one OpenAI-compatible content chunk.
func chatDelta(text string) string {
	quoted, _ := json.Marshal(text)
	return "data: {\"choices\":[{\"delta\":{\"content\":" + string(quoted) + "}}]}\n\n"
}

func chatFinish() string {
	return "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
}

// chatReasoning is one OpenAI-compatible thinking chunk.
func chatReasoning(text string) string {
	quoted, _ := json.Marshal(text)
	return "data: {\"choices\":[{\"delta\":{\"reasoning_content\":" + string(quoted) + "}}]}\n\n"
}

// A model that writes its tool call as text must still run the tool and finish
// the answer, and the markup must reach neither the user nor the next request.
func TestAssistantRecoversAToolCallWrittenAsText(t *testing.T) {
	var (
		mu      sync.Mutex
		bodies  []string
		request int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		turn := request
		request++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if turn == 0 {
			io.WriteString(w, chatDelta(leakProse+leakHead))
			w.(http.Flusher).Flush()
			io.WriteString(w, chatDelta(leakTail))
			io.WriteString(w, chatFinish())
			return
		}
		io.WriteString(w, chatDelta("The load is fine."))
		io.WriteString(w, chatFinish())
	}))
	defer srv.Close()

	state, shell := assistantSession(t, llm.NewOpenAI(llm.Config{BaseURL: srv.URL, APIKey: "test"}))
	state.Paint(&gfx.Canvas{}, panelWide)
	state.Click(hitAIToggle)
	ask(state, "check the load", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	// The tool ran on the session, so the turn continued instead of stopping.
	if !strings.Contains(shell.typed(), "echo dsml-recovered") {
		t.Fatalf("the recovered call never ran: %q", shell.typed())
	}
	tooled := false
	for _, item := range state.ai.items {
		if item.kind == aiTool && item.status == aiToolDone {
			tooled = true
		}
		if strings.Contains(item.text, "DSML") {
			t.Fatalf("markup reached the conversation: %q", item.text)
		}
	}
	if !tooled {
		t.Fatalf("no finished tool card: %+v", state.ai.items)
	}
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, "The load is fine.") || hasText(texts, "DSML") {
		t.Fatalf("the answer is not shown cleanly: %q", texts)
	}

	// The history the second request sent carries the result, not the markup:
	// a leaked turn must not teach the model to leak again.
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d", len(bodies))
	}
	if strings.Contains(bodies[1], "DSML") {
		t.Fatalf("the leak was sent back to the model: %s", bodies[1])
	}
	if !strings.Contains(bodies[1], "dsml-recovered") {
		t.Fatalf("the tool result is missing from the second request: %s", bodies[1])
	}
}

// A malformed block must not end a turn that already wrote an answer: the
// answer stays visible and joins the history, and the markup disappears.
func TestAssistantKeepsAnAnswerWrittenBesideAMalformedBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, chatDelta("The load is fine.\n<｜DSML｜ calls>\n<url>https://example.com</url>\n</｜DSML｜ calls>"))
		io.WriteString(w, chatFinish())
	}))
	defer srv.Close()

	state, _ := assistantSession(t, llm.NewOpenAI(llm.Config{BaseURL: srv.URL, APIKey: "test"}))
	state.Paint(&gfx.Canvas{}, panelWide)
	state.Click(hitAIToggle)
	ask(state, "check the load", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	for _, item := range state.ai.items {
		if item.kind == aiNotice {
			t.Fatalf("the run reported an error: %q", item.text)
		}
		if strings.Contains(item.text, "DSML") {
			t.Fatalf("markup reached the conversation: %q", item.text)
		}
	}
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, "The load is fine.") || hasText(texts, "DSML") {
		t.Fatalf("the answer is not shown cleanly: %q", texts)
	}
	last := llm.Message{}
	if n := len(state.ai.history); n > 0 {
		last = state.ai.history[n-1]
	}
	if last.Role != llm.RoleAssistant || !strings.Contains(last.Text, "The load is fine.") {
		t.Fatalf("the answer did not join the history: %+v", state.ai.history)
	}
}

// The same lane can write the call into the thinking channel. The answer is
// then empty, and before recovery the panel showed "the model returned an
// empty answer" while the tool never ran.
func TestAssistantRecoversAToolCallWrittenAsReasoningText(t *testing.T) {
	var (
		mu      sync.Mutex
		bodies  []string
		request int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		turn := request
		request++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if turn == 0 {
			io.WriteString(w, chatReasoning(leakProse+leakHead))
			w.(http.Flusher).Flush()
			io.WriteString(w, chatReasoning(leakTail))
			io.WriteString(w, chatFinish())
			return
		}
		io.WriteString(w, chatDelta("The load is fine."))
		io.WriteString(w, chatFinish())
	}))
	defer srv.Close()

	state, shell := assistantSession(t, llm.NewOpenAI(llm.Config{BaseURL: srv.URL, APIKey: "test"}))
	state.Paint(&gfx.Canvas{}, panelWide)
	state.Click(hitAIToggle)
	ask(state, "check the load", agent.ModeAgent)
	pumpUntil(t, state, "the run to end", func() bool { return state.ai.run == nil })

	if !strings.Contains(shell.typed(), "echo dsml-recovered") {
		t.Fatalf("the call written into the thinking channel never ran: %q", shell.typed())
	}
	for _, item := range state.ai.items {
		if item.kind == aiNotice {
			t.Fatalf("the run reported an error: %q", item.text)
		}
		if strings.Contains(item.text, "DSML") {
			t.Fatalf("markup reached the conversation: %q", item.text)
		}
	}
	_, texts := paintAt(state, panelWide)
	if !hasText(texts, "The load is fine.") || hasText(texts, "DSML") {
		t.Fatalf("the answer is not shown cleanly: %q", texts)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d", len(bodies))
	}
	if strings.Contains(bodies[1], "DSML") || !strings.Contains(bodies[1], "dsml-recovered") {
		t.Fatalf("the second request did not carry the tool result cleanly: %s", bodies[1])
	}
}
