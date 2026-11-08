package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type openAI struct {
	base
	baseErr        error
	modelsEndpoint string
	modelsErr      error
}

// NewOpenAI returns a Provider speaking OpenAI-compatible Chat Completions.
// An empty BaseURL selects https://api.openai.com/v1.
func NewOpenAI(cfg Config) Provider {
	u, err := openAIBase(cfg.BaseURL)
	p := &openAI{base: newBase(cfg, u), baseErr: err}
	if strings.TrimSpace(cfg.ModelsURL) != "" {
		p.modelsEndpoint, p.modelsErr = openAIModelsBase(cfg.ModelsURL)
	}
	return p
}

// openAIModelsBase normalizes a models URL: trailing slashes and a pasted
// /models suffix are dropped, and a bare host gets /v1.
func openAIModelsBase(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	u, err := normalizeBase(raw)
	if err != nil {
		return "", err
	}
	if path, ok := cutSuffixFold(u.Path, "/models"); ok {
		u.Path = path
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	return u.String(), nil
}

// openAIBase normalizes a base URL: trailing slashes and a pasted
// /chat/completions or /responses suffix are dropped, and a bare host gets /v1.
// Which of the two routes is used follows the provider, not the pasted address.
func openAIBase(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "https://api.openai.com/v1", nil
	}
	u, err := normalizeBase(raw)
	if err != nil {
		return "", err
	}
	if path, ok := cutSuffixFold(u.Path, "/chat/completions"); ok {
		u.Path = path
	} else {
		u.Path, _ = cutSuffixFold(u.Path, "/responses")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	return u.String(), nil
}

func (p *openAI) auth(r *http.Request) {
	r.Header.Set("Accept", "text/event-stream")
	if p.key != "" {
		r.Header.Set("Authorization", "Bearer "+p.key)
	}
}

type oaToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   *string         `json:"arguments,omitempty"`
}

type oaToolCall struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Function oaToolFunc `json:"function"`
}

// oaPart is one piece of a user message that carries pictures.
type oaPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *oaImageURL `json:"image_url,omitempty"`
}

type oaImageURL struct {
	URL string `json:"url"`
}

// oaContent is a message's content: null, a string, or, when the message has
// pictures, a list of parts. Plain text stays a string because not every
// OpenAI-compatible server reads the list form.
type oaContent struct {
	text  *string
	parts []oaPart
}

func (c oaContent) MarshalJSON() ([]byte, error) {
	switch {
	case c.parts != nil:
		return json.Marshal(c.parts)
	case c.text != nil:
		return json.Marshal(*c.text)
	}
	return []byte("null"), nil
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    oaContent    `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaTool struct {
	Type     string     `json:"type"`
	Function oaToolFunc `json:"function"`
}

type oaRequest struct {
	Model         string      `json:"model"`
	Messages      []oaMessage `json:"messages"`
	Stream        bool        `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Tools           []oaTool `json:"tools,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
}

func (p *openAI) buildBody(req Request) ([]byte, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	out := oaRequest{
		Model:           req.Model,
		Stream:          true,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		ReasoningEffort: req.ReasoningEffort,
	}
	if out.ReasoningEffort == "default" {
		out.ReasoningEffort = ""
	}
	out.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{true}
	str := func(s string) *string { return &s }
	if req.System != "" {
		out.Messages = append(out.Messages, oaMessage{Role: "system", Content: oaContent{text: str(req.System)}})
	}
	for _, m := range req.Messages {
		om := oaMessage{Role: string(m.Role)}
		switch m.Role {
		case RoleSystem, RoleUser:
			om.Content = oaContent{text: str(m.Text)}
			if len(m.Images) > 0 {
				om.Content = oaContent{parts: []oaPart{{Type: "text", Text: m.Text}}}
				for _, image := range m.Images {
					om.Content.parts = append(om.Content.parts, oaPart{Type: "image_url", ImageURL: &oaImageURL{URL: image.dataURL()}})
				}
			}
		case RoleAssistant:
			if m.Text != "" || len(m.ToolCalls) == 0 {
				om.Content = oaContent{text: str(m.Text)}
			}
			for _, tc := range m.ToolCalls {
				args, err := validateArgs(tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("tool call %q: %w", tc.Name, err)
				}
				om.ToolCalls = append(om.ToolCalls, oaToolCall{ID: tc.ID, Type: "function",
					Function: oaToolFunc{Name: tc.Name, Arguments: &args}})
			}
		case RoleTool:
			if m.ToolCallID == "" {
				return nil, errors.New("llm: tool message without ToolCallID")
			}
			om.Content, om.ToolCallID = oaContent{text: str(m.Text)}, m.ToolCallID
		default:
			return nil, fmt.Errorf("llm: unknown role %q", m.Role)
		}
		out.Messages = append(out.Messages, om)
	}
	for _, t := range req.Tools {
		schema, err := validateSchema(t.Schema)
		if err != nil {
			return nil, fmt.Errorf("tool %q: %w", t.Name, err)
		}
		out.Tools = append(out.Tools, oaTool{Type: "function",
			Function: oaToolFunc{Name: t.Name, Description: t.Description, Parameters: schema}})
	}
	return json.Marshal(out)
}

func (p *openAI) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if p.baseErr != nil {
		return nil, p.baseErr
	}
	body, err := p.buildBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := p.do(ctx, http.MethodPost, p.endpoint+"/chat/completions", body, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		p.auth(r)
	})
	if err != nil {
		return nil, err
	}
	return p.pump(ctx, resp.Body, &oaHandler{dsml: newDSMLFilter(len(req.Tools) > 0)}), nil
}

func (p *openAI) Models(ctx context.Context) ([]string, error) {
	if p.modelsErr != nil {
		return nil, p.modelsErr
	}
	endpoint := p.endpoint
	if p.modelsEndpoint != "" {
		endpoint = p.modelsEndpoint
	} else if p.baseErr != nil {
		return nil, p.baseErr
	}
	resp, err := p.do(ctx, http.MethodGet, endpoint+"/models", nil, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		if p.key != "" {
			r.Header.Set("Authorization", "Bearer "+p.key)
		}
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decodeBounded(resp.Body, &list); err != nil {
		return nil, p.scrubErr(err)
	}
	ids := make([]string, 0, len(list.Data))
	for _, d := range list.Data {
		ids = append(ids, d.ID)
	}
	return sortedUnique(ids), nil
}

type oaToolDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaDelta struct {
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content"`
	Reasoning        string        `json:"reasoning"`
	ToolCalls        []oaToolDelta `json:"tool_calls"`
}

type oaChoice struct {
	Delta        oaDelta `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// The cached share of PromptTokens is reported under different names:
	// OpenAI and most compatible providers nest it, DeepSeek and Moonshot do not.
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	CachedTokens         int `json:"cached_tokens"`
}

// cacheRead is the number of prompt tokens served from the provider's cache.
func (u *oaUsage) cacheRead() int {
	for _, n := range [...]int{u.PromptTokensDetails.CachedTokens, u.PromptCacheHitTokens, u.CachedTokens} {
		if n > 0 {
			return min(n, u.PromptTokens)
		}
	}
	return 0
}

type oaChunk struct {
	Choices []oaChoice      `json:"choices"`
	Usage   *oaUsage        `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

type oaPartial struct {
	id, name string
	args     strings.Builder
}

type oaHandler struct {
	calls    map[int]*oaPartial
	last     int
	finished bool
	stop     string
	usage    Usage
	// dsml recovers tool calls a provider wrote as text, and recovered says
	// one was, so the stop reason can name the turn a tool turn.
	dsml      dsmlFilter
	recovered bool
	// chunk is reused between events. Strings and tool calls still allocate
	// inside it; this only saves rebuilding the outer struct and the choices
	// slice for every token.
	chunk oaChunk
}

func (h *oaHandler) slot(d oaToolDelta) *oaPartial {
	if h.calls == nil {
		h.calls = map[int]*oaPartial{}
	}
	var idx int
	switch {
	case d.Index != nil:
		idx = *d.Index
	case d.ID != "" && len(h.calls) > 0:
		idx = h.last + 1
		for _, c := range h.calls {
			if c.id == d.ID {
				idx = h.last
			}
		}
	case len(h.calls) > 0:
		idx = h.last
	}
	h.last = idx
	c := h.calls[idx]
	if c == nil {
		c = &oaPartial{}
		h.calls[idx] = c
	}
	return c
}

func (h *oaHandler) flushTools(em *emitter) error {
	idxs := make([]int, 0, len(h.calls))
	for i := range h.calls {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		c := h.calls[i]
		args, err := validateArgs(c.args.String())
		if err != nil {
			return fmt.Errorf("tool call %q: %w", c.name, err)
		}
		if c.name == "" {
			return errors.New("llm: tool call without a name")
		}
		id := c.id
		if id == "" {
			id = "call_" + strconv.Itoa(i)
		}
		tc := &ToolCall{ID: id, Name: c.name, Arguments: args}
		if err := em.send(Event{Kind: EventToolCall, ToolCall: tc}); err != nil {
			return err
		}
	}
	h.calls = nil
	return nil
}

func (h *oaHandler) finish(em *emitter) error {
	if err := h.flushTools(em); err != nil {
		return err
	}
	if err := h.flushText(em); err != nil {
		return err
	}
	if h.recovered && (h.stop == "" || h.stop == "stop") {
		h.stop = "tool_calls"
	}
	return em.send(Event{Kind: EventDone, Usage: h.usage, StopReason: h.stop})
}

// text publishes one content delta after the DSML filter has taken the
// markup out of it.
func (h *oaHandler) text(delta string, em *emitter) error {
	text, calls, err := h.dsml.push(delta)
	if err != nil {
		return err
	}
	return h.emitText(text, calls, em)
}

// flushText publishes what the filter still holds when the model has finished.
func (h *oaHandler) flushText(em *emitter) error {
	text, calls, err := h.dsml.flush()
	if err != nil {
		return err
	}
	return h.emitText(text, calls, em)
}

func (h *oaHandler) emitText(text string, calls []ToolCall, em *emitter) error {
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

func (h *oaHandler) event(ev sseEvent, em *emitter) (bool, error) {
	if bytes.Equal(bytes.TrimSpace(ev.Data), []byte("[DONE]")) {
		return true, h.finish(em)
	}
	// Clear the reused chunk while keeping the choices backing array. The JSON
	// decoder overwrites fields that are present and leaves absent ones as
	// they were, so every element up to capacity must be zeroed first.
	if cap := cap(h.chunk.Choices); cap > 0 {
		choices := h.chunk.Choices[:cap]
		for index := range choices {
			choices[index] = oaChoice{}
		}
		h.chunk.Choices = choices[:0]
	} else {
		h.chunk.Choices = nil
	}
	h.chunk.Usage, h.chunk.Error = nil, nil
	if err := json.Unmarshal(ev.Data, &h.chunk); err != nil {
		return false, fmt.Errorf("llm: malformed stream chunk: %w", err)
	}
	c := &h.chunk
	if len(c.Error) > 0 && string(c.Error) != "null" {
		return false, errors.New("llm: provider error: " + errorMessage(c.Error))
	}
	if c.Usage != nil {
		h.usage = Usage{InputTokens: c.Usage.PromptTokens, OutputTokens: c.Usage.CompletionTokens, CacheReadTokens: c.Usage.cacheRead()}
	}
	if len(c.Choices) == 0 {
		return false, nil
	}
	ch := c.Choices[0]
	r := ch.Delta.ReasoningContent
	if r == "" {
		r = ch.Delta.Reasoning
	}
	if r != "" {
		if err := em.send(Event{Kind: EventReasoning, Text: r}); err != nil {
			return false, err
		}
	}
	if ch.Delta.Content != "" {
		if err := h.text(ch.Delta.Content, em); err != nil {
			return false, err
		}
	}
	for _, d := range ch.Delta.ToolCalls {
		s := h.slot(d)
		if d.ID != "" && s.id == "" {
			s.id = d.ID
		}
		if d.Function.Name != "" && s.name == "" {
			s.name = d.Function.Name
		}
		s.args.WriteString(d.Function.Arguments)
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		h.finished, h.stop = true, *ch.FinishReason
		return false, h.flushTools(em)
	}
	return false, nil
}

// eof tolerates servers that omit [DONE] after a finish_reason.
func (h *oaHandler) eof(em *emitter) error {
	if !h.finished {
		return errors.New("llm: stream ended before completion")
	}
	return h.finish(em)
}
