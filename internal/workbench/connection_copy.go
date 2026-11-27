package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

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

// copyInfoPasswordProbe caches one password-availability lookup while a single
// tab or host context menu stays open, so repainting an open submenu does not
// re-decrypt records or re-trigger an OS keystore prompt on every frame.
type copyInfoPasswordProbe struct {
	name      string
	checked   bool
	available bool
}

// hostPasswordAvailable reports whether host has a saved password that can be
// copied (or a legacy master-password vault that must be unlocked first).
func (state *State) hostPasswordAvailable(host Host) bool {
	name, ok := state.hostPasswordSecretNameForCopy(host)
	if !ok {
		return false
	}
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 {
		return false
	}
	if vaultFile.NeedsConversion() {
		return true
	}
	if state.copyInfoProbe.checked && state.copyInfoProbe.name == name {
		return state.copyInfoProbe.available
	}
	value, found, err := vaultFile.Get(name)
	available := err != nil || (found && value != "")
	state.copyInfoProbe = copyInfoPasswordProbe{name: name, checked: true, available: available}
	return available
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

// hostCredentialAvailable reports whether host has a copyable credential
// (either a saved password or a copyable pinned SSH private key).
func (state *State) hostCredentialAvailable(host Host) bool {
	if _, ok := state.hostPrivateKeyForCopy(host); ok {
		return true
	}
	return state.hostPasswordAvailable(host)
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
// key or saved password), opening the legacy vault unlock dialog when needed
// and reporting read errors to the status line. retry runs after unlock.
func (state *State) resolveHostCredentialForCopy(host Host, includePassphrase bool, retry func()) (resolvedCopyCredential, bool) {
	if identity, ok := state.hostPrivateKeyForCopy(host); ok {
		vaultFile := state.secrets.vault
		if (identity.Stored || includePassphrase) && vaultFile != nil && vaultFile.NeedsConversion() {
			state.openVaultUnlock(nil, retry)
			return resolvedCopyCredential{}, false
		}
		data, err := state.identityKeyData(identity)
		if err != nil {
			state.status = err.Error()
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
		state.status = state.vaultErrorText(err)
		return resolvedCopyCredential{}, false
	}
	if !found {
		return resolvedCopyCredential{}, true
	}
	return resolvedCopyCredential{password: value}, true
}

func (state *State) copyHostCredential(host Host) {
	cred, ok := state.resolveHostCredentialForCopy(host, false, func() {
		state.copyHostCredential(host)
	})
	if !ok {
		return
	}
	if cred.privateKey != "" {
		state.copyConnectionText(cred.privateKey)
		return
	}
	if cred.password != "" {
		state.copyConnectionText(cred.password)
	}
}

func (state *State) copyHostConnectionInfo(host Host, includeCredentials bool) {
	var cred resolvedCopyCredential
	if includeCredentials {
		var ok bool
		cred, ok = state.resolveHostCredentialForCopy(host, true, func() {
			state.copyHostConnectionInfo(host, true)
		})
		if !ok {
			return
		}
	}
	state.copyConnectionText(state.formatConnectionInfo(host, includeCredentials, cred))
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
	}
}

// hasCopyableConnectionInfo reports whether at least one entry in the Copy Info
// submenu can act on host, without touching the vault on the top-level menu
// paint.
func (state *State) hasCopyableConnectionInfo(host Host) bool {
	if connectionHostAddress(host) != "" || connectionAddressAndPort(host) != "" || host.Label != "" || state.connectionCopyUsername(host) != "" {
		return true
	}
	return state.formatConnectionInfo(host, false, resolvedCopyCredential{}) != ""
}

// addCopyInfoSubmenu adds the shared "Copy Info >" row used by both the session
// tab menu and the host card/list menu.
func (state *State) addCopyInfoSubmenu(c ui.C, rows *menuRows, triggerID gfx.ID, host Host, disabled bool) {
	rowDisabled := disabled || !state.hasCopyableConnectionInfo(host)
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
// individual fields, credentials, or formatted connection info (with or without
// credentials).
func (state *State) declareCopyInfoSubmenu(c ui.C, menu *ui.MenuItems, host Host) {
	menu.RowHeight(contextMenuRow)
	m := state.messages
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }

	hostAddress := connectionHostAddress(host)
	address := connectionAddressAndPort(host)
	name := host.Label
	username := state.connectionCopyUsername(host)
	hasCredential := state.hostCredentialAvailable(host)
	hasInfo := state.formatConnectionInfo(host, false, resolvedCopyCredential{}) != ""
	hasCredInfo := hasInfo && hasCredential

	addItem := func(label string, ico gfx.Icon, disabled bool, action func()) {
		if menu.Item(label).Icon(ico).Disabled(disabled).Clicked() {
			c.After(action)
		}
	}

	addItem(m.CopyHostAddress, icon(iconCopy), hostAddress == "", func() {
		state.copyConnectionText(hostAddress)
	})
	addItem(m.CopyAddressAndPort, icon(iconCopy), address == "", func() {
		state.copyConnectionText(address)
	})
	addItem(m.CopyConnectionName, icon(iconCopy), name == "", func() {
		state.copyConnectionText(name)
	})
	menu.Separator()
	addItem(m.CopyUsername, icon(iconCopy), username == "", func() {
		state.copyConnectionText(username)
	})
	addItem(m.CopyCredential, icon(iconKey), !hasCredential, func() {
		state.copyHostCredential(host)
	})
	menu.Separator()
	addItem(m.CopyConnectionInfo, icon(iconLink), !hasInfo, func() {
		state.copyHostConnectionInfo(host, false)
	})
	addItem(m.CopyConnectionInfoWithCredentials, icon(iconKey), !hasCredInfo, func() {
		state.copyHostConnectionInfo(host, true)
	})
}
