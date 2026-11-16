package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"motty/internal/sshconfig"
)

// editorRequest is what the window hands to the background: the folder, and for a
// server the host as it is now with the names the SSH config might know it by, so
// nothing is read from the window again.
type editorRequest struct {
	editor    ExternalEditor
	directory string
	// local is a folder on this computer, which the editor opens by path.
	local bool
	host  editorHost
	// aliases are the names to look for in the SSH config, best first, and
	// configPath is the file to look in.
	aliases    []string
	configPath string
	// portFmt explains a port the chosen editor cannot carry, and jumpFmt a host
	// that is reached through a jump host the SSH config does not know about.
	portFmt string
	jumpFmt string
}

// editorHost is the part of a saved host a target is made from.
type editorHost struct {
	address    string
	user       string // the login, the system user when the host names none
	systemUser string
	port       uint16
	// mottyJump is set when the host is reached through a jump host kept only in
	// Motty, which the editor's ssh would not know to use.
	mottyJump bool
}

// resolveEditorAuthority is what the editor's URL puts before the folder: the
// alias the SSH config knows this very host by, or user@host[:port] for a host
// the config does not name. Empty means a folder on this computer.
func resolveEditorAuthority(request editorRequest) (string, error) {
	if request.local {
		return "", nil
	}
	if alias := request.configuredAlias(); alias != "" {
		return editorAuthorityPart(alias)
	}
	host := request.host
	if host.address == "" {
		return "", fmt.Errorf("the host has no address")
	}
	if host.mottyJump {
		// The editor's ssh would connect directly, to a machine the jump host
		// may be the only way to, or to another one at the same address.
		return "", fmt.Errorf("%s", request.jumpFmt)
	}
	port := port22(host.port)
	address, err := editorAuthorityPart(host.address)
	if err != nil {
		return "", err
	}
	if port != defaultPort {
		// VS Code's authority has no port syntax: a refusal, not another host.
		if request.editor == editorVSCode {
			return "", fmt.Errorf(request.portFmt, host.port)
		}
		address = net.JoinHostPort(address, strconv.Itoa(int(port)))
	} else if strings.ContainsRune(address, ':') {
		// An IPv6 address without a port still takes the brackets a URL requires.
		address = "[" + address + "]"
	}
	if host.user != "" {
		user, err := editorAuthorityPart(host.user)
		if err != nil {
			return "", err
		}
		address = user + "@" + address
	}
	return address, nil
}

// configuredAlias is the first name in the SSH config that is this very host: a
// Host block of the same name could point at another machine, user or port, and
// the editor's ssh would then connect there. Empty means none is.
func (request editorRequest) configuredAlias() string {
	if len(request.aliases) == 0 {
		return ""
	}
	config, err := sshconfig.Load(request.configPath)
	if err != nil {
		return ""
	}
	for _, alias := range request.aliases {
		if known, ok := config.Host(alias); ok && request.isHost(known) {
			return known.Alias
		}
	}
	return ""
}

// isHost reports whether a config entry reaches the host the request is for.
func (request editorRequest) isHost(known sshconfig.Host) bool {
	host := request.host
	name := known.HostName
	if name == "" {
		name = known.Alias
	}
	user := known.User
	if user == "" {
		user = host.systemUser
	}
	return strings.EqualFold(name, host.address) && port22(known.Port) == port22(host.port) && user == host.user
}

// editorAuthorityPart makes one piece of an authority safe to put in a URL: the
// characters that would end it or change what the URL names are refused, and
// bytes outside ASCII (an alias in Chinese) are percent-escaped.
func editorAuthorityPart(part string) (string, error) {
	if part == "" {
		return "", fmt.Errorf("the SSH name is empty")
	}
	var escaped strings.Builder
	for index := 0; index < len(part); index++ {
		switch character := part[index]; {
		case character <= ' ' || character == 0x7f || strings.IndexByte("/?#%\\@", character) >= 0:
			return "", fmt.Errorf("the SSH name %q holds a character an editor link cannot carry", part)
		case character >= 0x80:
			fmt.Fprintf(&escaped, "%%%02X", character)
		default:
			escaped.WriteByte(character)
		}
	}
	return escaped.String(), nil
}
