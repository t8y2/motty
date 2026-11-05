package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrUnsigned means the running program is not signed in a way a download can be
// compared with, so there is nothing to hold an update to.
var ErrUnsigned = errors.New("selfupdate: this copy of Motty is not signed by a developer certificate, so an update cannot be checked against it")

// ErrNotAccepted means the system refused to run the new version. A ticket that
// is not stapled to the download, and a machine that cannot reach the notarization
// service, both end here, so the caller cannot tell them apart and says so.
var ErrNotAccepted = errors.New("selfupdate: the system will not run the new version")

// Identity is who signed a program: on macOS the Developer ID team, on Windows
// the certificate's subject.
type Identity struct {
	Team    string // macOS team identifier
	Subject string // Windows signer certificate subject
}

// run runs a command and returns what it printed, standard error included:
// codesign writes its report there. Tests replace it to stand in for the
// system's verdict.
var run = execCommand

func execCommand(ctx context.Context, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	hide(cmd)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.String(), err
}

// parseCodesign reads `codesign -dvv` output. Only a Developer ID signature
// counts: the system judges nothing else fit to run when downloaded.
func parseCodesign(output string) (Identity, error) {
	var team, authority string
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "TeamIdentifier":
			team = value
		case "Authority":
			if authority == "" {
				authority = value
			}
		}
	}
	if team == "" || team == "not set" || !strings.HasPrefix(authority, "Developer ID Application") {
		return Identity{}, ErrUnsigned
	}
	return Identity{Team: team}, nil
}

// macIdentity is who signed the application at path.
func macIdentity(ctx context.Context, app string) (Identity, error) {
	output, err := run(ctx, nil, "/usr/bin/codesign", "-dvv", app)
	if err != nil {
		return Identity{}, fmt.Errorf("%w (%s)", ErrUnsigned, firstLine(output))
	}
	return parseCodesign(output)
}

// verifyMac holds the downloaded application to the signature of the running
// one: intact, signed by the same team, and acceptable to Gatekeeper.
func verifyMac(ctx context.Context, app string, want Identity) error {
	if output, err := run(ctx, nil, "/usr/bin/codesign", "--verify", "--deep", "--strict", app); err != nil {
		return fmt.Errorf("selfupdate: the new version's signature is not valid: %s", firstLine(output))
	}
	got, err := macIdentity(ctx, app)
	if err != nil {
		return fmt.Errorf("selfupdate: the new version is not signed by a developer certificate")
	}
	if got.Team != want.Team {
		return fmt.Errorf("selfupdate: the new version is signed by team %s, not %s", got.Team, want.Team)
	}
	if output, err := run(ctx, nil, "/usr/sbin/spctl", "--assess", "--type", "execute", app); err != nil {
		return fmt.Errorf("%w: %s", ErrNotAccepted, firstLine(output))
	}
	return nil
}

// powershellSignature prints a program's Authenticode status and signer.
const powershellSignature = `$s = Get-AuthenticodeSignature -LiteralPath $env:MOTTY_VERIFY; Write-Output $s.Status; if ($s.SignerCertificate) { Write-Output $s.SignerCertificate.Subject }`

// parseSignature reads what powershellSignature printed.
func parseSignature(output string) (status string, identity Identity) {
	lines := strings.Split(strings.ReplaceAll(output, "\r", ""), "\n")
	if len(lines) > 0 {
		status = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		identity.Subject = strings.TrimSpace(lines[1])
	}
	return status, identity
}

func windowsSignature(ctx context.Context, path string) (string, Identity, error) {
	output, err := run(ctx, []string{"MOTTY_VERIFY=" + path}, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", powershellSignature)
	if err != nil {
		return "", Identity{}, fmt.Errorf("selfupdate: reading a signature: %s", firstLine(output))
	}
	status, identity := parseSignature(output)
	return status, identity, nil
}

// windowsIdentity is who signed the program at path, if its signature is valid.
func windowsIdentity(ctx context.Context, path string) (Identity, error) {
	status, identity, err := windowsSignature(ctx, path)
	if err != nil {
		return Identity{}, err
	}
	if status != "Valid" || identity.Subject == "" {
		return Identity{}, ErrUnsigned
	}
	return identity, nil
}

// verifyWindows holds the downloaded installer to the signer of the running program.
func verifyWindows(ctx context.Context, setup string, want Identity) error {
	status, got, err := windowsSignature(ctx, setup)
	if err != nil {
		return err
	}
	if status != "Valid" || got.Subject == "" {
		return fmt.Errorf("selfupdate: the installer's signature is not valid (%s)", status)
	}
	if got.Subject != want.Subject {
		return fmt.Errorf("selfupdate: the installer is signed by %q, not %q", got.Subject, want.Subject)
	}
	return nil
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if line, _, found := strings.Cut(text, "\n"); found {
		return strings.TrimSpace(line)
	}
	return text
}
