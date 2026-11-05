package workbench

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

var hostBrowserHits = tree.Feature("host-browser")

var (
	hitHostBrowserOpen     = tree.ID(shellHits, "host-browser-open")
	hitHostBrowserClose    = tree.ID(hostBrowserHits, "close")
	hitHostBrowserBack     = tree.ID(hostBrowserHits, "back")
	hitHostBrowserForward  = tree.ID(hostBrowserHits, "forward")
	hitHostBrowserReload   = tree.ID(hostBrowserHits, "reload")
	hitHostBrowserAddress  = tree.ID(hostBrowserHits, "address")
	hitHostBrowserGo       = tree.ID(hostBrowserHits, "go")
	hitHostBrowserExternal = tree.ID(hostBrowserHits, "external")
	hitHostBrowserRetry    = tree.ID(hostBrowserHits, "retry")
)

type hostBrowserView interface {
	ID() ui.NativeViewID
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

type hostBrowser struct {
	open         bool
	host         hostBrowserHost
	ready        bool
	view         hostBrowserView
	tunnel       *portforward.Forward
	address      text.Editor
	url          string
	err          string
	editing      bool
	focusAddress bool
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
				state.openHostBrowserPage(&state.sessions[index])
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
		context.Kind = layoutcore.ContextBrowser
		context.Open = true
		context.Side = layoutcore.ContextRight
		if context.Width < 360 {
			context.Width = 420
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
	if browser.view != nil {
		browser.view.Close()
		browser.view = nil
	}
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
	state.openHostBrowserPage(session)
}

func (state *State) openHostBrowserPage(session *Session) {
	browser := state.hostBrowserFor(session)
	if !browser.open || browser.view != nil || browser.tunnel == nil || !browser.ready || browser.host == nil || state.closed {
		return
	}
	proxy := browser.tunnel.Status().ListenAddress
	target := browser.url
	if target == "" {
		target = "http://127.0.0.1/"
	}
	view, err := browser.host.Open(app.WebViewConfig{URL: target, Proxy: proxy, OnLoad: func(load app.WebLoad) {
		if normalized, normalizeErr := normalizeHostBrowserURL(load.URL); normalizeErr == nil {
			browser.url = normalized
			if !browser.editing {
				state.setHostBrowserAddress(browser, normalized)
			}
		}
		if load.Phase == app.WebLoadStarted {
			browser.err = ""
		}
	}})
	if err != nil {
		browser.err = state.messages.HostBrowser.Unsupported
		return
	}
	browser.err = ""
	browser.url = target
	state.setHostBrowserAddress(browser, target)
	browser.view = view
}

func (state *State) setHostBrowserAddress(browser *hostBrowser, target string) {
	browser.address = *text.NewEditor(target)
}

func (state *State) navigateHostBrowser(session *Session) {
	browser := state.hostBrowserFor(session)
	target, err := normalizeHostBrowserURL(browser.address.String())
	if err != nil {
		browser.err = state.messages.HostBrowser.InvalidAddress
		return
	}
	browser.err = ""
	browser.url = target
	state.setHostBrowserAddress(browser, target)
	if browser.view != nil {
		browser.view.Load(target)
		browser.view.Focus()
	} else {
		state.openHostBrowserPage(session)
	}
}

func (state *State) clickHostBrowser(id ui.ID) bool {
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
	case hitHostBrowserAddress:
		browser.editing = true
		return true
	case hitHostBrowserGo:
		browser.editing = false
		state.navigateHostBrowser(session)
		return true
	case hitHostBrowserBack:
		if browser.view != nil {
			browser.editing = false
			browser.view.Back()
		}
		return true
	case hitHostBrowserForward:
		if browser.view != nil {
			browser.editing = false
			browser.view.Forward()
		}
		return true
	case hitHostBrowserReload:
		if browser.view != nil {
			browser.editing = false
			browser.err = ""
			browser.view.Reload()
		}
		return true
	case hitHostBrowserExternal:
		if target, err := normalizeHostBrowserURL(browser.address.String()); err == nil {
			state.openExternal(target)
		}
		return true
	case hitHostBrowserRetry:
		state.closeHostBrowserTunnel(session)
		state.startHostBrowserTunnel(session)
		return true
	}
	return false
}

func (state *State) closeHostBrowserTunnel(session *Session) {
	browser := state.hostBrowserFor(session)
	if browser.view != nil {
		browser.view.Close()
		browser.view = nil
	}
	if browser.tunnel != nil {
		browser.tunnel.CloseAsync()
		browser.tunnel = nil
	}
}

func (state *State) hostBrowserKey(event ui.KeyEvent) (ui.EventResult, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.browser == nil || state.modal != modalNone || event.Repeat {
		return ui.EventResult{}, false
	}
	browser := session.browser
	if event.Target == hitHostBrowserAddress {
		switch event.Key {
		case ui.KeyEnter:
			if _, composing := browser.address.Composition(); composing {
				return ui.EventResult{}, false
			}
			browser.editing = false
			state.navigateHostBrowser(session)
			return ui.EventResult{Handled: true, Repaint: true}, true
		case ui.KeyEscape:
			browser.editing = false
			state.setHostBrowserAddress(browser, browser.url)
			browser.err = ""
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	modifiers := ui.ModifierControl | ui.ModifierMeta
	if event.Modifiers&modifiers != 0 && event.Modifiers&^modifiers == 0 {
		switch strings.ToLower(event.Text) {
		case "l":
			browser.focusAddress = true
			return ui.EventResult{Handled: true, Repaint: true, Focus: hitHostBrowserAddress}, true
		case "r":
			state.clickHostBrowser(hitHostBrowserReload)
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return ui.EventResult{}, false
}
