package workbench

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/portforward"
	"motty/internal/sshbrowser"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

var hostBrowserHits = tree.Feature("host-browser")

var (
	hitHostBrowserOpen           = tree.ID(shellHits, "host-browser-open")
	hitHostBrowserClose          = tree.ID(hostBrowserHits, "close")
	hitHostBrowserBack           = tree.ID(hostBrowserHits, "back")
	hitHostBrowserForward        = tree.ID(hostBrowserHits, "forward")
	hitHostBrowserReload         = tree.ID(hostBrowserHits, "reload")
	hitHostBrowserAddress        = tree.ID(hostBrowserHits, "address")
	hitHostBrowserGo             = tree.ID(hostBrowserHits, "go")
	hitHostBrowserExternal       = tree.ID(hostBrowserHits, "external")
	hitHostBrowserLaunchExternal = tree.ID(hostBrowserHits, "launch-external")
	hitHostBrowserRetry          = tree.ID(hostBrowserHits, "retry")
	hitHostBrowserNewTab         = tree.ID(hostBrowserHits, "new-tab")

	hostBrowserTabList      = tree.Sub(hostBrowserHits, tree.Name("tab"))
	hostBrowserTabCloseList = tree.Sub(hostBrowserHits, tree.Name("tab-close"))
)

// A page opens wider than a file list; dragging it is limited to two thirds of
// the terminal area (contextPanelMaximum).
const defaultBrowserPanelWidth = float32(640)

type hostBrowserView interface {
	ID() gfx.NativeViewID
	Load(string)
	Reload()
	Back()
	Forward()
	Focus()
	Close()
}

type hostBrowserHost interface {
	Open(app.WebViewConfig) (hostBrowserView, error)
}

type systemHostBrowserHost struct {
	host *app.WebHost
}

func (host systemHostBrowserHost) Open(config app.WebViewConfig) (hostBrowserView, error) {
	return host.host.Open(config)
}

// hostBrowser is the browser of one SSH session: every tab is its own page, and
// all of them reach the network through the session's one SOCKS5 forward.
type hostBrowser struct {
	open         bool
	host         hostBrowserHost
	ready        bool
	tunnel       *portforward.Forward
	err          string
	tabs         []*hostBrowserTab
	active       int
	nextTab      int
	focusAddress bool
	external     *sshbrowser.Browser
}

// hostBrowserTab is one page. A tab without an address is blank: it has no
// view until the user enters one.
type hostBrowserTab struct {
	id      int
	view    hostBrowserView
	address text.Editor
	url     string
	err     string
	editing bool
}

func (browser *hostBrowser) newTab() *hostBrowserTab {
	browser.nextTab++
	tab := &hostBrowserTab{id: browser.nextTab}
	browser.tabs = append(browser.tabs, tab)
	browser.active = len(browser.tabs) - 1
	browser.focusAddress = true
	return tab
}

// current is the active tab, or nil once the browser is closed.
func (browser *hostBrowser) current() *hostBrowserTab {
	if len(browser.tabs) == 0 {
		return nil
	}
	browser.active = min(max(browser.active, 0), len(browser.tabs)-1)
	return browser.tabs[browser.active]
}

func (browser *hostBrowser) tabIndex(id int) int {
	for index, tab := range browser.tabs {
		if tab.id == id {
			return index
		}
	}
	return -1
}

func (browser *hostBrowser) closeViews() {
	for _, tab := range browser.tabs {
		if tab.view != nil {
			tab.view.Close()
			tab.view = nil
		}
	}
}

// problem is the error to show above the active page: the route's, else the tab's.
func (browser *hostBrowser) problem(tab *hostBrowserTab) string {
	if browser.err != "" {
		return browser.err
	}
	return tab.err
}

func (tab *hostBrowserTab) title(blank string) string {
	if parsed, err := url.Parse(tab.url); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return blank
}

var hostBrowserPort = regexp.MustCompile(`^[^/:]+:\d+(?:[/?#]|$)`)
var hostBrowserIPv6 = regexp.MustCompile(`^\[[0-9a-fA-F:]+\](?::\d+)?(?:/|$)`)

func normalizeHostBrowserURL(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" || len(raw) > 2048 {
		return "", errors.New("enter a valid web address")
	}
	candidate := raw
	parsed, parseErr := url.Parse(raw)
	if parseErr != nil && !strings.Contains(raw, "://") {
		parsed = &url.URL{}
	}
	if parsed.Scheme == "" || hostBrowserPort.MatchString(raw) {
		host := raw
		if slash := strings.IndexAny(host, "/?#"); slash >= 0 {
			host = host[:slash]
		}
		if hostPort := strings.TrimPrefix(host, "["); strings.HasPrefix(raw, "[") {
			host = strings.Split(hostPort, "]")[0]
		} else if withoutPort, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = withoutPort
		} else if colon := strings.LastIndex(host, ":"); colon >= 0 {
			host = host[:colon]
		}
		scheme := "https"
		if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil || hostBrowserIPv6.MatchString(raw) {
			scheme = "http"
		}
		candidate = scheme + "://" + raw
	}
	parsed, parseErr = url.Parse(candidate)
	if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("only HTTP and HTTPS pages can open here")
	}
	if parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("enter a valid web address without credentials")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	return parsed.String(), nil
}

func (state *State) HostBrowserWebHost() *app.WebHost {
	host := &app.WebHost{}
	state.browserHost = systemHostBrowserHost{host: host}
	host.OnReady = func() {
		for index := range state.sessions {
			if state.sessions[index].browser != nil {
				state.sessions[index].browser.host = state.browserHost
				state.sessions[index].browser.ready = true
				state.openHostBrowserPages(&state.sessions[index])
			}
		}
	}
	return host
}

func (state *State) hostBrowserFor(session *Session) *hostBrowser {
	if session.browser == nil {
		session.browser = &hostBrowser{}
	}
	if state.browserHost != nil {
		session.browser.host = state.browserHost
		session.browser.ready = true
	}
	return session.browser
}

func (state *State) toggleHostBrowser() {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.host.isSSH() || session.isLocal() {
		return
	}
	workspaceID := state.deck.ActiveWorkspace()
	context, ok := state.deck.Context(workspaceID)
	if !ok {
		return
	}
	browser := state.hostBrowserFor(session)
	if browser.open && context.Kind == layoutcore.ContextBrowser && context.Open {
		state.closeHostBrowser(session)
		context.Open = false
	} else {
		if context.Kind == layoutcore.ContextSFTP && context.Open {
			state.releaseIdleSFTP(session)
		}
		browser.open = true
		if len(browser.tabs) == 0 {
			browser.newTab()
		}
		context.Kind = layoutcore.ContextBrowser
		context.Open = true
		context.Side = layoutcore.ContextRight
		context.Width = defaultBrowserPanelWidth
		if state.browserPanelWidth > 0 {
			context.Width = state.browserPanelWidth
		}
		state.startHostBrowserTunnel(session)
	}
	_ = state.deck.SetContext(workspaceID, context)
	state.fitWorkspaceTerminals(workspaceID)
}

func (state *State) closeHostBrowser(session *Session) {
	if session == nil || session.browser == nil {
		return
	}
	browser := session.browser
	browser.open = false
	browser.closeViews()
	if browser.external != nil {
		browser.external.Close()
		browser.external = nil
	}
	browser.tabs, browser.active = nil, 0
	if browser.tunnel != nil {
		browser.tunnel.CloseAsync()
		browser.tunnel = nil
	}
}

func (state *State) startHostBrowserTunnel(session *Session) {
	browser := state.hostBrowserFor(session)
	if browser.tunnel != nil || session.conn == nil || session.phase != sshconn.PhaseConnected {
		return
	}
	tunnel, err := portforward.Start(portforward.Config{
		ListenAddress: "127.0.0.1:0", Mode: portforward.ModeSOCKS5,
		Dial: session.conn.DialContext, Notify: state.options.Invalidate,
	})
	if err != nil {
		browser.err = err.Error()
		return
	}
	browser.tunnel = tunnel
	browser.err = ""
	state.openHostBrowserPages(session)
}

func (state *State) openHostBrowserPages(session *Session) {
	browser := state.hostBrowserFor(session)
	for _, tab := range browser.tabs {
		state.openHostBrowserPage(browser, tab)
	}
}

// openHostBrowserPage opens the page of a tab that has an address; a blank tab
// stays without a view.
func (state *State) openHostBrowserPage(browser *hostBrowser, tab *hostBrowserTab) {
	if !browser.open || tab.view != nil || tab.url == "" || browser.tunnel == nil || !browser.ready || browser.host == nil || state.closed {
		return
	}
	proxy := browser.tunnel.Status().ListenAddress
	view, err := browser.host.Open(app.WebViewConfig{URL: tab.url, Proxy: proxy, OnLoad: func(load app.WebLoad) {
		if normalized, normalizeErr := normalizeHostBrowserURL(load.URL); normalizeErr == nil {
			tab.url = normalized
			if !tab.editing {
				tab.address = *text.NewEditor(normalized)
			}
		}
		if load.Phase == app.WebLoadStarted {
			tab.err = ""
		}
	}})
	if err != nil {
		tab.err = state.messages.HostBrowser.Unsupported
		return
	}
	tab.err = ""
	tab.view = view
}

func (state *State) setHostBrowserAddress(browser *hostBrowser, target string) {
	if tab := browser.current(); tab != nil {
		tab.address = *text.NewEditor(target)
	}
}

func (state *State) navigateHostBrowser(session *Session) {
	browser := state.hostBrowserFor(session)
	tab := browser.current()
	if tab == nil {
		return
	}
	tab.editing = false
	target, err := normalizeHostBrowserURL(tab.address.String())
	if err != nil {
		tab.err = state.messages.HostBrowser.InvalidAddress
		return
	}
	tab.err = ""
	tab.url = target
	tab.address = *text.NewEditor(target)
	if tab.view != nil {
		tab.view.Load(target)
	} else {
		state.openHostBrowserPage(browser, tab)
	}
	if tab.view != nil {
		tab.view.Focus()
	}
}

func (state *State) closeHostBrowserTab(session *Session, index int) {
	browser := session.browser
	if tab := browser.tabs[index]; tab.view != nil {
		tab.view.Close()
	}
	browser.tabs = append(browser.tabs[:index], browser.tabs[index+1:]...)
	if len(browser.tabs) == 0 {
		state.clickHostBrowser(hitHostBrowserClose)
		return
	}
	if browser.active > index || browser.active == len(browser.tabs) {
		browser.active--
	}
	browser.focusAddress = browser.current().url == ""
}

func (state *State) clickHostBrowser(id gfx.ID) bool {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if id == hitHostBrowserOpen {
		if ok {
			state.toggleHostBrowser()
		}
		return ok
	}
	if !ok || session.browser == nil {
		return false
	}
	browser := session.browser
	if tabID, found := id.In(hostBrowserTabList); found {
		if index := browser.tabIndex(tabID); index >= 0 {
			browser.active = index
			browser.focusAddress = browser.tabs[index].url == ""
		}
		return true
	}
	if tabID, found := id.In(hostBrowserTabCloseList); found {
		if index := browser.tabIndex(tabID); index >= 0 {
			state.closeHostBrowserTab(session, index)
		}
		return true
	}
	switch id {
	case hitHostBrowserClose:
		context, found := state.deck.Context(state.deck.ActiveWorkspace())
		if found {
			context.Open = false
			_ = state.deck.SetContext(state.deck.ActiveWorkspace(), context)
		}
		state.closeHostBrowser(session)
		state.fitWorkspaceTerminals(state.deck.ActiveWorkspace())
		return true
	case hitHostBrowserNewTab:
		browser.newTab()
		return true
	case hitHostBrowserAddress:
		if tab := browser.current(); tab != nil {
			tab.editing = true
		}
		return true
	case hitHostBrowserGo:
		state.navigateHostBrowser(session)
		return true
	case hitHostBrowserRetry:
		state.closeHostBrowserTunnel(session)
		for _, tab := range browser.tabs {
			tab.err = ""
		}
		state.startHostBrowserTunnel(session)
		return true
	}
	tab := browser.current()
	if tab == nil {
		return false
	}
	switch id {
	case hitHostBrowserBack:
		if tab.view != nil {
			tab.editing = false
			tab.view.Back()
		}
		return true
	case hitHostBrowserForward:
		if tab.view != nil {
			tab.editing = false
			tab.view.Forward()
		}
		return true
	case hitHostBrowserReload:
		if tab.view != nil {
			tab.editing = false
			tab.err = ""
			tab.view.Reload()
		}
		return true
	case hitHostBrowserExternal, hitHostBrowserLaunchExternal:
		target := ""
		if tab != nil {
			target = tab.url
			if target == "" {
				target = tab.address.String()
			}
		}
		state.openHostBrowserExternal(session, target)
		return true
	}
	return false
}

func (session *Session) canOpenHostBrowser() bool {
	return session != nil && session.hasServer() && !session.disconnected && session.conn != nil && session.phase == sshconn.PhaseConnected
}

func (state *State) openHostBrowserExternal(session *Session, target string) {
	if !session.canOpenHostBrowser() {
		return
	}
	browser := state.hostBrowserFor(session)
	if browser.tunnel == nil {
		state.startHostBrowserTunnel(session)
		if browser.tunnel == nil {
			state.showActionError(state.messages.HostBrowser.Title, state.messages.HostBrowser.Unavailable)
			return
		}
	}
	if browser.external != nil && browser.external.Running() {
		browser.external.Close()
		browser.external = nil
	}
	if target != "" {
		if normalized, err := normalizeHostBrowserURL(target); err == nil {
			target = normalized
		}
	}
	proxy := browser.tunnel.Status().ListenAddress
	ext, err := sshbrowser.Launch(proxy, target)
	if errors.Is(err, sshbrowser.ErrBrowserNotFound) {
		state.showActionError(state.messages.HostBrowser.Title, state.messages.HostBrowser.BrowserNotFound)
		return
	}
	if err != nil {
		state.showActionFailure(state.messages.HostBrowser.Title, err, state.messages.HostBrowser.BrowserStartFailed)
		return
	}
	browser.external = ext
}

// closeHostBrowserTunnel drops the pages and the forward but keeps the tabs, so
// reconnecting reopens them.
func (state *State) closeHostBrowserTunnel(session *Session) {
	browser := state.hostBrowserFor(session)
	browser.closeViews()
	if browser.external != nil {
		browser.external.Close()
		browser.external = nil
	}
	if browser.tunnel != nil {
		browser.tunnel.CloseAsync()
		browser.tunnel = nil
	}
}

func (state *State) hostBrowserKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.browser == nil || state.modal != modalNone || event.Repeat {
		return gfx.EventResult{}, false
	}
	browser := session.browser
	if tab := browser.current(); tab != nil && event.Target == hitHostBrowserAddress {
		switch event.Key {
		case gfx.KeyEnter:
			if _, composing := tab.address.Composition(); composing {
				return gfx.EventResult{}, false
			}
			state.navigateHostBrowser(session)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case gfx.KeyEscape:
			tab.editing = false
			tab.address = *text.NewEditor(tab.url)
			tab.err = ""
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	modifiers := gfx.ModifierControl | gfx.ModifierMeta
	if event.Modifiers&modifiers != 0 && event.Modifiers&^modifiers == 0 {
		switch strings.ToLower(event.Text) {
		case "l":
			browser.focusAddress = true
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitHostBrowserAddress}, true
		case "r":
			state.clickHostBrowser(hitHostBrowserReload)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}
