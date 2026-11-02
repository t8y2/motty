package sshconn

import (
	"strings"
	"testing"
)

// TestProbeReadsEveryMarker keeps the shell probe and the marker table in step:
// a marker added to one and not the other would never be reported.
func TestProbeReadsEveryMarker(t *testing.T) {
	for _, marker := range systemMarkers {
		if !strings.Contains(remoteSystemCommand, " "+marker.path) {
			t.Errorf("remoteSystemCommand does not look for %s", marker.path)
		}
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
		{"kernel=Linux\nid=deepin\n", SystemDeepin},
		{"kernel=Linux\nid=uos\n", SystemUOS},
		{"kernel=Linux\nid=pop\n", SystemPopOS},
		{"kernel=Linux\nid=zorin\n", SystemZorin},
		{"kernel=Linux\nid=elementary\n", SystemElementary},
		{"kernel=Linux\nid=void\n", SystemVoid},
		{"kernel=Linux\nid=truenas\n", SystemTrueNAS},
		{"kernel=Linux\nmarker=/etc.defaults/VERSION\n", SystemSynology},
		{"kernel=Linux\nmarker=/etc/config/uLinux.conf\n", SystemQNAP},
		{"kernel=Linux\nmarker=/etc/unraid-version\n", SystemUnraid},
		{"kernel=Linux\nmarker=/etc/slackware-version\n", SystemSlackware},
		// Proxmox VE and OpenMediaVault both report Debian.
		{"kernel=Linux\nid=debian\nmarker=/etc/pve\n", SystemProxmox},
		{"kernel=Linux\nid=debian\nmarker=/etc/openmediavault\n", SystemOpenMediaVault},
		{"kernel=Linux\n", SystemLinux},
		{"kernel=Darwin\n", SystemDarwin},
		{"kernel=FreeBSD\n", SystemFreeBSD},
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
	for system := SystemLinux; system.String() != ""; system++ {
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
	} {
		if system.IsLinux() != want {
			t.Errorf("%v.IsLinux() = %v", system, !want)
		}
	}
}
