package mcs

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"testing"
	"unicode/utf16"
)

func TestClientCoreDataEncode(t *testing.T) {
	core := DefaultClientCoreData(1024, 768, 24, 1)
	data := core.Encode()

	// Verify key fields at known offsets
	// Version: offset 0, 4 bytes
	version := binary.LittleEndian.Uint32(data[0:4])
	if version != VersionRDP5 {
		t.Errorf("version = 0x%08X, want 0x%08X", version, VersionRDP5)
	}

	// DesktopWidth: offset 4, 2 bytes
	width := binary.LittleEndian.Uint16(data[4:6])
	if width != 1024 {
		t.Errorf("width = %d, want 1024", width)
	}

	// DesktopHeight: offset 6, 2 bytes
	height := binary.LittleEndian.Uint16(data[6:8])
	if height != 768 {
		t.Errorf("height = %d, want 768", height)
	}

	// ColorDepth: offset 8, 2 bytes
	colorDepth := binary.LittleEndian.Uint16(data[8:10])
	if colorDepth != ColorDepthRNS_UD_COLOR_8BPP {
		t.Errorf("colorDepth = 0x%04X, want 0x%04X", colorDepth, ColorDepthRNS_UD_COLOR_8BPP)
	}

	// SASSequence: offset 10, 2 bytes
	sas := binary.LittleEndian.Uint16(data[10:12])
	if sas != SASSequenceRDP {
		t.Errorf("SASSequence = 0x%04X, want 0x%04X", sas, SASSequenceRDP)
	}

	// KeyboardLayout: offset 12, 4 bytes
	kbLayout := binary.LittleEndian.Uint32(data[12:16])
	if kbLayout != 0x00000409 {
		t.Errorf("keyboardLayout = 0x%08X, want 0x00000409", kbLayout)
	}

	// ClientBuild: offset 16, 4 bytes
	build := binary.LittleEndian.Uint32(data[16:20])
	if build != 2600 {
		t.Errorf("clientBuild = %d, want 2600", build)
	}
}

func TestClientCoreDataUTF16ClientName(t *testing.T) {
	core := &ClientCoreData{
		Version:    VersionRDP5,
		ClientName: "gopher-rdp",
	}
	data := core.Encode()

	// ClientName starts at offset 20, 32 bytes UTF-16LE
	nameBytes := data[20:52]

	// Decode UTF-16LE
	u16 := make([]uint16, 16)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(nameBytes[i*2:])
	}
	// Find null terminator
	end := 0
	for end < len(u16) && u16[end] != 0 {
		end++
	}
	decoded := string(utf16.Decode(u16[:end]))
	if decoded != "gopher-rdp" {
		t.Errorf("clientName = %q, want %q", decoded, "gopher-rdp")
	}
}

func TestClientCoreDataEncodeBlock(t *testing.T) {
	core := DefaultClientCoreData(800, 600, 16, 0)
	block := core.EncodeBlock()

	// Block header: type (2) + length (2)
	blockType := binary.LittleEndian.Uint16(block[0:2])
	blockLen := binary.LittleEndian.Uint16(block[2:4])

	if blockType != ClientCoreDataType {
		t.Errorf("block type = 0x%04X, want 0x%04X", blockType, ClientCoreDataType)
	}
	if int(blockLen) != len(block) {
		t.Errorf("block length = %d, but actual = %d", blockLen, len(block))
	}
}

func TestClientSecurityDataEncode(t *testing.T) {
	sec := &ClientSecurityData{
		EncryptionMethods:    0x0000001B,
		ExtEncryptionMethods: 0,
	}
	data := sec.Encode()

	if len(data) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(data))
	}

	methods := binary.LittleEndian.Uint32(data[0:4])
	if methods != 0x0000001B {
		t.Errorf("encryptionMethods = 0x%08X, want 0x0000001B", methods)
	}

	ext := binary.LittleEndian.Uint32(data[4:8])
	if ext != 0 {
		t.Errorf("extEncryptionMethods = 0x%08X, want 0", ext)
	}
}

func TestClientSecurityDataEncodeBlock(t *testing.T) {
	sec := &ClientSecurityData{}
	block := sec.EncodeBlock()

	if len(block) != 12 { // 4 header + 8 data
		t.Fatalf("expected 12 bytes, got %d", len(block))
	}

	blockType := binary.LittleEndian.Uint16(block[0:2])
	if blockType != ClientSecurityDataType {
		t.Errorf("block type = 0x%04X, want 0x%04X", blockType, ClientSecurityDataType)
	}

	blockLen := binary.LittleEndian.Uint16(block[2:4])
	if blockLen != 12 {
		t.Errorf("block length = %d, want 12", blockLen)
	}
}

func TestClientNetworkDataEncode(t *testing.T) {
	net := &ClientNetworkData{
		Channels: []ChannelDef{
			{Name: "rdpdr", Options: ChannelOptionInitialized | ChannelOptionCompress},
			{Name: "cliprdr", Options: ChannelOptionInitialized | ChannelOptionEncryptRDP},
		},
	}
	data := net.Encode()

	// Channel count: 4 bytes
	count := binary.LittleEndian.Uint32(data[0:4])
	if count != 2 {
		t.Errorf("channel count = %d, want 2", count)
	}

	// First channel: name(8) + options(4) at offset 4
	name1 := string(bytes.TrimRight(data[4:12], "\x00"))
	if name1 != "rdpdr" {
		t.Errorf("channel 1 name = %q, want %q", name1, "rdpdr")
	}

	opts1 := binary.LittleEndian.Uint32(data[12:16])
	wantOpts1 := ChannelOptionInitialized | ChannelOptionCompress
	if opts1 != wantOpts1 {
		t.Errorf("channel 1 options = 0x%08X, want 0x%08X", opts1, wantOpts1)
	}

	// Second channel at offset 16
	name2 := string(bytes.TrimRight(data[16:24], "\x00"))
	if name2 != "cliprdr" {
		t.Errorf("channel 2 name = %q, want %q", name2, "cliprdr")
	}
}

func TestClientNetworkDataEncodeBlock(t *testing.T) {
	net := &ClientNetworkData{
		Channels: []ChannelDef{
			{Name: "test", Options: 0},
		},
	}
	block := net.EncodeBlock()

	blockType := binary.LittleEndian.Uint16(block[0:2])
	if blockType != ClientNetworkDataType {
		t.Errorf("block type = 0x%04X, want 0x%04X", blockType, ClientNetworkDataType)
	}
}

func TestClientNetworkDataEmpty(t *testing.T) {
	net := &ClientNetworkData{}
	data := net.Encode()

	count := binary.LittleEndian.Uint32(data[0:4])
	if count != 0 {
		t.Errorf("channel count = %d, want 0", count)
	}
	if len(data) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(data))
	}
}

func TestDecodeServerData(t *testing.T) {
	// Build a minimal server data blob with all three blocks
	var buf bytes.Buffer

	// Server Core Data
	binary.Write(&buf, binary.LittleEndian, ServerCoreDataType)
	binary.Write(&buf, binary.LittleEndian, uint16(16))  // length: 4 header + 12 data
	binary.Write(&buf, binary.LittleEndian, VersionRDP5) // version
	binary.Write(&buf, binary.LittleEndian, uint32(1))   // clientRequestedProto
	binary.Write(&buf, binary.LittleEndian, uint32(0))   // earlyCapFlags

	// Server Security Data (minimal: encryption method + level, no cert)
	binary.Write(&buf, binary.LittleEndian, ServerSecurityDataType)
	binary.Write(&buf, binary.LittleEndian, uint16(12)) // length: 4 header + 8 data
	binary.Write(&buf, binary.LittleEndian, uint32(0))  // encryption method
	binary.Write(&buf, binary.LittleEndian, uint32(0))  // encryption level

	// Server Network Data
	binary.Write(&buf, binary.LittleEndian, ServerNetworkDataType)
	binary.Write(&buf, binary.LittleEndian, uint16(10))   // length: 4 header + 6 data
	binary.Write(&buf, binary.LittleEndian, uint16(1003)) // IO channel
	binary.Write(&buf, binary.LittleEndian, uint16(1))    // channel count
	binary.Write(&buf, binary.LittleEndian, uint16(1004)) // channel ID

	core, sec, net, err := DecodeServerData(slog.Default(), buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeServerData error: %v", err)
	}

	if core == nil {
		t.Fatal("expected non-nil server core data")
	}
	if core.Version != VersionRDP5 {
		t.Errorf("core version = 0x%08X, want 0x%08X", core.Version, VersionRDP5)
	}

	if sec == nil {
		t.Fatal("expected non-nil server security data")
	}

	if net == nil {
		t.Fatal("expected non-nil server network data")
	}
	if net.IOChannelID != 1003 {
		t.Errorf("IO channel = %d, want 1003", net.IOChannelID)
	}
	if len(net.ChannelIDs) != 1 || net.ChannelIDs[0] != 1004 {
		t.Errorf("channel IDs = %v, want [1004]", net.ChannelIDs)
	}
}

func TestDecodeServerDataMissingNetwork(t *testing.T) {
	// Only server core, no network data
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, ServerCoreDataType)
	binary.Write(&buf, binary.LittleEndian, uint16(8))
	binary.Write(&buf, binary.LittleEndian, VersionRDP5)

	_, _, _, err := DecodeServerData(slog.Default(), buf.Bytes())
	if err == nil {
		t.Error("expected error when server network data is missing")
	}
}

func TestMapColorDepth(t *testing.T) {
	tests := []struct {
		depth    uint16
		wantHigh uint16
		wantPost uint16
	}{
		{15, HighColor15BPP, ColorDepthRNS_UD_COLOR_16BPP},
		{16, HighColor16BPP, ColorDepthRNS_UD_COLOR_16BPP},
		{24, HighColor24BPP, ColorDepthRNS_UD_COLOR_24BPP},
		{32, HighColor24BPP, ColorDepthRNS_UD_COLOR_24BPP},
	}
	for _, tt := range tests {
		high, post, _ := mapColorDepth(tt.depth)
		if high != tt.wantHigh {
			t.Errorf("depth=%d: highColor = 0x%04X, want 0x%04X", tt.depth, high, tt.wantHigh)
		}
		if post != tt.wantPost {
			t.Errorf("depth=%d: postBeta2 = 0x%04X, want 0x%04X", tt.depth, post, tt.wantPost)
		}
	}
}

func TestClientCoreDataEncodeWithDPI(t *testing.T) {
	core := DefaultClientCoreData(1920, 1080, 32, 1)
	core.Version = VersionRDP10
	core.DesktopScaleFactor = 150
	core.DeviceScaleFactor = 100
	core.DesktopPhysicalWidth = 338
	core.DesktopPhysicalHeight = 190
	core.DesktopOrientation = 0
	core.EarlyCapabilityFlags |= EarlyCapSupportMonitorLayout

	data := core.Encode()
	expectedLen := clientCoreFixedSize + clientCoreOptionalDPISize // 216 + 18 = 234
	if len(data) != expectedLen {
		t.Fatalf("encoded length = %d, want %d", len(data), expectedLen)
	}

	// Version should be RDP10
	version := binary.LittleEndian.Uint32(data[0:4])
	if version != VersionRDP10 {
		t.Errorf("version = 0x%08X, want 0x%08X", version, VersionRDP10)
	}

	// DPI fields start at offset 216
	off := clientCoreFixedSize
	physW := binary.LittleEndian.Uint32(data[off:])
	if physW != 338 {
		t.Errorf("desktopPhysicalWidth = %d, want 338", physW)
	}
	physH := binary.LittleEndian.Uint32(data[off+4:])
	if physH != 190 {
		t.Errorf("desktopPhysicalHeight = %d, want 190", physH)
	}
	orient := binary.LittleEndian.Uint16(data[off+8:])
	if orient != 0 {
		t.Errorf("desktopOrientation = %d, want 0", orient)
	}
	dScale := binary.LittleEndian.Uint32(data[off+10:])
	if dScale != 150 {
		t.Errorf("desktopScaleFactor = %d, want 150", dScale)
	}
	devScale := binary.LittleEndian.Uint32(data[off+14:])
	if devScale != 100 {
		t.Errorf("deviceScaleFactor = %d, want 100", devScale)
	}
}

func TestClientCoreDataEncodeWithoutDPI(t *testing.T) {
	// Backward compat: no DPI fields when DesktopScaleFactor is 0
	core := DefaultClientCoreData(1024, 768, 24, 1)
	data := core.Encode()
	if len(data) != clientCoreFixedSize {
		t.Errorf("encoded length = %d, want %d (no DPI)", len(data), clientCoreFixedSize)
	}
}

func TestClientCoreDataDPIBlockLength(t *testing.T) {
	// EncodeBlock should produce correct block header length with DPI
	core := DefaultClientCoreData(1920, 1080, 32, 1)
	core.DesktopScaleFactor = 200
	core.DeviceScaleFactor = 140

	block := core.EncodeBlock()
	blockLen := binary.LittleEndian.Uint16(block[2:4])
	if int(blockLen) != len(block) {
		t.Errorf("block header length = %d, actual = %d", blockLen, len(block))
	}
	expectedLen := 4 + clientCoreFixedSize + clientCoreOptionalDPISize
	if len(block) != expectedLen {
		t.Errorf("block length = %d, want %d", len(block), expectedLen)
	}
}

func TestEncodeUTF16LEFixedLen(t *testing.T) {
	result := encodeUTF16LEFixedLen("AB", 8)
	if len(result) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(result))
	}
	// 'A' = 0x41, 'B' = 0x42 in UTF-16LE
	if result[0] != 0x41 || result[1] != 0x00 {
		t.Errorf("first char: got %X %X, want 41 00", result[0], result[1])
	}
	if result[2] != 0x42 || result[3] != 0x00 {
		t.Errorf("second char: got %X %X, want 42 00", result[2], result[3])
	}
	// Rest should be null
	for i := 4; i < 8; i++ {
		if result[i] != 0 {
			t.Errorf("byte %d = 0x%02X, want 0x00", i, result[i])
		}
	}
}

func TestClientMonitorDataEncodeSingle(t *testing.T) {
	md := &ClientMonitorData{
		Monitors: []MonitorDef{
			{Left: 0, Top: 0, Right: 1919, Bottom: 1079, Flags: MonitorPrimary},
		},
	}
	data := md.Encode()

	// flags(4) + count(4) + 1*20 = 28 bytes
	if len(data) != 28 {
		t.Fatalf("length = %d, want 28", len(data))
	}

	// flags field = 0
	flags := binary.LittleEndian.Uint32(data[0:4])
	if flags != 0 {
		t.Errorf("flags = 0x%08X, want 0", flags)
	}

	count := binary.LittleEndian.Uint32(data[4:8])
	if count != 1 {
		t.Errorf("monitorCount = %d, want 1", count)
	}

	// Monitor: left(i32) + top(i32) + right(i32) + bottom(i32) + flags(u32)
	off := 8
	left := int32(binary.LittleEndian.Uint32(data[off:]))
	if left != 0 {
		t.Errorf("left = %d, want 0", left)
	}
	right := int32(binary.LittleEndian.Uint32(data[off+8:]))
	if right != 1919 {
		t.Errorf("right = %d, want 1919", right)
	}
	bottom := int32(binary.LittleEndian.Uint32(data[off+12:]))
	if bottom != 1079 {
		t.Errorf("bottom = %d, want 1079", bottom)
	}
	mFlags := binary.LittleEndian.Uint32(data[off+16:])
	if mFlags != MonitorPrimary {
		t.Errorf("monitor flags = 0x%08X, want 0x%08X", mFlags, MonitorPrimary)
	}
}

func TestClientMonitorDataEncodeDual(t *testing.T) {
	md := &ClientMonitorData{
		Monitors: []MonitorDef{
			{Left: 0, Top: 0, Right: 1919, Bottom: 1079, Flags: MonitorPrimary},
			{Left: 1920, Top: 0, Right: 3839, Bottom: 1079, Flags: 0},
		},
	}
	data := md.Encode()

	// flags(4) + count(4) + 2*20 = 48 bytes
	if len(data) != 48 {
		t.Fatalf("length = %d, want 48", len(data))
	}

	count := binary.LittleEndian.Uint32(data[4:8])
	if count != 2 {
		t.Errorf("monitorCount = %d, want 2", count)
	}

	// Second monitor at offset 28
	off := 28
	left := int32(binary.LittleEndian.Uint32(data[off:]))
	if left != 1920 {
		t.Errorf("monitor2 left = %d, want 1920", left)
	}
	right := int32(binary.LittleEndian.Uint32(data[off+8:]))
	if right != 3839 {
		t.Errorf("monitor2 right = %d, want 3839", right)
	}
	mFlags := binary.LittleEndian.Uint32(data[off+16:])
	if mFlags != 0 {
		t.Errorf("monitor2 flags = 0x%08X, want 0", mFlags)
	}
}

func TestClientMonitorDataEncodeBlock(t *testing.T) {
	md := &ClientMonitorData{
		Monitors: []MonitorDef{
			{Left: 0, Top: 0, Right: 1919, Bottom: 1079, Flags: MonitorPrimary},
		},
	}
	block := md.EncodeBlock()

	blockType := binary.LittleEndian.Uint16(block[0:2])
	if blockType != ClientMonitorDataType {
		t.Errorf("block type = 0x%04X, want 0x%04X", blockType, ClientMonitorDataType)
	}

	blockLen := binary.LittleEndian.Uint16(block[2:4])
	// header(4) + flags(4) + count(4) + 1*20 = 32
	if blockLen != 32 {
		t.Errorf("block length = %d, want 32", blockLen)
	}
	if int(blockLen) != len(block) {
		t.Errorf("block header length = %d, actual = %d", blockLen, len(block))
	}
}

func TestEncodeUTF16LEFixedLenTruncation(t *testing.T) {
	// Size=6 means max 2 chars (3 code units but reserve 1 for null = 2 chars)
	result := encodeUTF16LEFixedLen("ABCDE", 6)
	if len(result) != 6 {
		t.Fatalf("expected 6 bytes, got %d", len(result))
	}
	// Should only contain "AB" + null terminator space
	if result[0] != 0x41 || result[1] != 0x00 {
		t.Errorf("first char wrong")
	}
	if result[2] != 0x42 || result[3] != 0x00 {
		t.Errorf("second char wrong")
	}
}

func TestClientClusterDataCarriesTheConsoleSessionRequest(t *testing.T) {
	// mstsc /admin, and FreeRDP's /admin, ask for the console session by setting
	// REDIRECTED_SESSIONID_FIELD_VALID with session id 0 (MS-RDPBCGR 2.2.1.3.5).
	cluster := &ClientClusterData{
		Flags: RedirectionSupported | (RedirectionVersion5 << 2) | RedirectedSessionIDFieldValid,
	}
	data := cluster.Encode()
	if len(data) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(data))
	}
	if flags := binary.LittleEndian.Uint32(data[0:4]); flags != 0x00000013 {
		t.Errorf("flags = 0x%08X, want 0x00000013", flags)
	}
	if session := binary.LittleEndian.Uint32(data[4:8]); session != 0 {
		t.Errorf("redirected session id = %d, want 0 (the console)", session)
	}

	block := cluster.EncodeBlock()
	if blockType := binary.LittleEndian.Uint16(block[0:2]); blockType != ClientClusterDataType {
		t.Errorf("block type = 0x%04X, want 0x%04X", blockType, ClientClusterDataType)
	}
	if blockLen := binary.LittleEndian.Uint16(block[2:4]); blockLen != 12 {
		t.Errorf("block length = %d, want 12", blockLen)
	}
}
