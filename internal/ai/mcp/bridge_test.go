package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
