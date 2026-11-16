package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"motty/internal/sessionimport"
	"motty/internal/sshconn"
)

// xshellLaunchPath reports whether args name one existing .xsh file to open.
// File association and "motty path/to/session.xsh" both arrive that way. A bare
// name that is not a file is left for the usual target parser.
func xshellLaunchPath(args []string) (string, bool) {
	var path string
	for _, arg := range args {
		switch {
		case arg == "", arg == "--background", arg == "-ssh":
			continue
		case path != "":
			return "", false
		default:
			path = arg
		}
	}
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".xsh") {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

// openXshellFile parses a single .xsh session and connects at once without
// writing it into the host library. Loopback addresses get the tunnel policy
// bastion clients need; other hosts open as a temporary target.
func (state *State) openXshellFile(path string) {
	result, err := sessionimport.ParseXshell(path)
	if err != nil {
		state.activeSection = sectionHosts
		state.status = fmt.Sprintf(state.messages.Tools.LaunchXshellFailedFmt, filepath.Base(path), err)
		return
	}
	var entry sessionimport.Entry
	found := false
	for _, candidate := range result.Entries {
		if candidate.Protocol == sessionimport.SSH || candidate.Protocol == sessionimport.Telnet {
			entry, found = candidate, true
			break
		}
	}
	if !found {
		state.activeSection = sectionHosts
		state.status = fmt.Sprintf(state.messages.Tools.LaunchXshellEmptyFmt, filepath.Base(path))
		return
	}
	host := hostFromSessionEntry(entry)
	state.openHostSession(host)
}

// hostFromSessionEntry turns one imported session into an ephemeral host. It is
// not added to the saved catalog.
func hostFromSessionEntry(entry sessionimport.Entry) Host {
	port := uint16(entry.Port)
	if port == 0 {
		port = defaultPort
	}
	host := Host{
		Label:   entry.Name,
		Address: entry.Host,
		User:    entry.User,
		Port:    port,
	}
	if entry.Protocol == sessionimport.Telnet {
		host.Protocol = protocolTelnet
		if entry.Port == 0 {
			host.Port = defaultTelnetPort
		}
	}
	if host.isSSH() && sessionimport.IsLoopbackHost(host.Address) {
		host.tunnel = true
		applyLoopbackTunnelPolicy(&host)
	}
	host.finish()
	if host.Label == "" {
		host.Label = host.target
	}
	return host
}

// applyLoopbackTunnelPolicy is how Motty reaches a one-shot bastion tunnel on
// this computer: compatible algorithms, password authentication (empty Enter
// allowed), and no private-key attempt for a placeholder UserKey.
func applyLoopbackTunnelPolicy(host *Host) {
	host.Algo = &hostAlgo{mode: algoCompatible}
	host.Auth = authPassword
	host.Identity = ""
}

// loopbackTunnel reports the ephemeral host opened from a local bastion tunnel
// file. A saved host on 127.0.0.1 keeps the ordinary connection policy: the
// tunnel rules are for the one-shot file, not for every local password host.
func loopbackTunnel(host Host) bool {
	return host.isSSH() && host.tunnel
}

// disableKeepAliveForTunnel clears keepalive on a loopback tunnel connection.
func disableKeepAliveForTunnel(host Host, config *sshconn.Config) {
	if loopbackTunnel(host) {
		config.KeepAlive = sshconn.KeepAliveConfig{}
	}
}
