package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// editorRequest is what the window hands to the background: the folder, and for a
// server the host as it is now with the alias the SSH config knows it by, so
// nothing is read from the window again.
type editorRequest struct {
	editor    ExternalEditor
	directory string
	host      editorHost
	alias     string
	// portFmt explains a port the chosen editor cannot carry.
	portFmt string
}

// editorHost is the part of a saved host a target is made from.
type editorHost struct {
	address string
	user    string
	port    uint16
}

// resolveEditorAuthority is what the editor's URL puts before the folder: the
// alias the SSH config knows the host by, or user@host[:port] for a host the
// config does not name. Empty means a folder on this computer, which the editor
// opens by path.
func resolveEditorAuthority(request editorRequest) (string, error) {
	if request.alias != "" {
		return request.alias, nil
	}
	host, port := request.host, port22(request.host.port)
	address := host.address
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
		address = host.user + "@" + address
	}
	return address, nil
}
