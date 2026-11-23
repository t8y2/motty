package sshconn

import (
	"strings"
	"testing"
)

func TestPowerShellActivationIsOneLineAndHidesItsMarker(t *testing.T) {
	line := PowerShellActivation("tok-123", "/C:/Users/me", 4)
	if !strings.HasSuffix(line, "\r") || strings.ContainsAny(strings.TrimSuffix(line, "\r"), "\r\n") {
		t.Fatalf("the activation must be one line ended by Enter, got %q", line)
	}
	for _, r := range line {
		if r > 0x7e && r != '\r' {
			t.Fatalf("the line must be ASCII, found %q", r)
		}
	}
	// The terminal drops everything up to the ready marker, and it matches the
	// marker bytes: the echoed line must never contain them.
	if marker := string(IntegrationReadyMarker("tok-123")); strings.Contains(line, marker) {
		t.Fatalf("the echoed line contains the ready marker")
	}
	if !strings.Contains(line, string(IntegrationEchoTag("tok-123"))) {
		t.Fatalf("the line must carry the echo tag the terminal recognizes")
	}
}

func TestPowerShellActivationRefusesWhatCouldBeTakenForCode(t *testing.T) {
	if PowerShellActivation("tok'; calc", "", 0) != "" || PowerShellActivation("", "", 0) != "" {
		t.Fatalf("a token that is not plain must not be put into a script")
	}
}

func TestPowerShellStartFolderIsThePathPowerShellTakes(t *testing.T) {
	tests := []struct {
		directory, want string
		ok              bool
	}{
		{"/C:/Users/me", "C:/Users/me", true},
		{"/c:", "c:/", true},
		{"/C:/", "C:/", true},
		{"/home/me", "/home/me", true},
		{"/Cx/Users", "/Cx/Users", true},
		{"C:/Users/me", "", false},
		{"", "", false},
		{"/a\nb", "", false},
	}
	for _, test := range tests {
		got, ok := powerShellStartFolder(test.directory)
		if got != test.want || ok != test.ok {
			t.Errorf("powerShellStartFolder(%q) = %q, %v; want %q, %v", test.directory, got, ok, test.want, test.ok)
		}
	}
}
