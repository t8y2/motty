package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// captureTransport records the request it is handed and answers 400, so a
// provider aimed at any host can be inspected without the network.
type captureTransport struct{ got *http.Request }

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.got = r
	return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request",
		Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}, Request: r}, nil
}

func TestOpenCodeSessionHeader(t *testing.T) {
	providers := map[string]func(Config) Provider{
		"chat":      NewOpenAI,
		"responses": NewOpenAIResponses,
		"anthropic": NewAnthropic,
	}
	bases := map[string]string{
		"chat":      "https://opencode.ai/zen/go/v1",
		"responses": "https://opencode.ai/zen/go/v1",
		"anthropic": "https://opencode.ai/zen/go",
	}
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}}
	for name, newProvider := range providers {
		t.Run(name, func(t *testing.T) {
			stream := func(base, session string) *http.Request {
				tr := &captureTransport{}
				p := newProvider(Config{BaseURL: base, APIKey: "k", Client: &http.Client{Transport: tr}})
				r := req
				r.SessionID = session
				if _, err := p.Stream(context.Background(), r); err == nil {
					t.Fatal("want the canned 400")
				}
				if tr.got == nil {
					t.Fatal("no request sent")
				}
				return tr.got
			}

			got := stream(bases[name], "ses_conversation")
			if h := got.Header.Get("x-opencode-session"); h != "ses_conversation" {
				t.Fatalf("x-opencode-session = %q, want the conversation's id", h)
			}

			// A one-off request (a model test) still carries a fresh id.
			first := stream(bases[name], "").Header.Get("x-opencode-session")
			second := stream(bases[name], "").Header.Get("x-opencode-session")
			if !strings.HasPrefix(first, "ses_") || first == second {
				t.Fatalf("one-off ids = %q, %q; want two distinct ses_ ids", first, second)
			}

			// Subdomains count as OpenCode, other hosts never see the header.
			sub := strings.Replace(bases[name], "://opencode.ai", "://api.OpenCode.ai", 1)
			if h := stream(sub, "ses_x").Header.Get("x-opencode-session"); h != "ses_x" {
				t.Fatalf("subdomain header = %q", h)
			}
			for _, other := range []string{"https://api.example.com/v1", "https://opencode.ai.evil.com/v1", "https://notopencode.ai/v1"} {
				if h := stream(other, "ses_x").Header.Get("x-opencode-session"); h != "" {
					t.Fatalf("%s got x-opencode-session %q", other, h)
				}
			}
		})
	}
}

func TestNewSessionID(t *testing.T) {
	a, b := NewSessionID(), NewSessionID()
	if a == b || len(a) != len("ses_")+32 || !strings.HasPrefix(a, "ses_") {
		t.Fatalf("NewSessionID = %q, %q", a, b)
	}
}
