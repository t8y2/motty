// Package disp implements the MS-RDPEDISP (Display Update Virtual Channel
// Extension) protocol for dynamic session resize.
//
// The server opens the dynamic channel "Microsoft::Windows::RDS::DisplayControl"
// via DRDYNVC. The client can then send monitor layout PDUs to request
// resolution changes. The server responds by deactivating and reactivating
// the session at the new resolution.
package disp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"

	"motty/internal/rdpcore/util"
)

// Dynamic channel name for display control.
const ChannelName = "Microsoft::Windows::RDS::DisplayControl"

// PDU type constants.
const (
	TypeCaps          uint32 = 0x00000005
	TypeMonitorLayout uint32 = 0x00000002
)

// CapsPDU represents the server's DISPLAYCONTROL_CAPS_PDU.
type CapsPDU struct {
	MaxNumMonitors     uint32
	MaxMonitorAreaSize uint32 // max total pixels across all monitors
}

// MonitorLayout represents a single monitor in DISPLAYCONTROL_MONITOR_LAYOUT.
type MonitorLayout struct {
	Flags              uint32 // 0x01 = primary
	Left               int32
	Top                int32
	Width              uint32
	Height             uint32
	PhysicalWidth      uint32 // mm
	PhysicalHeight     uint32 // mm
	Orientation        uint32 // 0, 90, 180, 270
	DesktopScaleFactor uint32 // 100-500
	DeviceScaleFactor  uint32 // 100, 140, 180
}

// monitorLayoutSize is the wire size of each monitor entry (40 bytes).
const monitorLayoutSize = 40

// DecodeCaps parses a DISPLAYCONTROL_CAPS_PDU from channel data.
// Wire format: type(4) + length(4) + maxNumMonitors(4) + maxMonitorArea(4)
func DecodeCaps(data []byte) (*CapsPDU, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("disp caps PDU too short: %d bytes", len(data))
	}
	pduType := binary.LittleEndian.Uint32(data[0:4])
	if pduType != TypeCaps {
		return nil, fmt.Errorf("disp: expected caps type 0x%08X, got 0x%08X", TypeCaps, pduType)
	}
	return &CapsPDU{
		MaxNumMonitors:     binary.LittleEndian.Uint32(data[8:12]),
		MaxMonitorAreaSize: binary.LittleEndian.Uint32(data[12:16]),
	}, nil
}

// EncodeMonitorLayout builds a DISPLAYCONTROL_MONITOR_LAYOUT_PDU.
// Wire format: type(4) + length(4) + monitorLayoutSize(4) + numMonitors(4) + monitors(40*n)
func EncodeMonitorLayout(monitors []MonitorLayout) []byte {
	n := len(monitors)
	totalLen := 16 + n*monitorLayoutSize
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(buf[0:4], TypeMonitorLayout)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(totalLen))
	binary.LittleEndian.PutUint32(buf[8:12], monitorLayoutSize)
	binary.LittleEndian.PutUint32(buf[12:16], uint32(n))

	off := 16
	for i := range monitors {
		m := &monitors[i]
		binary.LittleEndian.PutUint32(buf[off:], m.Flags)
		binary.LittleEndian.PutUint32(buf[off+4:], uint32(m.Left))
		binary.LittleEndian.PutUint32(buf[off+8:], uint32(m.Top))
		binary.LittleEndian.PutUint32(buf[off+12:], m.Width)
		binary.LittleEndian.PutUint32(buf[off+16:], m.Height)
		binary.LittleEndian.PutUint32(buf[off+20:], m.PhysicalWidth)
		binary.LittleEndian.PutUint32(buf[off+24:], m.PhysicalHeight)
		binary.LittleEndian.PutUint32(buf[off+28:], m.Orientation)
		binary.LittleEndian.PutUint32(buf[off+32:], m.DesktopScaleFactor)
		binary.LittleEndian.PutUint32(buf[off+36:], m.DeviceScaleFactor)
		off += monitorLayoutSize
	}
	return buf
}

// Handler manages the RDPEDISP protocol over a dynamic virtual channel.
type Handler struct {
	sendFn  func([]byte) error // sends on the RDPEDISP dynamic channel
	log     *slog.Logger
	caps    *CapsPDU
	ready   bool   // true after caps exchange
	onReady func() // called once when caps are received
}

// NewHandler creates an RDPEDISP handler.
// sendFn writes data to the display control dynamic channel.
func NewHandler(sendFn func([]byte) error, log *slog.Logger) *Handler {
	return &Handler{sendFn: sendFn, log: log}
}

// ProcessPDU handles data received on the display control dynamic channel.
func (h *Handler) ProcessPDU(data []byte) {
	if len(data) < 4 {
		return
	}
	pduType := binary.LittleEndian.Uint32(data[0:4])
	switch pduType {
	case TypeCaps:
		caps, err := DecodeCaps(data)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "caps decode error", slog.Any("err", err))
			return
		}
		h.caps = caps
		h.ready = true
		h.log.LogAttrs(context.Background(), slog.LevelInfo, "server caps", slog.Int("maxMonitors", int(caps.MaxNumMonitors)), slog.Int("maxArea", int(caps.MaxMonitorAreaSize)))
		if h.onReady != nil {
			h.onReady()
			h.onReady = nil // fire once
		}
	default:
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "unknown PDU type", util.Hex8("type", pduType))
	}
}

// OnReady sets a callback that fires once when the server sends its capabilities.
// If caps were already received, fn is called immediately.
func (h *Handler) OnReady(fn func()) {
	if h.ready {
		fn()
		return
	}
	h.onReady = fn
}

// Ready returns true after the server has sent its capabilities.
func (h *Handler) Ready() bool {
	return h.ready
}

// Caps returns the server's display capabilities, or nil if not yet received.
func (h *Handler) Caps() *CapsPDU {
	return h.caps
}

// physicalDimensionMM returns the physical length in millimeters for pixels at
// the given desktop scale percentage (100% = 96 DPI), matching Windows' own
// RDP client and FreeRDP (MS-RDPBCGR 2.2.1.3.2, MS-RDPEDISP 2.2.2.2.1).
func physicalDimensionMM(pixels, desktopScale uint32) uint32 {
	if desktopScale == 0 {
		desktopScale = 100
	}
	dpi := float64(96) * float64(desktopScale) / 100.0
	return uint32(math.Round(float64(pixels) * 25.4 / dpi))
}

// Resize sends a monitor layout PDU requesting a single-monitor resize.
// Width is forced to even (per MS-RDPEDISP requirement) and both
// dimensions are clamped to [200, 8192]. Physical dimensions are computed
// at 96 DPI (100% scale).
func (h *Handler) Resize(width, height uint32) error {
	return h.ResizeWithDPI(width, height, 100, 100)
}

// clampDim clamps a dimension to [200, 8192].
func clampDim(v uint32) uint32 {
	if v < 200 {
		return 200
	}
	if v > 8192 {
		return 8192
	}
	return v
}

// MonitorLayoutPrimary is the flag for the primary monitor.
const MonitorLayoutPrimary uint32 = 0x01

// ResizeMulti sends a monitor layout PDU for multiple monitors.
// Exactly one monitor must have the PRIMARY flag (0x01). Each monitor's
// width is forced even, dimensions are clamped to [200, 8192], and physical
// dimensions are computed from the monitor's desktop scale (default 100% = 96 DPI)
// when missing.
func (h *Handler) ResizeMulti(monitors []MonitorLayout) error {
	if !h.ready {
		return fmt.Errorf("disp: not ready (no server caps received)")
	}
	if len(monitors) == 0 {
		return fmt.Errorf("disp: no monitors provided")
	}

	// Single pass: validate primary count + apply constraints
	primaryCount := 0
	for i := range monitors {
		m := &monitors[i]
		if m.Flags&MonitorLayoutPrimary != 0 {
			primaryCount++
		}
		m.Width = clampDim(m.Width)
		m.Height = clampDim(m.Height)
		m.Width &^= 1 // force even

		if m.DesktopScaleFactor == 0 {
			m.DesktopScaleFactor = 100
		}
		if m.DeviceScaleFactor == 0 {
			m.DeviceScaleFactor = 100
		}
		if m.PhysicalWidth == 0 {
			m.PhysicalWidth = physicalDimensionMM(m.Width, m.DesktopScaleFactor)
		}
		if m.PhysicalHeight == 0 {
			m.PhysicalHeight = physicalDimensionMM(m.Height, m.DesktopScaleFactor)
		}
	}
	if primaryCount != 1 {
		return fmt.Errorf("disp: exactly one primary monitor required, got %d", primaryCount)
	}

	data := EncodeMonitorLayout(monitors)
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "sending monitor layout", slog.Int("monitors", len(monitors)))
	return h.sendFn(data)
}

// ResizeWithDPI sends a monitor layout PDU with DPI scale factors.
func (h *Handler) ResizeWithDPI(width, height, desktopScale, deviceScale uint32) error {
	if !h.ready {
		return fmt.Errorf("disp: not ready (no server caps received)")
	}
	if desktopScale == 0 {
		desktopScale = 100
	}
	if deviceScale == 0 {
		deviceScale = 100
	}
	// Clamp and force even width
	width = clampDim(width)
	height = clampDim(height)
	width &^= 1
	// Compute physical dimensions from DPI scale
	physW := physicalDimensionMM(width, desktopScale)
	physH := physicalDimensionMM(height, desktopScale)

	monitors := []MonitorLayout{{
		Flags:              0x01, // primary
		Width:              width,
		Height:             height,
		PhysicalWidth:      physW,
		PhysicalHeight:     physH,
		DesktopScaleFactor: desktopScale,
		DeviceScaleFactor:  deviceScale,
	}}
	data := EncodeMonitorLayout(monitors)
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "sending monitor layout", slog.Int("width", int(width)), slog.Int("height", int(height)), slog.Int("physW", int(physW)), slog.Int("physH", int(physH)), slog.Int("desktopScale", int(desktopScale)), slog.Int("deviceScale", int(deviceScale)))
	return h.sendFn(data)
}
