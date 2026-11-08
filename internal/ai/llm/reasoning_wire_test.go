package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIReasoningEffortWireFormat(t *testing.T) {
	p := &openAI{}
	req := Request{
		Model: "o3-mini",
		Messages: []Message{
			{Role: RoleUser, Text: "hello"},
		},
		ReasoningEffort: "high",
	}
	body, err := p.buildBody(req)
	if err != nil {
		t.Fatalf("buildBody failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if raw["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort='high', got %v", raw["reasoning_effort"])
	}
}

func TestResponsesReasoningWireFormat(t *testing.T) {
	p := &openAIResponses{}
	req := Request{
		Model: "o3-mini",
		Messages: []Message{
			{Role: RoleUser, Text: "hello"},
		},
		ReasoningEffort: "low",
	}
	body, err := p.buildBody(req)
	if err != nil {
		t.Fatalf("buildBody failed: %v", err)
	}
	var raw struct {
		Reasoning *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if raw.Reasoning == nil || raw.Reasoning.Effort != "low" {
		t.Errorf("expected reasoning.effort='low', got %+v", raw.Reasoning)
	}
}

func TestAnthropicThinkingWireFormat(t *testing.T) {
	p := &anthropic{}
	req := Request{
		Model: "claude-3-7-sonnet-20250219",
		Messages: []Message{
			{Role: RoleUser, Text: "hello"},
		},
		ReasoningEffort: "medium",
	}
	body, err := p.buildBody(req)
	if err != nil {
		t.Fatalf("buildBody failed: %v", err)
	}
	if !strings.Contains(string(body), `"thinking":{"type":"enabled","budget_tokens":4096}`) {
		t.Errorf("anthropic wire missing thinking block: %s", string(body))
	}
}

// A tool turn continued with thinking on must carry its thinking blocks back
// unchanged, and a request that does not think must not send them.
func TestAnthropicThinkingBlocksReplay(t *testing.T) {
	p := &anthropic{}
	history := []Message{
		{Role: RoleUser, Text: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "t1", Name: "run", Arguments: "{}"}},
			Thinking: []ThinkingBlock{{Text: "plan", Signature: "sig"}, {Redacted: true, Data: "opaque"}}},
		{Role: RoleTool, ToolCallID: "t1", Text: "ok"},
	}
	body, err := p.buildBody(Request{Model: "claude-x", Messages: history, ReasoningEffort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type, Thinking, Signature, Data string
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	blocks := raw.Messages[1].Content
	if len(blocks) != 3 || blocks[0].Type != "thinking" || blocks[0].Thinking != "plan" || blocks[0].Signature != "sig" ||
		blocks[1].Type != "redacted_thinking" || blocks[1].Data != "opaque" || blocks[2].Type != "tool_use" {
		t.Fatalf("assistant blocks = %+v, want thinking, redacted_thinking, tool_use in that order", blocks)
	}
	body, err = p.buildBody(Request{Model: "claude-x", Messages: history})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "thinking") {
		t.Fatalf("a request that does not think sent thinking blocks: %s", body)
	}
}

func TestAnthropicThinkingBudgetStaysBelowMaxTokens(t *testing.T) {
	p := &anthropic{}
	body, err := p.buildBody(Request{Model: "claude-x", Messages: []Message{{Role: RoleUser, Text: "hi"}}, ReasoningEffort: "max", MaxTokens: 6000})
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		MaxTokens int `json:"max_tokens"`
		Thinking  struct {
			Budget int `json:"budget_tokens"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.MaxTokens != 6000 || raw.Thinking.Budget >= raw.MaxTokens {
		t.Fatalf("max_tokens %d, budget %d: the caller's limit must hold and the budget stay under it", raw.MaxTokens, raw.Thinking.Budget)
	}
}
