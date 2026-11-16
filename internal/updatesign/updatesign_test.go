package updatesign

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
)

func keyPair(t *testing.T) (minisign.PublicKey, minisign.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := minisign.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey
}

func signed(t *testing.T, privateKey minisign.PrivateKey, body []byte, version, file string) string {
	t.Helper()
	signature, err := Sign(privateKey, bytes.NewReader(body), version, file, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

func TestASignatureNamesTheVersionAndFileItWasMadeFor(t *testing.T) {
	publicKey, privateKey := keyPair(t)
	body := []byte("the package bytes")
	signature := signed(t, privateKey, body, "0.1.2", "motty-0.1.2-darwin-arm64.zip")

	if err := Verify(publicKey, bytes.NewReader(body), signature, "0.1.2", "motty-0.1.2-darwin-arm64.zip"); err != nil {
		t.Fatalf("a good signature was refused: %v", err)
	}
	for name, test := range map[string]struct{ version, file string }{
		"newer version": {"0.1.3", "motty-0.1.2-darwin-arm64.zip"},
		"older version": {"0.1.1", "motty-0.1.2-darwin-arm64.zip"},
		"another file":  {"0.1.2", "motty-0.1.2-linux-amd64.tar.gz"},
	} {
		if err := Verify(publicKey, bytes.NewReader(body), signature, test.version, test.file); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestATamperedPackageIsRefused(t *testing.T) {
	publicKey, privateKey := keyPair(t)
	body := []byte("the package bytes")
	signature := signed(t, privateKey, body, "0.1.2", "a.zip")

	tampered := append([]byte(nil), body...)
	tampered[0] ^= 0xff
	if err := Verify(publicKey, bytes.NewReader(tampered), signature, "0.1.2", "a.zip"); !errors.Is(err, ErrSignature) {
		t.Errorf("a changed package gave %v", err)
	}
	if err := Verify(publicKey, bytes.NewReader(append(body, 'x')), signature, "0.1.2", "a.zip"); !errors.Is(err, ErrSignature) {
		t.Errorf("a longer package gave %v", err)
	}
}

func TestAnotherKeyCannotSignForMotty(t *testing.T) {
	_, otherPrivate := keyPair(t)
	publicKey, _ := keyPair(t)
	body := []byte("the package bytes")
	signature := signed(t, otherPrivate, body, "0.1.2", "a.zip")
	if err := Verify(publicKey, bytes.NewReader(body), signature, "0.1.2", "a.zip"); !errors.Is(err, ErrSignature) {
		t.Errorf("a foreign key gave %v", err)
	}
}

func TestAMissingOrBrokenSignatureIsRefused(t *testing.T) {
	publicKey, _ := keyPair(t)
	body := []byte("the package bytes")
	if err := Verify(publicKey, bytes.NewReader(body), "", "0.1.2", "a.zip"); !errors.Is(err, ErrUnsigned) {
		t.Errorf("no signature gave %v", err)
	}
	for name, signature := range map[string]string{
		"not base64": "!!!",
		"too long":   strings.Repeat("A", 8192),
		"empty":      "Cg==",
	} {
		if err := Verify(publicKey, bytes.NewReader(body), signature, "0.1.2", "a.zip"); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAFeedSignatureMustNameTheReleaseKey(t *testing.T) {
	publicKey, privateKey := keyPair(t)
	signature := signed(t, privateKey, []byte("the package bytes"), "0.1.2", "a.zip")
	if err := UsableWith(publicKey, signature); err != nil {
		t.Fatalf("a signature from the key was refused: %v", err)
	}
	otherKey, _ := keyPair(t)
	if err := UsableWith(otherKey, signature); !errors.Is(err, ErrSignature) {
		t.Errorf("another key accepted the signature: %v", err)
	}
	if err := UsableWith(publicKey, ""); !errors.Is(err, ErrUnsigned) {
		t.Errorf("no signature gave %v", err)
	}
	if err := UsableWith(publicKey, "!!"); !errors.Is(err, ErrSignature) {
		t.Errorf("a broken signature gave %v", err)
	}
}

func TestTheEmbeddedReleaseKeyParses(t *testing.T) {
	if id := ReleaseKey().ID(); id == 0 {
		t.Fatal("the embedded key has no id")
	}
}
