package cli

import (
	"encoding/json"
	"errors"
	"motty/internal/ai/llm"
	"strings"
)

type parser struct {
	kind     string
	done     bool
	usage    llm.Usage
	streamed map[int]bool
	text     bool
	// piStreamed marks that the current Pi message already streamed its text,
	// so the message_end snapshot is not appended a second time.
	piStreamed bool
}

func newParser(kind string) *parser { return &parser{kind: kind} }
func (p *parser) line(line []byte) ([]llm.Event, error) {
	var header struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
	}
	if json.Unmarshal(line, &header) != nil || header.Type == "" {
		return nil, errors.New("cli: invalid_output")
	}
	if header.Type == "error" || header.Type == "turn.failed" || header.IsError {
		return nil, providerError(line)
	}
	relevant := false
	switch p.kind {
	case "claude-code-cli":
		relevant = header.Type == "stream_event" || header.Type == "assistant" || header.Type == "result"
	case "codex-cli":
		relevant = header.Type == "item.completed" || header.Type == "turn.completed"
	case "opencode-cli":
		relevant = header.Type == "text" || header.Type == "reasoning" || header.Type == "step_finish"
	case "pi-cli":
		relevant = header.Type == "message_start" || header.Type == "message_update" ||
			header.Type == "message_end" || header.Type == "agent_settled"
	default:
		return nil, errors.New("cli: unsupported_provider")
	}
	if !relevant {
		return nil, nil
	}
	var v struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
		Usage   struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
			// Pi reports the same counts under its own names.
			PiInput  int `json:"input"`
			PiOutput int `json:"output"`
		} `json:"usage"`
		Event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Block struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content_block"`
			Delta struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"delta"`
		} `json:"event"`
		Item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Message struct {
			Role string `json:"role"`
			// Content is an array of blocks for assistant messages and a plain
			// string for user messages, so it is decoded where each kind needs it.
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Assistant struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
		Part struct {
			Text   string `json:"text"`
			Reason string `json:"reason"`
			Tokens struct {
				Input  int `json:"input"`
				Output int `json:"output"`
			} `json:"tokens"`
		} `json:"part"`
	}
	if decodeErr := json.Unmarshal(line, &v); decodeErr != nil || v.Type == "" {
		return nil, errors.New("cli: invalid_output")
	}
	if v.Type == "error" || v.Type == "turn.failed" || v.IsError {
		return nil, errors.New("cli: provider_failed")
	}
	if p.done {
		return nil, errors.New("cli: output_after_completion")
	}
	var out []llm.Event
	add := func(kind llm.EventKind, s string) {
		if s != "" {
			out = append(out, llm.Event{Kind: kind, Text: s})
			if kind == llm.EventText {
				p.text = true
			}
		}
	}
	switch p.kind {
	case "claude-code-cli":
		switch v.Type {
		case "stream_event":
			if v.Event.Type == "message_start" {
				p.streamed = nil
			}
			streamText := func(text string) {
				if text != "" {
					if p.streamed == nil {
						p.streamed = make(map[int]bool)
					}
					p.streamed[v.Event.Index] = true
					add(llm.EventText, text)
				}
			}
			if v.Event.Type == "content_block_start" && v.Event.Block.Type == "text" {
				streamText(v.Event.Block.Text)
			}
			switch v.Event.Delta.Type {
			case "text_delta":
				streamText(v.Event.Delta.Text)
			case "thinking_delta":
				add(llm.EventReasoning, v.Event.Delta.Thinking)
			}
		case "assistant":
			// Tool/thinking block starts must not reset text already streamed
			// in the same message; only skip the matching snapshot blocks.
			for index, block := range messageBlocks(v.Message.Content) {
				if block.Type == "text" && !p.streamed[index] {
					add(llm.EventText, block.Text)
				}
			}
			p.streamed = nil
		case "result":
			if v.Subtype != "success" {
				return nil, providerError(line)
			}
			if !p.text {
				add(llm.EventText, v.Result)
			}
			p.done = true
			p.usage = llm.Usage{InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}
		}
	case "codex-cli":
		switch v.Type {
		case "item.completed":
			switch v.Item.Type {
			case "agent_message":
				add(llm.EventText, v.Item.Text)
			case "reasoning":
				add(llm.EventReasoning, v.Item.Text)
			}
		case "turn.completed":
			p.done = true
			p.usage = llm.Usage{InputTokens: v.Usage.Input, OutputTokens: v.Usage.Output}
		}
	case "opencode-cli":
		switch v.Type {
		case "text":
			add(llm.EventText, v.Part.Text)
		case "reasoning":
			add(llm.EventReasoning, v.Part.Text)
		case "step_finish":
			p.usage.InputTokens += v.Part.Tokens.Input
			p.usage.OutputTokens += v.Part.Tokens.Output
			if v.Part.Reason == "stop" {
				p.done = true
			}
		}
	case "pi-cli":
		switch v.Type {
		case "message_start":
			if v.Message.Role == "assistant" {
				p.piStreamed = false
			}
		case "message_update":
			switch v.Assistant.Type {
			case "text_delta":
				p.piStreamed = true
				add(llm.EventText, v.Assistant.Delta)
			case "thinking_delta":
				add(llm.EventReasoning, v.Assistant.Delta)
			case "error":
				return nil, providerError(line)
			}
			if v.Usage.PiInput > 0 || v.Usage.PiOutput > 0 {
				p.usage = llm.Usage{InputTokens: v.Usage.PiInput, OutputTokens: v.Usage.PiOutput}
			}
		case "message_end":
			if v.Message.Role == "assistant" && !p.piStreamed {
				for _, block := range messageBlocks(v.Message.Content) {
					if block.Type == "text" {
						add(llm.EventText, block.Text)
					}
				}
			}
			if v.Usage.PiInput > 0 || v.Usage.PiOutput > 0 {
				p.usage = llm.Usage{InputTokens: v.Usage.PiInput, OutputTokens: v.Usage.PiOutput}
			}
		case "agent_settled":
			p.done = true
		}
	default:
		return nil, errors.New("cli: unsupported_provider")
	}
	return out, nil
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// messageBlocks reads an assistant message's content array; a user message's
// string content yields none.
func messageBlocks(raw json.RawMessage) []textBlock {
	var blocks []textBlock
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

func providerError(raw []byte) error {
	text := strings.ToLower(string(raw))
	for _, hint := range []string{"not logged in", "not authenticated", "please log in", "please run /login", "login required"} {
		if strings.Contains(text, hint) {
			return errors.New("cli: login_required")
		}
	}
	return errors.New("cli: provider_failed")
}
