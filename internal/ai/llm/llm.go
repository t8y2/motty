// Package llm is a small streaming client for two LLM wire protocols:
// OpenAI-compatible Chat Completions and Anthropic Messages. It uses only the
// standard library, never logs, and never lets the API key reach an error
// message or an event.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Role is the author of a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a complete tool invocation requested by the model.
type ToolCall struct {
	ID   string
	Name string
	// Arguments is raw JSON text: a JSON object, or "{}" when empty.
	Arguments string
}

// Image is a picture sent with a user message. MediaType is one of the four
// formats every vision endpoint accepts; DetectImage finds it from the bytes.
type Image struct {
	MediaType string
	Data      []byte
}

// Media types an Image can carry.
const (
	ImagePNG  = "image/png"
	ImageJPEG = "image/jpeg"
	ImageGIF  = "image/gif"
	ImageWebP = "image/webp"
)

// DetectImage names the format of data from its signature, never from a file
// name, or returns "" when it is none of the supported ones.
func DetectImage(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return ImagePNG
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return ImageJPEG
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return ImageGIF
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return ImageWebP
	}
	return ""
}

// Message is one conversation turn. RoleTool messages carry the result text
// of the call named by ToolCallID. Images belong to user messages only; they
// stay out of the JSON form, which the CLI backends embed in their prompt.
type Message struct {
	Role       Role
	Text       string
	Images     []Image `json:"-"`
	ToolCalls  []ToolCall
	ToolCallID string
	// Thinking is the reasoning blocks of an assistant turn that Anthropic
	// requires back, unchanged, while the turn continues through tool calls.
	// It is kept for the run in progress only, so it stays out of the JSON form.
	Thinking []ThinkingBlock `json:"-"`
}

// ThinkingBlock is one thinking or redacted_thinking block of a Messages API
// response, with the signature that proves it came from the model.
type ThinkingBlock struct {
	Text      string
	Signature string
	// Redacted marks a redacted_thinking block, whose opaque payload is in Data.
	Redacted bool
	Data     string
}

// ToolSpec declares a tool the model may call.
type ToolSpec struct {
	Name        string
	Description string
	// Schema is a JSON Schema object; empty means no parameters.
	Schema json.RawMessage
}

// Request is one model invocation.
type Request struct {
	Model           string
	System          string
	Messages        []Message
	Tools           []ToolSpec
	MaxTokens       int
	Temperature     *float64
	ReasoningEffort string
}

// EventKind classifies a streamed Event.
type EventKind uint8

const (
	EventText EventKind = iota
	EventReasoning
	EventToolCall
	EventDone
	EventError
	// EventThinking carries one finished ThinkingBlock to keep for replay; the
	// readable text already went out as EventReasoning deltas.
	EventThinking
)

// Usage is the token accounting reported by the provider, when it reports any.
type Usage struct {
	// InputTokens counts every prompt token of the request, cached or not.
	InputTokens  int
	OutputTokens int
	// CacheReadTokens is the part of InputTokens the provider served from its
	// prompt cache, CacheWriteTokens the part it stored there (Anthropic
	// reports it, OpenAI-compatible providers do not). Zero means none or
	// not reported.
	CacheReadTokens  int
	CacheWriteTokens int
}

// Add returns the sum of u and o.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + o.InputTokens,
		OutputTokens:     u.OutputTokens + o.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
	}
}

// Event is one streamed item. Exactly one EventDone or EventError terminates
// a stream, unless the caller's context is cancelled first.
type Event struct {
	Kind       EventKind
	Text       string
	ToolCall   *ToolCall
	Thinking   *ThinkingBlock
	Usage      Usage
	StopReason string
	Err        error
}

// Authentication styles. An empty style keeps the protocol's historical
// behavior: x-api-key for Anthropic and Authorization: Bearer for
// OpenAI-compatible endpoints.
const (
	AuthStyleAPIKey = "api_key"
	AuthStyleBearer = "bearer"
)

// Config selects the endpoint and credentials of a Provider.
type Config struct {
	BaseURL   string
	ModelsURL string
	APIKey    string
	// AuthStyle applies to Anthropic only. AuthStyleBearer sends the key as
	// Authorization: Bearer instead of x-api-key. OpenAI-compatible requests
	// always use bearer authentication.
	AuthStyle string
	// Client is used for all requests. Nil selects a client with no overall
	// timeout but with dial, TLS and response-header timeouts.
	Client *http.Client
	// MaxResponseBytes bounds the total bytes of one SSE stream (default 16 MiB).
	MaxResponseBytes int64
	// IdleTimeout fails a stream that delivers no data for this long (default
	// 2m). A provider can hold a connection open without producing anything,
	// and no other timeout covers that.
	IdleTimeout time.Duration
}

// Provider streams model output and lists models.
type Provider interface {
	Stream(ctx context.Context, req Request) (<-chan Event, error)
	Models(ctx context.Context) ([]string, error)
}

// HTTPError is returned for a non-2xx response. Body is a bounded,
// secret-scrubbed snippet.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return "llm: HTTP " + e.Status
	}
	return "llm: HTTP " + e.Status + ": " + e.Body
}

const (
	defaultMaxResponse = 16 << 20
	defaultIdle        = 2 * time.Minute
	maxLineBytes       = 1 << 20
	maxErrBody         = 512
	maxModelsBody      = 4 << 20
)

type base struct {
	endpoint  string // normalized base, no trailing slash
	key       string
	authStyle string
	client    *http.Client
	max       int64
	idle      time.Duration
}

func newBase(cfg Config, endpoint string) base {
	c := cfg.Client
	if c == nil {
		c = defaultClient()
	}
	m := cfg.MaxResponseBytes
	if m <= 0 {
		m = defaultMaxResponse
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = defaultIdle
	}
	return base{endpoint: endpoint, key: cfg.APIKey, authStyle: cfg.AuthStyle, client: c, max: m, idle: idle}
}

func defaultClient() *http.Client {
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

// scrub removes the API key (raw and JSON-escaped) from s.
func (b *base) scrub(s string) string {
	if b.key == "" {
		return s
	}
	s = strings.ReplaceAll(s, b.key, "***")
	if esc, err := json.Marshal(b.key); err == nil && len(esc) > 2 {
		s = strings.ReplaceAll(s, string(esc[1:len(esc)-1]), "***")
	}
	return s
}

type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.err }

func (b *base) scrubErr(err error) error {
	if err == nil || b.key == "" {
		return err
	}
	if m := err.Error(); strings.Contains(m, b.key) || b.scrub(m) != m {
		return &scrubbedError{msg: b.scrub(m), err: errors.Unwrap(err)}
	}
	return err
}

// do performs a request and converts non-2xx statuses to *HTTPError.
func (b *base) do(ctx context.Context, method, url string, body []byte, hdr func(*http.Request)) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, b.scrubErr(err)
	}
	hdr(req)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, b.scrubErr(err)
	}
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status, Body: b.snippet(snippet)}
	}
	return resp, nil
}

func (b *base) snippet(raw []byte) string {
	s := b.scrub(strings.ToValidUTF8(string(raw), "?"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrBody {
		s = strings.ToValidUTF8(s[:maxErrBody], "") + "..."
	}
	return s
}

// normalizeBase parses and trims a user supplied base URL.
func normalizeBase(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("llm: invalid base URL %q", raw)
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

func cutSuffixFold(s, suf string) (string, bool) {
	if len(s) >= len(suf) && strings.EqualFold(s[len(s)-len(suf):], suf) {
		return s[:len(s)-len(suf)], true
	}
	return s, false
}

// validateArgs returns arguments as a JSON object text, "{}" when empty.
func validateArgs(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}", nil
	}
	if s[0] != '{' || !json.Valid([]byte(s)) {
		return "", errors.New("llm: tool arguments are not a valid JSON object")
	}
	return s, nil
}

func validateSchema(raw json.RawMessage) (json.RawMessage, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	if t[0] != '{' || !json.Valid(t) {
		return nil, errors.New("llm: tool schema is not a valid JSON object")
	}
	return t, nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Model) == "" {
		return errors.New("llm: request has no model")
	}
	if len(req.Messages) == 0 {
		return errors.New("llm: request has no messages")
	}
	for _, t := range req.Tools {
		if t.Name == "" {
			return errors.New("llm: tool without a name")
		}
	}
	for _, m := range req.Messages {
		if len(m.Images) > 0 && m.Role != RoleUser {
			return errors.New("llm: only user messages carry images")
		}
		for _, image := range m.Images {
			if len(image.Data) == 0 || image.MediaType == "" || DetectImage(image.Data) != image.MediaType {
				return errors.New("llm: image is empty or not in its declared format")
			}
		}
	}
	return nil
}

// dataURL is an image as the data: URL the OpenAI APIs take.
func (i Image) dataURL() string {
	return "data:" + i.MediaType + ";base64," + base64.StdEncoding.EncodeToString(i.Data)
}

func sortedUnique(ids []string) []string {
	sort.Strings(ids)
	out := ids[:0]
	for i, s := range ids {
		if s == "" || (i > 0 && s == ids[i-1]) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// errorMessage extracts a message from an "error" JSON value, which may be an
// object with a message or a plain string.
func errorMessage(raw json.RawMessage) string {
	var o struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	if json.Unmarshal(raw, &o) == nil && (o.Message != "" || o.Type != "") {
		if o.Message == "" {
			return o.Type
		}
		return o.Message
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// decodeBounded decodes one JSON document of at most maxModelsBody bytes.
func decodeBounded(r io.Reader, v any) error {
	lr := &io.LimitedReader{R: r, N: maxModelsBody + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return err
	}
	if len(data) > maxModelsBody {
		return errors.New("llm: response body too large")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("llm: malformed response: %w", err)
	}
	return nil
}

// StopsAtLimit reports whether a provider's stop reason means it cut the answer
// off because the output budget ran out, so the caller can say so instead of
// presenting half an answer as the whole of it.
func StopsAtLimit(reason string) bool {
	switch reason {
	case "length", "max_tokens", "max_output_tokens":
		return true
	}
	return false
}
