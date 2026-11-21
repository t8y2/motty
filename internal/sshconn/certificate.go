package sshconn

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// maxCertificateSize bounds a certificate file read from disk.
const maxCertificateSize = 1 << 20

// CertificateFor validates data as an OpenSSH user certificate for public and
// returns it. public may be nil when the key's public half cannot be read
// before its passphrase; the key match is then checked when the certificate is
// used, by ssh.NewCertSigner.
func CertificateFor(data []byte, public ssh.PublicKey) (*ssh.Certificate, error) {
	if len(data) > maxCertificateSize {
		return nil, errors.New("the certificate exceeds 1 MiB")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	certificate, ok := key.(*ssh.Certificate)
	if !ok {
		return nil, errors.New("the file is a public key, not a certificate")
	}
	if certificate.CertType != ssh.UserCert {
		return nil, errors.New("the certificate is not a user certificate")
	}
	if public != nil && !bytes.Equal(certificate.Key.Marshal(), public.Marshal()) {
		return nil, errors.New("the certificate belongs to a different key")
	}
	return certificate, nil
}

// ReadCertificate reads and validates an OpenSSH user certificate file for
// public. It is how the application checks a certificate before keeping it.
func ReadCertificate(path string, public ssh.PublicKey) (*ssh.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return CertificateFor(data, public)
}

// CertificateLine is the authorized_keys text of a user certificate: the form
// OpenSSH pairs with a key in a -cert.pub file.
func CertificateLine(certificate *ssh.Certificate) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(certificate)))
}

// certificateSigner wraps signer in the OpenSSH user certificate that
// accompanies its key file, the way OpenSSH pairs id_ed25519 with
// id_ed25519-cert.pub: same path, "-cert.pub" appended. Without a companion
// file the signer is returned unchanged. A companion that is not a user
// certificate for exactly this key is an error, so a mismatch is reported
// instead of silently authenticating with the bare key.
func certificateSigner(path string, signer ssh.Signer) (ssh.Signer, error) {
	companion := path + "-cert.pub"
	data, err := os.ReadFile(companion)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return signer, nil
		}
		return nil, fmt.Errorf("read certificate %s: %w", companion, err)
	}
	certificate, err := CertificateFor(data, signer.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("use certificate %s: %w", companion, err)
	}
	return wrapCertificate(certificate, signer)
}

// wrapCertificate wraps signer in certificate. It reports a certificate made for
// another key rather than authenticating with the bare key.
func wrapCertificate(certificate *ssh.Certificate, signer ssh.Signer) (ssh.Signer, error) {
	certified, err := ssh.NewCertSigner(certificate, signer)
	if err != nil {
		return nil, fmt.Errorf("use certificate %s: %w", ssh.FingerprintSHA256(certificate.Key), err)
	}
	return certified, nil
}

// CertificateBeside reports whether an identity file has the companion
// certificate OpenSSH pairs with it.
func CertificateBeside(path string) bool {
	info, err := os.Stat(path + "-cert.pub")
	return err == nil && info.Mode().IsRegular()
}
