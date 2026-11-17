package keyidentity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// securityKeyFile builds the OpenSSH private-key container OpenSSH writes for
// an sk-* identity. The private half is a device handle only; inspection reads
// the public half, so a placeholder there is enough to test what the file
// exposes before the device is touched.
func securityKeyFile(t *testing.T, encrypted bool) []byte {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicBlob := ssh.Marshal(struct {
		Type        string
		Key         []byte
		Application string
	}{ssh.KeyAlgoSKED25519, public, "ssh:"})

	stringField := func(value []byte) []byte {
		out := make([]byte, 4+len(value))
		binary.BigEndian.PutUint32(out, uint32(len(value)))
		copy(out[4:], value)
		return out
	}
	cipherName := "none"
	if encrypted {
		cipherName = "aes256-ctr"
	}
	body := []byte("openssh-key-v1\x00")
	body = append(body, stringField([]byte(cipherName))...)
	body = append(body, stringField([]byte("none"))...)
	body = append(body, stringField(nil)...)
	body = append(body, 0, 0, 0, 1) // one key
	body = append(body, stringField(publicBlob)...)
	body = append(body, stringField([]byte("placeholder"))...)
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
}

func TestSecurityKeyInspectNamesTheDeviceKey(t *testing.T) {
	data := securityKeyFile(t, false)
	info, err := InspectData("id_ed25519_sk", data)
	if err != nil {
		t.Fatalf("InspectData: %v", err)
	}
	if !info.SecurityKey {
		t.Fatal("SecurityKey = false")
	}
	if got, want := info.Algorithm, ssh.KeyAlgoSKED25519; got != want {
		t.Fatalf("Algorithm = %q, want %q", got, want)
	}
	if info.PublicKey == "" || info.Fingerprint == "" {
		t.Fatalf("public half is missing: %+v", info)
	}
	if info.Encrypted {
		t.Fatal("an unencrypted container reported as encrypted")
	}
}

func TestEncryptedSecurityKeyStillNamesTheKey(t *testing.T) {
	info, err := InspectData("id_ed25519_sk", securityKeyFile(t, true))
	if err != nil {
		t.Fatalf("InspectData: %v", err)
	}
	if !info.SecurityKey || !info.Encrypted {
		t.Fatalf("info = %+v, want security key and encrypted", info)
	}
	if info.Fingerprint == "" {
		t.Fatal("the public half was not read from the container")
	}
}

func TestSecurityKeyCannotBeParsedWithoutAnAgent(t *testing.T) {
	_, err := Parse(securityKeyFile(t, false), nil)
	if !errors.Is(err, ErrSecurityKeyNeedsAgent) {
		t.Fatalf("Parse error = %v, want ErrSecurityKeyNeedsAgent", err)
	}
	var securityKey *SecurityKeyError
	if !errors.As(err, &securityKey) {
		t.Fatalf("Parse error = %T, want *SecurityKeyError", err)
	}
	if securityKey.PublicKey.Type() != ssh.KeyAlgoSKED25519 {
		t.Fatalf("public key type = %q", securityKey.PublicKey.Type())
	}
}

func TestSecurityKeyPublicKeyDoesNotNeedAPubFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519_sk")
	if err := os.WriteFile(path, securityKeyFile(t, false), 0o600); err != nil {
		t.Fatal(err)
	}
	public, err := PublicKey(path)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if public.Type() != ssh.KeyAlgoSKED25519 {
		t.Fatalf("type = %q", public.Type())
	}
	if !bytes.Contains(ssh.MarshalAuthorizedKey(public), []byte(ssh.KeyAlgoSKED25519)) {
		t.Fatalf("authorized key = %q", ssh.MarshalAuthorizedKey(public))
	}
}
