package nla

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"log/slog"
)

// ntlmSeal provides NTLM SEAL/SIGN operations with Extended Session Security.
// RC4 cipher handles are persistent (NOT reset between messages) per the spec.
type ntlmSeal struct {
	clientSignKey [16]byte
	serverSignKey [16]byte
	clientSealKey [16]byte
	serverSealKey [16]byte
	clientSeal    *rc4.Cipher
	serverSeal    *rc4.Cipher
	seqNumClient  uint32
	seqNumServer  uint32
	log           *slog.Logger
}

// newNTLMSeal derives signing/sealing keys from the ExportedSessionKey
// and initializes persistent RC4 cipher handles.
func newNTLMSeal(exportedSessionKey [16]byte, log *slog.Logger) *ntlmSeal {
	s := &ntlmSeal{}
	s.log = log

	// Derive keys per MS-NLMP 3.4.4
	// ClientSigningKey = MD5(ExportedSessionKey || "session key to client-to-server signing key magic constant\0")
	s.clientSignKey = signKey(exportedSessionKey[:],
		"session key to client-to-server signing key magic constant\x00")
	s.serverSignKey = signKey(exportedSessionKey[:],
		"session key to server-to-client signing key magic constant\x00")

	s.clientSealKey = sealKey(exportedSessionKey[:],
		"session key to client-to-server sealing key magic constant\x00")
	s.serverSealKey = sealKey(exportedSessionKey[:],
		"session key to server-to-client sealing key magic constant\x00")

	s.clientSeal, _ = rc4.NewCipher(s.clientSealKey[:])
	s.serverSeal, _ = rc4.NewCipher(s.serverSealKey[:])
	return s
}

// seal encrypts a message and produces signature(16) || ciphertext.
// Per MS-NLMP 3.4.4, the message is encrypted first, then the MAC is computed.
// Both operations share the same RC4 handle, so order affects keystream offsets.
func (s *ntlmSeal) seal(plaintext []byte) []byte {
	// Encrypt first (consumes len(plaintext) bytes of RC4 keystream)
	ciphertext := make([]byte, len(plaintext))
	s.clientSeal.XORKeyStream(ciphertext, plaintext)

	// MAC over plaintext (consumes 8 bytes of RC4 keystream for checksum encryption)
	sig := s.mac(s.clientSignKey[:], s.clientSeal, s.seqNumClient, plaintext)
	s.seqNumClient++

	s.log.LogAttrs(context.Background(), slog.LevelDebug, "NTLM seal", slog.Int("plainLen", len(plaintext)), slog.Int("seqNum", int(s.seqNumClient)))
	return append(sig[:], ciphertext...)
}

// unseal decrypts a message and verifies the server's MAC.
// Input: signature(16) || ciphertext.
func (s *ntlmSeal) unseal(data []byte) ([]byte, error) {
	if len(data) < 16 {
		return nil, errors.New("ntlm seal: data too short")
	}
	sig := data[:16]
	ciphertext := data[16:]

	// Decrypt
	plaintext := make([]byte, len(ciphertext))
	s.serverSeal.XORKeyStream(plaintext, ciphertext)

	// Verify MAC
	expected := s.mac(s.serverSignKey[:], s.serverSeal, s.seqNumServer, plaintext)
	s.seqNumServer++

	if !hmacEqual(sig, expected[:]) {
		return nil, errors.New("ntlm seal: MAC verification failed")
	}
	s.log.LogAttrs(context.Background(), slog.LevelDebug, "NTLM unseal", slog.Int("cipherLen", len(ciphertext)), slog.Int("seqNum", int(s.seqNumServer)))
	return plaintext, nil
}

// mac computes the NTLM MAC with Extended Session Security per MS-NLMP 3.4.4.2.
func (s *ntlmSeal) mac(signingKey []byte, sealHandle *rc4.Cipher, seqNum uint32, message []byte) [16]byte {
	var seqBuf [4]byte
	binary.LittleEndian.PutUint32(seqBuf[:], seqNum)

	// HMAC_MD5(SigningKey, SeqNum || Message)
	h := hmac.New(md5.New, signingKey)
	h.Write(seqBuf[:])
	h.Write(message)
	hmacResult := h.Sum(nil)

	// Truncate to first 8 bytes and encrypt with seal handle
	var encrypted [8]byte
	sealHandle.XORKeyStream(encrypted[:], hmacResult[:8])

	// Build signature: Version(4) + Checksum(8) + SeqNum(4)
	var sig [16]byte
	binary.LittleEndian.PutUint32(sig[0:4], 0x00000001) // Version
	copy(sig[4:12], encrypted[:])
	copy(sig[12:16], seqBuf[:])
	return sig
}

func signKey(exportedSessionKey []byte, magic string) [16]byte {
	h := md5.New()
	h.Write(exportedSessionKey)
	h.Write([]byte(magic))
	var key [16]byte
	copy(key[:], h.Sum(nil))
	return key
}

func sealKey(exportedSessionKey []byte, magic string) [16]byte {
	h := md5.New()
	h.Write(exportedSessionKey)
	h.Write([]byte(magic))
	var key [16]byte
	copy(key[:], h.Sum(nil))
	return key
}

// mechListMIC signs the SPNEGO mechanism list (RFC 4178 §5, MS-SPNG 3.2.5.1).
// The MIC is the first signed message of the NTLM context: it uses sequence
// number zero and its own request-specific RC4 state, so the persistent sealing
// cipher that the first application message uses stays at its start. Peers that
// instead restore a shared handle around the MIC end up at the same state.
func (s *ntlmSeal) mechListMIC(mechTypes []byte) [16]byte {
	cipher, _ := rc4.NewCipher(s.clientSealKey[:])
	return s.mac(s.clientSignKey[:], cipher, 0, mechTypes)
}

// verifyMechListMIC checks the acceptor's mechListMIC over the mechanism list
// the initiator sent, with the same sequence and RC4 rules as mechListMIC.
func (s *ntlmSeal) verifyMechListMIC(mechTypes, mic []byte) bool {
	cipher, _ := rc4.NewCipher(s.serverSealKey[:])
	expected := s.mac(s.serverSignKey[:], cipher, 0, mechTypes)
	return hmacEqual(mic, expected[:])
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
