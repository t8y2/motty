package workbench

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"golang.org/x/crypto/ssh"
	"motty/internal/keyidentity"
)

// writeWorkbenchCertificate signs the key at identity with a fresh CA and writes
// its user certificate at path, as OpenSSH pairs a key with -cert.pub.
func writeWorkbenchCertificate(t *testing.T, identity, path string) {
	t.Helper()
	signer, err := keyidentity.Load(identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &ssh.Certificate{
		CertType:        ssh.UserCert,
		Key:             signer.PublicKey(),
		ValidPrincipals: []string{"tester"},
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(time.Hour).Unix()),
	}
	if err := certificate.SignCert(rand.Reader, newTestSigner(t)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, ssh.MarshalAuthorizedKey(certificate), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestImportedKeyKeepsTheCertificateBesideIt proves an imported key takes the
// -cert.pub file OpenSSH pairs with it into the vault, so the key still signs in
// with its certificate after both files are gone.
func TestImportedKeyKeepsTheCertificateBesideIt(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_ed25519", "")
	writeWorkbenchCertificate(t, identity, identity+"-cert.pub")

	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{identity}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	if len(state.identities) != 1 || !state.identities[0].Certificate {
		t.Fatalf("certificate was not kept with the key: %+v", state.identities)
	}
	if got := state.identities[0].cells[3]; got != state.messages.KeyCertificatePresent {
		t.Fatalf("certificate cell = %q, want %q", got, state.messages.KeyCertificatePresent)
	}
	record, err := readStoredKeyRecord(state.secrets.vault, state.identities[0].Path)
	if err != nil || record.Certificate == "" {
		t.Fatalf("vault record = %+v, %v", record, err)
	}

	// The certificate, like the key, no longer depends on its file.
	for _, path := range []string{identity, identity + "-cert.pub"} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	restarted := newTestState(t, options)
	if len(restarted.identities) != 1 || !restarted.identities[0].Certificate {
		t.Fatalf("certificate lost with its file: %+v", restarted.identities)
	}
}

// TestCertificateAttachedFromTheKeychainIsKeptWithTheKey proves a key imported
// without a certificate can be given one afterwards, and that a certificate for
// another key is refused instead of kept.
func TestCertificateAttachedFromTheKeychainIsKeptWithTheKey(t *testing.T) {
	identity := writeWorkbenchIdentity(t, "id_ed25519", "")
	issued := filepath.Join(t.TempDir(), "issued-cert.pub")
	writeWorkbenchCertificate(t, identity, issued)
	other := writeWorkbenchIdentity(t, "id_other", "")
	wrong := filepath.Join(t.TempDir(), "other-cert.pub")
	writeWorkbenchCertificate(t, other, wrong)

	options := freshOptions(t)
	files := []string{identity}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return files, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	if len(state.identities) != 1 || state.identities[0].Certificate {
		t.Fatalf("key without a certificate = %+v", state.identities)
	}

	// A certificate for a different key changes nothing.
	files = []string{wrong}
	state.Click(hitKeysCertificate)
	if state.identities[0].Certificate {
		t.Fatal("a certificate for another key was kept")
	}

	files = []string{issued}
	state.Click(hitKeysCertificate)
	if !state.identities[0].Certificate {
		t.Fatal("the certificate was not attached")
	}
	record, err := readStoredKeyRecord(state.secrets.vault, state.identities[0].Path)
	if err != nil || record.Certificate == "" {
		t.Fatalf("vault record = %+v, %v", record, err)
	}
	attached := record.Certificate

	// Attaching again replaces the certificate rather than duplicating it.
	second := filepath.Join(t.TempDir(), "renewed-cert.pub")
	writeWorkbenchCertificate(t, identity, second)
	files = []string{second}
	state.Click(hitKeysCertificate)
	record, err = readStoredKeyRecord(state.secrets.vault, state.identities[0].Path)
	if err != nil || record.Certificate == "" || record.Certificate == attached {
		t.Fatalf("certificate was not replaced: %+v, %v", record, err)
	}
}
