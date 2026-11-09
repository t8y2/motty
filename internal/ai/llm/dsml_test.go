package llm

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The V4.1 "calls" dialect, the shape issue #225 shows.
const dsmlCalls = "<｜DSML｜ calls>\n" +
	"<｜DSML｜ invoke name=\"terminal_exec\">\n" +
	"<｜DSML｜ parameter name=\"command\" string=\"true\">uptime</｜DSML｜ parameter>\n" +
	"<｜DSML｜ parameter name=\"timeout_seconds\" string=\"false\">30</｜DSML｜ parameter>\n" +
	"</｜DSML｜ invoke>\n" +
	"</｜DSML｜ calls>"

var dsmlWant = []ToolCall{{Name: "terminal_exec", Arguments: `{"command":"uptime","timeout_seconds":30}`}}

// spacedClosers writes the closing tags the way a serving lane may: a space
// between "<" and "/" and between the bars. The scanner has always accepted
// this shape; the recovery must read the same grammar.
func spacedClosers(markup string) string {
	return strings.ReplaceAll(markup, "</｜DSML｜", "< / ｜ ｜ DSML ｜ ｜")
}

// feedDSML pushes chunks through a filter and returns what it published.
func feedDSML(t *testing.T, recover bool, chunks ...string) (string, []ToolCall, error) {
	t.Helper()
	f := newDSMLFilter(true, recover)
	var (
		b     strings.Builder
		calls []ToolCall
	)
	for _, chunk := range chunks {
		text, got, err := f.push(chunk)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(text)
		calls = append(calls, got...)
	}
	text, got, err := f.flush()
	b.WriteString(text)
	calls = append(calls, got...)
	return b.String(), calls, err
}

func TestDSMLRecoversEveryDialect(t *testing.T) {
	cases := []struct {
		name   string
		markup string
		want   []ToolCall
	}{
		{"v41-calls", dsmlCalls, dsmlWant},
		{"v32-function_calls", strings.ReplaceAll(dsmlCalls, "calls>", "function_calls>"), dsmlWant},
		{"v4-tool_calls", strings.ReplaceAll(dsmlCalls, "calls>", "tool_calls>"), dsmlWant},
		{"ascii-bars", strings.ReplaceAll(dsmlCalls, "｜", "|"), dsmlWant},
		{"doubled-bars", strings.ReplaceAll(dsmlCalls, "｜", "||"), dsmlWant},
		{"spaced-bars", strings.ReplaceAll(dsmlCalls, "｜", " | "), dsmlWant},
		{"spaced-closers", spacedClosers(dsmlCalls), dsmlWant},
		{"spaced-tags", spacedClosers(strings.ReplaceAll(dsmlCalls, "<｜DSML｜", "< ｜ ｜ DSML ｜ ｜")), dsmlWant},
		{"no-wrapper", "<｜DSML｜ invoke name=\"terminal_exec\">" +
			"<｜DSML｜ parameter name=\"command\" string=\"true\">uptime</｜DSML｜ parameter>" +
			"<｜DSML｜ parameter name=\"timeout_seconds\" string=\"false\">30</｜DSML｜ parameter>" +
			"</｜DSML｜ invoke>", dsmlWant},
		{"bare-json", "<｜DSML｜ calls><｜DSML｜ invoke name=\"terminal_exec\">" +
			`{"command":"uptime"}</｜DSML｜ invoke></｜DSML｜ calls>`,
			[]ToolCall{{Name: "terminal_exec", Arguments: `{"command":"uptime"}`}}},
		{"truncated-block", "<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"terminal_exec\">\n" +
			"<｜DSML｜ parameter name=\"command\" string=\"true\">uptime</｜DSML｜ parameter>\n" +
			"<｜DSML｜ parameter name=\"timeout_seconds\" string=\"false\">30</｜DSML｜ parameter>\n" +
			"</｜DSML｜ invoke>\n", dsmlWant},
		{"prose-around", "Running it.\n" + dsmlCalls + "\nThen I will read the output.", dsmlWant},
		{"stray-tags", "<｜DSML｜ caller>hello</｜DSML｜ caller>", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, calls, err := feedDSML(t, true, c.markup)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, c.want) {
				t.Fatalf("calls = %#v, want %#v", calls, c.want)
			}
			if strings.Contains(text, "DSML") {
				t.Fatalf("markup reached the text channel: %q", text)
			}
			if c.name == "prose-around" && (!strings.HasPrefix(text, "Running it.") || !strings.Contains(text, "Then I will read")) {
				t.Fatalf("prose lost: %q", text)
			}
			if c.name == "stray-tags" && text != "hello" {
				t.Fatalf("text = %q, want %q", text, "hello")
			}
		})
	}
}

// A delta boundary can fall anywhere, including inside a bar or an opener.
func TestDSMLRecoversAcrossAnyDeltaBoundary(t *testing.T) {
	for name, markup := range map[string]string{
		"canonical": dsmlCalls,
		"spaced":    spacedClosers(dsmlCalls),
	} {
		t.Run(name, func(t *testing.T) {
			wantText, wantCalls, err := feedDSML(t, true, markup)
			if err != nil {
				t.Fatal(err)
			}
			for cut := 1; cut < len(markup); cut++ {
				text, calls, err := feedDSML(t, true, markup[:cut], markup[cut:])
				if err != nil {
					t.Fatalf("cut %d: %v", cut, err)
				}
				if !reflect.DeepEqual(calls, wantCalls) || text != wantText {
					t.Fatalf("cut %d: text %q calls %#v", cut, text, calls)
				}
			}
			var chunks []string
			for _, r := range markup {
				chunks = append(chunks, string(r))
			}
			text, calls, err := feedDSML(t, true, chunks...)
			if err != nil {
				t.Fatalf("bytewise: %v", err)
			}
			if !reflect.DeepEqual(calls, wantCalls) || text != wantText {
				t.Fatalf("bytewise: text %q calls %#v", text, calls)
			}
		})
	}
}

func TestDSMLPassesOrdinaryTextThrough(t *testing.T) {
	for _, text := range []string{
		"",
		"No markup here.",
		"a < b and c > d",
		"use <div> or <|> as separators",
		strings.Repeat("x", 4096),
	} {
		got, calls, err := feedDSML(t, true, text)
		if err != nil || got != text || calls != nil {
			t.Fatalf("%q: got %q %#v %v", text, got, calls, err)
		}
	}
	// The same text split at every boundary must survive too.
	long := "if a < b then print(a < b); no tag follows this prefix <"
	for cut := 0; cut <= len(long); cut++ {
		got, _, err := feedDSML(t, true, long[:cut], long[cut:])
		if err != nil || got != long {
			t.Fatalf("cut %d: got %q err %v", cut, got, err)
		}
	}
}

func TestDSMLMarkupThatYieldsNoCallIsAnError(t *testing.T) {
	_, _, err := feedDSML(t, true, "<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"x\">\n</｜DSML｜ calls>")
	if err == nil {
		t.Fatal("an unrecoverable block passed as an answer")
	}
}

// A turn that offers no tools still strips the markup: the user must never see
// protocol output, and it must not join the history as an answer. It just does
// not fabricate a call the turn cannot run.
func TestDSMLWithoutToolsStripsMarkup(t *testing.T) {
	got, calls, err := feedDSML(t, false, "Let me check.\n"+dsmlCalls)
	if err != nil {
		t.Fatalf("stripping must not fail: %v", err)
	}
	if calls != nil || strings.Contains(got, "DSML") {
		t.Fatalf("a request without tools must not recover calls or show markup: %q %#v", got, calls)
	}
	if !strings.Contains(got, "Let me check.") {
		t.Fatalf("prose was lost: %q", got)
	}
}

func TestDSMLThroughTheChatStream(t *testing.T) {
	body := oaText("Let me check.\n"+dsmlCalls+"\n") + oaFinish("stop")
	srv := sseServer(t, body, true, nil)
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
		Tools: []ToolSpec{{Name: "terminal_exec"}}}
	ch, err := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if stop.Kind != EventDone {
		t.Fatalf("stream ended with %+v", stop)
	}
	if got := toolCalls(evs); !reflect.DeepEqual(got, dsmlWant) {
		t.Fatalf("calls = %#v", got)
	}
	if got := text(evs, EventText); strings.Contains(got, "DSML") || !strings.Contains(got, "Let me check.") {
		t.Fatalf("text = %q", got)
	}
	if stop.StopReason != "tool_calls" {
		t.Fatalf("stop reason = %q, want the recovered call to have upgraded it", stop.StopReason)
	}
}

func TestDSMLWithoutToolsStripsMarkupFromTheStream(t *testing.T) {
	body := oaText("Let me check.\n", dsmlCalls) + oaFinish("stop")
	srv := sseServer(t, body, false, nil)
	ch, err := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if stop.Kind != EventDone || stop.StopReason != "stop" {
		t.Fatalf("terminal event = %+v", stop)
	}
	if toolCalls(evs) != nil {
		t.Fatalf("a request without tools recovered calls: %+v", evs)
	}
	if got := text(evs, EventText); strings.Contains(got, "DSML") || !strings.Contains(got, "Let me check.") {
		t.Fatalf("text = %q", got)
	}
}

// A serving lane can write the call into the thinking channel instead of the
// content. The answer is then empty, so the call has to be recovered there.
func TestDSMLInReasoningIsRecoveredWhenTheTurnIsEmpty(t *testing.T) {
	body := oaReasoning("Let me check.\n", dsmlCalls) + oaFinish("stop")
	srv := sseServer(t, body, true, nil)
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
		Tools: []ToolSpec{{Name: "terminal_exec"}}}
	ch, err := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if stop.Kind != EventDone || stop.StopReason != "tool_calls" {
		t.Fatalf("terminal event = %+v", stop)
	}
	if got := toolCalls(evs); !reflect.DeepEqual(got, dsmlWant) {
		t.Fatalf("calls = %#v", got)
	}
	if got := text(evs, EventText); got != "" {
		t.Fatalf("the thinking channel leaked into the answer: %q", got)
	}
	if got := text(evs, EventReasoning); strings.Contains(got, "DSML") || !strings.Contains(got, "Let me check.") {
		t.Fatalf("reasoning text = %q", got)
	}
}

// The reasoning channel is not where the answer is, so its markup only acts
// when the turn would otherwise be empty; a real call must not run twice and a
// quoted example must not run at all.
func TestDSMLInReasoningDoesNotDoubleRunTheCall(t *testing.T) {
	t.Run("answered in prose", func(t *testing.T) {
		body := oaReasoning(dsmlCalls) + oaText("The load is fine.") + oaFinish("stop")
		srv := sseServer(t, body, false, nil)
		req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
			Tools: []ToolSpec{{Name: "terminal_exec"}}}
		ch, _ := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
		evs := collect(t, ch)
		stop := terminal(t, evs)
		if got := toolCalls(evs); got != nil {
			t.Fatalf("a quoted call ran: %#v", got)
		}
		if stop.StopReason != "stop" {
			t.Fatalf("stop reason = %q", stop.StopReason)
		}
	})
	t.Run("called natively", func(t *testing.T) {
		body := oaReasoning(dsmlCalls) +
			oaToolChunks(0, "c1", "terminal_exec", `{"command":"ls"}`, 8) + oaFinish("tool_calls")
		srv := sseServer(t, body, false, nil)
		req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
			Tools: []ToolSpec{{Name: "terminal_exec"}}}
		ch, _ := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
		evs := collect(t, ch)
		terminal(t, evs)
		want := []ToolCall{{ID: "c1", Name: "terminal_exec", Arguments: `{"command":"ls"}`}}
		if got := toolCalls(evs); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls = %#v, want only the native one", got)
		}
	})
}

// Thinking may quote the markup without meaning to call anything, so a block
// that yields no call must not end the turn.
func TestDSMLInReasoningThatYieldsNoCallIsNotAnError(t *testing.T) {
	body := oaReasoning("<｜DSML｜ calls>\n<｜DSML｜ invoke name=\"x\">\n") + oaFinish("stop")
	srv := sseServer(t, body, false, nil)
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
		Tools: []ToolSpec{{Name: "terminal_exec"}}}
	ch, _ := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if stop.Kind != EventDone {
		t.Fatalf("terminal event = %+v", stop)
	}
	if got := toolCalls(evs); got != nil {
		t.Fatalf("calls = %#v", got)
	}
	if got := text(evs, EventReasoning); strings.Contains(got, "DSML") {
		t.Fatalf("markup reached the reasoning text: %q", got)
	}
}

func TestDSMLInReasoningThroughTheResponsesStream(t *testing.T) {
	body := rsThink(dsmlCalls) + rsCompleted()
	srv := sseServer(t, body, true, nil)
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
		Tools: []ToolSpec{{Name: "terminal_exec"}}}
	ch, err := NewOpenAIResponses(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if stop.Kind != EventDone || stop.StopReason != "tool_calls" {
		t.Fatalf("terminal event = %+v", stop)
	}
	if got := toolCalls(evs); !reflect.DeepEqual(got, dsmlWant) {
		t.Fatalf("calls = %#v", got)
	}
	if got := text(evs, EventReasoning); strings.Contains(got, "DSML") {
		t.Fatalf("markup reached the reasoning text: %q", got)
	}
}

func TestDSMLThroughTheResponsesStream(t *testing.T) {
	body := rsText("Let me check.\n", dsmlCalls, "\n") + rsCompleted()
	srv := sseServer(t, body, true, nil)
	req := Request{Model: "m", Messages: []Message{{Role: RoleUser, Text: "check"}},
		Tools: []ToolSpec{{Name: "terminal_exec"}}}
	ch, err := NewOpenAIResponses(Config{BaseURL: srv.URL, APIKey: testKey}).Stream(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	stop := terminal(t, evs)
	if got := toolCalls(evs); !reflect.DeepEqual(got, dsmlWant) {
		t.Fatalf("calls = %#v", got)
	}
	if got := text(evs, EventText); strings.Contains(got, "DSML") || !strings.Contains(got, "Let me check.") {
		t.Fatalf("text = %q", got)
	}
	if stop.StopReason != "tool_calls" {
		t.Fatalf("stop reason = %q", stop.StopReason)
	}
}

// The filter sits on every content delta, so the ordinary case must stay free.
func BenchmarkDSMLText(b *testing.B) {
	delta := "The quick brown fox jumps over the lazy dog; nothing here is markup. "
	b.SetBytes(int64(len(delta)))
	b.ReportAllocs()
	f := newDSMLFilter(true, true)
	for i := 0; i < b.N; i++ {
		if _, _, err := f.push(delta); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDSMLMarkup(b *testing.B) {
	b.SetBytes(int64(len(dsmlCalls)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f := newDSMLFilter(true, true)
		if _, _, err := f.push(dsmlCalls); err != nil {
			b.Fatal(err)
		}
		if _, _, err := f.flush(); err != nil {
			b.Fatal(err)
		}
	}
}
