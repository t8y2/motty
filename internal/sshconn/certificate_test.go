package sshconn

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// certificateFor signs public as a user or host certificate with ca.
func certificateFor(t *testing.T, ca ssh.Signer, public ssh.PublicKey, certType uint32, principals []string) *ssh.Certificate {
	t.Helper()
	certificate := &ssh.Certificate{
		CertType:        certType,
		Key:             public,
		ValidPrincipals: principals,
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(time.Hour).Unix()),
	}
	if err := certificate.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	return certificate
}

func writeCertificate(t *testing.T, path string, certificate *ssh.Certificate) {
	t.Helper()
	line := certificate.Type() + " " + base64.StdEncoding.EncodeToString(certificate.Marshal()) + " test-cert\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestUserCertificateAuthenticates proves the client offers the certificate
// that accompanies an identity file, and that the server accepts it as the
// credential.
func TestUserCertificateAuthenticates(t *testing.T) {
	dir := t.TempDir()
	identity, signer := writeKey(t, dir)
	caSigner := newSigner(t)
	certificate := certificateFor(t, caSigner, signer.PublicKey(), ssh.UserCert, []string{"tester"})
	writeCertificate(t, identity+"-cert.pub", certificate)

	certifiedSigner, err := certificateSigner(identity, signer)
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{certifiedSigner.PublicKey()}
	})
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{identity}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	status := harness.until("the certificate to authenticate", func(status Status) bool {
		return status.Phase == PhaseConnected || status.Phase == PhaseFailed
	})
	if status.Phase != PhaseConnected {
		t.Fatalf("certificate authentication failed: %v", status.Err)
	}
	if status.Method != "publickey" {
		t.Fatalf("authenticated by %q", status.Method)
	}
}

// TestEncryptedCertifiedIdentityUnlocksOnDemand proves an encrypted identity
// with a certificate still offers the certificate, and unlocks on demand.
func TestEncryptedCertifiedIdentityUnlocksOnDemand(t *testing.T) {
	_, private := newPrivate(t)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("locked"))
	if err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(identity, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}
	plain, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	caSigner := newSigner(t)
	certificate := certificateFor(t, caSigner, plain.PublicKey(), ssh.UserCert, []string{"tester"})
	writeCertificate(t, identity+"-cert.pub", certificate)
	certifiedSigner, err := certificateSigner(identity, plain)
	if err != nil {
		t.Fatalf("certify: %v", err)
	}

	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{certifiedSigner.PublicKey()}
	})
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{identity}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	if prompt := harness.phase(PhaseKeyPassphrasePrompt); prompt.IdentityPath != identity {
		t.Fatalf("prompt = %+v", prompt)
	}
	harness.conn.SubmitKeyPassphrase("locked")
	status := harness.until("the certificate to authenticate", func(status Status) bool {
		return status.Phase == PhaseConnected || status.Phase == PhaseFailed
	})
	if status.Phase != PhaseConnected || status.Method != "publickey" {
		t.Fatalf("encrypted certificate authentication failed: %+v", status)
	}
}

// TestCertificateMismatchIsSkipped proves a certificate that belongs to
// another key is reported as skipped instead of silently authenticating with
// the bare key.
func TestCertificateMismatchIsSkipped(t *testing.T) {
	dir := t.TempDir()
	identity, signer := writeKey(t, dir)
	caSigner := newSigner(t)
	other := newSigner(t)
	certificate := certificateFor(t, caSigner, other.PublicKey(), ssh.UserCert, []string{"tester"})
	writeCertificate(t, identity+"-cert.pub", certificate)

	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{signer.PublicKey()} // the bare key would pass
	})
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{identity}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	status := harness.until("the mismatching certificate to be refused", func(status Status) bool {
		return status.Phase == PhaseFailed
	})
	if len(status.Skipped) == 0 {
		t.Fatal("the mismatching certificate was not reported as skipped")
	}
}

// TestHostCertificateTrustedByCA proves a server that presents a host
// certificate connects without a prompt when the signing CA is on file as
// @cert-authority, and fails as unknown when it is not.
func TestHostCertificateTrustedByCA(t *testing.T) {
	caSigner := newSigner(t)
	hostSigner := newSigner(t)
	certificate := certificateFor(t, caSigner, hostSigner.PublicKey(), ssh.HostCert, []string{"127.0.0.1"})
	certifiedHost, err := ssh.NewCertSigner(certificate, hostSigner)
	if err != nil {
		t.Fatal(err)
	}

	server := startServer(t, func(server *testServer) {
		server.hostKey = certifiedHost
		server.password = "pw"
	})
	harness := dial(t, server, nil)

	// Without the CA on file the certificate is an unknown host key.
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.RejectHostKey()

	// With the CA on file before the dial, the certificate verifies without a
	// prompt: the connection goes straight to the password question.
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	line := "@cert-authority " + bracketed(server) + " " + caSigner.PublicKey().Type() +
		" " + base64.StdEncoding.EncodeToString(caSigner.PublicKey().Marshal()) + "\n"
	if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	harness = dial(t, server, func(config *Config) { config.KnownHostsPath = knownHosts })
	harness.phase(PhasePasswordPrompt) // past host-key verification, waiting for auth
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)
}

// bracketed is how known_hosts names this test server's address.
func bracketed(server *testServer) string {
	host, port := server.host()
	return "[" + host + "]:" + itoa(port)
}
