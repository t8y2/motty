package workbench

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// The outbound proxy: one SOCKS5 or HTTP CONNECT proxy that carries the first
// connection of every session, except to the destinations on its bypass list.
// (The Proxies page is something else: it routes a host through another host.)
// The proxy's password is kept in the saved-passwords vault, never in the state
// file.

// outboundProxyHits is the place of the controls this file paints.
var outboundProxyHits = tree.Feature("outbound-proxy")

var (
	hitSettingsProxyConfigure = tree.ID(outboundProxyHits, "settings-proxy-configure")
	hitProxyKindNone          = tree.ID(outboundProxyHits, "proxy-kind-none")
	hitProxyKindSOCKS5        = tree.ID(outboundProxyHits, "proxy-kind-socks5")
	hitProxyKindHTTP          = tree.ID(outboundProxyHits, "proxy-kind-http")
	hitOutboundProxyAddress   = tree.ID(outboundProxyHits, "outbound-proxy-address")
	hitOutboundProxyUser      = tree.ID(outboundProxyHits, "outbound-proxy-user")
	hitOutboundProxyPassword  = tree.ID(outboundProxyHits, "outbound-proxy-password")
	hitOutboundProxyBypass    = tree.ID(outboundProxyHits, "outbound-proxy-bypass")
	hitOutboundProxySubmit    = tree.ID(outboundProxyHits, "outbound-proxy-submit")
	hitOutboundProxyCancel    = tree.ID(outboundProxyHits, "outbound-proxy-cancel")
	hitOutboundProxyClose     = tree.ID(outboundProxyHits, "outbound-proxy-close")
	hitOutboundProxyDismiss   = tree.ID(outboundProxyHits, "outbound-proxy-dismiss")
)

const (
	proxyPasswordSecret = "outbound-proxy:password"
	defaultProxyBypass  = "localhost, 127.0.0.0/8, ::1"
	maxBypassEntries    = 50
	maxBypassEntryLen   = 100
)

// outboundProxy is the saved proxy; its password is not part of it.
type outboundProxy struct {
	kind    sshconn.ProxyKind
	address string
	user    string
	bypass  string
}

type outboundProxyForm struct {
	kind                            sshconn.ProxyKind
	address, user, password, bypass text.Editor
	dialogError                     string
	// text is the settings row's description, made for textKind, textFor (the
	// address) and textLanguage.
	text, textFor string
	textKind      sshconn.ProxyKind
	textLanguage  i18n.Language
}

func proxyKindString(kind sshconn.ProxyKind) string {
	switch kind {
	case sshconn.ProxySOCKS5:
		return "socks5"
	case sshconn.ProxyHTTPConnect:
		return "http"
	}
	return ""
}

func proxyKindFromString(value string) sshconn.ProxyKind {
	switch value {
	case "socks5":
		return sshconn.ProxySOCKS5
	case "http":
		return sshconn.ProxyHTTPConnect
	}
	return sshconn.ProxyNone
}

func (state *State) proxyKindName(kind sshconn.ProxyKind) string {
	m := state.messages.Tools
	switch kind {
	case sshconn.ProxySOCKS5:
		return m.ProxySOCKS5
	case sshconn.ProxyHTTPConnect:
		return m.ProxyHTTP
	}
	return m.ProxyNone
}

// outboundProxyDescription is what the settings row says about the proxy; it is
// made again only when the proxy changes.
func (state *State) outboundProxyDescription() string {
	proxy := state.proxy
	form := &state.proxyForm
	if form.text == "" || form.textKind != proxy.kind || form.textFor != proxy.address || form.textLanguage != state.language {
		form.textKind, form.textFor, form.textLanguage = proxy.kind, proxy.address, state.language
		if proxy.kind == sshconn.ProxyNone {
			state.proxyForm.text = state.messages.Tools.ProxyDescriptionNone
		} else {
			state.proxyForm.text = fmt.Sprintf(state.messages.Tools.ProxyDescriptionFmt, state.proxyKindName(proxy.kind), proxy.address)
		}
	}
	return state.proxyForm.text
}

// splitBypass reads the bypass list: entries separated by commas or spaces.
func splitBypass(value string) []string {
	var entries []string
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' }) {
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// validProxyAddress reports whether value is host:port with a usable port.
func validProxyAddress(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" || strings.ContainsAny(host, " \t\r\n\x00/\\") {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 1 && number <= 65535
}

// currentProxy is what a connection is given: the saved proxy with its password,
// or nil for none.
func (state *State) currentProxy() *sshconn.Proxy {
	saved := state.proxy
	if saved.kind == sshconn.ProxyNone || saved.address == "" {
		return nil
	}
	proxy := &sshconn.Proxy{Kind: saved.kind, Address: saved.address, Username: saved.user, Bypass: splitBypass(saved.bypass)}
	if saved.user != "" && state.secrets.vault != nil {
		password, found, err := state.secrets.vault.Get(proxyPasswordSecret)
		switch {
		case errors.Is(err, vault.ErrLocked):
			proxy.Err = errors.New(state.messages.Tools.ProxyPasswordLocked)
		case err == nil && found:
			proxy.Password = password
		}
	}
	return proxy
}

func (state *State) openOutboundProxyDialog() {
	form, saved := &state.proxyForm, state.proxy
	form.kind = saved.kind
	form.address, form.user, form.password, form.bypass = text.Editor{}, text.Editor{}, text.Editor{}, text.Editor{}
	form.address.Insert(saved.address)
	form.user.Insert(saved.user)
	bypass := saved.bypass
	if saved.kind == sshconn.ProxyNone && bypass == "" {
		bypass = defaultProxyBypass
	}
	form.bypass.Insert(bypass)
	form.dialogError = ""
	state.modal = modalOutboundProxy
}

func (state *State) clickOutboundProxy(id ui.ID) {
	form := &state.proxyForm
	switch id {
	case hitProxyKindNone:
		form.kind = sshconn.ProxyNone
	case hitProxyKindSOCKS5:
		form.kind = sshconn.ProxySOCKS5
	case hitProxyKindHTTP:
		form.kind = sshconn.ProxyHTTPConnect
	case hitOutboundProxySubmit:
		state.submitOutboundProxy()
	case hitOutboundProxyCancel, hitOutboundProxyClose, hitOutboundProxyDismiss:
		state.closeModal()
	}
}

func (state *State) submitOutboundProxy() {
	form, m := &state.proxyForm, state.messages.Tools
	address := strings.TrimSpace(form.address.String())
	user := strings.TrimSpace(form.user.String())
	bypass := strings.TrimSpace(form.bypass.String())
	password := form.password.String()
	if form.kind != sshconn.ProxyNone {
		switch {
		case !validProxyAddress(address):
			form.dialogError = m.ProxyErrorAddress
			return
		case len(user) > 255 || len(password) > 255:
			form.dialogError = m.ProxyErrorLogin
			return
		}
	}
	entries := splitBypass(bypass)
	if len(entries) > maxBypassEntries {
		form.dialogError = fmt.Sprintf(m.ProxyErrorBypassFmt, maxBypassEntries)
		return
	}
	for _, entry := range entries {
		if len(entry) > maxBypassEntryLen {
			form.dialogError = fmt.Sprintf(m.ProxyErrorBypassFmt, maxBypassEntries)
			return
		}
	}
	if form.kind != sshconn.ProxyNone && user != "" && password != "" {
		if state.secrets.vault == nil {
			form.dialogError = m.ProxyErrorNoVault
			return
		}
		if err := state.secrets.vault.Set(proxyPasswordSecret, password); err != nil {
			if errors.Is(err, vault.ErrLocked) {
				form.dialogError = m.ProxyPasswordLocked
			} else {
				form.dialogError = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			}
			return
		}
	}
	if (form.kind == sshconn.ProxyNone || user == "") && state.secrets.vault != nil {
		// No login any more: the password is not kept.
		if err := state.secrets.vault.Delete(proxyPasswordSecret); err != nil && !errors.Is(err, vault.ErrLocked) {
			form.dialogError = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			return
		}
	}
	state.proxy = outboundProxy{kind: form.kind, address: address, user: user, bypass: strings.Join(entries, ", ")}
	if form.kind == sshconn.ProxyNone {
		state.proxy.address, state.proxy.user = "", ""
	}
	state.closeModal()
	state.save()
}

func (state *State) paintOutboundProxyDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	form := &state.proxyForm
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitOutboundProxyDismiss, CloseID: hitOutboundProxyClose, InitialFocusID: hitOutboundProxyAddress,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: withError(312, form.dialogError), // the kind and four fields
	})
	layout := dialog.Begin(m.ProxyDialogTitle, m.ProxyDialogDescription)
	options := [...]components.SegmentOption{
		{ID: hitProxyKindNone, Label: m.ProxyNone},
		{ID: hitProxyKindSOCKS5, Label: m.ProxySOCKS5},
		{ID: hitProxyKindHTTP, Label: m.ProxyHTTP},
	}
	selected := hitProxyKindNone
	switch form.kind {
	case sshconn.ProxySOCKS5:
		selected = hitProxyKindSOCKS5
	case sshconn.ProxyHTTPConnect:
		selected = hitProxyKindHTTP
	}
	disabled := form.kind == sshconn.ProxyNone
	entries := [...]struct {
		id          ui.ID
		label       string
		placeholder string
		editor      *text.Editor
		secure      bool
	}{
		{hitOutboundProxyAddress, m.ProxyAddress, "127.0.0.1:1080", &form.address, false},
		{hitOutboundProxyUser, m.ProxyUser, "", &form.user, false},
		{hitOutboundProxyPassword, m.ProxyPassword, m.ProxyPasswordPlaceholder, &form.password, true},
		{hitOutboundProxyBypass, m.ProxyBypass, defaultProxyBypass, &form.bypass, false},
	}
	var kind flex.Box
	var slots [len(entries)]flex.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		kind = f.Slot().Height(34).MarginSides(0, 0, 14, 0)
		for index := range slots {
			slots[index] = f.Slot().Height(62).MarginSides(0, 0, 4, 0)
		}
		state.problemLine(f, form.dialogError)
	})
	f.End()
	components.BeginSegmentedControl(canvas, theme, kind.Rect(), options[:], selected).Paint()
	for index, entry := range entries {
		content := components.BeginField(canvas, theme, slots[index].Rect()).Paint(entry.label, "", "")
		value := entry.editor.String()
		focused := canvas.Focused() == entry.id
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: entry.id, Label: entry.label, Value: value, Placeholder: entry.placeholder, Secure: entry.secure,
			Focused: focused, PlatformEditing: focused && value != "", Disabled: disabled && entry.id != hitOutboundProxyBypass,
		})
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitOutboundProxyCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitOutboundProxySubmit, Label: m.ProxySave},
	)
	dialog.End()
}

// paintOutboundProxyRow is the Outbound proxy row of the Connection settings.
func (state *State) paintOutboundProxyRow(page *settingsPage) {
	m := state.messages.Tools
	page.actionRow(m.ProxyRow, state.outboundProxyDescription(), false,
		components.ButtonAction{ID: hitSettingsProxyConfigure, Label: m.ProxyConfigure, Variant: components.ButtonOutline})
}
