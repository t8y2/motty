package keyidentity

import (
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGeneratedKeyRoundTrips(t *testing.T) {
	for _, passphrase := range []string{"", "correct horse"} {
		generated, err := Generate("me@laptop", []byte(passphrase))
		if err != nil {
			t.Fatal(err)
		}
		info, err := InspectData("sshkey:test", generated.Private)
		if err != nil {
			t.Fatal(err)
		}
		if info.Encrypted != (passphrase != "") || info.Algorithm != ssh.KeyAlgoED25519 {
			t.Fatalf("passphrase %q: info = %+v", passphrase, info)
		}
		// What the user pastes on a server must be the key the file holds.
		public, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(generated.Public))
		if err != nil || comment != "me@laptop" || ssh.FingerprintSHA256(public) != info.Fingerprint {
			t.Fatalf("public %q: %v, comment %q, fingerprint %s want %s", generated.Public, err, comment, ssh.FingerprintSHA256(public), info.Fingerprint)
		}
		if !strings.HasPrefix(info.PublicKey, "ssh-ed25519 ") || strings.Contains(info.PublicKey, "\n") {
			t.Fatalf("public key line = %q", info.PublicKey)
		}
		var secret []byte
		if passphrase != "" {
			secret = []byte(passphrase)
		}
		signer, err := Parse(generated.Private, secret)
		if err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != info.Fingerprint {
			t.Fatalf("Parse = %v, %v", signer, err)
		}
	}
}

func TestNormalizeAcceptsWhatPastingProduces(t *testing.T) {
	generated, err := Generate("", nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := InspectData("k", generated.Private)
	if err != nil {
		t.Fatal(err)
	}
	pasted := "\n   " + strings.ReplaceAll(strings.TrimSpace(string(generated.Private)), "\n", "\r\n   ") + "   \r\n\r\n"
	data, err := Normalize(pasted)
	if err != nil {
		t.Fatal(err)
	}
	got, err := InspectData("k", data)
	if err != nil || got.Fingerprint != want.Fingerprint {
		t.Fatalf("InspectData = %+v, %v; want fingerprint %s", got, err, want.Fingerprint)
	}
}

func TestNormalizeRefusesWhatIsNotAKey(t *testing.T) {
	for name, text := range map[string]string{
		"empty":      " \n\t ",
		"public key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFake me@host",
		"prose":      "here is my key",
	} {
		if _, err := Normalize(text); err == nil {
			t.Errorf("%s: Normalize accepted %q", name, text)
		}
	}
	if _, err := Normalize("-----BEGIN OPENSSH PRIVATE KEY-----\n" + strings.Repeat("A", maxPrivateKeySize) + "\n-----END OPENSSH PRIVATE KEY-----"); err == nil {
		t.Error("Normalize accepted a key over the size limit")
	}
}
