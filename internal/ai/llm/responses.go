package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// openAIResponses speaks the OpenAI Responses API (POST {base}/responses). It
// shares the base URL rules, authentication and model listing of the Chat
// Completions client and replaces only the request and the stream.
type openAIResponses struct {
	openAI
}

// NewOpenAIResponses returns a Provider speaking the OpenAI Responses API. An
// empty BaseURL selects https://api.openai.com/v1.
func NewOpenAIResponses(cfg Config) Provider {
	u, err := openAIBase(cfg.BaseURL)
	p := &openAIResponses{openAI{base: newBase(cfg, u), baseErr: err}}
	if strings.TrimSpace(cfg.ModelsURL) != "" {
		p.modelsEndpoint, p.modelsErr = openAIModelsBase(cfg.ModelsURL)
	}
	return p
}

// rsItem is one element of the request's input list: a message, a function
// call the model made earlier, or the output of such a call.
// rsPart is one piece of a user message that carries pictures.
type rsPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// rsContent is a message's content: a string, or, when it has pictures, a list
// of parts. The empty value is left out of the item.
type rsContent struct {
	text  *string
	parts []rsPart
}

func (c rsContent) IsZero() bool { return c.text == nil && c.parts == nil }

func (c rsContent) MarshalJSON() ([]byte, error) {
	if c.parts != nil {
		return json.Marshal(c.parts)
	}
	return json.Marshal(c.text)
}

type rsItem struct {
	Type      string    `json:"type,omitempty"`
	Role      string    `json:"role,omitempty"`
	Content   rsContent `json:"content,omitzero"`
	CallID    string    `json:"call_id,omitempty"`
	Name      string    `json:"name,omitempty"`
	Arguments *string   `json:"arguments,omitempty"`
	Output    *string   `json:"output,omitempty"`
}

type rsTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	// Strict is always sent: the API treats an absent value as true, which
	// rejects every schema that is not closed and fully required.
	Strict bool `json:"strict"`
}

type rsReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type rsRequest struct {
	Model           string       `json:"model"`
	Instructions    string       `json:"instructions,omitempty"`
	Input           []rsItem     `json:"input"`
	Tools           []rsTool     `json:"tools,omitempty"`
	Stream          bool         `json:"stream"`
	Store           bool         `json:"store"`
	MaxOutputTokens int          `json:"max_output_tokens,omitempty"`
	Temperature     *float64     `json:"temperature,omitempty"`
	Reasoning       *rsReasoning `json:"reasoning,omitempty"`
}

func (p *openAIResponses) buildBody(req Request) ([]byte, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	// Conversations are replayed in full on every request, so nothing needs to
	// be kept on the provider's side.
	out := rsRequest{Model: req.Model, Instructions: req.System, Stream: true, Store: false,
		MaxOutputTokens: req.MaxTokens, Temperature: req.Temperature, Input: []rsItem{}}
	if req.ReasoningEffort != "" && req.ReasoningEffort != "default" {
		out.Reasoning = &rsReasoning{Effort: req.ReasoningEffort}
	}
	str := func(s string) *string { return &s }
	for _, m := range req.Messages {
		switch m.Role {
		case RoleSystem, RoleUser:
			item := rsItem{Role: string(m.Role), Content: rsContent{text: str(m.Text)}}
			if len(m.Images) > 0 {
				item.Content = rsContent{parts: []rsPart{{Type: "input_text", Text: m.Text}}}
				for _, image := range m.Images {
					item.Content.parts = append(item.Content.parts, rsPart{Type: "input_image", ImageURL: image.dataURL()})
				}
			}
			out.Input = append(out.Input, item)
		case RoleAssistant:
			if m.Text != "" || len(m.ToolCalls) == 0 {
				out.Input = append(out.Input, rsItem{Role: "assistant", Content: rsContent{text: str(m.Text)}})
			}
			for _, tc := range m.ToolCalls {
				args, err := validateArgs(tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("tool call %q: %w", tc.Name, err)
				}
				out.Input = append(out.Input, rsItem{Type: "function_call", CallID: tc.ID, Name: tc.Name, Arguments: &args})
			}
		case RoleTool:
			if m.ToolCallID == "" {
				return nil, errors.New("llm: tool message without ToolCallID")
			}
			out.Input = append(out.Input, rsItem{Type: "function_call_output", CallID: m.ToolCallID, Output: str(m.Text)})
		default:
			return nil, fmt.Errorf("llm: unknown role %q", m.Role)
		}
	}
	for _, t := range req.Tools {
		schema, err := validateSchema(t.Schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", t.Name, err)
		}
		out.Tools = append(out.Tools, rsTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: schema})
	}
	return json.Marshal(out)
}

func (p *openAIResponses) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if p.baseErr != nil {
		return nil, p.baseErr
	}
	body, err := p.buildBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.do(ctx, http.MethodPost, p.endpoint+"/responses", body, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		p.auth(r)
	})
	if err != nil {
		return nil, err
	}
	return p.pump(ctx, resp.Body, &rsHandler{dsml: newDSMLFilter(len(req.Tools) > 0)}), nil
}

type rsOutItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type rsUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type rsResponse struct {
	Usage             *rsUsage        `json:"usage"`
	Error             json.RawMessage `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
}

// rsEvent is the union of the stream events this client reads, told apart by
// Type. The SSE event name is not used: it duplicates Type and some gateways
// drop it.
type rsEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	ItemID      string          `json:"item_id"`
	OutputIndex *int            `json:"output_index"`
	Arguments   string          `json:"arguments"`
	Item        *rsOutItem      `json:"item"`
	Response    *rsResponse     `json:"response"`
	Message     string          `json:"message"`
	Error       json.RawMessage `json:"error"`
}

type rsCall struct {
	callID, name string
	args         strings.Builder
	sent         bool
}

type rsHandler struct {
	calls   []*rsCall
	byID    map[string]*rsCall
	byIndex map[int]*rsCall
	sentAny bool
	ev      rsEvent
	// dsml recovers tool calls a provider wrote as text, and recovered says
	// one was, so the stop reason can name the turn a tool turn.
	dsml      dsmlFilter
	recovered bool
}

// call returns the function call an event belongs to, creating it on first
// sight. Items are named by their item ID and by their position in the output;
// gateways differ in which of the two they repeat on later events.
func (h *rsHandler) call(itemID string, index *int) *rsCall {
	var c *rsCall
	if itemID != "" {
		c = h.byID[itemID]
	}
	if c == nil && index != nil {
		c = h.byIndex[*index]
	}
	if c == nil {
		c = &rsCall{}
		h.calls = append(h.calls, c)
	}
	if itemID != "" {
		if h.byID == nil {
			h.byID = map[string]*rsCall{}
		}
		h.byID[itemID] = c
	}
	if index != nil {
		if h.byIndex == nil {
			h.byIndex = map[int]*rsCall{}
		}
		h.byIndex[*index] = c
	}
	return c
}

func (h *rsHandler) send(c *rsCall, em *emitter) error {
	if c.sent {
		return nil
	}
	if c.name == "" {
		return errors.New("llm: tool call without a name")
	}
	args, err := validateArgs(c.args.String())
	if err != nil {
		return fmt.Errorf("tool call %q: %w", c.name, err)
	}
	id := c.callID
	if id == "" {
		id = "call_" + strconv.Itoa(len(h.calls))
	}
	c.sent, h.sentAny = true, true
	return em.send(Event{Kind: EventToolCall, ToolCall: &ToolCall{ID: id, Name: c.name, Arguments: args}})
}

func (h *rsHandler) finish(em *emitter, reason string, u *rsUsage) error {
	for _, c := range h.calls {
		if err := h.send(c, em); err != nil {
			return err
		}
	}
	if err := h.flushText(em); err != nil {
		return err
	}
	if reason == "" {
		reason = "stop"
		if h.sentAny {
			reason = "tool_calls"
		}
	}
	if h.recovered && reason == "stop" {
		reason = "tool_calls"
	}
	var usage Usage
	if u != nil {
		usage = Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: min(u.InputTokensDetails.CachedTokens, u.InputTokens)}
	}
	return em.send(Event{Kind: EventDone, Usage: usage, StopReason: reason})
}

func (h *rsHandler) event(ev sseEvent, em *emitter) (bool, error) {
	h.ev = rsEvent{}
	e := &h.ev
	if err := json.Unmarshal(ev.Data, e); err != nil {
		return false, fmt.Errorf("llm: malformed stream chunk: %w", err)
	}
	switch e.Type {
	case "response.output_text.delta":
		if e.Delta != "" {
			return false, h.text(e.Delta, em)
		}
	case "response.refusal.delta":
		if e.Delta != "" {
			return false, em.send(Event{Kind: EventText, Text: e.Delta})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if e.Delta != "" {
			return false, em.send(Event{Kind: EventReasoning, Text: e.Delta})
		}
	case "response.output_item.added":
		if e.Item != nil && e.Item.Type == "function_call" {
			c := h.call(e.Item.ID, e.OutputIndex)
			if e.Item.CallID != "" {
				c.callID = e.Item.CallID
			}
			if e.Item.Name != "" {
				c.name = e.Item.Name
			}
			c.args.WriteString(e.Item.Arguments)
		}
	case "response.function_call_arguments.delta":
		h.call(e.ItemID, e.OutputIndex).args.WriteString(e.Delta)
	case "response.function_call_arguments.done":
		if e.Arguments != "" {
			c := h.call(e.ItemID, e.OutputIndex)
			c.args.Reset()
			c.args.WriteString(e.Arguments)
		}
	case "response.output_item.done":
		if e.Item != nil && e.Item.Type == "function_call" {
			c := h.call(e.Item.ID, e.OutputIndex)
			if e.Item.CallID != "" {
				c.callID = e.Item.CallID
			}
			if e.Item.Name != "" {
				c.name = e.Item.Name
			}
			if e.Item.Arguments != "" {
				c.args.Reset()
				c.args.WriteString(e.Item.Arguments)
			}
			return false, h.send(c, em)
		}
	case "response.completed":
		var u *rsUsage
		if e.Response != nil {
			u = e.Response.Usage
		}
		return true, h.finish(em, "", u)
	case "response.incomplete":
		reason, usage := "length", (*rsUsage)(nil)
		if e.Response != nil {
			usage = e.Response.Usage
			if d := e.Response.IncompleteDetails; d != nil && d.Reason != "" && d.Reason != "max_output_tokens" {
				reason = d.Reason
			}
		}
		return true, h.finish(em, reason, usage)
	case "response.failed":
		var raw json.RawMessage
		if e.Response != nil {
			raw = e.Response.Error
		}
		return false, errors.New("llm: provider error: " + errorMessage(orRaw(raw, "response failed")))
	case "error":
		message := e.Message
		if message == "" {
			message = "unspecified error"
		}
		return false, errors.New("llm: provider error: " + errorMessage(orRaw(e.Error, message)))
	default:
		// Gateways that put an error object on an untyped chunk.
		if len(e.Error) > 0 && string(e.Error) != "null" {
			return false, errors.New("llm: provider error: " + errorMessage(e.Error))
		}
	}
	return false, nil
}

// orRaw returns raw, or text as a JSON string when raw says nothing.
func orRaw(raw json.RawMessage, text string) json.RawMessage {
	if len(raw) > 0 && string(raw) != "null" {
		return raw
	}
	quoted, _ := json.Marshal(text)
	return quoted
}

// text publishes one output delta after the DSML filter has taken the markup
// out of it.
func (h *rsHandler) text(delta string, em *emitter) error {
	text, calls, err := h.dsml.push(delta)
	if err != nil {
		return err
	}
	return h.emitText(text, calls, em)
}

// flushText publishes what the filter still holds when the response is done.
func (h *rsHandler) flushText(em *emitter) error {
	text, calls, err := h.dsml.flush()
	if err != nil {
		return err
	}
	return h.emitText(text, calls, em)
}

func (h *rsHandler) emitText(text string, calls []ToolCall, em *emitter) error {
	if text != "" {
		if err := em.send(Event{Kind: EventText, Text: text}); err != nil {
			return err
		}
	}
	for i := range calls {
		h.recovered = true
		if err := em.send(Event{Kind: EventToolCall, ToolCall: &calls[i]}); err != nil {
			return err
		}
	}
	return nil
}

func (h *rsHandler) eof(em *emitter) error {
	return errors.New("llm: stream ended before completion")
}
