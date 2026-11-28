package workbench

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// copyInfoHits is the place of the confirmation dialog controls for copying
// credentials onto the system clipboard.
var copyInfoHits = tree.Feature("copy-info")

var (
	hitCopyCredentialConfirmDismiss = tree.ID(copyInfoHits, "copy-credential-confirm-dismiss")
	hitCopyCredentialConfirmClose   = tree.ID(copyInfoHits, "copy-credential-confirm-close")
	hitCopyCredentialConfirmCancel  = tree.ID(copyInfoHits, "copy-credential-confirm-cancel")
	hitCopyCredentialConfirmOK      = tree.ID(copyInfoHits, "copy-credential-confirm-ok")
)

// defaultSecretClipboardTTL is how long a copied credential remains on the
// system clipboard before Motty clears it (provided nothing else replaced it).
const defaultSecretClipboardTTL = 30 * time.Second

// copyCredentialConfirm remembers the pending credential copy while the
// confirmation dialog is open. No vault or keystore read happens until the user
// explicitly confirms.
type copyCredentialConfirm struct {
	host        Host
	includeInfo bool
}

// secretClipboardState tracks the active auto-clear timer for a credential that
// Motty placed on the system clipboard. Only the SHA-256 digest of the copied
// text is kept in memory so plaintext secrets do not linger in State.
type secretClipboardState struct {
	timer  *time.Timer
	serial uint64
	digest [32]byte
	active bool
}

// connectionAddressAndPort is the endpoint text the copy actions place on the
// clipboard. Portless connections, including local, WSL and serial hosts, have
// nothing useful to copy under that label.
func connectionAddressAndPort(host Host) string {
	if host.isLocal() || host.Protocol == protocolSerial || host.Address == "" || host.Port == 0 {
		return ""
	}
	return net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
}

// connectionHostAddress is the endpoint text without its port: what a user
// pastes where only the host matters. Local shells, WSL distributions and
// serial devices have no network address, so none copy anything.
func connectionHostAddress(host Host) string {
	if host.isLocal() || host.Protocol == protocolSerial {
		return ""
	}
	return host.Address
}

// connectionCopyUsername is the account name offered by the Copy Info submenu.
// SSH falls back to the OS user as the actual connection does; desktop and
// telnet hosts only report an explicit login so a password-only VNC host does
// not fabricate the local OS user.
func (state *State) connectionCopyUsername(host Host) string {
	switch {
	case host.ID == localHostID || host.Protocol == protocolSerial:
		return ""
	case host.isWSL():
		return host.User
	case host.isSSH():
		return state.login(host)
	default:
		return state.storedLogin(host)
	}
}

// hostPasswordSecretNameForCopy is the vault entry holding the password of
// host: its shared identity's secret when it uses one, otherwise the host's
// own saved password.
func (state *State) hostPasswordSecretNameForCopy(host Host) (string, bool) {
	switch host.Protocol {
	case protocolSSH:
		if !host.Auth.usesPassword() {
			return "", false
		}
	case protocolVNC, protocolRDP:
	default:
		return "", false
	}
	if state.usesCredential(host) {
		return credentialSecretName(host.Credential), true
	}
	if host.CatalogID == "" {
		return "", false
	}
	return hostPasswordSecretName(host)
}

// hostPrivateKeyForCopy returns the pinned SSH identity whose private key text
// can be copied for host. Security keys hold no copyable private key material.
func (state *State) hostPrivateKeyForCopy(host Host) (identityRecord, bool) {
	if !host.isSSH() || host.Auth != authPrivateKey {
		return identityRecord{}, false
	}
	fingerprint := strings.TrimSpace(host.Identity)
	if fingerprint == "" {
		return identityRecord{}, false
	}
	identity, ok := state.identityForFingerprint(fingerprint)
	if !ok || identity.Error != "" || identity.SecurityKey {
		return identityRecord{}, false
	}
	return identity, true
}

// hostCredentialMayExist reports whether host can have a copyable credential
// using only in-memory metadata and vault record count (vault.Len). It never
// decrypts records or calls the OS keystore while painting or expanding menus.
func (state *State) hostCredentialMayExist(host Host) bool {
	if _, ok := state.hostPrivateKeyForCopy(host); ok {
		return true
	}
	if _, ok := state.hostPasswordSecretNameForCopy(host); !ok {
		return false
	}
	vaultFile := state.secrets.vault
	return vaultFile != nil && vaultFile.Len() > 0
}

func connectionProtocolName(host Host) string {
	switch host.Protocol {
	case protocolSSH:
		if host.mosh() {
			return "Mosh"
		}
		return "SSH"
	case protocolTelnet:
		return "Telnet"
	case protocolSerial:
		return "Serial"
	case protocolVNC:
		return "VNC"
	case protocolRDP:
		return "RDP"
	case protocolWSL:
		return "WSL"
	default:
		return "SSH"
	}
}

// resolvedCopyCredential holds the resolved secret material to copy for a host:
// either a password or a private key (with its optional saved passphrase).
type resolvedCopyCredential struct {
	password   string
	privateKey string
	passphrase string
}

// formatConnectionInfo formats host's connection details as copyable lines.
// includeCredentials appends the resolved password or private key (plus saved
// key passphrase when present). Local shells have no remote connection info.
func (state *State) formatConnectionInfo(host Host, includeCredentials bool, cred resolvedCopyCredential) string {
	if host.ID == localHostID {
		return ""
	}
	address := host.Address
	if host.isWSL() && address == "" {
		address = state.messages.WSL.DefaultDistribution
	}
	if address == "" && host.Label == "" {
		return ""
	}
	m := state.messages
	lines := make([]string, 0, 7)
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
		}
	}
	add(m.CopyInfoFieldName, host.Label)
	add(m.CopyInfoFieldProtocol, connectionProtocolName(host))
	add(m.FieldAddress, address)
	if host.Port != 0 && host.Protocol != protocolSerial && !host.isWSL() {
		add(m.FieldPort, strconv.Itoa(int(host.Port)))
	}
	add(m.FieldUser, state.connectionCopyUsername(host))
	if includeCredentials {
		if cred.password != "" {
			add(m.FieldPassword, cred.password)
		}
		if cred.passphrase != "" {
			add(m.KeyPassphraseLabel, cred.passphrase)
		}
		if keyText := strings.TrimRight(cred.privateKey, "\r\n"); keyText != "" {
			lines = append(lines, fmt.Sprintf("%s:\n%s", m.FieldIdentity, keyText))
		}
	}
	return strings.Join(lines, "\n")
}

// resolveHostCredentialForCopy reads the host's copyable credential (private
// key or saved password) strictly on explicit user confirmation, opening the
// legacy vault unlock dialog when needed and reporting read errors via action
// notice and status line. retry runs after unlock.
func (state *State) resolveHostCredentialForCopy(host Host, includePassphrase bool, title string, retry func()) (resolvedCopyCredential, bool) {
	if identity, ok := state.hostPrivateKeyForCopy(host); ok {
		vaultFile := state.secrets.vault
		needsVault := identity.Stored || (includePassphrase && vaultFile != nil && vaultFile.Len() > 0)
		if needsVault && vaultFile != nil && vaultFile.NeedsConversion() {
			state.openVaultUnlock(nil, retry)
			return resolvedCopyCredential{}, false
		}
		data, err := state.identityKeyData(identity)
		if err != nil {
			state.showActionError(title, err.Error())
			return resolvedCopyCredential{}, false
		}
		keyText := string(data)
		clear(data)
		var passphrase string
		if includePassphrase && vaultFile != nil && vaultFile.Len() > 0 && !vaultFile.NeedsConversion() {
			if value, found, getErr := vaultFile.Get(passphraseSecretName(identity.Path)); getErr == nil && found {
				passphrase = value
			}
		}
		return resolvedCopyCredential{privateKey: keyText, passphrase: passphrase}, true
	}

	name, hasSecret := state.hostPasswordSecretNameForCopy(host)
	if !hasSecret {
		return resolvedCopyCredential{}, true
	}
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 {
		return resolvedCopyCredential{}, true
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, retry)
		return resolvedCopyCredential{}, false
	}
	value, found, err := vaultFile.Get(name)
	if err != nil {
		state.showActionError(title, state.vaultErrorText(err))
		return resolvedCopyCredential{}, false
	}
	if !found {
		return resolvedCopyCredential{}, true
	}
	return resolvedCopyCredential{password: value}, true
}

// askCopyCredentialConfirm prompts the user before copying plaintext secrets
// onto the system clipboard.
func (state *State) askCopyCredentialConfirm(host Host, includeInfo bool) {
	state.copyCredentialConfirm = copyCredentialConfirm{host: host, includeInfo: includeInfo}
	state.modal = modalCopyCredentialConfirm
}

func (state *State) clickCopyCredentialConfirmDialog(id gfx.ID) {
	switch id {
	case hitCopyCredentialConfirmDismiss, hitCopyCredentialConfirmClose, hitCopyCredentialConfirmCancel:
		state.modal = modalNone
		state.copyCredentialConfirm = copyCredentialConfirm{}
	case hitCopyCredentialConfirmOK:
		pending := state.copyCredentialConfirm
		state.modal = modalNone
		state.copyCredentialConfirm = copyCredentialConfirm{}
		if pending.includeInfo {
			state.copyHostConnectionInfoWithCredentials(pending.host)
			return
		}
		state.copyHostCredential(pending.host)
	}
}

// copyCredentialConfirmKey makes Escape cancel and Enter on the confirm button
// confirm.
func (state *State) copyCredentialConfirmKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalCopyCredentialConfirm || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		state.clickCopyCredentialConfirmDialog(hitCopyCredentialConfirmCancel)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == gfx.KeyEnter && event.Target == hitCopyCredentialConfirmOK {
		state.clickCopyCredentialConfirmDialog(hitCopyCredentialConfirmOK)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// declareCopyCredentialConfirmDialog warns that plaintext credentials will be
// placed on the system clipboard and automatically cleared after a short TTL.
func (state *State) declareCopyCredentialConfirmDialog(c ui.C) {
	m := state.messages
	pending := state.copyCredentialConfirm
	title := m.CopyCredentialConfirmTitle
	if pending.includeInfo {
		title = m.CopyCredentialInfoConfirmTitle
	}
	target := strings.TrimSpace(pending.host.Label)
	if target == "" {
		target = strings.TrimSpace(pending.host.Address)
	}
	seconds := secretClipboardDisplaySeconds(state.secretClipboardClearAfter())
	description := fmt.Sprintf(m.CopyCredentialConfirmDescriptionFmt, target, seconds)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), dialogViewport(c), 520, description, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: 520, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitCopyCredentialConfirmDismiss, CloseID: hitCopyCredentialConfirmClose, InitialFocusID: hitCopyCredentialConfirmCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitCopyCredentialConfirmCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitCopyCredentialConfirmOK, Label: m.CopyCredentialConfirmAction, Variant: components.ButtonDestructive},
		)
	})
}

func (state *State) copyHostCredential(host Host) {
	title := state.messages.CopyCredential
	cred, ok := state.resolveHostCredentialForCopy(host, false, title, func() {
		state.copyHostCredential(host)
	})
	if !ok {
		return
	}
	if cred.privateKey != "" {
		state.copySecretClipboardText(title, cred.privateKey)
		return
	}
	if cred.password != "" {
		state.copySecretClipboardText(title, cred.password)
		return
	}
	state.showActionInfo(title, state.messages.CopyCredentialMissing)
}

func (state *State) copyHostConnectionInfo(host Host) {
	state.copyConnectionText(state.formatConnectionInfo(host, false, resolvedCopyCredential{}))
}

func (state *State) copyHostConnectionInfoWithCredentials(host Host) {
	title := state.messages.CopyConnectionInfoWithCredentials
	cred, ok := state.resolveHostCredentialForCopy(host, true, title, func() {
		state.copyHostConnectionInfoWithCredentials(host)
	})
	if !ok {
		return
	}
	if cred.privateKey == "" && cred.password == "" {
		state.showActionInfo(title, state.messages.CopyCredentialMissing)
		return
	}
	state.copySecretClipboardText(title, state.formatConnectionInfo(host, true, cred))
}

func (state *State) secretClipboardClearAfter() time.Duration {
	if state.secretClipboardTTL > 0 {
		return state.secretClipboardTTL
	}
	return defaultSecretClipboardTTL
}

func secretClipboardDisplaySeconds(d time.Duration) int {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

// copySecretClipboardText copies a sensitive value to the system clipboard and
// arms a one-shot timer to clear the clipboard once the TTL expires (only if
// the clipboard still holds the same content).
func (state *State) copySecretClipboardText(title, value string) {
	if value == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(value); err != nil {
		state.showActionError(title, err.Error())
		return
	}
	state.stopSecretClipboardClear()
	ttl := state.secretClipboardClearAfter()
	state.secretClipboard.serial++
	serial := state.secretClipboard.serial
	state.secretClipboard.digest = sha256.Sum256([]byte(value))
	state.secretClipboard.active = true
	state.secretClipboard.timer = time.AfterFunc(ttl, func() {
		state.aiPost(func(s *State) {
			s.clearSecretClipboardIfMatch(serial)
		})
	})
	state.showActionInfo(title, fmt.Sprintf(state.messages.CopyCredentialCopiedAutoClearFmt, secretClipboardDisplaySeconds(ttl)))
}

// clearSecretClipboardIfMatch clears the system clipboard if this timer is
// still current and the clipboard text still matches the copied secret's hash.
func (state *State) clearSecretClipboardIfMatch(serial uint64) {
	if !state.secretClipboard.active || state.secretClipboard.serial != serial {
		return
	}
	digest := state.secretClipboard.digest
	state.stopSecretClipboardClear()
	if state.options.ClipboardText == nil || state.options.SetClipboardText == nil {
		return
	}
	current, err := state.options.ClipboardText()
	if err != nil {
		return
	}
	currentDigest := sha256.Sum256([]byte(current))
	if subtle.ConstantTimeCompare(currentDigest[:], digest[:]) != 1 {
		return
	}
	if err := state.options.SetClipboardText(""); err == nil {
		state.status = state.messages.CopyCredentialCleared
	}
}

// clearSecretClipboardNow verifies and clears the system clipboard immediately
// if it still holds a pending copied credential, then stops the auto-clear
// timer and scrubs the stored digest. Called when the window closes so copied
// plaintext never outlives the window.
func (state *State) clearSecretClipboardNow() {
	if state.secretClipboard.active {
		state.clearSecretClipboardIfMatch(state.secretClipboard.serial)
		return
	}
	state.stopSecretClipboardClear()
}

// stopSecretClipboardClear cancels any pending credential auto-clear timer and
// scrubs the stored digest.
func (state *State) stopSecretClipboardClear() {
	if state.secretClipboard.timer != nil {
		state.secretClipboard.timer.Stop()
		state.secretClipboard.timer = nil
	}
	state.secretClipboard.active = false
	clear(state.secretClipboard.digest[:])
}

// copyConnectionText places what a connection copy row offers on the clipboard.
// A failure goes to the status line, where the terminal's own Copy reports it,
// instead of being dropped along with the menu that asked for it.
func (state *State) copyConnectionText(value string) {
	if value == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(value); err != nil {
		state.status = err.Error()
		return
	}
	// A non-sensitive copy replaces whatever secret was scheduled to expire.
	state.stopSecretClipboardClear()
}

// hasCopyableMoreInfo reports whether at least one entry in the Copy Info
// submenu can act on host, without touching the vault or OS keystore.
func (state *State) hasCopyableMoreInfo(host Host) bool {
	if state.connectionCopyUsername(host) != "" || state.hostCredentialMayExist(host) {
		return true
	}
	return state.formatConnectionInfo(host, false, resolvedCopyCredential{}) != ""
}

// addCopyInfoSubmenu adds the shared "Copy Info >" row used by both the session
// tab menu and the host card/list menu. The top-level quick copy actions (IP,
// IP:PORT, Connection Name) stay one click away on the parent menu.
func (state *State) addCopyInfoSubmenu(c ui.C, rows *menuRows, triggerID gfx.ID, host Host, disabled bool) {
	rowDisabled := disabled || !state.hasCopyableMoreInfo(host)
	rows.addSubmenu(components.ContextMenuItem{
		ID:       triggerID,
		Label:    state.messages.CopyInfo,
		Icon:     lucideIcons[iconCopy],
		Disabled: rowDisabled,
	}, func(menu *ui.MenuItems) {
		if rowDisabled {
			return
		}
		state.declareCopyInfoSubmenu(c, menu, host)
	})
}

// declareCopyInfoSubmenu declares the right-hand submenu choices for copying
// username, credentials, or formatted connection info (with or without
// credentials). It never reads the vault or keystore while open.
func (state *State) declareCopyInfoSubmenu(c ui.C, menu *ui.MenuItems, host Host) {
	menu.RowHeight(contextMenuRow)
	m := state.messages
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }

	username := state.connectionCopyUsername(host)
	mayHaveCredential := state.hostCredentialMayExist(host)
	hasInfo := state.formatConnectionInfo(host, false, resolvedCopyCredential{}) != ""
	mayHaveCredInfo := hasInfo && mayHaveCredential

	addItem := func(label string, ico gfx.Icon, disabled bool, action func()) {
		if menu.Item(label).Icon(ico).Disabled(disabled).Clicked() {
			c.After(action)
		}
	}

	addItem(m.CopyUsername, icon(iconCopy), username == "", func() {
		state.copyConnectionText(username)
	})
	addItem(m.CopyCredential, icon(iconKey), !mayHaveCredential, func() {
		state.askCopyCredentialConfirm(host, false)
	})
	menu.Separator()
	addItem(m.CopyConnectionInfo, icon(iconLink), !hasInfo, func() {
		state.copyHostConnectionInfo(host)
	})
	addItem(m.CopyConnectionInfoWithCredentials, icon(iconKey), !mayHaveCredInfo, func() {
		state.askCopyCredentialConfirm(host, true)
	})
}
