// Package x224 implements the X.224 (ISO 8073) connection-oriented transport protocol.
// In RDP, X.224 is used for the initial connection negotiation.
//
// X.224 TPDU (Transport Protocol Data Unit) types used in RDP:
//   - Connection Request (CR): Client initiates connection
//   - Connection Confirm (CC): Server accepts connection
//   - Data (DT): Regular data transfer
//   - Disconnect Request (DR): Connection termination
//
// MS-RDPBCGR references:
//   - Section 2.2.1.1: Client X.224 Connection Request PDU
//   - Section 2.2.1.2: Server X.224 Connection Confirm PDU
package x224

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"strings"

	"motty/internal/rdpcore/util"
)

// TPDU type codes (X.224 section 13)
const (
	TypeConnectionRequest  = 0xE0 // CR TPDU
	TypeConnectionConfirm  = 0xD0 // CC TPDU
	TypeDisconnectRequest  = 0x80 // DR TPDU
	TypeData               = 0xF0 // DT TPDU
	TypeDataAcknowledgment = 0x00 // AK TPDU (not used in RDP)
	TypeError              = 0x70 // ER TPDU
)

// RDP Negotiation constants (MS-RDPBCGR 2.2.1.1.1)
const (
	// Negotiation request/response types
	TypeRDPNegReq  = 0x01 // RDP_NEG_REQ
	TypeRDPNegRsp  = 0x02 // RDP_NEG_RSP
	TypeRDPNegFail = 0x03 // RDP_NEG_FAILURE

	// Requested protocols (flags can be combined)
	ProtocolRDP      uint32 = 0x00000000 // Standard RDP Security
	ProtocolSSL      uint32 = 0x00000001 // TLS 1.0, 1.1, or 1.2
	ProtocolHybrid   uint32 = 0x00000002 // CredSSP (TLS + NLA)
	ProtocolRDSTLS   uint32 = 0x00000004 // RDSTLS
	ProtocolHybridEx uint32 = 0x00000008 // CredSSP with Early User Auth
)

// RDP_NEG_RSP flags (MS-RDPBCGR 2.2.1.2.1)
const (
	// ExtendedClientDataSupported: the server accepts the extended client data
	// blocks of the GCC Conference Create Request (2.2.1.3), among them the
	// monitor and message channel blocks.
	ExtendedClientDataSupported byte = 0x01
)

// Negotiation failure codes (MS-RDPBCGR 2.2.1.2.2)
const (
	FailSSLRequiredByServer     uint32 = 0x00000001
	FailSSLNotAllowedByServer   uint32 = 0x00000002
	FailSSLCertNotOnServer      uint32 = 0x00000003
	FailInconsistentFlags       uint32 = 0x00000004
	FailHybridRequiredByServer  uint32 = 0x00000005
	FailSSLWithUserAuthRequired uint32 = 0x00000006
)

// ConnectionRequest represents an X.224 Connection Request TPDU
type ConnectionRequest struct {
	// X.224 fields
	DestRef uint16 // Destination reference (always 0)
	SrcRef  uint16 // Source reference
	Class   byte   // Class and options (always 0 for RDP)

	// RDP Negotiation Request (optional)
	Cookie           string // Cookie field: "Cookie: mstshash=" + cookie (MS-RDPBCGR 2.2.1.1)
	SendCookie       bool   // Always send Cookie header, even if Cookie is empty
	NegotiationFlags byte   // Request flags
	RequestedProtos  uint32 // Requested security protocols

	// RoutingToken is the complete routingToken field of a reconnection: the
	// LoadBalanceInfo of a Server Redirection PDU, ending in CRLF. It takes
	// the place of the Cookie field, which the two exclude (MS-RDPBCGR
	// 3.2.5.3.1).
	RoutingToken string
}

// ConnectionConfirm represents an X.224 Connection Confirm TPDU
type ConnectionConfirm struct {
	// X.224 fields
	DestRef uint16 // Destination reference
	SrcRef  uint16 // Source reference
	Class   byte   // Class and options

	// RDP Negotiation Response (optional)
	Type          byte   // RDP_NEG_RSP or RDP_NEG_FAILURE
	Flags         byte   // Response flags
	SelectedProto uint32 // Selected protocol (if success)
	FailureCode   uint32 // Failure reason (if failure)
}

// DataTPDU represents an X.224 Data TPDU
type DataTPDU struct {
	EOT  bool   // End of TSDU flag
	Data []byte // User data
}

// Encode serializes a Connection Request to bytes.
// The RDP Negotiation Request (RDP_NEG_REQ) is always included per
// MS-RDPBCGR recommendation, even when requesting Standard RDP Security
// (requestedProtocols = 0).
func (cr *ConnectionRequest) Encode() []byte {
	// Calculate variable data size up front
	varLen := 8 // RDP_NEG_REQ is always 8 bytes
	token := cr.RoutingToken
	if token != "" && !strings.HasSuffix(token, "\r\n") {
		token += "\r\n" // the routingToken field ends in CRLF
	}
	// A routing token and a cookie are mutually exclusive (MS-RDPBCGR 3.2.5.3.1)
	sendCookie := token == "" && (cr.SendCookie || cr.Cookie != "")
	if token != "" {
		varLen += len(token)
	}
	if sendCookie {
		varLen += 17 + len(cr.Cookie) + 2 // "Cookie: mstshash=" + cookie + "\r\n"
	}

	// Fixed header: LI(1) + type(1) + destRef(2) + srcRef(2) + class(1) = 7 bytes
	totalLen := 7 + varLen
	buf := make([]byte, totalLen)

	// LI = 6 + varLen (LI doesn't count itself)
	buf[0] = byte(6 + varLen)
	buf[1] = TypeConnectionRequest
	binary.BigEndian.PutUint16(buf[2:4], cr.DestRef)
	binary.BigEndian.PutUint16(buf[4:6], cr.SrcRef)
	buf[6] = cr.Class

	// Variable part
	offset := 7
	if token != "" {
		offset += copy(buf[offset:], token)
	}
	if sendCookie {
		offset += copy(buf[offset:], "Cookie: mstshash=")
		offset += copy(buf[offset:], cr.Cookie)
		buf[offset] = '\r'
		buf[offset+1] = '\n'
		offset += 2
	}

	// RDP_NEG_REQ
	buf[offset] = TypeRDPNegReq
	buf[offset+1] = cr.NegotiationFlags
	binary.LittleEndian.PutUint16(buf[offset+2:], 8)
	binary.LittleEndian.PutUint32(buf[offset+4:], cr.RequestedProtos)

	return buf
}

// DecodeConnectionConfirm parses a Connection Confirm TPDU from bytes
func DecodeConnectionConfirm(log *slog.Logger, data []byte) (*ConnectionConfirm, error) {
	if len(data) < 7 {
		return nil, fmt.Errorf("connection confirm too short: %d bytes", len(data))
	}

	cc := &ConnectionConfirm{}

	// Parse X.224 CC TPDU header
	li := data[0]       // Length indicator
	typeCode := data[1] // Should be 0xD0 (CC)

	if typeCode&0xF0 != TypeConnectionConfirm {
		return nil, fmt.Errorf("expected Connection Confirm (0xD0), got 0x%02X", typeCode)
	}

	cc.DestRef = binary.BigEndian.Uint16(data[2:4])
	cc.SrcRef = binary.BigEndian.Uint16(data[4:6])
	cc.Class = data[6]

	// Check for RDP Negotiation Response (after the 7-byte fixed header).
	// LI includes both fixed fields and negotiation data, so we check
	// whether enough bytes exist at offset 7 rather than at offset li.
	_ = li
	const negStart = 7
	if negStart+8 <= len(data) {
		cc.Type = data[negStart]
		cc.Flags = data[negStart+1]
		// Skip length field (2 bytes)

		if cc.Type == TypeRDPNegRsp {
			cc.SelectedProto = binary.LittleEndian.Uint32(data[negStart+4 : negStart+8])
		} else if cc.Type == TypeRDPNegFail {
			cc.FailureCode = binary.LittleEndian.Uint32(data[negStart+4 : negStart+8])
		}
	}

	log.LogAttrs(context.Background(), slog.LevelDebug, "X.224 Connection Confirm", util.Hex2("type", cc.Type), util.Hex2("flags", cc.Flags), util.Hex8("selectedProto", cc.SelectedProto), util.Hex8("failureCode", cc.FailureCode))
	return cc, nil
}

// EncodeDataTPDU creates an X.224 Data TPDU wrapper for user data
func EncodeDataTPDU(data []byte) []byte {
	// DT TPDU header: LI (1 byte) + Type (1 byte) + EOT/NR (1 byte)
	// For RDP, we always use EOT=1, NR=0
	result := make([]byte, 3+len(data))
	result[0] = 0x02     // Length indicator (2 bytes follow in header)
	result[1] = TypeData // DT TPDU (0xF0)
	result[2] = 0x80     // EOT = 1, NR = 0
	copy(result[3:], data)
	return result
}

// DecodeDataTPDU extracts user data from an X.224 Data TPDU.
// Returns the user data payload directly (no allocation).
func DecodeDataTPDU(data []byte) ([]byte, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("data TPDU too short: %d bytes", len(data))
	}

	li := data[0]
	typeCode := data[1]

	if typeCode != TypeData {
		return nil, fmt.Errorf("expected Data TPDU (0xF0), got 0x%02X", typeCode)
	}

	offset := int(li) + 1
	if offset > len(data) {
		return nil, fmt.Errorf("data TPDU LI %d exceeds packet length %d", li, len(data))
	}

	return data[offset:], nil
}

// ProtocolName returns a human-readable name for a protocol flag
func ProtocolName(proto uint32) string {
	switch proto {
	case ProtocolRDP:
		return "Standard RDP Security"
	case ProtocolSSL:
		return "TLS"
	case ProtocolHybrid:
		return "CredSSP (NLA)"
	case ProtocolRDSTLS:
		return "RDSTLS"
	case ProtocolHybridEx:
		return "CredSSP Extended"
	default:
		return fmt.Sprintf("Unknown (0x%08X)", proto)
	}
}

// FailureReason returns a human-readable reason for negotiation failure
func FailureReason(code uint32) string {
	switch code {
	case FailSSLRequiredByServer:
		return "SSL required by server"
	case FailSSLNotAllowedByServer:
		return "SSL not allowed by server"
	case FailSSLCertNotOnServer:
		return "SSL certificate not on server"
	case FailInconsistentFlags:
		return "Inconsistent flags"
	case FailHybridRequiredByServer:
		return "CredSSP (NLA) required by server"
	case FailSSLWithUserAuthRequired:
		return "SSL with user auth required"
	default:
		return fmt.Sprintf("Unknown failure (0x%08X)", code)
	}
}
