package sec

import (
	"encoding/binary"
	"testing"
)

func TestEncodeClientInfo(t *testing.T) {
	info := &ClientInfo{
		Domain:   "CORP",
		Username: "admin",
		Password: "pass",
	}

	data := EncodeClientInfo(info)

	// Fixed header is 18 bytes
	if len(data) < 18 {
		t.Fatalf("encoded data too short: %d bytes", len(data))
	}

	off := 0

	// codePage
	codePage := binary.LittleEndian.Uint32(data[off:])
	if codePage != 0 {
		t.Errorf("codePage = %d, want 0", codePage)
	}
	off += 4

	// flags
	flags := binary.LittleEndian.Uint32(data[off:])
	wantFlags := InfoUnicode | InfoMouse | InfoDisableCtrlAltDel | InfoMaximizeShell | InfoEnableWinKey | InfoLogonNotify | InfoAutologon
	if flags != wantFlags {
		t.Errorf("flags = 0x%08X, want 0x%08X", flags, wantFlags)
	}
	off += 4

	// cb fields
	cbDomain := binary.LittleEndian.Uint16(data[off:])
	off += 2
	cbUser := binary.LittleEndian.Uint16(data[off:])
	off += 2
	cbPass := binary.LittleEndian.Uint16(data[off:])
	off += 2
	cbAltShell := binary.LittleEndian.Uint16(data[off:])
	off += 2
	cbWorkDir := binary.LittleEndian.Uint16(data[off:])
	off += 2

	// "CORP" in UTF-16LE = 8 bytes
	if cbDomain != 8 {
		t.Errorf("cbDomain = %d, want 8", cbDomain)
	}
	// "admin" in UTF-16LE = 10 bytes
	if cbUser != 10 {
		t.Errorf("cbUserName = %d, want 10", cbUser)
	}
	// "pass" in UTF-16LE = 8 bytes
	if cbPass != 8 {
		t.Errorf("cbPassword = %d, want 8", cbPass)
	}
	if cbAltShell != 0 {
		t.Errorf("cbAlternateShell = %d, want 0", cbAltShell)
	}
	if cbWorkDir != 0 {
		t.Errorf("cbWorkingDir = %d, want 0", cbWorkDir)
	}

	// Verify domain string ("CORP" = C:0x43, O:0x4F, R:0x52, P:0x50)
	if data[off] != 0x43 || data[off+1] != 0x00 {
		t.Errorf("domain[0:2] = %02X %02X, want 43 00", data[off], data[off+1])
	}
	off += int(cbDomain) + 2 // skip string + null term

	// Verify username starts with 'a' (0x61)
	if data[off] != 0x61 || data[off+1] != 0x00 {
		t.Errorf("username[0:2] = %02X %02X, want 61 00", data[off], data[off+1])
	}

	// Verify extended info is present after basic info strings.
	// Extended info starts after: header(18) + strings with null terms
	extStart := 18 + int(cbDomain) + 2 + int(cbUser) + 2 + int(cbPass) + 2 + 2 + 2
	if len(data) <= extStart {
		t.Fatalf("no extended info: total length = %d, basic ends at %d", len(data), extStart)
	}

	// clientAddressFamily should be AF_INET (0x0002)
	addrFamily := binary.LittleEndian.Uint16(data[extStart:])
	if addrFamily != 0x0002 {
		t.Errorf("clientAddressFamily = 0x%04X, want 0x0002", addrFamily)
	}

	// cbClientAddress should be > 0
	cbAddr := binary.LittleEndian.Uint16(data[extStart+2:])
	if cbAddr == 0 {
		t.Error("cbClientAddress = 0, want > 0")
	}
}

func TestEncodeClientInfo_EmptyFields(t *testing.T) {
	info := &ClientInfo{
		Username: "user",
	}

	data := EncodeClientInfo(info)

	// cbDomain should be 0
	cbDomain := binary.LittleEndian.Uint16(data[8:10])
	if cbDomain != 0 {
		t.Errorf("cbDomain = %d, want 0", cbDomain)
	}

	// cbPassword should be 0
	cbPass := binary.LittleEndian.Uint16(data[12:14])
	if cbPass != 0 {
		t.Errorf("cbPassword = %d, want 0", cbPass)
	}
}

func TestEncodeClientInfo_AudioCaptureFlag(t *testing.T) {
	off := EncodeClientInfo(&ClientInfo{Username: "user"})
	if flags := binary.LittleEndian.Uint32(off[4:8]); flags&InfoAudioCapture != 0 {
		t.Fatalf("flags = %#x, did not expect InfoAudioCapture", flags)
	}

	on := EncodeClientInfo(&ClientInfo{Username: "user", AudioInput: true})
	if flags := binary.LittleEndian.Uint32(on[4:8]); flags&InfoAudioCapture == 0 {
		t.Fatalf("flags = %#x, want InfoAudioCapture (%#x)", flags, InfoAudioCapture)
	}
}

func TestEncodeUTF16LE(t *testing.T) {
	tests := []struct {
		input string
		want  []byte
	}{
		{"A", []byte{0x41, 0x00}},
		{"AB", []byte{0x41, 0x00, 0x42, 0x00}},
		{"", nil},
	}

	for _, tt := range tests {
		got := encodeUTF16LE(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("encodeUTF16LE(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("encodeUTF16LE(%q)[%d] = 0x%02X, want 0x%02X", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
