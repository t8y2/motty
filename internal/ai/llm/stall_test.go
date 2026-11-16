package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A connection that stays open and sends nothing must fail, not hang: the
// panel shows an error and the user can retry.
func TestASilentStreamFailsInsteadOfHanging(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, oaData(`{"choices":[{"delta":{"content":"partial"}}]}`))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer srv.Close()
	defer close(release)

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey, IdleTimeout: 100 * time.Millisecond})
	ch, err := p.Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("the stream closed without an error event")
			}
			switch ev.Kind {
			case EventText:
				seen.WriteString(ev.Text)
			case EventError:
				if ev.Err == nil || !strings.Contains(ev.Err.Error(), "stalled") {
					t.Fatalf("error = %v, want a stall", ev.Err)
				}
				if seen.String() != "partial" {
					t.Fatalf("text before the stall = %q", seen.String())
				}
				return
			}
		case <-deadline:
			t.Fatal("a silent stream was never failed")
		}
	}
}

// A slow but living stream keeps its answer: the watcher measures silence, not
// the time a long answer takes.
func TestASlowStreamKeepsItsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []string{"one ", "two ", "three"} {
			_, _ = io.WriteString(w, oaText(part))
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
		_, _ = io.WriteString(w, oaFinish("stop"))
	}))
	defer srv.Close()

	p := NewOpenAI(Config{BaseURL: srv.URL, APIKey: testKey, IdleTimeout: 150 * time.Millisecond})
	ch, err := p.Stream(bg, simpleReq)
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	done := terminal(t, evs)
	if done.Kind != EventDone || text(evs, EventText) != "one two three" {
		t.Fatalf("events = %+v", evs)
	}
}
