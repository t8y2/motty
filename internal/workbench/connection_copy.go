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
	value, found, err := vaultFile.Get(name)
	if err != nil {
		// Keep the row clickable so choosing it surfaces the vault error in the
		// status line instead of silently appearing empty.
		return true
	}
	return found && value != ""
}

// hostKeyCredentialLabel names the private key configured on an SSH host, if a
// specific key is pinned.
func (state *State) hostKeyCredentialLabel(host Host) string {
	if !host.isSSH() || host.Auth != authPrivateKey {
		return ""
	}
	if fingerprint := strings.TrimSpace(host.Identity); fingerprint != "" {
		if identity, ok := state.identityForFingerprint(fingerprint); ok && identity.Name != "" {
			return identity.Name
		}
	}
	if host.SSHOptions != nil && len(host.SSHOptions.IdentityFiles) > 0 {
		return strings.Join(host.SSHOptions.IdentityFiles, ", ")
	}
	return ""
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

// formatConnectionInfo formats host's connection details as copyable lines.
// includeCredentials appends the resolved password (or pinned key name when
// key-authenticated). Local shells have no remote connection info to format.
func (state *State) formatConnectionInfo(host Host, includeCredentials bool, password string) string {
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
	lines := make([]string, 0, 6)
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
		if password != "" {
			add(m.FieldPassword, password)
		} else if keyLabel := state.hostKeyCredentialLabel(host); keyLabel != "" {
			add(m.FieldIdentity, keyLabel)
		}
	}
	return strings.Join(lines, "\n")
}

// resolveHostPasswordForCopy reads the saved password for host, triggering the
// legacy vault unlock dialog when required and reporting vault errors to the
// status line. retry runs once the vault is unlocked.
func (state *State) resolveHostPasswordForCopy(host Host, retry func()) (password string, ok bool) {
	name, hasSecret := state.hostPasswordSecretNameForCopy(host)
	if !hasSecret {
		return "", true
	}
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 {
		return "", true
	}
	if vaultFile.NeedsConversion() {
		state.openVaultUnlock(nil, retry)
		return "", false
	}
	value, found, err := vaultFile.Get(name)
	if err != nil {
		state.status = state.vaultErrorText(err)
		return "", false
	}
	if !found {
		return "", true
	}
	return value, true
}

func (state *State) copyHostPassword(host Host) {
	password, ok := state.resolveHostPasswordForCopy(host, func() {
		state.copyHostPassword(host)
	})
	if !ok || password == "" {
		return
	}
	state.copyConnectionText(password)
}

func (state *State) copyHostConnectionInfo(host Host, includeCredentials bool) {
	password := ""
	if includeCredentials {
		var ok bool
		password, ok = state.resolveHostPasswordForCopy(host, func() {
			state.copyHostConnectionInfo(host, true)
		})
		if !ok {
			return
		}
	}
	state.copyConnectionText(state.formatConnectionInfo(host, includeCredentials, password))
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
	return state.formatConnectionInfo(host, false, "") != ""
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
// individual fields or formatted connection info (with or without credentials).
func (state *State) declareCopyInfoSubmenu(c ui.C, menu *ui.MenuItems, host Host) {
	menu.RowHeight(contextMenuRow)
	m := state.messages
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }

	hostAddress := connectionHostAddress(host)
	address := connectionAddressAndPort(host)
	name := host.Label
	username := state.connectionCopyUsername(host)
	hasPassword := state.hostPasswordAvailable(host)
	hasInfo := state.formatConnectionInfo(host, false, "") != ""
	hasCredInfo := hasInfo && (hasPassword || state.hostKeyCredentialLabel(host) != "")

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
	addItem(m.CopyPassword, icon(iconKey), !hasPassword, func() {
		state.copyHostPassword(host)
	})
	menu.Separator()
	addItem(m.CopyConnectionInfo, icon(iconLink), !hasInfo, func() {
		state.copyHostConnectionInfo(host, false)
	})
	addItem(m.CopyConnectionInfoWithCredentials, icon(iconKey), !hasCredInfo, func() {
		state.copyHostConnectionInfo(host, true)
	})
}
