package workbench

import (
	"crypto/sha256"
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
	hitHostProxyConfigure     = tree.ID(outboundProxyHits, "host-proxy-configure")
	hitProxyKindInherit       = tree.ID(outboundProxyHits, "proxy-kind-inherit")
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
	proxyPasswordSecret     = "outbound-proxy:password"
	hostProxyPasswordPrefix = "host-proxy:"
	defaultProxyBypass      = "localhost, 127.0.0.0/8, ::1"
	maxBypassEntries        = 50
	maxBypassEntryLen       = 100
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
	forHost, inherit                bool
	address, user, password, bypass text.Editor
	dialogError                     string
	// text is the settings row's description, made for textKind, textFor (the
	// address) and textLanguage.
	text, textFor string
	textKind      sshconn.ProxyKind
	textLanguage  i18n.Language
}

func cloneOutboundProxy(proxy *outboundProxy) *outboundProxy {
	if proxy == nil {
		return nil
	}
	copy := *proxy
	return &copy
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

// savedHostProxy preserves the difference between inheriting the global proxy
// (nil) and explicitly connecting directly (kind "direct").
func savedHostProxy(proxy *outboundProxy) *savedOutboundProxy {
	if proxy == nil {
		return nil
	}
	kind := proxyKindString(proxy.kind)
	if proxy.kind == sshconn.ProxyNone {
		kind = "direct"
	}
	return &savedOutboundProxy{Kind: kind, Address: proxy.address, User: proxy.user, Bypass: proxy.bypass}
}

// restoredHostProxy drops malformed overrides to inheritance. A broken saved
// value must not silently force a direct connection.
func restoredHostProxy(saved *savedOutboundProxy) *outboundProxy {
	if saved == nil {
		return nil
	}
	if saved.Kind == "direct" {
		return &outboundProxy{}
	}
	kind := proxyKindFromString(saved.Kind)
	if kind == sshconn.ProxyNone || !validProxyAddress(saved.Address) || len(saved.User) > 255 {
		return nil
	}
	entries := splitBypass(saved.Bypass)
	if len(entries) > maxBypassEntries {
		return nil
	}
	for _, entry := range entries {
		if len(entry) > maxBypassEntryLen {
			return nil
		}
	}
	return &outboundProxy{kind: kind, address: saved.Address, user: saved.User, bypass: strings.Join(entries, ", ")}
}

// hostProxyPasswordSecret binds a proxy password to the login it was entered
// for. A changed kind, address or user therefore cannot reuse the old secret:
// the lookup name changes, and the user must enter the new password.
func hostProxyPasswordSecret(catalogID string, proxy *outboundProxy) string {
	if proxy == nil || proxy.kind == sshconn.ProxyNone || proxy.user == "" {
		return ""
	}
	login := sha256.Sum256([]byte(proxyKindString(proxy.kind) + "\x00" + proxy.address + "\x00" + proxy.user))
	return fmt.Sprintf("%s%s:%x:password", hostProxyPasswordPrefix, catalogID, login[:8])
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
func (state *State) proxyWithPassword(saved outboundProxy, passwordSecret string) *sshconn.Proxy {
	if saved.kind == sshconn.ProxyNone || saved.address == "" {
		return nil
	}
	proxy := &sshconn.Proxy{Kind: saved.kind, Address: saved.address, Username: saved.user, Bypass: splitBypass(saved.bypass)}
	if saved.user != "" && state.secrets.vault != nil {
		password, found, err := state.secrets.vault.Get(passwordSecret)
		switch {
		case errors.Is(err, vault.ErrLocked):
			proxy.Err = errors.New(state.messages.Tools.ProxyPasswordLocked)
		case err == nil && found:
			proxy.Password = password
		}
	}
	return proxy
}

func (state *State) currentProxy() *sshconn.Proxy {
	return state.proxyWithPassword(state.proxy, proxyPasswordSecret)
}

// proxyForHost resolves a host's policy at connection time so inherited hosts
// observe later changes to the global proxy.
func (state *State) proxyForHost(host Host) *sshconn.Proxy {
	if host.Protocol == protocolSerial {
		return nil
	}
	if host.Proxy == nil {
		return state.currentProxy()
	}
	return state.proxyWithPassword(*host.Proxy, hostProxyPasswordSecret(host.CatalogID, host.Proxy))
}

// proxyRouteKey identifies the non-secret route a live connection used. It is
// intentionally conservative: a changed bypass list starts a fresh connection
// even when both lists happen to make this one target take the same path.
func proxyRouteKey(proxy *sshconn.Proxy) string {
	if proxy == nil || proxy.Kind == sshconn.ProxyNone || proxy.Address == "" {
		return ""
	}
	var key strings.Builder
	write := func(value string) {
		key.WriteString(strconv.Itoa(len(value)))
		key.WriteByte(':')
		key.WriteString(value)
	}
	key.WriteString(strconv.Itoa(int(proxy.Kind)))
	key.WriteByte('|')
	write(proxy.Address)
	key.WriteByte('|')
	write(proxy.Username)
	for _, entry := range proxy.Bypass {
		key.WriteByte('|')
		write(entry)
	}
	return key.String()
}

func (state *State) hostProxyPrecheck() string {
	if state.editor.protocol == protocolSerial {
		return ""
	}
	proxy := state.editor.proxy
	if proxy == nil || proxy.kind == sshconn.ProxyNone || proxy.user == "" || state.editor.proxyPassword == "" {
		return ""
	}
	vaultFile := state.secrets.vault
	switch {
	case vaultFile == nil:
		return state.messages.Tools.ProxyErrorNoVault
	case vaultFile.Unlocked():
		return ""
	case vaultFile.HasMasterPassword():
		return state.messages.Tools.ProxyPasswordLocked
	case vaultFile.Prepare() != nil:
		return state.messages.Tools.ProxyErrorNoVault
	}
	return ""
}

func sameProxyLogin(left, right *outboundProxy) bool {
	return left != nil && right != nil && left.kind == right.kind && left.address == right.address && left.user == right.user && left.user != ""
}

// hostProxyChanges plans the proxy-password mutations for the host dialog.
// The caller applies them together with the host password so a failed submit
// cannot leave either credential half-changed.
func (state *State) hostProxyChanges(host Host, previous, source *Host) (note string, changes []vault.Change, failure string) {
	vaultFile := state.secrets.vault
	proxy := host.Proxy
	needsPassword := proxy != nil && proxy.kind != sshconn.ProxyNone && proxy.user != ""
	destination := hostProxyPasswordSecret(host.CatalogID, proxy)
	previousSecret := ""
	if previous != nil {
		previousSecret = hostProxyPasswordSecret(host.CatalogID, previous.Proxy)
	}
	if needsPassword && state.editor.proxyPassword != "" {
		if vaultFile == nil {
			return "", nil, state.messages.Tools.ProxyErrorNoVault
		}
		changes = append(changes, vault.Change{Name: destination, Value: state.editor.proxyPassword})
		if previousSecret != "" && previousSecret != destination && vaultFile.Unlocked() {
			changes = append(changes, vault.Change{Name: previousSecret, Delete: true})
		}
		return "", changes, ""
	}
	if needsPassword && previous == nil && source != nil && sameProxyLogin(proxy, source.Proxy) && vaultFile != nil && vaultFile.Len() > 0 {
		if !vaultFile.Unlocked() {
			return state.messages.Tools.HostProxyPasswordNotCarried, nil, ""
		}
		origin := hostProxyPasswordSecret(source.CatalogID, source.Proxy)
		if origin == "" {
			return "", nil, ""
		}
		value, found, err := vaultFile.Get(origin)
		if err != nil {
			return "", nil, fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
		}
		if found {
			return "", []vault.Change{{Name: destination, Value: value}}, ""
		}
	}
	if previous != nil && needsPassword && previous.Proxy != nil &&
		previous.Proxy.kind != sshconn.ProxyNone && previous.Proxy.user != "" &&
		!sameProxyLogin(proxy, previous.Proxy) {
		// A password is bound to the login it was entered for; never carry it
		// over to a different proxy endpoint or account.
		return "", nil, state.messages.Tools.HostProxyPasswordChanged
	}
	hadPassword := previous != nil && previous.Proxy != nil && previous.Proxy.kind != sshconn.ProxyNone && previous.Proxy.user != ""
	if !needsPassword && hadPassword && vaultFile != nil {
		// A locked master-password vault must be opened before its secret can be
		// retired. The default system-keystore vault can be opened by Apply.
		if !vaultFile.Unlocked() && vaultFile.HasMasterPassword() && vaultFile.Len() > 0 {
			return "", nil, state.messages.Tools.ProxyPasswordLocked
		}
		if previousSecret != "" {
			return "", []vault.Change{{Name: previousSecret, Delete: true}}, ""
		}
	}
	return "", nil, ""
}

func (state *State) openOutboundProxyDialog() {
	form, saved := &state.proxyForm, state.proxy
	form.kind, form.forHost, form.inherit = saved.kind, false, false
	state.loadProxyForm(saved, "")
	state.modal = modalOutboundProxy
}

func (state *State) openHostProxyDialog() {
	form := &state.proxyForm
	form.forHost, form.inherit = true, state.editor.proxy == nil
	saved := outboundProxy{}
	if state.editor.proxy != nil {
		saved = *state.editor.proxy
	}
	form.kind = saved.kind
	state.loadProxyForm(saved, state.editor.proxyPassword)
	state.modal = modalOutboundProxy
}

func (state *State) loadProxyForm(saved outboundProxy, password string) {
	form := &state.proxyForm
	form.address, form.user, form.password, form.bypass = text.Editor{}, text.Editor{}, text.Editor{}, text.Editor{}
	form.address.Insert(saved.address)
	form.user.Insert(saved.user)
	form.password.Insert(password)
	bypass := saved.bypass
	if saved.kind == sshconn.ProxyNone && bypass == "" {
		bypass = defaultProxyBypass
	}
	form.bypass.Insert(bypass)
	form.dialogError = ""
}

func (state *State) clickOutboundProxy(id ui.ID) {
	form := &state.proxyForm
	switch id {
	case hitProxyKindInherit:
		form.inherit, form.kind = true, sshconn.ProxyNone
	case hitProxyKindNone:
		form.inherit, form.kind = false, sshconn.ProxyNone
	case hitProxyKindSOCKS5:
		form.inherit, form.kind = false, sshconn.ProxySOCKS5
	case hitProxyKindHTTP:
		form.inherit, form.kind = false, sshconn.ProxyHTTPConnect
	case hitOutboundProxySubmit:
		state.submitOutboundProxy()
	case hitOutboundProxyCancel, hitOutboundProxyClose, hitOutboundProxyDismiss:
		if form.forHost {
			state.modal = modalNewHost
		} else {
			state.closeModal()
		}
	}
}

func (state *State) proxyFormValue() (outboundProxy, string, bool) {
	form, m := &state.proxyForm, state.messages.Tools
	address := strings.TrimSpace(form.address.String())
	user := strings.TrimSpace(form.user.String())
	bypass := strings.TrimSpace(form.bypass.String())
	password := form.password.String()
	if form.kind != sshconn.ProxyNone {
		switch {
		case !validProxyAddress(address):
			form.dialogError = m.ProxyErrorAddress
			return outboundProxy{}, "", false
		case len(user) > 255 || len(password) > 255:
			form.dialogError = m.ProxyErrorLogin
			return outboundProxy{}, "", false
		}
	}
	entries := splitBypass(bypass)
	if len(entries) > maxBypassEntries {
		form.dialogError = fmt.Sprintf(m.ProxyErrorBypassFmt, maxBypassEntries)
		return outboundProxy{}, "", false
	}
	for _, entry := range entries {
		if len(entry) > maxBypassEntryLen {
			form.dialogError = fmt.Sprintf(m.ProxyErrorBypassFmt, maxBypassEntries)
			return outboundProxy{}, "", false
		}
	}
	saved := outboundProxy{kind: form.kind, address: address, user: user, bypass: strings.Join(entries, ", ")}
	if form.kind == sshconn.ProxyNone {
		saved.address, saved.user = "", ""
	}
	form.dialogError = ""
	return saved, password, true
}

func (state *State) submitOutboundProxy() {
	form, m := &state.proxyForm, state.messages.Tools
	if form.forHost && form.inherit {
		state.editor.proxy, state.editor.proxyPassword = nil, ""
		state.modal = modalNewHost
		return
	}
	saved, password, ok := state.proxyFormValue()
	if !ok {
		return
	}
	if form.forHost {
		state.editor.proxy, state.editor.proxyPassword = cloneOutboundProxy(&saved), password
		state.modal = modalNewHost
		return
	}
	if saved.kind != sshconn.ProxyNone && saved.user != "" && password != "" {
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
	if (saved.kind == sshconn.ProxyNone || saved.user == "") && state.secrets.vault != nil {
		// No login any more: the password is not kept.
		if err := state.secrets.vault.Delete(proxyPasswordSecret); err != nil && !errors.Is(err, vault.ErrLocked) {
			form.dialogError = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
			return
		}
	}
	state.proxy = saved
	state.closeModal()
	state.save()
}

func (state *State) paintOutboundProxyDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	form := &state.proxyForm
	title, description := m.ProxyDialogTitle, m.ProxyDialogDescription
	if form.forHost {
		title, description = m.HostProxyDialogTitle, m.HostProxyDialogDescription
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitOutboundProxyDismiss, CloseID: hitOutboundProxyClose, InitialFocusID: hitOutboundProxyAddress,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: withError(312, form.dialogError), // the kind and four fields
	})
	layout := dialog.Begin(title, description)
	options := [...]components.SegmentOption{
		{ID: hitProxyKindInherit, Label: m.HostProxyInherit},
		{ID: hitProxyKindNone, Label: m.HostProxyDirect},
		{ID: hitProxyKindSOCKS5, Label: m.ProxySOCKS5},
		{ID: hitProxyKindHTTP, Label: m.ProxyHTTP},
	}
	selected := hitProxyKindNone
	if form.inherit {
		selected = hitProxyKindInherit
	} else {
		switch form.kind {
		case sshconn.ProxySOCKS5:
			selected = hitProxyKindSOCKS5
		case sshconn.ProxyHTTPConnect:
			selected = hitProxyKindHTTP
		}
	}
	visibleOptions := options[:]
	if !form.forHost {
		options[1].Label = m.ProxyNone
		visibleOptions = options[1:]
	}
	disabled := form.inherit || form.kind == sshconn.ProxyNone
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
	components.BeginSegmentedControl(canvas, theme, kind.Rect(), visibleOptions, selected).Paint()
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

func (state *State) hostProxyDescription() string {
	m, proxy := state.messages.Tools, state.editor.proxy
	if proxy == nil {
		return m.HostProxyDescriptionInherit
	}
	if proxy.kind == sshconn.ProxyNone {
		return m.HostProxyDescriptionDirect
	}
	return fmt.Sprintf(m.HostProxyDescriptionFmt, state.proxyKindName(proxy.kind), proxy.address)
}

func (state *State) paintHostProxyRow(canvas *ui.Canvas, bounds ui.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(m.HostProxyRow, "", "")
	button := components.BeginButton(canvas, theme, hitHostProxyConfigure, ui.Rect{}).XSmall().SizeToFit(m.ProxyConfigure)
	var action flex.Box
	f := state.partLayout.Begin(canvas, theme, content)
	f.Row().Fill().Items(flex.Center).Gap(8).Children(func() {
		f.Text(state.hostProxyDescription()).FontSize(metrics.FontSizeS).Color(state.pal.muted).Ellipsis().Grow(1)
		action = f.Slot().Size(button.BoundsWidth(), 24)
	})
	f.End()
	components.BeginButton(canvas, theme, hitHostProxyConfigure, action.Rect()).XSmall().Outline(m.ProxyConfigure)
	state.rememberHostControl(hitHostProxyConfigure, action.Rect())
}

// paintOutboundProxyRow is the Outbound proxy row of the Connection settings.
func (state *State) paintOutboundProxyRow(page *settingsPage) {
	m := state.messages.Tools
	page.actionRow(m.ProxyRow, state.outboundProxyDescription(), false,
		components.ButtonAction{ID: hitSettingsProxyConfigure, Label: m.ProxyConfigure, Variant: components.ButtonOutline})
}
