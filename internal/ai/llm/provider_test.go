package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- stream body builders ----

func oaData(s string) string { return "data: " + s + "\n\n" }

func oaText(parts ...string) string {
	var sb strings.Builder
	for _, p := range parts {
		b, _ := json.Marshal(p)
		sb.WriteString(oaData(`{"choices":[{"delta":{"content":` + string(b) + `}}]}`))
	}
	return sb.String()
}

func oaFinish(reason string) string {
	return oaData(`{"choices":[{"delta":{},"finish_reason":"`+reason+`"}]}`) +
		oaData(`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3}}`) + oaData("[DONE]")
}

func anEv(name, data string) string { return "event: " + name + "\ndata: " + data + "\n\n" }

func anStart() string {
	return anEv("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":1}}}`) +
		anEv("ping", `{"type":"ping"}`)
}

func anTextBlock(i int, parts ...string) string {
	s := anEv("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, i))
	for _, p := range parts {
		b, _ := json.Marshal(p)
		s += anEv("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, i, b))
	}
	return s + anEv("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
}

func anToolBlock(i int, id, name, args string, chunk int) string {
	s := anEv("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, i, id, name))
	for _, p := range splitRunes(args, chunk) {
		part, _ := json.Marshal(p)
		s += anEv("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, i, part))
	}
	return s + anEv("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i))
}

func anEnd(reason string) string {
	return anEv("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+reason+`"},"usage":{"output_tokens":9}}`) +
		anEv("message_stop", `{"type":"message_stop"}`)
}

func oaToolChunks(idx int, id, name, args string, chunk int) string {
	s := oaData(fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":""}}]}}]}`, idx, id, name))
	for _, p := range splitRunes(args, chunk) {
		part, _ := json.Marshal(p)
		s += oaData(fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":%s}}]}}]}`, idx, part))
	}
	return s
}

// ---- Responses API stream body builders ----

func rsEv(typ, rest string) string {
	if rest != "" {
		rest = "," + rest
	}
	return "event: " + typ + "\ndata: " + `{"type":"` + typ + `"` + rest + "}\n\n"
}

func rsText(parts ...string) string {
	s := rsEv("response.created", `"response":{"status":"in_progress"}`)
	for _, p := range parts {
		b, _ := json.Marshal(p)
		s += rsEv("response.output_text.delta", `"item_id":"msg_1","output_index":0,"delta":`+string(b))
	}
	return s
}

func rsCompleted() string {
	return rsEv("response.completed", `"response":{"status":"completed","usage":{"input_tokens":7,"output_tokens":3}}`)
}

func rsToolItem(idx int, callID, name, args string, chunk int) string {
	item := fmt.Sprintf(`"output_index":%d,"item":{"type":"function_call","id":"fc_%s","call_id":%q,"name":%q,"arguments":%%q}`, idx, callID, callID, name)
	s := rsEv("response.output_item.added", fmt.Sprintf(item, ""))
	for _, p := range splitRunes(args, chunk) {
		part, _ := json.Marshal(p)
		s += rsEv("response.function_call_arguments.delta", fmt.Sprintf(`"item_id":"fc_%s","output_index":%d,"delta":%s`, callID, idx, part))
	}
	s += rsEv("response.function_call_arguments.done", fmt.Sprintf(`"item_id":"fc_%s","output_index":%d,"arguments":%q`, callID, idx, args))
	return s + rsEv("response.output_item.done", fmt.Sprintf(item, args))
}

type proto struct {
	name string
	new  func(Config) Provider
	head string                       // a stream prefix that has already produced the text "a"
	text func(parts ...string) string // full successful text stream
	tool func(calls ...[3]string) string
}

var protos = []proto{
	{"openai", NewOpenAI, oaText("a"),
		func(parts ...string) string { return oaText(parts...) + oaFinish("stop") },
		func(calls ...[3]string) string {
			s := ""
			for i, c := range calls {
				s += oaToolChunks(i, c[0], c[1], c[2], 2)
			}
			return s + oaFinish("tool_calls")
		}},
	{"anthropic", NewAnthropic, anStart() + anTextBlock(0, "a"),
		func(parts ...string) string { return anStart() + anTextBlock(0, parts...) + anEnd("end_turn") },
		func(calls ...[3]string) string {
			s := anStart()
			for i, c := range calls {
				s += anToolBlock(i, c[0], c[1], c[2], 2)
			}
			return s + anEnd("tool_use")
		}},
	{"openai-responses", NewOpenAIResponses, rsText("a"),
		func(parts ...string) string { return rsText(parts...) + rsCompleted() },
		func(calls ...[3]string) string {
			s := ""
			for i, c := range calls {
				s += rsToolItem(i, c[0], c[1], c[2], 2)
			}
			return s + rsCompleted()
		}},
}

func (p proto) provider(srv *httptest.Server, mutate ...func(*Config)) Provider {
	cfg := Config{BaseURL: srv.URL, APIKey: testKey, Client: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}
	for _, m := range mutate {
		m(&cfg)
	}
	return p.new(cfg)
}

var simpleReq = Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}}

func TestPlainText(t *testing.T) {
	for _, p := range protos {
		for _, bytewise := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bytewise=%v", p.name, bytewise), func(t *testing.T) {
				srv := sseServer(t, p.text("Hel", "lo ", "你好🙂", " world"), bytewise, nil)
				ch, err := p.provider(srv).Stream(bg, simpleReq)
				if err != nil {
					t.Fatal(err)
				}
				evs := collect(t, ch)
				done := terminal(t, evs)
				if done.Kind != EventDone || text(evs, EventText) != "Hello 你好🙂 world" {
					t.Fatalf("got %+v", evs)
				}
				if p.name != "anthropic" && (done.Usage != (Usage{InputTokens: 7, OutputTokens: 3}) || done.StopReason != "stop") {
					t.Fatalf("done %+v", done)
				}
				if p.name == "anthropic" && (done.Usage != (Usage{InputTokens: 11, OutputTokens: 9}) || done.StopReason != "end_turn") {
					t.Fatalf("done %+v", done)
				}
			})
		}
	}
}

func TestReasoning(t *testing.T) {
	oa := oaData(`{"choices":[{"delta":{"reasoning_content":"think "}}]}`) +
		oaData(`{"choices":[{"delta":{"reasoning_content":"hard","content":null}}]}`) + oaText("answer") + oaFinish("stop")
	an := anStart() + anEv("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`) +
		anEv("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think "}}`) +
		anEv("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hard"}}`) +
		anEv("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"abc"}}`) +
		anEv("content_block_stop", `{"type":"content_block_stop","index":0}`) + anTextBlock(1, "answer") + anEnd("end_turn")
	for i, body := range []string{oa, an} {
		p := protos[i]
		srv := sseServer(t, body, false, nil)
		ch, err := p.provider(srv).Stream(bg, simpleReq)
		if err != nil {
			t.Fatal(err)
		}
		evs := collect(t, ch)
		terminal(t, evs)
		if text(evs, EventReasoning) != "think hard" || text(evs, EventText) != "answer" {
			t.Fatalf("%s: %+v", p.name, evs)
		}
	}
}

func TestParallelToolCalls(t *testing.T) {
	args1 := `{"path":"/etc/hosts","lines":[1,2,3],"note":"héllo 🙂"}`
	args2 := `{"cmd":"ls -la"}`
	for _, p := range protos {
		for _, bytewise := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bytewise=%v", p.name, bytewise), func(t *testing.T) {
				body := p.tool([3]string{"call_a", "read", args1}, [3]string{"call_b", "run", args2})
				srv := sseServer(t, body, bytewise, nil)
				ch, err := p.provider(srv).Stream(bg, simpleReq)
				if err != nil {
					t.Fatal(err)
				}
				evs := collect(t, ch)
				done := terminal(t, evs)
				want := []ToolCall{{"call_a", "read", args1}, {"call_b", "run", args2}}
				if got := toolCalls(evs); !reflect.DeepEqual(got, want) || done.Kind != EventDone {
					t.Fatalf("got %+v", evs)
				}
				if !strings.Contains(done.StopReason, "tool") {
					t.Fatalf("stop %q", done.StopReason)
				}
			})
		}
	}
}

func TestOpenAIToolCallsInterleavedAndEmptyArgs(t *testing.T) {
	body := oaData(`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"two","arguments":"{\"x\":"}},{"index":0,"id":"a","function":{"name":"one"}}]}}]}`) +
		oaData(`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"1}"}}]}}]}`) + oaFinish("tool_calls")
	srv := sseServer(t, body, false, nil)
	ch, _ := NewOpenAI(Config{BaseURL: srv.URL}).Stream(bg, simpleReq)
	evs := collect(t, ch)
	terminal(t, evs)
	want := []ToolCall{{"a", "one", "{}"}, {"b", "two", `{"x":1}`}}
	if got := toolCalls(evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestOpenAINoDoneAfterFinish(t *testing.T) {
	srv := sseServer(t, oaText("a")+oaData(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`), false, nil)
	ch, _ := NewOpenAI(Config{BaseURL: srv.URL}).Stream(bg, simpleReq)
	evs := collect(t, ch)
	if terminal(t, evs).Kind != EventDone {
		t.Fatalf("%+v", evs)
	}
}

func TestTruncatedStreamIsError(t *testing.T) {
	bodies := []string{oaText("a"), anStart() + anTextBlock(0, "a"), rsText("a")}
	for i, b := range bodies {
		srv := sseServer(t, b, false, nil)
		ch, _ := protos[i].provider(srv).Stream(bg, simpleReq)
		evs := collect(t, ch)
		if terminal(t, evs).Kind != EventError {
			t.Fatalf("%s: %+v", protos[i].name, evs)
		}
	}
}

func TestInvalidToolJSON(t *testing.T) {
	bodies := []string{
		oaToolChunks(0, "a", "t", `{"x":`, 2) + oaFinish("tool_calls"),
		anStart() + anToolBlock(0, "a", "t", `{"x":`, 2) + anEnd("tool_use"),
		oaToolChunks(0, "a", "t", `[1]`, 2) + oaFinish("tool_calls"),
		anStart() + anToolBlock(0, "a", "t", `"str"`, 2) + anEnd("tool_use"),
		rsToolItem(0, "a", "t", `{"x":`, 2) + rsCompleted(),
		rsToolItem(0, "a", "t", `[1]`, 2) + rsCompleted(),
	}
	owner := []int{0, 1, 0, 1, 2, 2}
	for i, b := range bodies {
		srv := sseServer(t, b, false, nil)
		ch, _ := protos[owner[i]].provider(srv).Stream(bg, simpleReq)
		evs := collect(t, ch)
		if terminal(t, evs).Kind != EventError || len(toolCalls(evs)) != 0 {
			t.Fatalf("case %d: %+v", i, evs)
		}
	}
}

func TestProviderErrorEvents(t *testing.T) {
	bodies := []string{
		oaText("a") + oaData(`{"error":{"message":"overloaded `+testKey+`"}}`),
		anStart() + anEv("error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded `+testKey+`"}}`),
		rsText("a") + rsEv("error", `"code":"server_error","message":"overloaded `+testKey+`"`),
	}
	for i, b := range bodies {
		srv := sseServer(t, b, false, nil)
		ch, _ := protos[i].provider(srv).Stream(bg, simpleReq)
		evs := collect(t, ch)
		last := terminal(t, evs)
		if last.Kind != EventError || !strings.Contains(last.Err.Error(), "verloaded") || strings.Contains(last.Err.Error(), testKey) {
			t.Fatalf("%s: %+v", protos[i].name, last)
		}
	}
}

func TestHTTPErrorsScrubKey(t *testing.T) {
	for _, p := range protos {
		for _, code := range []int{401, 429, 500} {
			t.Run(fmt.Sprintf("%s/%d", p.name, code), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(code)
					fmt.Fprintf(w, `{"error":{"message":"Incorrect API key provided: %s.\n\tBearer %s"}}`, testKey, testKey)
					w.Write([]byte(strings.Repeat("x", 100000)))
				}))
				defer srv.Close()
				prov := p.provider(srv)
				_, err := prov.Stream(bg, simpleReq)
				_, err2 := prov.Models(bg)
				for _, e := range []error{err, err2} {
					he, ok := e.(*HTTPError)
					if !ok || he.StatusCode != code {
						t.Fatalf("got %T %v", e, e)
					}
					msg := e.Error()
					if strings.Contains(msg, testKey) || !strings.Contains(msg, fmt.Sprint(code)) || len(msg) > 700 || strings.ContainsAny(msg, "\n\t") {
						t.Fatalf("bad error %q", msg)
					}
				}
			})
		}
	}
}

func TestConnectionErrorHasNoKey(t *testing.T) {
	for _, p := range protos {
		prov := p.new(Config{BaseURL: "http://127.0.0.1:1", APIKey: testKey})
		_, err := prov.Stream(bg, simpleReq)
		if err == nil || strings.Contains(err.Error(), testKey) {
			t.Fatalf("%v", err)
		}
	}
}

func TestMidStreamDisconnect(t *testing.T) {
	for _, p := range protos {
		t.Run(p.name, func(t *testing.T) {
			full := p.text("hello")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte(full[:len(full)/2]))
				w.(http.Flusher).Flush()
				panic(http.ErrAbortHandler)
			}))
			defer srv.Close()
			ch, err := p.provider(srv).Stream(bg, simpleReq)
			if err != nil {
				t.Fatal(err)
			}
			evs := collect(t, ch)
			if last := terminal(t, evs); last.Kind != EventError || last.Err == nil {
				t.Fatalf("%+v", evs)
			}
		})
	}
}

// blockingServer sends head then blocks until the client goes away.
func blockingServer(t *testing.T, head string, gone chan<- struct{}) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readAll(r) // lets the server notice a closed connection
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(head))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(gone)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestContextCancelMidStream(t *testing.T) {
	for _, p := range protos {
		t.Run(p.name, func(t *testing.T) {
			base := runtimeGoroutines()
			gone := make(chan struct{})
			srv := blockingServer(t, p.head, gone)
			ctx, cancel := context.WithCancel(bg)
			ch, err := p.provider(srv).Stream(ctx, simpleReq)
			if err != nil {
				t.Fatal(err)
			}
			if ev := <-ch; ev.Kind != EventText {
				t.Fatalf("%+v", ev)
			}
			cancel()
			start := time.Now()
			evs := collect(t, ch)
			if time.Since(start) > 2*time.Second {
				t.Fatal("cancel was not prompt")
			}
			for _, e := range evs {
				if e.Kind == EventDone || e.Kind == EventError {
					t.Fatalf("terminal event after cancel: %+v", e)
				}
			}
			select {
			case <-gone:
			case <-time.After(5 * time.Second):
				t.Fatal("server never saw the disconnect")
			}
			srv.Close()
			waitGoroutines(t, base)
		})
	}
}

// A consumer that stops reading must not pin the goroutine once ctx ends.
func TestAbandonedConsumerDoesNotLeak(t *testing.T) {
	base := runtimeGoroutines()
	srv := sseServer(t, oaText(strings.Split(strings.Repeat("x", 200), "")...)+oaFinish("stop"), false, nil)
	ctx, cancel := context.WithCancel(bg)
	ch, err := NewOpenAI(Config{BaseURL: srv.URL, Client: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}}).Stream(ctx, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	<-ch // read one, then abandon with the buffer full
	time.Sleep(100 * time.Millisecond)
	cancel()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				srv.Close()
				waitGoroutines(t, base)
				return
			}
		case <-deadline:
			t.Fatal("channel never closed")
		}
	}
}

func TestSlowConsumer(t *testing.T) {
	parts := strings.Split(strings.Repeat("ab", 60), "")
	srv := sseServer(t, oaText(parts...)+oaFinish("stop"), false, nil)
	ch, _ := NewOpenAI(Config{BaseURL: srv.URL}).Stream(bg, simpleReq)
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
		if len(evs)%20 == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	terminal(t, evs)
	if text(evs, EventText) != strings.Repeat("ab", 60) {
		t.Fatalf("lost text: %q", text(evs, EventText))
	}
}

func TestOversizedLineAndTotal(t *testing.T) {
	big := strings.Repeat("a", 2<<20)
	for _, p := range protos {
		t.Run(p.name+"/line", func(t *testing.T) {
			srv := sseServer(t, "data: "+big+"\n\n", false, nil)
			ch, _ := p.provider(srv).Stream(bg, simpleReq)
			evs := collect(t, ch)
			if terminal(t, evs).Kind != EventError {
				t.Fatalf("%+v", evs)
			}
		})
		t.Run(p.name+"/total", func(t *testing.T) {
			srv := sseServer(t, p.text(strings.Split(strings.Repeat("y", 500), "")...), false, nil)
			ch, _ := p.provider(srv, func(c *Config) { c.MaxResponseBytes = 2000 }).Stream(bg, simpleReq)
			evs := collect(t, ch)
			last := terminal(t, evs)
			if last.Kind != EventError || !strings.Contains(last.Err.Error(), "size limit") {
				t.Fatalf("%+v", last)
			}
		})
	}
}

func TestMalformedChunk(t *testing.T) {
	for i, b := range []string{oaData("{not json"), anStart() + anEv("content_block_delta", "{oops"), oaData("{not json")} {
		srv := sseServer(t, b, false, nil)
		ch, _ := protos[i].provider(srv).Stream(bg, simpleReq)
		if terminal(t, collect(t, ch)).Kind != EventError {
			t.Fatal("want error")
		}
	}
}

func TestBaseURLNormalisation(t *testing.T) {
	oa := []struct{ in, want string }{
		{"", "https://api.openai.com/v1"},
		{"  https://api.openai.com/v1/ ", "https://api.openai.com/v1"},
		{"https://api.openai.com", "https://api.openai.com/v1"},
		{"https://api.openai.com/", "https://api.openai.com/v1"},
		{"http://localhost:11434/v1///", "http://localhost:11434/v1"},
		{"https://api.deepseek.com/v1/chat/completions", "https://api.deepseek.com/v1"},
		{"https://open.bigmodel.cn/api/paas/v4/", "https://open.bigmodel.cn/api/paas/v4"},
		{"https://x.test/openai?key=1#f", "https://x.test/openai"},
		{"https://api.openai.com/v1/responses", "https://api.openai.com/v1"},
		{"https://gw.test/api/Responses/", "https://gw.test/api"},
		{"https://gw.test/responses", "https://gw.test/v1"},
	}
	for _, c := range oa {
		got, err := openAIBase(c.in)
		if err != nil || got != c.want {
			t.Errorf("openAIBase(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	an := []struct{ in, want string }{
		{"", "https://api.anthropic.com"},
		{"https://api.anthropic.com/", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/", "https://api.anthropic.com"},
		{"https://api.anthropic.com/v1/messages", "https://api.anthropic.com"},
		{"https://proxy.test/anthropic//", "https://proxy.test/anthropic"},
		{"https://proxy.test/anthropic/v1", "https://proxy.test/anthropic"},
	}
	for _, c := range an {
		got, err := anthropicBase(c.in)
		if err != nil || got != c.want {
			t.Errorf("anthropicBase(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	oaModels := []struct{ in, want string }{
		{"", ""},
		{"https://opencode.ai/inference/go/v1", "https://opencode.ai/inference/go/v1"},
		{"https://opencode.ai/inference/go/v1/models", "https://opencode.ai/inference/go/v1"},
		{"https://opencode.ai/inference/go/v1/models/", "https://opencode.ai/inference/go/v1"},
		{"https://api.openai.com", "https://api.openai.com/v1"},
	}
	for _, c := range oaModels {
		got, err := openAIModelsBase(c.in)
		if err != nil || got != c.want {
			t.Errorf("openAIModelsBase(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"ftp://x", "not a url", "http://", "://x"} {
		if _, err := openAIBase(bad); err == nil {
			t.Errorf("openAIBase(%q) should fail", bad)
		}
		if _, err := NewAnthropic(Config{BaseURL: bad}).Stream(bg, simpleReq); err == nil {
			t.Errorf("anthropic Stream with base %q should fail", bad)
		}
	}
}

func TestEndpointPaths(t *testing.T) {
	for _, p := range protos {
		for _, suffix := range []string{"", "/", "/v1", "/v1/"} {
			var path atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path.Store(r.URL.Path)
				w.Write([]byte(p.text("x")))
			}))
			prov := p.new(Config{BaseURL: srv.URL + suffix})
			ch, err := prov.Stream(bg, simpleReq)
			if err != nil {
				t.Fatal(err)
			}
			collect(t, ch)
			srv.Close()
			want := map[string]string{"openai": "/v1/chat/completions", "anthropic": "/v1/messages", "openai-responses": "/v1/responses"}[p.name]
			if path.Load() != want {
				t.Errorf("%s base %q: path %v want %s", p.name, suffix, path.Load(), want)
			}
		}
	}
}

func TestModelsURLSeparateFromBaseURL(t *testing.T) {
	var chatCalled, modelsCalled bool
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatCalled = true
		if r.URL.Path != "/inference/go/openai/v1/chat/completions" {
			t.Errorf("chat path = %q", r.URL.Path)
		}
		w.Write([]byte(protos[0].text("hello")))
	}))
	defer chatSrv.Close()

	modelsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelsCalled = true
		if r.URL.Path != "/inference/go/v1/models" {
			t.Errorf("models path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer modelsSrv.Close()

	prov := NewOpenAI(Config{
		BaseURL:   chatSrv.URL + "/inference/go/openai/v1",
		ModelsURL: modelsSrv.URL + "/inference/go/v1",
	})

	list, err := prov.Models(bg)
	if err != nil || len(list) != 1 || list[0] != "gpt-4o" {
		t.Fatalf("Models() = %v, %v; want [gpt-4o]", list, err)
	}
	if !modelsCalled {
		t.Fatal("models server was not called")
	}

	ch, err := prov.Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if !chatCalled {
		t.Fatal("chat server was not called")
	}
}

func jsonEq(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got invalid JSON: %v\n%s", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}

var goldenReq = func() Request {
	temp := 0.25
	return Request{
		Model:       "the-model",
		System:      "be brief",
		MaxTokens:   256,
		Temperature: &temp,
		Tools: []ToolSpec{
			{Name: "read", Description: "read a file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)},
			{Name: "noargs"},
		},
		Messages: []Message{
			{Role: RoleUser, Text: "look at two files"},
			{Role: RoleAssistant, Text: "ok", ToolCalls: []ToolCall{
				{ID: "c1", Name: "read", Arguments: `{"path":"a"}`},
				{ID: "c2", Name: "noargs"},
			}},
			{Role: RoleTool, ToolCallID: "c1", Text: "AAA"},
			{Role: RoleTool, ToolCallID: "c2", Text: "BBB"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c3", Name: "read", Arguments: `{"path":"b"}`}}},
			{Role: RoleTool, ToolCallID: "c3", Text: "CCC"},
			{Role: RoleUser, Text: "thanks"},
		},
	}
}()

func TestOpenAIRequestGolden(t *testing.T) {
	var got []byte
	var hdr http.Header
	srv := sseServer(t, oaFinish("stop"), false, func(r *http.Request, b []byte) { got, hdr = b, r.Header.Clone() })
	ch, err := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, goldenReq)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if hdr.Get("Authorization") != "Bearer "+testKey || hdr.Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", hdr)
	}
	jsonEq(t, got, `{
	"model":"the-model","stream":true,"stream_options":{"include_usage":true},"max_tokens":256,"temperature":0.25,
	"messages":[
	 {"role":"system","content":"be brief"},
	 {"role":"user","content":"look at two files"},
	 {"role":"assistant","content":"ok","tool_calls":[
	   {"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a\"}"}},
	   {"id":"c2","type":"function","function":{"name":"noargs","arguments":"{}"}}]},
	 {"role":"tool","content":"AAA","tool_call_id":"c1"},
	 {"role":"tool","content":"BBB","tool_call_id":"c2"},
	 {"role":"assistant","content":null,"tool_calls":[{"id":"c3","type":"function","function":{"name":"read","arguments":"{\"path\":\"b\"}"}}]},
	 {"role":"tool","content":"CCC","tool_call_id":"c3"},
	 {"role":"user","content":"thanks"}],
	"tools":[
	 {"type":"function","function":{"name":"read","description":"read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}},
	 {"type":"function","function":{"name":"noargs","parameters":{"type":"object","properties":{}}}}]
	}`)
}

func TestAnthropicRequestGolden(t *testing.T) {
	var got []byte
	var hdr http.Header
	srv := sseServer(t, anStart()+anEnd("end_turn"), false, func(r *http.Request, b []byte) { got, hdr = b, r.Header.Clone() })
	ch, err := NewAnthropic(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, goldenReq)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if hdr.Get("x-api-key") != testKey || hdr.Get("anthropic-version") != "2023-06-01" || hdr.Get("Authorization") != "" {
		t.Fatalf("headers %v", hdr)
	}
	jsonEq(t, got, `{
	"model":"the-model","stream":true,"max_tokens":256,"temperature":0.25,"system":[{"type":"text","text":"be brief","cache_control":{"type":"ephemeral"}}],
	"messages":[
	 {"role":"user","content":[{"type":"text","text":"look at two files"}]},
	 {"role":"assistant","content":[{"type":"text","text":"ok"},
	   {"type":"tool_use","id":"c1","name":"read","input":{"path":"a"}},
	   {"type":"tool_use","id":"c2","name":"noargs","input":{}}]},
	 {"role":"user","content":[
	   {"type":"tool_result","tool_use_id":"c1","content":"AAA"},
	   {"type":"tool_result","tool_use_id":"c2","content":"BBB"}]},
	 {"role":"assistant","content":[{"type":"tool_use","id":"c3","name":"read","input":{"path":"b"}}]},
	 {"role":"user","content":[
	   {"type":"tool_result","tool_use_id":"c3","content":"CCC"},
	   {"type":"text","text":"thanks","cache_control":{"type":"ephemeral"}}]}],
	"tools":[
	 {"name":"read","description":"read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}},
	 {"name":"noargs","input_schema":{"type":"object","properties":{}},"cache_control":{"type":"ephemeral"}}]
	}`)
}

func TestAnthropicBearerAuthentication(t *testing.T) {
	var hdr http.Header
	srv := sseServer(t, anStart()+anEnd("end_turn"), false, func(r *http.Request, _ []byte) { hdr = r.Header.Clone() })
	ch, err := NewAnthropic(Config{BaseURL: srv.URL, APIKey: testKey, AuthStyle: AuthStyleBearer}).Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if hdr.Get("Authorization") != "Bearer "+testKey || hdr.Get("x-api-key") != "" {
		t.Fatalf("headers %v", hdr)
	}
	if hdr.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("version header %v", hdr)
	}
}

func TestRequestValidation(t *testing.T) {
	bad := []Request{
		{Messages: simpleReq.Messages},
		{Model: "m"},
		{Model: "m", Messages: []Message{{Role: RoleTool, Text: "x"}}},
		{Model: "m", Messages: []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "1", Name: "t", Arguments: "[1]"}}}}},
		{Model: "m", Messages: simpleReq.Messages, Tools: []ToolSpec{{Name: "t", Schema: json.RawMessage(`[`)}}},
		{Model: "m", Messages: []Message{{Role: "weird", Text: "x"}}},
	}
	for i, r := range bad {
		for _, p := range protos {
			if _, err := p.new(Config{BaseURL: "http://127.0.0.1:1"}).Stream(bg, r); err == nil || strings.Contains(err.Error(), "connect") {
				t.Errorf("%s case %d: want validation error, got %v", p.name, i, err)
			}
		}
	}
}

func TestModels(t *testing.T) {
	var gotHdr http.Header
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHdr, gotPath = r.Header.Clone(), r.URL.Path
		w.Write([]byte(`{"data":[{"id":"zeta"},{"id":"alpha"},{"id":"zeta"},{"id":""},{"id":"mid"}],"has_more":false}`))
	}))
	defer srv.Close()
	for _, p := range protos {
		ids, err := p.provider(srv).Models(bg)
		if err != nil || !reflect.DeepEqual(ids, []string{"alpha", "mid", "zeta"}) {
			t.Fatalf("%s: %v %v", p.name, ids, err)
		}
		if p.name != "anthropic" && (gotPath != "/v1/models" || gotHdr.Get("Authorization") != "Bearer "+testKey) {
			t.Fatalf("openai %s %v", gotPath, gotHdr)
		}
		if p.name == "anthropic" && (gotPath != "/v1/models" || gotHdr.Get("x-api-key") != testKey || gotHdr.Get("anthropic-version") != "2023-06-01") {
			t.Fatalf("anthropic %s %v", gotPath, gotHdr)
		}
	}
}

func TestAnthropicModelsPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after_id") == "" {
			w.Write([]byte(`{"data":[{"id":"b"}],"has_more":true,"last_id":"b"}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"a"}],"has_more":false,"last_id":"a"}`))
	}))
	defer srv.Close()
	ids, err := NewAnthropic(Config{BaseURL: srv.URL}).Models(bg)
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestModelsBoundedAndMalformed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Header.Get("X-Big") == "1" {
			w.Write([]byte(`{"data":[{"id":"` + strings.Repeat("a", 5<<20) + `"}]}`))
			return
		}
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := NewOpenAI(Config{BaseURL: srv.URL}).Models(bg); err == nil {
		t.Fatal("want malformed error")
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"` + strings.Repeat("a", 5<<20) + `"}]}`))
	}))
	defer big.Close()
	if _, err := NewAnthropic(Config{BaseURL: big.URL}).Models(bg); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("got %v", err)
	}
}

func TestDefaultClientHasNoOverallTimeout(t *testing.T) {
	c := defaultClient()
	if c.Timeout != 0 {
		t.Fatal("default client must not set an overall timeout")
	}
	tr := c.Transport.(*http.Transport)
	if tr.ResponseHeaderTimeout == 0 || tr.TLSHandshakeTimeout == 0 {
		t.Fatal("missing header/TLS timeouts")
	}
}

func splitRunes(s string, n int) []string {
	var out []string
	r := []rune(s)
	for i := 0; i < len(r); i += n {
		out = append(out, string(r[i:min(i+n, len(r))]))
	}
	return out
}

// The OpenAI handler reuses its chunk struct between events. A field absent
// from the next event must not survive from the previous one.
func TestOpenAIReusedChunkDoesNotLeakFields(t *testing.T) {
	body := oaData(`{"choices":[{"delta":{"content":"a","tool_calls":[{"index":0,"id":"call","function":{"name":"run","arguments":"{}"}}]}}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`) +
		oaData(`{"choices":[{"delta":{"content":"b"},"finish_reason":"stop"}]}`) + oaData("[DONE]")
	ch, err := NewOpenAI(Config{BaseURL: sseServer(t, body, false, nil).URL}).Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	done := terminal(t, evs)
	if got := text(evs, EventText); got != "ab" {
		t.Fatalf("text = %q", got)
	}
	if calls := toolCalls(evs); len(calls) != 1 || calls[0].ID != "call" {
		t.Fatalf("tool calls = %+v", calls)
	}
	if done.Usage != (Usage{InputTokens: 5, OutputTokens: 1}) || done.StopReason != "stop" {
		t.Fatalf("done = %+v", done)
	}
}

func TestAnthropicUsageCountsCachedTokens(t *testing.T) {
	// Anthropic reports cached tokens apart from input_tokens; the request's
	// prompt is all three.
	body := anEv("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":4,"cache_creation_input_tokens":100,"cache_read_input_tokens":2000,"output_tokens":1}}}`) +
		anTextBlock(0, "hi") + anEnd("end_turn")
	srv := sseServer(t, body, false, nil)
	ch, err := NewAnthropic(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	done := terminal(t, collect(t, ch))
	want := Usage{InputTokens: 2104, OutputTokens: 9, CacheReadTokens: 2000, CacheWriteTokens: 100}
	if done.Usage != want {
		t.Fatalf("usage = %+v, want %+v", done.Usage, want)
	}
}

func TestOpenAIUsageReportsCachedTokens(t *testing.T) {
	for name, usage := range map[string]string{
		"nested":   `{"prompt_tokens":3000,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2816}}`,
		"deepseek": `{"prompt_tokens":3000,"completion_tokens":3,"prompt_cache_hit_tokens":2816,"prompt_cache_miss_tokens":184}`,
		"flat":     `{"prompt_tokens":3000,"completion_tokens":3,"cached_tokens":2816}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := oaText("hi") + oaData(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`) +
				oaData(`{"choices":[],"usage":`+usage+`}`) + oaData("[DONE]")
			srv := sseServer(t, body, false, nil)
			ch, err := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, simpleReq)
			if err != nil {
				t.Fatal(err)
			}
			want := Usage{InputTokens: 3000, OutputTokens: 3, CacheReadTokens: 2816}
			if done := terminal(t, collect(t, ch)); done.Usage != want {
				t.Fatalf("usage = %+v, want %+v", done.Usage, want)
			}
		})
	}
}

func TestOpenAIResponsesRequestGolden(t *testing.T) {
	var got []byte
	var hdr http.Header
	var path string
	srv := sseServer(t, rsCompleted(), false, func(r *http.Request, b []byte) { got, hdr, path = b, r.Header.Clone(), r.URL.Path })
	ch, err := NewOpenAIResponses(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, goldenReq)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if path != "/v1/responses" || hdr.Get("Authorization") != "Bearer "+testKey || hdr.Get("Content-Type") != "application/json" {
		t.Fatalf("%s %v", path, hdr)
	}
	jsonEq(t, got, `{
	"model":"the-model","stream":true,"store":false,"max_output_tokens":256,"temperature":0.25,
	"instructions":"be brief",
	"input":[
	 {"role":"user","content":"look at two files"},
	 {"role":"assistant","content":"ok"},
	 {"type":"function_call","call_id":"c1","name":"read","arguments":"{\"path\":\"a\"}"},
	 {"type":"function_call","call_id":"c2","name":"noargs","arguments":"{}"},
	 {"type":"function_call_output","call_id":"c1","output":"AAA"},
	 {"type":"function_call_output","call_id":"c2","output":"BBB"},
	 {"type":"function_call","call_id":"c3","name":"read","arguments":"{\"path\":\"b\"}"},
	 {"type":"function_call_output","call_id":"c3","output":"CCC"},
	 {"role":"user","content":"thanks"}],
	"tools":[
	 {"type":"function","name":"read","description":"read a file","strict":false,"parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}},
	 {"type":"function","name":"noargs","strict":false,"parameters":{"type":"object","properties":{}}}]
	}`)
}

func TestOpenAIResponsesStreams(t *testing.T) {
	item := func(typ string, idx int, fields string) string {
		return rsEv(typ, fmt.Sprintf(`"output_index":%d,"item":{"type":"function_call",%s}`, idx, fields))
	}
	cases := []struct {
		name   string
		body   string
		text   string
		reason string
		usage  Usage
		calls  []ToolCall
		errMsg string
	}{
		{name: "reasoning and answer",
			body: rsEv("response.reasoning_summary_text.delta", `"delta":"think "`) +
				rsEv("response.reasoning_text.delta", `"delta":"hard"`) + rsText("answer") + rsCompleted(),
			text: "answer", reason: "stop", usage: Usage{InputTokens: 7, OutputTokens: 3}},
		{name: "cached input tokens",
			body: rsText("x") + rsEv("response.completed", `"response":{"usage":{"input_tokens":100,"output_tokens":5,"input_tokens_details":{"cached_tokens":64}}}`),
			text: "x", reason: "stop", usage: Usage{InputTokens: 100, OutputTokens: 5, CacheReadTokens: 64}},
		{name: "cache count never exceeds input",
			body:   rsEv("response.completed", `"response":{"usage":{"input_tokens":10,"output_tokens":1,"input_tokens_details":{"cached_tokens":99}}}`),
			reason: "stop", usage: Usage{InputTokens: 10, OutputTokens: 1, CacheReadTokens: 10}},
		{name: "cut off by the output limit",
			body: rsText("par") + rsEv("response.incomplete", `"response":{"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":4,"output_tokens":2}}`),
			text: "par", reason: "length", usage: Usage{InputTokens: 4, OutputTokens: 2}},
		{name: "other incomplete reason is kept",
			body:   rsEv("response.incomplete", `"response":{"incomplete_details":{"reason":"content_filter"}}`),
			reason: "content_filter"},
		{name: "refusal is shown as text",
			body: rsEv("response.refusal.delta", `"delta":"no"`) + rsCompleted(), text: "no", reason: "stop", usage: Usage{InputTokens: 7, OutputTokens: 3}},
		{name: "arguments only on the done item",
			body: item("response.output_item.added", 0, `"id":"fc_1","call_id":"c1","name":"run"`) +
				item("response.output_item.done", 0, `"id":"fc_1","call_id":"c1","name":"run","arguments":"{\"a\":1}"`) + rsCompleted(),
			reason: "tool_calls", usage: Usage{InputTokens: 7, OutputTokens: 3}, calls: []ToolCall{{"c1", "run", `{"a":1}`}}},
		{name: "arguments only as deltas, flushed at completion",
			body: item("response.output_item.added", 0, `"id":"fc_1","call_id":"c1","name":"run"`) +
				rsEv("response.function_call_arguments.delta", `"item_id":"fc_1","delta":"{\"a\""`) +
				rsEv("response.function_call_arguments.delta", `"item_id":"fc_1","delta":":2}"`) + rsCompleted(),
			reason: "tool_calls", usage: Usage{InputTokens: 7, OutputTokens: 3}, calls: []ToolCall{{"c1", "run", `{"a":2}`}}},
		{name: "items named by position only",
			body: item("response.output_item.added", 1, `"call_id":"c2","name":"two"`) +
				item("response.output_item.added", 0, `"call_id":"c1","name":"one"`) +
				rsEv("response.function_call_arguments.delta", `"output_index":1,"delta":"{\"k\":1}"`) +
				item("response.output_item.done", 0, `"call_id":"c1","name":"one","arguments":""`) +
				item("response.output_item.done", 1, `"call_id":"c2","name":"two","arguments":""`) + rsCompleted(),
			reason: "tool_calls", usage: Usage{InputTokens: 7, OutputTokens: 3},
			calls: []ToolCall{{"c1", "one", "{}"}, {"c2", "two", `{"k":1}`}}},
		{name: "failed response",
			body: rsText("a") + rsEv("response.failed", `"response":{"error":{"code":"x","message":"model overloaded"}}`), text: "a", errMsg: "model overloaded"},
		{name: "error without a message",
			body: rsEv("error", `"code":"boom"`), errMsg: "provider error: unspecified error"},
		{name: "untyped error object",
			body: oaData(`{"error":{"message":"quota exceeded"}}`), errMsg: "quota exceeded"},
	}
	for _, c := range cases {
		for _, bytewise := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/bytewise=%v", c.name, bytewise), func(t *testing.T) {
				srv := sseServer(t, c.body, bytewise, nil)
				ch, err := NewOpenAIResponses(Config{BaseURL: srv.URL}).Stream(bg, simpleReq)
				if err != nil {
					t.Fatal(err)
				}
				evs := collect(t, ch)
				last := terminal(t, evs)
				if c.errMsg != "" {
					if last.Kind != EventError || !strings.Contains(last.Err.Error(), c.errMsg) {
						t.Fatalf("want error %q, got %+v", c.errMsg, evs)
					}
					return
				}
				if last.Kind != EventDone || last.StopReason != c.reason || last.Usage != c.usage {
					t.Fatalf("done %+v", last)
				}
				if text(evs, EventText) != c.text {
					t.Fatalf("text %q", text(evs, EventText))
				}
				if got := toolCalls(evs); !reflect.DeepEqual(got, c.calls) {
					t.Fatalf("calls %+v", got)
				}
				if c.name == "reasoning and answer" && text(evs, EventReasoning) != "think hard" {
					t.Fatalf("reasoning %q", text(evs, EventReasoning))
				}
			})
		}
	}
}

func TestOpenAIResponsesIgnoresChatCompletionsStream(t *testing.T) {
	// A server that answers the Responses route in Chat Completions framing
	// produces no completion event, so it must surface as an error rather
	// than as an empty answer.
	srv := sseServer(t, oaText("hi")+oaFinish("stop"), false, nil)
	ch, _ := NewOpenAIResponses(Config{BaseURL: srv.URL}).Stream(bg, simpleReq)
	if terminal(t, collect(t, ch)).Kind != EventError {
		t.Fatal("want error")
	}
}

func TestAnthropicStreamKeepsThinkingBlocks(t *testing.T) {
	h := &anHandler{}
	var got []Event
	events := make(chan Event, 16)
	em := &emitter{ctx: context.Background(), ch: events}
	send := func(name, data string) {
		if _, err := h.event(sseEvent{Name: name, Data: []byte(data)}, em); err != nil {
			t.Fatal(err)
		}
	}
	send("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
	send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"a"}}`)
	send("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}`)
	send("content_block_stop", `{"type":"content_block_stop","index":0}`)
	send("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque"}}`)
	send("content_block_stop", `{"type":"content_block_stop","index":1}`)
	close(events)
	for ev := range events {
		got = append(got, ev)
	}
	var blocks []ThinkingBlock
	for _, ev := range got {
		if ev.Kind == EventThinking {
			blocks = append(blocks, *ev.Thinking)
		}
	}
	if len(blocks) != 2 || blocks[0] != (ThinkingBlock{Text: "a", Signature: "sig"}) || blocks[1] != (ThinkingBlock{Redacted: true, Data: "opaque"}) {
		t.Fatalf("thinking blocks = %+v", blocks)
	}
}
