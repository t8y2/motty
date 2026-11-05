package rfb

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/subtle"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"
	"unicode/utf8"
)

// RA2 (security type 5) exchanges PKCS#1-v1.5-encrypted random values, binds
// the RSA public keys with SHA-1, and uses directional AES-128-EAX records for
// both authentication and the entire RFB session. SHA-1 is mandated by this
// wire protocol; it is not a certificate signature or a password hash.
//
// Protocol references: noVNC core/ra2.js (the shared RA2/RA2ne handshake) and
// TigerVNC common/rfb/CSecurityRSAAES.cxx. This implementation uses Go's RSA
// primitives and independently implements the protocol; it does not enable
// RA2ne, unverified type 13, or other proprietary security schemes.
const (
	ra2MinKeyBits    = 1024 // also interoperates with older RSA-AES servers
	ra2MaxKeyBits    = 8192
	ra2ClientKeyBits = 2048
	ra2RandomSize    = 16
	ra2UserPass      = 1
	ra2Password      = 2
)

// readerConn retains bytes the version/security reader may have prefetched
// when encryption begins. Writes and connection control still use the stream.
type readerConn struct {
	net.Conn
	reader io.Reader
}

func (conn *readerConn) Read(p []byte) (int, error) { return conn.reader.Read(p) }

func readRA2PublicKey(reader io.Reader) (*rsa.PublicKey, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, nil, err
	}
	bits := binary.BigEndian.Uint32(header[:])
	if bits < ra2MinKeyBits || bits > ra2MaxKeyBits {
		return nil, nil, fmt.Errorf("%w: RSA-AES key length %d is outside %d..%d bits", ErrProtocol, bits, ra2MinKeyBits, ra2MaxKeyBits)
	}
	size := int((bits + 7) / 8)
	wire := make([]byte, 4+2*size)
	copy(wire, header[:])
	if _, err := io.ReadFull(reader, wire[4:]); err != nil {
		return nil, nil, err
	}
	n := new(big.Int).SetBytes(wire[4 : 4+size])
	e := new(big.Int).SetBytes(wire[4+size:])
	// Go RSA public exponents must fit in int32. Reject malformed lengths and
	// even moduli/exponents before verification or expensive key operations.
	if n.BitLen() != int(bits) || n.Bit(0) == 0 || !e.IsInt64() || e.Int64() < 3 || e.Int64() > 1<<31-1 || e.Bit(0) == 0 {
		return nil, nil, fmt.Errorf("%w: invalid RSA-AES public key", ErrProtocol)
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, wire, nil
}

func ra2PublicKeyWire(key *rsa.PublicKey) []byte {
	size := key.Size()
	wire := make([]byte, 4+2*size)
	binary.BigEndian.PutUint32(wire, uint32(key.N.BitLen()))
	key.N.FillBytes(wire[4 : 4+size])
	big.NewInt(int64(key.E)).FillBytes(wire[4+size:])
	return wire
}

func ra2Hash(parts ...[]byte) []byte {
	hash := sha1.New()
	for _, part := range parts {
		_, _ = hash.Write(part)
	}
	return hash.Sum(nil)
}

// userWait excludes user interaction from the network handshake's deadline.
// Context cancellation is owned by desktop/vnc and closes the connection even
// while its credential or trust callback is waiting.
func (client *Client) userWait(callback func() error) error {
	if err := client.conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	err := callback()
	if err != nil {
		return err
	}
	return client.conn.SetDeadline(time.Now().Add(client.timeout))
}

func (client *Client) authenticateRA2(config Config) error {
	if config.VerifyServerKey == nil {
		return ErrServerKeyRequired
	}
	serverKey, serverWire, err := readRA2PublicKey(client.r)
	if err != nil {
		return fmt.Errorf("rfb: reading RSA-AES server key: %w", err)
	}
	der, err := x509.MarshalPKIXPublicKey(serverKey)
	if err != nil {
		return err
	}
	if err := client.userWait(func() error { return config.VerifyServerKey(der) }); err != nil {
		return err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, ra2ClientKeyBits)
	if err != nil {
		return err
	}
	clientWire := ra2PublicKeyWire(&privateKey.PublicKey)
	if _, err := client.conn.Write(clientWire); err != nil {
		return err
	}
	clientRandom := make([]byte, ra2RandomSize)
	if _, err := io.ReadFull(rand.Reader, clientRandom); err != nil {
		return err
	}
	defer clear(clientRandom)
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, serverKey, clientRandom)
	if err != nil {
		return fmt.Errorf("rfb: encrypting RSA-AES client random: %w", err)
	}
	message := binary.BigEndian.AppendUint16(nil, uint16(len(encrypted)))
	message = append(message, encrypted...)
	if _, err := client.conn.Write(message); err != nil {
		return err
	}
	var length [2]byte
	if _, err := io.ReadFull(client.r, length[:]); err != nil {
		return err
	}
	if int(binary.BigEndian.Uint16(length[:])) != privateKey.Size() {
		return fmt.Errorf("%w: invalid RSA-AES encrypted random length", ErrProtocol)
	}
	encrypted = make([]byte, privateKey.Size())
	if _, err := io.ReadFull(client.r, encrypted); err != nil {
		return err
	}
	// On invalid PKCS#1 padding, keep a random session key and let the EAX
	// proof fail uniformly, rather than exposing an RSA padding oracle.
	serverRandom := make([]byte, ra2RandomSize)
	if _, err := io.ReadFull(rand.Reader, serverRandom); err != nil {
		return err
	}
	if err := rsa.DecryptPKCS1v15SessionKey(rand.Reader, privateKey, encrypted, serverRandom); err != nil {
		clear(serverRandom)
		return fmt.Errorf("%w: invalid RSA-AES encrypted random", ErrProtocol)
	}
	defer clear(serverRandom)
	readKey := ra2Hash(clientRandom, serverRandom)[:ra2RandomSize]
	writeKey := ra2Hash(serverRandom, clientRandom)[:ra2RandomSize]
	defer clear(readKey)
	defer clear(writeKey)
	secured, err := newEAXConn(&readerConn{Conn: client.conn, reader: client.r}, readKey, writeKey)
	if err != nil {
		return err
	}
	// The wrapper owns the original buffered reader. Its replacement reads
	// authenticated plaintext, including the SecurityResult and ServerInit.
	client.conn = secured
	client.r = bufio.NewReaderSize(secured, 64<<10)
	if _, err := secured.Write(ra2Hash(clientWire, serverWire)); err != nil {
		return err
	}
	proof := make([]byte, sha1.Size)
	if _, err := io.ReadFull(client.r, proof); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(proof, ra2Hash(serverWire, clientWire)) != 1 {
		return fmt.Errorf("%w: RSA-AES server key proof does not match", ErrProtocol)
	}
	var subtype [1]byte
	if _, err := io.ReadFull(client.r, subtype[:]); err != nil {
		return err
	}
	credentials, err := client.ra2Credentials(config, subtype[0])
	if err != nil {
		return err
	}
	defer clear(credentials)
	if _, err := secured.Write(credentials); err != nil {
		return err
	}
	if err := client.securityResult(); err != nil {
		return err
	}
	client.Encrypted = true
	return nil
}

func (client *Client) ra2Credentials(config Config, subtype byte) ([]byte, error) {
	username, password := config.Username, config.Password
	switch subtype {
	case ra2UserPass:
		if config.AskCredentials != nil {
			if err := client.userWait(func() (err error) {
				username, password, err = config.AskCredentials()
				return err
			}); err != nil {
				return nil, err
			}
		} else if username != "" && password == "" && config.AskPassword != nil {
			if err := client.userWait(func() (err error) {
				password, err = config.AskPassword()
				return err
			}); err != nil {
				return nil, err
			}
		}
		if password == "" {
			return nil, ErrAuthRequired
		}
	case ra2Password:
		username = ""
		if password == "" && config.AskPassword != nil {
			if err := client.userWait(func() (err error) {
				password, err = config.AskPassword()
				return err
			}); err != nil {
				return nil, err
			}
		}
		if password == "" {
			return nil, ErrAuthRequired
		}
	default:
		return nil, fmt.Errorf("%w: RSA-AES authentication subtype %d", ErrUnsupported, subtype)
	}
	// Lengths are UTF-8 byte counts, not characters. Never silently truncate
	// credentials: it would authenticate a different account or password.
	if !utf8.ValidString(username) || !utf8.ValidString(password) || len(username) > 255 || len(password) > 255 {
		return nil, fmt.Errorf("%w: RSA-AES credentials exceed 255 UTF-8 bytes or contain invalid UTF-8", ErrProtocol)
	}
	out := append([]byte{byte(len(username))}, username...)
	out = append(out, byte(len(password)))
	return append(out, password...), nil
}
