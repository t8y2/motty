package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
)

func runBridge(t *testing.T, url, token, input string) []string {
	t.Helper()
	var out strings.Builder
	if err := Bridge(context.Background(), strings.NewReader(input), &out, url+Path, token, nil); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestBridgeRoundTripAnd202(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	lines := runBridge(t, ts.URL, testToken,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n"+
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"+
			"\n"+
			`{ "jsonrpc":"2.0", "id":2, "method":"tools/call", "params":{"name":"list_sessions","arguments":{}} }`)
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) || strings.ContainsAny(l, "\n") {
			t.Errorf("not one compact JSON line: %q", l)
		}
	}
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, `"protocolVersion":"2025-06-18"`) || !strings.Contains(got, "web:22") {
		t.Errorf("replies: %s", got)
	}
}

func TestBridgeWrongTokenAndUnreachable(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	lines := runBridge(t, ts.URL, "wrongwrongwrongwrongwrongwrong", `{"jsonrpc":"2.0","id":4,"method":"ping"}`+"\n"+`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(lines) != 1 || !strings.Contains(lines[0], `"id":4`) || !strings.Contains(lines[0], "401") || strings.Contains(lines[0], testToken) {
		t.Fatalf("%q", lines)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	lines = runBridge(t, url, testToken, `{"jsonrpc":"2.0","id":"a","method":"ping"}`)
	if len(lines) != 1 || !strings.Contains(lines[0], `"id":"a"`) || !strings.Contains(lines[0], "error") {
		t.Fatalf("%q", lines)
	}
}

// switchableHandler is an HTTP handler whose target can be replaced, the way a
// restarted application answers with a new, empty session table.
type switchableHandler struct{ target atomic.Value }

func (h *switchableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.target.Load().(http.Handler).ServeHTTP(w, r)
}

func newBridgePipe(t *testing.T, url string) (io.WriteCloser, *bufio.Reader, <-chan error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(context.Background(), inR, outW, url+Path, testToken, nil)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	return inW, bufio.NewReader(outR), done
}

func sendBridgeLine(t *testing.T, in io.Writer, line string) {
	t.Helper()
	if _, err := io.WriteString(in, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func readBridgeLine(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	return strings.TrimSpace(line)
}

func TestBridgeStartsANewSessionWhenTheServerForgotIt(t *testing.T) {
	first, err := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }})
	if err != nil {
		t.Fatal(err)
	}
	var target switchableHandler
	target.target.Store(first.Handler())
	ts := httptest.NewServer(&target)
	t.Cleanup(ts.Close)
	in, br, done := newBridgePipe(t, ts.URL)

	sendBridgeLine(t, in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if got := readBridgeLine(t, br); !strings.Contains(got, `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize: %s", got)
	}

	// The application restarted; the session the bridge holds is gone.
	target.target.Store(second.Handler())
	sendBridgeLine(t, in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`)
	got := readBridgeLine(t, br)
	if !strings.Contains(got, `"id":2`) || !strings.Contains(got, "web:22") {
		t.Fatalf("call after the restart: %s", got)
	}
	if strings.Contains(got, "motty-bridge") {
		t.Fatalf("the bridge's own initialize reached the client: %s", got)
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatalf("bridge: %v", err)
	}
}

func TestBridgeReportsWhenItCannotStartANewSession(t *testing.T) {
	first, err := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }})
	if err != nil {
		t.Fatal(err)
	}
	var target switchableHandler
	target.target.Store(first.Handler())
	ts := httptest.NewServer(&target)
	t.Cleanup(ts.Close)
	in, br, done := newBridgePipe(t, ts.URL)

	sendBridgeLine(t, in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	readBridgeLine(t, br)

	target.target.Store(http.NotFoundHandler())
	sendBridgeLine(t, in, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`)
	got := readBridgeLine(t, br)
	if !strings.Contains(got, `"id":2`) || !strings.Contains(got, "could not start a new one") {
		t.Fatalf("failed renewal: %s", got)
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatalf("bridge: %v", err)
	}
}

func TestBridgeOversizedLine(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", MaxLine+10) + `"}}`
	lines := runBridge(t, ts.URL, testToken, big+"\n"+`{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %d", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "longer than") || !strings.Contains(joined, `"id":2`) {
		t.Errorf("%s", joined)
	}
}

func TestBridgeWithPipesStopsOnEOFAndOnContext(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Bridge(context.Background(), inR, outW, ts.URL+Path, testToken, nil); outW.Close() }()
	br := bufio.NewReader(outR)
	io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n")
	line, err := br.ReadString('\n')
	if err != nil || !strings.Contains(line, `"id":1`) {
		t.Fatalf("%q %v", line, err)
	}
	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("EOF gave %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not stop on EOF")
	}

	ctx, cancel := context.WithCancel(context.Background())
	inR2, inW2 := io.Pipe()
	defer inW2.Close()
	done = make(chan error, 1)
	go func() { done <- Bridge(ctx, inR2, io.Discard, ts.URL+Path, testToken, nil) }()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("ctx gave %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not stop on ctx")
	}
}

func TestBridgeStreamsRZSZProgressBeforeTheCallFinishes(t *testing.T) {
	release := make(chan struct{})
	server, err := New(Config{
		Token: testToken,
		Tools: tools.All(),
		Dispatch: func(ctx context.Context, name string, args []byte) gate.Result {
			if name != tools.NameRZSZUpload {
				t.Errorf("tool %s", name)
			}
			gate.ReportProgress(ctx, gate.ProgressReport{Message: "waiting for the remote to accept a.bin; 0 bytes have moved"})
			gate.ReportProgress(ctx, gate.ProgressReport{Message: "waiting again"})
			<-release
			gate.ReportProgress(ctx, gate.ProgressReport{Progress: 100, Total: 200, HasTotal: true, Message: "transferring a.bin; 100/200 bytes have moved"})
			return gate.Result{Text: "transferred"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- Bridge(context.Background(), inR, outW, ts.URL+Path, testToken, nil)
		outW.Close()
	}()
	lines := make(chan string, 8)
	go func() {
		br := bufio.NewReader(outR)
		for {
			line, err := br.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	_, err = io.WriteString(inW, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rzsz_upload","arguments":{"local_path":"C:/a.bin"},"_meta":{"progressToken":"p1"}}}`+"\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-lines:
		if !strings.Contains(line, `"method":"notifications/progress"`) || !strings.Contains(line, "waiting for the remote") || strings.Contains(line, "waiting again") {
			t.Fatalf("first progress line: %s", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("progress was not streamed before the call finished")
	}
	close(release)
	var got strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("stream ended without the tool result")
			}
			got.WriteString(line)
			if strings.Contains(line, `"id":3`) {
				goto done
			}
		case <-deadline:
			t.Fatalf("result did not follow progress: %s", got.String())
		}
	}
done:
	text := got.String()
	if strings.Contains(text, "waiting again") || !strings.Contains(text, `"progress":100`) || !strings.Contains(text, `"total":200`) || !strings.Contains(text, "transferred") {
		t.Fatalf("after release: %s", text)
	}
	inW.Close()
	select {
	case err := <-bridgeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not return")
	}
}

func TestRZSZWithoutAnEventStreamStaysOneJSONResult(t *testing.T) {
	server, err := New(Config{
		Token: testToken,
		Tools: tools.All(),
		Dispatch: func(ctx context.Context, name string, args []byte) gate.Result {
			gate.ReportProgress(ctx, gate.ProgressReport{Progress: 5, Total: 9, HasTotal: true, Message: "transferring"})
			return gate.Result{Text: "done"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	resp, body := post(t, ts.URL, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"rzsz_upload","arguments":{"local_path":"C:/a.bin"},"_meta":{"progressToken":9}}}`, nil)
	if resp.Header.Get("Content-Type") != "application/json" || strings.Contains(body, "notifications/progress") || !strings.Contains(body, "done") {
		t.Fatalf("content-type %s body %s", resp.Header.Get("Content-Type"), body)
	}
}

func TestBridgeSlowCallDoesNotBlockPing(t *testing.T) {
	release := make(chan struct{})
	b := &fakeBackend{block: true, execRelease: release}
	_, ts := newServer(t, okGate(b), nil)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { Bridge(context.Background(), inR, outW, ts.URL+Path, testToken, nil); outW.Close() }()
	br := bufio.NewReader(outR)
	io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"ls"}}}`+"\n")
	io.WriteString(inW, `{"jsonrpc":"2.0","id":2,"method":"ping"}`+"\n")
	line, _ := br.ReadString('\n')
	if !strings.Contains(line, `"id":2`) {
		t.Fatalf("ping did not overtake the slow call: %q", line)
	}
	close(release)
	inW.Close()
	io.Copy(io.Discard, outR)
}

// TestAToolsCallNotificationIsNotAProgressCall guards the stream decision for a
// request without an id: it is a notification, and there is nothing to stream.
func TestAToolsCallNotificationIsNotAProgressCall(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"` + tools.NameRZSZUpload + `","_meta":{"progressToken":1}}}`)
	if _, ok := rzszProgressCall("text/event-stream", body); ok {
		t.Fatal("a tools/call without an id was treated as a progress call")
	}
}
