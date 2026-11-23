package disp

import (
	"encoding/binary"
	"log/slog"
	"testing"
)

var testLogger = slog.Default()

func TestDecodeCaps(t *testing.T) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(data[4:8], 16)
	binary.LittleEndian.PutUint32(data[8:12], 16)
	binary.LittleEndian.PutUint32(data[12:16], 25600000)

	caps, err := DecodeCaps(data)
	if err != nil {
		t.Fatalf("DecodeCaps error: %v", err)
	}
	if caps.MaxNumMonitors != 16 {
		t.Errorf("maxMonitors = %d, want 16", caps.MaxNumMonitors)
	}
	if caps.MaxMonitorAreaSize != 25600000 {
		t.Errorf("maxArea = %d, want 25600000", caps.MaxMonitorAreaSize)
	}
}

func TestDecodeCapsTooShort(t *testing.T) {
	_, err := DecodeCaps([]byte{0, 0, 0})
	if err == nil {
		t.Error("expected error for short data")
	}
}

func TestDecodeCapsWrongType(t *testing.T) {
	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], 0x99)
	_, err := DecodeCaps(data)
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

func TestEncodeMonitorLayout(t *testing.T) {
	monitors := []MonitorLayout{
		{
			Flags:              0x01,
			Left:               0,
			Top:                0,
			Width:              1920,
			Height:             1080,
			PhysicalWidth:      530,
			PhysicalHeight:     300,
			Orientation:        0,
			DesktopScaleFactor: 100,
			DeviceScaleFactor:  100,
		},
	}

	data := EncodeMonitorLayout(monitors)

	// Check header
	pduType := binary.LittleEndian.Uint32(data[0:4])
	if pduType != TypeMonitorLayout {
		t.Errorf("type = 0x%08X, want 0x%08X", pduType, TypeMonitorLayout)
	}

	totalLen := binary.LittleEndian.Uint32(data[4:8])
	expectedLen := uint32(16 + monitorLayoutSize)
	if totalLen != expectedLen {
		t.Errorf("totalLen = %d, want %d", totalLen, expectedLen)
	}

	layoutSize := binary.LittleEndian.Uint32(data[8:12])
	if layoutSize != monitorLayoutSize {
		t.Errorf("layoutSize = %d, want %d", layoutSize, monitorLayoutSize)
	}

	numMonitors := binary.LittleEndian.Uint32(data[12:16])
	if numMonitors != 1 {
		t.Errorf("numMonitors = %d, want 1", numMonitors)
	}

	// Check first monitor
	off := 16
	flags := binary.LittleEndian.Uint32(data[off:])
	if flags != 0x01 {
		t.Errorf("flags = 0x%08X, want 0x01", flags)
	}
	w := binary.LittleEndian.Uint32(data[off+12:])
	if w != 1920 {
		t.Errorf("width = %d, want 1920", w)
	}
	h := binary.LittleEndian.Uint32(data[off+16:])
	if h != 1080 {
		t.Errorf("height = %d, want 1080", h)
	}
	dScale := binary.LittleEndian.Uint32(data[off+32:])
	if dScale != 100 {
		t.Errorf("desktopScaleFactor = %d, want 100", dScale)
	}
}

func TestEncodeMonitorLayoutMultiple(t *testing.T) {
	monitors := []MonitorLayout{
		{Flags: 0x01, Width: 1920, Height: 1080},
		{Flags: 0x00, Left: 1920, Width: 1920, Height: 1080},
	}
	data := EncodeMonitorLayout(monitors)
	expectedLen := 16 + 2*monitorLayoutSize
	if len(data) != expectedLen {
		t.Errorf("length = %d, want %d", len(data), expectedLen)
	}
	numMonitors := binary.LittleEndian.Uint32(data[12:16])
	if numMonitors != 2 {
		t.Errorf("numMonitors = %d, want 2", numMonitors)
	}
}

func TestHandlerProcessCaps(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLogger)
	if h.Ready() {
		t.Error("should not be ready before caps")
	}

	data := make([]byte, 16)
	binary.LittleEndian.PutUint32(data[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(data[4:8], 16)
	binary.LittleEndian.PutUint32(data[8:12], 4)
	binary.LittleEndian.PutUint32(data[12:16], 8294400)

	h.ProcessPDU(data)

	if !h.Ready() {
		t.Error("should be ready after caps")
	}
	if h.Caps().MaxNumMonitors != 4 {
		t.Errorf("maxMonitors = %d, want 4", h.Caps().MaxNumMonitors)
	}
}

func TestHandlerResizeMulti(t *testing.T) {
	var sent []byte
	h := NewHandler(func(data []byte) error {
		sent = make([]byte, len(data))
		copy(sent, data)
		return nil
	}, testLogger)

	// Send caps first
	capsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(capsData[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(capsData[4:8], 16)
	binary.LittleEndian.PutUint32(capsData[8:12], 16)
	binary.LittleEndian.PutUint32(capsData[12:16], 25600000)
	h.ProcessPDU(capsData)

	monitors := []MonitorLayout{
		{Flags: MonitorLayoutPrimary, Width: 1920, Height: 1080},
		{Flags: 0, Left: 1920, Width: 1920, Height: 1080},
	}
	if err := h.ResizeMulti(monitors); err != nil {
		t.Fatalf("ResizeMulti error: %v", err)
	}
	if sent == nil {
		t.Fatal("no PDU sent")
	}

	numMonitors := binary.LittleEndian.Uint32(sent[12:16])
	if numMonitors != 2 {
		t.Errorf("numMonitors = %d, want 2", numMonitors)
	}

	// Check first monitor width (forced even)
	off := 16
	w1 := binary.LittleEndian.Uint32(sent[off+12:])
	if w1 != 1920 {
		t.Errorf("monitor1 width = %d, want 1920", w1)
	}

	// Check second monitor position
	off2 := 16 + monitorLayoutSize
	left2 := int32(binary.LittleEndian.Uint32(sent[off2+4:]))
	if left2 != 1920 {
		t.Errorf("monitor2 left = %d, want 1920", left2)
	}

	// Physical dimensions should be auto-computed at 96 DPI (1920 * 25.4 / 96 = 508 mm)
	physW := binary.LittleEndian.Uint32(sent[off+20:])
	if physW != 508 {
		t.Errorf("monitor1 physicalWidth = %d, want 508", physW)
	}
	physH := binary.LittleEndian.Uint32(sent[off+24:])
	if physH != 286 {
		t.Errorf("monitor1 physicalHeight = %d, want 286", physH)
	}
}

func TestHandlerResizeMultiNoPrimary(t *testing.T) {
	h := NewHandler(func(data []byte) error { return nil }, testLogger)

	// Send caps
	capsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(capsData[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(capsData[4:8], 16)
	binary.LittleEndian.PutUint32(capsData[8:12], 16)
	binary.LittleEndian.PutUint32(capsData[12:16], 25600000)
	h.ProcessPDU(capsData)

	monitors := []MonitorLayout{
		{Flags: 0, Width: 1920, Height: 1080},
		{Flags: 0, Left: 1920, Width: 1920, Height: 1080},
	}
	if err := h.ResizeMulti(monitors); err == nil {
		t.Error("expected error when no primary monitor")
	}
}

func TestHandlerResizeMultiConstraints(t *testing.T) {
	var sent []byte
	h := NewHandler(func(data []byte) error {
		sent = make([]byte, len(data))
		copy(sent, data)
		return nil
	}, testLogger)

	// Send caps
	capsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(capsData[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(capsData[4:8], 16)
	binary.LittleEndian.PutUint32(capsData[8:12], 16)
	binary.LittleEndian.PutUint32(capsData[12:16], 25600000)
	h.ProcessPDU(capsData)

	// Odd width and out-of-range dimensions
	monitors := []MonitorLayout{
		{Flags: MonitorLayoutPrimary, Width: 1921, Height: 100}, // odd width, too small height
	}
	if err := h.ResizeMulti(monitors); err != nil {
		t.Fatalf("ResizeMulti error: %v", err)
	}

	off := 16
	w := binary.LittleEndian.Uint32(sent[off+12:])
	if w != 1920 { // 1921 forced even → 1920
		t.Errorf("width = %d, want 1920 (forced even)", w)
	}
	hh := binary.LittleEndian.Uint32(sent[off+16:])
	if hh != 200 { // 100 clamped to 200
		t.Errorf("height = %d, want 200 (clamped)", hh)
	}
}

func TestHandlerResize(t *testing.T) {
	var sent []byte
	h := NewHandler(func(data []byte) error {
		sent = make([]byte, len(data))
		copy(sent, data)
		return nil
	}, testLogger)

	// Should fail before caps
	if err := h.Resize(1920, 1080); err == nil {
		t.Error("expected error before caps received")
	}

	// Send caps
	capsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(capsData[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(capsData[4:8], 16)
	binary.LittleEndian.PutUint32(capsData[8:12], 16)
	binary.LittleEndian.PutUint32(capsData[12:16], 25600000)
	h.ProcessPDU(capsData)

	if err := h.Resize(2560, 1440); err != nil {
		t.Fatalf("Resize error: %v", err)
	}
	if sent == nil {
		t.Fatal("no PDU sent")
	}

	pduType := binary.LittleEndian.Uint32(sent[0:4])
	if pduType != TypeMonitorLayout {
		t.Errorf("type = 0x%08X, want MonitorLayout", pduType)
	}
	off := 16
	w := binary.LittleEndian.Uint32(sent[off+12:])
	if w != 2560 {
		t.Errorf("width = %d, want 2560", w)
	}
	hh := binary.LittleEndian.Uint32(sent[off+16:])
	if hh != 1440 {
		t.Errorf("height = %d, want 1440", hh)
	}
	// 2560x1440 at 100% (96 DPI): round(2560 * 25.4 / 96) = 677 mm, round(1440 * 25.4 / 96) = 381 mm
	physW := binary.LittleEndian.Uint32(sent[off+20:])
	if physW != 677 {
		t.Errorf("physicalWidth = %d, want 677", physW)
	}
	physH := binary.LittleEndian.Uint32(sent[off+24:])
	if physH != 381 {
		t.Errorf("physicalHeight = %d, want 381", physH)
	}
}

func TestHandlerResizeWithDPI(t *testing.T) {
	var sent []byte
	h := NewHandler(func(data []byte) error {
		sent = make([]byte, len(data))
		copy(sent, data)
		return nil
	}, testLogger)

	capsData := make([]byte, 16)
	binary.LittleEndian.PutUint32(capsData[0:4], TypeCaps)
	binary.LittleEndian.PutUint32(capsData[4:8], 16)
	binary.LittleEndian.PutUint32(capsData[8:12], 16)
	binary.LittleEndian.PutUint32(capsData[12:16], 25600000)
	h.ProcessPDU(capsData)

	if err := h.ResizeWithDPI(1920, 1080, 150, 100); err != nil {
		t.Fatalf("ResizeWithDPI error: %v", err)
	}
	off := 16
	physW := binary.LittleEndian.Uint32(sent[off+20:])
	physH := binary.LittleEndian.Uint32(sent[off+24:])
	dScale := binary.LittleEndian.Uint32(sent[off+32:])
	devScale := binary.LittleEndian.Uint32(sent[off+36:])
	if physW != 339 || physH != 191 || dScale != 150 || devScale != 100 {
		t.Errorf("got phys=%dx%d scale=%d/%d, want 339x191 150/100", physW, physH, dScale, devScale)
	}
}
