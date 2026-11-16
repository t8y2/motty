package rfb

import (
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"strings"
	"unicode/utf8"
)

// ARD (security type 30) is Apple Remote Desktop authentication, the account
// login of macOS Screen Sharing and Remote Management. The server sends
// Diffie-Hellman parameters, the client derives an AES-128 key from the shared
// secret, and the account name and password cross the connection encrypted
// with it. The session that follows is not encrypted and the server is not
// authenticated, so this protects the credentials from a passive observer only:
// carry the connection through an SSH tunnel when the network is not trusted.
//
// Apple documents neither the exchange nor its types; this follows how its
// clients and servers behave and the implementations of gtk-vnc and other
// viewers. The client sends its public key after the encrypted credentials, so
// the server can derive the same shared secret.
const (
	// ardMinKeyBytes and ardMaxKeyBytes bound the modulus the server may
	// choose: 512-bit legacy parameters up to 4096-bit. The upper bound keeps a
	// hostile server from forcing an arbitrarily expensive exponentiation.
	ardMinKeyBytes = 64
	ardMaxKeyBytes = 512
	// ardCredentials is the fixed plaintext size: the account name and the
	// password in two NUL-terminated 64-byte fields.
	ardCredentials = 128
	ardFieldBytes  = 64
	// ardPrivateBits is the client's ephemeral exponent. The modulus bounds the
	// exchange's strength; the exponent only has to be unpredictable.
	ardPrivateBits = 256
)

// ardParameters is the Diffie-Hellman material a server sends after the client
// chooses Apple Remote Desktop authentication.
type ardParameters struct {
	generator int
	prime     *big.Int
	public    *big.Int
	keyBytes  int
}

// readARDParameters reads and checks one ARD parameter message. Nothing the
// server sends is trusted: the lengths, the modulus, the generator and the
// public key are all bounded or validated before the client does any work.
func readARDParameters(reader io.Reader) (ardParameters, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return ardParameters{}, fmt.Errorf("rfb: reading ARD parameters: %w", err)
	}
	generator := int(binary.BigEndian.Uint16(header[0:]))
	keyBytes := int(binary.BigEndian.Uint16(header[2:]))
	if keyBytes < ardMinKeyBytes || keyBytes > ardMaxKeyBytes {
		return ardParameters{}, fmt.Errorf("%w: ARD key length %d is outside %d..%d bytes", ErrProtocol, keyBytes, ardMinKeyBytes, ardMaxKeyBytes)
	}
	wire := make([]byte, 2*keyBytes)
	if _, err := io.ReadFull(reader, wire); err != nil {
		return ardParameters{}, fmt.Errorf("rfb: reading ARD key material: %w", err)
	}
	prime := new(big.Int).SetBytes(wire[:keyBytes])
	public := new(big.Int).SetBytes(wire[keyBytes:])
	if generator < 2 || prime.Bit(0) == 0 || prime.Cmp(big.NewInt(3)) <= 0 ||
		public.Cmp(big.NewInt(1)) <= 0 || public.Cmp(prime) >= 0 {
		return ardParameters{}, fmt.Errorf("%w: invalid ARD Diffie-Hellman parameters", ErrProtocol)
	}
	return ardParameters{generator: generator, prime: prime, public: public, keyBytes: keyBytes}, nil
}

// authenticateARD answers the Apple Remote Desktop challenge. The server sends
// Diffie-Hellman parameters, then the client returns the credentials encrypted
// with AES-128-ECB under MD5 of the shared secret, followed by its own public
// key. The RFB stream after it stays in the clear, so Encrypted remains false.
func (client *Client) authenticateARD(config Config) error {
	parameters, err := readARDParameters(client.r)
	if err != nil {
		return err
	}
	username, password, err := client.ardCredentials(config)
	if err != nil {
		return err
	}
	if !utf8.ValidString(username) || len(username) >= ardFieldBytes || strings.ContainsRune(username, 0) {
		return fmt.Errorf("%w: the ARD account name must be UTF-8 and shorter than %d bytes", ErrProtocol, ardFieldBytes)
	}
	if !utf8.ValidString(password) || len(password) >= ardFieldBytes || strings.ContainsRune(password, 0) {
		return fmt.Errorf("%w: the ARD password must be UTF-8 and shorter than %d bytes", ErrProtocol, ardFieldBytes)
	}

	private, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), ardPrivateBits))
	if err != nil {
		return err
	}
	private.Add(private, big.NewInt(2))
	clientPublic := new(big.Int).Exp(big.NewInt(int64(parameters.generator)), private, parameters.prime)
	shared := new(big.Int).Exp(parameters.public, private, parameters.prime)
	sharedWire := shared.FillBytes(make([]byte, parameters.keyBytes))
	defer clear(sharedWire)
	key := md5.Sum(sharedWire) // the ARD key derivation, not a password hash
	defer clear(key[:])

	plain := make([]byte, ardCredentials)
	if _, err := rand.Read(plain); err != nil {
		return err // random padding makes equal credentials encrypt differently
	}
	defer clear(plain)
	copy(plain, username)
	plain[len(username)] = 0
	copy(plain[ardFieldBytes:], password)
	plain[ardFieldBytes+len(password)] = 0

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return err // not reachable: the MD5 digest is an AES-128 key
	}
	for offset := 0; offset < ardCredentials; offset += aes.BlockSize {
		block.Encrypt(plain[offset:offset+aes.BlockSize], plain[offset:offset+aes.BlockSize])
	}
	message := append(plain, clientPublic.FillBytes(make([]byte, parameters.keyBytes))...)
	if _, err := client.conn.Write(message); err != nil {
		return err
	}
	return client.securityResult()
}

// ardCredentials obtains the account name and password the challenge encrypts.
// Unlike legacy VNC authentication, ARD has no password-only mode: both fields
// are required. AskCredentials takes precedence because a saved system account
// is kept apart from a legacy VNC password.
func (client *Client) ardCredentials(config Config) (username, password string, err error) {
	username, password = config.Username, config.Password
	switch {
	case config.AskCredentials != nil:
		if err := client.userWait(func() (err error) {
			username, password, err = config.AskCredentials()
			return err
		}); err != nil {
			return "", "", err
		}
	case username != "" && password == "" && config.AskPassword != nil:
		if err := client.userWait(func() (err error) {
			password, err = config.AskPassword()
			return err
		}); err != nil {
			return "", "", err
		}
	}
	if username == "" || password == "" {
		return "", "", ErrAuthRequired
	}
	return username, password, nil
}
