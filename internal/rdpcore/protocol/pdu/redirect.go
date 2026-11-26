// Server Redirection Packet decoding (MS-RDPBCGR 2.2.13.1).
package pdu

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Share Control PDU types of the Server Redirection PDUs (MS-RDPBCGR
// 2.2.13.2, 2.2.13.3.1).
const (
	TypeRedirect         uint16 = 0x0015 // PDUTYPE_SERVER_REDIR_PDU (5), version 0x0010
	TypeEnhancedRedirect uint16 = 0x001A // PDUTYPE_SERVER_REDIR_PDU (10), version 0x0010
)

// Redirection flags (MS-RDPBCGR 2.2.13.1).
const (
	LBTargetNetAddress      uint32 = 0x00000001
	LBLoadBalanceInfo       uint32 = 0x00000002
	LBUsername              uint32 = 0x00000004
	LBDomain                uint32 = 0x00000008
	LBPassword              uint32 = 0x00000010
	LBDontStoreUsername     uint32 = 0x00000020
	LBSmartcardLogon        uint32 = 0x00000040
	LBNoRedirect            uint32 = 0x00000080
	LBTargetFQDN            uint32 = 0x00000100
	LBTargetNetBIOS         uint32 = 0x00000200
	LBTargetNetAddresses    uint32 = 0x00000800
	LBClientTSVURL          uint32 = 0x00001000
	LBServerTSVCapable      uint32 = 0x00002000
	LBPasswordIsPKEncrypted uint32 = 0x00004000
	LBRedirectionGUID       uint32 = 0x00008000
	LBTargetCertificate     uint32 = 0x00010000
)

// secRedirectionPkt is the value of the packet's Flags field and of the
// security header flag that fronts the Standard Security variant.
const secRedirectionPkt uint16 = 0x0400

// RedirectInfo holds the parsed Server Redirection Packet.
type RedirectInfo struct {
	SessionID uint32 // session the client must reconnect to
	Flags     uint32 // LB_* flags saying which fields the server supplied

	// Server is where to reconnect: LB_TARGET_NET_ADDRESS, LB_TARGET_FQDN or
	// LB_TARGET_NETBIOS_NAME, in that order of preference. It is empty for a
	// routing token redirection, which leads back to the redirecting server.
	Server          string
	TargetAddresses []string // LB_TARGET_NET_ADDRESSES, in the server's order

	Username string
	Domain   string
	Password []byte // cleartext password or opaque cookie; see LB_PASSWORD

	// LoadBalanceInfo is the routingToken of the next X.224 Connection Request,
	// as the server spelled it.
	LoadBalanceInfo []byte

	// TargetCertificate is the DER certificate of the server to reconnect to
	// (LB_TARGET_CERTIFICATE); nil when the server supplied none.
	TargetCertificate []byte
}

// DecodeRedirectPDU parses a Server Redirection Packet.
//
// data starts after the Share Control Header. enhanced selects the Enhanced
// Security Server Redirection PDU, whose packet follows two padding octets;
// the Standard Security variant carries the packet directly (MS-RDPBCGR
// 2.2.13.2.1, 2.2.13.3.1).
func DecodeRedirectPDU(data []byte, enhanced bool) (*RedirectInfo, error) {
	if enhanced {
		if len(data) < 2 {
			return nil, fmt.Errorf("redirection packet too short for padding")
		}
		data = data[2:]
	}

	// Flags (2), Length (2) and SessionID (4) precede RedirFlags. Length
	// describes the whole packet, which the PDU boundaries already give.
	if len(data) < 12 {
		return nil, fmt.Errorf("redirection packet too short for header")
	}
	if flags := binary.LittleEndian.Uint16(data[0:2]); flags != secRedirectionPkt {
		return nil, fmt.Errorf("redirection packet flags 0x%04X, want 0x%04X", flags, secRedirectionPkt)
	}

	ri := &RedirectInfo{
		SessionID: binary.LittleEndian.Uint32(data[4:8]),
		Flags:     binary.LittleEndian.Uint32(data[8:12]),
	}

	off := 12
	if ri.Flags&LBTargetNetAddress != 0 {
		s, n, err := readRedirectUTF16(data, off)
		if err != nil {
			return nil, fmt.Errorf("TargetNetAddress: %w", err)
		}
		ri.Server = s
		off += n
	}

	if ri.Flags&LBLoadBalanceInfo != 0 {
		b, n, err := readRedirectBlob(data, off)
		if err != nil {
			return nil, fmt.Errorf("LoadBalanceInfo: %w", err)
		}
		ri.LoadBalanceInfo = b
		off += n
	}

	if ri.Flags&LBUsername != 0 {
		s, n, err := readRedirectUTF16(data, off)
		if err != nil {
			return nil, fmt.Errorf("Username: %w", err)
		}
		ri.Username = s
		off += n
	}

	if ri.Flags&LBDomain != 0 {
		s, n, err := readRedirectUTF16(data, off)
		if err != nil {
			return nil, fmt.Errorf("Domain: %w", err)
		}
		ri.Domain = s
		off += n
	}

	if ri.Flags&LBPassword != 0 {
		b, n, err := readRedirectBlob(data, off)
		if err != nil {
			return nil, fmt.Errorf("Password: %w", err)
		}
		ri.Password = b
		off += n
	}

	if ri.Flags&LBTargetFQDN != 0 {
		s, n, err := readRedirectUTF16(data, off)
		if err != nil {
			return nil, fmt.Errorf("TargetFQDN: %w", err)
		}
		// A name says more than the address it resolved to on the server side.
		ri.Server = s
		off += n
	}

	if ri.Flags&LBTargetNetBIOS != 0 {
		s, n, err := readRedirectUTF16(data, off)
		if err != nil {
			return nil, fmt.Errorf("TargetNetBiosName: %w", err)
		}
		if ri.Server == "" {
			ri.Server = s
		}
		off += n
	}

	if ri.Flags&LBClientTSVURL != 0 {
		// The URL is checked against a TsvUrl the client would have sent with
		// its load balance information, which this client never does.
		_, n, err := readRedirectBlob(data, off)
		if err != nil {
			return nil, fmt.Errorf("TsvUrl: %w", err)
		}
		off += n
	}

	if ri.Flags&LBRedirectionGUID != 0 {
		_, n, err := readRedirectBlob(data, off)
		if err != nil {
			return nil, fmt.Errorf("RedirectionGuid: %w", err)
		}
		off += n
	}

	if ri.Flags&LBTargetCertificate != 0 {
		cert, n, err := readRedirectCertificate(data, off)
		if err != nil {
			return nil, fmt.Errorf("TargetCertificate: %w", err)
		}
		ri.TargetCertificate = cert
		off += n
	}

	if ri.Flags&LBTargetNetAddresses != 0 {
		addresses, n, err := readRedirectAddresses(data, off)
		if err != nil {
			return nil, fmt.Errorf("TargetNetAddresses: %w", err)
		}
		ri.TargetAddresses = addresses
		if ri.Server == "" && len(addresses) > 0 {
			// The list is the target when the server named no single one.
			ri.Server = addresses[0]
		}
		off += n
	}

	if ri.Flags&LBNoRedirect != 0 {
		ri.Server = ""
	}

	return ri, nil
}

// readRedirectBlob reads a length(u32)-prefixed opaque blob.
// It returns the data and the bytes it takes in the packet (4 + length).
func readRedirectBlob(data []byte, off int) ([]byte, int, error) {
	if off < 0 || off+4 > len(data) || off+4 < 0 {
		return nil, 0, fmt.Errorf("truncated at length")
	}
	blobLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	if blobLen < 0 || off+blobLen > len(data) {
		return nil, 0, fmt.Errorf("truncated at data (need %d, have %d)", blobLen, len(data)-off)
	}
	b := make([]byte, blobLen)
	copy(b, data[off:off+blobLen])
	return b, 4 + blobLen, nil
}

// readRedirectUTF16 reads a length(u32)-prefixed null-terminated UTF-16LE
// string. It returns the Go string and the bytes it takes (4 + length).
func readRedirectUTF16(data []byte, off int) (string, int, error) {
	if off < 0 || off+4 > len(data) || off+4 < 0 {
		return "", 0, fmt.Errorf("truncated at length")
	}
	strLen := int(binary.LittleEndian.Uint32(data[off : off+4]))
	off += 4
	if strLen < 0 || off+strLen > len(data) || strLen%2 != 0 {
		return "", 0, fmt.Errorf("truncated or odd-length data (need %d, have %d)", strLen, len(data)-off)
	}
	return decodeUTF16(data[off : off+strLen]), 4 + strLen, nil
}

// Certificate container element types and encodings (MS-RDPBCGR 2.2.13.1.2).
const (
	certElementCertificate uint32 = 32
	certEncodingDER        uint32 = 1
)

// readRedirectCertificate reads LB_TARGET_CERTIFICATE: a length-prefixed
// Base64 text in UTF-16LE that encodes the certificate container elements of
// MS-RDPBCGR 2.2.13.1.2. It returns the DER certificate of the first
// certificate element.
func readRedirectCertificate(data []byte, off int) ([]byte, int, error) {
	text, n, err := readRedirectUTF16(data, off)
	if err != nil {
		return nil, 0, err
	}

	// The text is line-wrapped; each line decodes on its own (FreeRDP decodes
	// the tokens it splits at CR and LF). Ignore the newlines and decode at
	// once, since the Base64 alphabet has neither.
	encoded := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, text)
	container, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, 0, fmt.Errorf("Base64: %w", err)
	}

	for pos := 0; pos+12 <= len(container); {
		elementType := binary.LittleEndian.Uint32(container[pos:])
		encoding := binary.LittleEndian.Uint32(container[pos+4:])
		length := int(binary.LittleEndian.Uint32(container[pos+8:]))
		pos += 12
		if pos+length > len(container) {
			break
		}
		if elementType == certElementCertificate && encoding == certEncodingDER && length > 0 {
			der := make([]byte, length)
			copy(der, container[pos:pos+length])
			return der, n, nil
		}
		pos += length
	}
	return nil, 0, fmt.Errorf("no DER certificate element")
}

// readRedirectAddresses reads LB_TARGET_NET_ADDRESSES: a count followed by
// that many length-prefixed UTF-16LE addresses.
func readRedirectAddresses(data []byte, off int) ([]string, int, error) {
	blob, n, err := readRedirectBlob(data, off)
	if err != nil {
		return nil, 0, err
	}
	if len(blob) < 4 {
		return nil, 0, fmt.Errorf("truncated at count")
	}
	count := int(binary.LittleEndian.Uint32(blob))
	blob = blob[4:]
	// Each address takes at least its four-byte length.
	if count < 0 || count > len(blob)/4 {
		return nil, 0, fmt.Errorf("address count %d exceeds %d bytes", count, len(blob))
	}
	addresses := make([]string, 0, count)
	for range count {
		address, used, err := readRedirectUTF16(blob, 0)
		if err != nil {
			return nil, 0, err
		}
		addresses = append(addresses, address)
		blob = blob[used:]
	}
	return addresses, n, nil
}

// decodeUTF16 converts a null-terminated UTF-16LE byte slice to a Go string.
func decodeUTF16(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	n := len(b) / 2
	u16s := make([]uint16, n)
	for i := range n {
		u16s[i] = binary.LittleEndian.Uint16(b[i*2 : i*2+2])
	}
	// Strip null terminator
	if len(u16s) > 0 && u16s[len(u16s)-1] == 0 {
		u16s = u16s[:len(u16s)-1]
	}
	return string(utf16.Decode(u16s))
}
