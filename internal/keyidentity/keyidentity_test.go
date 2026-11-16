package keyidentity

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func writeIdentity(t testing.TB, passphrase string) (string, ssh.Signer) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(private, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return path, signer
}

func TestPublicIdentityNeedsNoPrivateKeyOrPassphrase(t *testing.T) {
	path, signer := writeIdentity(t, "locked")
	public, err := PublicKey(path)
	if err != nil || ssh.FingerprintSHA256(public) != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("encrypted identity public key = %v, %v", public, err)
	}
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, path + ".pub"} {
		public, err := PublicKey(candidate)
		if err != nil || ssh.FingerprintSHA256(public) != ssh.FingerprintSHA256(signer.PublicKey()) {
			t.Fatalf("public-only identity = %v, %v", public, err)
		}
	}
}

func TestInspectAndLoadPrivateKeys(t *testing.T) {
	for _, test := range []struct {
		name       string
		passphrase string
		encrypted  bool
	}{
		{name: "plain"},
		{name: "encrypted", passphrase: "correct horse", encrypted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, want := writeIdentity(t, test.passphrase)
			info, err := Inspect(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Path != path || info.Name != "id_ed25519" || info.Algorithm != ssh.KeyAlgoED25519 ||
				info.Fingerprint != ssh.FingerprintSHA256(want.PublicKey()) || info.Encrypted != test.encrypted {
				t.Fatalf("info = %+v", info)
			}
			var passphrase []byte
			if test.passphrase != "" {
				passphrase = []byte(test.passphrase)
			}
			got, err := Load(path, passphrase)
			if err != nil {
				t.Fatalf("Load = %v, %v", got, err)
			}
			if err := got.PublicKey().Verify([]byte("probe"), sign(t, want, []byte("probe"))); err != nil {
				t.Fatalf("loaded key did not verify: %v", err)
			}
		})
	}
}

func TestInspectRejectsInvalidAndOversizedFiles(t *testing.T) {
	directory := t.TempDir()
	invalid := filepath.Join(directory, "invalid")
	if err := os.WriteFile(invalid, []byte("not a private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(invalid); err == nil {
		t.Fatal("invalid file was accepted")
	}
	large := filepath.Join(directory, "large")
	if err := os.WriteFile(large, make([]byte, maxPrivateKeySize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(large); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large error = %v", err)
	}
	if _, err := Inspect(directory); err == nil {
		t.Fatal("directory was accepted")
	}
}

func TestWrongPassphraseFails(t *testing.T) {
	path, _ := writeIdentity(t, "right")
	if _, err := Load(path, []byte("wrong")); err == nil {
		t.Fatal("wrong passphrase was accepted")
	}
}

func BenchmarkInspect(b *testing.B) {
	path, _ := writeIdentity(b, "benchmark")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Inspect(path); err != nil {
			b.Fatal(err)
		}
	}
}

func sign(t testing.TB, signer ssh.Signer, message []byte) *ssh.Signature {
	t.Helper()
	signature, err := signer.Sign(rand.Reader, message)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

// testSSHString wraps one SSH wire string for the PPK test fixtures.
func testSSHString(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

// writePPK writes a minimal unencrypted PPK version 2 file for one key.
func writePPK(t testing.TB, private ed25519.PrivateKey, public ed25519.PublicKey) string {
	t.Helper()
	pubBlob := append(testSSHString([]byte("ssh-ed25519")), testSSHString(public)...)
	blobSection := func(name string, data []byte) string {
		lines := chunk(base64.StdEncoding.EncodeToString(data))
		return name + ": " + strconv.Itoa(strings.Count(lines, "\n")) + "\n" + lines
	}
	body := "PuTTY-User-Key-File-2: ssh-ed25519\nEncryption: none\nComment: ppk\n" +
		blobSection("Public-Lines", pubBlob) +
		blobSection("Private-Lines", private[:32]) +
		"Private-MAC: " + ppkMAC(pubBlob, private[:32]) + "\n"
	path := filepath.Join(t.TempDir(), "key.ppk")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func chunk(encoded string) string {
	var out strings.Builder
	for len(encoded) > 64 {
		out.WriteString(encoded[:64])
		out.WriteString("\n")
		encoded = encoded[64:]
	}
	out.WriteString(encoded)
	out.WriteString("\n")
	return out.String()
}

func ppkMAC(public, private []byte) string {
	input := testSSHString([]byte("ssh-ed25519"))
	input = append(input, testSSHString([]byte("none"))...)
	input = append(input, testSSHString([]byte("ppk"))...)
	input = append(input, testSSHString(public)...)
	input = append(input, testSSHString(private)...)
	mac := sha1.Sum([]byte("putty-private-key-file-mac-key"))
	digest := hmac.New(sha1.New, mac[:])
	digest.Write(input)
	return hex.EncodeToString(digest.Sum(nil))
}

// TestLoadReadsPPK proves a PuTTY key file loads through the same entry point
// as an OpenSSH identity, and Inspect describes it.
func TestLoadReadsPPK(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := writePPK(t, private, public)
	signer, err := Load(path, nil)
	if err != nil {
		t.Fatalf("load ppk: %v", err)
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Fatalf("ppk loaded as %q", signer.PublicKey().Type())
	}
	info, err := Inspect(path)
	if err != nil {
		t.Fatalf("inspect ppk: %v", err)
	}
	if info.Encrypted || info.Algorithm != "ssh-ed25519" {
		t.Fatalf("ppk info: %+v", info)
	}
}

// TestLoadNamesSecurityKeys proves an sk- identity fails with the limit named.
func TestLoadNamesSecurityKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_sk")
	if err := os.WriteFile(path, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\ntest body mentioning sk-ssh-ed25519@openssh.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, nil)
	if err == nil || !strings.Contains(err.Error(), "hardware security keys") {
		t.Fatalf("want a named security-key error, got %v", err)
	}
}

func TestLoadEncryptedPKCS8(t *testing.T) {
	// Produced by OpenSSL 3 and ssh-keygen; each covers a different cipher and
	// PBKDF2 hash, and all use the passphrase "correct horse".
	for _, name := range []string{
		"pkcs8_ed25519_aes256_sha256",
		"pkcs8_rsa_aes128_sha1",
		"pkcs8_ecdsa_des3_sha256",
		"ssh_keygen_pkcs8_rsa",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", name+".pem")
			publicText, err := os.ReadFile(filepath.Join("testdata", name+".pub"))
			if err != nil {
				t.Fatal(err)
			}
			want, _, _, _, err := ssh.ParseAuthorizedKey(publicText)
			if err != nil {
				t.Fatal(err)
			}

			info, err := Inspect(path)
			if err != nil || !info.Encrypted {
				t.Fatalf("Inspect = %+v, %v; want an encrypted key", info, err)
			}
			_, err = Load(path, nil)
			var missing *ssh.PassphraseMissingError
			if !errors.As(err, &missing) {
				t.Fatalf("Load without a passphrase = %v, want ssh.PassphraseMissingError", err)
			}
			if _, err := Load(path, []byte("wrong horse")); err == nil {
				t.Fatal("Load accepted a wrong passphrase")
			}
			signer, err := Load(path, []byte("correct horse"))
			if err != nil {
				t.Fatal(err)
			}
			if got := ssh.FingerprintSHA256(signer.PublicKey()); got != ssh.FingerprintSHA256(want) {
				t.Fatalf("loaded key %s, want %s", got, ssh.FingerprintSHA256(want))
			}
			signature := sign(t, signer, []byte("probe"))
			if err := want.Verify([]byte("probe"), signature); err != nil {
				t.Fatalf("signature did not verify against the file's public key: %v", err)
			}
		})
	}
}

func TestInspectNamesUnsupportedPKCS8Schemes(t *testing.T) {
	// The legacy PBES1 scheme of `openssl pkcs8 -topk8 -v1`: re-export is the
	// only way forward, and the message must say so rather than looking like a
	// corrupt file.
	der, err := base64.StdEncoding.DecodeString("MFgwHAYKKoZIhvcNAQwBAzAOBAib6sVjTeUhlQICCAAEODfkXOA0FGQvBSdX6VhUONvBCNPN6LSNZm3Tw7wIvDX/1acoYZr7HO3CcLUVn3fTlB/8e9dpcpMi")
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "legacy")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(path); err == nil || !strings.Contains(err.Error(), "legacy scheme") {
		t.Fatalf("Inspect = %v, want the legacy scheme named", err)
	}
}
