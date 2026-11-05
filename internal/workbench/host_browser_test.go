package workbench

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

type fakeHostBrowserView struct {
	id      ui.NativeViewID
	loaded  []string
	back    int
	forward int
	reload  int
	closed  int
}

func (view *fakeHostBrowserView) ID() ui.NativeViewID { return view.id }
func (view *fakeHostBrowserView) Load(target string)  { view.loaded = append(view.loaded, target) }
func (view *fakeHostBrowserView) Reload()             { view.reload++ }
func (view *fakeHostBrowserView) Back()               { view.back++ }
func (view *fakeHostBrowserView) Forward()            { view.forward++ }
func (view *fakeHostBrowserView) Focus()              {}
func (view *fakeHostBrowserView) Close()              { view.closed++ }

type fakeHostBrowserHost struct {
	config app.WebViewConfig
	view   *fakeHostBrowserView
	err    error
}

func (host *fakeHostBrowserHost) Open(config app.WebViewConfig) (hostBrowserView, error) {
	host.config = config
	if host.err != nil {
		return nil, host.err
	}
	return host.view, nil
}

func connectedBrowserSession(state *State, conn Connection) *Session {
	state.sessions = append(state.sessions, Session{ID: 1, HostID: 1, Title: "internal", host: Host{ID: 1, Protocol: protocolSSH}, conn: conn, phase: sshconn.PhaseConnected, term: newTerminal(80, 24)})
	state.activeSection = sectionTerminal
	state.ensureWorkspaceDeck()
	state.syncActiveSession()
	return &state.sessions[0]
}

func TestNormalizeHostBrowserURL(t *testing.T) {
	valid := map[string]string{
		"localhost:3000":          "http://localhost:3000/",
		"127.0.0.1:8080":          "http://127.0.0.1:8080/",
		"[::1]:3000/app":          "http://[::1]:3000/app",
		"service.internal:8080":   "https://service.internal:8080/",
		"https://example.com/app": "https://example.com/app",
	}
	for input, want := range valid {
		got, err := normalizeHostBrowserURL(input)
		if err != nil || got != want {
			t.Errorf("normalizeHostBrowserURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "https://user:pass@example.com/"} {
		if _, err := normalizeHostBrowserURL(input); err == nil {
			t.Errorf("unsafe URL accepted: %q", input)
		}
	}
}

func TestHostBrowserToolbarOpensTunnelWithIsolatedProxy(t *testing.T) {
	conn := &fakeConn{}
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, conn)
	host := &fakeHostBrowserHost{view: &fakeHostBrowserView{id: 8}}
	state.browserHost = host
	state.toggleHostBrowser()
	browser := session.browser
	if browser == nil || browser.tunnel == nil {
		t.Fatal("Browser did not start its SSH dynamic forward")
	}
	if host.config.URL != "http://127.0.0.1/" || host.config.Proxy != browser.tunnel.Status().ListenAddress {
		t.Fatalf("page config = %+v", host.config)
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if context.Kind != 5 || !context.Open {
		t.Fatalf("terminal context = %+v", context)
	}
	state.setHostBrowserAddress(browser, "service.internal:8080/status")
	state.clickHostBrowser(hitHostBrowserGo)
	if browser.url != "https://service.internal:8080/status" {
		t.Fatalf("address = %q", browser.url)
	}
	state.clickHostBrowser(hitHostBrowserClose)
	if browser.tunnel != nil || browser.view != nil || browser.open {
		t.Fatal("closing Browser left its page or tunnel running")
	}
}

func TestHostBrowserPanelClicksReachTheMountedControls(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	view := &fakeHostBrowserView{id: 9}
	state.browserHost = &fakeHostBrowserHost{view: view}
	state.toggleHostBrowser()
	state.setHostBrowserAddress(session.browser, "127.0.0.1:3065")
	state.Click(contextID(hitHostBrowserGo))
	if len(view.loaded) != 1 || view.loaded[0] != "http://127.0.0.1:3065/" {
		t.Fatalf("panel Go loaded %v", view.loaded)
	}
	state.Click(contextID(hitHostBrowserReload))
	if view.reload != 1 {
		t.Fatal("panel Reload did not reach the page")
	}
	state.Click(contextID(hitHostBrowserClose))
	if session.browser.open || view.closed != 1 {
		t.Fatal("panel Close left the page open")
	}
}

func TestHostBrowserRejectsUnsupportedProxyInsteadOfDirectLoad(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	state.browserHost = &fakeHostBrowserHost{err: errors.New("proxy unsupported")}
	state.toggleHostBrowser()
	if session.browser.err == "" || session.browser.view != nil {
		t.Fatalf("unsupported browser = %+v", session.browser)
	}
}

func TestHostBrowserSOCKS5UsesRemoteHostnamesAndInternalAddresses(t *testing.T) {
	dialed := make(chan string, 2)
	conn := &fakeConn{dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed <- address
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			_, _ = io.Copy(io.Discard, server)
		}()
		return client, nil
	}}
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, conn)
	state.toggleHostBrowser()
	proxy := session.browser.tunnel.Status().ListenAddress
	for _, target := range []string{"service.internal:8080", "10.20.0.15:9090"} {
		client, err := net.DialTimeout("tcp", proxy, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write([]byte{5, 1, 0}); err != nil {
			t.Fatal(err)
		}
		method := make([]byte, 2)
		if _, err := io.ReadFull(client, method); err != nil {
			t.Fatal(err)
		}
		host, rawPort, err := net.SplitHostPort(target)
		if err != nil {
			t.Fatal(err)
		}
		request := []byte{5, 1, 0, 3, byte(len(host))}
		request = append(request, host...)
		port, _ := net.LookupPort("tcp", rawPort)
		request = binary.BigEndian.AppendUint16(request, uint16(port))
		if _, err := client.Write(request); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 10)
		if _, err := io.ReadFull(client, reply); err != nil {
			t.Fatal(err)
		}
		if reply[1] != 0 {
			t.Fatalf("SOCKS5 reply for %s = %v", target, reply)
		}
		_ = client.Close()
		select {
		case got := <-dialed:
			if got != target {
				t.Fatalf("SSH dialed %q, want %q", got, target)
			}
		case <-time.After(time.Second):
			t.Fatalf("SSH did not dial %s", target)
		}
	}
}

func TestHostBrowserRightPanelDoesNotOverlapTerminal(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	session.browser = &hostBrowser{open: true, view: &fakeHostBrowserView{id: 7}}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Kind, context.Open, context.Side, context.Width = 5, true, 1, 420
	_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	views := canvas.NativeViews()
	if len(views) != 1 {
		t.Fatalf("native views = %+v", views)
	}
	page := views[0].Bounds
	if page.X < 900 || page.X+page.Width > 1400 {
		t.Fatalf("browser page overlaps terminal: %+v", page)
	}
	for _, hit := range canvas.Hits() {
		if hit.ID == hitHostBrowserAddress && (hit.Bounds.X < page.X || hit.Bounds.X+hit.Bounds.Width > page.X+page.Width) {
			t.Fatalf("address toolbar is outside browser panel: page %+v address %+v", page, hit.Bounds)
		}
	}
}

func TestHostBrowserNativeViewOnlyVisibleForConnectedActiveSession(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	session.browser = &hostBrowser{open: true, view: &fakeHostBrowserView{id: 7}}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Kind, context.Open = 5, true
	_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if len(canvas.NativeViews()) != 1 || canvas.NativeViews()[0].ID != 7 {
		t.Fatalf("visible page = %+v", canvas.NativeViews())
	}
	canvas.Reset()
	state.Click(hitHosts)
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if len(canvas.NativeViews()) != 0 {
		t.Fatal("leaving the terminal did not hide the page")
	}
}

func TestHostBrowserAndFilesPanelAreExclusive(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	state.toggleHostBrowser()
	state.showFilesPanel(session)
	if session.browser.open || session.browser.tunnel != nil {
		t.Fatal("opening files left a hidden Browser tunnel running")
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if context.Kind != 1 {
		t.Fatalf("context kind = %d", context.Kind)
	}
}
