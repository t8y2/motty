package x224

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"testing"
)

func TestConnectionRequestEncodeMinimal(t *testing.T) {
	cr := &ConnectionRequest{
		DestRef: 0,
		SrcRef:  0,
		Class:   0,
	}
	data := cr.Encode()

	// Minimal CR: LI(1) + type(1) + destRef(2) + srcRef(2) + class(1) + RDP_NEG_REQ(8) = 15 bytes
	if len(data) != 15 {
		t.Fatalf("expected 15 bytes, got %d", len(data))
	}
	if data[0] != 14 { // LI = 6 + 8 (fixed header + RDP_NEG_REQ)
		t.Errorf("LI = %d, want 14", data[0])
	}
	if data[1] != TypeConnectionRequest {
		t.Errorf("type = 0x%02X, want 0x%02X", data[1], TypeConnectionRequest)
	}
	// RDP_NEG_REQ with requestedProtocols = 0 (Standard RDP Security)
	if data[7] != TypeRDPNegReq {
		t.Errorf("neg type = 0x%02X, want 0x%02X", data[7], TypeRDPNegReq)
	}
	protos := binary.LittleEndian.Uint32(data[11:15])
	if protos != ProtocolRDP {
		t.Errorf("protocols = 0x%08X, want 0x%08X", protos, ProtocolRDP)
	}
}

func TestConnectionRequestEncodeWithCookie(t *testing.T) {
	cr := &ConnectionRequest{
		Cookie: "testuser",
	}
	data := cr.Encode()

	// Should contain the cookie string
	if !bytes.Contains(data, []byte("Cookie: mstshash=testuser\r\n")) {
		t.Error("encoded data does not contain expected cookie")
	}
}

func TestConnectionRequestEncodeWithProtocols(t *testing.T) {
	cr := &ConnectionRequest{
		RequestedProtos: ProtocolSSL | ProtocolHybrid,
	}
	data := cr.Encode()

	// Find RDP_NEG_REQ at the end: type=0x01, flags=0x00, length=0x0008, protocols
	// The negotiation request is in the variable data after the 7-byte fixed header
	negOffset := 7 // after fixed header
	if len(data) < negOffset+8 {
		t.Fatalf("data too short: %d bytes", len(data))
	}

	if data[negOffset] != TypeRDPNegReq {
		t.Errorf("neg type = 0x%02X, want 0x%02X", data[negOffset], TypeRDPNegReq)
	}

	negLen := binary.LittleEndian.Uint16(data[negOffset+2 : negOffset+4])
	if negLen != 8 {
		t.Errorf("neg length = %d, want 8", negLen)
	}

	protos := binary.LittleEndian.Uint32(data[negOffset+4 : negOffset+8])
	want := ProtocolSSL | ProtocolHybrid
	if protos != want {
		t.Errorf("protocols = 0x%08X, want 0x%08X", protos, want)
	}
}

func TestConnectionRequestEncodeWithCookieAndProtocols(t *testing.T) {
	cr := &ConnectionRequest{
		Cookie:          "admin",
		RequestedProtos: ProtocolSSL,
	}
	data := cr.Encode()

	// Verify cookie is present
	if !bytes.Contains(data, []byte("Cookie: mstshash=admin\r\n")) {
		t.Error("cookie not found in encoded data")
	}

	// The LI should account for both cookie and negotiation request
	li := data[0]
	expectedVarLen := len("Cookie: mstshash=admin\r\n") + 8 // cookie + neg request
	expectedLI := 6 + expectedVarLen
	if int(li) != expectedLI {
		t.Errorf("LI = %d, want %d", li, expectedLI)
	}
}

func TestDecodeConnectionConfirmSuccess(t *testing.T) {
	// Build a valid CC response with SSL selected
	// LI covers the fixed header (type + destref + srcref + class = 6 bytes)
	// plus the negotiation data (8 bytes) = 14, but LI doesn't count itself
	// so the negotiation data starts at byte 7 (index 7).
	var buf bytes.Buffer
	buf.WriteByte(6)                                     // LI (fixed header only)
	buf.WriteByte(TypeConnectionConfirm)                 // Type 0xD0
	binary.Write(&buf, binary.BigEndian, uint16(0))      // DestRef
	binary.Write(&buf, binary.BigEndian, uint16(0x1234)) // SrcRef
	buf.WriteByte(0)                                     // Class
	buf.WriteByte(TypeRDPNegRsp)                         // RDP_NEG_RSP
	buf.WriteByte(0x00)                                  // Flags
	binary.Write(&buf, binary.LittleEndian, uint16(8))   // Length
	binary.Write(&buf, binary.LittleEndian, ProtocolSSL) // Selected protocol

	cc, err := DecodeConnectionConfirm(slog.Default(), buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeConnectionConfirm error: %v", err)
	}
	if cc.Type != TypeRDPNegRsp {
		t.Errorf("type = 0x%02X, want 0x%02X", cc.Type, TypeRDPNegRsp)
	}
	if cc.SelectedProto != ProtocolSSL {
		t.Errorf("protocol = 0x%08X, want 0x%08X", cc.SelectedProto, ProtocolSSL)
	}
	if cc.SrcRef != 0x1234 {
		t.Errorf("SrcRef = 0x%04X, want 0x1234", cc.SrcRef)
	}
}

func TestDecodeConnectionConfirmLI14(t *testing.T) {
	// Real servers set LI=14 (6 fixed + 8 negotiation), not LI=6.
	// Verify parsing works with the larger LI value.
	var buf bytes.Buffer
	buf.WriteByte(14)                                    // LI (fixed + negotiation)
	buf.WriteByte(TypeConnectionConfirm)                 // Type 0xD0
	binary.Write(&buf, binary.BigEndian, uint16(0))      // DestRef
	binary.Write(&buf, binary.BigEndian, uint16(0x1234)) // SrcRef
	buf.WriteByte(0)                                     // Class
	buf.WriteByte(TypeRDPNegRsp)                         // RDP_NEG_RSP
	buf.WriteByte(0x1F)                                  // Flags
	binary.Write(&buf, binary.LittleEndian, uint16(8))   // Length
	binary.Write(&buf, binary.LittleEndian, ProtocolSSL) // Selected protocol

	cc, err := DecodeConnectionConfirm(slog.Default(), buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeConnectionConfirm error: %v", err)
	}
	if cc.Type != TypeRDPNegRsp {
		t.Errorf("type = 0x%02X, want 0x%02X", cc.Type, TypeRDPNegRsp)
	}
	if cc.SelectedProto != ProtocolSSL {
		t.Errorf("protocol = 0x%08X, want 0x%08X", cc.SelectedProto, ProtocolSSL)
	}
	if cc.Flags != 0x1F {
		t.Errorf("flags = 0x%02X, want 0x1F", cc.Flags)
	}
}

func TestDecodeConnectionConfirmHybrid(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(6)
	buf.WriteByte(TypeConnectionConfirm)
	binary.Write(&buf, binary.BigEndian, uint16(0))
	binary.Write(&buf, binary.BigEndian, uint16(0))
	buf.WriteByte(0)
	buf.WriteByte(TypeRDPNegRsp)
	buf.WriteByte(0x00)
	binary.Write(&buf, binary.LittleEndian, uint16(8))
	binary.Write(&buf, binary.LittleEndian, ProtocolHybrid)

	cc, err := DecodeConnectionConfirm(slog.Default(), buf.Bytes())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if cc.SelectedProto != ProtocolHybrid {
		t.Errorf("protocol = 0x%08X, want 0x%08X", cc.SelectedProto, ProtocolHybrid)
	}
}

func TestDecodeConnectionConfirmFailure(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(6)
	buf.WriteByte(TypeConnectionConfirm)
	binary.Write(&buf, binary.BigEndian, uint16(0))
	binary.Write(&buf, binary.BigEndian, uint16(0))
	buf.WriteByte(0)
	buf.WriteByte(TypeRDPNegFail)
	buf.WriteByte(0x00)
	binary.Write(&buf, binary.LittleEndian, uint16(8))
	binary.Write(&buf, binary.LittleEndian, FailHybridRequiredByServer)

	cc, err := DecodeConnectionConfirm(slog.Default(), buf.Bytes())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if cc.Type != TypeRDPNegFail {
		t.Errorf("type = 0x%02X, want 0x%02X", cc.Type, TypeRDPNegFail)
	}
	if cc.FailureCode != FailHybridRequiredByServer {
		t.Errorf("failure = 0x%08X, want 0x%08X", cc.FailureCode, FailHybridRequiredByServer)
	}
}

func TestDecodeConnectionConfirmTooShort(t *testing.T) {
	_, err := DecodeConnectionConfirm(slog.Default(), []byte{0x05, 0xD0, 0x00})
	if err == nil {
		t.Error("expected error for too-short input")
	}
}

func TestDecodeConnectionConfirmWrongType(t *testing.T) {
	data := []byte{0x06, TypeConnectionRequest, 0x00, 0x00, 0x00, 0x00, 0x00}
	_, err := DecodeConnectionConfirm(slog.Default(), data)
	if err == nil {
		t.Error("expected error for wrong type code")
	}
}

func TestEncodeDataTPDU(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	result := EncodeDataTPDU(data)

	if len(result) != 6 { // 3 header + 3 data
		t.Fatalf("expected 6 bytes, got %d", len(result))
	}
	if result[0] != 0x02 { // LI
		t.Errorf("LI = 0x%02X, want 0x02", result[0])
	}
	if result[1] != TypeData {
		t.Errorf("type = 0x%02X, want 0x%02X", result[1], TypeData)
	}
	if result[2] != 0x80 { // EOT=1
		t.Errorf("EOT/NR = 0x%02X, want 0x80", result[2])
	}
	if !bytes.Equal(result[3:], data) {
		t.Errorf("data = %X, want %X", result[3:], data)
	}
}

func TestEncodeDataTPDUEmpty(t *testing.T) {
	result := EncodeDataTPDU([]byte{})
	if len(result) != 3 {
		t.Fatalf("expected 3 bytes (header only), got %d", len(result))
	}
}

func TestDecodeDataTPDU(t *testing.T) {
	input := []byte{0x02, TypeData, 0x80, 0xAA, 0xBB}
	data, err := DecodeDataTPDU(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(data, []byte{0xAA, 0xBB}) {
		t.Errorf("data = %X, want AABB", data)
	}
}

func TestDecodeDataTPDUNoEOT(t *testing.T) {
	input := []byte{0x02, TypeData, 0x00, 0xCC}
	data, err := DecodeDataTPDU(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(data, []byte{0xCC}) {
		t.Errorf("data = %X, want CC", data)
	}
}

func TestDecodeDataTPDUTooShort(t *testing.T) {
	_, err := DecodeDataTPDU([]byte{0x02, TypeData})
	if err == nil {
		t.Error("expected error for too-short data TPDU")
	}
}

func TestDecodeDataTPDUWrongType(t *testing.T) {
	_, err := DecodeDataTPDU([]byte{0x02, TypeConnectionRequest, 0x80, 0x01})
	if err == nil {
		t.Error("expected error for wrong TPDU type")
	}
}

func TestDataTPDURoundTrip(t *testing.T) {
	original := []byte("test payload data for round trip")
	encoded := EncodeDataTPDU(original)
	decoded, err := DecodeDataTPDU(encoded)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Errorf("round-trip mismatch: got %q, want %q", decoded, original)
	}
}

func TestProtocolName(t *testing.T) {
	tests := []struct {
		proto uint32
		want  string
	}{
		{ProtocolRDP, "Standard RDP Security"},
		{ProtocolSSL, "TLS"},
		{ProtocolHybrid, "CredSSP (NLA)"},
		{ProtocolRDSTLS, "RDSTLS"},
		{ProtocolHybridEx, "CredSSP Extended"},
		{0x99999999, "Unknown (0x99999999)"},
	}
	for _, tt := range tests {
		got := ProtocolName(tt.proto)
		if got != tt.want {
			t.Errorf("ProtocolName(0x%08X) = %q, want %q", tt.proto, got, tt.want)
		}
	}
}

func TestFailureReason(t *testing.T) {
	tests := []struct {
		code uint32
		want string
	}{
		{FailSSLRequiredByServer, "SSL required by server"},
		{FailSSLNotAllowedByServer, "SSL not allowed by server"},
		{FailSSLCertNotOnServer, "SSL certificate not on server"},
		{FailInconsistentFlags, "Inconsistent flags"},
		{FailHybridRequiredByServer, "CredSSP (NLA) required by server"},
		{FailSSLWithUserAuthRequired, "SSL with user auth required"},
		{0xDEADBEEF, "Unknown failure (0xDEADBEEF)"},
	}
	for _, tt := range tests {
		got := FailureReason(tt.code)
		if got != tt.want {
			t.Errorf("FailureReason(0x%08X) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// A routingToken takes the place of the cookie and is written as the
// redirection spelled it; one that lacks its CRLF gets it (MS-RDPBCGR
// 2.2.1.1, 3.2.5.3.1). GNOME Remote Desktop's remote login and RD Connection
// Brokers tell the reconnected client apart by it.
func TestConnectionRequestEncodeWithRoutingToken(t *testing.T) {
	cr := &ConnectionRequest{
		Cookie:          "alice",
		SendCookie:      true,
		RequestedProtos: ProtocolSSL | ProtocolHybrid,
		RoutingToken:    "Cookie: msts=2134026432.15629.000\r\n",
	}
	data := cr.Encode()
	want := "Cookie: msts=2134026432.15629.000\r\n"
	if !bytes.Contains(data, []byte(want)) || bytes.Contains(data, []byte("mstshash")) {
		t.Errorf("encoded %q, want the routingToken and no cookie", data)
	}
	variable := data[7 : len(data)-8]
	if string(variable) != want {
		t.Errorf("variable part = %q, want %q", variable, want)
	}
	if int(data[0])+1 != len(data) || binary.LittleEndian.Uint32(data[len(data)-4:]) != ProtocolSSL|ProtocolHybrid {
		t.Errorf("length indicator %d with %d bytes, or the negotiation request moved", data[0], len(data))
	}

	data = (&ConnectionRequest{SendCookie: true, RoutingToken: "Cookie: msts=1"}).Encode()
	if variable := data[7 : len(data)-8]; string(variable) != "Cookie: msts=1\r\n" {
		t.Errorf("variable part = %q, want the token terminated with CRLF", variable)
	}
}
