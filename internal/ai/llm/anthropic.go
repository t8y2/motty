package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const anthropicVersion = "2023-06-01"

type anthropic struct {
	base
	baseErr        error
	modelsEndpoint string
	modelsErr      error
}

// NewAnthropic returns a Provider speaking the Anthropic Messages API. An
// empty BaseURL selects https://api.anthropic.com.
func NewAnthropic(cfg Config) Provider {
	u, err := anthropicBase(cfg.BaseURL)
	p := &anthropic{base: newBase(cfg, u), baseErr: err}
	if strings.TrimSpace(cfg.ModelsURL) != "" {
		p.modelsEndpoint, p.modelsErr = anthropicModelsBase(cfg.ModelsURL)
	}
	return p
}

// anthropicModelsBase normalizes a models URL: trailing slashes, /models, and
// /v1 are dropped so /v1/models can be cleanly appended.
func anthropicModelsBase(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	u, err := normalizeBase(raw)
	if err != nil {
		return "", err
	}
	u.Path, _ = cutSuffixFold(u.Path, "/models")
	u.Path, _ = cutSuffixFold(u.Path, "/v1")
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// anthropicBase returns the base without any /v1 or /v1/messages suffix;
// endpoints are appended as /v1/....
func anthropicBase(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "https://api.anthropic.com", nil
	}
	u, err := normalizeBase(raw)
	if err != nil {
		return "", err
	}
	u.Path, _ = cutSuffixFold(u.Path, "/messages")
	u.Path, _ = cutSuffixFold(u.Path, "/v1")
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func (p *anthropic) headers(r *http.Request) {
	r.Header.Set("anthropic-version", anthropicVersion)
	if p.key == "" {
		return
	}
	// Exactly one credential header: relays that accept only a bearer token
	// reject a request that also carries x-api-key.
	if p.authStyle == AuthStyleBearer {
		r.Header.Set("Authorization", "Bearer "+p.key)
		return
	}
	r.Header.Set("x-api-key", p.key)
}

type anBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Source    *anImage        `json:"source,omitempty"`
	// Thinking, Signature and Data make up thinking and redacted_thinking
	// blocks, which go back to the API exactly as it sent them.
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
	// CacheControl marks the end of a cacheable prefix.
	CacheControl *anCacheControl `json:"cache_control,omitempty"`
}

// anImage is the base64 source of an image block.
type anImage struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// anCacheControl is a prompt-cache breakpoint: the provider caches the whole
// request prefix up to and including the block that carries it.
type anCacheControl struct {
	Type string `json:"type"`
}

var ephemeral = &anCacheControl{Type: "ephemeral"}

type anMessage struct {
	Role    string    `json:"role"`
	Content []anBlock `json:"content"`
}

type anTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *anCacheControl `json:"cache_control,omitempty"`
}

type anThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anRequest struct {
	Model       string      `json:"model"`
	MaxTokens   int         `json:"max_tokens"`
	System      []anBlock   `json:"system,omitempty"`
	Messages    []anMessage `json:"messages"`
	Tools       []anTool    `json:"tools,omitempty"`
	Stream      bool        `json:"stream"`
	Temperature *float64    `json:"temperature,omitempty"`
	Thinking    *anThinking `json:"thinking,omitempty"`
}

const (
	defaultAnthropicMaxTokens  = 4096
	minAnthropicThinkingBudget = 1024
)

// anthropicThinkingBudget is the thinking token budget of an effort level; zero
// means the request does not think. "default" leaves the choice to the model,
// which for Anthropic means no thinking.
func anthropicThinkingBudget(effort string) int {
	switch effort {
	case "minimal":
		return 1024
	case "low":
		return 2048
	case "medium":
		return 4096
	case "high":
		return 8192
	case "xhigh":
		return 16384
	case "max":
		return 32768
	}
	return 0
}

func (p *anthropic) buildBody(req Request) ([]byte, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	out := anRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: true, Temperature: req.Temperature}
	if out.MaxTokens <= 0 {
		out.MaxTokens = defaultAnthropicMaxTokens
	}

	if budget := anthropicThinkingBudget(req.ReasoningEffort); budget > 0 {
		switch {
		case req.MaxTokens <= 0:
			// No limit was asked for, so make room for the answer after the thinking.
			out.MaxTokens = budget + defaultAnthropicMaxTokens
		case budget >= req.MaxTokens:
			// The caller's limit stands: the budget must stay below it.
			budget = req.MaxTokens - 1
		}
		if budget >= minAnthropicThinkingBudget {
			out.Thinking = &anThinking{Type: "enabled", BudgetTokens: budget}
			// Thinking allows only the default temperature.
			out.Temperature = nil
		}
	}
	system := []string{}
	if req.System != "" {
		system = append(system, req.System)
	}
	// addUser appends blocks to the trailing user message so that parallel
	// tool results (and a following user turn) share one message.
	addUser := func(b anBlock) {
		if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == "user" {
			out.Messages[n-1].Content = append(out.Messages[n-1].Content, b)
			return
		}
		out.Messages = append(out.Messages, anMessage{Role: "user", Content: []anBlock{b}})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem:
			if m.Text != "" {
				system = append(system, m.Text)
			}
		case RoleUser:
			// Pictures go ahead of the words about them, which is how the API
			// documents getting the best reading of both.
			for _, image := range m.Images {
				addUser(anBlock{Type: "image", Source: &anImage{Type: "base64", MediaType: image.MediaType, Data: base64.StdEncoding.EncodeToString(image.Data)}})
			}
			if m.Text != "" {
				addUser(anBlock{Type: "text", Text: m.Text})
			}
		case RoleTool:
			if m.ToolCallID == "" {
				return nil, errors.New("llm: tool message without ToolCallID")
			}
			addUser(anBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text})
		case RoleAssistant:
			var blocks []anBlock
			// Thinking goes first, and only when this request thinks: the API
			// refuses a tool turn that continues without its blocks.
			if out.Thinking != nil {
				for _, t := range m.Thinking {
					if t.Redacted {
						blocks = append(blocks, anBlock{Type: "redacted_thinking", Data: t.Data})
					} else {
						blocks = append(blocks, anBlock{Type: "thinking", Thinking: t.Text, Signature: t.Signature})
					}
				}
			}
			if m.Text != "" {
				blocks = append(blocks, anBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				args, err := validateArgs(tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("tool call %q: %w", tc.Name, err)
				}
				blocks = append(blocks, anBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: json.RawMessage(args)})
			}
			if len(blocks) > 0 {
				out.Messages = append(out.Messages, anMessage{Role: "assistant", Content: blocks})
			}
		default:
			return nil, fmt.Errorf("llm: unknown role %q", m.Role)
		}
	}
	if len(out.Messages) == 0 {
		return nil, errors.New("llm: request has no non-empty messages")
	}
	for _, t := range req.Tools {
		schema, err := validateSchema(t.Schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", t.Name, err)
		}
		out.Tools = append(out.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	out.markCacheable(strings.Join(system, "\n\n"))
	return json.Marshal(out)
}

// markCacheable sets the system prompt and places the prompt-cache breakpoints.
// The prefix is tools, then system, then messages, and a breakpoint caches
// everything before it, so one goes on each of the last tool, the system
// prompt and the last message block: a later request that repeats those
// prefixes reads them at a fraction of the price instead of reprocessing them.
// Providers cache only prefixes above a model-specific length (1024 tokens and
// up) and silently ignore a breakpoint on a shorter one.
func (r *anRequest) markCacheable(system string) {
	if n := len(r.Tools); n > 0 {
		r.Tools[n-1].CacheControl = ephemeral
	}
	if system != "" {
		r.System = []anBlock{{Type: "text", Text: system, CacheControl: ephemeral}}
	}
	if n := len(r.Messages); n > 0 {
		last := r.Messages[n-1].Content
		last[len(last)-1].CacheControl = ephemeral
	}
}

func (p *anthropic) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if p.baseErr != nil {
		return nil, p.baseErr
	}
	body, err := p.buildBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.do(ctx, http.MethodPost, p.endpoint+"/v1/messages", body, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "text/event-stream")
		p.headers(r)
	})
	if err != nil {
		return nil, err
	}
	return p.pump(ctx, resp.Body, &anHandler{}), nil
}

func (p *anthropic) Models(ctx context.Context) ([]string, error) {
	if p.modelsErr != nil {
		return nil, p.modelsErr
	}
	endpoint := p.endpoint
	if p.modelsEndpoint != "" {
		endpoint = p.modelsEndpoint
	} else if p.baseErr != nil {
		return nil, p.baseErr
	}
	var ids []string
	after := ""
	for page := 0; page < 10; page++ {
		q := url.Values{"limit": {"1000"}}
		if after != "" {
			q.Set("after_id", after)
		}
		resp, err := p.do(ctx, http.MethodGet, endpoint+"/v1/models?"+q.Encode(), nil, func(r *http.Request) {
			r.Header.Set("Accept", "application/json")
			p.headers(r)
		})
		if err != nil {
			return nil, err
		}
		var list struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		err = decodeBounded(resp.Body, &list)
		resp.Body.Close()
		if err != nil {
			return nil, p.scrubErr(err)
		}
		for _, d := range list.Data {
			ids = append(ids, d.ID)
		}
		if !list.HasMore || list.LastID == "" || list.LastID == after {
			break
		}
		after = list.LastID
	}
	return sortedUnique(ids), nil
}

type anHandler struct {
	blocks map[int]*anBlockState
	usage  anUsage
	stop   string
}

// anUsage is what the stream reports. Anthropic counts the cached tokens
// apart from input_tokens, so the request's whole prompt is their sum.
type anUsage struct {
	input, output, cacheRead, cacheWrite int
}

func (u anUsage) total() Usage {
	return Usage{
		InputTokens:      u.input + u.cacheRead + u.cacheWrite,
		OutputTokens:     u.output,
		CacheReadTokens:  u.cacheRead,
		CacheWriteTokens: u.cacheWrite,
	}
}

// anUsageFields are the usage members of message_start and message_delta; a
// relay may leave any of them out, and an absent field must not erase a count
// an earlier event reported.
type anUsageFields struct {
	InputTokens      *int `json:"input_tokens"`
	OutputTokens     *int `json:"output_tokens"`
	CacheReadTokens  *int `json:"cache_read_input_tokens"`
	CacheWriteTokens *int `json:"cache_creation_input_tokens"`
}

func (u *anUsage) merge(f anUsageFields) {
	for _, field := range []struct {
		from *int
		to   *int
	}{{f.InputTokens, &u.input}, {f.OutputTokens, &u.output}, {f.CacheReadTokens, &u.cacheRead}, {f.CacheWriteTokens, &u.cacheWrite}} {
		if field.from != nil && *field.from > 0 {
			*field.to = *field.from
		}
	}
}

type anBlockState struct {
	tool bool
	id   string
	name string
	args strings.Builder
	// think is set for thinking and redacted_thinking blocks, which are kept
	// whole for replay.
	think    bool
	thinking ThinkingBlock
	text     strings.Builder // the thinking text, which the block carries whole
}

type anEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage anUsageFields `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
		Data string `json:"data"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage anUsageFields   `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func (h *anHandler) event(ev sseEvent, em *emitter) (bool, error) {
	var e anEvent
	if err := json.Unmarshal(ev.Data, &e); err != nil {
		return false, fmt.Errorf("llm: malformed stream event: %w", err)
	}
	if e.Type == "" {
		e.Type = ev.Name
	}
	switch e.Type {
	case "message_start":
		h.usage.merge(e.Message.Usage)
	case "content_block_start":
		if h.blocks == nil {
			h.blocks = map[int]*anBlockState{}
		}
		b := &anBlockState{tool: e.ContentBlock.Type == "tool_use", id: e.ContentBlock.ID, name: e.ContentBlock.Name}
		switch e.ContentBlock.Type {
		case "thinking":
			b.think = true
		case "redacted_thinking":
			b.think = true
			b.thinking = ThinkingBlock{Redacted: true, Data: e.ContentBlock.Data}
		}
		h.blocks[e.Index] = b
		if e.ContentBlock.Type == "text" && e.ContentBlock.Text != "" {
			return false, em.send(Event{Kind: EventText, Text: e.ContentBlock.Text})
		}
	case "content_block_delta":
		switch e.Delta.Type {
		case "text_delta":
			if e.Delta.Text != "" {
				return false, em.send(Event{Kind: EventText, Text: e.Delta.Text})
			}
		case "thinking_delta":
			if e.Delta.Thinking != "" {
				if b := h.blocks[e.Index]; b != nil {
					b.text.WriteString(e.Delta.Thinking)
				}
				return false, em.send(Event{Kind: EventReasoning, Text: e.Delta.Thinking})
			}
		case "signature_delta":
			if b := h.blocks[e.Index]; b != nil {
				b.thinking.Signature = e.Delta.Signature
			}
		case "input_json_delta":
			if b := h.blocks[e.Index]; b != nil {
				b.args.WriteString(e.Delta.PartialJSON)
			}
		}
	case "content_block_stop":
		b := h.blocks[e.Index]
		delete(h.blocks, e.Index)
		if b != nil && b.think {
			block := b.thinking
			if !block.Redacted {
				block.Text = b.text.String()
			}
			return false, em.send(Event{Kind: EventThinking, Thinking: &block})
		}
		if b == nil || !b.tool {
			return false, nil
		}
		args, err := validateArgs(b.args.String())
		if err != nil {
			return false, fmt.Errorf("tool call %q: %w", b.name, err)
		}
		if b.name == "" || b.id == "" {
			return false, errors.New("llm: tool_use block without id or name")
		}
		return false, em.send(Event{Kind: EventToolCall, ToolCall: &ToolCall{ID: b.id, Name: b.name, Arguments: args}})
	case "message_delta":
		if e.Delta.StopReason != "" {
			h.stop = e.Delta.StopReason
		}
		h.usage.merge(e.Usage)
	case "message_stop":
		return true, em.send(Event{Kind: EventDone, Usage: h.usage.total(), StopReason: h.stop})
	case "error":
		return false, errors.New("llm: provider error: " + errorMessage(e.Error))
	}
	return false, nil
}

func (h *anHandler) eof(*emitter) error {
	return errors.New("llm: stream ended before message_stop")
}
