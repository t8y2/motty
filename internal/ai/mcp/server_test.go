package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeBackend struct {
	execs, writes, removes atomic.Int32
	execStarted            chan struct{}
	execCtxDone            chan struct{}
	execRelease            <-chan struct{}
	block                  bool
}

func (b *fakeBackend) Sessions(context.Context) ([]gate.SessionInfo, error) {
	return []gate.SessionInfo{{ID: "1", Title: "web", Address: "web:22", Active: true, Ready: true}}, nil
}
func (b *fakeBackend) ReadTerminal(context.Context, tools.TerminalReadArgs) (string, error) {
	return "screen", nil
}
func (b *fakeBackend) Exec(ctx context.Context, a tools.TerminalExecArgs) (gate.ExecResult, error) {
	b.execs.Add(1)
	if b.block {
		if b.execStarted != nil {
			close(b.execStarted)
		}
		select {
		case <-ctx.Done():
			if b.execCtxDone != nil {
				close(b.execCtxDone)
			}
			return gate.ExecResult{}, ctx.Err()
		case <-b.execRelease:
		}
	}
	return gate.ExecResult{Output: "out", ExitCode: 3}, nil
}
func (b *fakeBackend) ListDir(context.Context, tools.SFTPListArgs) ([]gate.FileEntry, error) {
	return []gate.FileEntry{{Name: "a", Size: 1}}, nil
}
func (b *fakeBackend) ReadFile(context.Context, tools.SFTPReadArgs) (string, bool, error) {
	return "data", false, nil
}
func (b *fakeBackend) WriteFile(context.Context, tools.SFTPWriteArgs) error {
	b.writes.Add(1)
	return nil
}
func (b *fakeBackend) Remove(context.Context, tools.SFTPRemoveArgs) error {
	b.removes.Add(1)
	return nil
}

func newServer(t testing.TB, g gate.Gate, mut func(*Config)) (*Server, *httptest.Server) {
	t.Helper()
	cfg := Config{Token: testToken, Gate: func() gate.Gate { return g }, ServerName: "motty", Version: "1"}
	if mut != nil {
		mut(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func okGate(b *fakeBackend) gate.Gate {
	return gate.Gate{Backend: b, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}}
}

func post(t testing.TB, url, body string, mut func(*http.Request)) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+Path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	if mut != nil {
		mut(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

type reply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func rpc(t testing.TB, url, body string) reply {
	t.Helper()
	resp, text := post(t, url, body, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, text)
	}
	var r reply
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("bad json %q: %v", text, err)
	}
	return r
}

type callOut struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

func call(t testing.TB, url, name, args string) callOut {
	t.Helper()
	r := rpc(t, url, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
	if r.Error != nil {
		t.Fatalf("protocol error: %+v", r.Error)
	}
	var c callOut
	if err := json.Unmarshal(r.Result, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestHandshakeAndEveryToolKind(t *testing.T) {
	b := &fakeBackend{}
	_, ts := newServer(t, okGate(b), nil)

	r := rpc(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`)
	var init struct {
		ProtocolVersion string
		Capabilities    struct{ Tools struct{ ListChanged bool } }
		ServerInfo      struct{ Name string }
		Instructions    string
	}
	if err := json.Unmarshal(r.Result, &init); err != nil || init.ProtocolVersion != "2025-03-26" || init.ServerInfo.Name != "motty" || init.Instructions == "" {
		t.Fatalf("init: %s %v", r.Result, err)
	}
	r = rpc(t, ts.URL, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if !strings.Contains(string(r.Result), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("unsupported version not downgraded: %s", r.Result)
	}

	resp, body := post(t, ts.URL, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil)
	if resp.StatusCode != http.StatusAccepted || body != "" {
		t.Fatalf("notification: %d %q", resp.StatusCode, body)
	}
	if r := rpc(t, ts.URL, `{"jsonrpc":"2.0","id":"p","method":"ping"}`); string(r.Result) != "{}" || string(r.ID) != `"p"` {
		t.Fatalf("ping: %+v", r)
	}

	r = rpc(t, ts.URL, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	var list struct {
		Tools []struct {
			Name        string
			Description string
			InputSchema map[string]any
			Annotations struct {
				ReadOnlyHint    bool
				DestructiveHint bool
			}
		}
	}
	if err := json.Unmarshal(r.Result, &list); err != nil || len(list.Tools) != len(tools.All()) {
		t.Fatalf("list: %s %v", r.Result, err)
	}
	for i, tl := range list.Tools {
		sp := tools.All()[i]
		if tl.Name != sp.Name || tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %d: %+v", i, tl)
		}
		if tl.Annotations.ReadOnlyHint != (sp.Kind == tools.Read) || tl.Annotations.DestructiveHint == (sp.Kind == tools.Read) {
			t.Errorf("annotations for %s: %+v", sp.Name, tl.Annotations)
		}
	}

	// read kinds
	if c := call(t, ts.URL, tools.NameListSessions, `{}`); c.IsError || !strings.Contains(c.Content[0].Text, "web:22") || c.Content[0].Type != "text" {
		t.Fatalf("list_sessions: %+v", c)
	}
	if c := call(t, ts.URL, tools.NameTerminalRead, `{"lines":5}`); c.IsError || c.Content[0].Text != "screen" {
		t.Fatalf("terminal_read: %+v", c)
	}
	if c := call(t, ts.URL, tools.NameSFTPList, `{"path":"/tmp"}`); c.IsError {
		t.Fatalf("sftp_list: %+v", c)
	}
	// exec at low risk runs without approval
	if c := call(t, ts.URL, tools.NameTerminalExec, `{"command":"ls"}`); c.IsError || !strings.Contains(c.Content[0].Text, "exit status 3") {
		t.Fatalf("exec: %+v", c)
	}
	// write kind without an approver is refused, not a protocol error
	if c := call(t, ts.URL, tools.NameSFTPWrite, `{"path":"/tmp/a","content":"x"}`); !c.IsError || b.writes.Load() != 0 {
		t.Fatalf("write: %+v", c)
	}
	if c := call(t, ts.URL, tools.NameSFTPRemove, `{"path":"/tmp/a"}`); !c.IsError || b.removes.Load() != 0 {
		t.Fatalf("remove: %+v", c)
	}
	// bad arguments are a tool error
	if c := call(t, ts.URL, tools.NameTerminalExec, `{"nope":1}`); !c.IsError {
		t.Fatalf("bad args: %+v", c)
	}
	// omitted arguments
	r = rpc(t, ts.URL, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_sessions"}}`)
	if r.Error != nil {
		t.Fatalf("no arguments: %+v", r.Error)
	}
}

func TestAuthFailures(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for name, hdr := range map[string]string{
		"missing": "", "wrong": "Bearer " + strings.Repeat("x", 32), "short": "Bearer abc",
		"scheme": "Basic " + testToken, "prefix": "Bearer " + testToken[:30],
		"extra": "Bearer " + testToken + "x",
	} {
		resp, text := post(t, ts.URL, body, func(r *http.Request) {
			if hdr == "" {
				r.Header.Del("Authorization")
			} else {
				r.Header.Set("Authorization", hdr)
			}
		})
		if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" || strings.Contains(text, testToken) {
			t.Errorf("%s: %d %q", name, resp.StatusCode, text)
		}
	}
	if resp, _ := post(t, ts.URL, body, func(r *http.Request) { r.Header.Set("Authorization", "bearer "+testToken) }); resp.StatusCode != 200 {
		t.Errorf("lowercase scheme: %d", resp.StatusCode)
	}
}

func TestShortTokenAndMissingGateRejected(t *testing.T) {
	if _, err := New(Config{Token: "short", Gate: func() gate.Gate { return gate.Gate{} }}); err == nil {
		t.Error("short token accepted")
	}
	if _, err := New(Config{Token: testToken}); err == nil {
		t.Error("nil gate accepted")
	}
	if _, err := New(Config{Token: testToken + " x", Gate: func() gate.Gate { return gate.Gate{} }}); err == nil {
		t.Error("token with a space accepted")
	}
	if a, b := GenerateToken(), GenerateToken(); a == b || len(a) < MinTokenLen {
		t.Errorf("tokens %q %q", a, b)
	}
}

func TestOriginAndHost(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for origin, want := range map[string]int{
		"http://evil.example": 403, "null": 403, "http://127.0.0.1.evil.com": 403,
		"http://localhost:3000": 200, "http://127.0.0.1:8080": 200, "http://[::1]:1": 200,
	} {
		resp, _ := post(t, ts.URL, body, func(r *http.Request) { r.Header.Set("Origin", origin) })
		if resp.StatusCode != want {
			t.Errorf("origin %q: %d want %d", origin, resp.StatusCode, want)
		}
	}
	for host, want := range map[string]int{"evil.example": 403, "evil.example:80": 403, "10.0.0.5:1": 403, "localhost:9": 200, "[::1]:9": 200} {
		resp, _ := post(t, ts.URL, body, func(r *http.Request) { r.Host = host })
		if resp.StatusCode != want {
			t.Errorf("host %q: %d want %d", host, resp.StatusCode, want)
		}
	}
}

func TestMethodContentTypeSizeAndPath(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), func(c *Config) { c.MaxBody = 256 })
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(m, ts.URL+Path, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, _ := http.DefaultClient.Do(req)
		if resp.StatusCode != 405 || resp.Header.Get("Allow") != "POST" {
			t.Errorf("%s: %d %q", m, resp.StatusCode, resp.Header.Get("Allow"))
		}
		resp.Body.Close()
	}
	for _, ct := range []string{"text/plain", "", "application/x-www-form-urlencoded"} {
		resp, _ := post(t, ts.URL, `{}`, func(r *http.Request) {
			if ct == "" {
				r.Header.Del("Content-Type")
			} else {
				r.Header.Set("Content-Type", ct)
			}
		})
		if resp.StatusCode != 415 {
			t.Errorf("content type %q: %d", ct, resp.StatusCode)
		}
	}
	if resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
	}); resp.StatusCode != 200 {
		t.Errorf("charset content type: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts.URL, strings.Repeat(" ", 1000)+`{}`, nil); resp.StatusCode != 413 {
		t.Errorf("big body: %d", resp.StatusCode)
	}
	// chunked body without a Content-Length
	req, _ := http.NewRequest(http.MethodPost, ts.URL+Path, io.NopCloser(strings.NewReader(strings.Repeat(" ", 1000))))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 413 {
		t.Errorf("chunked big body: %v %v", resp, err)
	}
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/other", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 404 {
		t.Errorf("other path: %d", resp.StatusCode)
	}
	if resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, func(r *http.Request) { r.Header.Set("MCP-Protocol-Version", "1900-01-01") }); resp.StatusCode != 400 {
		t.Errorf("bad protocol header: %d", resp.StatusCode)
	}
}

func TestMalformedBatchUnknownAndInvalidParams(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	code := func(body string) int {
		r := rpc(t, ts.URL, body)
		if r.Error == nil {
			t.Fatalf("%s: no error: %s", body, r.Result)
		}
		return r.Error.Code
	}
	for body, want := range map[string]int{
		`{not json`: codeParse, ``: codeParse, `[1,`: codeParse,
		`[]`: codeInvalidRequest, `{"jsonrpc":"1.0","id":1,"method":"ping"}`: codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1}`:                    codeInvalidRequest,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`:   codeInvalidRequest,
		`{"jsonrpc":"2.0","id":true,"method":"ping"}`: codeInvalidRequest,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`: codeInvalidRequest,
		`"str"`: codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":"nope"}`:                                                         codeMethodNotFound,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`:                                                   codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}`:                                       codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{}}`:                                       codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"zzz"}}`:                           codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":[1]}}`: codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":"x"}}`: codeInvalidParams,
	} {
		if got := code(body); got != want {
			t.Errorf("%s: code %d want %d", body, got, want)
		}
	}
	// batch of 17 is refused, a mixed batch answers only the requests
	var many []string
	for i := 0; i < 17; i++ {
		many = append(many, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	}
	if got := code("[" + strings.Join(many, ",") + "]"); got != codeInvalidRequest {
		t.Errorf("17-batch: %d", got)
	}
	_, text := post(t, ts.URL, `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"nope"}]`, nil)
	var items []reply
	if err := json.Unmarshal([]byte(text), &items); err != nil || len(items) != 2 || items[1].Error == nil {
		t.Fatalf("batch: %q %v", text, err)
	}
	resp, text := post(t, ts.URL, `[{"jsonrpc":"2.0","method":"notifications/initialized"}]`, nil)
	if resp.StatusCode != 202 || text != "" {
		t.Errorf("notification-only batch: %d %q", resp.StatusCode, text)
	}
	// a client response is ignored
	if resp, _ := post(t, ts.URL, `{"jsonrpc":"2.0","id":5,"result":{}}`, nil); resp.StatusCode != 202 {
		t.Errorf("client response: %d", resp.StatusCode)
	}
}

func TestObserverModeRefusesWrites(t *testing.T) {
	b := &fakeBackend{}
	g := okGate(b)
	g.Mode = gate.ModeObserve
	g.Approver = func(context.Context, gate.Request) (bool, string, error) { return true, "", nil }
	_, ts := newServer(t, g, nil)
	for name, args := range map[string]string{
		tools.NameTerminalExec: `{"command":"ls"}`,
		tools.NameSFTPWrite:    `{"path":"/tmp/a","content":"x"}`,
		tools.NameSFTPRemove:   `{"path":"/tmp/a"}`,
	} {
		if c := call(t, ts.URL, name, args); !c.IsError || !strings.Contains(c.Content[0].Text, "read-only") {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if b.execs.Load()+b.writes.Load()+b.removes.Load() != 0 {
		t.Fatal("observer connection changed something")
	}
	if c := call(t, ts.URL, tools.NameListSessions, `{}`); c.IsError {
		t.Fatalf("%+v", c)
	}
}

func TestApprovalFlowOverHTTP(t *testing.T) {
	b := &fakeBackend{}
	var answer atomic.Bool
	var seen atomic.Value
	g := okGate(b)
	g.Approver = func(_ context.Context, r gate.Request) (bool, string, error) {
		seen.Store(r)
		return answer.Load(), "", nil
	}
	_, ts := newServer(t, g, nil)

	answer.Store(true)
	if c := call(t, ts.URL, tools.NameSFTPWrite, `{"path":"/tmp/a","content":"x"}`); c.IsError || b.writes.Load() != 1 {
		t.Fatalf("approved: %+v", c)
	}
	if r := seen.Load().(gate.Request); r.Tool != tools.NameSFTPWrite || r.Kind != tools.Write {
		t.Errorf("request: %+v", r)
	}
	answer.Store(false)
	c := call(t, ts.URL, tools.NameSFTPRemove, `{"path":"/tmp/a"}`)
	if !c.IsError || !strings.Contains(c.Content[0].Text, "declined") || b.removes.Load() != 0 {
		t.Fatalf("declined: %+v", c)
	}
}

func TestGateIsAskedPerRequest(t *testing.T) {
	var mode atomic.Int32
	b := &fakeBackend{}
	cfg := func(c *Config) {
		c.Gate = func() gate.Gate {
			g := okGate(b)
			g.Mode = gate.Mode(mode.Load())
			return g
		}
	}
	_, ts := newServer(t, gate.Gate{}, cfg)
	if c := call(t, ts.URL, tools.NameTerminalExec, `{"command":"ls"}`); c.IsError {
		t.Fatalf("%+v", c)
	}
	mode.Store(int32(gate.ModeObserve))
	if c := call(t, ts.URL, tools.NameTerminalExec, `{"command":"ls"}`); !c.IsError {
		t.Fatalf("%+v", c)
	}
}

func TestNoBackendIsAToolError(t *testing.T) {
	_, ts := newServer(t, gate.Gate{}, nil)
	if c := call(t, ts.URL, tools.NameListSessions, `{}`); !c.IsError {
		t.Fatalf("%+v", c)
	}
}

func TestClientDisconnectCancelsTheBackendCall(t *testing.T) {
	b := &fakeBackend{block: true, execStarted: make(chan struct{}), execCtxDone: make(chan struct{})}
	_, ts := newServer(t, okGate(b), nil)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+Path,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"ls"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	errc := make(chan error, 1)
	go func() { _, err := http.DefaultClient.Do(req); errc <- err }()
	<-b.execStarted
	cancel()
	select {
	case <-b.execCtxDone:
	case <-time.After(5 * time.Second):
		t.Fatal("backend call was not cancelled")
	}
	<-errc
}

func TestCallTimeout(t *testing.T) {
	b := &fakeBackend{block: true}
	_, ts := newServer(t, okGate(b), func(c *Config) { c.CallTimeout = 50 * time.Millisecond })
	start := time.Now()
	c := call(t, ts.URL, tools.NameTerminalExec, `{"command":"ls"}`)
	if !c.IsError || !strings.Contains(c.Content[0].Text, "timed out") || time.Since(start) > 3*time.Second {
		t.Fatalf("%+v after %v", c, time.Since(start))
	}
}

// ignoringBackend never looks at ctx: the server must still answer on time.
type ignoringBackend struct{ fakeBackend }

func (b *ignoringBackend) Exec(context.Context, tools.TerminalExecArgs) (gate.ExecResult, error) {
	time.Sleep(300 * time.Millisecond)
	return gate.ExecResult{}, nil
}

func TestTimeoutHoldsEvenIfBackendIgnoresContext(t *testing.T) {
	_, ts := newServer(t, gate.Gate{Backend: &ignoringBackend{}, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}}, func(c *Config) { c.CallTimeout = 30 * time.Millisecond })
	start := time.Now()
	if c := call(t, ts.URL, tools.NameTerminalExec, `{"command":"ls"}`); !c.IsError || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("%+v after %v", c, time.Since(start))
	}
}

type panicBackend struct{ fakeBackend }

func (*panicBackend) Sessions(context.Context) ([]gate.SessionInfo, error) { panic("boom") }

func TestBackendPanicIsContained(t *testing.T) {
	_, ts := newServer(t, gate.Gate{Backend: &panicBackend{}}, nil)
	if c := call(t, ts.URL, tools.NameListSessions, `{}`); !c.IsError {
		t.Fatalf("%+v", c)
	}
}

func TestListenRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.5:0", "[::]:0", "example.com:80", "nonsense"} {
		if ln, err := Listen(addr); err == nil {
			ln.Close()
			t.Errorf("%s accepted", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:0"} {
		ln, err := Listen(addr)
		if err != nil {
			t.Errorf("%s: %v", addr, err)
			continue
		}
		if !ln.Addr().(*net.TCPAddr).IP.IsLoopback() {
			t.Errorf("%s bound to %v", addr, ln.Addr())
		}
		ln.Close()
	}
	if ln, err := Listen("[::1]:0"); err == nil {
		ln.Close()
	}
}

func TestShutdownCancelsInFlightCallAndLeaksNothing(t *testing.T) {
	before := runtime.NumGoroutine()
	b := &fakeBackend{block: true, execStarted: make(chan struct{}), execCtxDone: make(chan struct{})}
	s, err := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(b) }})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve(ln) }()

	client := &http.Client{Transport: &http.Transport{}}
	url := "http://" + ln.Addr().String() + Path
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"ls"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	got := make(chan string, 1)
	go func() {
		resp, err := client.Do(req)
		if err != nil {
			got <- "error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		bb, _ := io.ReadAll(resp.Body)
		got <- string(bb)
	}()
	<-b.execStarted
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	<-b.execCtxDone
	if err := <-served; err != nil {
		t.Fatalf("serve: %v", err)
	}
	<-got
	client.CloseIdleConnections()
	waitGoroutines(t, before)
}

func waitGoroutines(t *testing.T, before int) {
	t.Helper()
	http.DefaultClient.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= before+1 || time.Now().After(deadline) {
			if n > before+1 {
				buf := make([]byte, 1<<16)
				t.Fatalf("goroutines %d > %d\n%s", n, before, buf[:runtime.Stack(buf, true)])
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestManyConcurrentCallsAreRaceFree(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	done := make(chan struct{})
	for i := 0; i < 16; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 10; j++ {
				if c := call(t, ts.URL, tools.NameListSessions, `{}`); c.IsError {
					t.Error("error result")
				}
			}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

func TestResponsesNeverContainTheToken(t *testing.T) {
	_, ts := newServer(t, okGate(&fakeBackend{}), nil)
	for _, body := range []string{`{bad`, `{"jsonrpc":"2.0","id":1,"method":"nope"}`} {
		if _, text := post(t, ts.URL, body, nil); strings.Contains(text, testToken) {
			t.Errorf("token echoed: %s", text)
		}
	}
}

func FuzzHandler(f *testing.F) {
	s, err := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }, CallTimeout: time.Second})
	if err != nil {
		f.Fatal(err)
	}
	h := s.Handler()
	for _, seed := range []string{
		``, `{`, `[]`, `[{}]`, `null`, `"x"`, `1`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":5}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":null}`,
		`{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"terminal_read","arguments":{"lines":3}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sftp_list","arguments":{"path":1}}}`,
		`[{"jsonrpc":"2.0","method":"x"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`,
		`{"jsonrpc":"2.0","id":1e400,"method":"ping"}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+Path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusAccepted:
			if rec.Body.Len() != 0 {
				t.Fatalf("202 with body %q", rec.Body.Bytes())
			}
		case http.StatusOK:
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("invalid JSON %q for input %q", rec.Body.Bytes(), body)
			}
		default:
			t.Fatalf("status %d for %q", rec.Code, body)
		}
	})
}

func BenchmarkToolsList(b *testing.B) {
	s, _ := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }})
	h := s.Handler()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+Path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+testToken)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkToolsCall(b *testing.B) {
	s, _ := New(Config{Token: testToken, Gate: func() gate.Gate { return okGate(&fakeBackend{}) }})
	h := s.Handler()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"ls"}}}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+Path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+testToken)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

// Shutdown may be called while Serve is only just starting (a hub that is
// switched off at once): neither order may race or leave the listener open.
func TestShutdownRacingServeLeavesNothingListening(t *testing.T) {
	for range 50 {
		server, err := New(Config{Token: strings.Repeat("t", 32), Gate: func() gate.Gate { return gate.Gate{} }})
		if err != nil {
			t.Fatal(err)
		}
		ln, err := Listen("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { defer close(done); _ = server.Serve(ln) }()
		_ = server.Shutdown(context.Background())
		<-done
		if c, err := net.DialTimeout("tcp", ln.Addr().String(), 200*time.Millisecond); err == nil {
			c.Close()
			t.Fatal("the listener is still open")
		}
	}
}
