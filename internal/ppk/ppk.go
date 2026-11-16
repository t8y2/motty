// Package ppk reads PuTTY's PPK private-key files (format versions 2 and 3,
// the ones puttygen has written since 2004 and 2020) and turns them into SSH
// signers, so a .ppk file works wherever an OpenSSH identity file does.
//
// The format: header lines "Name: value", a base64 public blob that is an SSH
// wire public key, a base64 private blob whose field layout depends on the
// algorithm, optional AES-256-CBC encryption of that private blob (version 3
// derives its keys with Argon2id, version 2 with SHA-1 chains), and a MAC over
// the header values and both blobs. Everything here is bounded; a damaged file
// never allocates by an attacker-chosen amount.
package ppk

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"
)

// Header is the first line of a PPK file, before the colon and space.
const Header = "PuTTY-User-Key-File-"

// ErrNotPPK reports a file that does not start with the PPK header, so a
// caller can fall back to another private-key format.
var ErrNotPPK = errors.New("ppk: not a PuTTY PPK key file")

// ErrWrongPassphrase reports a MAC that did not verify under the passphrase
// given, which for an encrypted PPK means the passphrase is wrong.
var ErrWrongPassphrase = errors.New("ppk: wrong passphrase")

// maxBlobLines bounds the "Public-Lines"/"Private-Lines" count the way PuTTY's
// own reader does, so a damaged header cannot ask for unbounded reading.
const maxBlobLines = 10240

// maxBlobBytes bounds one decoded blob.
const maxBlobBytes = 64 << 10

// file is one parsed PPK before any decryption.
type file struct {
	version     int
	algorithm   string
	encryption  string
	comment     string
	public      []byte
	private     []byte
	mac         []byte
	argonFlavor string
	argonMemory uint32
	argonPasses uint32
	argonThread uint32
	argonSalt   []byte
	// derived is the Argon2 output of the one passphrase this file is parsed
	// with, kept so the cipher key and the MAC key cost one derivation.
	derived []byte
}

// The Argon2 parameters come from the file, so a damaged or hostile one could
// ask for terabytes or hours. These bound them well above what PuTTYgen writes
// (8 MiB, 13 passes, 1 lane by default).
const (
	maxArgonMemoryKiB = 512 << 10
	maxArgonPasses    = 4096
	maxArgonLanes     = 64
)

// argonMaterial derives the 80 bytes version 3 splits into the cipher key, the
// IV and the MAC key: Argon2's output length is part of its input, so deriving
// parts separately would give different keys. PuTTY's memory parameter is in KiB,
// as argon2.IDKey takes it.
func (f *file) argonMaterial(passphrase []byte) ([]byte, error) {
	if f.derived != nil {
		return f.derived, nil
	}
	if f.argonMemory > maxArgonMemoryKiB || f.argonPasses > maxArgonPasses || f.argonThread > maxArgonLanes {
		return nil, fmt.Errorf("ppk: Argon2 parameters (memory %d KiB, %d passes, %d lanes) are beyond what is supported", f.argonMemory, f.argonPasses, f.argonThread)
	}
	f.derived = argon2.IDKey(passphrase, f.argonSalt, f.argonPasses, f.argonMemory, uint8(f.argonThread), 32+16+32)
	return f.derived, nil
}

// LooksLikePPK reports whether data starts like a PPK file.
func LooksLikePPK(data []byte) bool {
	return bytes.HasPrefix(data, []byte(Header))
}

// Parse decodes one PPK key. passphrase is used when the key is encrypted;
// an encrypted key without a passphrase fails with *ssh.PassphraseMissingError
// (whose PublicKey is filled in, so callers can show the fingerprint first).
func Parse(data []byte, passphrase []byte) (ssh.Signer, error) {
	parsed, err := parseFile(data)
	if err != nil {
		return nil, err
	}
	if parsed.encrypted() && passphrase == nil {
		return nil, &ssh.PassphraseMissingError{PublicKey: parsed.publicKey()}
	}
	private, err := parsed.decrypt(passphrase)
	if err != nil {
		return nil, err
	}
	if err := parsed.verifyMAC(passphrase, private); err != nil {
		return nil, err
	}
	return buildSigner(parsed, private)
}

// ParsePublic reads only the public part of a PPK file, which is never
// encrypted, for display before a passphrase is asked for.
func ParsePublic(data []byte) (ssh.PublicKey, string, bool, error) {
	parsed, err := parseFile(data)
	if err != nil {
		return nil, "", false, err
	}
	return parsed.publicKey(), parsed.comment, parsed.encrypted(), nil
}

func (f *file) encrypted() bool { return f.encryption == "aes256-cbc" }

func (f *file) publicKey() ssh.PublicKey {
	key, err := ssh.ParsePublicKey(f.public)
	if err != nil {
		return nil
	}
	return key
}

// parseFile reads the headers, blobs and MAC of one PPK file.
func parseFile(data []byte) (*file, error) {
	lines, err := splitLines(data)
	if err != nil {
		return nil, err
	}
	f := &file{}
	first, rest := lines[0], lines[1:]
	name, value, ok := header(first)
	if !ok || name != Header+"2" && name != Header+"3" {
		if name == Header+"1" || strings.HasPrefix(name, Header) {
			return nil, fmt.Errorf("ppk: PPK format version of %q is not supported (2 and 3 are)", value)
		}
		return nil, ErrNotPPK
	}
	f.version = int(name[len(Header)] - '0')
	f.algorithm = value
	var publicDone bool
	for len(rest) > 0 {
		line, remainder := rest[0], rest[1:]
		name, value, ok := header(line)
		if !ok {
			return nil, fmt.Errorf("ppk: expected a header line, found %q", truncateForError(line))
		}
		switch {
		case name == "Encryption" && f.encryption == "":
			f.encryption = value
		case name == "Comment" && f.comment == "":
			f.comment = value
		case name == "Public-Lines" && !publicDone:
			f.public, rest, err = readBlob(remainder, value)
			if err != nil {
				return nil, err
			}
			publicDone = true
			continue
		case name == "Key-Derivation" && f.argonFlavor == "":
			f.argonFlavor = value
		case name == "Argon2-Memory" && f.argonMemory == 0:
			if f.argonMemory, err = parseUint32(value, "Argon2-Memory"); err != nil {
				return nil, err
			}
		case name == "Argon2-Passes" && f.argonPasses == 0:
			if f.argonPasses, err = parseUint32(value, "Argon2-Passes"); err != nil {
				return nil, err
			}
		case name == "Argon2-Parallelism" && f.argonThread == 0:
			if f.argonThread, err = parseUint32(value, "Argon2-Parallelism"); err != nil {
				return nil, err
			}
		case name == "Argon2-Salt" && f.argonSalt == nil:
			if f.argonSalt, err = parseHex(value); err != nil {
				return nil, err
			}
		case name == "Private-Lines" && f.private == nil:
			f.private, rest, err = readBlob(remainder, value)
			if err != nil {
				return nil, err
			}
			continue
		case name == "Private-MAC" && f.mac == nil:
			if f.mac, err = parseHex(value); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("ppk: unexpected or duplicate header %q", name)
		}
		rest = remainder
	}
	if !publicDone || f.private == nil || f.mac == nil || f.encryption == "" {
		return nil, errors.New("ppk: file ends before the private blob or MAC")
	}
	if f.encryption != "none" && !f.encrypted() {
		return nil, fmt.Errorf("ppk: unknown encryption %q", f.encryption)
	}
	if f.version == 3 && f.encrypted() {
		if f.argonFlavor == "" || f.argonSalt == nil {
			return nil, errors.New("ppk: encrypted version 3 key without Argon2 parameters")
		}
	}
	return f, nil
}

// splitLines splits on LF, tolerating CR and CR+LF line endings like PuTTY.
func splitLines(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, errors.New("ppk: empty key file")
	}
	text := string(data)
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 4*maxBlobLines {
		return nil, errors.New("ppk: too many lines")
	}
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines, nil
}

func header(line string) (name, value string, ok bool) {
	index := strings.Index(line, ": ")
	if index <= 0 {
		return "", "", false
	}
	return line[:index], line[index+2:], true
}

// readBlob reads count base64 lines from lines and returns the decoded blob
// and the unread remainder.
func readBlob(lines []string, countText string) (blob []byte, rest []string, err error) {
	count, err := strconv.Atoi(countText)
	if err != nil || count < 0 || count > maxBlobLines {
		return nil, nil, fmt.Errorf("ppk: bad blob line count %q", truncateForError(countText))
	}
	if count > len(lines) {
		return nil, nil, errors.New("ppk: file ends inside a blob")
	}
	var joined bytes.Buffer
	for _, line := range lines[:count] {
		if len(line) > maxBlobBytes {
			return nil, nil, errors.New("ppk: blob line too long")
		}
		joined.WriteString(line)
		if joined.Len() > base64.StdEncoding.EncodedLen(maxBlobBytes) {
			return nil, nil, errors.New("ppk: blob too large")
		}
	}
	blob, err = base64.StdEncoding.DecodeString(joined.String())
	if err != nil || len(blob) > maxBlobBytes {
		return nil, nil, errors.New("ppk: blob is not valid base64")
	}
	return blob, lines[count:], nil
}

func parseUint32(text, name string) (uint32, error) {
	value, err := strconv.ParseUint(text, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("ppk: bad %s value %q", name, truncateForError(text))
	}
	return uint32(value), nil
}

func parseHex(text string) ([]byte, error) {
	if len(text)%2 != 0 || len(text) > 2*maxBlobBytes {
		return nil, errors.New("ppk: bad hex value")
	}
	out := make([]byte, len(text)/2)
	for i := range out {
		nybble, err := fromHexNybble(text[2*i])
		if err != nil {
			return nil, err
		}
		low, err := fromHexNybble(text[2*i+1])
		if err != nil {
			return nil, err
		}
		out[i] = nybble<<4 | low
	}
	return out, nil
}

func fromHexNybble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, errors.New("ppk: bad hex digit")
}

func truncateForError(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

// decrypt returns the private blob in the clear. Encrypted blobs are padded to
// the AES block size with random bytes; the padding stays, because the MAC
// covers it and the field layout inside the blob is self-delimiting.
func (f *file) decrypt(passphrase []byte) ([]byte, error) {
	if !f.encrypted() {
		return f.private, nil
	}
	if len(f.private)%aes.BlockSize != 0 {
		return nil, errors.New("ppk: encrypted private blob is not block aligned")
	}
	key, iv, err := f.cipherKeys(passphrase)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(f.private))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, f.private)
	return out, nil
}

// cipherKeys derives the AES key and IV. Version 3 uses one Argon2id pass over
// the passphrase; version 2 chains SHA-1 the way PuTTY always has.
func (f *file) cipherKeys(passphrase []byte) (key, iv []byte, err error) {
	if f.version >= 3 {
		switch f.argonFlavor {
		case "Argon2id":
		case "Argon2d", "Argon2i":
			return nil, nil, fmt.Errorf("ppk: key derivation %s is not supported; re-export the key from PuTTYgen (its default, Argon2id, works)", f.argonFlavor)
		default:
			return nil, nil, fmt.Errorf("ppk: unknown key derivation %q", f.argonFlavor)
		}
		if f.argonMemory == 0 || f.argonPasses == 0 || f.argonThread == 0 || len(f.argonSalt) == 0 {
			return nil, nil, errors.New("ppk: incomplete Argon2 parameters")
		}
		material, err := f.argonMaterial(passphrase)
		if err != nil {
			return nil, nil, err
		}
		return material[:32], material[32:48], nil
	}
	// Version 2: SHA-1 of a counter and the passphrase, concatenated, plus a
	// zero IV.
	var material []byte
	for counter := uint32(0); len(material) < 32; counter++ {
		hash := sha1.New()
		var counterBytes [4]byte
		binary.BigEndian.PutUint32(counterBytes[:], counter)
		hash.Write(counterBytes[:])
		hash.Write(passphrase)
		material = hash.Sum(material)
	}
	return material[:32], make([]byte, aes.BlockSize), nil
}

// verifyMAC checks the Private-MAC line. The MAC covers the algorithm,
// encryption and comment header values and both blobs, so a public/private
// mismatch or an edited comment cannot pass.
func (f *file) verifyMAC(passphrase, private []byte) error {
	var key []byte
	switch {
	case f.version >= 3 && f.encrypted():
		material, err := f.argonMaterial(passphrase)
		if err != nil {
			return err
		}
		key = material[48:80]
	case f.version >= 3:
		key = nil // unencrypted version 3 MACs with an empty key
	default:
		// Version 2 always derives a MAC key, even without encryption.
		hash := sha1.New()
		hash.Write([]byte("putty-private-key-file-mac-key"))
		hash.Write(passphrase)
		key = hash.Sum(nil)
	}
	var input []byte
	input = appendSSHString(input, []byte(f.algorithm))
	input = appendSSHString(input, []byte(f.encryption))
	input = appendSSHString(input, []byte(f.comment))
	input = appendSSHString(input, f.public)
	input = appendSSHString(input, private)
	expected := f.mac
	if f.version >= 3 {
		mac := hmac.New(sha256.New, key)
		mac.Write(input)
		if len(expected) == sha256.Size && hmac.Equal(mac.Sum(nil), expected) {
			return nil
		}
	} else {
		mac := hmac.New(sha1.New, key)
		mac.Write(input)
		if len(expected) == sha1.Size && hmac.Equal(mac.Sum(nil), expected) {
			return nil
		}
	}
	if f.encrypted() {
		return ErrWrongPassphrase
	}
	return errors.New("ppk: MAC failed")
}

func appendSSHString(dst []byte, s []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(s)))
	dst = append(dst, length[:]...)
	return append(dst, s...)
}

// wireReader walks an SSH wire blob with bounds checks.
type wireReader struct {
	data []byte
	err  error
}

func (r *wireReader) take(n int) []byte {
	if r.err != nil || n < 0 || len(r.data) < n {
		r.err = errors.New("ppk: blob truncated")
		return nil
	}
	out := r.data[:n]
	r.data = r.data[n:]
	return out
}

func (r *wireReader) takeString() []byte {
	var length [4]byte
	if !r.takeInto(length[:]) {
		return nil
	}
	n := int(binary.BigEndian.Uint32(length[:]))
	if n > len(r.data) {
		r.err = errors.New("ppk: string length exceeds the blob")
		return nil
	}
	return r.take(n)
}

func (r *wireReader) takeInto(dst []byte) bool {
	taken := r.take(len(dst))
	if taken == nil {
		return false
	}
	copy(dst, taken)
	return true
}

// takeMPInt reads a big-endian SSH mpint.
func (r *wireReader) takeMPInt() *big.Int {
	raw := r.takeString()
	if raw == nil || r.err != nil {
		return nil
	}
	if len(raw) > 0 && raw[0]&0x80 != 0 {
		r.err = errors.New("ppk: negative mpint")
		return nil
	}
	return new(big.Int).SetBytes(raw)
}

// buildSigner turns the decrypted private blob into a signer, verifying that
// the private fields really belong to the public key on the same file.
func buildSigner(f *file, private []byte) (ssh.Signer, error) {
	public := f.publicKey()
	if public == nil {
		return nil, errors.New("ppk: public blob is not a valid SSH public key")
	}
	switch f.algorithm {
	case "ssh-rsa":
		return rsaSigner(f.public, private)
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return ecdsaSigner(f.algorithm, f.public, private)
	case "ssh-ed25519":
		return ed25519Signer(f.public, private)
	}
	return nil, fmt.Errorf("ppk: algorithm %q is not supported", f.algorithm)
}

func rsaSigner(publicBlob, private []byte) (ssh.Signer, error) {
	pub := wireReader{data: publicBlob}
	if string(pub.takeString()) != "ssh-rsa" || pub.err != nil {
		return nil, errors.New("ppk: malformed ssh-rsa public blob")
	}
	exponent := pub.takeMPInt()
	modulus := pub.takeMPInt()
	if pub.err != nil || exponent == nil || modulus == nil {
		return nil, errors.New("ppk: malformed ssh-rsa public blob")
	}
	priv := wireReader{data: private}
	d := priv.takeMPInt()
	p := priv.takeMPInt()
	q := priv.takeMPInt()
	iqmp := priv.takeMPInt()
	if priv.err != nil || d == nil || p == nil || q == nil || iqmp == nil {
		return nil, errors.New("ppk: malformed ssh-rsa private blob")
	}
	if exponent.BitLen() > 31 || !exponent.IsInt64() {
		return nil, errors.New("ppk: RSA exponent out of range")
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: modulus, E: int(exponent.Int64())},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("ppk: inconsistent RSA key: %w", err)
	}
	key.Precompute()
	return ssh.NewSignerFromKey(key)
}

var ecCurve = map[string]elliptic.Curve{
	"ecdsa-sha2-nistp256": elliptic.P256(),
	"ecdsa-sha2-nistp384": elliptic.P384(),
	"ecdsa-sha2-nistp521": elliptic.P521(),
}

func ecdsaSigner(algorithm string, publicBlob, private []byte) (ssh.Signer, error) {
	pub := wireReader{data: publicBlob}
	if string(pub.takeString()) != algorithm {
		return nil, errors.New("ppk: public blob does not match the algorithm header")
	}
	if curveName := string(pub.takeString()); curveName != algorithm[len("ecdsa-sha2-"):] || pub.err != nil {
		return nil, errors.New("ppk: malformed ecdsa public blob")
	}
	point := pub.takeString()
	if pub.err != nil || len(point) < 1 || point[0] != 4 {
		return nil, errors.New("ppk: ecdsa public point must be uncompressed")
	}
	curve := ecCurve[algorithm]
	byteLen := (curve.Params().BitSize + 7) / 8
	if len(point) != 1+2*byteLen {
		return nil, errors.New("ppk: ecdsa public point has the wrong size")
	}
	x := new(big.Int).SetBytes(point[1 : 1+byteLen])
	y := new(big.Int).SetBytes(point[1+byteLen:])
	priv := wireReader{data: private}
	d := priv.takeMPInt()
	if priv.err != nil || d == nil || d.Sign() <= 0 {
		return nil, errors.New("ppk: malformed ecdsa private blob")
	}
	// The private scalar must produce the public point on the file.
	gx, gy := curve.ScalarBaseMult(d.Bytes())
	if gx.Cmp(x) != 0 || gy.Cmp(y) != 0 {
		return nil, errors.New("ppk: ecdsa private key does not match the public key")
	}
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}
	return ssh.NewSignerFromKey(key)
}

func ed25519Signer(publicBlob, private []byte) (ssh.Signer, error) {
	pub := wireReader{data: publicBlob}
	if string(pub.takeString()) != "ssh-ed25519" {
		return nil, errors.New("ppk: malformed ssh-ed25519 public blob")
	}
	encoded := pub.takeString()
	if pub.err != nil || len(encoded) != ed25519.PublicKeySize {
		return nil, errors.New("ppk: ssh-ed25519 public key has the wrong size")
	}
	// The private blob is the seed as a little-endian integer, which puttygen
	// writes in exactly 32 bytes; shorter forms mean trimmed zero bytes.
	if len(private) == 0 || len(private) > ed25519.SeedSize {
		return nil, errors.New("ppk: ssh-ed25519 private blob has the wrong size")
	}
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		if i < len(private) {
			seed[i] = private[i]
		}
	}
	key := ed25519.NewKeyFromSeed(seed[:])
	if !bytes.Equal(key.Public().(ed25519.PublicKey), encoded) {
		return nil, errors.New("ppk: ssh-ed25519 private key does not match the public key")
	}
	return ssh.NewSignerFromKey(key)
}
