package keyidentity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/ssh"
)

// The "ENCRYPTED PRIVATE KEY" PEM form is an RFC 5208 PKCS#8 EncryptedPrivateKeyInfo
// protected by RFC 8018 PBES2. OpenSSL 3 writes it by default for `openssl genrsa
// -aes256` and `openssl pkcs8 -topk8`, and ssh-keygen writes it for `-m PKCS8`.
// golang.org/x/crypto/ssh does not read it, so the file would otherwise be
// dropped without a passphrase prompt.

const pkcs8EncryptedType = "ENCRYPTED PRIVATE KEY"

var (
	oidPBES2      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidHMACSHA1   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACSHA224 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 8}
	oidHMACSHA256 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHMACSHA384 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	oidHMACSHA512 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}
	oidAES128CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC  = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3CBC = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
)

// maxPBKDF2Iters bounds a file that would otherwise stall the connection.
const maxPBKDF2Iters = 10_000_000

type algorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type encryptedPrivateKeyInfo struct {
	Algorithm     algorithmIdentifier
	EncryptedData []byte
}

type pbes2Params struct {
	KeyDerivation algorithmIdentifier
	Encryption    algorithmIdentifier
}

type pbkdf2Params struct {
	Salt       []byte
	Iterations int
	KeyLength  int                 `asn1:"optional"`
	PRF        algorithmIdentifier `asn1:"optional"`
}

// pkcs8Scheme is what an encrypted PKCS#8 block asks for, validated but not yet
// applied to a passphrase.
type pkcs8Scheme struct {
	salt       []byte
	iterations int
	keyLength  int
	prf        func() hash.Hash
	newBlock   func(key []byte) (cipher.Block, error)
	blockKey   int
	iv         []byte
	ciphertext []byte
}

// encryptedPKCS8 returns the block of an "ENCRYPTED PRIVATE KEY" file, or nil
// for any other content.
func encryptedPKCS8(data []byte) *pem.Block {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != pkcs8EncryptedType {
		return nil
	}
	return block
}

// parsePKCS8Scheme reads the encryption parameters of block. It fails for a
// malformed block and for schemes this reader does not implement, which is how
// Inspect refuses them at import instead of at the first connection.
func parsePKCS8Scheme(der []byte) (*pkcs8Scheme, error) {
	var info encryptedPrivateKeyInfo
	if rest, err := asn1.Unmarshal(der, &info); err != nil || len(rest) != 0 {
		return nil, errors.New("encrypted PKCS#8 key is malformed")
	}
	if !info.Algorithm.Algorithm.Equal(oidPBES2) {
		return nil, fmt.Errorf("encrypted PKCS#8 key uses the legacy scheme %v; re-export it with PBES2 (AES) or as an OpenSSH key", info.Algorithm.Algorithm)
	}
	var params pbes2Params
	if rest, err := asn1.Unmarshal(info.Algorithm.Parameters.FullBytes, &params); err != nil || len(rest) != 0 {
		return nil, errors.New("encrypted PKCS#8 key has malformed PBES2 parameters")
	}
	if !params.KeyDerivation.Algorithm.Equal(oidPBKDF2) {
		return nil, fmt.Errorf("encrypted PKCS#8 key derives its key with %v, which is not supported; only PBKDF2 is", params.KeyDerivation.Algorithm)
	}
	scheme := &pkcs8Scheme{ciphertext: info.EncryptedData}
	var kdf pbkdf2Params
	if rest, err := asn1.Unmarshal(params.KeyDerivation.Parameters.FullBytes, &kdf); err != nil || len(rest) != 0 {
		return nil, errors.New("encrypted PKCS#8 key has malformed PBKDF2 parameters")
	}
	if kdf.Iterations < 1 || kdf.Iterations > maxPBKDF2Iters {
		return nil, errors.New("encrypted PKCS#8 key has an unusable PBKDF2 iteration count")
	}
	scheme.salt, scheme.iterations, scheme.keyLength = kdf.Salt, kdf.Iterations, kdf.KeyLength

	switch {
	case len(kdf.PRF.Algorithm) == 0 || kdf.PRF.Algorithm.Equal(oidHMACSHA1):
		scheme.prf = sha1.New
	case kdf.PRF.Algorithm.Equal(oidHMACSHA224):
		scheme.prf = sha256.New224
	case kdf.PRF.Algorithm.Equal(oidHMACSHA256):
		scheme.prf = sha256.New
	case kdf.PRF.Algorithm.Equal(oidHMACSHA384):
		scheme.prf = sha512.New384
	case kdf.PRF.Algorithm.Equal(oidHMACSHA512):
		scheme.prf = sha512.New
	default:
		return nil, fmt.Errorf("encrypted PKCS#8 key uses the unsupported PBKDF2 hash %v", kdf.PRF.Algorithm)
	}

	cipherOID := params.Encryption.Algorithm
	switch {
	case cipherOID.Equal(oidAES128CBC):
		scheme.blockKey, scheme.newBlock = 16, aes.NewCipher
	case cipherOID.Equal(oidAES192CBC):
		scheme.blockKey, scheme.newBlock = 24, aes.NewCipher
	case cipherOID.Equal(oidAES256CBC):
		scheme.blockKey, scheme.newBlock = 32, aes.NewCipher
	case cipherOID.Equal(oidDESEDE3CBC):
		scheme.blockKey, scheme.newBlock = 24, des.NewTripleDESCipher
	default:
		return nil, fmt.Errorf("encrypted PKCS#8 key uses the unsupported cipher %v", cipherOID)
	}
	if rest, err := asn1.Unmarshal(params.Encryption.Parameters.FullBytes, &scheme.iv); err != nil || len(rest) != 0 {
		return nil, errors.New("encrypted PKCS#8 key has a malformed initialization vector")
	}
	if scheme.keyLength != 0 && scheme.keyLength != scheme.blockKey {
		return nil, errors.New("encrypted PKCS#8 key has a key length that does not fit its cipher")
	}
	return scheme, nil
}

// decrypt returns the PKCS#8 PrivateKeyInfo, or x509.IncorrectPasswordError
// when the passphrase does not open it.
func (scheme *pkcs8Scheme) decrypt(passphrase []byte) ([]byte, error) {
	key := pbkdf2.Key(passphrase, scheme.salt, scheme.iterations, scheme.blockKey, scheme.prf)
	defer clear(key)
	block, err := scheme.newBlock(key)
	if err != nil {
		return nil, err
	}
	size := block.BlockSize()
	if len(scheme.iv) != size || len(scheme.ciphertext) == 0 || len(scheme.ciphertext)%size != 0 {
		return nil, errors.New("encrypted PKCS#8 key has data that does not fit its cipher")
	}
	plain := make([]byte, len(scheme.ciphertext))
	cipher.NewCBCDecrypter(block, scheme.iv).CryptBlocks(plain, scheme.ciphertext)
	trimmed, ok := unpad(plain, size)
	if !ok {
		clear(plain)
		return nil, x509.IncorrectPasswordError
	}
	return trimmed, nil
}

// unpad removes PKCS#7 padding. A wrong passphrase yields garbage that
// usually fails here; the rare garbage that passes fails the key parse.
func unpad(data []byte, size int) ([]byte, bool) {
	pad := int(data[len(data)-1])
	if pad == 0 || pad > size || pad > len(data) {
		return nil, false
	}
	var bad byte
	for _, b := range data[len(data)-pad:] {
		bad |= b ^ byte(pad)
	}
	if subtle.ConstantTimeByteEq(bad, 0) != 1 {
		return nil, false
	}
	return data[:len(data)-pad], true
}

// loadEncryptedPKCS8 opens one encrypted PKCS#8 block with passphrase. A nil
// passphrase reports ssh.PassphraseMissingError, as the other formats do, so
// callers prompt for it.
func loadEncryptedPKCS8(block *pem.Block, passphrase []byte) (ssh.Signer, error) {
	scheme, err := parsePKCS8Scheme(block.Bytes)
	if err != nil {
		return nil, err
	}
	if passphrase == nil {
		return nil, &ssh.PassphraseMissingError{}
	}
	plain, err := scheme.decrypt(passphrase)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	parsed, err := x509.ParsePKCS8PrivateKey(plain)
	if err != nil {
		return nil, x509.IncorrectPasswordError
	}
	return ssh.NewSignerFromKey(parsed)
}
