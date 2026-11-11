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
	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

type fakeHostBrowserView struct {
	id      gfx.NativeViewID
	loaded  []string
	back    int
	forward int
	reload  int
	closed  int
}

func (view *fakeHostBrowserView) ID() gfx.NativeViewID { return view.id }
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

func openTestBrowser(view *fakeHostBrowserView) *hostBrowser {
	browser := &hostBrowser{open: true}
	browser.newTab().view = view
	browser.focusAddress = false
	return browser
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
	if len(browser.tabs) != 1 || browser.tabs[0].view != nil || host.config.URL != "" {
		t.Fatalf("Browser did not open on a blank tab: tabs %d, page config %+v", len(browser.tabs), host.config)
	}
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if context.Kind != 5 || !context.Open {
		t.Fatalf("terminal context = %+v", context)
	}
	state.setHostBrowserAddress(browser, "service.internal:8080/status")
	state.clickHostBrowser(hitHostBrowserGo)
	tab := browser.current()
	if tab.url != "https://service.internal:8080/status" || host.config.URL != tab.url {
		t.Fatalf("address = %q, page config %+v", tab.url, host.config)
	}
	if host.config.Proxy != browser.tunnel.Status().ListenAddress {
		t.Fatalf("page config = %+v", host.config)
	}
	state.clickHostBrowser(hitHostBrowserClose)
	if browser.tunnel != nil || len(browser.tabs) != 0 || browser.open || host.view.closed != 1 {
		t.Fatal("closing Browser left its page or tunnel running")
	}
}

func TestHostBrowserNewTabsArePagesOfTheirOwn(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	host := &fakeHostBrowserHost{view: &fakeHostBrowserView{id: 1}}
	state.browserHost = host
	state.toggleHostBrowser()
	browser := session.browser
	state.setHostBrowserAddress(browser, "127.0.0.1:3065")
	state.clickHostBrowser(hitHostBrowserGo)
	first := host.view

	host.view = &fakeHostBrowserView{id: 2}
	state.Click(contextID(hitHostBrowserNewTab))
	if len(browser.tabs) != 2 || browser.active != 1 || browser.current().view != nil || browser.current().address.String() != "" {
		t.Fatalf("new tab is not blank and active: %+v", browser.current())
	}
	state.setHostBrowserAddress(browser, "127.0.0.1:9090")
	state.clickHostBrowser(hitHostBrowserGo)
	second := host.view
	if browser.current().view != second || len(first.loaded) != 0 {
		t.Fatal("second tab reused the first tab's page")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if views := canvas.NativeViews(); len(views) != 1 || views[0].ID != 2 {
		t.Fatalf("visible pages = %+v", views)
	}
	firstTab := hostBrowserTabList.At(browser.tabs[0].id)
	if _, ok := hitByID(&canvas, contextID(firstTab)); !ok {
		t.Fatal("tab strip is missing the first tab")
	}
	state.Click(contextID(firstTab))
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if views := canvas.NativeViews(); len(views) != 1 || views[0].ID != 1 {
		t.Fatalf("switching tabs shows %+v", views)
	}
	if browser.current().address.String() != "http://127.0.0.1:3065/" {
		t.Fatalf("address bar = %q", browser.current().address.String())
	}

	state.Click(contextID(hostBrowserTabCloseList.At(browser.tabs[0].id)))
	if first.closed != 1 || len(browser.tabs) != 1 || browser.current().view != second || browser.tunnel == nil {
		t.Fatal("closing a tab did not keep the other page and the route")
	}
	state.Click(contextID(hostBrowserTabCloseList.At(browser.tabs[0].id)))
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if second.closed != 1 || browser.open || browser.tunnel != nil || context.Open {
		t.Fatal("closing the last tab did not close Browser")
	}
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.toggleHostBrowser()
	if len(browser.tabs) != 1 || browser.current().url != "" {
		t.Fatalf("reopened Browser has %d tabs, want one blank tab", len(browser.tabs))
	}
}

func TestHostBrowserReconnectReopensEveryTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	host := &fakeHostBrowserHost{view: &fakeHostBrowserView{id: 1}}
	state.browserHost = host
	state.toggleHostBrowser()
	browser := session.browser
	state.setHostBrowserAddress(browser, "127.0.0.1:3065")
	state.clickHostBrowser(hitHostBrowserGo)
	state.clickHostBrowser(hitHostBrowserNewTab)
	state.closeHostBrowserTunnel(session)
	if browser.tabs[0].view != nil || len(browser.tabs) != 2 {
		t.Fatal("dropping the route kept a page or lost the tabs")
	}
	host.config = app.WebViewConfig{}
	state.startHostBrowserTunnel(session)
	if browser.tabs[0].view == nil || host.config.URL != "http://127.0.0.1:3065/" || browser.tabs[1].view != nil {
		t.Fatalf("reconnect reopened %+v", host.config)
	}
}

func TestHostBrowserPanelClicksReachTheMountedControls(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	view := &fakeHostBrowserView{id: 9}
	host := &fakeHostBrowserHost{view: view}
	state.browserHost = host
	state.toggleHostBrowser()
	state.setHostBrowserAddress(session.browser, "127.0.0.1:3065")
	state.Click(contextID(hitHostBrowserGo))
	if host.config.URL != "http://127.0.0.1:3065/" {
		t.Fatalf("panel Go opened %q", host.config.URL)
	}
	state.setHostBrowserAddress(session.browser, "127.0.0.1:9090")
	state.Click(contextID(hitHostBrowserGo))
	if len(view.loaded) != 1 || view.loaded[0] != "http://127.0.0.1:9090/" {
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
	state.setHostBrowserAddress(session.browser, "127.0.0.1:3065")
	state.clickHostBrowser(hitHostBrowserGo)
	if tab := session.browser.current(); tab.err == "" || tab.view != nil {
		t.Fatalf("unsupported browser = %+v", tab)
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
	session.browser = openTestBrowser(&fakeHostBrowserView{id: 7})
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Kind, context.Open, context.Side, context.Width = 5, true, 1, 420
	_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
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
	session.browser = openTestBrowser(&fakeHostBrowserView{id: 7})
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	context.Kind, context.Open = 5, true
	_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if len(canvas.NativeViews()) != 1 || canvas.NativeViews()[0].ID != 7 {
		t.Fatalf("visible page = %+v", canvas.NativeViews())
	}
	canvas.Reset()
	state.Click(hitHosts)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
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

func dragContextDividerTo(t *testing.T, state *State, size gfx.Size, x float32) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	divider, ok := hitByID(&canvas, hitContextDivider)
	if !ok {
		t.Fatal("context divider is missing")
	}
	y := divider.Bounds.Y + divider.Bounds.Height/2
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, X: divider.Bounds.X, Y: y, Viewport: size})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerMove, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: x, Y: y, Viewport: size})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Target: hitContextDivider, Captured: true, X: x, Y: y, Viewport: size})
}

func TestHostBrowserPanelDragsToTwoThirdsOfTheTerminalArea(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	state.toggleHostBrowser()
	session.browser.current().view = &fakeHostBrowserView{id: 7}
	size := gfx.Size{Width: 1400, Height: 900}
	base := state.baseWorkspacePixelBounds(size)
	limit := base.Width * 2 / 3

	dragContextDividerTo(t, state, size, base.X+1)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	views := canvas.NativeViews()
	if len(views) != 1 {
		t.Fatalf("native views = %+v", views)
	}
	page := views[0].Bounds
	if page.Width < limit-1 || page.X < base.X+base.Width-limit-1 {
		t.Fatalf("page = %+v, want two thirds (%v) of %+v", page, limit, base)
	}
	if pane := state.workspacePixelBounds(size); pane.Width < base.Width-limit-2 {
		t.Fatalf("terminal area = %+v, want at least a third of %+v", pane, base)
	}

	restored := newTestState(t, state.options)
	if restored.browserPanelWidth != state.browserPanelWidth || restored.browserPanelWidth < limit-1 {
		t.Fatalf("restored browser width = %v, saved %v", restored.browserPanelWidth, state.browserPanelWidth)
	}

	// The files panel keeps its own, narrower width.
	state.showFilesPanel(session)
	context, _ := state.deck.Context(state.deck.ActiveWorkspace())
	if width := state.contextPanelWidth(base, context); width > layoutcore.MaxContextWidth {
		t.Fatalf("files panel inherited the browser width: %v", width)
	}
}

func TestLocalProxyAndBrowserToolsHaveDistinctIcons(t *testing.T) {
	icons := map[gfx.ID]uint8{}
	for _, tool := range toolbarCatalog[toolbarTerminal] {
		icons[tool.id] = tool.icon
	}
	browser, okBrowser := icons[hitHostBrowserOpen]
	proxy, okProxy := icons[hitLocalProxyOpen]
	if !okBrowser || !okProxy || browser == proxy {
		t.Fatalf("browser icon %d (%v), local proxy icon %d (%v)", browser, okBrowser, proxy, okProxy)
	}
}

func TestHostBrowserUnsupportedOffersExternalLaunchButton(t *testing.T) {
	state := newTestState(t, testOptions(t))
	session := connectedBrowserSession(state, &fakeConn{})
	state.browserHost = &fakeHostBrowserHost{err: errors.New("proxy unsupported")}
	state.toggleHostBrowser()
	state.setHostBrowserAddress(session.browser, "127.0.0.1:3065")
	state.clickHostBrowser(hitHostBrowserGo)

	var canvas gfx.Canvas
	state.paintHostBrowser(&canvas, gfx.Rect{Width: 640, Height: 480}, session)
	hit, found := hitByID(&canvas, hitHostBrowserLaunchExternal)
	if !found || hit.Disabled {
		t.Fatalf("expected launch external button when unsupported, found: %v, hit: %+v", found, hit)
	}
}

func TestTabMenuOffersSSHBrowser(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	conn := connect(t, state, world)
	tab := state.deck.ActiveWorkspace()

	check := func(wantOpen, wantClose bool) {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, state.viewport)
		rightClickTab(state, hitTabList.At(int(tab)))
		state.Paint(&canvas, state.viewport)
		_, hasOpen := hitByID(&canvas, hitTabMenuBrowserOpen)
		_, hasClose := hitByID(&canvas, hitTabMenuBrowserClose)
		if hasOpen != wantOpen || hasClose != wantClose {
			t.Fatalf("SSH browser menu: got open=%v, close=%v; want open=%v, close=%v", hasOpen, hasClose, wantOpen, wantClose)
		}
		state.Click(hitTabMenuDismiss)
	}

	check(false, false)
	conn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	check(true, false)

	session := &state.sessions[0]
	session.host.Protocol = protocolTelnet
	check(false, false)
	session.host.Protocol = protocolSSH
	check(true, false)
}
