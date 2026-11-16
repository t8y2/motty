package agent

import (
	"strings"
	"testing"

	"motty/internal/ai/llm"
)

func TestTrimHistoryKeepsEverythingUnderBudget(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "one"},
		{Role: llm.RoleAssistant, Text: "answer one"},
		{Role: llm.RoleUser, Text: "two"},
	}
	if got := trimHistory(history, 1000); len(got) != 3 {
		t.Fatalf("kept %d of 3 messages under budget", len(got))
	}
}

func TestTrimHistoryCutsWholeRoundsFromTheFront(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Text: strings.Repeat("a", 400)},
		{Role: llm.RoleAssistant, Text: strings.Repeat("b", 400)},
		{Role: llm.RoleUser, Text: strings.Repeat("c", 400)},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "terminal_read"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Text: strings.Repeat("d", 100)},
		{Role: llm.RoleAssistant, Text: "final answer"},
	}
	// A budget that fits only the last round keeps the last user message and
	// everything after it; the tool result never loses its assistant message.
	trimmed := trimHistory(history, 700)
	if len(trimmed) != 4 || trimmed[0].Role != llm.RoleUser {
		t.Fatalf("trim kept %d messages starting with %v", len(trimmed), trimmed[0].Role)
	}
	if trimmed[2].ToolCallID != "1" {
		t.Fatalf("tool result lost its place: %+v", trimmed)
	}
}

func TestTrimHistoryAlwaysKeepsTheNewestRound(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Text: "old"},
		{Role: llm.RoleUser, Text: strings.Repeat("huge", 1000)},
	}
	trimmed := trimHistory(history, 10)
	if len(trimmed) != 1 || trimmed[0].Text != history[1].Text {
		t.Fatalf("the newest round must survive any budget: %d kept", len(trimmed))
	}
}

// TestRunTrimsLongHistory proves the provider sees the trimmed history, not
// the whole conversation.
func TestRunTrimsLongHistory(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleUser, Text: strings.Repeat("old ", 4000)},
		{Role: llm.RoleAssistant, Text: "old answer"},
		{Role: llm.RoleUser, Text: "the question"},
	}
	p := &fakeProvider{turns: []turn{{events: []llm.Event{text("answer"), done(1, 1)}}}}
	_, err := Run(t.Context(), Config{Provider: p, MaxHistoryBytes: 1000}, history, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	req := p.reqs[0]
	p.mu.Unlock()
	sent := req.Messages
	if len(sent) != 2 || sent[0].Text != "the question" {
		t.Fatalf("provider saw %d messages, first %q", len(sent), firstText(sent))
	}
}

func firstText(messages []llm.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[0].Text
}

func TestRunSendsUserImagesAndHistoryKeepsThem(t *testing.T) {
	picture := llm.Image{MediaType: llm.ImagePNG, Data: []byte("\x89PNG\r\n\x1a\nxxxx")}
	provider := &fakeProvider{turns: []turn{{events: []llm.Event{text("a red square"), done(1, 1)}}}}
	messages, err := Run(t.Context(), Config{Provider: provider, Model: "m", UserImages: []llm.Image{picture}}, nil, "what is this", nil)
	if err != nil {
		t.Fatal(err)
	}
	sent := provider.reqs[0].Messages
	if len(sent) != 1 || len(sent[0].Images) != 1 || sent[0].Images[0].MediaType != llm.ImagePNG {
		t.Fatalf("request messages = %+v", sent)
	}
	if len(messages) == 0 || len(messages[0].Images) != 1 {
		t.Fatalf("the returned history lost the picture: %+v", messages)
	}
}

func TestAPictureCostsAFixedShareOfTheHistoryBudget(t *testing.T) {
	huge := llm.Message{Role: llm.RoleUser, Text: "q", Images: []llm.Image{{MediaType: llm.ImagePNG, Data: make([]byte, 4<<20)}}}
	if got := messageBytes(huge); got != len("q")+8+imageCost {
		t.Fatalf("messageBytes = %d", got)
	}
}
