package rfb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	eaxTagSize   = aes.BlockSize
	eaxMaxRecord = 1<<16 - 1
)

// eaxCipher implements EAX as specified by Bellare, Rogaway and Wagner, using
// AES-CMAC (NIST SP 800-38B) for its three domain-separated authentications.
// RA2 uses a 128-bit AES key and the full 128-bit authentication tag.
type eaxCipher struct {
	block  cipher.Block
	k1, k2 [aes.BlockSize]byte
}

func newEAXCipher(key []byte) (*eaxCipher, error) {
	if len(key) != 16 && len(key) != 32 {
		return nil, errors.New("rfb: RSA-AES requires a 128-bit or 256-bit AES key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	c := &eaxCipher{block: block}
	var zero, l [aes.BlockSize]byte
	block.Encrypt(l[:], zero[:])
	c.k1 = cmacDouble(l)
	c.k2 = cmacDouble(c.k1)
	return c, nil
}

// cmacDouble multiplies a block by x in GF(2^128), with reduction polynomial
// x^128+x^7+x^2+x+1. Blocks and their bit ordering are big-endian.
func cmacDouble(block [aes.BlockSize]byte) (out [aes.BlockSize]byte) {
	var carry byte
	for i := len(block) - 1; i >= 0; i-- {
		out[i] = block[i]<<1 | carry
		carry = block[i] >> 7
	}
	out[len(out)-1] ^= 0x87 & (0 - carry)
	return out
}

// mac returns CMAC(0^120 || domain || data) without copying the entire data.
func (c *eaxCipher) mac(domain byte, data []byte) (state [aes.BlockSize]byte) {
	var last [aes.BlockSize]byte
	last[len(last)-1] = domain
	if len(data) == 0 {
		for i := range last {
			last[i] ^= c.k1[i]
		}
		c.block.Encrypt(state[:], last[:])
		return state
	}
	c.block.Encrypt(state[:], last[:])
	for len(data) > aes.BlockSize {
		for i := range state {
			state[i] ^= data[i]
		}
		c.block.Encrypt(state[:], state[:])
		data = data[aes.BlockSize:]
	}
	clear(last[:])
	copy(last[:], data)
	key := c.k1
	if len(data) < aes.BlockSize {
		last[len(data)] = 0x80
		key = c.k2
	}
	for i := range last {
		last[i] ^= state[i] ^ key[i]
	}
	c.block.Encrypt(state[:], last[:])
	return state
}

func (c *eaxCipher) seal(nonce, plaintext, header []byte) []byte {
	n := c.mac(0, nonce)
	h := c.mac(1, header)
	out := make([]byte, len(plaintext)+eaxTagSize)
	cipher.NewCTR(c.block, n[:]).XORKeyStream(out[:len(plaintext)], plaintext)
	tag := c.mac(2, out[:len(plaintext)])
	for i := range tag {
		out[len(plaintext)+i] = tag[i] ^ n[i] ^ h[i]
	}
	return out
}

func (c *eaxCipher) open(nonce, ciphertext, header []byte) ([]byte, error) {
	if len(ciphertext) < eaxTagSize {
		return nil, fmt.Errorf("%w: truncated RSA-AES authentication tag", ErrProtocol)
	}
	message, receivedTag := ciphertext[:len(ciphertext)-eaxTagSize], ciphertext[len(ciphertext)-eaxTagSize:]
	n := c.mac(0, nonce)
	h := c.mac(1, header)
	tag := c.mac(2, message)
	for i := range tag {
		tag[i] ^= n[i] ^ h[i]
	}
	if subtle.ConstantTimeCompare(tag[:], receivedTag) != 1 {
		return nil, fmt.Errorf("%w: invalid RSA-AES authentication tag", ErrProtocol)
	}
	// No plaintext becomes visible before the complete record authenticates.
	out := make([]byte, len(message))
	cipher.NewCTR(c.block, n[:]).XORKeyStream(out, message)
	return out, nil
}

// eaxConn carries the RA2 encrypted stream. Each direction has its own record
// nonce, starting at zero and increasing as an unsigned little-endian integer.
// The two-byte big-endian plaintext length is authenticated as associated data.
// net.Conn supplies addresses, deadlines and Close without changing their
// behavior. The handshake must retain any buffered bytes in the supplied Conn.
type eaxConn struct {
	net.Conn
	readCipher, writeCipher *eaxCipher
	readMu, writeMu         sync.Mutex
	readNonce, writeNonce   [aes.BlockSize]byte
	readExhausted           bool
	writeExhausted          bool
	pending                 []byte
	readErr                 error
	failMu                  sync.Mutex
	failure                 error
}

var _ net.Conn = (*eaxConn)(nil)

func newEAXConn(conn net.Conn, readKey, writeKey []byte) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("rfb: RSA-AES requires a connection")
	}
	r, err := newEAXCipher(readKey)
	if err != nil {
		return nil, err
	}
	w, err := newEAXCipher(writeKey)
	if err != nil {
		return nil, err
	}
	return &eaxConn{Conn: conn, readCipher: r, writeCipher: w}, nil
}

func (conn *eaxConn) failed() error {
	conn.failMu.Lock()
	defer conn.failMu.Unlock()
	return conn.failure
}

// fail invalidates both directions when continuing would risk accepting or
// sending bytes on a corrupted encrypted stream. It also wakes a blocked peer
// operation. A read timeout before any record byte has arrived is recoverable.
func (conn *eaxConn) fail(err error) error {
	conn.failMu.Lock()
	if conn.failure == nil {
		conn.failure = err
	}
	err = conn.failure
	conn.failMu.Unlock()
	_ = conn.Conn.Close()
	return err
}

// incrementEAXNonce returns false when the last possible nonce was consumed.
// The caller records exhaustion so the zero value can never be reused.
func incrementEAXNonce(nonce *[aes.BlockSize]byte) bool {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return true
		}
	}
	return false
}

func (conn *eaxConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	conn.readMu.Lock()
	defer conn.readMu.Unlock()
	for {
		if err := conn.failed(); err != nil {
			return 0, err
		}
		if len(conn.pending) > 0 {
			n := copy(dst, conn.pending)
			conn.pending = conn.pending[n:]
			return n, nil
		}
		plaintext, err := conn.readRecord()
		if err != nil {
			return 0, err
		}
		conn.pending = plaintext
		// A valid empty record advances the nonce but never causes Read(large)
		// to return (0, nil), which would make io.Reader consumers spin.
	}
}

// readRecord is called with readMu held. Record boundaries are independent of
// RFB messages: a hash, subtype, or framebuffer may span or share records.
func (conn *eaxConn) readRecord() ([]byte, error) {
	if conn.readErr != nil {
		return nil, conn.readErr
	}
	if conn.readExhausted {
		return nil, conn.fail(fmt.Errorf("%w: RSA-AES receive nonce exhausted", ErrProtocol))
	}
	var header [2]byte
	n, err := io.ReadFull(conn.Conn, header[:])
	if err != nil {
		if n == 0 {
			// Nothing in this record was consumed. A deadline can be reset and
			// another Read attempted with the same nonce.
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				return nil, err
			}
			conn.readErr = err
			return nil, err
		}
		return nil, conn.fail(fmt.Errorf("rfb: reading RSA-AES record header: %w", err))
	}
	length := int(binary.BigEndian.Uint16(header[:]))
	body := make([]byte, length+eaxTagSize)
	if _, err := io.ReadFull(conn.Conn, body); err != nil {
		return nil, conn.fail(fmt.Errorf("rfb: reading RSA-AES record body: %w", err))
	}
	plaintext, err := conn.readCipher.open(conn.readNonce[:], body, header[:])
	if err != nil {
		return nil, conn.fail(err)
	}
	conn.readExhausted = !incrementEAXNonce(&conn.readNonce)
	return plaintext, nil
}

func (conn *eaxConn) Write(src []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()
	if err := conn.failed(); err != nil {
		return 0, err
	}
	written := 0
	for len(src) > 0 {
		if conn.writeExhausted {
			return written, conn.fail(fmt.Errorf("%w: RSA-AES send nonce exhausted", ErrProtocol))
		}
		message := src[:min(len(src), eaxMaxRecord)]
		var header [2]byte
		binary.BigEndian.PutUint16(header[:], uint16(len(message)))
		body := conn.writeCipher.seal(conn.writeNonce[:], message, header[:])
		record := make([]byte, 2+len(body))
		copy(record, header[:])
		copy(record[2:], body)
		complete, err := writeEAXRecord(conn.Conn, record)
		if complete {
			written += len(message)
			conn.writeExhausted = !incrementEAXNonce(&conn.writeNonce)
		}
		if err != nil {
			return written, conn.fail(fmt.Errorf("rfb: writing RSA-AES record: %w", err))
		}
		src = src[len(message):]
	}
	return written, nil
}

func writeEAXRecord(writer io.Writer, record []byte) (bool, error) {
	for len(record) > 0 {
		n, err := writer.Write(record)
		if n < 0 || n > len(record) {
			return false, io.ErrShortWrite
		}
		record = record[n:]
		if err != nil {
			return len(record) == 0, err
		}
		if n == 0 {
			return false, io.ErrShortWrite
		}
	}
	return true, nil
}
