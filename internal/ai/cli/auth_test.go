package cli

import (
	"os"
	"reflect"
	"testing"
)

func TestOfficialLoginCommandsUseSelectedExecutableAndNoShell(t *testing.T) {
	path, _ := os.Executable()
	for _, tc := range []struct {
		kind   string
		device bool
		want   []string
	}{
		{"claude-code-cli", false, []string{"auth", "login"}},
		{"codex-cli", false, []string{"login"}},
		{"codex-cli", true, []string{"login", "--device-auth"}},
		{"opencode-cli", false, []string{"auth", "login"}},
		{"qoder-cli", false, []string{"login"}},
	} {
		cmd, err := LoginCommand(tc.kind, path, tc.device)
		if err != nil || cmd.Program != path || !reflect.DeepEqual(cmd.Args, tc.want) {
			t.Fatal(tc.kind, cmd, err)
		}
	}
	if _, err := LoginCommand("opencode-cli", path, true); err == nil {
		t.Fatal("unsupported device flow")
	}
}
func TestAuthStatusClassificationDoesNotExposeDetails(t *testing.T) {
	tests := []struct {
		kind           string
		stdout, stderr string
		failed         bool
		want           AuthState
	}{
		{"claude-code-cli", `{"loggedIn":true,"email":"PRIVATE"}`, "", false, AuthConfigured},
		{"claude-code-cli", `{"loggedIn":false}`, "", true, AuthSignedOut},
		{"codex-cli", "", "Logged in using an API key - PRIVATE", false, AuthConfigured},
		{"codex-cli", "", "Not logged in", true, AuthSignedOut},
		{"codex-cli", "", "network failed", true, AuthUnknown},
		{"opencode-cli", "provider config exists", "", false, AuthUnknown},
		{"qoder-cli", `{"logged_in":true,"version":"1.1.89"}`, "", false, AuthConfigured},
		{"qoder-cli", `{"logged_in":false,"version":"1.1.89"}`, "", false, AuthSignedOut},
		{"qoder-cli", `not json`, "", true, AuthUnknown},
	}
	for _, tc := range tests {
		got := classifyAuth(tc.kind, []byte(tc.stdout), []byte(tc.stderr), tc.failed)
		if got != tc.want {
			t.Fatal(tc.kind, got, tc.want)
		}
	}
}

func TestLoginReservationSerializesAcrossWindows(t *testing.T) {
	path, _ := os.Executable()
	release, err := ReserveLogin("codex-cli", path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if other, err := ReserveLogin("codex-cli", path); err == nil {
		other()
		t.Fatal("duplicate auth process allowed")
	}
	release()
	again, err := ReserveLogin("codex-cli", path)
	if err != nil {
		t.Fatal("reservation not released", err)
	}
	again()
}
