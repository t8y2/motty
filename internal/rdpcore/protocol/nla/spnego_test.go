package nla

import (
	"encoding/asn1"
	"testing"
)

func TestEncodeNegTokenInit(t *testing.T) {
	// Build a fake NTLM negotiate message
	ntlmMsg := []byte("NTLMSSP\x00\x01\x00\x00\x00")

	encoded, mechTypeList := encodeNegTokenInit(ntlmMsg)
	if len(mechTypeList) == 0 {
		t.Fatal("mechTypeList is empty")
	}

	// Should start with GSSAPI Application[0] tag = 0x60
	if len(encoded) < 2 || encoded[0] != 0x60 {
		t.Fatalf("expected Application[0] tag (0x60), got 0x%02X", encoded[0])
	}

	// Should contain the SPNEGO OID (1.3.6.1.5.5.2)
	spnegoOIDBytes, _ := asn1.Marshal(oidSPNEGO)
	found := false
	for i := 0; i+len(spnegoOIDBytes) <= len(encoded); i++ {
		match := true
		for j := range spnegoOIDBytes {
			if encoded[i+j] != spnegoOIDBytes[j] {
				match = false
				break
			}
		}
		if match {
			found = true
			break
		}
	}
	if !found {
		t.Error("SPNEGO OID not found in encoded NegTokenInit")
	}

	// Should contain the original NTLM message somewhere
	ntlmFound := false
	for i := 0; i+len(ntlmMsg) <= len(encoded); i++ {
		match := true
		for j := range ntlmMsg {
			if encoded[i+j] != ntlmMsg[j] {
				match = false
				break
			}
		}
		if match {
			ntlmFound = true
			break
		}
	}
	if !ntlmFound {
		t.Error("NTLM message not found in encoded NegTokenInit")
	}
}

func TestDecodeNegTokenResp(t *testing.T) {
	// Build a NegTokenResp wrapping a fake NTLM challenge
	ntlmChallenge := []byte("NTLMSSP\x00\x02\x00\x00\x00")
	resp := negTokenResp{
		NegState:      1, // accept-incomplete
		ResponseToken: ntlmChallenge,
	}

	innerBytes, err := asn1.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Wrap in context tag [1]
	wrapped := asn1WrapExplicit(0xa1, innerBytes)

	res, err := decodeNegTokenResp(wrapped)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if res.negState != 1 {
		t.Errorf("negState = %d, want 1", res.negState)
	}
	if string(res.responseToken) != string(ntlmChallenge) {
		t.Errorf("responseToken mismatch")
	}
}

func TestDecodeNegTokenRespDirect(t *testing.T) {
	// Test decoding without the [1] wrapper (just SEQUENCE)
	ntlmChallenge := []byte("test challenge")
	resp := negTokenResp{
		NegState:      1, // accept-incomplete
		ResponseToken: ntlmChallenge,
	}

	data, err := asn1.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	res, err := decodeNegTokenResp(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if res.negState != 1 {
		t.Errorf("negState = %d, want 1", res.negState)
	}
	if string(res.responseToken) != string(ntlmChallenge) {
		t.Errorf("responseToken mismatch")
	}
}

func TestDecodeNegTokenRespAbsentState(t *testing.T) {
	// An initiator's message omits negState; it must not decode as
	// accept-completed (0), because WinPR uses the difference to decide whether
	// the acceptor has to sign the mechanism list.
	mic := []byte("0123456789abcdef")
	data := encodeNegTokenResp([]byte("NTLMSSP\x00\x03\x00\x00\x00"), mic)

	res, err := decodeNegTokenResp(data)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if res.negState != negStateAbsent {
		t.Errorf("negState = %d, want %d", res.negState, negStateAbsent)
	}
	if string(res.mechListMIC) != string(mic) {
		t.Errorf("mechListMIC mismatch")
	}
}

func TestEncodeNegTokenResp(t *testing.T) {
	ntlmAuth := []byte("NTLMSSP\x00\x03\x00\x00\x00")
	encoded := encodeNegTokenResp(ntlmAuth, nil)

	// Should start with context tag [1] = 0xa1
	if len(encoded) < 2 || encoded[0] != 0xa1 {
		t.Fatalf("expected context tag [1] (0xa1), got 0x%02X", encoded[0])
	}

	// Should contain the NTLM message
	found := false
	for i := 0; i+len(ntlmAuth) <= len(encoded); i++ {
		match := true
		for j := range ntlmAuth {
			if encoded[i+j] != ntlmAuth[j] {
				match = false
				break
			}
		}
		if match {
			found = true
			break
		}
	}
	if !found {
		t.Error("NTLM message not found in encoded NegTokenResp")
	}
}

func TestASN1LengthEncoding(t *testing.T) {
	tests := []struct {
		length int
		want   []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7f}},
		{128, []byte{0x81, 0x80}},
		{255, []byte{0x81, 0xff}},
		{256, []byte{0x82, 0x01, 0x00}},
		{65535, []byte{0x82, 0xff, 0xff}},
	}

	for _, tt := range tests {
		got := asn1EncodeLength(tt.length)
		if len(got) != len(tt.want) {
			t.Errorf("asn1EncodeLength(%d) length = %d, want %d", tt.length, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("asn1EncodeLength(%d) = %x, want %x", tt.length, got, tt.want)
				break
			}
		}
	}
}
