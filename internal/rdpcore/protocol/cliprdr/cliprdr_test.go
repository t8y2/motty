package cliprdr

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"testing"
)

var testLog = slog.Default()

func TestDecodePDU(t *testing.T) {
	// Build a PDU: type=0x0002, flags=0x0001, dataLen=4, payload=[0x0D,0x00,0x00,0x00]
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], 0x0002)
	binary.LittleEndian.PutUint16(data[2:4], 0x0001)
	binary.LittleEndian.PutUint32(data[4:8], 4)
	binary.LittleEndian.PutUint32(data[8:12], 13)

	msgType, msgFlags, payload, err := decodePDU(data)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatList {
		t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBFormatList)
	}
	if msgFlags != CBResponseOK {
		t.Errorf("msgFlags = 0x%04X, want 0x%04X", msgFlags, CBResponseOK)
	}
	if len(payload) != 4 {
		t.Fatalf("payload len = %d, want 4", len(payload))
	}
	if binary.LittleEndian.Uint32(payload) != 13 {
		t.Errorf("payload value = %d, want 13", binary.LittleEndian.Uint32(payload))
	}
}

func TestDecodePDU_TooShort(t *testing.T) {
	_, _, _, err := decodePDU([]byte{0, 1, 2})
	if err == nil {
		t.Error("expected error for short PDU")
	}
}

func TestDecodePDU_TruncatedPayload(t *testing.T) {
	// Header says 100 bytes but only 4 present
	data := make([]byte, 12)
	binary.LittleEndian.PutUint16(data[0:2], CBFormatDataResponse)
	binary.LittleEndian.PutUint16(data[2:4], CBResponseOK)
	binary.LittleEndian.PutUint32(data[4:8], 100) // claimed 100 bytes
	binary.LittleEndian.PutUint32(data[8:12], 42)

	_, _, payload, err := decodePDU(data)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	// Should clamp to actual available data
	if len(payload) != 4 {
		t.Errorf("payload len = %d, want 4 (clamped)", len(payload))
	}
}

func TestEncodeDecodeCapsPDU(t *testing.T) {
	pdu := encodeCapsPDU(CBCapsVersion2, CBUseLongFormatNames)

	// Decode outer header
	msgType, _, payload, err := decodePDU(pdu)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBClipCaps {
		t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBClipCaps)
	}

	version, flags, err := decodeCapsPDU(payload)
	if err != nil {
		t.Fatalf("decodeCapsPDU: %v", err)
	}
	if version != CBCapsVersion2 {
		t.Errorf("version = %d, want %d", version, CBCapsVersion2)
	}
	if flags != CBUseLongFormatNames {
		t.Errorf("flags = 0x%08X, want 0x%08X", flags, CBUseLongFormatNames)
	}
}

func TestDecodeCapsPDU_TooShort(t *testing.T) {
	_, _, err := decodeCapsPDU([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for short caps payload")
	}
}

func TestEncodeDecodeFormatListLong(t *testing.T) {
	tests := []struct {
		name    string
		formats []FormatListEntry
	}{
		{
			"single standard format",
			[]FormatListEntry{{FormatID: CFUnicodeText}},
		},
		{
			"multiple formats",
			[]FormatListEntry{
				{FormatID: CFUnicodeText},
				{FormatID: 0xC000, FormatName: "HTML Format"},
			},
		},
		{
			"empty list",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdu := encodeFormatListLong(tt.formats)
			msgType, _, payload, err := decodePDU(pdu)
			if err != nil {
				t.Fatalf("decodePDU: %v", err)
			}
			if msgType != CBFormatList {
				t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBFormatList)
			}
			got := decodeFormatListLong(payload)
			if len(got) != len(tt.formats) {
				t.Fatalf("got %d entries, want %d", len(got), len(tt.formats))
			}
			for i, f := range got {
				if f.FormatID != tt.formats[i].FormatID {
					t.Errorf("[%d] FormatID = %d, want %d", i, f.FormatID, tt.formats[i].FormatID)
				}
				if f.FormatName != tt.formats[i].FormatName {
					t.Errorf("[%d] FormatName = %q, want %q", i, f.FormatName, tt.formats[i].FormatName)
				}
			}
		})
	}
}

func TestEncodeDecodeFormatListShort(t *testing.T) {
	tests := []struct {
		name    string
		formats []FormatListEntry
	}{
		{
			"single standard format",
			[]FormatListEntry{{FormatID: CFUnicodeText}},
		},
		{
			"format with name",
			[]FormatListEntry{{FormatID: 0xC000, FormatName: "HTML"}},
		},
		{
			"empty list",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdu := encodeFormatListShort(tt.formats)
			msgType, _, payload, err := decodePDU(pdu)
			if err != nil {
				t.Fatalf("decodePDU: %v", err)
			}
			if msgType != CBFormatList {
				t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBFormatList)
			}
			got := decodeFormatListShort(payload)
			if len(got) != len(tt.formats) {
				t.Fatalf("got %d entries, want %d", len(got), len(tt.formats))
			}
			for i, f := range got {
				if f.FormatID != tt.formats[i].FormatID {
					t.Errorf("[%d] FormatID = %d, want %d", i, f.FormatID, tt.formats[i].FormatID)
				}
				if f.FormatName != tt.formats[i].FormatName {
					t.Errorf("[%d] FormatName = %q, want %q", i, f.FormatName, tt.formats[i].FormatName)
				}
			}
			// Verify each entry is exactly 36 bytes
			if len(tt.formats) > 0 && len(payload) != shortFormatEntrySize*len(tt.formats) {
				t.Errorf("payload len = %d, want %d", len(payload), shortFormatEntrySize*len(tt.formats))
			}
		})
	}
}

func TestEncodeFormatDataRequest(t *testing.T) {
	pdu := encodeFormatDataRequest(CFUnicodeText)
	if len(pdu) != 12 {
		t.Fatalf("len = %d, want 12", len(pdu))
	}
	msgType, _, payload, err := decodePDU(pdu)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatDataRequest {
		t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBFormatDataRequest)
	}
	if len(payload) != 4 {
		t.Fatalf("payload len = %d, want 4", len(payload))
	}
	fmtID := binary.LittleEndian.Uint32(payload)
	if fmtID != CFUnicodeText {
		t.Errorf("formatID = %d, want %d", fmtID, CFUnicodeText)
	}
}

func TestEncodeFormatDataResponse(t *testing.T) {
	// Success with data
	data := []byte{0x41, 0x00, 0x00, 0x00} // "A" in UTF-16LE + null
	pdu := encodeFormatDataResponse(true, data)
	msgType, msgFlags, payload, err := decodePDU(pdu)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatDataResponse {
		t.Errorf("msgType = 0x%04X, want 0x%04X", msgType, CBFormatDataResponse)
	}
	if msgFlags != CBResponseOK {
		t.Errorf("flags = 0x%04X, want CBResponseOK", msgFlags)
	}
	if !bytes.Equal(payload, data) {
		t.Errorf("payload mismatch")
	}

	// Failure with no data
	pdu = encodeFormatDataResponse(false, nil)
	_, msgFlags, payload, err = decodePDU(pdu)
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgFlags != CBResponseFail {
		t.Errorf("flags = 0x%04X, want CBResponseFail", msgFlags)
	}
	if len(payload) != 0 {
		t.Errorf("payload len = %d, want 0", len(payload))
	}
}

func TestHandlerHandshake_WithLongFormatNames(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// Simulate server sending CB_CLIP_CAPS with long format names
	capsPDU := encodeCapsPDU(CBCapsVersion2, CBUseLongFormatNames)
	h.ProcessPDU(capsPDU)

	if h.serverCaps != CBUseLongFormatNames {
		t.Errorf("serverCaps = 0x%08X, want 0x%08X", h.serverCaps, CBUseLongFormatNames)
	}

	// Simulate server sending CB_MONITOR_READY
	monitorReadyPDU := encodePDU(CBMonitorReady, 0, nil)
	h.ProcessPDU(monitorReadyPDU)

	if !h.ready {
		t.Error("handler should be ready after monitor ready")
	}
	if !h.useLongFormatNames {
		t.Error("should negotiate long format names when server supports it")
	}

	// Should have sent 2 PDUs: caps + format list
	if len(sent) != 2 {
		t.Fatalf("sent %d PDUs, want 2", len(sent))
	}

	// First should be CB_CLIP_CAPS
	msgType, _, _, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU[0]: %v", err)
	}
	if msgType != CBClipCaps {
		t.Errorf("sent[0] type = 0x%04X, want CBClipCaps", msgType)
	}

	// Second should be CB_FORMAT_LIST (long format) with 2 entries: CF_UNICODETEXT + CF_DIB
	msgType, _, payload, err := decodePDU(sent[1])
	if err != nil {
		t.Fatalf("decodePDU[1]: %v", err)
	}
	if msgType != CBFormatList {
		t.Errorf("sent[1] type = 0x%04X, want CBFormatList", msgType)
	}
	// Long format: 2 × (formatId(4) + null(2)) = 12 bytes
	if len(payload) != 12 {
		t.Errorf("format list payload = %d bytes, want 12 (long format, 2 entries)", len(payload))
	}
}

func TestHandlerHandshake_WithoutLongFormatNames(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// Simulate server sending CB_CLIP_CAPS WITHOUT long format names
	capsPDU := encodeCapsPDU(CBCapsVersion2, 0)
	h.ProcessPDU(capsPDU)

	// Simulate server sending CB_MONITOR_READY
	monitorReadyPDU := encodePDU(CBMonitorReady, 0, nil)
	h.ProcessPDU(monitorReadyPDU)

	if !h.ready {
		t.Error("handler should be ready after monitor ready")
	}
	if h.useLongFormatNames {
		t.Error("should NOT negotiate long format names when server doesn't support it")
	}

	// Should have sent 2 PDUs: caps + format list
	if len(sent) != 2 {
		t.Fatalf("sent %d PDUs, want 2", len(sent))
	}

	// First: CB_CLIP_CAPS
	msgType, _, _, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU[0]: %v", err)
	}
	if msgType != CBClipCaps {
		t.Errorf("sent[0] type = 0x%04X, want CBClipCaps", msgType)
	}

	// Second: CB_FORMAT_LIST (short format) with 2 entries: CF_UNICODETEXT + CF_DIB
	msgType, _, payload, err := decodePDU(sent[1])
	if err != nil {
		t.Fatalf("decodePDU[1]: %v", err)
	}
	if msgType != CBFormatList {
		t.Errorf("sent[1] type = 0x%04X, want CBFormatList", msgType)
	}
	// Short format: 2 × (formatId(4) + formatName[32]) = 72 bytes
	if len(payload) != shortFormatEntrySize*2 {
		t.Errorf("format list payload = %d bytes, want %d (short format, 2 entries)", len(payload), shortFormatEntrySize*2)
	}
}

func TestHandlerHandshake_NoCaps(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// Server sends only CB_MONITOR_READY (no CB_CLIP_CAPS at all)
	monitorReadyPDU := encodePDU(CBMonitorReady, 0, nil)
	h.ProcessPDU(monitorReadyPDU)

	if !h.ready {
		t.Error("handler should be ready after monitor ready")
	}
	if h.useLongFormatNames {
		t.Error("should NOT negotiate long format names when server sends no caps")
	}

	// Should have sent 2 PDUs: caps + format list (short format)
	if len(sent) != 2 {
		t.Fatalf("sent %d PDUs, want 2", len(sent))
	}

	msgType, _, _, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU[0]: %v", err)
	}
	if msgType != CBClipCaps {
		t.Errorf("sent[0] type = 0x%04X, want CBClipCaps", msgType)
	}

	msgType, _, payload, err := decodePDU(sent[1])
	if err != nil {
		t.Fatalf("decodePDU[1]: %v", err)
	}
	if msgType != CBFormatList {
		t.Errorf("sent[1] type = 0x%04X, want CBFormatList", msgType)
	}
	// Short format: 2 entries
	if len(payload) != shortFormatEntrySize*2 {
		t.Errorf("format list payload = %d bytes, want %d (short format, 2 entries)", len(payload), shortFormatEntrySize*2)
	}
}

func TestHandlerFormatListCallback(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	var gotHasText bool
	var callbackCalled bool
	h.OnRemoteCopy(func(hasText, hasImage, hasFiles bool) {
		callbackCalled = true
		gotHasText = hasText
	})

	// Server sends format list with CF_UNICODETEXT (short format, since no caps negotiated)
	serverFormatList := encodeFormatListShort([]FormatListEntry{{FormatID: CFUnicodeText}})
	h.ProcessPDU(serverFormatList)

	if !callbackCalled {
		t.Error("onRemoteCopy callback was not called")
	}
	if !gotHasText {
		t.Error("hasText should be true")
	}

	// Should have sent a CB_FORMAT_LIST_RESPONSE
	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	msgType, msgFlags, _, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatListResponse {
		t.Errorf("type = 0x%04X, want CBFormatListResponse", msgType)
	}
	if msgFlags != CBResponseOK {
		t.Errorf("flags = 0x%04X, want CBResponseOK", msgFlags)
	}
}

func TestHandlerFormatListCallback_NoText(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)

	var gotHasText bool
	h.OnRemoteCopy(func(hasText, hasImage, hasFiles bool) {
		gotHasText = hasText
	})

	// Server sends format list without CF_UNICODETEXT (short format)
	serverFormatList := encodeFormatListShort([]FormatListEntry{{FormatID: 1}}) // CF_TEXT, not UNICODE
	h.ProcessPDU(serverFormatList)

	if gotHasText {
		t.Error("hasText should be false when CF_UNICODETEXT not in list")
	}
}

func TestHandlerDataRequestResponse(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// Set local clipboard
	h.localText = "hello world"

	// Server requests CF_UNICODETEXT
	reqPDU := encodeFormatDataRequest(CFUnicodeText)
	h.ProcessPDU(reqPDU)

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}

	msgType, msgFlags, payload, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatDataResponse {
		t.Errorf("type = 0x%04X, want CBFormatDataResponse", msgType)
	}
	if msgFlags != CBResponseOK {
		t.Errorf("flags = 0x%04X, want CBResponseOK", msgFlags)
	}

	// Decode the text from the response
	text := textFromWire(payload)
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
}

func TestHandlerDataRequestResponse_Empty(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// No local text set — request should fail
	reqPDU := encodeFormatDataRequest(CFUnicodeText)
	h.ProcessPDU(reqPDU)

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	_, msgFlags, _, _ := decodePDU(sent[0])
	if msgFlags != CBResponseFail {
		t.Errorf("flags = 0x%04X, want CBResponseFail", msgFlags)
	}
}

func TestHandlerTextDataCallback(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)

	var gotText string
	h.OnTextData(func(text string) {
		gotText = text
	})
	if err := h.RequestRemoteText(); err != nil {
		t.Fatal(err)
	}

	// Simulate server sending format data response with "Hello"
	wireData := textToWire("Hello")
	respPDU := encodeFormatDataResponse(true, wireData)
	h.ProcessPDU(respPDU)

	if gotText != "Hello" {
		t.Errorf("got %q, want %q", gotText, "Hello")
	}
}

func TestHandlerSetLocalClipboard(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	if err := h.SetLocalClipboard("test"); err != nil {
		t.Fatalf("SetLocalClipboard: %v", err)
	}

	if h.localText != "test" {
		t.Errorf("localText = %q, want %q", h.localText, "test")
	}

	// Should have sent CB_FORMAT_LIST (short format, no caps negotiated) with 2 entries
	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	msgType, _, payload, _ := decodePDU(sent[0])
	if msgType != CBFormatList {
		t.Errorf("type = 0x%04X, want CBFormatList", msgType)
	}
	formats := decodeFormatListShort(payload)
	if len(formats) != 2 {
		t.Fatalf("format list = %+v, want 2 entries", formats)
	}
	if formats[0].FormatID != CFUnicodeText {
		t.Errorf("formats[0] = %d, want CF_UNICODETEXT", formats[0].FormatID)
	}
	if formats[1].FormatID != CFDIB {
		t.Errorf("formats[1] = %d, want CF_DIB", formats[1].FormatID)
	}
}

func TestHandlerRequestRemoteText(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	if err := h.RequestRemoteText(); err != nil {
		t.Fatalf("RequestRemoteText: %v", err)
	}

	if !h.pendingReq {
		t.Error("pendingReq should be true")
	}

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	msgType, _, payload, _ := decodePDU(sent[0])
	if msgType != CBFormatDataRequest {
		t.Errorf("type = 0x%04X, want CBFormatDataRequest", msgType)
	}
	fmtID := binary.LittleEndian.Uint32(payload)
	if fmtID != CFUnicodeText {
		t.Errorf("formatID = %d, want %d", fmtID, CFUnicodeText)
	}
}

func TestCapabilityConstants(t *testing.T) {
	// Verify constants match MS-RDPECLIP spec
	if CBCanLockClipData != 0x00000010 {
		t.Errorf("CBCanLockClipData = 0x%08X, want 0x00000010", CBCanLockClipData)
	}
	if CBHugeFileSupportEnabled != 0x00000020 {
		t.Errorf("CBHugeFileSupportEnabled = 0x%08X, want 0x00000020", CBHugeFileSupportEnabled)
	}
}

// --- SetEnabled toggle tests ---

func TestSetEnabled_DefaultEnabled(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)
	if !h.Enabled() {
		t.Error("new handler should be enabled by default")
	}
}

func TestSetEnabled_SuppressesCallbacks(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)

	var callbackCalled bool
	h.OnRemoteCopy(func(hasText, hasImage, hasFiles bool) {
		callbackCalled = true
	})

	h.SetEnabled(false)

	// Server sends format list — should be ACKed but callback suppressed
	serverFormatList := encodeFormatListShort([]FormatListEntry{{FormatID: CFUnicodeText}})
	h.ProcessPDU(serverFormatList)

	if callbackCalled {
		t.Error("callback should not fire when disabled")
	}
}

func TestSetEnabled_RejectsDataRequests(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	h.localText = "hello"
	h.SetEnabled(false)

	// Server requests CF_UNICODETEXT — should get CBResponseFail
	reqPDU := encodeFormatDataRequest(CFUnicodeText)
	h.ProcessPDU(reqPDU)

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	_, msgFlags, _, _ := decodePDU(sent[0])
	if msgFlags != CBResponseFail {
		t.Errorf("flags = 0x%04X, want CBResponseFail when disabled", msgFlags)
	}
}

func TestSetEnabled_NoopOutboundWhenDisabled(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	h.SetEnabled(false)

	if err := h.SetLocalClipboard("test"); err != nil {
		t.Fatalf("SetLocalClipboard: %v", err)
	}
	if err := h.RequestRemoteText(); err != nil {
		t.Fatalf("RequestRemoteText: %v", err)
	}
	if err := h.SetLocalImage([]byte{1, 2, 3}); err != nil {
		t.Fatalf("SetLocalImage: %v", err)
	}
	if err := h.RequestRemoteImage(); err != nil {
		t.Fatalf("RequestRemoteImage: %v", err)
	}

	if len(sent) != 0 {
		t.Errorf("sent %d PDUs, want 0 when disabled", len(sent))
	}
}

func TestSetEnabled_ReEnableSendsFormatList(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)
	h.ready = true
	h.SetEnabled(false)
	sent = nil // clear

	h.SetEnabled(true)

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1 (re-announce)", len(sent))
	}
	msgType, _, _, _ := decodePDU(sent[0])
	if msgType != CBFormatList {
		t.Errorf("type = 0x%04X, want CBFormatList", msgType)
	}
}

// --- CF_DIB bitmap tests ---

func TestHandlerFormatListCallback_WithImage(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)

	var gotHasText, gotHasImage bool
	h.OnRemoteCopy(func(hasText, hasImage, hasFiles bool) {
		gotHasText = hasText
		gotHasImage = hasImage
	})

	serverFormatList := encodeFormatListShort([]FormatListEntry{
		{FormatID: CFUnicodeText},
		{FormatID: CFDIB},
	})
	h.ProcessPDU(serverFormatList)

	if !gotHasText {
		t.Error("hasText should be true")
	}
	if !gotHasImage {
		t.Error("hasImage should be true")
	}
}

func TestHandlerRequestRemoteImage(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	if err := h.RequestRemoteImage(); err != nil {
		t.Fatalf("RequestRemoteImage: %v", err)
	}

	if !h.pendingReq {
		t.Error("pendingReq should be true")
	}
	if h.pendingFormat != CFDIB {
		t.Errorf("pendingFormat = %d, want %d", h.pendingFormat, CFDIB)
	}

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	msgType, _, payload, _ := decodePDU(sent[0])
	if msgType != CBFormatDataRequest {
		t.Errorf("type = 0x%04X, want CBFormatDataRequest", msgType)
	}
	fmtID := binary.LittleEndian.Uint32(payload)
	if fmtID != CFDIB {
		t.Errorf("formatID = %d, want %d", fmtID, CFDIB)
	}
}

func TestHandlerImageDataCallback(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLog)
	h.pendingReq = true
	h.pendingFormat = CFDIB

	// Create a minimal valid 1x1 24-bit DIB
	dib := make([]byte, bihSize+4) // 1px row padded to 4 bytes
	binary.LittleEndian.PutUint32(dib[0:4], bihSize)
	binary.LittleEndian.PutUint32(dib[4:8], 1)    // width
	binary.LittleEndian.PutUint32(dib[8:12], 1)   // height (bottom-up)
	binary.LittleEndian.PutUint16(dib[12:14], 1)  // planes
	binary.LittleEndian.PutUint16(dib[14:16], 24) // bpp
	dib[bihSize] = 0xFF                           // B
	dib[bihSize+1] = 0x00                         // G
	dib[bihSize+2] = 0x00                         // R

	var gotPNG []byte
	h.OnImageData(func(pngData []byte) {
		gotPNG = pngData
	})

	respPDU := encodeFormatDataResponse(true, dib)
	h.ProcessPDU(respPDU)

	if gotPNG == nil {
		t.Fatal("onImageData callback was not called")
	}
	// Verify it's valid PNG (starts with PNG magic)
	if len(gotPNG) < 8 || string(gotPNG[1:4]) != "PNG" {
		t.Error("received data is not valid PNG")
	}
}

func TestHandlerDataRequestResponse_DIB(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		cp := make([]byte, len(data))
		copy(cp, data)
		sent = append(sent, cp)
		return nil
	}, testLog)

	// Create a small valid PNG for local image
	png := createTestPNG(t, 2, 2)
	h.localImage = png

	// Server requests CF_DIB
	reqPDU := encodeFormatDataRequest(CFDIB)
	h.ProcessPDU(reqPDU)

	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}

	msgType, msgFlags, payload, err := decodePDU(sent[0])
	if err != nil {
		t.Fatalf("decodePDU: %v", err)
	}
	if msgType != CBFormatDataResponse {
		t.Errorf("type = 0x%04X, want CBFormatDataResponse", msgType)
	}
	if msgFlags != CBResponseOK {
		t.Errorf("flags = 0x%04X, want CBResponseOK", msgFlags)
	}

	// Verify the DIB header
	if len(payload) < bihSize {
		t.Fatalf("payload too short: %d bytes", len(payload))
	}
	biSize := binary.LittleEndian.Uint32(payload[0:4])
	if biSize != bihSize {
		t.Errorf("biSize = %d, want %d", biSize, bihSize)
	}
	w := binary.LittleEndian.Uint32(payload[4:8])
	h2 := binary.LittleEndian.Uint32(payload[8:12])
	if w != 2 || h2 != 2 {
		t.Errorf("DIB dimensions = %dx%d, want 2x2", w, h2)
	}
}

// createTestPNG creates a minimal PNG image of the given dimensions.
func createTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
