// Package keyidentity inspects and loads SSH private-key files without storing
// key material or passphrases beyond one explicit operation.
package keyidentity

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/ppk"
)

const maxPrivateKeySize = 1 << 20

// ErrTooLarge reports a file that exceeds the bounded private-key input size.
var ErrTooLarge = errors.New("SSH private key exceeds 1 MiB")

// Info is safe display metadata derived from one private-key file.
type Info struct {
	Path        string
	Name        string
	Algorithm   string
	Fingerprint string
	// PublicKey is the key in authorized_keys form ("ssh-ed25519 AAAA…"), ready
	// to paste on a server. Empty while the key is locked and its public half is
	// not readable without the passphrase.
	PublicKey string
	Encrypted bool
	Size      int64
	Modified  time.Time
}

// Inspect validates path as a supported private key and returns its public
// metadata. Encrypted OpenSSH keys and PPK files expose their public key
// without a passphrase; older encrypted PEM forms and encrypted PKCS#8 files
// may leave Algorithm and Fingerprint empty until authentication.
func Inspect(path string) (Info, error) {
	absolute, data, file, err := read(path)
	if err != nil {
		return Info{}, err
	}
	info, err := InspectData(absolute, data)
	if err != nil {
		return Info{}, err
	}
	info.Size, info.Modified = file.Size(), file.ModTime()
	return info, nil
}

// InspectData is Inspect for a key whose bytes are already in hand, such as one
// kept in the vault. name stands where the path would: it becomes Path and,
// as its last element, Name.
func InspectData(name string, data []byte) (Info, error) {
	if len(data) > maxPrivateKeySize {
		return Info{}, ErrTooLarge
	}
	info := Info{Path: name, Name: filepath.Base(name), Size: int64(len(data))}
	if ppk.LooksLikePPK(data) {
		public, _, encrypted, err := ppk.ParsePublic(data)
		if err != nil {
			return Info{}, fmt.Errorf("inspect SSH private key: %w", err)
		}
		info.describe(public)
		info.Encrypted = encrypted
		return info, nil
	}
	if block := encryptedPKCS8(data); block != nil {
		if _, err := parsePKCS8Scheme(block.Bytes); err != nil {
			return Info{}, fmt.Errorf("inspect SSH private key: %w", err)
		}
		info.Encrypted = true
		return info, nil
	}
	if err := securityKeyCheck(data); err != nil {
		return Info{}, fmt.Errorf("inspect SSH private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		info.describe(signer.PublicKey())
		return info, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return Info{}, fmt.Errorf("inspect SSH private key: %w", err)
	}
	info.Encrypted = true
	if missing.PublicKey != nil {
		info.describe(missing.PublicKey)
	}
	return info, nil
}

func (info *Info) describe(public ssh.PublicKey) {
	info.Algorithm = public.Type()
	info.Fingerprint = ssh.FingerprintSHA256(public)
	info.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
}

// securityKeyCheck refuses FIDO2 hardware-key identities with a message that
// names the limit, instead of a bare parse error. The key type appears in the
// clear body of an unencrypted OpenSSH file; encrypted ones fail later with
// the generic parse error.
func securityKeyCheck(data []byte) error {
	for _, keyType := range [...]string{"sk-ssh-ed25519@openssh.com", "sk-ecdsa-sha2-"} {
		if bytes.Contains(data, []byte(keyType)) {
			return fmt.Errorf("hardware security keys (%s) are not supported; use a key file without the sk- prefix", keyType)
		}
	}
	return nil
}

// Load parses one private key file, OpenSSH/PEM or PuTTY PPK format.
// passphrase may be nil for an unencrypted key. Callers own the passphrase
// bytes and can clear them immediately after return.
func Load(path string, passphrase []byte) (ssh.Signer, error) {
	_, data, _, err := read(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, passphrase)
}

// Parse is Load for a key whose bytes are already in hand.
func Parse(data, passphrase []byte) (ssh.Signer, error) {
	if ppk.LooksLikePPK(data) {
		return ppk.Parse(data, passphrase)
	}
	if block := encryptedPKCS8(data); block != nil {
		return loadEncryptedPKCS8(block, passphrase)
	}
	if err := securityKeyCheck(data); err != nil {
		return nil, err
	}
	if passphrase == nil {
		return ssh.ParsePrivateKey(data)
	}
	return ssh.ParsePrivateKeyWithPassphrase(data, passphrase)
}

func read(path string) (absolute string, data []byte, info os.FileInfo, err error) {
	absolute, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve SSH private key: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return "", nil, nil, fmt.Errorf("open SSH private key: %w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", nil, nil, fmt.Errorf("stat SSH private key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, nil, fmt.Errorf("SSH private key is not a regular file")
	}
	if info.Size() > maxPrivateKeySize {
		return "", nil, nil, ErrTooLarge
	}
	data, err = io.ReadAll(io.LimitReader(file, maxPrivateKeySize+1))
	if err != nil {
		return "", nil, nil, fmt.Errorf("read SSH private key: %w", err)
	}
	if len(data) > maxPrivateKeySize {
		return "", nil, nil, ErrTooLarge
	}
	return absolute, data, info, nil
}
