package mosh

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const (
	blockSize = 16
	tagSize   = 16
)

// OCB implements AES-128-OCB3 authenticated encryption (RFC 7253).
type OCB struct {
	enc     cipher.Block
	dec     cipher.Block
	lStar   [blockSize]byte
	lDollar [blockSize]byte
	l       [32][blockSize]byte
}

// NewOCBFromBase64 creates an OCB cipher from a base64-encoded key.
func NewOCBFromBase64(b64 string) (*OCB, error) {
	key, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("ocb: bad base64: %w", err)
	}
	return NewOCB(key)
}

// NewOCB creates an OCB cipher from a 16-byte AES key.
func NewOCB(key []byte) (*OCB, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("ocb: key must be 16 bytes, got %d", len(key))
	}
	enc, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// AES decrypt block for OCB decrypt.
	dec, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	o := &OCB{enc: enc, dec: dec}

	// L_* = ENCIPHER(K, zeros)
	enc.Encrypt(o.lStar[:], make([]byte, blockSize))

	// L_$ = double(L_*)
	o.lDollar = gfDouble(o.lStar)

	// L_0..L_31 = successive doublings
	o.l[0] = gfDouble(o.lDollar)
	for i := 1; i < 32; i++ {
		o.l[i] = gfDouble(o.l[i-1])
	}
	return o, nil
}

// Encrypt encrypts plaintext with the given nonce (max 15 bytes).
// Returns ciphertext followed by tag (16 bytes), matching mosh's wire format.
func (o *OCB) Encrypt(nonce, plaintext []byte) []byte {
	offset := o.initOffset(nonce)
	var checksum [blockSize]byte

	fullBlocks := len(plaintext) / blockSize
	remaining := len(plaintext) % blockSize
	ciphertext := make([]byte, len(plaintext))

	for i := 0; i < fullBlocks; i++ {
		xorInto(&offset, &o.l[ntz(i+1)])

		// XOR plaintext block into checksum.
		var pBlock [blockSize]byte
		copy(pBlock[:], plaintext[i*blockSize:])
		xorInto(&checksum, &pBlock)

		// C_i = Offset xor ENCIPHER(K, Offset xor P_i)
		var tmp [blockSize]byte
		xorBlocks(&tmp, &offset, &pBlock)
		var enc [blockSize]byte
		o.enc.Encrypt(enc[:], tmp[:])
		xorInto(&enc, &offset)
		copy(ciphertext[i*blockSize:], enc[:])
	}

	if remaining > 0 {
		xorInto(&offset, &o.lStar)

		var pad [blockSize]byte
		o.enc.Encrypt(pad[:], offset[:])

		pStar := plaintext[fullBlocks*blockSize:]
		for i := 0; i < remaining; i++ {
			ciphertext[fullBlocks*blockSize+i] = pStar[i] ^ pad[i]
		}

		// Checksum_* = Checksum_m xor (P_* || 1 || zeros)
		var padded [blockSize]byte
		copy(padded[:], pStar)
		padded[remaining] = 0x80
		xorInto(&checksum, &padded)
	}

	// Tag = ENCIPHER(K, Checksum xor Offset xor L_$)
	xorInto(&checksum, &offset)
	xorInto(&checksum, &o.lDollar)
	var tag [blockSize]byte
	o.enc.Encrypt(tag[:], checksum[:])

	// Return ciphertext || tag (mosh wire order).
	result := make([]byte, len(ciphertext)+tagSize)
	copy(result, ciphertext)
	copy(result[len(ciphertext):], tag[:])
	return result
}

// Decrypt decrypts ciphertext+tag with the given nonce.
// Returns plaintext or nil if authentication fails.
func (o *OCB) Decrypt(nonce, ciphertextAndTag []byte) []byte {
	if len(ciphertextAndTag) < tagSize {
		return nil
	}

	ciphertext := ciphertextAndTag[:len(ciphertextAndTag)-tagSize]
	tag := ciphertextAndTag[len(ciphertextAndTag)-tagSize:]

	offset := o.initOffset(nonce)
	var checksum [blockSize]byte

	fullBlocks := len(ciphertext) / blockSize
	remaining := len(ciphertext) % blockSize
	plaintext := make([]byte, len(ciphertext))

	for i := 0; i < fullBlocks; i++ {
		xorInto(&offset, &o.l[ntz(i+1)])

		var cBlock [blockSize]byte
		copy(cBlock[:], ciphertext[i*blockSize:])
		var tmp [blockSize]byte
		xorBlocks(&tmp, &offset, &cBlock)
		var dec [blockSize]byte
		o.dec.Decrypt(dec[:], tmp[:])
		xorInto(&dec, &offset)
		copy(plaintext[i*blockSize:], dec[:])
		xorInto(&checksum, &dec)
	}

	if remaining > 0 {
		xorInto(&offset, &o.lStar)

		var pad [blockSize]byte
		o.enc.Encrypt(pad[:], offset[:])

		cStar := ciphertext[fullBlocks*blockSize:]
		for i := 0; i < remaining; i++ {
			plaintext[fullBlocks*blockSize+i] = cStar[i] ^ pad[i]
		}

		var padded [blockSize]byte
		copy(padded[:], plaintext[fullBlocks*blockSize:fullBlocks*blockSize+remaining])
		padded[remaining] = 0x80
		xorInto(&checksum, &padded)
	}

	// Compute expected tag.
	xorInto(&checksum, &offset)
	xorInto(&checksum, &o.lDollar)
	var expectedTag [blockSize]byte
	o.enc.Encrypt(expectedTag[:], checksum[:])

	// Constant-time tag comparison.
	if subtle.ConstantTimeCompare(tag, expectedTag[:]) != 1 {
		return nil
	}
	return plaintext
}

// initOffset computes the initial offset from a nonce (RFC 7253 Section 4.2).
func (o *OCB) initOffset(nonce []byte) [blockSize]byte {
	nonceLen := len(nonce)
	if nonceLen > 15 {
		panic("ocb: nonce must be <= 15 bytes")
	}

	// Build the full 16-byte nonce block.
	var nn [blockSize]byte
	nn[0] = byte(((tagSize * 8) % 128) & 0x7f)
	nn[blockSize-1-nonceLen] |= 0x01
	copy(nn[blockSize-nonceLen:], nonce)

	bottom := nn[15] & 0x3f
	nn[15] &= 0xc0

	// Ktop = ENCIPHER(K, nn)
	var ktop [blockSize]byte
	o.enc.Encrypt(ktop[:], nn[:])

	// Stretch = Ktop || (Ktop[0..7] xor Ktop[1..8])
	var stretch [24]byte
	copy(stretch[:16], ktop[:])
	for i := 0; i < 8; i++ {
		stretch[16+i] = ktop[i] ^ ktop[i+1]
	}

	// Extract 128 bits starting at bit position `bottom`.
	var offset [blockSize]byte
	byteShift := int(bottom) >> 3
	bitShift := int(bottom) & 7

	for i := 0; i < blockSize; i++ {
		idx := byteShift + i
		if idx < 24 {
			offset[i] = (stretch[idx] << bitShift) & 0xff
			if idx+1 < 24 {
				offset[i] |= stretch[idx+1] >> (8 - bitShift)
			}
		}
	}
	return offset
}

// gfDouble doubles a block in GF(2^128) with polynomial x^128+x^7+x^2+x+1.
func gfDouble(block [blockSize]byte) [blockSize]byte {
	var result [blockSize]byte
	carry := byte(0)
	for i := blockSize - 1; i >= 0; i-- {
		tmp := (block[i] << 1) | carry
		result[i] = tmp
		carry = block[i] >> 7
	}
	if carry != 0 {
		result[blockSize-1] ^= 0x87
	}
	return result
}

// ntz returns the number of trailing zeros of a positive integer.
func ntz(n int) int {
	if n == 0 {
		return 32
	}
	count := 0
	for (n & 1) == 0 {
		count++
		n >>= 1
	}
	return count
}

func xorInto(a *[blockSize]byte, b *[blockSize]byte) {
	for i := 0; i < blockSize; i++ {
		a[i] ^= b[i]
	}
}

func xorBlocks(dst, a, b *[blockSize]byte) {
	for i := 0; i < blockSize; i++ {
		dst[i] = a[i] ^ b[i]
	}
}
