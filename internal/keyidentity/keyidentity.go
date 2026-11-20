// Package keyidentity inspects and loads SSH private-key files without storing
// key material or passphrases beyond one explicit operation.
package keyidentity

import (
	"bytes"
	"encoding/pem"
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

// ErrSecurityKeyNeedsAgent reports a hardware security key (an OpenSSH sk-*
// identity). The private half is a credential on the device, so the key can
// only sign through an ssh-agent that can reach it; parsing the file cannot
// produce a signer. The application offers such keys through the agent.
var ErrSecurityKeyNeedsAgent = errors.New("SSH security keys are used through ssh-agent")

// SecurityKeyError names the security-key identity that Parse cannot load.
type SecurityKeyError struct {
	PublicKey ssh.PublicKey
}

func (err *SecurityKeyError) Error() string {
	return fmt.Sprintf("security key %s %s must be used through ssh-agent",
		err.PublicKey.Type(), ssh.FingerprintSHA256(err.PublicKey))
}

func (err *SecurityKeyError) Unwrap() error { return ErrSecurityKeyNeedsAgent }

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
	// SecurityKey marks a FIDO2/U2F key (OpenSSH sk-*). It can authenticate
	// only through an ssh-agent that reaches the device.
	SecurityKey bool
	Size        int64
	Modified    time.Time
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

func PublicKey(path string) (ssh.PublicKey, error) {
	for _, candidate := range []string{path + ".pub", path} {
		absolute, data, _, err := read(candidate)
		if err != nil {
			continue
		}
		public, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err == nil {
			return public, nil
		}
		if candidate == path {
			info, err := InspectData(absolute, data)
			if err == nil && info.PublicKey != "" {
				public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(info.PublicKey))
				return public, err
			}
		}
	}
	return nil, fmt.Errorf("no readable public identity for %s", path)
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
	if public, encrypted, err := openSSHPublicKey(data); err == nil && public != nil && IsSecurityKey(public) {
		// A security key's public half is in the clear even when the private
		// half is passphrase-protected, so the key can be named before the
		// agent asks for anything.
		info.describe(public)
		info.Encrypted = encrypted
		info.SecurityKey = true
		return info, nil
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

// openSSHPublicKey reads the public half of an OpenSSH private-key container
// without opening the private half. Every OpenSSH file keeps it in the clear;
// for a security key it is the only part that can be named before the device
// signs.
func openSSHPublicKey(data []byte) (ssh.PublicKey, bool, error) {
	const magic = "openssh-key-v1\x00"
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "OPENSSH PRIVATE KEY" || !bytes.HasPrefix(block.Bytes, []byte(magic)) {
		return nil, false, nil
	}
	var container struct {
		CipherName  string
		KDFName     string
		KDFOptions  []byte
		NumKeys     uint32
		PublicKey   []byte
		PrivateKeys []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(block.Bytes[len(magic):], &container); err != nil {
		return nil, false, fmt.Errorf("read OpenSSH key container: %w", err)
	}
	if container.NumKeys != 1 || len(container.PublicKey) == 0 {
		return nil, false, errors.New("unsupported OpenSSH key container")
	}
	public, err := ssh.ParsePublicKey(container.PublicKey)
	if err != nil {
		return nil, false, fmt.Errorf("read OpenSSH public key: %w", err)
	}
	return public, container.CipherName != "none", nil
}

// IsSecurityKey reports a FIDO2/U2F public key, or a certificate over one.
func IsSecurityKey(public ssh.PublicKey) bool {
	if public == nil {
		return false
	}
	if certificate, ok := public.(*ssh.Certificate); ok {
		public = certificate.Key
	}
	switch public.Type() {
	case ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
		return true
	}
	return false
}

// Load parses one private key file, OpenSSH/PEM or PuTTY PPK format.
// passphrase may be nil for an unencrypted key. Callers own the passphrase
// bytes and can clear them immediately after return. A security key returns a
// *SecurityKeyError: only an ssh-agent can sign with it.
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
	if public, _, err := openSSHPublicKey(data); err == nil && public != nil && IsSecurityKey(public) {
		return nil, &SecurityKeyError{PublicKey: public}
	}
	if passphrase == nil {
		return ssh.ParsePrivateKey(data)
	}
	return ssh.ParsePrivateKeyWithPassphrase(data, passphrase)
}

// ReadFile returns the bytes of one private key file, as they are on disk. It is
// for a caller that keeps the key itself, not the path it came from; InspectData
// says what the bytes are.
func ReadFile(path string) ([]byte, error) {
	_, data, _, err := read(path)
	return data, err
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
