package sshconn

import (
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// RemoteSystem is the operating system reported by an authenticated SSH host:
// a distribution where /etc/os-release names one this client has a badge for, and
// otherwise just its family. Unknown keeps rendering and persistence honest when
// the server refuses command sessions or does not expose enough information.
type RemoteSystem uint8

const (
	SystemUnknown RemoteSystem = iota
	SystemLinux
	SystemUbuntu
	SystemWindows
	SystemDarwin
	SystemDebian
	SystemFedora
	SystemAlpine
	SystemArch
	SystemCentOS
	SystemRedHat
	SystemRocky
	SystemAlma
	SystemOpenSUSE
	SystemKali
	SystemMint
	SystemRaspberryPi
	SystemFreeBSD
	SystemNixOS
	SystemGentoo
	SystemManjaro
	SystemSynology
	SystemQNAP
	SystemFnOS
	SystemTrueNAS
	SystemUnraid
	SystemOpenMediaVault
	SystemProxmox
	SystemOpenWrt
	SystemDeepin
	SystemUOS
	SystemKylin
	SystemPopOS
	SystemZorin
	SystemElementary
	SystemVoid
	SystemSlackware
	SystemAnolis
	SystemTencentOS
	SystemAmazonLinux
	SystemAzureLinux
	SystemOracleLinux
	SystemESXi
)

// systemNames is each system's persisted name. An /etc/os-release ID that has a
// badge is the same word, except where the ID is not what people call the system.
var systemNames = [...]string{
	SystemLinux: "linux", SystemUbuntu: "ubuntu", SystemWindows: "windows", SystemDarwin: "darwin",
	SystemDebian: "debian", SystemFedora: "fedora", SystemAlpine: "alpine", SystemArch: "arch",
	SystemCentOS: "centos", SystemRedHat: "rhel", SystemRocky: "rocky", SystemAlma: "almalinux",
	SystemOpenSUSE: "opensuse", SystemKali: "kali", SystemMint: "linuxmint", SystemRaspberryPi: "raspbian",
	SystemFreeBSD: "freebsd", SystemNixOS: "nixos", SystemGentoo: "gentoo", SystemManjaro: "manjaro",
	SystemSynology: "synology", SystemQNAP: "qnap", SystemFnOS: "fnos", SystemTrueNAS: "truenas",
	SystemUnraid: "unraid", SystemOpenMediaVault: "openmediavault", SystemProxmox: "proxmox",
	SystemOpenWrt: "openwrt", SystemDeepin: "deepin", SystemUOS: "uos", SystemKylin: "kylin",
	SystemPopOS: "popos", SystemZorin: "zorin",
	SystemElementary: "elementary", SystemVoid: "void", SystemSlackware: "slackware",
	SystemAnolis: "anolis", SystemTencentOS: "tencentos", SystemAmazonLinux: "amazonlinux",
	SystemAzureLinux: "azurelinux", SystemOracleLinux: "oraclelinux", SystemESXi: "esxi",
}

func (system RemoteSystem) String() string {
	if int(system) < len(systemNames) {
		return systemNames[system]
	}
	return ""
}

// IsLinux reports whether the system is a Linux distribution or Linux itself.
func (system RemoteSystem) IsLinux() bool {
	switch system {
	case SystemUnknown, SystemWindows, SystemDarwin, SystemFreeBSD, SystemESXi:
		return false
	}
	return int(system) < len(systemNames)
}

// ParseRemoteSystem restores a persisted system.
func ParseRemoteSystem(value string) RemoteSystem {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "macos" {
		return SystemDarwin
	}
	for system := SystemLinux; int(system) < len(systemNames); system++ {
		if systemNames[system] == value {
			return system
		}
	}
	return SystemUnknown
}

// osReleaseIDs maps an /etc/os-release ID to its system when the ID differs from
// the persisted name or has variants.
var osReleaseIDs = map[string]RemoteSystem{
	"opensuse-leap": SystemOpenSUSE, "opensuse-tumbleweed": SystemOpenSUSE, "opensuse-slowroll": SystemOpenSUSE,
	"rhel": SystemRedHat, "almalinux": SystemAlma, "rocky": SystemRocky, "raspbian": SystemRaspberryPi,
	"linuxmint": SystemMint, "arch": SystemArch, "archarm": SystemArch,
	"pop": SystemPopOS, "pop-os": SystemPopOS,
	// OpenWrt's derivatives keep the base system and only change the name, and
	// some of them rewrite the ID to match. They all show the OpenWrt badge:
	// there is no separate badge for a fork of a firmware.
	"qwrt": SystemOpenWrt, "immortalwrt": SystemOpenWrt, "lede": SystemOpenWrt,
	// The cloud vendors name their own systems with abbreviations that are not
	// what anyone calls them, so the ID needs an entry to reach the badge.
	"amzn": SystemAmazonLinux, "ol": SystemOracleLinux, "mariner": SystemAzureLinux,
	"tlinux": SystemTencentOS,
}

// systemMarkers are the files an appliance or a distribution with no
// /etc/os-release leaves behind, in the order they are read. A marker says more
// than an os-release ID: Proxmox VE and OpenMediaVault both report Debian, and a
// NAS reports only its kernel.
var systemMarkers = [...]struct {
	path   string
	system RemoteSystem
}{
	{"/etc.defaults/VERSION", SystemSynology},
	{"/etc/config/uLinux.conf", SystemQNAP},
	{"/etc/unraid-version", SystemUnraid},
	{"/etc/openmediavault", SystemOpenMediaVault},
	{"/etc/pve", SystemProxmox},
	// fnOS keeps its own tree under /usr/trim and reports plain Debian in
	// /etc/os-release, so the ID alone would badge it as Debian.
	{"/usr/trim", SystemFnOS},
	// A router firmware is OpenWrt whatever its /etc/os-release says, and some
	// forks ship an os-release without an ID at all. This file is the one every
	// OpenWrt build writes.
	{"/etc/openwrt_release", SystemOpenWrt},
	{"/etc/slackware-version", SystemSlackware},
}

// remoteSystemCommand prints the kernel name, the /etc/os-release ID where the
// host has one, and a line for every marker file that exists. The marker list
// comes from systemMarkers so the two cannot drift apart.
var remoteSystemCommand = remoteSystemProbe()

func remoteSystemProbe() string {
	var markers strings.Builder
	for _, marker := range systemMarkers {
		markers.WriteString(" " + marker.path)
	}
	return `printf 'kernel=%s\n' "$(uname -s 2>/dev/null)"; ` +
		`if [ -r /etc/os-release ]; then . /etc/os-release; printf 'id=%s\n' "$ID"; fi; ` +
		`for marker in` + markers.String() + `; do if [ -e "$marker" ]; then printf 'marker=%s\n' "$marker"; fi; done`
}

// standardImplementation reports whether the server's identification string
// names an SSH implementation whose optional protocol behavior is safe to use
// beside an interactive shell. Only these receive the system and shell
// integration probes, which open a second session, and the locale environment
// request. A gateway may answer any of those by ending the whole connection,
// so an unrecognized banner only gets the pty and shell its menu needs.
// Tailscale SSH is a full server of its own, not a menu gateway: it names
// itself "Tailscale" and answers command channels like the servers above.
func standardImplementation(banner string) bool {
	banner = strings.ToLower(banner)
	return strings.Contains(banner, "openssh") ||
		strings.Contains(banner, "dropbear") ||
		strings.Contains(banner, "libssh") ||
		strings.Contains(banner, "tailscale")
}

// classifyBanner is what the server's identification string alone says. A
// distribution banner is only a first guess: derivatives share their parent's
// OpenSSH, so a Linux guess is refined by asking the host.
func classifyBanner(banner string) RemoteSystem {
	banner = strings.ToLower(banner)
	switch {
	case strings.Contains(banner, "win32-openssh"), strings.Contains(banner, "windows"), strings.Contains(banner, "microsoft"):
		return SystemWindows
	case strings.Contains(banner, "darwin"), strings.Contains(banner, "macos"):
		return SystemDarwin
	case strings.Contains(banner, "ubuntu"):
		return SystemUbuntu
	case strings.Contains(banner, "debian"):
		return SystemDebian
	case strings.Contains(banner, "freebsd"):
		return SystemFreeBSD
	}
	return SystemUnknown
}

// classifyProbe reads the output of remoteSystemCommand: the kernel name and,
// where the host has one, the /etc/os-release ID, plus a line per marker file.
func classifyProbe(output string) RemoteSystem {
	var kernel, id string
	seen := make(map[string]bool, len(systemMarkers))
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "kernel":
			kernel = strings.ToLower(strings.TrimSpace(value))
		case "id":
			id = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))
		case "marker":
			seen[strings.TrimSpace(value)] = true
		}
	}
	switch {
	case strings.HasPrefix(kernel, "darwin"):
		return SystemDarwin
	case strings.HasPrefix(kernel, "freebsd"):
		return SystemFreeBSD
	case strings.Contains(kernel, "mingw"), strings.Contains(kernel, "cygwin"), strings.Contains(kernel, "msys"), strings.Contains(kernel, "windows"):
		return SystemWindows
	case strings.HasPrefix(kernel, "vmkernel"):
		// ESXi is its own kernel: it answers uname with VMkernel, and every
		// userland file it carries comes from busybox, not from a distribution.
		return SystemESXi
	}
	// An appliance's own marker outranks the distribution it borrows its userland
	// from, which is why the markers are read before the ID.
	for _, marker := range systemMarkers {
		if seen[marker.path] {
			return marker.system
		}
	}
	if id != "" {
		if system, ok := osReleaseIDs[id]; ok {
			return system
		}
		if system := ParseRemoteSystem(id); system != SystemUnknown && system.IsLinux() {
			return system
		}
	}
	if strings.HasPrefix(kernel, "linux") {
		return SystemLinux
	}
	return SystemUnknown
}

// remoteSystemProbeTimeout bounds the probe. A host that has not answered by
// then keeps the banner's guess and is left alone: closing a session whose
// command is still running can end the whole connection on a minimal server
// such as dropbear, and with it any shell beside it.
const remoteSystemProbeTimeout = 1500 * time.Millisecond

// maxProbeOutput bounds what the probe keeps. The command prints a few hundred
// bytes; a host that keeps writing after the probe gave up must not be able to
// grow the client's memory for as long as the connection stays open.
const maxProbeOutput = 4 << 10

// probeOutput keeps the first maxProbeOutput bytes the probe writes and drops
// the rest. Reporting the dropped bytes as written keeps the channel's copy
// loop moving instead of blocking on a full pipe.
type probeOutput struct {
	buf []byte
}

func (output *probeOutput) Write(p []byte) (int, error) {
	if room := maxProbeOutput - len(output.buf); room > 0 {
		output.buf = append(output.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (output *probeOutput) Bytes() []byte { return output.buf }

// detectRemoteSystem asks the host what it is when the banner could not say, or
// said only a Linux distribution that a derivative may share. The second result
// reports whether the host answered: when it refuses a command session, or says
// no more than Linux, the result is still the banner's guess.
//
// The command session is never closed from here: a session closed while its
// command still runs can end the whole connection on a minimal server such as
// dropbear. One that has not answered by the timeout is left to the connection.
func (conn *Conn) detectRemoteSystem(client *ssh.Client, banner string) (RemoteSystem, bool) {
	guess := classifyBanner(banner)
	if guess == SystemWindows || guess == SystemDarwin || guess == SystemFreeBSD {
		return guess, true
	}
	session, err := client.NewSession()
	if err != nil {
		return guess, false
	}
	type answer struct {
		output []byte
		err    error
	}
	answered := make(chan answer, 1)
	// The session is left to the connection when it outlives the timeout, so its
	// output has to be bounded from the start: Output would hand it a buffer that
	// grows for the rest of the connection.
	probeOut := &probeOutput{}
	session.Stdout = probeOut
	go func() {
		err := session.Run(remoteSystemCommand)
		answered <- answer{probeOut.Bytes(), err}
	}()
	timer := time.NewTimer(remoteSystemProbeTimeout)
	defer timer.Stop()
	var probe answer
	select {
	case probe = <-answered:
	case <-timer.C:
		// The channel is left to the server and released with the connection.
		conn.addCloser(session)
		return guess, false
	case <-conn.ctx.Done():
		conn.addCloser(session)
		return guess, false
	}
	if probe.err != nil {
		return guess, false
	}
	system := classifyProbe(string(probe.output))
	if system == SystemUnknown || system == SystemLinux && guess.IsLinux() {
		// A derivative with no badge of its own (Pop!_OS, Zorin) keeps the family
		// its banner named rather than falling back to plain Linux.
		return guess, false
	}
	return system, true
}
