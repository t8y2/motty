package selfupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The system's refusal covers a download with no ticket and a machine that
// cannot reach Apple, so the caller has to be able to tell it apart from a
// signature that does not match.
func TestAGatekeeperRefusalIsReportedAsItsOwnFailure(t *testing.T) {
	original := run
	t.Cleanup(func() { run = original })
	run = func(_ context.Context, _ []string, name string, args ...string) (string, error) {
		switch {
		case name == "/usr/bin/codesign" && len(args) > 0 && args[0] == "--verify":
			return "", nil
		case name == "/usr/bin/codesign":
			return "Authority=Developer ID Application: Example (TEAM1234)\nTeamIdentifier=TEAM1234\n", nil
		case name == "/usr/sbin/spctl":
			return "Motty.app: rejected\nsource=Unnotarized Developer ID", errors.New("exit status 3")
		}
		return "", nil
	}

	err := verifyMac(context.Background(), "/tmp/Motty.app", Identity{Team: "TEAM1234"})
	if !errors.Is(err, ErrNotAccepted) {
		t.Fatalf("error = %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("the error does not carry what the system said: %v", err)
	}
	if errors.Is(err, ErrUnsigned) {
		t.Fatal("a refusal was reported as an unsigned copy")
	}
}
