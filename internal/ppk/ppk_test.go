package ppk

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

// ppkWriter builds the PPK files under test, following the same format the
// parser reads, so both sides of the format are exercised.
type ppkWriter struct {
	version    int
	algorithm  string
	encryption string
	comment    string
	public     []byte
	private    []byte // plaintext private blob
	passphrase []byte
}

func writePPK(w ppkWriter) []byte {
	var b strings.Builder
	line := func(name, value string) {
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString("\n")
	}
	line("PuTTY-User-Key-File-"+strconv.Itoa(w.version), w.algorithm)
	line("Encryption", w.encryption)
	line("Comment", w.comment)
	blob := func(name string, data []byte) {
		encoded := base64.StdEncoding.EncodeToString(data)
		count := (len(encoded) + 63) / 64
		line(name, strconv.Itoa(count))
		for len(encoded) > 64 {
			b.WriteString(encoded[:64])
			b.WriteString("\n")
			encoded = encoded[64:]
		}
		if encoded != "" {
			b.WriteString(encoded)
			b.WriteString("\n")
		}
	}
	blob("Public-Lines", w.public)
	private := w.private
	var key, iv, macKey []byte
	var padded []byte
	if w.encryption == "aes256-cbc" {
		if w.version >= 3 {
			salt := bytes.Repeat([]byte{7}, 16)
			material := argon2.IDKey(w.passphrase, salt, 1, 64, 1, 32+16+32)
			key, iv, macKey = material[:32], material[32:48], material[48:]
			line("Key-Derivation", "Argon2id")
			line("Argon2-Memory", "64")
			line("Argon2-Passes", "1")
			line("Argon2-Parallelism", "1")
			line("Argon2-Salt", "07070707070707070707070707070707")
		} else {
			var material []byte
			for counter := uint32(0); len(material) < 32; counter++ {
				hash := sha1.New()
				var ctr [4]byte
				binary.BigEndian.PutUint32(ctr[:], counter)
				hash.Write(ctr[:])
				hash.Write(w.passphrase)
				material = hash.Sum(material)
			}
			key, iv = material[:32], make([]byte, 16)
			mac := sha1.Sum(append([]byte("putty-private-key-file-mac-key"), w.passphrase...))
			macKey = mac[:]
		}
		padded = append([]byte(nil), private...)
		for len(padded)%aes.BlockSize != 0 {
			padded = append(padded, 0xAB)
		}
		block, _ := aes.NewCipher(key)
		private = make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(private, padded)
	} else if w.version >= 3 {
		macKey = nil
	} else {
		mac := sha1.Sum([]byte("putty-private-key-file-mac-key"))
		macKey = mac[:]
	}
	blob("Private-Lines", private)
	// The MAC covers the decrypted private blob, padding included.
	macPrivate := private
	if padded != nil {
		macPrivate = padded
	}
	input := sshString([]byte(w.algorithm))
	input = append(input, sshString([]byte(w.encryption))...)
	input = append(input, sshString([]byte(w.comment))...)
	input = append(input, sshString(w.public)...)
	input = append(input, sshString(macPrivate)...)
	digest := hmac.New(sha1.New, macKey)
	if w.version >= 3 {
		digest = hmac.New(sha256.New, macKey)
	}
	digest.Write(input)
	b.WriteString("Private-MAC: ")
	b.WriteString(toHex(digest.Sum(nil)))
	b.WriteString("\n")
	return []byte(b.String())
}

func sshString(s []byte) []byte {
	out := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(out, uint32(len(s)))
	copy(out[4:], s)
	return out
}

func mpint(n *big.Int) []byte {
	raw := n.Bytes()
	if len(raw) > 0 && raw[0]&0x80 != 0 {
		raw = append([]byte{0}, raw...)
	}
	return sshString(raw)
}

func toHex(data []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(data)*2)
	for _, b := range data {
		out = append(out, digits[b>>4], digits[b&0xF])
	}
	return string(out)
}

// ed25519PPK builds a PPK writer for a fresh seed-derived key.
func ed25519PPK(t *testing.T, version int, encryption string, passphrase []byte) ppkWriter {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubBlob := append(sshString([]byte("ssh-ed25519")), sshString(public)...)
	return ppkWriter{
		version: version, algorithm: "ssh-ed25519", encryption: encryption,
		comment: "e2e@ppk", public: pubBlob, private: append([]byte(nil), private[:32]...), passphrase: passphrase,
	}
}

func signVerify(t *testing.T, signer ssh.Signer) {
	t.Helper()
	message := []byte("ppk test message")
	signature, err := signer.Sign(rand.Reader, message)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := signer.PublicKey().Verify(message, signature); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestParseEd25519V2Plain(t *testing.T) {
	signer, err := Parse(writePPK(ed25519PPK(t, 2, "none", nil)), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	signVerify(t, signer)
}

func TestParseEd25519V3Plain(t *testing.T) {
	signer, err := Parse(writePPK(ed25519PPK(t, 3, "none", nil)), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	signVerify(t, signer)
}

func TestParseV3Encrypted(t *testing.T) {
	data := writePPK(ed25519PPK(t, 3, "aes256-cbc", []byte("correct horse")))
	_, err := Parse(data, nil)
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("want PassphraseMissingError, got %v", err)
	}
	if _, err := Parse(data, []byte("wrong")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("want ErrWrongPassphrase, got %v", err)
	}
	signer, err := Parse(data, []byte("correct horse"))
	if err != nil {
		t.Fatalf("parse with the right passphrase: %v", err)
	}
	signVerify(t, signer)
}

func TestParseV2Encrypted(t *testing.T) {
	data := writePPK(ed25519PPK(t, 2, "aes256-cbc", []byte("ppk-pass")))
	if _, err := Parse(data, []byte("nope")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("want ErrWrongPassphrase, got %v", err)
	}
	signer, err := Parse(data, []byte("ppk-pass"))
	if err != nil {
		t.Fatalf("parse with the right passphrase: %v", err)
	}
	signVerify(t, signer)
}

func TestParseRSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key.Precompute()
	pubBlob := append(sshString([]byte("ssh-rsa")), mpint(big.NewInt(int64(key.E)))...)
	pubBlob = append(pubBlob, mpint(key.N)...)
	private := mpint(key.D)
	private = append(private, mpint(key.Primes[0])...)
	private = append(private, mpint(key.Primes[1])...)
	private = append(private, mpint(key.Precomputed.Qinv)...)
	signer, err := Parse(writePPK(ppkWriter{version: 2, algorithm: "ssh-rsa", encryption: "none",
		comment: "rsa@ppk", public: pubBlob, private: private}), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := signer.PublicKey().Type(); got != "ssh-rsa" {
		t.Fatalf("algorithm %q", got)
	}
	signVerify(t, signer)
}

func TestParseECDSA(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const byteLen = 32
	point := make([]byte, 1+2*byteLen)
	point[0] = 4
	key.X.FillBytes(point[1 : 1+byteLen])
	key.Y.FillBytes(point[1+byteLen:])
	pubBlob := sshString([]byte("ecdsa-sha2-nistp256"))
	pubBlob = append(pubBlob, sshString([]byte("nistp256"))...)
	pubBlob = append(pubBlob, sshString(point)...)
	signer, err := Parse(writePPK(ppkWriter{version: 3, algorithm: "ecdsa-sha2-nistp256", encryption: "none",
		comment: "ecdsa@ppk", public: pubBlob, private: mpint(key.D)}), nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	signVerify(t, signer)
}

func TestCRLFTolerated(t *testing.T) {
	data := writePPK(ed25519PPK(t, 2, "none", nil))
	signer, err := Parse(bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), nil)
	if err != nil {
		t.Fatalf("parse CRLF: %v", err)
	}
	signVerify(t, signer)
}

func TestTamperedFileFailsMAC(t *testing.T) {
	data := string(writePPK(ed25519PPK(t, 2, "none", nil)))
	tampered := strings.Replace(data, "Comment: e2e@ppk", "Comment: evil@ppk", 1)
	if _, err := Parse([]byte(tampered), nil); err == nil || errors.Is(err, ErrNotPPK) {
		t.Fatalf("edited comment must fail the MAC, got %v", err)
	}
}

func TestUnsupportedFlavourAndVersion(t *testing.T) {
	writer := ed25519PPK(t, 3, "aes256-cbc", []byte("pw"))
	data := strings.Replace(string(writePPK(writer)), "Argon2id", "Argon2d", 1)
	_, err := Parse([]byte(data), []byte("pw"))
	if err == nil || !strings.Contains(err.Error(), "Argon2d") {
		t.Fatalf("want a clear Argon2d error, got %v", err)
	}
	v1 := strings.Replace(string(writePPK(writer)), "PuTTY-User-Key-File-3", "PuTTY-User-Key-File-1", 1)
	if _, err := Parse([]byte(v1), nil); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want a version error, got %v", err)
	}
	if _, err := Parse([]byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"), nil); !errors.Is(err, ErrNotPPK) {
		t.Fatalf("want ErrNotPPK, got %v", err)
	}
}

func TestParsePublicWithoutPassphrase(t *testing.T) {
	public, comment, encrypted, err := ParsePublic(writePPK(ed25519PPK(t, 3, "aes256-cbc", []byte("pw"))))
	if err != nil {
		t.Fatalf("parse public: %v", err)
	}
	if !encrypted || comment != "e2e@ppk" || public.Type() != "ssh-ed25519" {
		t.Fatalf("public info: encrypted=%v comment=%q type=%q", encrypted, comment, public.Type())
	}
}

func TestMalformedFilesRejected(t *testing.T) {
	full := string(writePPK(ed25519PPK(t, 2, "none", nil)))
	cases := map[string]string{
		"empty":              "",
		"truncated":          full[:strings.Index(full, "Private-Lines")],
		"renamed mac header": strings.Replace(full, "Private-MAC", "Private-Mac", 1),
		"bad count":          strings.Replace(full, "Public-Lines: ", "Public-Lines: x", 1),
		"count beyond file":  strings.Replace(full, "Public-Lines: ", "Public-Lines: 9999", 1),
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data), nil); err == nil {
			t.Fatalf("%s: parse unexpectedly succeeded", name)
		}
	}
	// A private blob whose fields do not parse is refused even with a valid MAC.
	writer := ed25519PPK(t, 2, "none", nil)
	writer.private = []byte{1, 2, 3}
	if _, err := Parse(writePPK(writer), nil); err == nil {
		t.Fatal("garbage private blob must not parse")
	}
}

func TestArgonParametersBeyondTheLimitsAreRefusedBeforeAnyWork(t *testing.T) {
	data := string(writePPK(ed25519PPK(t, 3, "aes256-cbc", []byte("pass"))))
	hostile := strings.Replace(data, "Argon2-Memory: 64", "Argon2-Memory: 4294967295", 1)
	if hostile == data {
		t.Fatal("the fixture does not carry the memory parameter the test rewrites")
	}
	_, err := Parse([]byte(hostile), []byte("pass"))
	if err == nil || !strings.Contains(err.Error(), "beyond what is supported") {
		t.Fatalf("a 4 TiB Argon2 request was not refused: %v", err)
	}
}

func TestAMACOfTheWrongLengthDoesNotVerify(t *testing.T) {
	for _, version := range []int{2, 3} {
		data := string(writePPK(ed25519PPK(t, version, "none", nil)))
		lines := strings.Split(data, "\n")
		for index, line := range lines {
			if strings.HasPrefix(line, "Private-MAC: ") {
				lines[index] = "Private-MAC: 00"
			}
		}
		if _, err := Parse([]byte(strings.Join(lines, "\n")), nil); err == nil {
			t.Fatalf("version %d: a one-byte MAC was accepted", version)
		}
	}
}
