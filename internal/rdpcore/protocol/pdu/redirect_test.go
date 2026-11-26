package pdu

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"
)

// redirectionPacket starts a Server Redirection Packet (MS-RDPBCGR 2.2.13.1):
// Flags, Length (zero here: the decoder takes the PDU's bounds), SessionID and
// the flags announcing the variable fields that follow.
func redirectionPacket(sessionID, flags uint32) *bytes.Buffer {
	var packet bytes.Buffer
	packet.Write(binary.LittleEndian.AppendUint16(nil, 0x0400))
	packet.Write(make([]byte, 2))
	packet.Write(binary.LittleEndian.AppendUint32(nil, sessionID))
	packet.Write(binary.LittleEndian.AppendUint32(nil, flags))
	return &packet
}

// field appends a length-prefixed variable field.
func field(packet *bytes.Buffer, data []byte) {
	packet.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(data))))
	packet.Write(data)
}

// addresses encodes a TARGET_NET_ADDRESSES array: a count, then
// length-prefixed addresses.
func addresses(list ...string) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(list)))
	for _, address := range list {
		text := text16(address)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(text)))
		out = append(out, text...)
	}
	return out
}
func text16(s string) []byte {
	var out []byte
	for _, r := range s {
		out = binary.LittleEndian.AppendUint16(out, uint16(r))
	}
	return binary.LittleEndian.AppendUint16(out, 0)
}

func TestDecodeRedirectPDU_Enhanced(t *testing.T) {
	routingToken := []byte("Cookie: msts=2134026432.15629.000\r\n")

	packet := redirectionPacket(42, LBTargetNetAddress|LBLoadBalanceInfo|LBUsername|LBDomain)
	field(packet, text16("192.168.1.50"))
	field(packet, routingToken)
	field(packet, text16("admin"))
	field(packet, text16("CORP"))

	ri, err := DecodeRedirectPDU(append([]byte{0, 0}, packet.Bytes()...), true)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}

	if ri.SessionID != 42 || ri.Flags != LBTargetNetAddress|LBLoadBalanceInfo|LBUsername|LBDomain {
		t.Errorf("session %d flags %#x, want 42 and the four field flags", ri.SessionID, ri.Flags)
	}
	if ri.Server != "192.168.1.50" {
		t.Errorf("Server = %q, want %q", ri.Server, "192.168.1.50")
	}
	if ri.Domain != "CORP" || ri.Username != "admin" {
		t.Errorf("account = %q\\%q, want CORP\\admin", ri.Domain, ri.Username)
	}
	if string(ri.LoadBalanceInfo) != string(routingToken) {
		t.Errorf("LoadBalanceInfo = %q, want the routing token as spelled", ri.LoadBalanceInfo)
	}
}

// The Standard Security variant carries the packet behind its security
// header with none of the padding (MS-RDPBCGR 2.2.13.2.1).
func TestDecodeRedirectPDU_Standard(t *testing.T) {
	packet := redirectionPacket(0, LBTargetNetAddress)
	field(packet, text16("10.0.0.1"))

	ri, err := DecodeRedirectPDU(packet.Bytes(), false)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}
	if ri.Server != "10.0.0.1" || ri.SessionID != 0 {
		t.Errorf("read %q session %d, want 10.0.0.1 and no session", ri.Server, ri.SessionID)
	}
}

func TestDecodeRedirectPDU_FQDNOverridesNetAddress(t *testing.T) {
	packet := redirectionPacket(0, LBTargetNetAddress|LBTargetFQDN)
	field(packet, text16("10.0.0.1"))
	field(packet, text16("rdsh01.corp.local"))

	ri, err := DecodeRedirectPDU(packet.Bytes(), false)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}
	if ri.Server != "rdsh01.corp.local" {
		t.Errorf("Server = %q, want the FQDN", ri.Server)
	}
}

func TestDecodeRedirectPDU_NoRedirect(t *testing.T) {
	packet := redirectionPacket(0, LBTargetNetAddress|LBNoRedirect)
	field(packet, text16("10.0.0.1"))

	ri, err := DecodeRedirectPDU(packet.Bytes(), false)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}
	if ri.Server != "" {
		t.Errorf("Server = %q, want empty for LB_NOREDIRECT", ri.Server)
	}
}

func TestDecodeRedirectPDU_Truncated(t *testing.T) {
	shortField := redirectionPacket(0, LBLoadBalanceInfo)
	shortField.Write(binary.LittleEndian.AppendUint32(nil, 10))
	shortField.Write([]byte("Cookie:"))

	oddText := redirectionPacket(0, LBTargetNetAddress)
	oddText.Write(binary.LittleEndian.AppendUint32(nil, 5))
	oddText.Write([]byte("abcde"))

	// A security header without SEC_REDIRECTION_PKT is no redirection.
	license := make([]byte, 12)
	binary.LittleEndian.PutUint16(license, 0x0080)

	for name, test := range map[string]struct {
		data     []byte
		enhanced bool
	}{
		"padding":        {[]byte{0, 0}, true},
		"header":         {[]byte{0x00, 0x04, 0x00}, false},
		"other security": {license, false},
		"field":          {append([]byte{0, 0}, shortField.Bytes()...), true},
		"odd text":       {oddText.Bytes(), false},
	} {
		if _, err := DecodeRedirectPDU(test.data, test.enhanced); err == nil {
			t.Errorf("%s: decoded % x", name, test.data)
		}
	}
}

// A redirection from an RD Connection Broker: every field of the packet, as
// FreeRDP's server and Windows lay them out.
func TestDecodeRedirectPDU_EveryField(t *testing.T) {
	cert := []byte("certificate bytes")
	container := binary.LittleEndian.AppendUint32(nil, 32)     // CERT_cert_file_element
	container = binary.LittleEndian.AppendUint32(container, 1) // ENCODING_TYPE_ASN1_DER
	container = binary.LittleEndian.AppendUint32(container, uint32(len(cert)))
	container = append(container, cert...)
	// Windows line-wraps the Base64 text.
	encoded := base64.StdEncoding.EncodeToString(container)
	wrapped := encoded[:len(encoded)/2] + "\r\n" + encoded[len(encoded)/2:] + "\r\n"

	cookie := []byte{0x02, 0x00, 0x00, 0x80, 0x44}
	packet := redirectionPacket(0x5A17C0DE, LBTargetNetAddress|LBLoadBalanceInfo|LBUsername|LBDomain|LBPassword|
		LBTargetNetBIOS|LBClientTSVURL|LBPasswordIsPKEncrypted|LBRedirectionGUID|LBTargetCertificate|LBTargetNetAddresses)
	field(packet, text16("10.0.0.1"))
	field(packet, []byte("Cookie: msts=2134026432.15629.000\r\n"))
	field(packet, text16("admin"))
	field(packet, text16("CORP"))
	field(packet, cookie)
	field(packet, text16("rdsh01"))          // TargetNetBiosName
	field(packet, []byte("https://tsv/url")) // TsvUrl
	field(packet, text16("Z3VpZA=="))        // RedirectionGuid
	field(packet, text16(wrapped))
	field(packet, addresses("10.0.0.2", "10.0.0.3"))
	packet.Write(make([]byte, 8)) // pad

	ri, err := DecodeRedirectPDU(append([]byte{0, 0}, packet.Bytes()...), true)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}
	if ri.SessionID != 0x5A17C0DE || ri.Server != "10.0.0.1" || ri.Username != "admin" || ri.Domain != "CORP" {
		t.Fatalf("session %#x, server %q, account %q\\%q", ri.SessionID, ri.Server, ri.Domain, ri.Username)
	}
	if !bytes.Equal(ri.Password, cookie) || !bytes.Equal(ri.TargetCertificate, cert) {
		t.Errorf("password % x, certificate % x", ri.Password, ri.TargetCertificate)
	}
	if len(ri.TargetAddresses) != 2 || ri.TargetAddresses[0] != "10.0.0.2" || ri.TargetAddresses[1] != "10.0.0.3" {
		t.Errorf("target addresses %q", ri.TargetAddresses)
	}
}

// When none of the names is present, the first of the target addresses is
// where the client reconnects to.
func TestDecodeRedirectPDU_TargetAddressesNameTheServer(t *testing.T) {
	packet := redirectionPacket(0, LBTargetNetAddresses)
	field(packet, addresses("10.0.0.9"))

	ri, err := DecodeRedirectPDU(packet.Bytes(), false)
	if err != nil {
		t.Fatalf("DecodeRedirectPDU: %v", err)
	}
	if ri.Server != "10.0.0.9" {
		t.Errorf("Server = %q, want the only target address", ri.Server)
	}
}
