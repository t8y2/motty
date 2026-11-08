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
