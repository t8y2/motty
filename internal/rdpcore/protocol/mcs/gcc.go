// Package mcs implements the Multipoint Communication Service (T.125) protocol
// and GCC (Generic Conference Control, T.124) conference structures used in RDP.
//
// This file contains GCC client and server data block types used within
// MCS Connect Initial/Response PDUs.
package mcs

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"unicode/utf16"

	"motty/internal/rdpcore/util"
)

// GCC data block types — client to server
const (
	ClientCoreDataType     uint16 = 0xC001
	ClientSecurityDataType uint16 = 0xC002
	ClientNetworkDataType  uint16 = 0xC003
	ClientClusterDataType  uint16 = 0xC004
	ClientMonitorDataType  uint16 = 0xC005
)

// GCC data block types — server to client
const (
	ServerCoreDataType     uint16 = 0x0C01
	ServerSecurityDataType uint16 = 0x0C02
	ServerNetworkDataType  uint16 = 0x0C03
)

// RDP version constants
const (
	VersionRDP4  uint32 = 0x00080001
	VersionRDP5  uint32 = 0x00080004
	VersionRDP10 uint32 = 0x00080005
	VersionRDP61 uint32 = 0x00080006
)

// Color depth constants
const (
	ColorDepthRNS_UD_COLOR_4BPP  uint16 = 0xCA00
	ColorDepthRNS_UD_COLOR_8BPP  uint16 = 0xCA01
	ColorDepthRNS_UD_COLOR_16BPP uint16 = 0xCA02
	ColorDepthRNS_UD_COLOR_24BPP uint16 = 0xCA03
)

// High color depth constants
const (
	HighColor4BPP  uint16 = 0x0004
	HighColor8BPP  uint16 = 0x0008
	HighColor15BPP uint16 = 0x000F
	HighColor16BPP uint16 = 0x0010
	HighColor24BPP uint16 = 0x0018
)

// Supported color depth flags
const (
	SupportColor24BPP uint16 = 0x0001
	SupportColor16BPP uint16 = 0x0002
	SupportColor15BPP uint16 = 0x0004
	SupportColor32BPP uint16 = 0x0008
)

// Early capability flags
const (
	EarlyCapSupportErrInfoPDU        uint16 = 0x0001
	EarlyCapWant32BPPSession         uint16 = 0x0002
	EarlyCapSupportStatusInfoPDU     uint16 = 0x0004
	EarlyCapStrongAsymmetricKeys     uint16 = 0x0008
	EarlyCapValidConnectionType      uint16 = 0x0020
	EarlyCapSupportMonitorLayout     uint16 = 0x0040
	EarlyCapSupportNetCharAutodetect uint16 = 0x0080
	EarlyCapSupportDynvcGfxProtocol  uint16 = 0x0100
)

// Keyboard types
const (
	KeyboardIBMPC     uint32 = 0x00000001
	KeyboardIBMAT     uint32 = 0x00000002
	KeyboardJapan     uint32 = 0x00000003
	KeyboardEnhanced  uint32 = 0x00000004
	KeyboardNokia1050 uint32 = 0x00000005
	KeyboardNokia9140 uint32 = 0x00000006
	KeyboardJapanese  uint32 = 0x00000007
)

// Connection type constants
const (
	ConnectionTypeModem      uint8 = 0x01
	ConnectionTypeBroadLow   uint8 = 0x02
	ConnectionTypeSatellite  uint8 = 0x03
	ConnectionTypeBroadHigh  uint8 = 0x04
	ConnectionTypeWAN        uint8 = 0x05
	ConnectionTypeLAN        uint8 = 0x06
	ConnectionTypeAutoDetect uint8 = 0x07
)

// SAS (Secure Attention Sequence) constant
const SASSequenceRDP uint16 = 0xAA03

// Channel option flags
const (
	ChannelOptionInitialized  uint32 = 0x80000000
	ChannelOptionEncryptRDP   uint32 = 0x40000000
	ChannelOptionEncryptSC    uint32 = 0x20000000
	ChannelOptionEncryptCS    uint32 = 0x10000000
	ChannelOptionPriHigh      uint32 = 0x08000000
	ChannelOptionPriMed       uint32 = 0x04000000
	ChannelOptionPriLow       uint32 = 0x02000000
	ChannelOptionCompressRDP  uint32 = 0x00800000
	ChannelOptionCompress     uint32 = 0x00400000
	ChannelOptionShowProtocol uint32 = 0x00200000
)

// ClientCoreData field sizes (bytes)
const (
	clientCoreFixedSize = 4 + 2 + 2 + 2 + 2 + 4 + 4 + // version..clientBuild
		32 + // clientName
		4 + 4 + 4 + // keyboard fields
		64 + // imeFileName
		2 + 2 + 4 + 2 + 2 + 2 + // postBeta2..earlyCapFlags
		64 + // clientDigProductId
		1 + 1 + 4 // connectionType, pad, serverSelectedProtocol
	// = 216 bytes

	// Optional DPI fields after serverSelectedProtocol (RDP 10.0+, MS-RDPBCGR 2.2.1.3.2)
	clientCoreOptionalDPISize = 4 + 4 + 2 + 4 + 4 // physW + physH + orientation + desktopScale + deviceScale = 18 bytes
)

// ClientCoreData represents the TS_UD_CS_CORE client data block.
type ClientCoreData struct {
	Version                uint32
	DesktopWidth           uint16
	DesktopHeight          uint16
	ColorDepth             uint16
	SASSequence            uint16
	KeyboardLayout         uint32
	ClientBuild            uint32
	ClientName             string // max 15 chars, encoded as UTF-16LE (32 bytes)
	KeyboardType           uint32
	KeyboardSubType        uint32
	KeyboardFunctionKey    uint32
	ImeFileName            string // 64 bytes UTF-16LE
	PostBeta2ColorDepth    uint16
	ClientProductID        uint16
	SerialNumber           uint32
	HighColorDepth         uint16
	SupportedColorDepths   uint16
	EarlyCapabilityFlags   uint16
	ClientDigProductID     string // 64 bytes UTF-16LE
	ConnectionType         uint8
	Pad1Octet              uint8
	ServerSelectedProtocol uint32

	// Optional DPI fields (RDP 10.0+, MS-RDPBCGR 2.2.1.3.2).
	// Set DesktopScaleFactor > 0 to include these in the encoded block.
	DesktopPhysicalWidth  uint32 // Physical width in mm
	DesktopPhysicalHeight uint32 // Physical height in mm
	DesktopOrientation    uint16 // 0, 90, 180, or 270
	DesktopScaleFactor    uint32 // 100–500 percent
	DeviceScaleFactor     uint32 // 100, 140, or 180
}

// Encode serializes ClientCoreData to bytes (without the block header).
func (c *ClientCoreData) Encode() []byte {
	size := clientCoreFixedSize
	if c.DesktopScaleFactor > 0 {
		size += clientCoreOptionalDPISize
	}
	buf := make([]byte, size)
	off := 0

	binary.LittleEndian.PutUint32(buf[off:], c.Version)
	off += 4
	binary.LittleEndian.PutUint16(buf[off:], c.DesktopWidth)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.DesktopHeight)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.ColorDepth)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.SASSequence)
	off += 2
	binary.LittleEndian.PutUint32(buf[off:], c.KeyboardLayout)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], c.ClientBuild)
	off += 4

	// ClientName: 32 bytes UTF-16LE, null-terminated
	putUTF16LEFixedLen(buf[off:off+32], c.ClientName)
	off += 32

	binary.LittleEndian.PutUint32(buf[off:], c.KeyboardType)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], c.KeyboardSubType)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], c.KeyboardFunctionKey)
	off += 4

	// imeFileName: 64 bytes UTF-16LE
	putUTF16LEFixedLen(buf[off:off+64], c.ImeFileName)
	off += 64

	binary.LittleEndian.PutUint16(buf[off:], c.PostBeta2ColorDepth)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.ClientProductID)
	off += 2
	binary.LittleEndian.PutUint32(buf[off:], c.SerialNumber)
	off += 4
	binary.LittleEndian.PutUint16(buf[off:], c.HighColorDepth)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.SupportedColorDepths)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], c.EarlyCapabilityFlags)
	off += 2

	// clientDigProductId: 64 bytes UTF-16LE
	putUTF16LEFixedLen(buf[off:off+64], c.ClientDigProductID)
	off += 64

	buf[off] = c.ConnectionType
	off++
	buf[off] = c.Pad1Octet
	off++
	binary.LittleEndian.PutUint32(buf[off:], c.ServerSelectedProtocol)
	off += 4

	// Optional DPI fields (RDP 10.0+)
	if c.DesktopScaleFactor > 0 {
		binary.LittleEndian.PutUint32(buf[off:], c.DesktopPhysicalWidth)
		off += 4
		binary.LittleEndian.PutUint32(buf[off:], c.DesktopPhysicalHeight)
		off += 4
		binary.LittleEndian.PutUint16(buf[off:], c.DesktopOrientation)
		off += 2
		binary.LittleEndian.PutUint32(buf[off:], c.DesktopScaleFactor)
		off += 4
		binary.LittleEndian.PutUint32(buf[off:], c.DeviceScaleFactor)
	}

	return buf
}

// EncodeBlock serializes ClientCoreData with type+length header.
func (c *ClientCoreData) EncodeBlock() []byte {
	data := c.Encode()
	return writeBlockHeader(ClientCoreDataType, data)
}

// ClientSecurityData represents the TS_UD_CS_SEC client security data block.
type ClientSecurityData struct {
	EncryptionMethods    uint32
	ExtEncryptionMethods uint32
}

// Encode serializes ClientSecurityData to bytes (without header).
func (c *ClientSecurityData) Encode() []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], c.EncryptionMethods)
	binary.LittleEndian.PutUint32(buf[4:8], c.ExtEncryptionMethods)
	return buf
}

// EncodeBlock serializes ClientSecurityData with type+length header.
func (c *ClientSecurityData) EncodeBlock() []byte {
	data := c.Encode()
	return writeBlockHeader(ClientSecurityDataType, data)
}

// Cluster redirection flags (MS-RDPBCGR 2.2.1.3.6)
const (
	RedirectionSupported          uint32 = 0x00000001
	RedirectedSessionIDFieldValid uint32 = 0x00000002
	RedirectionVersion5           uint32 = 0x04 // shifted left 2 in flags field
)

// ClientClusterData represents the TS_UD_CS_CLUSTER client data block.
type ClientClusterData struct {
	Flags               uint32
	RedirectedSessionID uint32
}

// Encode serializes ClientClusterData to bytes (without header).
func (c *ClientClusterData) Encode() []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], c.Flags)
	binary.LittleEndian.PutUint32(buf[4:8], c.RedirectedSessionID)
	return buf
}

// EncodeBlock serializes ClientClusterData with type+length header.
func (c *ClientClusterData) EncodeBlock() []byte {
	data := c.Encode()
	return writeBlockHeader(ClientClusterDataType, data)
}

// MonitorPrimary is the flag indicating the primary monitor in MonitorDef.
const MonitorPrimary uint32 = 0x00000001

// MonitorDef represents a single monitor in TS_MONITOR_DEF (MS-RDPBCGR 2.2.1.3.6).
type MonitorDef struct {
	Left, Top, Right, Bottom int32
	Flags                    uint32 // MonitorPrimary = 0x01
}

// ClientMonitorData represents the TS_UD_CS_MONITOR client data block (MS-RDPBCGR 2.2.1.3.6).
type ClientMonitorData struct {
	Monitors []MonitorDef
}

// Encode serializes ClientMonitorData to bytes (without the block header).
// Wire format: flags(u32) + monitorCount(u32) + monitorDefArray[](20 bytes each)
func (c *ClientMonitorData) Encode() []byte {
	n := len(c.Monitors)
	buf := make([]byte, 8+n*20)
	encodeMonitorData(buf, c.Monitors)
	return buf
}

// EncodeBlock serializes ClientMonitorData with type+length header.
// Single allocation: header(4) + flags(4) + count(4) + monitors(20*n).
func (c *ClientMonitorData) EncodeBlock() []byte {
	n := len(c.Monitors)
	totalLen := 4 + 8 + n*20 // header + flags + count + monitors
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint16(buf[0:2], ClientMonitorDataType)
	binary.LittleEndian.PutUint16(buf[2:4], uint16(totalLen))
	encodeMonitorData(buf[4:], c.Monitors)
	return buf
}

// encodeMonitorData writes the monitor data payload into dst.
func encodeMonitorData(dst []byte, monitors []MonitorDef) {
	// flags = 0 (dst already zeroed)
	binary.LittleEndian.PutUint32(dst[4:8], uint32(len(monitors)))
	off := 8
	for i := range monitors {
		m := &monitors[i]
		binary.LittleEndian.PutUint32(dst[off:], uint32(m.Left))
		binary.LittleEndian.PutUint32(dst[off+4:], uint32(m.Top))
		binary.LittleEndian.PutUint32(dst[off+8:], uint32(m.Right))
		binary.LittleEndian.PutUint32(dst[off+12:], uint32(m.Bottom))
		binary.LittleEndian.PutUint32(dst[off+16:], m.Flags)
		off += 20
	}
}

// ChannelDef represents a channel definition in the network data.
type ChannelDef struct {
	Name    string // 8 bytes, null-padded ASCII
	Options uint32
}

// ClientNetworkData represents the TS_UD_CS_NET client network data block.
type ClientNetworkData struct {
	Channels []ChannelDef
}

// Encode serializes ClientNetworkData to bytes (without header).
func (c *ClientNetworkData) Encode() []byte {
	// 4 bytes count + 12 bytes per channel (8 name + 4 options)
	buf := make([]byte, 4+len(c.Channels)*12)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(c.Channels)))
	off := 4
	for _, ch := range c.Channels {
		// Channel name: 8 bytes, null-padded
		copy(buf[off:off+8], ch.Name)
		off += 8
		binary.LittleEndian.PutUint32(buf[off:], ch.Options)
		off += 4
	}
	return buf
}

// EncodeBlock serializes ClientNetworkData with type+length header.
func (c *ClientNetworkData) EncodeBlock() []byte {
	data := c.Encode()
	return writeBlockHeader(ClientNetworkDataType, data)
}

// ServerCoreData represents the TS_UD_SC_CORE server core data block.
type ServerCoreData struct {
	Version              uint32
	ClientRequestedProto uint32
	EarlyCapabilityFlags uint32
}

// ServerSecurityData represents the TS_UD_SC_SEC server security data block.
type ServerSecurityData struct {
	EncryptionMethod uint32
	EncryptionLevel  uint32
	RawData          []byte // Rest of the data (random, certificate, etc.)
}

// ServerNetworkData represents the TS_UD_SC_NET server network data block.
type ServerNetworkData struct {
	IOChannelID uint16
	ChannelIDs  []uint16
	// MessageChannelID is the separately negotiated TS_UD_SC_MCS_MSGCHANNEL.
	MessageChannelID uint16
}

// DecodeServerData parses GCC server data blocks from the Conference Create Response userData.
// Returns server core, security, and network data blocks.
func DecodeServerData(log *slog.Logger, data []byte) (*ServerCoreData, *ServerSecurityData, *ServerNetworkData, error) {
	var core *ServerCoreData
	var sec *ServerSecurityData
	var net *ServerNetworkData
	var messageChannelID uint16

	offset := 0
	for offset+4 <= len(data) {
		blockType := binary.LittleEndian.Uint16(data[offset : offset+2])
		blockLen := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))

		if blockLen < 4 || offset+blockLen > len(data) {
			if blockType == 0x0C04 {
				return core, sec, net, fmt.Errorf("truncated server message channel data")
			}
			break
		}

		blockData := data[offset+4 : offset+blockLen]

		switch blockType {
		case ServerCoreDataType:
			core = decodeServerCoreData(blockData)
		case ServerSecurityDataType:
			sec = decodeServerSecurityData(blockData)
		case ServerNetworkDataType:
			net = decodeServerNetworkData(blockData)
		case 0x0C04: // TS_UD_SC_MCS_MSGCHANNEL
			if len(blockData) != 2 {
				return core, sec, net, fmt.Errorf("invalid server message channel data length: %d", len(blockData))
			}
			messageChannelID = binary.LittleEndian.Uint16(blockData)
		}

		offset += blockLen
	}

	if core != nil {
		log.LogAttrs(context.Background(), slog.LevelDebug, "GCC server core", util.Hex8("version", core.Version), util.Hex8("earlyCapFlags", core.EarlyCapabilityFlags))
	}
	if sec != nil {
		log.LogAttrs(context.Background(), slog.LevelDebug, "GCC server security", util.Hex8("encMethod", sec.EncryptionMethod), util.Hex8("encLevel", sec.EncryptionLevel))
	}
	if net != nil {
		log.LogAttrs(context.Background(), slog.LevelDebug, "GCC server network", slog.Int("ioChannel", int(net.IOChannelID)), slog.Int("channels", len(net.ChannelIDs)))
	}
	if net == nil {
		return core, sec, nil, fmt.Errorf("server network data not found in response")
	}
	net.MessageChannelID = messageChannelID
	if messageChannelID != 0 {
		if messageChannelID == net.IOChannelID {
			return core, sec, net, fmt.Errorf("server message channel overlaps the I/O channel")
		}
		for _, channelID := range net.ChannelIDs {
			if channelID == messageChannelID {
				return core, sec, net, fmt.Errorf("server message channel overlaps a static channel")
			}
		}
	}

	return core, sec, net, nil
}

func decodeServerCoreData(data []byte) *ServerCoreData {
	sc := &ServerCoreData{}
	if len(data) >= 4 {
		sc.Version = binary.LittleEndian.Uint32(data[0:4])
	}
	if len(data) >= 8 {
		sc.ClientRequestedProto = binary.LittleEndian.Uint32(data[4:8])
	}
	if len(data) >= 12 {
		sc.EarlyCapabilityFlags = binary.LittleEndian.Uint32(data[8:12])
	}
	return sc
}

func decodeServerSecurityData(data []byte) *ServerSecurityData {
	ss := &ServerSecurityData{}
	if len(data) >= 4 {
		ss.EncryptionMethod = binary.LittleEndian.Uint32(data[0:4])
	}
	if len(data) >= 8 {
		ss.EncryptionLevel = binary.LittleEndian.Uint32(data[4:8])
	}
	if len(data) > 8 {
		ss.RawData = make([]byte, len(data)-8)
		copy(ss.RawData, data[8:])
	}
	return ss
}

func decodeServerNetworkData(data []byte) *ServerNetworkData {
	sn := &ServerNetworkData{}
	if len(data) < 4 {
		return sn
	}
	sn.IOChannelID = binary.LittleEndian.Uint16(data[0:2])
	channelCount := int(binary.LittleEndian.Uint16(data[2:4]))

	if channelCount > 0 {
		sn.ChannelIDs = make([]uint16, 0, channelCount)
	}
	offset := 4
	for i := 0; i < channelCount && offset+2 <= len(data); i++ {
		sn.ChannelIDs = append(sn.ChannelIDs, binary.LittleEndian.Uint16(data[offset:offset+2]))
		offset += 2
	}
	return sn
}

// writeBlockHeader prepends a GCC user data block header (type uint16 + length uint16).
func writeBlockHeader(blockType uint16, data []byte) []byte {
	totalLen := 4 + len(data) // 4 bytes for type + length
	header := make([]byte, totalLen)
	binary.LittleEndian.PutUint16(header[0:2], blockType)
	binary.LittleEndian.PutUint16(header[2:4], uint16(totalLen))
	copy(header[4:], data)
	return header
}

// putUTF16LEFixedLen encodes a string as UTF-16LE into a pre-allocated fixed-length buffer.
func putUTF16LEFixedLen(dst []byte, s string) {
	// dst is already zeroed from make()
	if s == "" {
		return
	}
	runes := []rune(s)
	// Truncate to fit (each rune is 2 bytes, reserve 2 for null terminator)
	maxRunes := (len(dst) / 2) - 1
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	u16 := utf16.Encode(runes)
	for i, c := range u16 {
		binary.LittleEndian.PutUint16(dst[i*2:], c)
	}
}

// encodeUTF16LEFixedLen encodes a string as UTF-16LE in a fixed-length byte buffer.
// Kept for test compatibility.
func encodeUTF16LEFixedLen(s string, size int) []byte {
	buf := make([]byte, size)
	putUTF16LEFixedLen(buf, s)
	return buf
}

// DefaultClientCoreData returns a ClientCoreData with sensible RDP defaults.
func DefaultClientCoreData(width, height, depth uint16, selectedProtocol uint32) *ClientCoreData {
	highColor, postBeta2, supportedDepths := mapColorDepth(depth)

	earlyFlags := EarlyCapSupportErrInfoPDU | EarlyCapStrongAsymmetricKeys |
		EarlyCapValidConnectionType
	if depth == 32 {
		earlyFlags |= EarlyCapWant32BPPSession
	}

	return &ClientCoreData{
		Version:                VersionRDP5,
		DesktopWidth:           width,
		DesktopHeight:          height,
		ColorDepth:             ColorDepthRNS_UD_COLOR_8BPP,
		SASSequence:            SASSequenceRDP,
		KeyboardLayout:         0x00000409, // US English
		ClientBuild:            2600,
		ClientName:             "gopher-rdp",
		KeyboardType:           KeyboardEnhanced,
		KeyboardSubType:        0,
		KeyboardFunctionKey:    12,
		PostBeta2ColorDepth:    postBeta2,
		ClientProductID:        1,
		SerialNumber:           0,
		HighColorDepth:         highColor,
		SupportedColorDepths:   supportedDepths,
		EarlyCapabilityFlags:   earlyFlags,
		ConnectionType:         ConnectionTypeLAN,
		ServerSelectedProtocol: selectedProtocol,
	}
}

// mapColorDepth converts a color depth (bits per pixel) to the various
// depth fields needed in ClientCoreData.
func mapColorDepth(depth uint16) (highColor, postBeta2 uint16, supported uint16) {
	supported = SupportColor24BPP | SupportColor16BPP | SupportColor15BPP | SupportColor32BPP
	switch depth {
	case 8:
		highColor = HighColor8BPP
		postBeta2 = ColorDepthRNS_UD_COLOR_8BPP
	case 15:
		highColor = HighColor15BPP
		postBeta2 = ColorDepthRNS_UD_COLOR_16BPP
	case 16:
		highColor = HighColor16BPP
		postBeta2 = ColorDepthRNS_UD_COLOR_16BPP
	case 32:
		highColor = HighColor24BPP // 32bpp uses 24 in HighColorDepth + earlyCapFlag
		postBeta2 = ColorDepthRNS_UD_COLOR_24BPP
	default: // 24
		highColor = HighColor24BPP
		postBeta2 = ColorDepthRNS_UD_COLOR_24BPP
	}
	return
}
