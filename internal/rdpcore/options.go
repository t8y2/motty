package rdp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// LevelTrace is a custom slog level for per-tile/pixel diagnostic dumps.
const LevelTrace = slog.LevelDebug - 4

// KeyboardMode controls how keyboard input is sent to the server.
type KeyboardMode int

const (
	KeyboardScancode KeyboardMode = iota // Send physical scancodes (remote layout determines characters)
	KeyboardUnicode                      // Send unicode codepoints for printable chars (local layout)
)

// SecurityMode chooses the security protocols the connection may negotiate.
type SecurityMode int

const (
	// SecurityNegotiate asks for Enhanced RDP Security: TLS with NLA
	// (CredSSP) first, then TLS alone. A server that offers only Standard RDP
	// Security is refused: that layer derives its keys from a server key that
	// is never authenticated, so anyone on the path can read the credentials
	// and the session.
	SecurityNegotiate SecurityMode = iota
	// SecurityStandard asks for Standard RDP Security only, with no TLS and no
	// NLA. Older servers, and servers whose administrator configured the RDP
	// security layer, require it. The traffic is RC4-encrypted, but from an
	// unauthenticated server key, so it is not protection against an attacker
	// on the path.
	SecurityStandard
)

// Options configures the RDP client connection.
//
// Motty divergence from upstream: Dial and VerifyCertificate replace the
// hard-coded net.DialTimeout and tls.Config{InsecureSkipVerify: true}; the
// audio, drive, printer, serial, parallel, smartcard, USB and camera options
// are gone with their packages. See ../../docs/remote-desktop.md.
type Options struct {
	// Connection
	Host string // Target hostname or IP (TLS server name, NLA target)
	Port int    // Target port (default: 3389)

	// Dial opens the byte stream to Host:Port. Nil dials TCP directly. The
	// client calls it again if the server needs a second connection (TLS
	// version fallback).
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// VerifyCertificate is called with the DER of the server's certificate
	// after every TLS handshake, before any credential is sent. Returning an
	// error aborts the connection. Nil accepts any certificate: callers must
	// set it.
	VerifyCertificate func(der []byte) error

	// Security selects the protocol the client may negotiate. The zero value
	// refuses Standard RDP Security, whose keys are not authenticated.
	Security SecurityMode

	// Authentication
	Username string // Login username
	Password string // Login password
	Domain   string // Windows domain (optional)

	// Display
	Width              uint16 // Desktop width in pixels
	Height             uint16 // Desktop height in pixels
	Depth              uint16 // Color depth (8, 15, 16, 24, or 32)
	DesktopScaleFactor uint32 // DPI scale percent (100–500, 0 = don't send)
	DeviceScaleFactor  uint32 // Device scale (100, 140, or 180, 0 = don't send)
	GFX                bool   // Enable RDPGFX graphics pipeline (EGFX)
	NoAVC              bool   // Disable H.264/AVC codec (force RemoteFX/ClearCodec)

	// Experience (visual effects)
	Wallpaper          bool // Show desktop wallpaper
	FullWindowDrag     bool // Show window contents while dragging
	MenuAnimations     bool // Enable menu animations
	Theming            bool // Enable visual themes (Aero/Luna)
	CursorShadow       bool // Show cursor shadow
	CursorSettings     bool // Apply cursor blink settings
	FontSmoothing      bool // Enable ClearType font smoothing
	DesktopComposition bool // Enable Desktop Window Manager (Aero Glass)

	// Redirect
	Clipboard bool // Redirect clipboard (default: true)

	// Session
	ConsoleSession bool // Connect to the console (admin) session

	// Heartbeat — detects silent connection loss. Sends keepalive at half
	// the timeout to provoke server responses. 0 = disabled.
	HeartbeatTimeout time.Duration // Max time without server data before disconnect (default: 10s)

	// Auto-reconnect
	AutoReconnect        bool // Reconnect automatically on connection loss
	MaxReconnectAttempts int  // Max reconnect attempts (0 = unlimited)

	// Multi-monitor
	// When set, defines the pre-connect monitor topology sent via TS_UD_CS_MONITOR.
	// Exactly one monitor must be marked Primary and must be at position (0,0).
	// When nil, single-monitor behavior is used (Width x Height).
	Monitors []MonitorConfig

	// Logging
	Logger *slog.Logger // nil = silent (discard handler)

	// Input
	KeyboardMode KeyboardMode // Keyboard input mode (default: KeyboardScancode)

	// Advanced
	Cookie string // Routing cookie (optional)
}

// MonitorConfig describes a single monitor in a multi-monitor topology.
type MonitorConfig struct {
	X, Y          int  // Position in virtual desktop (pixels)
	Width, Height int  // Resolution (pixels)
	Primary       bool // Primary monitor (exactly one required)
}

// DefaultOptions returns Options with sensible defaults.
func DefaultOptions() *Options {
	return &Options{
		Logger:           slog.New(slog.DiscardHandler),
		Port:             3389,
		Width:            1600,
		Height:           900,
		Depth:            32,
		Clipboard:        true,
		Theming:          true,
		CursorShadow:     true,
		CursorSettings:   true,
		FontSmoothing:    true,
		HeartbeatTimeout: 10 * time.Second,
	}
}

// Validate checks that required options are set
func (o *Options) Validate() error {
	if o.Host == "" {
		return ErrNoHost
	}
	if o.Port <= 0 || o.Port > 65535 {
		o.Port = 3389
	}
	if o.Width == 0 {
		o.Width = 1024
	}
	if o.Height == 0 {
		o.Height = 768
	}
	if o.Depth == 0 {
		o.Depth = 32
	}
	// Validate DPI scale: only Windows-supported levels are accepted.
	if o.DesktopScaleFactor != 0 {
		switch o.DesktopScaleFactor {
		case 100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, 500:
			// valid
		default:
			return fmt.Errorf("desktop-scale %d is not supported; use 100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, or 500", o.DesktopScaleFactor)
		}
	}
	if o.DeviceScaleFactor != 0 && o.DeviceScaleFactor != 100 && o.DeviceScaleFactor != 140 && o.DeviceScaleFactor != 180 {
		o.DeviceScaleFactor = 0
	}
	// Validate multi-monitor topology
	if len(o.Monitors) > 0 {
		primaryCount := 0
		for i := range o.Monitors {
			m := &o.Monitors[i]
			if m.Primary {
				primaryCount++
				if m.X != 0 || m.Y != 0 {
					return ErrMonitorPrimaryPosition
				}
			}
			// Force even width
			m.Width &^= 1
		}
		if primaryCount != 1 {
			return ErrMonitorPrimaryCount
		}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return nil
}
