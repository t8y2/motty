// Package updatesign checks that a release package was signed by the key Motty
// publishes its updates with. The release feed carries one signature per
// package, made over the package's bytes with a signed comment naming the
// version and the file; the comment is what stops a changed feed from passing
// an older release off as a newer one.
package updatesign

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"aead.dev/minisign"
)

// releaseKeyText is the public half of the release signing key. The private
// half lives in the release workflow (MOTTY_UPDATE_SIGNING_KEY) and in the
// owner's offline backup, never in this repository.
const releaseKeyText = `untrusted comment: minisign public key: C4B1B4A98B79DE2D
RWQt3nmLqbSxxNUcSrHSz1RYgkrnE2U6eioEHTy355AsjTQhIypdhNkj`

// ErrUnsigned means the release names no signature for the package.
var ErrUnsigned = errors.New("updatesign: the release has no signature")

// ErrSignature means the signature is not one this key made for this package,
// version and file.
var ErrSignature = errors.New("updatesign: the package is not signed by Motty")

// unsignedComment is the unauthenticated line above the signature. It is
// cosmetic: nothing reads it, and it is only here so a person opening the file
// sees what made it.
const unsignedComment = "signature from the Motty release key"

// ReleaseKey returns the key release signatures are checked against.
func ReleaseKey() minisign.PublicKey {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(releaseKeyText)); err != nil {
		panic("updatesign: the embedded release key does not parse: " + err.Error())
	}
	return key
}

// SignedComment is the comment a signature carries. It is signed together with
// the package, so the version and file in it cannot be changed without breaking
// the signature.
func SignedComment(version, file string, now time.Time) string {
	return "timestamp:" + strconv.FormatInt(now.Unix(), 10) + "\tfile:" + file + "\tversion:" + version
}

// Sign signs everything r reads, without holding the package in memory, and
// returns the base64 armoured signature the feed carries.
func Sign(privateKey minisign.PrivateKey, r io.Reader, version, file string, now time.Time) (string, error) {
	if version == "" || file == "" {
		return "", errors.New("updatesign: a signature needs a version and a file name")
	}
	reader := minisign.NewReader(r)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return "", fmt.Errorf("updatesign: reading the package: %w", err)
	}
	armour := reader.SignWithComments(privateKey, SignedComment(version, file, now), unsignedComment)
	return base64.StdEncoding.EncodeToString(armour), nil
}

// armour decodes a feed signature. It says nothing about the bytes; Verify is
// what checks those.
func armour(signature string) ([]byte, minisign.Signature, error) {
	var parsed minisign.Signature
	if signature == "" {
		return nil, parsed, ErrUnsigned
	}
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(decoded) == 0 || len(decoded) > 4096 {
		return nil, parsed, ErrSignature
	}
	if err := parsed.UnmarshalText(decoded); err != nil {
		return nil, parsed, ErrSignature
	}
	return decoded, parsed, nil
}

// Usable reports whether a feed signature could have been made by the release
// key: it decodes, parses as a minisign signature, and names this key. It does
// not verify any bytes, so a download can be refused before it starts.
func Usable(signature string) error { return UsableWith(ReleaseKey(), signature) }

// UsableWith is Usable against a given key, which a test can make itself.
func UsableWith(publicKey minisign.PublicKey, signature string) error {
	_, parsed, err := armour(signature)
	if err != nil {
		return err
	}
	if parsed.KeyID != publicKey.ID() {
		return ErrSignature
	}
	return nil
}

// Verify reads the package from r and holds it to signature, which must have
// been made for this version and file. The package is streamed, so a large one
// costs no more memory than a small one.
func Verify(publicKey minisign.PublicKey, r io.Reader, signature, version, file string) error {
	decoded, parsed, err := armour(signature)
	if err != nil {
		return err
	}
	reader := minisign.NewReader(r)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("updatesign: reading the package: %w", err)
	}
	if !reader.Verify(publicKey, decoded) {
		return ErrSignature
	}
	// Only now, with the signature checked, is the comment usable: minisign's
	// global signature covers it, and Verify is what checks that signature.
	signedVersion, signedFile, err := commentFields(parsed.TrustedComment)
	if err != nil {
		return ErrSignature
	}
	if signedVersion != version {
		return fmt.Errorf("%w: it was signed for version %q", ErrSignature, signedVersion)
	}
	if signedFile != file {
		return fmt.Errorf("%w: it was signed for %q", ErrSignature, signedFile)
	}
	return nil
}

// VerifyFile is Verify for a package already on disk.
func VerifyFile(publicKey minisign.PublicKey, path, signature, version, file string) error {
	packageFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer packageFile.Close()
	return Verify(publicKey, packageFile, signature, version, file)
}

func commentFields(comment string) (version, file string, err error) {
	for _, field := range strings.Split(comment, "\t") {
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			continue
		}
		switch key {
		case "version":
			version = value
		case "file":
			file = value
		}
	}
	if version == "" || file == "" {
		return "", "", errors.New("the signature comment does not name a version and a file")
	}
	return version, file, nil
}
