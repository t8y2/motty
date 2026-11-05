package sshconn

import (
	"slices"
	"strings"
	"testing"
)

// TestProbeReadsEveryMarker keeps the shell probe and the marker table in step in
// both directions and in order: a marker added to one and not the other would
// never be reported, one still probed after leaving the table would be read and
// ignored, and the table's order is the precedence an ambiguous host is read with.
func TestProbeReadsEveryMarker(t *testing.T) {
	const prefix, suffix = "for marker in", "; do"
	start := strings.Index(remoteSystemCommand, prefix)
	if start < 0 {
		t.Fatalf("remoteSystemCommand has no marker loop")
	}
	start += len(prefix)
	end := strings.Index(remoteSystemCommand[start:], suffix)
	if end < 0 {
		t.Fatalf("remoteSystemCommand's marker loop has no %q", suffix)
	}
	probed := strings.Fields(remoteSystemCommand[start : start+end])
	listed := make([]string, 0, len(systemMarkers))
	for _, marker := range systemMarkers {
		listed = append(listed, marker.path)
	}
	if !slices.Equal(probed, listed) {
		t.Fatalf("the probe reads %v, the table lists %v", probed, listed)
	}
}

func TestRemoteSystemClassification(t *testing.T) {
	for _, test := range []struct {
		probe string
		want  RemoteSystem
	}{
		{"kernel=Linux\nid=ubuntu\n", SystemUbuntu},
		{"kernel=Linux\nid=debian\n", SystemDebian},
		{"kernel=Linux\nid=raspbian\n", SystemRaspberryPi},
		{"kernel=Linux\nid=opensuse-tumbleweed\n", SystemOpenSUSE},
		{"kernel=Linux\nid=rhel\n", SystemRedHat},
		{"kernel=Linux\nid=\"almalinux\"\n", SystemAlma},
		{"kernel=Linux\nid=endeavouros\n", SystemLinux},
		{"kernel=Linux\nid=fnos\n", SystemFnOS},
		{"kernel=Linux\nid=openwrt\n", SystemOpenWrt},
		// QWRT ships an os-release with no usable ID; its release file is the tell.
		{"kernel=Linux\nmarker=/etc/openwrt_release\n", SystemOpenWrt},
		{"kernel=Linux\nid=qwrt\n", SystemOpenWrt},
		{"kernel=Linux\nid=immortalwrt\n", SystemOpenWrt},
		{"kernel=Linux\nid=deepin\n", SystemDeepin},
		{"kernel=Linux\nid=uos\n", SystemUOS},
		{"kernel=Linux\nid=kylin\n", SystemKylin},
		{"kernel=Linux\nid=pop\n", SystemPopOS},
		{"kernel=Linux\nid=zorin\n", SystemZorin},
		{"kernel=Linux\nid=elementary\n", SystemElementary},
		{"kernel=Linux\nid=void\n", SystemVoid},
		{"kernel=Linux\nid=truenas\n", SystemTrueNAS},
		{"kernel=Linux\nid=anolis\n", SystemAnolis},
		{"kernel=Linux\nid=tencentos\n", SystemTencentOS},
		{"kernel=Linux\nid=tlinux\n", SystemTencentOS},
		{"kernel=Linux\nid=amzn\n", SystemAmazonLinux},
		{"kernel=Linux\nid=azurelinux\n", SystemAzureLinux},
		{"kernel=Linux\nid=mariner\n", SystemAzureLinux},
		{"kernel=Linux\nid=ol\n", SystemOracleLinux},
		{"kernel=Linux\nmarker=/etc.defaults/VERSION\n", SystemSynology},
		{"kernel=Linux\nmarker=/etc/config/uLinux.conf\n", SystemQNAP},
		{"kernel=Linux\nmarker=/etc/unraid-version\n", SystemUnraid},
		{"kernel=Linux\nmarker=/etc/slackware-version\n", SystemSlackware},
		// fnOS ships Debian's os-release unchanged; /usr/trim is its own tree.
		{"kernel=Linux\nid=debian\nmarker=/usr/trim\n", SystemFnOS},
		// Proxmox VE and OpenMediaVault both report Debian.
		{"kernel=Linux\nid=debian\nmarker=/etc/pve\n", SystemProxmox},
		{"kernel=Linux\nid=debian\nmarker=/etc/openmediavault\n", SystemOpenMediaVault},
		{"kernel=Linux\n", SystemLinux},
		{"kernel=Darwin\n", SystemDarwin},
		{"kernel=FreeBSD\n", SystemFreeBSD},
		{"kernel=VMkernel\n", SystemESXi},
		{"kernel=MINGW64_NT-10.0\n", SystemWindows},
		{"", SystemUnknown},
	} {
		if got := classifyProbe(test.probe); got != test.want {
			t.Errorf("classifyProbe(%q) = %v, want %v", test.probe, got, test.want)
		}
	}
	for banner, want := range map[string]RemoteSystem{
		"SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13":        SystemUbuntu,
		"SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3":        SystemDebian,
		"SSH-2.0-OpenSSH_for_Windows_9.5 Win32-OpenSSH": SystemWindows,
		"SSH-2.0-OpenSSH_9.7":                           SystemUnknown,
	} {
		if got := classifyBanner(banner); got != want {
			t.Errorf("classifyBanner(%q) = %v, want %v", banner, got, want)
		}
	}
}

func TestStandardImplementation(t *testing.T) {
	for banner, want := range map[string]bool{
		"SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13": true,
		"SSH-2.0-OpenSSH_for_Windows_9.5":        true,
		"SSH-2.0-dropbear-ssh2022.83":            true,
		"SSH-2.0-libssh_0.11.0":                  true,
		"SSH-2.0-Tailscale":                      true,
		"SSH-2.0-BHostSSH_7.0":                   false,
		"SSH-2.0-SSHD_0.7.6":                     false,
		"SSH-2.0-Erlang":                         false,
		"SSH-2.0-Cisco-1.25":                     false,
		"":                                       false,
	} {
		if got := standardImplementation(banner); got != want {
			t.Errorf("standardImplementation(%q) = %v, want %v", banner, got, want)
		}
	}
}

func TestRemoteSystemPersistenceNamesRoundTrip(t *testing.T) {
	// Bounded by the table rather than by a name coming back empty, and each name is
	// checked before it is parsed: an empty name is what SystemUnknown's is, so it
	// would parse back to the missing system and a name lost in the middle would
	// otherwise end the loop and skip every system after it.
	for system := SystemLinux; system < RemoteSystem(len(systemNames)); system++ {
		if system.String() == "" {
			t.Errorf("system %d has no persistence name", int(system))
			continue
		}
		if got := ParseRemoteSystem(system.String()); got != system {
			t.Errorf("ParseRemoteSystem(%q) = %v, want %v", system.String(), got, system)
		}
	}
	// Hosts saved before distributions had badges keep their family.
	for _, saved := range []string{"linux", "ubuntu", "windows", "darwin", "macos"} {
		if ParseRemoteSystem(saved) == SystemUnknown {
			t.Errorf("a saved %q host lost its system", saved)
		}
	}
}

func TestOnlyDistributionsAndLinuxAreLinux(t *testing.T) {
	for system, want := range map[RemoteSystem]bool{
		SystemLinux: true, SystemDebian: true, SystemRaspberryPi: true,
		SystemWindows: false, SystemDarwin: false, SystemFreeBSD: false, SystemUnknown: false,
		SystemESXi: false,
	} {
		if system.IsLinux() != want {
			t.Errorf("%v.IsLinux() = %v", system, !want)
		}
	}
}

// TestTheProbeStopsBufferingAtItsLimit guards what a probe left to a talkative
// host holds: the session outlives its timeout, so its writer must stop growing
// instead of keeping everything the host sends for the rest of the connection.
func TestTheProbeStopsBufferingAtItsLimit(t *testing.T) {
	output := &probeOutput{}
	chunk := []byte(strings.Repeat("x", 1024))
	for round := 0; round < 100; round++ {
		if n, err := output.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("write %d: %d, %v", round, n, err)
		}
	}
	if got := len(output.Bytes()); got != maxProbeOutput {
		t.Fatalf("the probe buffered %d bytes, want the %d byte limit", got, maxProbeOutput)
	}
}
