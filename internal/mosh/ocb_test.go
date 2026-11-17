package mosh

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestOCBRoundTrip(t *testing.T) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	nonce := make([]byte, 12)
	rand.Read(nonce)

	for _, size := range []int{0, 1, 15, 16, 17, 31, 32, 100, 1024, 1400} {
		plaintext := make([]byte, size)
		rand.Read(plaintext)

		ct := ocb.Encrypt(nonce, plaintext)
		pt := ocb.Decrypt(nonce, ct)
		if pt == nil {
			t.Fatalf("size %d: decrypt failed", size)
		}
		if !bytes.Equal(pt, plaintext) {
			t.Fatalf("size %d: roundtrip mismatch", size)
		}

		// Tamper with tag (at end) — should fail.
		tampered := make([]byte, len(ct))
		copy(tampered, ct)
		tampered[len(ct)-1] ^= 0x01
		if ocb.Decrypt(nonce, tampered) != nil {
			t.Fatalf("size %d: tampered tag should fail", size)
		}

		// Tamper with ciphertext (at start) — should fail.
		if len(ct) > tagSize {
			tampered2 := make([]byte, len(ct))
			copy(tampered2, ct)
			tampered2[0] ^= 0x01
			if ocb.Decrypt(nonce, tampered2) != nil {
				t.Fatalf("size %d: tampered ciphertext should fail", size)
			}
		}
	}
}

// TestOCBMoshWireFormat tests the complete mosh wire format:
// encrypt on server side (direction=TO_CLIENT), decrypt on client side.
func TestOCBMoshWireFormat(t *testing.T) {
	// Simulate mosh key: 22 chars base64 = 16 bytes.
	key := make([]byte, 16)
	rand.Read(key)
	keyStr := base64.StdEncoding.EncodeToString(key)
	t.Logf("key: %s (%d bytes)", keyStr, len(key))

	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	// Server sends a message (direction = TO_CLIENT, bit 63 set).
	const dirServerBit = uint64(1) << 63
	seq := uint64(1)
	dirSeq := dirServerBit | seq

	// Build nonce: [0x00000000:4][dirSeq:8]
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], dirSeq)

	// Build plaintext: [timestamp:2][timestamp_reply:2][payload]
	payload := []byte("hello from mosh server")
	plaintext := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(plaintext[0:], 12345) // timestamp
	binary.BigEndian.PutUint16(plaintext[2:], 0)     // timestamp_reply
	copy(plaintext[4:], payload)

	// Encrypt returns [tag:16][ciphertext].
	tagAndCT := ocb.Encrypt(nonce[:], plaintext)

	// Build wire format: [dirSeq:8][tag+ciphertext]
	wire := make([]byte, 8+len(tagAndCT))
	binary.BigEndian.PutUint64(wire[0:], dirSeq)
	copy(wire[8:], tagAndCT)

	// Client side: parse wire format.
	if len(wire) < 24 {
		t.Fatal("wire too short")
	}

	rxDirSeq := binary.BigEndian.Uint64(wire[0:8])
	if rxDirSeq&dirServerBit == 0 {
		t.Fatal("direction bit not set")
	}
	rxSeq := rxDirSeq & ((uint64(1) << 63) - 1)
	if rxSeq != 1 {
		t.Fatalf("seq = %d, want 1", rxSeq)
	}

	// Reconstruct nonce.
	var rxNonce [12]byte
	copy(rxNonce[4:], wire[0:8])

	// Decrypt.
	rxPlaintext := ocb.Decrypt(rxNonce[:], wire[8:])
	if rxPlaintext == nil {
		t.Fatal("decrypt failed")
	}

	// Parse timestamp + payload.
	if len(rxPlaintext) < 4 {
		t.Fatal("plaintext too short")
	}
	ts := binary.BigEndian.Uint16(rxPlaintext[0:])
	if ts != 12345 {
		t.Fatalf("timestamp = %d, want 12345", ts)
	}
	rxPayload := rxPlaintext[4:]
	if string(rxPayload) != "hello from mosh server" {
		t.Fatalf("payload = %q", rxPayload)
	}
}

// TestOCBDifferentNonces ensures different nonces produce different ciphertexts.
func TestOCBDifferentNonces(t *testing.T) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, _ := NewOCB(key)

	plaintext := []byte("same plaintext")

	nonce1 := make([]byte, 12)
	nonce1[11] = 1
	nonce2 := make([]byte, 12)
	nonce2[11] = 2

	ct1 := ocb.Encrypt(nonce1, plaintext)
	ct2 := ocb.Encrypt(nonce2, plaintext)

	if bytes.Equal(ct1, ct2) {
		t.Fatal("different nonces produced same ciphertext")
	}

	// But each decrypts correctly with its own nonce.
	if pt := ocb.Decrypt(nonce1, ct1); !bytes.Equal(pt, plaintext) {
		t.Fatal("nonce1 decrypt failed")
	}
	if pt := ocb.Decrypt(nonce2, ct2); !bytes.Equal(pt, plaintext) {
		t.Fatal("nonce2 decrypt failed")
	}

	// Cross-nonce should fail.
	if ocb.Decrypt(nonce2, ct1) != nil {
		t.Fatal("cross-nonce should fail")
	}
}

func TestOCBBadKeyLength(t *testing.T) {
	_, err := NewOCB([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for bad key length")
	}
}

func TestOCBShortCiphertext(t *testing.T) {
	key := make([]byte, 16)
	ocb, _ := NewOCB(key)
	nonce := make([]byte, 12)

	// Less than tag size should return nil.
	if ocb.Decrypt(nonce, make([]byte, 15)) != nil {
		t.Fatal("short ciphertext should return nil")
	}
	if ocb.Decrypt(nonce, nil) != nil {
		t.Fatal("nil ciphertext should return nil")
	}
}

func TestNTZ(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{1, 0}, {2, 1}, {3, 0}, {4, 2}, {5, 0}, {6, 1}, {7, 0}, {8, 3},
		{16, 4}, {32, 5}, {0, 32},
	}
	for _, tt := range tests {
		if got := ntz(tt.n); got != tt.want {
			t.Errorf("ntz(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

// TestOCBRFC7253Vectors tests against RFC 7253 Appendix A test vectors
// for AES-128-OCB with TAGLEN=128. Only vectors with empty associated data
// are tested since this OCB implementation does not support AAD.
func TestOCBRFC7253Vectors(t *testing.T) {
	key := unhex(t, "000102030405060708090A0B0C0D0E0F")
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	vectors := []struct {
		name  string
		nonce string // hex
		plain string // hex, "" = empty
		ct    string // hex, ciphertext + tag
	}{
		{
			// RFC 7253 Appendix A, Vector 1: N=...1100, A="", P=""
			// Output is tag-only (16 bytes).
			name:  "Vector1_empty_empty",
			nonce: "BBAA99887766554433221100",
			plain: "",
			ct:    "785407BFFFC8AD9EDCC5520AC9111EE6",
		},
		{
			// RFC 7253 Appendix A, Vector 3: N=...1102, A="", P=8 bytes
			// Output is 8 bytes ciphertext + 16 bytes tag = 24 bytes.
			name:  "Vector3_empty_8B",
			nonce: "BBAA99887766554433221102",
			plain: "0001020304050607",
			ct:    "6DD42C17CBF9C7835DFD6E630E8F98EB3D2A49B0DC0F314E",
		},
		{
			// RFC 7253 Appendix A, Vector 5: N=...1104, A="", P=16 bytes
			// Output is 16 bytes ciphertext + 16 bytes tag = 32 bytes.
			name:  "Vector5_empty_16B",
			nonce: "BBAA99887766554433221104",
			plain: "000102030405060708090A0B0C0D0E0F",
			ct:    "571D535B60B277188BE5147170A9A22C5E77B6AF964090C0F8F567B7B2763E1C",
		},
		{
			// RFC 7253 Appendix A, Vector 7: N=...1106, A="", P=24 bytes
			// Output is 24 bytes ciphertext + 16 bytes tag = 40 bytes.
			name:  "Vector7_empty_24B",
			nonce: "BBAA99887766554433221106",
			plain: "000102030405060708090A0B0C0D0E0F1011121314151617",
			ct:    "5CE88EC2E0692706A915C00AEB8B23968467B2CFBB580496A361F6B4F1C479B222D7011EAA7B3144",
		},
		{
			// RFC 7253 Appendix A, Vector 9: N=...1108, A="", P=32 bytes
			// Output is 32 bytes ciphertext + 16 bytes tag = 48 bytes.
			name:  "Vector9_empty_32B",
			nonce: "BBAA99887766554433221108",
			plain: "000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F",
			ct:    "FED5B2062E331BD1D243DCE4030BF42B1F0391097939C462293DAC9FABC97010CFD6EF3E7FF48413E807CE43F63E7977",
		},
	}

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			nonce := unhex(t, v.nonce)
			plaintext := unhex(t, v.plain)
			expected := unhex(t, v.ct)

			// Encrypt and compare.
			got := ocb.Encrypt(nonce, plaintext)
			if !bytes.Equal(got, expected) {
				t.Fatalf("encrypt:\n got %X\nwant %X", got, expected)
			}

			// Decrypt and compare.
			pt := ocb.Decrypt(nonce, expected)
			if pt == nil {
				t.Fatal("decrypt returned nil")
			}
			if !bytes.Equal(pt, plaintext) {
				t.Fatalf("decrypt:\n got %X\nwant %X", pt, plaintext)
			}
		})
	}
}

// TestOCBConcurrent verifies OCB is safe for concurrent use with different nonces.
func TestOCBConcurrent(t *testing.T) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("concurrent test payload")
	const goroutines = 100

	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			nonce := make([]byte, 12)
			binary.BigEndian.PutUint64(nonce[4:], uint64(id))

			ct := ocb.Encrypt(nonce, plaintext)
			pt := ocb.Decrypt(nonce, ct)
			if pt == nil {
				errs <- fmt.Errorf("goroutine %d: decrypt failed", id)
				return
			}
			if !bytes.Equal(pt, plaintext) {
				errs <- fmt.Errorf("goroutine %d: roundtrip mismatch", id)
				return
			}
			errs <- nil
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// TestOCBEmptyPlaintextTagSize verifies that encrypting empty plaintext
// produces exactly 16 bytes (tag only) and decrypts back to empty.
func TestOCBEmptyPlaintextTagSize(t *testing.T) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	nonce := make([]byte, 12)
	nonce[11] = 1

	ct := ocb.Encrypt(nonce, nil)
	if len(ct) != tagSize {
		t.Fatalf("empty plaintext ciphertext length = %d, want %d", len(ct), tagSize)
	}

	pt := ocb.Decrypt(nonce, ct)
	if pt == nil {
		t.Fatal("decrypt returned nil")
	}
	if len(pt) != 0 {
		t.Fatalf("decrypted plaintext length = %d, want 0", len(pt))
	}
}

// unhex decodes a hex string, failing the test on error.
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// TestOCBNonceReuseDifferentOutput verifies that encrypting two different
// plaintexts with the same nonce produces different ciphertexts.
// NOTE: Nonce reuse is catastrophic for OCB security — an attacker can
// recover the auth key and forge messages. The mosh sequence number
// mechanism prevents nonce reuse in practice.
func TestOCBNonceReuseDifferentOutput(t *testing.T) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, err := NewOCB(key)
	if err != nil {
		t.Fatal(err)
	}

	nonce := make([]byte, 12)
	nonce[11] = 42

	pt1 := []byte("plaintext one")
	pt2 := []byte("plaintext two")

	ct1 := ocb.Encrypt(nonce, pt1)
	ct2 := ocb.Encrypt(nonce, pt2)

	if bytes.Equal(ct1, ct2) {
		t.Fatal("different plaintexts with same nonce produced identical ciphertexts")
	}

	// Both should still decrypt correctly.
	if got := ocb.Decrypt(nonce, ct1); !bytes.Equal(got, pt1) {
		t.Fatal("ct1 decrypt failed")
	}
	if got := ocb.Decrypt(nonce, ct2); !bytes.Equal(got, pt2) {
		t.Fatal("ct2 decrypt failed")
	}
}

func BenchmarkOCBEncrypt1400(b *testing.B) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, _ := NewOCB(key)
	nonce := make([]byte, 12)
	plaintext := make([]byte, 1400)
	rand.Read(plaintext)

	b.ResetTimer()
	b.SetBytes(1400)
	for i := 0; i < b.N; i++ {
		ocb.Encrypt(nonce, plaintext)
	}
}

func BenchmarkOCBDecrypt1400(b *testing.B) {
	key := make([]byte, 16)
	rand.Read(key)
	ocb, _ := NewOCB(key)
	nonce := make([]byte, 12)
	plaintext := make([]byte, 1400)
	rand.Read(plaintext)
	ct := ocb.Encrypt(nonce, plaintext)

	b.ResetTimer()
	b.SetBytes(1400)
	for i := 0; i < b.N; i++ {
		ocb.Decrypt(nonce, ct)
	}
}
