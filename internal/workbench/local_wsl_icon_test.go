package workbench

import (
	"testing"

	"motty/internal/localshell"
	"motty/internal/sshconn"
)

func TestAWSLShellIsMonitoredAsItsDistribution(t *testing.T) {
	for name, test := range map[string]struct {
		session *Session
		want    sshconn.RemoteSystem
	}{
		"a distribution on Windows": {
			session: &Session{conn: &localConn{shell: localshell.WSLShell("Ubuntu-22.04")}, host: Host{System: sshconn.SystemWindows}},
			want:    sshconn.SystemLinux,
		},
		"Windows PowerShell": {
			session: &Session{conn: &localConn{shell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, host: Host{System: sshconn.SystemWindows}},
			want:    sshconn.SystemWindows,
		},
	} {
		if got := monitorSystem(test.session); got != test.want {
			t.Errorf("%s is monitored as %v, want %v", name, got, test.want)
		}
	}
}

func TestAWSLTerminalsPanelOpensInTheDistribution(t *testing.T) {
	session := &Session{conn: &localConn{shell: localshell.WSLShell("Ubuntu")}}
	if got, want := (&State{}).localPanelStart(session), `\\wsl.localhost\Ubuntu\`; got != want {
		t.Fatalf("panel opens in %q, want %q", got, want)
	}
}

func TestWSLDistributionNamesMapToTheirBadge(t *testing.T) {
	for distro, want := range map[string]sshconn.RemoteSystem{
		"Ubuntu-22.04":       sshconn.SystemUbuntu,
		"Ubuntu":             sshconn.SystemUbuntu,
		"Debian":             sshconn.SystemDebian,
		"kali-linux":         sshconn.SystemKali,
		"FedoraLinux-42":     sshconn.SystemFedora,
		"RockyLinux-9":       sshconn.SystemRocky,
		"AlmaLinux-9":        sshconn.SystemAlma,
		"openSUSE-Leap-15.5": sshconn.SystemOpenSUSE,
		"OracleLinux_9_1":    sshconn.SystemOracleLinux,
		"AmazonLinux-2023":   sshconn.SystemAmazonLinux,
		"ArchLinux":          sshconn.SystemArch,
		"CentOS7":            sshconn.SystemCentOS,
	} {
		if got := wslDistroSystem(distro); got != want {
			t.Errorf("%s maps to %v, want %v", distro, got, want)
		}
	}
	if got := wslDistroSystem("Pengwin"); got != sshconn.SystemUnknown {
		t.Errorf("a distribution without a mark maps to %v, want unknown", got)
	}
}

func TestAWSLDistributionWearsItsOwnBadgeAndOthersWearLinux(t *testing.T) {
	ubuntu, ok := wslDistroBadgeArt("Ubuntu-22.04", false)
	if !ok {
		t.Fatal("Ubuntu has no badge")
	}
	debian, ok := wslDistroBadgeArt("Debian", false)
	if !ok {
		t.Fatal("Debian has no badge")
	}
	linux, ok := systemBadgeArt(sshconn.SystemLinux, false)
	if !ok {
		t.Fatal("Linux has no badge")
	}
	if ubuntu.background == debian.background {
		t.Error("Ubuntu and Debian wear the same badge")
	}
	unknown, ok := wslDistroBadgeArt("Pengwin", false)
	if !ok || unknown.background != linux.background {
		t.Error("a distribution without a mark did not fall back to the Linux badge")
	}
}

func TestPowerShellAndCMDHaveMarksOfTheirOwn(t *testing.T) {
	for _, shell := range []string{
		`C:\Program Files\PowerShell\7\pwsh.exe`,
		`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		`C:\Windows\System32\cmd.exe`,
	} {
		if _, ok := localShellGlyph(shell); !ok {
			t.Errorf("%s has no mark of its own", shell)
		}
	}
	for _, shell := range []string{"/bin/zsh", "", `C:\Program Files\Git\bin\bash.exe`} {
		if _, ok := localShellGlyph(shell); ok {
			t.Errorf("%s was given a Windows shell's mark", shell)
		}
	}
}
