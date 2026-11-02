// Package rdp provides a pure Go implementation of the Microsoft Remote Desktop Protocol client.
//
// This package implements MS-RDPBCGR (Remote Desktop Protocol: Basic Connectivity and Graphics Remoting)
// and related specifications to enable connecting to Windows Remote Desktop servers.
//
// Basic usage:
//
//	client, err := rdp.NewClient(&rdp.Options{
//	    Host:     "192.168.1.100",
//	    Username: "admin",
//	    Password: "secret",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	client.OnBitmap(func(update *rdp.BitmapUpdate) {
//	    // Handle bitmap
//	})
//
//	if err := client.Connect(); err != nil {
//	    log.Fatal(err)
//	}
package rdp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"motty/internal/rdpcore/protocol/ber"
	"motty/internal/rdpcore/protocol/caps"
	"motty/internal/rdpcore/protocol/clearcodec"
	"motty/internal/rdpcore/protocol/cliprdr"
	"motty/internal/rdpcore/protocol/disp"
	"motty/internal/rdpcore/protocol/dvc"
	"motty/internal/rdpcore/protocol/egfx"
	"motty/internal/rdpcore/protocol/fastpath"
	"motty/internal/rdpcore/protocol/lic"
	"motty/internal/rdpcore/protocol/mcs"
	"motty/internal/rdpcore/protocol/mppc"
	"motty/internal/rdpcore/protocol/nla"
	"motty/internal/rdpcore/protocol/nscodec"
	"motty/internal/rdpcore/protocol/orders"
	"motty/internal/rdpcore/protocol/pdu"
	"motty/internal/rdpcore/protocol/pointer"
	"motty/internal/rdpcore/protocol/rle"
	"motty/internal/rdpcore/protocol/sec"
	"motty/internal/rdpcore/protocol/tpkt"
	"motty/internal/rdpcore/protocol/x224"
	"motty/internal/rdpcore/util"
)

// ConnectionState represents the current state of the RDP connection
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateX224Negotiation
	StateTLSHandshake
	StateCredSSPAuth
	StateMCSConnect
	StateSecurityExchange
	StateLicensing
	StateCapabilities
	StateFinalization
	StateConnected
)

// String returns a human-readable state name
func (s ConnectionState) String() string {
	names := []string{
		"Disconnected",
		"Connecting",
		"X.224 Negotiation",
		"TLS Handshake",
		"CredSSP Auth",
		"MCS Connect",
		"Security Exchange",
		"Licensing",
		"Capabilities",
		"Finalization",
		"Connected",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("Unknown(%d)", s)
}

// BitmapUpdate represents a screen update from the server
type BitmapUpdate struct {
	X            int    // X position
	Y            int    // Y position
	Width        int    // Bitmap width
	Height       int    // Bitmap height
	BitsPerPixel int    // Color depth
	IsCompressed bool   // Whether Data is RLE-compressed
	TopDown      bool   // True for EGFX (top-down); false for legacy (bottom-up)
	Data         []byte // Raw pixel data (may be RLE-compressed)
}

// Pointer update types for PointerUpdate.Type
const (
	PointerNull    byte = 0 // Hide cursor
	PointerDefault byte = 1 // Restore default OS cursor
	PointerShape   byte = 2 // New cursor shape (Data contains RGBA)
	PointerCached  byte = 3 // Use cached cursor by CacheIndex
)

// PointerUpdate represents a cursor shape change from the server.
type PointerUpdate struct {
	Type       byte   // PointerNull, PointerDefault, PointerShape, or PointerCached
	CacheIndex uint16 // Cache slot index
	HotSpotX   uint16 // Hotspot X offset
	HotSpotY   uint16 // Hotspot Y offset
	Width      uint16 // Cursor width
	Height     uint16 // Cursor height
	Data       []byte // Top-down RGBA pixel data (only for PointerShape)
}

// Client is an RDP client connection
type Client struct {
	opts    *Options
	log     *slog.Logger
	logTpkt *slog.Logger
	logNla  *slog.Logger
	logX224 *slog.Logger
	logMcs  *slog.Logger
	logSec  *slog.Logger
	logLic  *slog.Logger
	logPdu  *slog.Logger
	logFp   *slog.Logger
	logPtr  *slog.Logger
	state   ConnectionState
	stateMu sync.RWMutex

	// Network
	conn     net.Conn
	tpktConn *tpkt.Conn
	tlsConn  *tls.Conn
	writeMu  sync.Mutex // serializes all outgoing writes

	// Negotiated parameters
	selectedProtocol uint32
	tlsMinVersion    uint16 // override for TLS version probing; 0 = default
	tlsMaxVersion    uint16 // override for TLS version probing; 0 = default
	credsspVersion   int    // CredSSP version to advertise; 0 = use MaxCredSSPVersion

	// MCS/GCC parameters
	userChannelID   uint16
	ioChannelID     uint16
	channelIDs      []uint16
	channelNames    []string                        // ordered, matching channelIDs indices
	channelMap      map[uint16]string               // channelID → name (built after MCS Connect)
	channelOpts     map[uint16]uint32               // channelID → channel options
	channelHandlers map[string]func(uint16, []byte) // name → handler
	serverCore      *mcs.ServerCoreData
	serverSec       *mcs.ServerSecurityData

	// Standard RDP Security (nil when using TLS)
	crypto *sec.RDPCrypto

	// Auto-reconnect cookie (MS-RDPBCGR 5.5)
	arcCookie    *sec.AutoReconnectCookie // set when server sends ARC_SC_PRIVATE_PACKET
	clientRandom [32]byte                 // preserved for HMAC verifier on reconnect

	// Dynamic virtual channels (DRDYNVC)
	dvc          *dvc.Handler
	onDynChannel func(name string, ch *dvc.DynChannel)

	// Clipboard (MS-RDPECLIP)
	clipHandler *cliprdr.Handler

	// RDPGFX graphics pipeline (MS-RDPEGFX)
	egfxHandler *egfx.Handler

	// Display control (MS-RDPEDISP)
	dispHandler *disp.Handler
	// dispReady is set once the server's display control capabilities have
	// arrived: from then on Resize changes the desktop without reconnecting.
	dispReady atomic.Bool
	// displayMu guards the desktop size and scale in opts and the queued resize
	// against the receive goroutine, which reads and writes them when the server
	// resets its graphics or reactivates the session.
	displayMu       sync.Mutex
	pendingResizeW  uint16               // queued resize width (0 = none)
	pendingResizeH  uint16               // queued resize height
	pendingMonitors []disp.MonitorLayout // queued multi-monitor resize (nil = none)

	// Share session
	shareID    atomic.Uint32
	serverCaps uint32 // bitfield of cap types advertised by server (bit N = type N)
	serverBpp  int    // server's advertised color depth (15, 16, 24, 32)

	// certErr is why Options.VerifyCertificate refused the server (Motty addition).
	certErr error

	// Reusable send buffer (zero allocs in steady state)
	sendBuf []byte

	// MPPC bulk data decompression (MS-RDPBCGR 3.1.8.4.1/3.1.8.4.2)
	mppcDecomp mppc.Decompressor

	// Reusable decompression buffer for RLE bitmap data
	decompBuf []byte

	// Reusable BitmapUpdate for EGFX callbacks (avoids per-call allocation)
	egfxBitmapUpdate BitmapUpdate

	// Reusable planes buffer for NSCodec decompression
	nscodecBuf []byte

	// GDI drawing orders
	orderState     orders.DecoderState // value — avoid alloc
	glyphCache     orders.GlyphCache   // value — avoid alloc
	glyphRenderBuf []byte              // reusable pixel buffer
	polyDeltaBuf   []int               // reusable delta buffer for Polyline
	clipActive     bool                // true when order has bounds → clip to clipRect
	clipLeft       int                 // clip rectangle (inclusive bounds)
	clipTop        int
	clipRight      int
	clipBottom     int

	// Framebuffer and bitmap cache
	framebufMu     sync.RWMutex             // protects framebuf during resize vs external reads
	framebuf       *Framebuffer             // client-side screen buffer (bottom-up BGRX)
	scrBltBuf      []byte                   // reusable scratch for ScrBlt overlap
	clipBuf        []byte                   // reusable buffer for sub-rect extraction during clipping
	bitmapCache    orders.BitmapCache       // bitmap cache (3 caches, value type)
	brushCache     orders.BrushCache        // brush cache (64 entries, value type)
	desktopSaveBuf *orders.DesktopSaveCache // desktop save cache for SaveBitmap orders

	// Pointer (cursor) cache and conversion buffer
	pointerCache [128]*PointerUpdate // matches advertised cache size
	pointerBuf   []byte              // reusable RGBA conversion buffer

	// Virtual channel reassembly (chunked VC data from server)
	vcReassembly      map[uint16][]byte // channelID → accumulated payload
	vcReassemblyTotal int               // total bytes across all reassembly buffers

	// Fast-path fragment reassembly
	fragBuf  []byte // reusable reassembly buffer (grows, reset via fragBuf[:0])
	fragCode byte   // update Code of the in-progress fragment sequence

	// Callbacks (protected by cbMu — use load*/On* methods, not direct access)
	cbMu              sync.RWMutex
	onBitmap          func(*BitmapUpdate)
	onStridedBitmap   func(x, y, w, h int, data []byte, stride int)
	onH264Frame       func(*egfx.H264Frame) // H.264 pass-through callback
	onBeginPaint      func()                // called before batch of display updates
	onEndPaint        func()                // called after batch of display updates
	painting          bool                  // true while inside beginPaint/endPaint (reentrant guard)
	onPointer         func(*PointerUpdate)
	onResize          func(width, height int)
	onDisconnect      func(error)
	onReconnecting    func()
	onReconnected     func()
	onClipboardUpdate func(hasText, hasImage bool)
	onClipboardText   func(text string)
	onClipboardImage  func(pngData []byte)

	// Heartbeat: last time we received any data from server (unix nanos)
	lastReceived atomic.Int64

	// Control
	done            chan struct{}
	receiveLoopDone chan struct{} // closed when receiveLoop exits
	closeMu         sync.Mutex
	closed          bool
	reconnecting    bool // true while reconnectLoop is running
}

// NewClient creates a new RDP client with the given options
func NewClient(opts *Options) (*Client, error) {
	if opts == nil {
		opts = DefaultOptions()
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	return &Client{
		opts:      opts,
		log:       opts.Logger.With("component", "RDP"),
		logTpkt:   opts.Logger.With("component", "TPKT"),
		logNla:    opts.Logger.With("component", "NLA"),
		logX224:   opts.Logger.With("component", "X224"),
		logMcs:    opts.Logger.With("component", "MCS"),
		logSec:    opts.Logger.With("component", "SEC"),
		logLic:    opts.Logger.With("component", "LIC"),
		logPdu:    opts.Logger.With("component", "PDU"),
		logFp:     opts.Logger.With("component", "FASTPATH"),
		logPtr:    opts.Logger.With("component", "POINTER"),
		done:      make(chan struct{}),
		serverBpp: int(opts.Depth), // default to requested depth, updated by server bitmap cap
	}, nil
}

// dialConn opens the transport with Options.Dial (Motty divergence), or TCP.
func (c *Client) dialConn(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if c.opts.Dial != nil {
		return c.opts.Dial(ctx, "tcp", addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// Connect establishes the RDP connection
func (c *Client) Connect() error {
	c.setState(StateConnecting)

	// Step 1: TCP connection
	addr := net.JoinHostPort(c.opts.Host, strconv.Itoa(c.opts.Port))
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "connecting", slog.String("addr", addr))

	conn, err := c.dialConn(addr)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}
	c.conn = conn
	c.tpktConn = tpkt.NewConn(conn, c.logTpkt)

	// Step 2: X.224 Connection Request with RDP negotiation.
	// Auto-fallback: try TLS+NLA first, then TLS-only, then legacy RDP Security.
	c.setState(StateX224Negotiation)
	if err := c.negotiateX224WithFallback(); err != nil {
		c.Close()
		return err
	}
	if c.selectedProtocol == x224.ProtocolRDP && !c.opts.AllowLegacySecurity {
		c.Close()
		return fmt.Errorf("%w: the server only offers standard RDP security, which is not encrypted against an attacker on the path", ErrNegotiationFailed)
	}

	// Step 3: TLS handshake (if TLS/CredSSP selected)
	var initialTLSErr error
	if c.selectedProtocol != x224.ProtocolRDP {
		c.setState(StateTLSHandshake)
		if err := c.upgradeTLS(); err != nil {
			if c.selectedProtocol != x224.ProtocolHybrid || errors.Is(err, ErrCertificateRejected) {
				c.Close()
				return err
			}
			// CredSSP path — initial TLS failed, will probe below.
			initialTLSErr = err
		}
	}

	// Step 3b: CredSSP/NLA authentication (if Hybrid selected).
	// Some servers fail CredSSP at certain TLS or CredSSP versions.
	// Probe: first try the natural TLS + max CredSSP version, then
	// iterate pinned TLS versions (1.3→1.0) × CredSSP versions (6→2).
	// If all NLA attempts fail, fall back to TLS-only.
	if c.selectedProtocol == x224.ProtocolHybrid {
		tlsVersions := []uint16{tls.VersionTLS13, tls.VersionTLS12, tls.VersionTLS11, tls.VersionTLS10}
		tlsNames := []string{"TLS 1.3", "TLS 1.2", "TLS 1.1", "TLS 1.0"}
		var credsspErr error
		if initialTLSErr != nil {
			credsspErr = initialTLSErr
		} else {
			credsspErr = c.tryCredSSP()
		}
		if credsspErr != nil && !errors.Is(credsspErr, nla.ErrCredentialsFatal) {
			credsspVersions := []int{6, 3, 2}
		probeLoop:
			for i, tlsVer := range tlsVersions {
				for _, csvVer := range credsspVersions {
					c.log.LogAttrs(context.Background(), slog.LevelInfo, "retrying CredSSP",
						slog.String("tls", tlsNames[i]), slog.Int("credsspVersion", csvVer))
					if reconnErr := c.reconnectWithTLS(x224.ProtocolSSL|x224.ProtocolHybrid, tlsVer, tlsVer); reconnErr != nil {
						if errors.Is(reconnErr, ErrCertificateRejected) {
							c.Close()
							return reconnErr
						}
						c.log.LogAttrs(context.Background(), slog.LevelDebug, "TLS handshake failed",
							slog.String("tls", tlsNames[i]), slog.Any("error", reconnErr))
						break // this TLS version not supported, try next
					}
					c.credsspVersion = csvVer
					credsspErr = c.tryCredSSP()
					if credsspErr == nil {
						break probeLoop
					}
					if errors.Is(credsspErr, nla.ErrCredentialsFatal) {
						break probeLoop
					}
				}
			}
			c.credsspVersion = 0 // reset
		}
		if credsspErr != nil && errors.Is(credsspErr, nla.ErrCredentialsFatal) {
			// Credentials are definitively wrong — no point retrying or falling back.
			c.Close()
			return fmt.Errorf("%w: %w", ErrAuthFailed, credsspErr)
		}
		if credsspErr != nil {
			// All NLA attempts failed. Try TLS-only fallback, probing versions.
			c.log.LogAttrs(context.Background(), slog.LevelWarn, "all CredSSP attempts failed, falling back to TLS-only",
				slog.Any("lastError", credsspErr))
			tlsFallbackOK := false
			// First try with default range (Go negotiates best version).
			if reconnErr := c.reconnectWithTLS(x224.ProtocolSSL, 0, 0); reconnErr == nil {
				tlsFallbackOK = true
			} else if errors.Is(reconnErr, ErrCertificateRejected) {
				c.Close()
				return reconnErr
			} else {
				// Probe pinned versions.
				for i, tlsVer := range tlsVersions {
					c.log.LogAttrs(context.Background(), slog.LevelInfo, "retrying TLS-only",
						slog.String("tls", tlsNames[i]))
					if reconnErr := c.reconnectWithTLS(x224.ProtocolSSL, tlsVer, tlsVer); reconnErr == nil {
						tlsFallbackOK = true
						break
					}
				}
			}
			if !tlsFallbackOK {
				// Server requires NLA — report the original CredSSP error.
				c.Close()
				return credsspErr
			}
		}
	}

	// Step 4: MCS Connect Initial and Response
	c.setState(StateMCSConnect)
	if err := c.mcsConnect(); err != nil {
		c.Close()
		return err
	}

	// Step 5: MCS Erect Domain, Attach User, Join Channels
	if err := c.mcsErectDomain(); err != nil {
		c.Close()
		return err
	}
	if err := c.mcsAttachUser(); err != nil {
		c.Close()
		return err
	}
	if err := c.mcsJoinChannels(); err != nil {
		c.Close()
		return err
	}

	// Step 6: Security Exchange
	c.setState(StateSecurityExchange)
	if err := c.securityExchange(); err != nil {
		c.Close()
		return err
	}

	// Step 6b: Client Info PDU (required for Standard RDP Security)
	if err := c.sendClientInfo(); err != nil {
		c.Close()
		return err
	}

	// Step 7: Licensing
	c.setState(StateLicensing)
	if err := c.handleLicensing(); err != nil {
		c.Close()
		return err
	}

	// Step 8: Capabilities Exchange
	c.setState(StateCapabilities)
	if err := c.handleCapabilitiesExchange(); err != nil {
		c.Close()
		return err
	}

	// Step 9: Connection Finalization
	c.setState(StateFinalization)
	if err := c.handleFinalization(); err != nil {
		c.Close()
		return err
	}

	// Initialize framebuffer and bitmap cache
	c.framebuf = NewFramebuffer(int(c.opts.Width), int(c.opts.Height))
	c.bitmapCache.Init([orders.NumBitmapCaches]int{600, 600, 2048, 4096, 2048})
	c.desktopSaveBuf = new(orders.DesktopSaveCache)

	c.log.LogAttrs(context.Background(), slog.LevelInfo, "connection established", slog.String("state", c.State().String()))
	c.setState(StateConnected)

	c.receiveLoopDone = make(chan struct{})
	go c.receiveLoop()
	if c.opts.HeartbeatTimeout > 0 {
		c.lastReceived.Store(time.Now().UnixNano())
		go c.keepAliveLoop()
		go c.heartbeatLoop()
	}

	return nil
}

// negotiateX224WithFallback tries progressively weaker security protocols:
// TLS+NLA → TLS-only → legacy RDP Security. Each downgrade requires a fresh
// TCP connection because the server closes the socket on negotiation failure.
func (c *Client) negotiateX224WithFallback() error {
	// Protocol sets to try, in order of preference.
	attempts := []struct {
		protos uint32
		label  string
	}{
		{x224.ProtocolSSL | x224.ProtocolHybrid, "TLS+NLA"},
		{x224.ProtocolSSL, "TLS"},
	}
	// Motty divergence: standard RDP Security (RC4 keyed from an unauthenticated
	// server key) lets anyone on the path read the password, and the negotiation
	// that selects it is itself unauthenticated, so it is only tried on request.
	if c.opts.AllowLegacySecurity {
		attempts = append(attempts, struct {
			protos uint32
			label  string
		}{x224.ProtocolRDP, "RDP Security"})
	}

	var lastErr error
	for i, a := range attempts {
		if i > 0 {
			// Reconnect TCP for the retry — server closed the previous socket.
			c.conn.Close()
			addr := net.JoinHostPort(c.opts.Host, strconv.Itoa(c.opts.Port))
			conn, err := c.dialConn(addr)
			if err != nil {
				return fmt.Errorf("%w: reconnect for %s: %v", ErrConnectionFailed, a.label, err)
			}
			c.conn = conn
			c.tpktConn = tpkt.NewConn(conn, c.logTpkt)
		}

		err := c.negotiateX224(a.protos)
		if err == nil {
			return nil
		}
		lastErr = err

		// Only retry on negotiation failure; other errors (I/O, parse) are fatal.
		if !errors.Is(err, ErrNegotiationFailed) {
			return err
		}
		c.logX224.LogAttrs(context.Background(), slog.LevelInfo, "protocol rejected, trying fallback",
			slog.String("tried", a.label), slog.Any("err", err))
	}
	return lastErr
}

// negotiateX224 performs a single X.224 connection negotiation attempt with
// the given requested protocols bitmask.
func (c *Client) negotiateX224(requestedProtos uint32) error {
	c.logX224.LogAttrs(context.Background(), slog.LevelDebug, "sending X.224 connection request",
		slog.String("protocols", x224.ProtocolName(requestedProtos)))

	// Build Connection Request with RDP negotiation.
	// Always send Cookie: mstshash= routing token (MS-RDPBCGR 2.2.1.1).
	cookie := c.opts.Cookie
	if cookie == "" {
		cookie = c.opts.Username
	}
	cr := &x224.ConnectionRequest{
		DestRef:         0,
		SrcRef:          0,
		Class:           0,
		Cookie:          cookie,
		SendCookie:      true,
		RequestedProtos: requestedProtos,
	}

	crBytes := cr.Encode()
	if err := c.tpktConn.Write(crBytes); err != nil {
		return fmt.Errorf("failed to send X.224 CR: %w", err)
	}

	resp, err := c.tpktConn.Read()
	if err != nil {
		return fmt.Errorf("failed to read X.224 CC: %w", err)
	}

	cc, err := x224.DecodeConnectionConfirm(c.logX224, resp)
	if err != nil {
		return fmt.Errorf("failed to parse X.224 CC: %w", err)
	}

	if cc.Type == x224.TypeRDPNegFail {
		return fmt.Errorf("%w: %s", ErrNegotiationFailed, x224.FailureReason(cc.FailureCode))
	}

	c.selectedProtocol = cc.SelectedProto
	c.logX224.LogAttrs(context.Background(), slog.LevelDebug, "server selected protocol",
		slog.String("protocol", x224.ProtocolName(c.selectedProtocol)))

	return nil
}

// upgradeTLS upgrades the connection to TLS
func (c *Client) upgradeTLS() error {
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "upgrading to TLS")

	tlsConfig := &tls.Config{
		// RDP servers use self-signed certificates, so the chain is not checked;
		// the caller pins the certificate itself through Options.VerifyCertificate
		// (Motty divergence: upstream accepted anything).
		InsecureSkipVerify: true,
		ServerName:         c.opts.Host,
		MinVersion:         tls.VersionTLS10, // Some RDP servers only support TLS 1.0
		VerifyConnection: func(cs tls.ConnectionState) error {
			if c.opts.VerifyCertificate == nil {
				return nil
			}
			if len(cs.PeerCertificates) == 0 {
				return errors.New("server presented no certificate")
			}
			if err := c.opts.VerifyCertificate(cs.PeerCertificates[0].Raw); err != nil {
				c.certErr = err
				return err
			}
			return nil
		},
	}

	// Apply version overrides from TLS probing, or cap at 1.2 for CredSSP.
	if c.tlsMinVersion != 0 {
		tlsConfig.MinVersion = c.tlsMinVersion
	}
	if c.tlsMaxVersion != 0 {
		tlsConfig.MaxVersion = c.tlsMaxVersion
	} else if c.selectedProtocol == x224.ProtocolHybrid {
		// Cap TLS at 1.2 when CredSSP/NLA is selected. Some Windows servers
		// fail to process CredSSP over TLS 1.3 (MS-CSSP was designed for 1.2).
		tlsConfig.MaxVersion = tls.VersionTLS12
	}

	tlsConn := tls.Client(c.conn, tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		if c.certErr != nil {
			// Motty divergence: a certificate the caller refused ends the attempt;
			// it is not a reason to try another protocol version.
			return fmt.Errorf("%w: %w", ErrCertificateRejected, c.certErr)
		}
		return fmt.Errorf("%w: %v", ErrTLSFailed, err)
	}

	c.tlsConn = tlsConn
	c.tpktConn.SetTCPConn(tlsConn)

	state := tlsConn.ConnectionState()
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "TLS handshake complete", util.Hex4("version", uint16(state.Version)), util.Hex4("cipher", uint16(state.CipherSuite)))
	return nil
}

// credsspAuth performs CredSSP/NLA authentication using NTLM over SPNEGO.
// tryCredSSP attempts CredSSP authentication on the current TLS connection.
func (c *Client) tryCredSSP() error {
	c.setState(StateCredSSPAuth)
	return c.credsspAuth()
}

// reconnectWithTLS tears down the current connection and establishes a new
// TCP → X.224 → TLS connection with the given protocol request and TLS version
// constraints. Pass minVer=0,maxVer=0 to use defaults.
func (c *Client) reconnectWithTLS(x224Protos uint32, minVer, maxVer uint16) error {
	c.conn.Close()
	addr := net.JoinHostPort(c.opts.Host, strconv.Itoa(c.opts.Port))
	conn, err := c.dialConn(addr)
	if err != nil {
		return fmt.Errorf("%w: reconnect: %v", ErrConnectionFailed, err)
	}
	c.conn = conn
	c.tlsConn = nil
	c.tpktConn = tpkt.NewConn(conn, c.logTpkt)

	c.setState(StateX224Negotiation)
	if err := c.negotiateX224(x224Protos); err != nil {
		return err
	}

	c.setState(StateTLSHandshake)
	c.tlsMinVersion = minVer
	c.tlsMaxVersion = maxVer
	if err := c.upgradeTLS(); err != nil {
		return err
	}
	return nil
}

func (c *Client) credsspAuth() error {
	ver := c.credsspVersion
	if ver == 0 {
		ver = nla.MaxCredSSPVersion
	}
	c.logNla.LogAttrs(context.Background(), slog.LevelInfo, "CredSSP/NLA authentication", slog.Int("advertiseVersion", ver))
	if err := nla.Authenticate(c.tlsConn, c.logNla, c.opts.Host, c.opts.Domain, c.opts.Username, c.opts.Password, ver); err != nil {
		// Motty divergence: %w, so that nla.ErrCredentialsFatal survives and the
		// caller stops at the first refusal. Upstream wrapped with %v, which hid it
		// and made one wrong password into 14 logon attempts (an account lockout
		// threshold of 10 is common).
		return fmt.Errorf("%w: %w", ErrAuthFailed, err)
	}
	c.logNla.LogAttrs(context.Background(), slog.LevelInfo, "CredSSP authentication successful")
	return nil
}

// mcsConnect performs the MCS Connect Initial/Response exchange
func (c *Client) mcsConnect() error {
	c.logMcs.LogAttrs(context.Background(), slog.LevelInfo, "MCS Connect Initial")

	// Build GCC client data blocks
	coreData := mcs.DefaultClientCoreData(c.opts.Width, c.opts.Height, c.opts.Depth, c.selectedProtocol)

	// RDP 10.0+ and monitor layout support are required for the server to
	// open the RDPEDISP dynamic channel, enabling mid-session resize.
	coreData.Version = mcs.VersionRDP10
	coreData.EarlyCapabilityFlags |= mcs.EarlyCapSupportMonitorLayout
	if c.opts.GFX {
		coreData.EarlyCapabilityFlags |= mcs.EarlyCapSupportDynvcGfxProtocol
	}

	// DPI scale factors (RDP 10.0+)
	if c.opts.DesktopScaleFactor > 0 {
		coreData.DesktopScaleFactor = c.opts.DesktopScaleFactor
		coreData.DeviceScaleFactor = c.opts.DeviceScaleFactor
		if coreData.DeviceScaleFactor == 0 {
			coreData.DeviceScaleFactor = 100
		}
		// Compute physical dimensions: width_px * 25.4 / (96 * scaleFactor/100)
		dpi := float64(96) * float64(c.opts.DesktopScaleFactor) / 100.0
		coreData.DesktopPhysicalWidth = uint32(float64(c.opts.Width) * 25.4 / dpi)
		coreData.DesktopPhysicalHeight = uint32(float64(c.opts.Height) * 25.4 / dpi)
	}

	// Per MS-RDPBCGR 2.2.1.3.3: when RDP_NEG_REQ was sent in the X.224 CR,
	// encryptionMethods SHOULD be set to 0 and extEncryptionMethods contains
	// the supported methods. If RDP_NEG_REQ was NOT sent (French locale only),
	// encryptionMethods contains the methods and extEncryptionMethods is 0.
	secData := &mcs.ClientSecurityData{
		EncryptionMethods:    0,
		ExtEncryptionMethods: 0x0000001B, // 40/56/128-bit + FIPS
	}
	clusterFlags := mcs.RedirectionSupported | (mcs.RedirectionVersion5 << 2)
	if c.opts.ConsoleSession {
		clusterFlags |= mcs.RedirectedSessionIDFieldValid
	}
	clusterData := &mcs.ClientClusterData{
		Flags: clusterFlags,
	}
	var channelDefs []mcs.ChannelDef
	if c.opts.Clipboard {
		channelDefs = append(channelDefs, mcs.ChannelDef{Name: "cliprdr", Options: mcs.ChannelOptionInitialized | mcs.ChannelOptionEncryptRDP | mcs.ChannelOptionCompressRDP | mcs.ChannelOptionShowProtocol})
	}
	// drdynvc must be the last static channel (matches MS reference client ordering).
	channelDefs = append(channelDefs, mcs.ChannelDef{Name: "drdynvc", Options: mcs.ChannelOptionInitialized | mcs.ChannelOptionCompress})
	// Audio channels are DVC-only (AUDIO_PLAYBACK_DVC, AUDIO_INPUT).
	netData := &mcs.ClientNetworkData{
		Channels: channelDefs,
	}
	// Store channel names and options in order for mapping after server responds
	c.channelNames = make([]string, len(channelDefs))
	channelDefOpts := make([]uint32, len(channelDefs))
	for i, ch := range channelDefs {
		c.channelNames[i] = ch.Name
		channelDefOpts[i] = ch.Options
	}

	// TS_UD_CS_MONITOR (0xC005) — multi-monitor topology
	var monitorBlock []byte
	if len(c.opts.Monitors) > 1 {
		n := len(c.opts.Monitors)
		defs := make([]mcs.MonitorDef, n)
		var minLeft, minTop int
		var maxRight, maxBottom int
		for i, m := range c.opts.Monitors {
			defs[i] = mcs.MonitorDef{
				Left:   int32(m.X),
				Top:    int32(m.Y),
				Right:  int32(m.X + m.Width - 1),
				Bottom: int32(m.Y + m.Height - 1),
			}
			if m.Primary {
				defs[i].Flags = mcs.MonitorPrimary
			}
			if m.X < minLeft {
				minLeft = m.X
			}
			if m.Y < minTop {
				minTop = m.Y
			}
			if r := m.X + m.Width; r > maxRight {
				maxRight = r
			}
			if b := m.Y + m.Height; b > maxBottom {
				maxBottom = b
			}
		}
		monitorBlock = (&mcs.ClientMonitorData{Monitors: defs}).EncodeBlock()
		c.opts.Width = uint16(maxRight - minLeft)
		c.opts.Height = uint16(maxBottom - minTop)
		coreData.DesktopWidth = c.opts.Width
		coreData.DesktopHeight = c.opts.Height
		// Recompute physical dimensions with updated desktop size.
		if c.opts.DesktopScaleFactor > 0 {
			dpi := float64(96) * float64(c.opts.DesktopScaleFactor) / 100.0
			coreData.DesktopPhysicalWidth = uint32(float64(c.opts.Width) * 25.4 / dpi)
			coreData.DesktopPhysicalHeight = uint32(float64(c.opts.Height) * 25.4 / dpi)
		}
	}

	// Assemble client data
	var clientData []byte
	clientData = append(clientData, coreData.EncodeBlock()...)
	clientData = append(clientData, clusterData.EncodeBlock()...)
	clientData = append(clientData, secData.EncodeBlock()...)
	clientData = append(clientData, netData.EncodeBlock()...)
	if monitorBlock != nil {
		clientData = append(clientData, monitorBlock...)
	}

	// Build GCC Conference Create Request
	gccRequest := mcs.EncodeGCCConferenceCreateRequest(clientData)

	// Build MCS Connect Initial
	ci := &mcs.ConnectInitial{
		CallingDomainSelector: []byte{1},
		CalledDomainSelector:  []byte{1},
		UpwardFlag:            true,
		TargetParameters: ber.DomainParameters{
			MaxChannelIDs:   34,
			MaxUserIDs:      2,
			MaxTokenIDs:     0,
			NumPriorities:   1,
			MinThroughput:   0,
			MaxHeight:       1,
			MaxMCSPDUSize:   65535,
			ProtocolVersion: 2,
		},
		MinimumParameters: ber.DomainParameters{
			MaxChannelIDs:   1,
			MaxUserIDs:      1,
			MaxTokenIDs:     1,
			NumPriorities:   1,
			MinThroughput:   0,
			MaxHeight:       1,
			MaxMCSPDUSize:   1056,
			ProtocolVersion: 2,
		},
		MaximumParameters: ber.DomainParameters{
			MaxChannelIDs:   65535,
			MaxUserIDs:      64535,
			MaxTokenIDs:     65535,
			NumPriorities:   1,
			MinThroughput:   0,
			MaxHeight:       1,
			MaxMCSPDUSize:   65535,
			ProtocolVersion: 2,
		},
		UserData: gccRequest,
	}

	// Wrap in X.224 Data TPDU and send via TPKT
	mcsData := ci.Encode()
	fullPkt := x224.EncodeDataTPDU(mcsData)
	if err := c.tpktConn.Write(fullPkt); err != nil {
		return fmt.Errorf("%w: failed to send MCS Connect Initial: %v", ErrMCSConnectFailed, err)
	}

	// Read MCS Connect Response
	resp, err := c.tpktConn.Read()
	if err != nil {
		return fmt.Errorf("%w: failed to read MCS Connect Response: %v", ErrMCSConnectFailed, err)
	}

	// Strip X.224 Data TPDU header
	mcsPayload, err := x224.DecodeDataTPDU(resp)
	if err != nil {
		return fmt.Errorf("%w: failed to parse X.224 DT: %v", ErrMCSConnectFailed, err)
	}

	// Parse MCS Connect Response (BER)
	cr, err := mcs.DecodeConnectResponse(c.logMcs, mcsPayload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSConnectFailed, err)
	}

	if cr.Result != 0 {
		return fmt.Errorf("%w: server returned result %d", ErrMCSConnectFailed, cr.Result)
	}

	// Extract GCC Conference Create Response user data
	gccUserData, err := mcs.DecodeGCCConferenceCreateResponse(cr.UserData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSConnectFailed, err)
	}

	// Parse server data blocks
	serverCore, serverSec, serverNet, err := mcs.DecodeServerData(c.logMcs, gccUserData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSConnectFailed, err)
	}

	c.serverCore = serverCore
	c.serverSec = serverSec
	c.ioChannelID = serverNet.IOChannelID
	c.channelIDs = serverNet.ChannelIDs

	// Build channel ID → name map and channel ID → options map
	c.channelMap = make(map[uint16]string, len(c.channelIDs))
	c.channelOpts = make(map[uint16]uint32, len(c.channelIDs))
	c.vcReassembly = make(map[uint16][]byte)
	for i, id := range c.channelIDs {
		if i < len(c.channelNames) {
			c.channelMap[id] = c.channelNames[i]
			c.channelOpts[id] = channelDefOpts[i]
			c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "channel mapped", slog.String("name", c.channelNames[i]), slog.Int("id", int(id)), util.Hex8("opts", uint32(channelDefOpts[i])))
		}
	}

	// Initialize DRDYNVC handler on the "drdynvc" static channel
	c.dvc = dvc.NewHandler(func(data []byte) error {
		return c.sendChannelData("drdynvc", data)
	}, c.opts.Logger.With("component", "DVC"), vcChunkSize)
	c.dvc.OnChannel(func(name string, ch *dvc.DynChannel) {
		// Reject MS-RDPEVOR (Video Optimized Remoting) channels.
		// The server uses these to attempt H.264 video offloading; if the
		// client accepts but doesn't respond, the server pauses EGFX frame
		// delivery for ~5-7s waiting for a codec response, causing video
		// freezes. Rejecting tells the server to stay on EGFX encoding.
		if name == "Microsoft::Windows::RDS::Video::Control::v08.01" ||
			name == "Microsoft::Windows::RDS::Video::Data::v08.01" {
			ch.Reject()
			return
		}
		// ECHO channel: echo received data back to server (round-trip keepalive)
		if name == "ECHO" {
			echoID := ch.ID
			ch.SetHandler(func(data []byte) {
				_ = c.dvc.SendData(echoID, data)
			})
		}
		// Auto-register RDPEDISP handler
		if name == disp.ChannelName {
			c.dispHandler = disp.NewHandler(func(data []byte) error {
				return c.dvc.SendData(ch.ID, data)
			}, c.opts.Logger.With("component", "DISP"))
			ch.SetHandler(func(data []byte) {
				if c.dispHandler == nil {
					return
				}
				c.dispHandler.ProcessPDU(data)
			})
			// Flush any pending resize once the server sends caps
			c.dispHandler.OnReady(func() {
				if c.dispHandler == nil {
					return
				}
				c.dispReady.Store(true)
				c.displayMu.Lock()
				defer c.displayMu.Unlock()
				if c.pendingMonitors != nil {
					monitors := c.pendingMonitors
					c.pendingMonitors = nil
					c.log.LogAttrs(context.Background(), slog.LevelDebug, "flushing queued multi-monitor resize", slog.Int("monitors", len(monitors)))
					if err := c.dispHandler.ResizeMulti(monitors); err != nil {
						c.log.LogAttrs(context.Background(), slog.LevelError, "queued multi-monitor resize failed", slog.Any("err", err))
					}
				} else if c.pendingResizeW > 0 && c.pendingResizeH > 0 {
					w, h := c.pendingResizeW, c.pendingResizeH
					c.pendingResizeW = 0
					c.pendingResizeH = 0
					c.log.LogAttrs(context.Background(), slog.LevelDebug, "flushing queued resize", slog.Int("width", int(w)), slog.Int("height", int(h)))
					var resizeErr error
					if c.opts.DesktopScaleFactor > 0 {
						resizeErr = c.dispHandler.ResizeWithDPI(uint32(w), uint32(h), c.opts.DesktopScaleFactor, c.opts.DeviceScaleFactor)
					} else {
						resizeErr = c.dispHandler.Resize(uint32(w), uint32(h))
					}
					if resizeErr != nil {
						c.log.LogAttrs(context.Background(), slog.LevelError, "queued resize failed", slog.Any("err", resizeErr))
					}
				}
			})
			c.log.LogAttrs(context.Background(), slog.LevelInfo, "display control channel opened", slog.Int("id", int(ch.ID)))
		}
		// RDPGFX graphics pipeline (MS-RDPEGFX)
		if name == egfx.ChannelName {
			c.egfxHandler = egfx.NewHandler(func(data []byte) error {
				return c.dvc.SendData(ch.ID, data)
			}, c.opts.Logger.With("component", "EGFX"))
			c.egfxHandler.SetClearCodecDecoder(clearcodec.New(c.opts.Logger.With("component", "CLEARCODEC")))
			if c.loadOnH264Frame() != nil && !c.opts.NoAVC {
				c.egfxHandler.SetAVCEnabled(true)
				c.egfxHandler.OnH264Frame(func(f *egfx.H264Frame) {
					c.loadOnH264Frame()(f)
				})
			}
			c.egfxHandler.OnStridedBitmap(func(surfID uint16, x, y, w, h int, data []byte, stride int) {
				if c.framebuf != nil {
					c.framebuf.WriteRectStridedTopDown(x, y, w, h, data, stride)
				}
				if fn := c.loadOnStridedBitmap(); fn != nil {
					fn(x, y, w, h, data, stride)
				}
			})
			if c.loadOnBitmap() != nil && c.loadOnStridedBitmap() == nil {
				c.egfxHandler.OnBitmap(func(surfID uint16, x, y, w, h int, data []byte) {
					c.egfxBitmapUpdate = BitmapUpdate{
						X: x, Y: y, Width: w, Height: h,
						BitsPerPixel: 32, TopDown: true, Data: data,
					}
					if fn := c.loadOnBitmap(); fn != nil {
						fn(&c.egfxBitmapUpdate)
					}
				})
			}
			c.egfxHandler.OnBeginPaint(func() {
				c.beginPaint()
			})
			c.egfxHandler.OnEndPaint(func() {
				c.endPaint()
			})
			c.egfxHandler.OnResetGraphics(func(w, h int) {
				// The server picks this size, so hold it to what a display can
				// show rather than letting one PDU allocate the frame buffer.
				if w <= 0 || h <= 0 {
					c.log.LogAttrs(context.Background(), slog.LevelWarn, "ResetGraphics with an empty desktop, ignoring",
						slog.Int("width", w), slog.Int("height", h))
					return
				}
				if w > MaxDesktopSize || h > MaxDesktopSize {
					c.log.LogAttrs(context.Background(), slog.LevelWarn, "ResetGraphics desktop larger than the client can show, clamping",
						slog.Int("width", w), slog.Int("height", h), slog.Int("limit", MaxDesktopSize))
					w, h = min(w, MaxDesktopSize), min(h, MaxDesktopSize)
				}
				c.log.LogAttrs(context.Background(), slog.LevelDebug, "EGFX ResetGraphics callback", slog.Int("width", w), slog.Int("height", h), slog.Bool("hasFramebuf", c.framebuf != nil))
				c.displayMu.Lock()
				c.opts.Width = uint16(w)
				c.opts.Height = uint16(h)
				c.displayMu.Unlock()
				c.framebufMu.Lock()
				if c.framebuf != nil {
					c.framebuf.Resize(w, h)
				}
				c.framebufMu.Unlock()
				if fn := c.loadOnResize(); fn != nil {
					fn(w, h)
				}
			})
			ch.SetHandler(func(data []byte) {
				if c.egfxHandler == nil {
					return
				}
				c.log.LogAttrs(context.Background(), slog.LevelDebug, "RDPGFX DVC data received", slog.Int("len", len(data)))
				c.egfxHandler.ProcessPDU(data)
			})
			// Send CAPS_ADVERTISE AFTER create response via OnOpen callback
			ch.OnOpen(func() {
				if c.egfxHandler == nil {
					return
				}
				c.log.LogAttrs(context.Background(), slog.LevelDebug, "RDPGFX channel sending CAPS_ADVERTISE")
				if err := c.egfxHandler.SendCapsAdvertise(); err != nil {
					c.log.LogAttrs(context.Background(), slog.LevelError, "failed to send EGFX caps advertise", slog.Any("err", err))
				} else {
					c.log.LogAttrs(context.Background(), slog.LevelDebug, "RDPGFX CAPS_ADVERTISE sent successfully")
				}
			})
			c.log.LogAttrs(context.Background(), slog.LevelInfo, "RDPGFX channel opened", slog.Int("id", int(ch.ID)))
		}
		if name == "AUDIO_PLAYBACK_DVC" {
			ch.Reject()
		}
		if fn := c.loadOnDynChannel(); fn != nil {
			fn(name, ch)
		}
	})
	c.registerChannelHandler("drdynvc", func(_ uint16, data []byte) {
		if len(data) < 1 {
			c.log.LogAttrs(context.Background(), slog.LevelWarn, "drdynvc empty PDU")
			return
		}
		c.log.LogAttrs(context.Background(), slog.LevelDebug, "drdynvc static channel data", slog.Int("len", len(data)), util.Hex2("hdr", data[0]), util.Hex2("cmd", (data[0]>>4)&0x0F))
		c.dvc.ProcessPDU(data)
	})

	// Initialize cliprdr handler on the "cliprdr" static channel
	if c.opts.Clipboard {
		c.clipHandler = cliprdr.NewHandler(func(data []byte) error {
			return c.sendChannelData("cliprdr", data)
		}, c.log.With("component", "CLIPRDR"))
		c.clipHandler.OnRemoteCopy(func(hasText, hasImage bool) {
			if fn := c.loadOnClipboardUpdate(); fn != nil {
				fn(hasText, hasImage)
			}
		})
		c.clipHandler.OnTextData(func(text string) {
			if fn := c.loadOnClipboardText(); fn != nil {
				fn(text)
			}
		})
		c.clipHandler.OnImageData(func(pngData []byte) {
			if fn := c.loadOnClipboardImage(); fn != nil {
				fn(pngData)
			}
		})
		c.registerChannelHandler("cliprdr", func(_ uint16, data []byte) {
			if c.clipHandler == nil {
				return
			}
			c.clipHandler.ProcessPDU(data)
		})
	}

	c.logMcs.LogAttrs(context.Background(), slog.LevelInfo, "MCS Connect Response", slog.Int("ioChannel", int(c.ioChannelID)), slog.Int("virtualChannels", len(c.channelIDs)))

	return nil
}

// mcsErectDomain sends the MCS Erect Domain Request
func (c *Client) mcsErectDomain() error {
	c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "MCS Erect Domain Request")
	data := mcs.EncodeErectDomainRequest()
	return c.tpktConn.Write(x224.EncodeDataTPDU(data))
}

// mcsAttachUser sends the MCS Attach User Request and reads the Confirm
func (c *Client) mcsAttachUser() error {
	c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "MCS Attach User Request")

	data := mcs.EncodeAttachUserRequest()
	if err := c.tpktConn.Write(x224.EncodeDataTPDU(data)); err != nil {
		return fmt.Errorf("%w: failed to send Attach User Request: %v", ErrMCSConnectFailed, err)
	}

	resp, err := c.tpktConn.Read()
	if err != nil {
		return fmt.Errorf("%w: failed to read Attach User Confirm: %v", ErrMCSConnectFailed, err)
	}

	mcsPayload, err := x224.DecodeDataTPDU(resp)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSConnectFailed, err)
	}

	userID, err := mcs.DecodeAttachUserConfirm(c.logMcs, mcsPayload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSConnectFailed, err)
	}

	c.userChannelID = userID
	c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "MCS Attach User Confirm", slog.Int("userChannel", int(c.userChannelID)))

	return nil
}

// mcsJoinChannels joins the user channel, I/O channel, and all virtual channels
func (c *Client) mcsJoinChannels() error {
	// Build the list of channels to join
	channels := []uint16{c.userChannelID, c.ioChannelID}
	channels = append(channels, c.channelIDs...)

	for _, chID := range channels {
		if err := c.mcsJoinChannel(chID); err != nil {
			return err
		}
	}
	return nil
}

// mcsJoinChannel joins a single MCS channel
func (c *Client) mcsJoinChannel(channelID uint16) error {
	c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "MCS Channel Join Request", slog.Int("channel", int(channelID)))

	data := mcs.EncodeChannelJoinRequest(c.userChannelID, channelID)
	if err := c.tpktConn.Write(x224.EncodeDataTPDU(data)); err != nil {
		return fmt.Errorf("%w: failed to send Channel Join Request for %d: %v",
			ErrMCSChannelJoinFailed, channelID, err)
	}

	resp, err := c.tpktConn.Read()
	if err != nil {
		return fmt.Errorf("%w: failed to read Channel Join Confirm for %d: %v",
			ErrMCSChannelJoinFailed, channelID, err)
	}

	mcsPayload, err := x224.DecodeDataTPDU(resp)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSChannelJoinFailed, err)
	}

	confirmedID, err := mcs.DecodeChannelJoinConfirm(c.logMcs, mcsPayload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMCSChannelJoinFailed, err)
	}

	c.logMcs.LogAttrs(context.Background(), slog.LevelDebug, "MCS Channel Join Confirm", slog.Int("channel", int(confirmedID)))
	return nil
}

// readDataPDU reads a TPKT packet, strips the X.224 Data TPDU header,
// and decodes the MCS Send Data Indication, returning the channel ID and user data.
func (c *Client) readDataPDU() (channelID uint16, userData []byte, err error) {
	resp, err := c.tpktConn.Read()
	if err != nil {
		return 0, nil, fmt.Errorf("reading TPKT: %w", err)
	}

	mcsPayload, err := x224.DecodeDataTPDU(resp)
	if err != nil {
		return 0, nil, fmt.Errorf("decoding X.224 DT: %w", err)
	}

	return mcs.DecodeSendDataIndication(c.logMcs, mcsPayload)
}

// securityExchange handles the Security Exchange phase.
// With TLS (ProtocolSSL or ProtocolHybrid), this is a no-op since the
// TLS layer already provides encryption. With Standard RDP Security
// (ProtocolRDP), it parses the server certificate, RSA-encrypts a client
// random, sends the Security Exchange PDU, and derives session keys.
func (c *Client) securityExchange() error {
	if c.selectedProtocol != x224.ProtocolRDP {
		// Enhanced RDP Security (TLS/NLA): client random is 32 zero bytes
		// for auto-reconnect HMAC calculation (MS-RDPBCGR 5.5).
		c.clientRandom = [32]byte{}
		c.logSec.LogAttrs(context.Background(), slog.LevelInfo, "Security Exchange (no-op with TLS)")
		return nil
	}

	c.logSec.LogAttrs(context.Background(), slog.LevelInfo, "Security Exchange (Standard RDP Security)")

	// Generate 32-byte client random for Security Exchange + auto-reconnect.
	if _, err := rand.Read(c.clientRandom[:]); err != nil {
		return fmt.Errorf("%w: generating client random: %v", ErrSecurityExchangeFailed, err)
	}

	if c.serverSec == nil || len(c.serverSec.RawData) == 0 {
		return fmt.Errorf("%w: no server security data", ErrSecurityExchangeFailed)
	}

	// Parse server certificate and extract RSA public key
	blob, err := sec.DecodeServerSecurityBlob(c.serverSec.RawData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSecurityExchangeFailed, err)
	}

	// RSA encrypt the client random with the server's public key
	encryptedRandom := sec.RSAEncrypt(c.clientRandom[:], &blob.Certificate.PublicKey)

	// Build and send Security Exchange PDU
	exchangeData := sec.EncodeSecurityExchange(encryptedRandom)
	mcsData := mcs.EncodeSendDataRequest(c.userChannelID, c.ioChannelID, exchangeData)
	if err := c.tpktConn.Write(x224.EncodeDataTPDU(mcsData)); err != nil {
		return fmt.Errorf("%w: sending security exchange: %v", ErrSecurityExchangeFailed, err)
	}

	// Derive session keys
	crypto, err := sec.NewRDPCrypto(c.clientRandom[:], blob.ServerRandom, c.serverSec.EncryptionMethod, c.logSec)
	if err != nil {
		return fmt.Errorf("%w: deriving keys: %v", ErrSecurityExchangeFailed, err)
	}
	c.crypto = crypto

	c.logSec.LogAttrs(context.Background(), slog.LevelInfo, "Security Exchange complete", slog.Int("method", int(c.serverSec.EncryptionMethod)))
	return nil
}

// sendClientInfo sends the Client Info PDU.
// With Standard RDP Security, the info PDU is encrypted.
// With TLS, this is a no-op (credentials are sent via CredSSP or not at all).
func (c *Client) sendClientInfo() error {
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "sending Client Info PDU")

	var perfFlags uint32
	if !c.opts.Wallpaper {
		perfFlags |= sec.PerfDisableWallpaper
	}
	if !c.opts.FullWindowDrag {
		perfFlags |= sec.PerfDisableFullWindowDrag
	}
	if !c.opts.MenuAnimations {
		perfFlags |= sec.PerfDisableMenuAnimations
	}
	if !c.opts.Theming {
		perfFlags |= sec.PerfDisableTheming
	}
	if !c.opts.CursorShadow {
		perfFlags |= sec.PerfDisableCursorShadow
	}
	if !c.opts.CursorSettings {
		perfFlags |= sec.PerfDisableCursorSettings
	}
	if c.opts.FontSmoothing {
		perfFlags |= sec.PerfEnableFontSmoothing
	}
	if c.opts.DesktopComposition {
		perfFlags |= sec.PerfEnableDesktopComposit
	}
	perfFlags |= sec.PerfDisablePlaybackSounds

	info := &sec.ClientInfo{
		Domain:              c.opts.Domain,
		Username:            c.opts.Username,
		Password:            c.opts.Password,
		PerfFlags:           perfFlags,
		AudioInput:          false,
		RemoteAudio:         false,
		NoAudioPlayback:     true,
		AutoReconnectCookie: c.arcCookie,
	}
	infoData := sec.EncodeClientInfo(info)

	var payload []byte
	if c.crypto != nil {
		// Standard RDP Security: encrypt and prepend security header
		payload = c.crypto.Encrypt(infoData, sec.InfoPkt|sec.Encrypt)
	} else {
		// TLS mode: prepend basic security header (SEC_INFO_PKT, no encryption)
		payload = make([]byte, 4+len(infoData))
		binary.LittleEndian.PutUint16(payload[0:2], sec.InfoPkt)
		binary.LittleEndian.PutUint16(payload[2:4], 0)
		copy(payload[4:], infoData)
	}

	mcsData := mcs.EncodeSendDataRequest(c.userChannelID, c.ioChannelID, payload)
	if err := c.tpktConn.Write(x224.EncodeDataTPDU(mcsData)); err != nil {
		return fmt.Errorf("sending client info: %w", err)
	}

	c.log.LogAttrs(context.Background(), slog.LevelInfo, "Client Info PDU sent")
	return nil
}

// readDecryptedPDU reads an MCS Send Data Indication and, if Standard RDP
// Security is active, strips the security header and decrypts the payload.
func (c *Client) readDecryptedPDU() (channelID uint16, payload []byte, err error) {
	// Loop to handle fast-path packets that arrive interleaved with slow-path.
	// Fast-path updates are dispatched inline; the loop continues until a
	// slow-path PDU arrives (MS-RDPBCGR 3.2.5.1).
	for {
		pktType, actionByte, data, err := c.tpktConn.ReadPacket()
		if err != nil {
			return 0, nil, fmt.Errorf("reading packet: %w", err)
		}

		if pktType == tpkt.PacketFastPath {
			// Dispatch fast-path update inline (same as receiveLoop)
			c.handleFastPathPDU(actionByte, data)
			continue
		}

		// Slow-path TPKT packet
		mcsPayload, err := x224.DecodeDataTPDU(data)
		if err != nil {
			return 0, nil, fmt.Errorf("decoding X.224 DT: %w", err)
		}

		// Check for MCS Disconnect Provider Ultimatum before assuming SDI.
		if mcs.PDUType(mcsPayload) == mcs.DomainMCSPDUDisconnectProviderUltimatum {
			reason, _ := mcs.DecodeDisconnectProviderUltimatum(c.logMcs, mcsPayload)
			return 0, nil, fmt.Errorf("%w: MCS Disconnect Provider Ultimatum (reason=%d)", ErrDisconnected, reason)
		}

		channelID, userData, err := mcs.DecodeSendDataIndication(c.logMcs, mcsPayload)
		if err != nil {
			return 0, nil, fmt.Errorf("decoding MCS SDI: %w", err)
		}

		// Decrypt (centralized, matching receiveLoop)
		userData = c.decryptSlowPath(userData)
		if userData == nil {
			return 0, nil, fmt.Errorf("decryption failed")
		}

		return channelID, userData, nil
	}
}

// readLicensingPDU reads an MCS Send Data Indication, validates the channel
// and SEC_LICENSE_PKT flag, and returns the licensing preamble and body data.
func (c *Client) readLicensingPDU() (lic.Preamble, []byte, error) {
	channelID, userData, err := c.readDataPDU()
	if err != nil {
		return lic.Preamble{}, nil, fmt.Errorf("%w: %v", ErrLicensingFailed, err)
	}

	if channelID != c.ioChannelID {
		return lic.Preamble{}, nil, fmt.Errorf("%w: licensing PDU on unexpected channel %d (expected %d)",
			ErrLicensingFailed, channelID, c.ioChannelID)
	}

	hdr, licData, err := sec.DecodeBasicSecurityHeader(userData)
	if err != nil {
		return lic.Preamble{}, nil, fmt.Errorf("%w: %v", ErrLicensingFailed, err)
	}

	if hdr.Flags&sec.LicensePkt == 0 {
		return lic.Preamble{}, nil, fmt.Errorf("%w: expected SEC_LICENSE_PKT flag, got 0x%04X",
			ErrLicensingFailed, hdr.Flags)
	}

	if hdr.Flags&sec.Encrypt != 0 && c.crypto != nil {
		licData, err = c.crypto.Decrypt(licData)
		if err != nil {
			return lic.Preamble{}, nil, fmt.Errorf("%w: decrypting licensing PDU: %v", ErrLicensingFailed, err)
		}
	}

	preamble, body, err := lic.DecodePreamble(c.logLic, licData)
	if err != nil {
		return lic.Preamble{}, nil, fmt.Errorf("%w: %v", ErrLicensingFailed, err)
	}

	return preamble, body, nil
}

// sendLicensingPDU wraps licensing data with a SEC_LICENSE_PKT header and
// sends it via MCS Send Data Request.
func (c *Client) sendLicensingPDU(data []byte) error {
	secHdr := sec.EncodeBasicSecurityHeader(sec.BasicSecurityHeader{
		Flags: sec.LicensePkt,
	})
	payload := make([]byte, len(secHdr)+len(data))
	copy(payload, secHdr)
	copy(payload[len(secHdr):], data)

	mcsData := mcs.EncodeSendDataRequest(c.userChannelID, c.ioChannelID, payload)
	return c.tpktConn.Write(x224.EncodeDataTPDU(mcsData))
}

// handleLicensing reads and processes the server licensing exchange.
// With TLS connections, the server typically sends a STATUS_VALID_CLIENT
// error alert, indicating no license is required. When a LICENSE_REQUEST
// is received, the full MS-RDPELE handshake is performed.
func (c *Client) handleLicensing() error {
	c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "waiting for licensing PDU")

	// Step 1: Read first PDU
	preamble, body, err := c.readLicensingPDU()
	if err != nil {
		return err
	}

	// Fast path: STATUS_VALID_CLIENT (common TLS case)
	if preamble.MsgType == lic.ErrorAlert {
		ea, err := lic.DecodeErrorAlert(body)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrLicensingFailed, err)
		}
		if ea.ErrorCode == lic.StatusValidClient {
			c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "licensing: STATUS_VALID_CLIENT (no license required)")
			return nil
		}
		return fmt.Errorf("%w: server error alert: code=0x%08X transition=0x%08X",
			ErrLicensingFailed, ea.ErrorCode, ea.StateTransition)
	}

	if preamble.MsgType != lic.LicenseRequest {
		return fmt.Errorf("%w: unexpected licensing message type 0x%02X",
			ErrLicensingFailed, preamble.MsgType)
	}

	// Step 2: Parse LICENSE_REQUEST
	c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "received LICENSE_REQUEST, starting handshake")
	lr, err := lic.DecodeLicenseRequest(body)
	if err != nil {
		c.logLic.LogAttrs(context.Background(), slog.LevelError, "failed to parse license request", slog.String("error", err.Error()), slog.Int("bodyLen", len(body)))
		return fmt.Errorf("%w: parsing license request: %v", ErrLicensingFailed, err)
	}
	c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "parsed LICENSE_REQUEST",
		slog.Int("serverCertLen", len(lr.ServerCert)),
		slog.Int("scopeCount", len(lr.ScopeList)))

	// Extract server RSA public key from the certificate blob
	serverCert, err := sec.DecodeServerCertificate(lr.ServerCert)
	if err != nil {
		c.logLic.LogAttrs(context.Background(), slog.LevelError, "failed to decode license server cert", slog.String("error", err.Error()), slog.Int("certLen", len(lr.ServerCert)))
		return fmt.Errorf("%w: decoding license server cert: %v", ErrLicensingFailed, err)
	}

	// Step 3: Generate client random and pre-master secret
	clientRandom := make([]byte, 32)
	if _, err := rand.Read(clientRandom); err != nil {
		return fmt.Errorf("%w: generating client random: %v", ErrLicensingFailed, err)
	}
	preMasterSecret := make([]byte, 48)
	if _, err := rand.Read(preMasterSecret); err != nil {
		return fmt.Errorf("%w: generating pre-master secret: %v", ErrLicensingFailed, err)
	}

	// Step 4: Derive licensing keys
	licCrypto := lic.DeriveLicenseKeys(preMasterSecret, clientRandom, lr.ServerRandom)

	// Step 5: RSA-encrypt pre-master secret
	encryptedPreMaster := sec.RSAEncrypt(preMasterSecret, &serverCert.PublicKey)

	// Step 6: Build & send NEW_LICENSE_REQUEST
	hostname := c.opts.Host
	if len(hostname) > 15 {
		hostname = hostname[:15]
	}
	newLicReq := lic.EncodeNewLicenseRequest(clientRandom, encryptedPreMaster, c.opts.Username, hostname)
	if err := c.sendLicensingPDU(newLicReq); err != nil {
		return fmt.Errorf("%w: sending new license request: %v", ErrLicensingFailed, err)
	}
	c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "sent NEW_LICENSE_REQUEST")

	// Step 7: Read PLATFORM_CHALLENGE
	preamble, body, err = c.readLicensingPDU()
	if err != nil {
		return err
	}

	// Handle error alert at this stage
	if preamble.MsgType == lic.ErrorAlert {
		ea, err := lic.DecodeErrorAlert(body)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrLicensingFailed, err)
		}
		if ea.ErrorCode == lic.StatusValidClient {
			c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "licensing: STATUS_VALID_CLIENT after new license request")
			return nil
		}
		return fmt.Errorf("%w: server error alert after new license request: code=0x%08X",
			ErrLicensingFailed, ea.ErrorCode)
	}

	if preamble.MsgType != lic.PlatformChallenge {
		return fmt.Errorf("%w: expected PLATFORM_CHALLENGE, got 0x%02X",
			ErrLicensingFailed, preamble.MsgType)
	}

	pc, err := lic.DecodePlatformChallenge(body)
	if err != nil {
		return fmt.Errorf("%w: parsing platform challenge: %v", ErrLicensingFailed, err)
	}

	// Decrypt the challenge
	decryptedChallenge := licCrypto.Decrypt(pc.EncryptedChallenge.Data)

	// Verify MAC (warn on mismatch, don't fail — some servers produce non-conforming MACs)
	expectedMAC := licCrypto.MAC(decryptedChallenge)
	macMatch := true
	for i := 0; i < 16 && i < len(pc.MACData) && i < len(expectedMAC); i++ {
		if pc.MACData[i] != expectedMAC[i] {
			macMatch = false
			break
		}
	}
	if !macMatch {
		c.logLic.LogAttrs(context.Background(), slog.LevelWarn, "platform challenge MAC mismatch (continuing)")
	}

	// Step 8: Build challenge response
	// HWID: PlatformId(u32) + Data1(u32) + Data2(u32) + Data3(u32) + Data4(u32) = 20 bytes
	var hwid [20]byte
	binary.LittleEndian.PutUint32(hwid[0:4], lic.ClientPlatformID)
	// Data1-4 are zero (anonymous client)

	// MAC is computed over (decryptedChallenge + HWID)
	macInput := make([]byte, len(decryptedChallenge)+20)
	copy(macInput, decryptedChallenge)
	copy(macInput[len(decryptedChallenge):], hwid[:])
	responseMAC := licCrypto.MAC(macInput)

	// Encrypt challenge response and HWID
	encryptedResponse := licCrypto.Encrypt(decryptedChallenge)
	encryptedHWID := licCrypto.Encrypt(hwid[:])

	// Step 9: Send PLATFORM_CHALLENGE_RESPONSE
	challengeResp := lic.EncodePlatformChallengeResponse(encryptedResponse, encryptedHWID, responseMAC)
	if err := c.sendLicensingPDU(challengeResp); err != nil {
		return fmt.Errorf("%w: sending platform challenge response: %v", ErrLicensingFailed, err)
	}
	c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "sent PLATFORM_CHALLENGE_RESPONSE")

	// Step 10: Read final PDU
	preamble, body, err = c.readLicensingPDU()
	if err != nil {
		return err
	}

	switch preamble.MsgType {
	case lic.NewLicense, lic.UpgradeLicense:
		c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "licensing complete",
			util.Hex2("msgType", preamble.MsgType))
		return nil
	case lic.ErrorAlert:
		ea, err := lic.DecodeErrorAlert(body)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrLicensingFailed, err)
		}
		if ea.ErrorCode == lic.StatusValidClient {
			c.logLic.LogAttrs(context.Background(), slog.LevelDebug, "licensing: STATUS_VALID_CLIENT")
			return nil
		}
		return fmt.Errorf("%w: licensing failed: error code=0x%08X transition=0x%08X",
			ErrLicensingFailed, ea.ErrorCode, ea.StateTransition)
	default:
		return fmt.Errorf("%w: unexpected final licensing message type 0x%02X",
			ErrLicensingFailed, preamble.MsgType)
	}
}

// handleCapabilitiesExchange reads the server's Demand Active PDU and
// responds with a Confirm Active PDU containing our capability sets.
// With TLS, the MCS user data starts directly with the Share Control Header.
// With Standard RDP Security, the security header is stripped and data decrypted
// by readDecryptedPDU.
func (c *Client) handleCapabilitiesExchange() error {
	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "waiting for Demand Active PDU")

	channelID, userData, err := c.readDecryptedPDU()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCapabilitiesFailed, err)
	}

	if channelID != c.ioChannelID {
		return fmt.Errorf("%w: demand active on unexpected channel %d (expected %d)",
			ErrCapabilitiesFailed, channelID, c.ioChannelID)
	}

	hdr, rest, err := pdu.DecodeShareControlHeader(c.logPdu, userData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCapabilitiesFailed, err)
	}

	if hdr.PDUType != pdu.TypeDemandActive {
		return fmt.Errorf("%w: expected Demand Active (0x%04X), got 0x%04X",
			ErrCapabilitiesFailed, pdu.TypeDemandActive, hdr.PDUType)
	}

	da, err := pdu.DecodeDemandActive(c.logPdu, rest)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCapabilitiesFailed, err)
	}

	c.shareID.Store(da.ShareID)
	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "Demand Active", util.Hex8("shareID", da.ShareID), slog.Int("serverCaps", int(da.NumberCapabilities)))

	// Parse server capabilities and build a bitfield for conditional cap echo.
	c.serverCaps = 0
	if serverCaps, err := caps.DecodeCapabilitySets(c.logPdu, da.CapabilitySets, da.NumberCapabilities); err == nil {
		for _, sc := range serverCaps {
			if sc.Type < 32 {
				c.serverCaps |= 1 << sc.Type
			}
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "server capability", util.Hex4("type", sc.Type), slog.Int("payloadLen", len(sc.Payload)))
			switch sc.Type {
			case caps.TypeBitmap:
				// MS-RDPBCGR 2.2.7.1.2: preferredBitsPerPixel at offset 0
				if len(sc.Payload) >= 2 {
					bpp := int(binary.LittleEndian.Uint16(sc.Payload[0:2]))
					if bpp == 8 || bpp == 15 || bpp == 16 || bpp == 24 || bpp == 32 {
						c.serverBpp = bpp
					}
					c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "server bitmap depth", slog.Int("bpp", bpp))
				}
			case caps.TypeSurfaceCommands:
				if len(sc.Payload) >= 4 {
					flags := binary.LittleEndian.Uint32(sc.Payload[0:4])
					c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "surface command flags", util.Hex8("cmdFlags", flags))
				}
			case caps.TypeBitmapCodecs:
				if len(sc.Payload) >= 1 {
					count := sc.Payload[0]
					c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "bitmap codecs", slog.Int("count", int(count)))
					p := 1
					for i := byte(0); i < count && p+19 <= len(sc.Payload); i++ {
						guid := sc.Payload[p : p+16]
						codecID := sc.Payload[p+16]
						propLen := int(binary.LittleEndian.Uint16(sc.Payload[p+17 : p+19]))
						c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "bitmap codec", slog.Int("index", int(i)), util.Bytes("guid", guid), slog.Int("codecID", int(codecID)), slog.Int("propLen", propLen))
						p += 19 + propLen
					}
				}
			}
		}
	}

	return c.sendConfirmActive()
}

// sendConfirmActive builds and sends the Confirm Active PDU with our capability sets.
// Called during initial connection (single-threaded) and reactivation (from receiveLoop).
func (c *Client) sendConfirmActive() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	const soundBeeps = false
	capsData, capsCount := caps.BuildConfirmCapabilities(
		c.opts.Width, c.opts.Height, c.opts.Depth, c.opts.GFX, c.serverCaps, soundBeeps)

	ca := &pdu.ConfirmActive{
		ShareID:            c.shareID.Load(),
		SourceDescriptor:   []byte("MSTSC\x00"),
		NumberCapabilities: capsCount,
		CapabilitySets:     capsData,
	}

	pduData := pdu.EncodeConfirmActive(ca, c.userChannelID)

	// Encrypt if using Standard RDP Security
	if c.crypto != nil {
		pduData = c.crypto.Encrypt(pduData, sec.Encrypt)
	}

	// Wrap: MCS Send Data Request → X.224 DT → TPKT
	mcsData := mcs.EncodeSendDataRequest(c.userChannelID, c.ioChannelID, pduData)
	if err := c.tpktConn.Write(x224.EncodeDataTPDU(mcsData)); err != nil {
		return fmt.Errorf("%w: failed to send Confirm Active: %v", ErrCapabilitiesFailed, err)
	}

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "sent Confirm Active", slog.Int("capabilitySets", int(capsCount)))
	return nil
}

// sendDataPDU wraps a Share Data PDU in MCS Send Data Request → X.224 DT → TPKT
// and writes it using a single reusable buffer (zero allocations in steady state).
// If Standard RDP Security is active, the payload is encrypted in-place.
//
// Buffer layout (TLS mode):
//
//	[TPKT(4)] [X.224 DT(3)] [MCS SDR(7-8)] [ShareControl(6) + ShareData(12)] [payload]
//
// Buffer layout (Standard RDP Security):
//
//	[TPKT(4)] [X.224 DT(3)] [MCS SDR(7-8)] [SecHdr(4) + MAC(8)] [ShareControl(6) + ShareData(12)] [payload]
func (c *Client) sendDataPDU(pduType2 uint8, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	payloadLen := len(payload)
	shareDataLen := 18 + payloadLen // Share Control (6) + Share Data (12) + payload

	// Compute MCS user data length (what MCS encapsulates)
	var mcsUserDataLen int
	var secHdrSize int
	if c.crypto != nil {
		secHdrSize = 12 // sec header (4) + MAC (8)
		mcsUserDataLen = secHdrSize + shareDataLen
	} else {
		mcsUserDataLen = shareDataLen
	}

	// MCS PER length encoding size
	mcsLenSize := 1
	if mcsUserDataLen >= 0x80 {
		mcsLenSize = 2
	}
	mcsHdrSize := 6 + mcsLenSize // type(1) + initiator(2) + channel(2) + priority(1) + length

	totalLen := 4 + 3 + mcsHdrSize + mcsUserDataLen // TPKT + X.224 + MCS + data

	// Grow sendBuf if needed
	if cap(c.sendBuf) < totalLen {
		c.sendBuf = make([]byte, totalLen)
	}
	buf := c.sendBuf[:totalLen]

	// TPKT header (4 bytes)
	buf[0] = tpkt.Version
	buf[1] = 0
	binary.BigEndian.PutUint16(buf[2:4], uint16(totalLen))

	// X.224 Data TPDU header (3 bytes)
	buf[4] = 0x02 // LI
	buf[5] = 0xF0 // DT TPDU
	buf[6] = 0x80 // EOT

	// MCS Send Data Request header
	off := 7
	buf[off] = mcs.DomainMCSPDUSendDataRequest
	binary.BigEndian.PutUint16(buf[off+1:off+3], c.userChannelID-1001)
	binary.BigEndian.PutUint16(buf[off+3:off+5], c.ioChannelID)
	buf[off+5] = 0x70 // dataPriority=high, segmentation=begin|end
	off += 6
	if mcsUserDataLen < 0x80 {
		buf[off] = byte(mcsUserDataLen)
		off++
	} else {
		buf[off] = byte(0x80 | (mcsUserDataLen >> 8))
		buf[off+1] = byte(mcsUserDataLen & 0xFF)
		off += 2
	}

	if c.crypto != nil {
		// Security header + MAC placeholder at off, filled by EncryptInPlace
		secStart := off
		off += secHdrSize

		// Share Control Header (6 bytes)
		binary.LittleEndian.PutUint16(buf[off:off+2], uint16(shareDataLen))
		binary.LittleEndian.PutUint16(buf[off+2:off+4], pdu.TypeData)
		binary.LittleEndian.PutUint16(buf[off+4:off+6], c.userChannelID)

		// Share Data Header (12 bytes)
		binary.LittleEndian.PutUint32(buf[off+6:off+10], c.shareID.Load())
		buf[off+10] = 0             // pad1
		buf[off+11] = pdu.StreamLow // streamID
		binary.LittleEndian.PutUint16(buf[off+12:off+14], uint16(payloadLen))
		buf[off+14] = pduType2                               // pduType2
		buf[off+15] = 0                                      // compressedType
		binary.LittleEndian.PutUint16(buf[off+16:off+18], 0) // compressedLength
		off += 18

		// Copy payload
		copy(buf[off:], payload)

		// Encrypt in-place: buf[secStart:] = secHeader(4) + MAC(8) + plaintext
		c.crypto.EncryptInPlace(buf[secStart:secStart+secHdrSize+shareDataLen], sec.Encrypt)
	} else {
		// Share Control Header (6 bytes)
		binary.LittleEndian.PutUint16(buf[off:off+2], uint16(shareDataLen))
		binary.LittleEndian.PutUint16(buf[off+2:off+4], pdu.TypeData)
		binary.LittleEndian.PutUint16(buf[off+4:off+6], c.userChannelID)

		// Share Data Header (12 bytes)
		binary.LittleEndian.PutUint32(buf[off+6:off+10], c.shareID.Load())
		buf[off+10] = 0             // pad1
		buf[off+11] = pdu.StreamLow // streamID
		binary.LittleEndian.PutUint16(buf[off+12:off+14], uint16(payloadLen))
		buf[off+14] = pduType2                               // pduType2
		buf[off+15] = 0                                      // compressedType
		binary.LittleEndian.PutUint16(buf[off+16:off+18], 0) // compressedLength
		off += 18

		// Copy payload
		copy(buf[off:], payload)
	}

	return c.tpktConn.WriteDirect(buf)
}

// handleFinalization performs the RDP connection finalization sequence.
// Matches MS-RDPBCGR 1.3.1.1 and the proven sequence from working implementations:
//  1. Send Synchronize, Control Cooperate, Control Request Control
//  2. Read 3 server responses (Synchronize, Control Cooperate, Control Granted)
//  3. Send Input Synchronize
//  4. Send Font List
//  5. Read Font Map response
func (c *Client) handleFinalization() error {
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "connection finalization")

	// Phase 1: Send 3 PDUs
	if err := c.sendDataPDU(pdu.PDUType2Synchronize, pdu.EncodeSynchronize(c.userChannelID)); err != nil {
		return fmt.Errorf("%w: sending synchronize: %v", ErrFinalizationFailed, err)
	}
	if err := c.sendDataPDU(pdu.PDUType2Control, pdu.EncodeControl(pdu.ControlCooperate)); err != nil {
		return fmt.Errorf("%w: sending control cooperate: %v", ErrFinalizationFailed, err)
	}
	if err := c.sendDataPDU(pdu.PDUType2Control, pdu.EncodeControl(pdu.ControlRequestControl)); err != nil {
		return fmt.Errorf("%w: sending control request: %v", ErrFinalizationFailed, err)
	}

	c.log.LogAttrs(context.Background(), slog.LevelDebug, "sent 3 finalization PDUs, reading server responses")

	// Phase 2: Read 3 server responses (Synchronize, Control, Control)
	const (
		gotSync    = 1 << 0
		gotControl = 1 << 1
		phase2All  = gotSync | gotControl
	)
	var received uint8

	for i := 0; i < 10; i++ {
		channelID, userData, err := c.readDecryptedPDU()
		if err != nil {
			return fmt.Errorf("%w: reading server finalization PDU: %v", ErrFinalizationFailed, err)
		}

		if channelID != c.ioChannelID {
			c.handleVirtualChannelData(channelID, userData)
			continue
		}

		_, scRest, err := pdu.DecodeShareControlHeader(c.logPdu, userData)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFinalizationFailed, err)
		}

		sdHdr, sdPayload, err := pdu.DecodeShareDataHeader(c.logPdu, scRest)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFinalizationFailed, err)
		}

		if sdHdr.CompressedType&mppc.FlagCompressed != 0 {
			if compType := sdHdr.CompressedType & 0x0F; compType <= mppc.TypeRDP5 {
				if decompressed, derr := c.mppcDecomp.Decompress(sdPayload, sdHdr.CompressedType); derr == nil {
					sdPayload = decompressed
				}
			}
		}

		switch sdHdr.PDUType2 {
		case pdu.PDUType2Synchronize:
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "received server Synchronize")
			received |= gotSync
		case pdu.PDUType2Control:
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "received server Control")
			received |= gotControl
		case pdu.PDUType2SetErrorInfo:
			errCode := uint32(0)
			if len(sdPayload) >= 4 {
				errCode = binary.LittleEndian.Uint32(sdPayload[:4])
			}
			if errCode != 0 {
				return fmt.Errorf("%w: server error info: 0x%08X (%s)", ErrFinalizationFailed, errCode, pdu.ErrorInfoName(errCode))
			}
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "received Set Error Info", util.Hex8("code", errCode))
		default:
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "finalization phase2: ignoring pduType2", slog.Int("pduType2", int(sdHdr.PDUType2)))
		}

		if received&phase2All == phase2All {
			break
		}
	}
	if received&phase2All != phase2All {
		return fmt.Errorf("%w: did not receive Sync+Control responses (got mask=0x%02X)", ErrFinalizationFailed, received)
	}

	// Phase 3: Send Input Synchronize (before fonts, per MS-RDPBCGR 1.3.1.1)
	c.sendInputSynchronize()

	// Phase 4: Send Font List
	if err := c.sendDataPDU(pdu.PDUType2FontList, pdu.EncodeFontList()); err != nil {
		return fmt.Errorf("%w: sending font list: %v", ErrFinalizationFailed, err)
	}

	c.log.LogAttrs(context.Background(), slog.LevelDebug, "sent input sync + font list, reading font map")

	// Phase 5: Read Font Map response
	for i := 0; i < 10; i++ {
		channelID, userData, err := c.readDecryptedPDU()
		if err != nil {
			return fmt.Errorf("%w: reading font map: %v", ErrFinalizationFailed, err)
		}

		if channelID != c.ioChannelID {
			c.handleVirtualChannelData(channelID, userData)
			continue
		}

		_, scRest, err := pdu.DecodeShareControlHeader(c.logPdu, userData)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFinalizationFailed, err)
		}

		sdHdr, sdPayload, err := pdu.DecodeShareDataHeader(c.logPdu, scRest)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrFinalizationFailed, err)
		}

		if sdHdr.CompressedType&mppc.FlagCompressed != 0 {
			if compType := sdHdr.CompressedType & 0x0F; compType <= mppc.TypeRDP5 {
				if decompressed, derr := c.mppcDecomp.Decompress(sdPayload, sdHdr.CompressedType); derr == nil {
					sdPayload = decompressed
				}
			}
		}

		switch sdHdr.PDUType2 {
		case pdu.PDUType2FontMap:
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "received server Font Map")
			c.log.LogAttrs(context.Background(), slog.LevelInfo, "finalization complete")
			return nil
		case pdu.PDUType2SetErrorInfo:
			errCode := uint32(0)
			if len(sdPayload) >= 4 {
				errCode = binary.LittleEndian.Uint32(sdPayload[:4])
			}
			if errCode != 0 {
				return fmt.Errorf("%w: server error info: 0x%08X (%s)", ErrFinalizationFailed, errCode, pdu.ErrorInfoName(errCode))
			}
		default:
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "finalization phase5: ignoring pduType2", slog.Int("pduType2", int(sdHdr.PDUType2)))
		}
	}

	return fmt.Errorf("%w: did not receive Font Map response", ErrFinalizationFailed)
}

// receiveLoop reads server PDUs in a background goroutine and dispatches them.
// It uses ReadPacket to handle both TPKT (slow-path) and fast-path framing.
func (c *Client) receiveLoop() {
	defer func() {
		if c.receiveLoopDone != nil {
			close(c.receiveLoopDone)
		}
	}()
	// Motty addition: whatever the server sends, a bug in a decoder ends this
	// connection and not the application. The decoders are fuzzed, but they are
	// 25,000 lines of code that read hostile bytes.
	defer func() {
		if r := recover(); r != nil {
			c.handleDisconnect(fmt.Errorf("%w: internal error while decoding the server's data: %v", ErrDisconnected, r))
		}
	}()
	for {
		pktType, actionByte, data, err := c.tpktConn.ReadPacket()
		if err != nil {
			c.handleDisconnect(err)
			return
		}
		c.lastReceived.Store(time.Now().UnixNano())

		switch pktType {
		case tpkt.PacketTPKT:
			mcsPayload, err := x224.DecodeDataTPDU(data)
			if err != nil {
				c.logX224.LogAttrs(context.Background(), slog.LevelWarn, "receiveLoop: bad X.224 DT", slog.Any("err", err))
				continue
			}
			// Check MCS PDU type before dispatching
			switch mcs.PDUType(mcsPayload) {
			case mcs.DomainMCSPDUDisconnectProviderUltimatum:
				reason, _ := mcs.DecodeDisconnectProviderUltimatum(c.logMcs, mcsPayload)
				c.handleDisconnect(fmt.Errorf("%w: MCS Disconnect Provider Ultimatum (reason=%d)", ErrDisconnected, reason))
				return
			case mcs.DomainMCSPDUSendDataIndication:
				channelID, userData, err := mcs.DecodeSendDataIndication(c.logMcs, mcsPayload)
				if err != nil {
					c.logMcs.LogAttrs(context.Background(), slog.LevelWarn, "receiveLoop: bad MCS SDI", slog.Any("err", err))
					continue
				}
				// Centralized security-layer decryption for ALL channels
				// (MS-RDPBCGR 5.3.6). The RC4 stream must be advanced for
				// every encrypted packet to stay in sync.
				userData = c.decryptSlowPath(userData)
				if channelID != c.ioChannelID {
					c.handleVirtualChannelData(channelID, userData)
					continue
				}
				c.handleSlowPathPDU(userData)
			default:
				c.logMcs.LogAttrs(context.Background(), slog.LevelWarn, "receiveLoop: unhandled MCS PDU type", util.Hex2("type", uint8(mcs.PDUType(mcsPayload))))
			}

		case tpkt.PacketFastPath:
			c.handleFastPathPDU(actionByte, data)
		}
	}
}

// keepAliveLoop periodically sends a RefreshRect PDU to provoke server
// responses for heartbeat detection. Runs at HeartbeatTimeout/2.
// Uses a 0×0 refresh request — a pure protocol-level keepalive that
// does not inject any input events (avoids triggering Sticky Keys, etc.).
func (c *Client) keepAliveLoop() {
	c.closeMu.Lock()
	done := c.done
	c.closeMu.Unlock()
	ticker := time.NewTicker(c.opts.HeartbeatTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if c.State() != StateConnected {
				return
			}
			_ = c.RefreshRect(0, 0, 0, 0)
		}
	}
}

// heartbeatLoop monitors lastReceived and triggers disconnect if the server
// goes silent for longer than HeartbeatTimeout.
func (c *Client) heartbeatLoop() {
	c.closeMu.Lock()
	done := c.done
	c.closeMu.Unlock()
	ticker := time.NewTicker(c.opts.HeartbeatTimeout / 2)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			last := time.Unix(0, c.lastReceived.Load())
			if time.Since(last) > c.opts.HeartbeatTimeout {
				c.log.LogAttrs(context.Background(), slog.LevelWarn, "heartbeat timeout", slog.Duration("silence", time.Since(last)))
				c.handleDisconnect(ErrHeartbeatTimeout)
				return
			}
		}
	}
}

// resetForReconnect closes network connections and resets negotiated state,
// preparing the client for a fresh Connect() call. Callbacks and reusable
// buffers are preserved.
func (c *Client) resetForReconnect() {
	// Mark disconnected first so new Send* calls return ErrNotConnected.
	c.setState(StateDisconnected)

	if c.tlsConn != nil {
		c.tlsConn.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	// Take writeMu to wait for any in-flight sendDataPDU/sendChannelData
	// to finish, then zero the fields they read.
	c.writeMu.Lock()
	c.tlsConn = nil
	c.conn = nil
	c.tpktConn = nil
	c.crypto = nil
	c.userChannelID = 0
	c.ioChannelID = 0
	c.channelMap = nil
	c.channelOpts = nil
	c.writeMu.Unlock()

	c.selectedProtocol = 0
	c.tlsMinVersion = 0
	c.tlsMaxVersion = 0
	c.credsspVersion = 0
	c.channelIDs = nil
	c.channelNames = nil
	c.channelHandlers = nil
	c.dvc = nil
	if c.egfxHandler != nil {
		c.egfxHandler.Close()
	}
	c.egfxHandler = nil
	c.clipHandler = nil
	c.dispHandler = nil
	c.dispReady.Store(false)
	c.vcReassembly = nil
	c.vcReassemblyTotal = 0
	c.pendingResizeW = 0
	c.pendingResizeH = 0
	c.pendingMonitors = nil
	c.serverCore = nil
	c.serverSec = nil
	c.crypto = nil
	c.shareID.Store(0)
	c.serverCaps = 0

	c.fragBuf = c.fragBuf[:0]
	c.fragCode = 0
	c.mppcDecomp = mppc.Decompressor{}
	c.orderState = orders.DecoderState{}
	c.glyphCache = orders.GlyphCache{}
	c.pointerCache = [128]*PointerUpdate{}
	c.framebuf = nil
	c.bitmapCache.Reset()
	c.brushCache.Reset()
	c.scrBltBuf = nil

	c.receiveLoopDone = nil

	c.closeMu.Lock()
	c.closed = false
	c.done = make(chan struct{})
	c.closeMu.Unlock()
}

// reconnectLoop attempts to re-establish the connection with retries.
func (c *Client) reconnectLoop() {
	defer func() {
		c.closeMu.Lock()
		c.reconnecting = false
		c.closeMu.Unlock()
	}()

	max := c.opts.MaxReconnectAttempts

	const reconnectDelay = 3 * time.Second

	for attempt := 1; max == 0 || attempt <= max; attempt++ {
		c.log.LogAttrs(context.Background(), slog.LevelWarn, "reconnecting", slog.Int("attempt", attempt), slog.Duration("delay", reconnectDelay))
		time.Sleep(reconnectDelay)
		c.resetForReconnect()
		if err := c.Connect(); err != nil {
			c.log.LogAttrs(context.Background(), slog.LevelError, "reconnect attempt failed", slog.Int("attempt", attempt), slog.Any("err", err))
			continue
		}
		c.log.LogAttrs(context.Background(), slog.LevelInfo, "reconnected successfully")
		if fn := c.loadOnReconnected(); fn != nil {
			fn()
		}
		return
	}

	c.closeMu.Lock()
	c.closed = true
	c.closeMu.Unlock()
	if fn := c.loadOnDisconnect(); fn != nil {
		fn(fmt.Errorf("%w: %d attempts exhausted", ErrReconnectFailed, max))
	}
}

// resizeViaReconnect performs a reconnect-based resize for servers that
// don't support the DISP channel (pre-Win8/2012). The new dimensions must
// already be stored in c.opts.Width/Height before calling this method.
func (c *Client) resizeViaReconnect() {
	c.closeMu.Lock()
	if c.closed || c.reconnecting {
		c.closeMu.Unlock()
		return
	}
	c.reconnecting = true
	close(c.done)
	// Close connection to unblock any goroutines stuck in reads/writes.
	if c.tlsConn != nil {
		c.tlsConn.Close()
	} else if c.conn != nil {
		c.conn.Close()
	}
	recvDone := c.receiveLoopDone
	c.closeMu.Unlock()

	// Wait for receiveLoop to finish dispatching before resetting state.
	if recvDone != nil {
		<-recvDone
	}

	if fn := c.loadOnReconnecting(); fn != nil {
		fn()
	}

	c.resetForReconnect()
	if err := c.Connect(); err != nil {
		c.log.LogAttrs(context.Background(), slog.LevelError, "resize reconnect failed", slog.Any("err", err))
		c.closeMu.Lock()
		c.reconnecting = false
		c.closed = true
		c.closeMu.Unlock()
		if fn := c.loadOnDisconnect(); fn != nil {
			fn(err)
		}
		return
	}
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "resize reconnect successful",
		slog.Int("width", int(c.opts.Width)), slog.Int("height", int(c.opts.Height)))
	c.closeMu.Lock()
	c.reconnecting = false
	c.closeMu.Unlock()
	if fn := c.loadOnReconnected(); fn != nil {
		fn()
	}
}

// handleDisconnect is called when the connection is lost.
// Server-initiated disconnects (logoff, admin kick) are not retried.
// If auto-reconnect is enabled and the disconnect was unexpected (I/O error,
// heartbeat timeout), it starts a reconnect loop; otherwise it calls the
// onDisconnect callback.
func (c *Client) handleDisconnect(err error) {
	c.closeMu.Lock()
	if c.closed || c.reconnecting {
		c.closeMu.Unlock()
		return
	}

	// Close done to stop receiveLoop, keepAliveLoop, heartbeatLoop.
	close(c.done)

	// Server-initiated disconnect (logoff, admin kick) — don't reconnect.
	if c.opts.AutoReconnect && !errors.Is(err, ErrDisconnected) {
		c.reconnecting = true
		c.closeMu.Unlock()
		if fn := c.loadOnReconnecting(); fn != nil {
			fn()
		}
		go c.reconnectLoop()
	} else {
		c.closed = true
		c.closeMu.Unlock()
		if fn := c.loadOnDisconnect(); fn != nil {
			fn(err)
		}
	}
}

// decryptSlowPath strips the TS_SECURITY_HEADER and decrypts the payload
// when Standard RDP Security is active. Must be called for ALL channels
// so the RC4 stream stays in sync (MS-RDPBCGR 5.3.6).
func (c *Client) decryptSlowPath(data []byte) []byte {
	if c.crypto == nil || len(data) < 4 {
		return data
	}
	flags := binary.LittleEndian.Uint16(data[0:2])
	rest := data[4:] // skip security header (flags + flagsHi)
	if flags&(sec.Encrypt|sec.RedirectionPkt) != 0 {
		var err error
		rest, err = c.crypto.Decrypt(rest)
		if err != nil {
			c.logSec.LogAttrs(context.Background(), slog.LevelError, "slow-path decrypt error", slog.Any("err", err))
			return nil
		}
	}
	return rest
}

// handleSlowPathPDU decodes a Share Control + Share Data PDU and dispatches
// on the inner pduType2. data has already been decrypted by decryptSlowPath.
func (c *Client) handleSlowPathPDU(data []byte) {
	if data == nil {
		return
	}

	scHdr, rest, err := pdu.DecodeShareControlHeader(c.logPdu, data)
	if err != nil {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "receiveLoop: bad share control header", slog.Any("err", err))
		return
	}

	// Match on low 4 bits (PDU type) ignoring version in high bits
	switch scHdr.PDUType & 0x000F {
	case pdu.TypeDeactivateAll & 0x000F:
		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "received Deactivate All PDU")
		// Server reactivation: re-run capabilities exchange + finalization.
		// This happens during RDPEDISP resize AND during normal login on legacy servers
		// (e.g. Windows Server 2003 reactivates after initial desktop draw).
		c.handleReactivation()
		return
	case pdu.TypeRedirect & 0x000F:
		c.handleRedirectPDU(rest, false)
		return
	case pdu.TypeEnhancedRedirect & 0x000F:
		c.handleRedirectPDU(rest, true)
		return
	case pdu.TypeData & 0x000F:
		// fall through to share data handling below
	default:
		return
	}

	sdHdr, payload, err := pdu.DecodeShareDataHeader(c.logPdu, rest)
	if err != nil {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "receiveLoop: bad share data header", slog.Any("err", err))
		return
	}

	// MPPC bulk decompression (MS-RDPBCGR 3.1.8.4.2)
	if sdHdr.CompressedType&mppc.FlagCompressed != 0 {
		compType := sdHdr.CompressedType & 0x0F
		if compType > mppc.TypeRDP5 {
			c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "slow-path: unsupported compression type", util.Hex2("ctype", sdHdr.CompressedType))
		} else {
			decompressed, derr := c.mppcDecomp.Decompress(payload, sdHdr.CompressedType)
			if derr != nil {
				c.logPdu.LogAttrs(context.Background(), slog.LevelError, "slow-path: MPPC decompress error", slog.Any("err", derr))
				return
			}
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "slow-path: MPPC decompressed",
				slog.Int("compressed", len(payload)), slog.Int("decompressed", len(decompressed)))
			payload = decompressed
		}
	}

	switch sdHdr.PDUType2 {
	case pdu.PDUType2Update:
		c.handleUpdatePDU(payload)
	case pdu.PDUType2Pointer:
		c.handleSlowPathPointer(payload)
	case pdu.PDUType2SaveSessionInfo:
		c.handleSaveSessionInfo(payload)
	case pdu.PDUType2AutoReconnectStatus:
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "auto-reconnect using cookie failed")
	case pdu.PDUType2SetErrorInfo:
		if len(payload) >= 4 {
			code := binary.LittleEndian.Uint32(payload[0:4])
			if code != 0 {
				c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "server Set Error Info", util.Hex8("code", code), slog.String("name", pdu.ErrorInfoName(code)))
			}
		}
	default:
		// Ignore other pduType2 values
	}
}

// handleSaveSessionInfo processes TS_SAVE_SESSION_INFO_PDU_DATA (MS-RDPBCGR 2.2.10.1.1).
// Extracts auto-reconnect cookie from TS_LOGON_INFO_EXTENDED when present.
func (c *Client) handleSaveSessionInfo(data []byte) {
	if len(data) < 4 {
		return
	}
	infoType := binary.LittleEndian.Uint32(data[0:4])
	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "Save Session Info", util.Hex8("infoType", infoType))

	const infoTypeLogonExtended uint32 = 3
	if infoType != infoTypeLogonExtended {
		return
	}

	// TS_LOGON_INFO_EXTENDED: length(2) + fieldsPresent(4) + fields...
	rest := data[4:]
	if len(rest) < 6 {
		return
	}
	// skip length(2)
	fieldsPresent := binary.LittleEndian.Uint32(rest[2:6])
	off := 6

	const logonExAutoReconnectCookie uint32 = 1
	if fieldsPresent&logonExAutoReconnectCookie == 0 {
		return
	}

	// TS_LOGON_INFO_FIELD: cbFieldData(4) + fieldData(variable)
	if off+4 > len(rest) {
		return
	}
	off += 4 // skip cbFieldData

	// ARC_SC_PRIVATE_PACKET: cbLen(4) + version(4) + logonId(4) + arcRandomBits(16) = 28
	if off+28 > len(rest) {
		return
	}
	arcLen := binary.LittleEndian.Uint32(rest[off:])
	if arcLen != 28 {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "invalid ARC_SC_PRIVATE_PACKET length", slog.Int("len", int(arcLen)))
		return
	}
	version := binary.LittleEndian.Uint32(rest[off+4:])
	if version != 1 {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "unsupported ARC_SC_PRIVATE_PACKET version", slog.Int("version", int(version)))
		return
	}

	arc := &sec.AutoReconnectCookie{
		LogonID: binary.LittleEndian.Uint32(rest[off+8:]),
	}
	copy(arc.ServerRandom[:], rest[off+12:off+28])
	arc.ClientRandom = c.clientRandom

	c.arcCookie = arc
	c.logPdu.LogAttrs(context.Background(), slog.LevelInfo, "saved auto-reconnect cookie", slog.Int("logonID", int(arc.LogonID)))
}

// handleRedirectPDU parses a Server Redirection PDU and disconnects with
// a RedirectError so the caller can reconnect to the target server.
func (c *Client) handleRedirectPDU(data []byte, enhanced bool) {
	ri, err := pdu.DecodeRedirectPDU(data, enhanced)
	if err != nil {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "failed to parse redirect PDU", slog.Any("err", err))
		return
	}
	if ri.Server == "" {
		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "redirect PDU with LB_NOREDIRECT, ignoring")
		return
	}
	c.logPdu.LogAttrs(context.Background(), slog.LevelInfo, "server redirection",
		slog.String("server", ri.Server),
		slog.String("domain", ri.Domain),
		slog.String("username", ri.Username),
		slog.Bool("hasLBInfo", len(ri.LoadBalanceInfo) > 0),
		slog.Bool("hasPassword", len(ri.Password) > 0))

	c.handleDisconnect(&RedirectError{Info: ri})
}

// RedirectError wraps a server redirection with the parsed redirect info.
// Callers can check for this with errors.As to extract the RedirectInfo
// and reconnect to the target server.
type RedirectError struct {
	Info *pdu.RedirectInfo
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("%s: %s", ErrServerRedirect, e.Info.Server)
}

func (e *RedirectError) Unwrap() error {
	return ErrServerRedirect
}

// handleReactivation handles server deactivation/reactivation.
// The server sends Deactivate All then a new Demand Active in several cases:
// RDPEDISP resize, login completion on legacy servers (Win2003), capability renegotiation.
// We re-run capabilities exchange + finalization without disconnecting
// (MS-RDPBCGR 1.3.1.3 Deactivation-Reactivation Sequence).
func (c *Client) handleReactivation() {
	c.log.LogAttrs(context.Background(), slog.LevelInfo, "reactivation: waiting for new Demand Active")

	// Reset order state and resize framebuffer BEFORE capabilities exchange.
	// During finalization, readDecryptedPDU dispatches fast-path order updates
	// inline — these render the new desktop (taskbar, icons, etc.). If we resize
	// AFTER finalization, the clear() in Resize wipes everything the server just
	// drew, and the server won't resend it (it thinks the client already has it).
	c.orderState = orders.DecoderState{}
	c.displayMu.Lock()
	width, height := int(c.opts.Width), int(c.opts.Height)
	c.displayMu.Unlock()
	c.framebufMu.Lock()
	if c.framebuf != nil {
		c.framebuf.Resize(width, height)
	}
	c.framebufMu.Unlock()
	if fn := c.loadOnResize(); fn != nil {
		fn(width, height)
	}

	// Read the new Demand Active from the server
	if err := c.handleCapabilitiesExchange(); err != nil {
		c.log.LogAttrs(context.Background(), slog.LevelError, "reactivation: capabilities exchange failed", slog.Any("err", err))
		c.handleDisconnect(err)
		return
	}

	if err := c.handleFinalization(); err != nil {
		c.log.LogAttrs(context.Background(), slog.LevelError, "reactivation: finalization failed", slog.Any("err", err))
		c.handleDisconnect(err)
		return
	}

	c.log.LogAttrs(context.Background(), slog.LevelInfo, "reactivation complete", slog.Int("width", width), slog.Int("height", height))
}

// sendInputSynchronize sends a TS_INPUT_EVENT with messageType=INPUT_SYNCHRONIZE.
// This notifies the server of the client's toggle key state (numlock, capslock, etc.)
// (MS-RDPBCGR 2.2.8.1.1.3.1.1).
func (c *Client) sendInputSynchronize() {
	// Build: eventTime(4) + messageType(2) + pad(2) + toggleFlags(4)
	var event [12]byte
	// messageType=0 (INPUT_SYNCHRONIZE), all other fields 0
	var payload [pdu.InputPDUHeaderSize + 12]byte
	binary.LittleEndian.PutUint16(payload[0:2], 1) // numEvents
	copy(payload[pdu.InputPDUHeaderSize:], event[:])

	if err := c.sendDataPDU(pdu.PDUType2Input, payload[:]); err != nil {
		c.log.LogAttrs(context.Background(), slog.LevelWarn, "failed to send input synchronize", slog.Any("err", err))
	}
}

// handleUpdatePDU dispatches slow-path update PDU subtypes (bitmap, palette, etc.).
func (c *Client) handleUpdatePDU(data []byte) {
	if len(data) < 2 {
		return
	}
	updateType := binary.LittleEndian.Uint16(data[0:2])
	switch updateType {
	case pdu.UpdateBitmap:
		rects, err := pdu.DecodeBitmapUpdateData(data)
		if err != nil {
			c.logPdu.LogAttrs(context.Background(), slog.LevelError, "receiveLoop: bad bitmap update", slog.Any("err", err))
			return
		}
		c.processBitmapRects(rects)
	case pdu.UpdateOrders:
		// Slow-path order update: updateType(2) + pad(2) + numOrders(2) + pad(2) + orderData
		if len(data) < 8 {
			return
		}
		c.handleOrders(data, false)
	case pdu.UpdatePalette:
		// Palette update: updateType(2) + pad(2) + numColors(2) + pad(2) + RGB entries
		c.handlePaletteUpdate(data[2:])
	}
}

// handleFastPathPDU decodes and dispatches fast-path output updates,
// reassembling fragmented updates (FragFirst → FragNext* → FragLast).
// actionByte is the first byte of the fast-path PDU containing security flags
// in bits 6-7 (used for Standard RDP Security decryption).
func (c *Client) handleFastPathPDU(actionByte byte, data []byte) {
	// Fast-path encryption: secFlags in bits 6-7 of the action byte
	secFlags := (actionByte >> 6) & 0x03
	if secFlags != 0 && c.crypto != nil {
		var err error
		data, err = c.crypto.DecryptFastPath(data)
		if err != nil {
			c.logSec.LogAttrs(context.Background(), slog.LevelError, "fast-path decrypt error", slog.Any("err", err))
			return
		}
	}

	updates, err := fastpath.DecodeUpdates(c.logFp, data)
	if err != nil {
		c.logFp.LogAttrs(context.Background(), slog.LevelError, "fast-path decode error", slog.Any("err", err))
		return
	}

	// Wrap the entire PDU in a single paint so all updates are atomic.
	// Nested beginPaint/endPaint calls inside individual handlers are no-ops.
	c.beginPaint()
	defer c.endPaint()

	for i := range updates {
		u := &updates[i]

		// MPPC bulk decompression (MS-RDPBCGR 3.1.8.4.1)
		updateData := u.Data
		if u.CompressionFlags&mppc.FlagCompressed != 0 {
			compType := u.CompressionFlags & 0x0F
			if compType > mppc.TypeRDP5 {
				c.logFp.LogAttrs(context.Background(), slog.LevelWarn, "fast-path: unsupported compression type", util.Hex2("ctype", u.CompressionFlags))
				continue
			}
			decompressed, derr := c.mppcDecomp.Decompress(u.Data, u.CompressionFlags)
			if derr != nil {
				c.logFp.LogAttrs(context.Background(), slog.LevelError, "fast-path: MPPC decompress error", slog.Any("err", derr))
				continue
			}
			c.logFp.LogAttrs(context.Background(), slog.LevelDebug, "fast-path: MPPC decompressed",
				slog.Int("compressed", len(u.Data)), slog.Int("decompressed", len(decompressed)))
			updateData = decompressed
		}

		switch u.Frag {
		case fastpath.FragSingle:
			c.dispatchFastPathUpdate(u.Code, updateData)

		case fastpath.FragFirst:
			c.fragCode = u.Code
			c.fragBuf = append(c.fragBuf[:0], updateData...)

		case fastpath.FragNext:
			if len(c.fragBuf) == 0 {
				continue // no FragFirst seen, discard
			}
			c.fragBuf = append(c.fragBuf, updateData...)
			if len(c.fragBuf) > maxFragReassembly {
				c.logFp.LogAttrs(context.Background(), slog.LevelWarn, "fast-path fragment reassembly exceeds limit, dropping", slog.Int("len", len(c.fragBuf)))
				c.fragBuf = c.fragBuf[:0]
				continue
			}

		case fastpath.FragLast:
			if len(c.fragBuf) == 0 {
				continue
			}
			c.fragBuf = append(c.fragBuf, updateData...)
			c.dispatchFastPathUpdate(c.fragCode, c.fragBuf)
			c.fragBuf = c.fragBuf[:0]
		}
	}
}

// dispatchFastPathUpdate routes a complete (or reassembled) fast-path update
// to the appropriate handler by update code.
func (c *Client) dispatchFastPathUpdate(code byte, data []byte) {
	c.logFp.LogAttrs(context.Background(), slog.LevelDebug, "fast-path update", util.Hex2("code", code), slog.Int("len", len(data)))
	switch code {
	case fastpath.UpdateBitmap:
		c.handleFastPathBitmap(data)
	case fastpath.UpdateOrders:
		c.handleOrders(data, true)
	case fastpath.UpdatePtrNull:
		c.handlePointerNull()
	case fastpath.UpdatePtrDefault:
		c.handlePointerDefault()
	case fastpath.UpdatePtrColor:
		c.handlePointerColor(data)
	case fastpath.UpdatePtrNew:
		c.handlePointerNew(data)
	case fastpath.UpdatePtrLarge:
		c.handlePointerLarge(data)
	case fastpath.UpdatePtrCached:
		c.handlePointerCached(data)
	case fastpath.UpdateSurfCmds:
		c.handleSurfaceCommands(data)
	case fastpath.UpdatePalette:
		// Fast-path palette: skip 2 padding bytes, then palette data
		if len(data) > 2 {
			c.handlePaletteUpdate(data[2:])
		}
	case fastpath.UpdatePtrPosition:
		// Pointer position update — we don't track server-side cursor position
	case fastpath.UpdateSynchronize:
		// Synchronize — no action needed
	}
}

// handleSurfaceCommands parses surface command PDUs (frame markers) and
// sends frame acknowledge responses. The server uses frame markers to track
// rendering progress; the client must acknowledge them when advertising
// CAPSTYPE_FRAME_ACKNOWLEDGE.
//
// Surface command format: cmdType(2 LE) + cmdData...
// Frame marker:  cmdType=0x0004, frameAction(2 LE) + frameId(4 LE)
// frameAction: 0x0000=BEGIN, 0x0001=END
func (c *Client) handleSurfaceCommands(data []byte) {
	off := 0
	for off+2 <= len(data) {
		cmdType := binary.LittleEndian.Uint16(data[off : off+2])
		off += 2
		c.logFp.LogAttrs(context.Background(), slog.LevelDebug, "surface cmd", util.Hex4("type", cmdType))
		switch cmdType {
		case 0x0004: // CMDTYPE_FRAME_MARKER
			if off+6 > len(data) {
				return
			}
			frameAction := binary.LittleEndian.Uint16(data[off : off+2])
			frameID := binary.LittleEndian.Uint32(data[off+2 : off+6])
			off += 6
			if frameAction == 0x0001 { // FRAME_END
				c.sendFrameAcknowledge(frameID)
			}
		case 0x0001, 0x0006: // CMDTYPE_SET_SURFACE_BITS, CMDTYPE_STREAM_SURFACE_BITS
			// TS_SURFCMD_SET_SURF_BITS / TS_SURFCMD_STREAM_SURF_BITS (MS-RDPBCGR 2.2.9.2.1):
			//   destLeft(2) + destTop(2) + destRight(2) + destBottom(2)
			// followed immediately by TS_BITMAP_DATA_EX (MS-RDPBCGR 2.2.9.2.3):
			//   bpp(1) + flags(1) + reserved(1) + codecID(1) + width(2) + height(2) + bitmapDataLength(4)
			// Total header: 8 + 12 = 20 bytes minimum
			if off+20 > len(data) {
				c.logFp.LogAttrs(context.Background(), slog.LevelWarn, "surface bits: header truncated", slog.Int("off", off), slog.Int("need", off+20), slog.Int("have", len(data)))
				return
			}
			destLeft := int(binary.LittleEndian.Uint16(data[off : off+2]))
			destTop := int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
			// off+4: destRight(2), off+6: destBottom(2) — not needed, dimensions from TS_BITMAP_DATA_EX

			// TS_BITMAP_DATA_EX starts at off+8
			// bpp := data[off+8]
			// flags := data[off+9]
			// reserved := data[off+10]
			codecID := data[off+11]
			width := int(binary.LittleEndian.Uint16(data[off+12 : off+14]))
			height := int(binary.LittleEndian.Uint16(data[off+14 : off+16]))
			bitmapDataLength := int(binary.LittleEndian.Uint32(data[off+16 : off+20]))
			payloadStart := off + 20
			if payloadStart+bitmapDataLength > len(data) {
				c.logFp.LogAttrs(context.Background(), slog.LevelWarn, "surface bits: payload truncated", slog.Int("off", off), slog.Int("bitmapDataLen", bitmapDataLength), slog.Int("totalLen", len(data)))
				return
			}
			payload := data[payloadStart : payloadStart+bitmapDataLength]

			c.logFp.LogAttrs(context.Background(), slog.LevelDebug, "surface bits", util.Hex2("codecID", codecID), slog.Int("width", width), slog.Int("height", height), slog.Int("x", destLeft), slog.Int("y", destTop), slog.Int("bytes", bitmapDataLength))
			c.handleSurfaceBits(destLeft, destTop, codecID, width, height, payload)

			// Advance past the entire command: dest rect(8) + TS_BITMAP_DATA_EX header(12) + bitmapData
			off = payloadStart + bitmapDataLength
		default:
			// Unknown surface command — can't determine length, stop parsing.
			return
		}
	}
}

// handleSurfaceBits decodes a codec-compressed bitmap and writes it to the framebuffer.
// Emits directly from decoded buffer to avoid the double-copy of emitDirtyRect.
func (c *Client) handleSurfaceBits(destLeft, destTop int, codecID byte, width, height int, payload []byte) {
	c.logFp.LogAttrs(context.Background(), slog.LevelDebug, "surface bits", util.Hex2("codecID", codecID), slog.Int("width", width), slog.Int("height", height), slog.Int("x", destLeft), slog.Int("y", destTop), slog.Int("bytes", len(payload)))
	var data []byte
	switch codecID {
	case 0x01: // NSCodec
		var err error
		c.decompBuf, c.nscodecBuf, err = nscodec.Decompress(c.decompBuf, c.nscodecBuf, width, height, payload, c.logFp)
		if err != nil {
			c.logFp.LogAttrs(context.Background(), slog.LevelError, "nscodec decompression failed", slog.Any("err", err))
			return
		}
		// Force alpha=0xFF — RDP desktops are always fully opaque.
		// Servers often send alpha=0 which causes transparency artifacts.
		for i := 3; i < len(c.decompBuf); i += 4 {
			c.decompBuf[i] = 0xFF
		}
		data = c.decompBuf
	case 0x00: // Uncompressed
		data = payload
	default:
		c.logFp.LogAttrs(context.Background(), slog.LevelWarn, "unsupported surface codec", util.Hex2("codecID", codecID))
		return
	}

	if c.framebuf != nil {
		c.framebuf.WriteRect(destLeft, destTop, width, height, data)
	}
	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X: destLeft, Y: destTop, Width: width, Height: height,
			BitsPerPixel: 32,
			Data:         data,
		})
	}
}

// sendFrameAcknowledge sends a TS_FRAME_ACKNOWLEDGE_PDU for a completed frame.
func (c *Client) sendFrameAcknowledge(frameID uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], frameID)
	_ = c.sendDataPDU(pdu.PDUType2FrameAcknowledge, buf[:])
}

// handleOrders decodes drawing orders from either fast-path or slow-path updates.
// For fast-path: numOrders(2) + orderData. For slow-path: updateType(2) + pad(2) + numOrders(2) + pad(2) + orderData.
func (c *Client) handleOrders(data []byte, fastPath bool) {
	var numOrders int
	var orderData []byte
	if fastPath {
		if len(data) < 2 {
			return
		}
		numOrders = int(binary.LittleEndian.Uint16(data[0:2]))
		orderData = data[2:]
	} else {
		if len(data) < 8 {
			return
		}
		numOrders = int(binary.LittleEndian.Uint16(data[4:6]))
		orderData = data[8:]
	}

	// Bracket order batch with beginPaint/endPaint so the display renders
	// only after all orders are applied (prevents flicker during FullWindowDrag).
	c.beginPaint()
	defer c.endPaint()

	ctx := context.Background()
	var start time.Time
	if c.logFp.Enabled(ctx, LevelTrace) {
		start = time.Now()
	}
	c.logPdu.LogAttrs(ctx, slog.LevelDebug, "order batch", slog.Int("numOrders", numOrders), slog.Int("dataLen", len(orderData)))
	c.orderState.DebugBailReason = ""
	orders.DecodeOrders(&c.orderState, orderData, numOrders, c.executeOrder)
	if c.orderState.DebugBailReason != "" {
		c.logPdu.LogAttrs(ctx, slog.LevelWarn, "order decode bail", slog.String("reason", c.orderState.DebugBailReason), slog.Int("numOrders", numOrders))
	}
	if !start.IsZero() {
		c.logFp.LogAttrs(ctx, LevelTrace, "order batch",
			slog.Int("orders", numOrders),
			slog.Duration("total", time.Since(start)))
	}
}

// executeOrder handles a single decoded drawing order.
func (c *Client) executeOrder(state *orders.DecoderState, ord *orders.Order) {
	if ord.IsSecondary {
		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "secondary order", slog.Int("type", int(ord.SecondaryType)), slog.Int("dataLen", len(ord.SecData)))
		switch ord.SecondaryType {
		case orders.SecondaryCacheGlyph:
			orders.DecodeCacheGlyph(&c.glyphCache, ord.SecData)
		case orders.SecondaryBitmapUncompressed:
			c.decompBuf = orders.DecodeCacheBitmapV1(
				&c.bitmapCache, ord.SecData, false,
				c.decompBuf, c.palette())
		case orders.SecondaryCacheBitmap:
			c.decompBuf = orders.DecodeCacheBitmapV1(
				&c.bitmapCache, ord.SecData, true,
				c.decompBuf, c.palette())
		case orders.SecondaryBitmapUncompV2:
			c.decompBuf = orders.DecodeCacheBitmapV2(
				&c.bitmapCache, ord.ExtraFlags, ord.SecData, false,
				c.decompBuf, c.palette())
		case orders.SecondaryBitmapCompV2:
			c.decompBuf = orders.DecodeCacheBitmapV2(
				&c.bitmapCache, ord.ExtraFlags, ord.SecData, true,
				c.decompBuf, c.palette())
		case orders.SecondaryCacheBrush:
			orders.DecodeCacheBrush(&c.brushCache, ord.SecData)
		default:
			c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "unhandled secondary order", slog.Int("type", int(ord.SecondaryType)))
		}
		return
	}

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "primary order",
		slog.Int("type", int(ord.Type)),
		slog.Int("bL", int(state.Bounds.Left)), slog.Int("bT", int(state.Bounds.Top)),
		slog.Int("bR", int(state.Bounds.Right)), slog.Int("bB", int(state.Bounds.Bottom)))

	onBitmapFn := c.loadOnBitmap()
	if onBitmapFn == nil {
		return
	}

	// Set clip rect from bounds (MS-RDPEGDI 2.2.2.2.1.1.2)
	if ord.HasBounds {
		c.clipActive = true
		c.clipLeft = int(state.Bounds.Left)
		c.clipTop = int(state.Bounds.Top)
		c.clipRight = int(state.Bounds.Right)
		c.clipBottom = int(state.Bounds.Bottom)
	} else {
		c.clipActive = false
	}

	var pixels []byte
	var x, y, w, h int

	switch ord.Type {
	case orders.OrderGlyphIndex:
		pixels, x, y, w, h, c.glyphRenderBuf = orders.RenderGlyphIndex(
			c.glyphRenderBuf, &state.GlyphIndex, &c.glyphCache, c.serverBpp, c.palette())
		if pixels == nil && c.framebuf != nil {
			// Fallback: FB-direct for zero-rect cases (e.g. desktop icon labels)
			x, y, w, h = orders.RenderGlyphIndexFB(
				c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height,
				&state.GlyphIndex, &c.glyphCache, c.serverBpp, c.palette())
			c.onBitmapFromFB(x, y, w, h)
			return
		}
	case orders.OrderFastIndex:
		pixels, x, y, w, h, c.glyphRenderBuf = orders.RenderFastIndex(
			c.glyphRenderBuf, &state.FastIndex, &c.glyphCache, c.serverBpp, c.palette())
		if pixels == nil && c.framebuf != nil {
			x, y, w, h = orders.RenderFastIndexFB(
				c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height,
				&state.FastIndex, &c.glyphCache, c.serverBpp, c.palette())
			c.onBitmapFromFB(x, y, w, h)
			return
		}
	case orders.OrderFastGlyph:
		pixels, x, y, w, h, c.glyphRenderBuf = orders.RenderFastGlyph(
			c.glyphRenderBuf, &state.FastGlyph, &c.glyphCache, c.serverBpp, c.palette())
		if pixels == nil && c.framebuf != nil {
			x, y, w, h = orders.RenderFastGlyphFB(
				c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height,
				&state.FastGlyph, &c.glyphCache, c.serverBpp, c.palette())
			c.onBitmapFromFB(x, y, w, h)
			return
		}
	case orders.OrderOpaqueRect:
		pixels, x, y, w, h, c.glyphRenderBuf = orders.RenderOpaqueRect(
			c.glyphRenderBuf, &state.OpaqueRect, c.serverBpp, c.palette())
		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "OpaqueRect",
			slog.Int("x", x), slog.Int("y", y), slog.Int("w", w), slog.Int("h", h),
			slog.Int("r", int(state.OpaqueRect.ColorR)), slog.Int("g", int(state.OpaqueRect.ColorG)), slog.Int("b", int(state.OpaqueRect.ColorB)))
	case orders.OrderPatBlt:
		c.executePatBlt(&state.PatBlt)
		return
	case orders.OrderDstBlt:
		c.executeDstBlt(&state.DstBlt)
		return
	case orders.OrderScrBlt:
		c.executeScrBlt(&state.ScrBlt)
		return
	case orders.OrderMemBlt:
		c.executeMemBlt(&state.MemBlt)
		return
	case orders.OrderMem3Blt:
		c.executeMem3Blt(&state.Mem3Blt)
		return
	case orders.OrderLineTo:
		c.executeLineTo(&state.LineTo)
		return
	case orders.OrderPolyline:
		c.executePolyline(&state.Polyline)
		return
	case orders.OrderPolygonSC:
		c.executePolygonSC(&state.PolygonSC)
		return
	case orders.OrderPolygonCB:
		c.executePolygonCB(&state.PolygonCB)
		return
	case orders.OrderEllipseSC:
		c.executeEllipseSC(&state.EllipseSC)
		return
	case orders.OrderEllipseCB:
		c.executeEllipseCB(&state.EllipseCB)
		return
	case orders.OrderSaveBitmap:
		c.executeSaveBitmap(&state.SaveBitmap)
		return
	default:
		return
	}

	if pixels == nil {
		return
	}

	// Apply order bounds clip (glyph and OpaqueRect orders use the common path
	// and don't go through execute* functions that apply clipRect internally).
	if c.clipActive {
		cx, cy, cw, ch := c.clipRect(x, y, w, h)
		if cw <= 0 || ch <= 0 {
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "common-path CLIPPED OUT",
				slog.Int("ox", x), slog.Int("oy", y), slog.Int("ow", w), slog.Int("oh", h),
				slog.Int("cL", c.clipLeft), slog.Int("cT", c.clipTop), slog.Int("cR", c.clipRight), slog.Int("cB", c.clipBottom))
			return
		}
		if cx != x || cy != y || cw != w || ch != h {
			c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "common-path clip",
				slog.Int("ox", x), slog.Int("oy", y), slog.Int("ow", w), slog.Int("oh", h),
				slog.Int("cx", cx), slog.Int("cy", cy), slog.Int("cw", cw), slog.Int("ch", ch))
			// Extract clipped sub-rect from bottom-up pixel buffer.
			dx := cx - x
			startRow := (y + h) - (cy + ch) // bottom-up row offset
			srcStride := w * 4
			dstStride := cw * 4
			need := ch * dstStride
			if cap(c.clipBuf) >= need {
				c.clipBuf = c.clipBuf[:need]
			} else {
				c.clipBuf = make([]byte, need)
			}
			for r := 0; r < ch; r++ {
				srcOff := (startRow+r)*srcStride + dx*4
				dstOff := r * dstStride
				copy(c.clipBuf[dstOff:dstOff+dstStride], pixels[srcOff:srcOff+dstStride])
			}
			pixels = c.clipBuf
			x, y, w, h = cx, cy, cw, ch
		}
	}

	// Glyph orders use transparent background (alpha=0) for pixels that should
	// preserve the existing framebuffer content. Composite over the framebuffer
	// so the display always receives fully opaque pixels.
	isGlyph := ord.Type == orders.OrderGlyphIndex || ord.Type == orders.OrderFastIndex || ord.Type == orders.OrderFastGlyph
	if c.framebuf != nil {
		if isGlyph {
			need := w * h * 4
			if cap(c.scrBltBuf) >= need {
				c.scrBltBuf = c.scrBltBuf[:need]
			} else {
				c.scrBltBuf = make([]byte, need)
			}
			c.framebuf.ReadRect(c.scrBltBuf, x, y, w, h)
			for i := 0; i < need; i += 4 {
				if pixels[i+3] != 0 {
					c.scrBltBuf[i] = pixels[i]
					c.scrBltBuf[i+1] = pixels[i+1]
					c.scrBltBuf[i+2] = pixels[i+2]
					c.scrBltBuf[i+3] = pixels[i+3]
				}
			}
			c.framebuf.WriteRect(x, y, w, h, c.scrBltBuf)
			pixels = c.scrBltBuf
		} else {
			c.framebuf.WriteRect(x, y, w, h, pixels)
		}
	}

	onBitmapFn(&BitmapUpdate{
		X:            x,
		Y:            y,
		Width:        w,
		Height:       h,
		BitsPerPixel: 32,
		Data:         pixels,
	})
}

// onBitmapFromFB reads the dirty rect back from the framebuffer and sends it
// via onBitmap. Used for orders that render directly into the framebuffer
// (glyph orders that render directly via MS-RDPEGDI 2.2.2.2.1.2.5).
func (c *Client) onBitmapFromFB(x, y, w, h int) {
	fn := c.loadOnBitmap()
	if w <= 0 || h <= 0 || c.framebuf == nil || fn == nil {
		return
	}
	need := w * h * 4
	if cap(c.scrBltBuf) >= need {
		c.scrBltBuf = c.scrBltBuf[:need]
	} else {
		c.scrBltBuf = make([]byte, need)
	}
	c.framebuf.ReadRect(c.scrBltBuf, x, y, w, h)
	fn(&BitmapUpdate{
		X:            x,
		Y:            y,
		Width:        w,
		Height:       h,
		BitsPerPixel: 32,
		Data:         c.scrBltBuf,
	})
}

// clipRect clips a rectangle (x,y,w,h) to the active clip bounds.
// Returns the clipped rectangle. If the result is empty, w or h will be <= 0.
func (c *Client) clipRect(x, y, w, h int) (int, int, int, int) {
	if !c.clipActive {
		return x, y, w, h
	}
	// Intersect with clip bounds (right/bottom are inclusive per MS-RDPEGDI 2.2.2.2.1.1.2)
	if x < c.clipLeft {
		w -= c.clipLeft - x
		x = c.clipLeft
	}
	if y < c.clipTop {
		h -= c.clipTop - y
		y = c.clipTop
	}
	if x+w > c.clipRight+1 {
		w = c.clipRight + 1 - x
	}
	if y+h > c.clipBottom+1 {
		h = c.clipBottom + 1 - y
	}
	return x, y, w, h
}

// executeDstBlt draws a DstBlt directly into the framebuffer with full ROP3.
// Writes framebuffer and output buffer simultaneously (single pass, no read-back).
func (c *Client) executeDstBlt(s *orders.DstBltState) {
	if c.framebuf == nil {
		return
	}

	x := int(s.Left)
	y := int(s.Top)
	w := int(s.Width)
	h := int(s.Height)
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to framebuffer
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > c.framebuf.Width {
		w = c.framebuf.Width - x
	}
	if y+h > c.framebuf.Height {
		h = c.framebuf.Height - y
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to active clip bounds
	x, y, w, h = c.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "DstBlt",
		slog.Int("x", x), slog.Int("y", y), slog.Int("w", w), slog.Int("h", h),
		slog.Int("rop", int(s.Rop)))

	rop := s.Rop
	fb := c.framebuf.Pixels
	fbStride := c.framebuf.Stride
	fbBaseRow := c.framebuf.Height - y - h
	copyBytes := w * 4

	// Prepare output buffer
	need := w * h * 4
	if cap(c.glyphRenderBuf) >= need {
		c.glyphRenderBuf = c.glyphRenderBuf[:need]
	} else {
		c.glyphRenderBuf = make([]byte, need)
	}

	// BCE: prove max indices in-bounds
	_ = fb[(fbBaseRow+h-1)*fbStride+x*4+copyBytes-1]
	_ = c.glyphRenderBuf[(h-1)*copyBytes+copyBytes-1]

	switch rop {
	case 0x00: // BLACKNESS
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				fb[fi] = 0
				fb[fi+1] = 0
				fb[fi+2] = 0
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = 0
				c.glyphRenderBuf[oi+1] = 0
				c.glyphRenderBuf[oi+2] = 0
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	case 0xFF: // WHITENESS
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				fb[fi] = 0xFF
				fb[fi+1] = 0xFF
				fb[fi+2] = 0xFF
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = 0xFF
				c.glyphRenderBuf[oi+1] = 0xFF
				c.glyphRenderBuf[oi+2] = 0xFF
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	default:
		// Generic ROP3: pat=0xFF, src=0xFF, dst from framebuffer
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				rr := orders.ApplyROP3(rop, 0xFF, 0xFF, fb[fi])
				rg := orders.ApplyROP3(rop, 0xFF, 0xFF, fb[fi+1])
				rb := orders.ApplyROP3(rop, 0xFF, 0xFF, fb[fi+2])
				fb[fi] = rr
				fb[fi+1] = rg
				fb[fi+2] = rb
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	}

	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X:            x,
			Y:            y,
			Width:        w,
			Height:       h,
			BitsPerPixel: 32,
			Data:         c.glyphRenderBuf,
		})
	}
}

// executePatBlt draws a PatBlt directly into the framebuffer with full ROP3 + brush.
// Writes framebuffer and output buffer simultaneously (single pass, no read-back).
func (c *Client) executePatBlt(s *orders.PatBltState) {
	if c.framebuf == nil {
		return
	}

	x := int(s.Left)
	y := int(s.Top)
	w := int(s.Width)
	h := int(s.Height)
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to framebuffer
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > c.framebuf.Width {
		w = c.framebuf.Width - x
	}
	if y+h > c.framebuf.Height {
		h = c.framebuf.Height - y
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to active clip bounds
	x, y, w, h = c.clipRect(x, y, w, h)
	if w <= 0 || h <= 0 {
		return
	}

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "PatBlt",
		slog.Int("x", x), slog.Int("y", y), slog.Int("w", w), slog.Int("h", h),
		slog.Int("rop", int(s.Rop)), slog.Int("style", int(s.BrushStyle)),
		slog.Int("fg", int(s.ForeColor)), slog.Int("bg", int(s.BackColor)))

	rop := s.Rop
	fb := c.framebuf.Pixels
	fbStride := c.framebuf.Stride
	fbBaseRow := c.framebuf.Height - y - h
	copyBytes := w * 4

	// Prepare output buffer
	need := w * h * 4
	if cap(c.glyphRenderBuf) >= need {
		c.glyphRenderBuf = c.glyphRenderBuf[:need]
	} else {
		c.glyphRenderBuf = make([]byte, need)
	}

	// BCE: prove max indices in-bounds
	_ = fb[(fbBaseRow+h-1)*fbStride+x*4+copyBytes-1]
	_ = c.glyphRenderBuf[(h-1)*copyBytes+copyBytes-1]

	// BLACKNESS/WHITENESS fast paths — no brush or dst read needed
	switch rop {
	case 0x00: // BLACKNESS
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				fb[fi] = 0
				fb[fi+1] = 0
				fb[fi+2] = 0
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = 0
				c.glyphRenderBuf[oi+1] = 0
				c.glyphRenderBuf[oi+2] = 0
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
		if fn := c.loadOnBitmap(); fn != nil {
			fn(&BitmapUpdate{
				X: x, Y: y, Width: w, Height: h,
				BitsPerPixel: 32, Data: c.glyphRenderBuf,
			})
		}
		return
	case 0xFF: // WHITENESS
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				fb[fi] = 0xFF
				fb[fi+1] = 0xFF
				fb[fi+2] = 0xFF
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = 0xFF
				c.glyphRenderBuf[oi+1] = 0xFF
				c.glyphRenderBuf[oi+2] = 0xFF
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
		if fn := c.loadOnBitmap(); fn != nil {
			fn(&BitmapUpdate{
				X: x, Y: y, Width: w, Height: h,
				BitsPerPixel: 32, Data: c.glyphRenderBuf,
			})
		}
		return
	}

	// Resolve brush (same logic as executeMem3Blt)
	fgR, fgG, fgB := orders.ColourToRGBA(s.ForeColor, c.serverBpp, c.palette())
	bgR, bgG, bgB := orders.ColourToRGBA(s.BackColor, c.serverBpp, c.palette())

	// BS_NULL (style=1): hollow brush, no drawing.
	if s.BrushStyle == 0x01 {
		return
	}

	// BS_SOLID (style=0): fill with foreground color.
	if s.BrushStyle == 0x00 {
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				pr := orders.ApplyROP3(rop, fgR, 0xFF, fb[fi])
				pg := orders.ApplyROP3(rop, fgG, 0xFF, fb[fi+1])
				pb := orders.ApplyROP3(rop, fgB, 0xFF, fb[fi+2])
				fb[fi] = pr
				fb[fi+1] = pg
				fb[fi+2] = pb
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = pr
				c.glyphRenderBuf[oi+1] = pg
				c.glyphRenderBuf[oi+2] = pb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
		if fn := c.loadOnBitmap(); fn != nil {
			fn(&BitmapUpdate{
				X: x, Y: y, Width: w, Height: h,
				BitsPerPixel: 32, Data: c.glyphRenderBuf,
			})
		}
		return
	}

	brushMono, brushColorData, colorBrush := c.resolveBrush(
		s.BrushStyle, s.BrushHatch, s.BrushExtra)
	brushOrgX := int(s.BrushOrgX)
	brushOrgY := int(s.BrushOrgY)

	if rop == 0xF0 && !colorBrush {
		// PATCOPY mono fast path — write pattern directly, no dst read
		// Mono brush: bit=1 → bg, bit=0 → fg (inverted per MS-RDPEGDI 2.2.2.2.1.2.3)
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			patRow := brushMono[(y+h-1-r-brushOrgY)&7]
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				if patRow&(0x80>>uint((x+px-brushOrgX)&7)) != 0 {
					fb[fi] = bgR
					fb[fi+1] = bgG
					fb[fi+2] = bgB
					c.glyphRenderBuf[oi] = bgR
					c.glyphRenderBuf[oi+1] = bgG
					c.glyphRenderBuf[oi+2] = bgB
				} else {
					fb[fi] = fgR
					fb[fi+1] = fgG
					fb[fi+2] = fgB
					c.glyphRenderBuf[oi] = fgR
					c.glyphRenderBuf[oi+1] = fgG
					c.glyphRenderBuf[oi+2] = fgB
				}
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	} else if rop == 0xF0 && colorBrush {
		// PATCOPY color brush fast path
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			brushRowBase := ((y + h - 1 - r - brushOrgY) & 7) * 32
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				bi := brushRowBase + ((x+px-brushOrgX)&7)*4
				fb[fi] = brushColorData[bi]
				fb[fi+1] = brushColorData[bi+1]
				fb[fi+2] = brushColorData[bi+2]
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = brushColorData[bi]
				c.glyphRenderBuf[oi+1] = brushColorData[bi+1]
				c.glyphRenderBuf[oi+2] = brushColorData[bi+2]
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	} else if colorBrush {
		// Generic ROP3 with color brush, src=0xFF
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			brushRowBase := ((y + h - 1 - r - brushOrgY) & 7) * 32
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				bi := brushRowBase + ((x+px-brushOrgX)&7)*4
				rr := orders.ApplyROP3(rop, brushColorData[bi], 0xFF, fb[fi])
				rg := orders.ApplyROP3(rop, brushColorData[bi+1], 0xFF, fb[fi+1])
				rb := orders.ApplyROP3(rop, brushColorData[bi+2], 0xFF, fb[fi+2])
				fb[fi] = rr
				fb[fi+1] = rg
				fb[fi+2] = rb
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	} else {
		// Generic ROP3 with mono brush, src=0xFF
		// Mono brush: bit=1 → bg, bit=0 → fg (inverted per MS-RDPEGDI 2.2.2.2.1.2.3)
		for r := 0; r < h; r++ {
			fbOff := (fbBaseRow+r)*fbStride + x*4
			outOff := r * copyBytes
			patRow := brushMono[(y+h-1-r-brushOrgY)&7]
			for px := 0; px < w; px++ {
				fi := fbOff + px*4
				oi := outOff + px*4
				var patR, patG, patB byte
				if patRow&(0x80>>uint((x+px-brushOrgX)&7)) != 0 {
					patR, patG, patB = bgR, bgG, bgB
				} else {
					patR, patG, patB = fgR, fgG, fgB
				}
				rr := orders.ApplyROP3(rop, patR, 0xFF, fb[fi])
				rg := orders.ApplyROP3(rop, patG, 0xFF, fb[fi+1])
				rb := orders.ApplyROP3(rop, patB, 0xFF, fb[fi+2])
				fb[fi] = rr
				fb[fi+1] = rg
				fb[fi+2] = rb
				fb[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	}

	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X:            x,
			Y:            y,
			Width:        w,
			Height:       h,
			BitsPerPixel: 32,
			Data:         c.glyphRenderBuf,
		})
	}
}

// executeScrBlt handles screen-to-screen blit via the framebuffer with full ROP3.
func (c *Client) executeScrBlt(s *orders.ScrBltState) {
	if c.framebuf == nil {
		return
	}
	w := int(s.Width)
	h := int(s.Height)
	if w <= 0 || h <= 0 {
		return
	}

	if s.Rop == 0xCC {
		// Clip to active clip bounds
		dstX0, dstY0 := int(s.Left), int(s.Top)
		srcX0, srcY0 := int(s.SrcLeft), int(s.SrcTop)
		nx, ny, nw, nh := c.clipRect(dstX0, dstY0, w, h)
		if nw <= 0 || nh <= 0 {
			return
		}
		srcX0 += nx - dstX0
		srcY0 += ny - dstY0

		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "ScrBlt",
			slog.Int("dx", nx), slog.Int("dy", ny), slog.Int("w", nw), slog.Int("h", nh),
			slog.Int("sx", srcX0), slog.Int("sy", srcY0), slog.Int("rop", int(s.Rop)))

		// SRCCOPY fast path — CopyRect handles overlap via scratch buffer
		c.scrBltBuf = c.framebuf.CopyRect(
			nx, ny,
			srcX0, srcY0,
			nw, nh, c.scrBltBuf)

		if fn := c.loadOnBitmap(); fn != nil {
			fn(&BitmapUpdate{
				X:            nx,
				Y:            ny,
				Width:        nw,
				Height:       nh,
				BitsPerPixel: 32,
				Data:         c.scrBltBuf[:nw*nh*4],
			})
		}
		return
	}

	// Generic ROP3 path: read src region first (overlap safety), then apply per-pixel
	dstX := int(s.Left)
	dstY := int(s.Top)
	srcX := int(s.SrcLeft)
	srcY := int(s.SrcTop)

	// Clip dst to framebuffer
	if dstX < 0 {
		srcX -= dstX
		w += dstX
		dstX = 0
	}
	if dstY < 0 {
		srcY -= dstY
		h += dstY
		dstY = 0
	}
	if dstX+w > c.framebuf.Width {
		w = c.framebuf.Width - dstX
	}
	if dstY+h > c.framebuf.Height {
		h = c.framebuf.Height - dstY
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to active clip bounds
	{
		nx, ny, nw, nh := c.clipRect(dstX, dstY, w, h)
		srcX += nx - dstX
		srcY += ny - dstY
		dstX, dstY, w, h = nx, ny, nw, nh
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Read source into scratch before modifying destination (overlap safety)
	need := w * h * 4
	if cap(c.scrBltBuf) >= need {
		c.scrBltBuf = c.scrBltBuf[:need]
	} else {
		c.scrBltBuf = make([]byte, need)
	}
	c.framebuf.ReadRect(c.scrBltBuf, srcX, srcY, w, h)

	// Prepare output buffer
	if cap(c.glyphRenderBuf) >= need {
		c.glyphRenderBuf = c.glyphRenderBuf[:need]
	} else {
		c.glyphRenderBuf = make([]byte, need)
	}

	// Apply ROP3 per pixel: pat=0xFF, src from scratch, dst from framebuffer
	rop := s.Rop
	fb := c.framebuf.Pixels
	fbStride := c.framebuf.Stride
	fbBaseRow := c.framebuf.Height - dstY - h
	copyBytes := w * 4

	// BCE: prove max indices in-bounds
	_ = fb[(fbBaseRow+h-1)*fbStride+dstX*4+copyBytes-1]
	_ = c.scrBltBuf[(h-1)*copyBytes+copyBytes-1]
	_ = c.glyphRenderBuf[(h-1)*copyBytes+copyBytes-1]

	for r := 0; r < h; r++ {
		fbOff := (fbBaseRow+r)*fbStride + dstX*4
		bufOff := r * copyBytes
		for px := 0; px < w; px++ {
			fi := fbOff + px*4
			bi := bufOff + px*4
			rr := orders.ApplyROP3(rop, 0xFF, c.scrBltBuf[bi], fb[fi])
			rg := orders.ApplyROP3(rop, 0xFF, c.scrBltBuf[bi+1], fb[fi+1])
			rb := orders.ApplyROP3(rop, 0xFF, c.scrBltBuf[bi+2], fb[fi+2])
			fb[fi] = rr
			fb[fi+1] = rg
			fb[fi+2] = rb
			fb[fi+3] = 0xFF
			c.glyphRenderBuf[bi] = rr
			c.glyphRenderBuf[bi+1] = rg
			c.glyphRenderBuf[bi+2] = rb
			c.glyphRenderBuf[bi+3] = 0xFF
		}
	}

	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X:            dstX,
			Y:            dstY,
			Width:        w,
			Height:       h,
			BitsPerPixel: 32,
			Data:         c.glyphRenderBuf,
		})
	}
}

// executeMemBlt handles memory-to-screen blit from the bitmap cache with full ROP3.
// Writes to framebuffer and emits bitmap update in a single pass (no double-copy).
func (c *Client) executeMemBlt(s *orders.MemBltState) {
	if c.framebuf == nil {
		return
	}

	// Low byte of CacheID is the actual cache ID
	entry := c.bitmapCache.Get(int(s.CacheID&0xFF), int(s.CacheIndex))
	if entry == nil {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "MemBlt cache miss",
			slog.Int("cacheID", int(s.CacheID&0xFF)), slog.Int("cacheIndex", int(s.CacheIndex)),
			slog.Int("left", int(s.Left)), slog.Int("top", int(s.Top)),
			slog.Int("width", int(s.Width)), slog.Int("height", int(s.Height)))
		return
	}

	dstX := int(s.Left)
	dstY := int(s.Top)
	srcX := int(s.SrcLeft)
	srcY := int(s.SrcTop)
	w := int(s.Width)
	h := int(s.Height)

	// Clamp source rect to cached bitmap bounds
	if srcX < 0 {
		dstX -= srcX
		w += srcX
		srcX = 0
	}
	if srcY < 0 {
		dstY -= srcY
		h += srcY
		srcY = 0
	}
	if srcX+w > entry.Width {
		w = entry.Width - srcX
	}
	if srcY+h > entry.Height {
		h = entry.Height - srcY
	}
	// Clamp to framebuffer bounds
	if dstX < 0 {
		srcX -= dstX
		w += dstX
		dstX = 0
	}
	if dstY < 0 {
		srcY -= dstY
		h += dstY
		dstY = 0
	}
	if dstX+w > c.framebuf.Width {
		w = c.framebuf.Width - dstX
	}
	if dstY+h > c.framebuf.Height {
		h = c.framebuf.Height - dstY
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to active clip bounds
	{
		nx, ny, nw, nh := c.clipRect(dstX, dstY, w, h)
		srcX += nx - dstX
		srcY += ny - dstY
		dstX, dstY, w, h = nx, ny, nw, nh
	}
	if w <= 0 || h <= 0 {
		return
	}

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "MemBlt",
		slog.Int("dx", dstX), slog.Int("dy", dstY), slog.Int("w", w), slog.Int("h", h),
		slog.Int("sx", srcX), slog.Int("sy", srcY), slog.Int("rop", int(s.Rop)),
		slog.Int("cacheID", int(s.CacheID&0xFF)), slog.Int("cacheIdx", int(s.CacheIndex)),
		slog.Int("entryW", entry.Width), slog.Int("entryH", entry.Height))

	// Prepare output buffer for callback (reuse glyphRenderBuf)
	need := w * h * 4
	if cap(c.glyphRenderBuf) >= need {
		c.glyphRenderBuf = c.glyphRenderBuf[:need]
	} else {
		c.glyphRenderBuf = make([]byte, need)
	}

	// Both cache entry and framebuffer are bottom-up RGBA.
	// After clipping, all rows are in-range — no per-row checks needed.
	entryStride := entry.Width * 4
	copyBytes := w * 4
	cacheBaseRow := entry.Height - srcY - h
	fbStride := c.framebuf.Stride
	fbPixels := c.framebuf.Pixels
	fbBaseRow := c.framebuf.Height - dstY - h

	// BCE: prove max indices are in-bounds so the compiler eliminates per-iteration checks.
	_ = entry.Data[(cacheBaseRow+h-1)*entryStride+srcX*4+copyBytes-1]
	_ = fbPixels[(fbBaseRow+h-1)*fbStride+dstX*4+copyBytes-1]
	_ = c.glyphRenderBuf[(h-1)*copyBytes+copyBytes-1]

	rop := s.Rop
	if rop == 0xCC {
		// SRCCOPY fast path — bulk copy
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			src := entry.Data[cacheOff : cacheOff+copyBytes]
			copy(fbPixels[fbOff:fbOff+copyBytes], src)
			copy(c.glyphRenderBuf[outOff:outOff+copyBytes], src)
		}
	} else {
		// Generic ROP3: pat=0xFF, src from cache, dst from framebuffer
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				ci := cacheOff + px*4
				fi := fbOff + px*4
				oi := outOff + px*4
				rr := orders.ApplyROP3(rop, 0xFF, entry.Data[ci], fbPixels[fi])
				rg := orders.ApplyROP3(rop, 0xFF, entry.Data[ci+1], fbPixels[fi+1])
				rb := orders.ApplyROP3(rop, 0xFF, entry.Data[ci+2], fbPixels[fi+2])
				fbPixels[fi] = rr
				fbPixels[fi+1] = rg
				fbPixels[fi+2] = rb
				fbPixels[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	}

	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X:            dstX,
			Y:            dstY,
			Width:        w,
			Height:       h,
			BitsPerPixel: 32,
			Data:         c.glyphRenderBuf,
		})
	}
}

// emitDirtyRect reads a bounding box from the framebuffer and emits it as a BitmapUpdate.
func (c *Client) emitDirtyRect(x, y, w, h int) {
	onBitmapFn := c.loadOnBitmap()
	if onBitmapFn == nil || w <= 0 || h <= 0 {
		return
	}
	// Clamp to framebuffer bounds
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > c.framebuf.Width {
		w = c.framebuf.Width - x
	}
	if y+h > c.framebuf.Height {
		h = c.framebuf.Height - y
	}
	if w <= 0 || h <= 0 {
		return
	}
	need := w * h * 4
	if cap(c.scrBltBuf) >= need {
		c.scrBltBuf = c.scrBltBuf[:need]
	} else {
		c.scrBltBuf = make([]byte, need)
	}
	c.framebuf.ReadRect(c.scrBltBuf, x, y, w, h)
	onBitmapFn(&BitmapUpdate{
		X: x, Y: y, Width: w, Height: h,
		BitsPerPixel: 32,
		Data:         c.scrBltBuf[:need],
	})
}

// executeLineTo draws a line directly into the framebuffer.
func (c *Client) executeLineTo(s *orders.LineToState) {
	if c.framebuf == nil {
		return
	}
	x, y, w, h := orders.DrawLineTo(c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.serverBpp, c.palette())
	c.emitDirtyRect(x, y, w, h)
}

// executePolyline draws connected line segments directly into the framebuffer.
func (c *Client) executePolyline(s *orders.PolylineState) {
	if c.framebuf == nil {
		return
	}
	var x, y, w, h int
	x, y, w, h, c.polyDeltaBuf = orders.DrawPolyline(
		c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.polyDeltaBuf, c.serverBpp, c.palette())
	c.emitDirtyRect(x, y, w, h)
}

// executeEllipseSC draws a solid-color ellipse directly into the framebuffer.
func (c *Client) executeEllipseSC(s *orders.EllipseSCState) {
	if c.framebuf == nil {
		return
	}
	x, y, w, h := orders.DrawEllipseSC(c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.serverBpp, c.palette())
	c.emitDirtyRect(x, y, w, h)
}

// executePolygonSC draws a solid-color filled polygon directly into the framebuffer.
func (c *Client) executePolygonSC(s *orders.PolygonSCState) {
	if c.framebuf == nil {
		return
	}
	var x, y, w, h int
	x, y, w, h, c.polyDeltaBuf = orders.DrawPolygonSC(
		c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.polyDeltaBuf, c.serverBpp, c.palette())
	c.emitDirtyRect(x, y, w, h)
}

// executePolygonCB draws a brush-filled polygon directly into the framebuffer.
func (c *Client) executePolygonCB(s *orders.PolygonCBState) {
	if c.framebuf == nil {
		return
	}

	fgR, fgG, fgB := orders.ColourToRGBA(s.ForeColor, c.serverBpp, c.palette())
	bgR, bgG, bgB := orders.ColourToRGBA(s.BackColor, c.serverBpp, c.palette())

	if s.BrushStyle == 0x01 { // BS_NULL
		return
	}

	brushMono, brushColorData, colorBrush := c.resolveBrush(
		s.BrushStyle, s.BrushHatch, s.BrushExtra)

	var x, y, w, h int
	x, y, w, h, c.polyDeltaBuf = orders.DrawPolygonCB(
		c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.polyDeltaBuf, c.serverBpp,
		brushMono, brushColorData, colorBrush,
		fgR, fgG, fgB, bgR, bgG, bgB,
		int(s.BrushOrgX), int(s.BrushOrgY), s.Rop2)
	c.emitDirtyRect(x, y, w, h)
}

// executeEllipseCB draws a brush-filled ellipse directly into the framebuffer.
func (c *Client) executeEllipseCB(s *orders.EllipseCBState) {
	if c.framebuf == nil {
		return
	}

	fgR, fgG, fgB := orders.ColourToRGBA(s.ForeColor, c.serverBpp, c.palette())
	bgR, bgG, bgB := orders.ColourToRGBA(s.BackColor, c.serverBpp, c.palette())

	if s.BrushStyle == 0x01 { // BS_NULL
		return
	}

	brushMono, brushColorData, colorBrush := c.resolveBrush(
		s.BrushStyle, s.BrushHatch, s.BrushExtra)

	x, y, w, h := orders.DrawEllipseCB(
		c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height, s, c.serverBpp,
		brushMono, brushColorData, colorBrush,
		fgR, fgG, fgB, bgR, bgG, bgB,
		int(s.BrushOrgX), int(s.BrushOrgY), s.Rop2)
	c.emitDirtyRect(x, y, w, h)
}

// resolveBrush resolves a brush from style/hatch/extra into mono pattern, color data, and type flag.
func (c *Client) resolveBrush(style, hatch uint8, extra [7]byte) (brushMono [8]byte, brushColorData [256]byte, colorBrush bool) {
	if style&0x80 != 0 {
		cached := c.brushCache.Get(hatch)
		if cached != nil {
			if cached.Mono {
				brushMono = cached.MonoData
			} else {
				brushColorData = cached.Data
				colorBrush = true
			}
		} else {
			brushMono = [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		}
	} else if style == 0x03 {
		brushMono[7] = hatch
		for i := range 7 {
			brushMono[6-i] = extra[i]
		}
	} else if style == 0x02 {
		if int(hatch) < len(orders.HatchPatterns) {
			brushMono = orders.HatchPatterns[hatch]
		} else {
			brushMono = [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		}
	} else {
		brushMono = [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	}
	return
}

// executeSaveBitmap handles the SaveBitmap/DesktopSave primary order.
// Action 0 saves a framebuffer rectangle to cache; action 1 restores it.
func (c *Client) executeSaveBitmap(s *orders.SaveBitmapState) {
	if c.framebuf == nil || c.desktopSaveBuf == nil {
		return
	}

	left := int(s.Left)
	top := int(s.Top)
	right := int(s.Right)
	bottom := int(s.Bottom)

	c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "SaveBitmap",
		slog.Int("action", int(s.Action)), slog.Int("offset", int(s.Offset)),
		slog.Int("left", left), slog.Int("top", top),
		slog.Int("right", right), slog.Int("bottom", bottom))

	if s.Action == 0 {
		// Save: copy framebuffer region into desktop cache
		c.desktopSaveBuf.Save(s.Offset, left, top, right, bottom,
			c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height)
	} else {
		// Restore: copy desktop cache back into framebuffer
		x, y, w, h := c.desktopSaveBuf.Restore(s.Offset, left, top, right, bottom,
			c.framebuf.Pixels, c.framebuf.Width, c.framebuf.Height)
		if w > 0 && h > 0 {
			c.emitDirtyRect(x, y, w, h)
		}
	}
}

// executeMem3Blt handles memory-to-screen blit with ternary ROP from the bitmap cache.
func (c *Client) executeMem3Blt(s *orders.Mem3BltState) {
	if c.framebuf == nil {
		return
	}

	entry := c.bitmapCache.Get(int(s.CacheID&0xFF), int(s.CacheIndex))
	if entry == nil {
		c.logPdu.LogAttrs(context.Background(), slog.LevelWarn, "Mem3Blt cache miss",
			slog.Int("cacheID", int(s.CacheID&0xFF)), slog.Int("cacheIndex", int(s.CacheIndex)),
			slog.Int("left", int(s.Left)), slog.Int("top", int(s.Top)),
			slog.Int("width", int(s.Width)), slog.Int("height", int(s.Height)))
		return
	}

	dstX := int(s.Left)
	dstY := int(s.Top)
	srcX := int(s.SrcLeft)
	srcY := int(s.SrcTop)
	w := int(s.Width)
	h := int(s.Height)

	// Clamp source rect to cached bitmap bounds
	if srcX < 0 {
		dstX -= srcX
		w += srcX
		srcX = 0
	}
	if srcY < 0 {
		dstY -= srcY
		h += srcY
		srcY = 0
	}
	if srcX+w > entry.Width {
		w = entry.Width - srcX
	}
	if srcY+h > entry.Height {
		h = entry.Height - srcY
	}
	if dstX < 0 {
		srcX -= dstX
		w += dstX
		dstX = 0
	}
	if dstY < 0 {
		srcY -= dstY
		h += dstY
		dstY = 0
	}
	if dstX+w > c.framebuf.Width {
		w = c.framebuf.Width - dstX
	}
	if dstY+h > c.framebuf.Height {
		h = c.framebuf.Height - dstY
	}
	if w <= 0 || h <= 0 {
		return
	}

	// Clip to active clip bounds
	{
		nx, ny, nw, nh := c.clipRect(dstX, dstY, w, h)
		srcX += nx - dstX
		srcY += ny - dstY
		dstX, dstY, w, h = nx, ny, nw, nh
	}
	if w <= 0 || h <= 0 {
		return
	}

	rop := s.Rop
	fgR, fgG, fgB := orders.ColourToRGBA(s.ForeColor, c.serverBpp, c.palette())
	bgR, bgG, bgB := orders.ColourToRGBA(s.BackColor, c.serverBpp, c.palette())

	// Resolve brush: monochrome pattern, color cache, or solid.
	var brushMono [8]byte        // monochrome 8x8 pattern (bit=1 → bg, 0 → fg)
	var brushColorData [256]byte // color 8x8 RGBA (stack-local, avoids pointer chase)
	colorBrush := false
	solidBrush := false // true = use fg directly as pattern (BS_SOLID)

	if s.BrushStyle == 0x00 {
		// BS_SOLID: pattern = fg for all pixels
		solidBrush = true
	} else if s.BrushStyle == 0x01 {
		// BS_NULL: hollow brush — no pattern fill (rare in Mem3Blt, treat as solid fg)
		solidBrush = true
	} else if s.BrushStyle&0x80 != 0 {
		// Cached brush — BrushHatch is cache entry index
		cached := c.brushCache.Get(s.BrushHatch)
		if cached != nil {
			if cached.Mono {
				brushMono = cached.MonoData
			} else {
				brushColorData = cached.Data
				colorBrush = true
			}
		} else {
			// Cache miss → solid forecolor
			solidBrush = true
		}
	} else if s.BrushStyle == 0x03 {
		// RDP4 brush: reverse byte order (MS-RDPEGDI 2.2.2.2.1.2.3)
		brushMono[7] = s.BrushHatch
		for i := range 7 {
			brushMono[6-i] = s.BrushExtra[i]
		}
	} else if s.BrushStyle == 0x02 {
		// Hatch pattern
		if int(s.BrushHatch) < len(orders.HatchPatterns) {
			brushMono = orders.HatchPatterns[s.BrushHatch]
		} else {
			solidBrush = true
		}
	} else {
		// Unknown style → treat as solid fg
		solidBrush = true
	}
	brushOrgX := int(s.BrushOrgX)
	brushOrgY := int(s.BrushOrgY)

	// Prepare output buffer
	need := w * h * 4
	if cap(c.glyphRenderBuf) >= need {
		c.glyphRenderBuf = c.glyphRenderBuf[:need]
	} else {
		c.glyphRenderBuf = make([]byte, need)
	}

	entryStride := entry.Width * 4
	copyBytes := w * 4
	cacheBaseRow := entry.Height - srcY - h
	fbStride := c.framebuf.Stride
	fbPixels := c.framebuf.Pixels
	fbBaseRow := c.framebuf.Height - dstY - h

	// BCE: prove max indices are in-bounds so the compiler eliminates per-iteration checks.
	maxCacheIdx := (cacheBaseRow+h-1)*entryStride + srcX*4 + copyBytes - 1
	maxFbIdx := (fbBaseRow+h-1)*fbStride + dstX*4 + copyBytes - 1
	maxOutIdx := (h-1)*copyBytes + copyBytes - 1
	_ = entry.Data[maxCacheIdx]
	_ = fbPixels[maxFbIdx]
	_ = c.glyphRenderBuf[maxOutIdx]

	if rop == 0xCC {
		// SRCCOPY fast path — same as MemBlt
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			src := entry.Data[cacheOff : cacheOff+copyBytes]
			copy(fbPixels[fbOff:fbOff+copyBytes], src)
			copy(c.glyphRenderBuf[outOff:outOff+copyBytes], src)
		}
	} else if solidBrush {
		// Solid brush: use fg as pattern for all pixels
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			for px := 0; px < w; px++ {
				ci := cacheOff + px*4
				fi := fbOff + px*4
				oi := outOff + px*4
				srcR, srcG, srcB := entry.Data[ci], entry.Data[ci+1], entry.Data[ci+2]
				dstR, dstG, dstB := fbPixels[fi], fbPixels[fi+1], fbPixels[fi+2]
				rr := orders.ApplyROP3(rop, fgR, srcR, dstR)
				rg := orders.ApplyROP3(rop, fgG, srcG, dstG)
				rb := orders.ApplyROP3(rop, fgB, srcB, dstB)
				fbPixels[fi] = rr
				fbPixels[fi+1] = rg
				fbPixels[fi+2] = rb
				fbPixels[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	} else if colorBrush {
		// Color brush ROP3: sample RGBA directly from stack-local brush copy
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			brushRowBase := ((dstY + h - 1 - r - brushOrgY) & 7) * 32 // screenRow * 8 pixels * 4 bytes
			for px := 0; px < w; px++ {
				ci := cacheOff + px*4
				fi := fbOff + px*4
				oi := outOff + px*4
				srcR, srcG, srcB := entry.Data[ci], entry.Data[ci+1], entry.Data[ci+2]
				dstR, dstG, dstB := fbPixels[fi], fbPixels[fi+1], fbPixels[fi+2]
				bi := brushRowBase + ((dstX+px-brushOrgX)&7)*4
				patR, patG, patB := brushColorData[bi], brushColorData[bi+1], brushColorData[bi+2]
				rr := orders.ApplyROP3(rop, patR, srcR, dstR)
				rg := orders.ApplyROP3(rop, patG, srcG, dstG)
				rb := orders.ApplyROP3(rop, patB, srcB, dstB)
				fbPixels[fi] = rr
				fbPixels[fi+1] = rg
				fbPixels[fi+2] = rb
				fbPixels[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	} else {
		// Monochrome brush ROP3: bit=1 → bg, bit=0 → fg (inverted per MS-RDPEGDI 2.2.2.2.1.2.3)
		for r := 0; r < h; r++ {
			cacheOff := (cacheBaseRow+r)*entryStride + srcX*4
			fbOff := (fbBaseRow+r)*fbStride + dstX*4
			outOff := r * copyBytes
			patRow := brushMono[(dstY+h-1-r-brushOrgY)&7]
			for px := 0; px < w; px++ {
				ci := cacheOff + px*4
				fi := fbOff + px*4
				oi := outOff + px*4
				srcR, srcG, srcB := entry.Data[ci], entry.Data[ci+1], entry.Data[ci+2]
				dstR, dstG, dstB := fbPixels[fi], fbPixels[fi+1], fbPixels[fi+2]
				var patR, patG, patB byte
				if patRow&(0x80>>uint((dstX+px-brushOrgX)&7)) != 0 {
					patR, patG, patB = bgR, bgG, bgB
				} else {
					patR, patG, patB = fgR, fgG, fgB
				}
				rr := orders.ApplyROP3(rop, patR, srcR, dstR)
				rg := orders.ApplyROP3(rop, patG, srcG, dstG)
				rb := orders.ApplyROP3(rop, patB, srcB, dstB)
				fbPixels[fi] = rr
				fbPixels[fi+1] = rg
				fbPixels[fi+2] = rb
				fbPixels[fi+3] = 0xFF
				c.glyphRenderBuf[oi] = rr
				c.glyphRenderBuf[oi+1] = rg
				c.glyphRenderBuf[oi+2] = rb
				c.glyphRenderBuf[oi+3] = 0xFF
			}
		}
	}

	if fn := c.loadOnBitmap(); fn != nil {
		fn(&BitmapUpdate{
			X:            dstX,
			Y:            dstY,
			Width:        w,
			Height:       h,
			BitsPerPixel: 32,
			Data:         c.glyphRenderBuf,
		})
	}
}

// handlePointerNull notifies the callback to hide the cursor.
// handlePaletteUpdate processes a palette update (MS-RDPBCGR 2.2.9.1.1.3.1.1).
// Format: pad(2) + numColors(u16) + pad(2) + RGB entries (3 bytes each).
// MS-RDPBCGR 2.2.9.1.1.3.1.1 TS_UPDATE_PALETTE_DATA.
func (c *Client) handlePaletteUpdate(data []byte) {
	if len(data) < 6 {
		return
	}
	nColors := int(binary.LittleEndian.Uint16(data[2:4]))
	off := 6
	if off+nColors*3 > len(data) {
		nColors = (len(data) - off) / 3
	}
	if c.framebuf != nil {
		for i := 0; i < nColors && i < 256; i++ {
			c.framebuf.Palette[i] = [3]byte{data[off], data[off+1], data[off+2]}
			off += 3
		}
		c.logPdu.LogAttrs(context.Background(), slog.LevelDebug, "palette update", slog.Int("colors", nColors))
	}
}

func (c *Client) handlePointerNull() {
	if fn := c.loadOnPointer(); fn != nil {
		fn(&PointerUpdate{Type: PointerNull})
	}
}

// handlePointerDefault notifies the callback to restore the default OS cursor.
func (c *Client) handlePointerDefault() {
	if fn := c.loadOnPointer(); fn != nil {
		fn(&PointerUpdate{Type: PointerDefault})
	}
}

// handlePointerColor decodes a 24bpp color pointer update, caches it, and notifies.
func (c *Client) handlePointerColor(data []byte) {
	pu, buf, err := pointer.DecodeColorPointer(c.logPtr, c.pointerBuf, data)
	if err != nil {
		c.logPtr.LogAttrs(context.Background(), slog.LevelError, "pointer color decode failed", slog.Any("err", err))
		return
	}
	c.pointerBuf = buf
	c.cacheAndNotifyPointer(&pu)
}

// handlePointerNew decodes a variable-bpp pointer update, caches it, and notifies.
func (c *Client) handlePointerNew(data []byte) {
	pu, buf, err := pointer.DecodeNewPointer(c.logPtr, c.pointerBuf, data)
	if err != nil {
		c.logPtr.LogAttrs(context.Background(), slog.LevelError, "pointer new decode failed", slog.Any("err", err))
		return
	}
	c.pointerBuf = buf
	c.cacheAndNotifyPointer(&pu)
}

// handlePointerLarge decodes a large pointer update (u32 lengths), caches it, and notifies.
func (c *Client) handlePointerLarge(data []byte) {
	pu, buf, err := pointer.DecodeLargePointer(c.logPtr, c.pointerBuf, data)
	if err != nil {
		c.logPtr.LogAttrs(context.Background(), slog.LevelError, "pointer large decode failed", slog.Any("err", err))
		return
	}
	c.pointerBuf = buf
	c.cacheAndNotifyPointer(&pu)
}

// handlePointerCached looks up a cached pointer by index and notifies.
func (c *Client) handlePointerCached(data []byte) {
	idx, err := pointer.DecodeCached(data)
	if err != nil {
		c.logPtr.LogAttrs(context.Background(), slog.LevelError, "pointer cached decode failed", slog.Any("err", err))
		return
	}
	if int(idx) < len(c.pointerCache) && c.pointerCache[idx] != nil {
		if fn := c.loadOnPointer(); fn != nil {
			fn(c.pointerCache[idx])
		}
	} else {
		if fn := c.loadOnPointer(); fn != nil {
			fn(&PointerUpdate{Type: PointerCached, CacheIndex: idx})
		}
	}
}

// cacheAndNotifyPointer stores an owned copy of the pointer RGBA data in the
// cache (since pointerBuf is reused) and notifies the callback.
func (c *Client) cacheAndNotifyPointer(pu *pointer.PointerUpdate) {
	update := &PointerUpdate{
		Type:       PointerShape,
		CacheIndex: pu.CacheIndex,
		HotSpotX:   pu.HotSpotX,
		HotSpotY:   pu.HotSpotY,
		Width:      pu.Width,
		Height:     pu.Height,
		Data:       make([]byte, len(pu.Data)),
	}
	copy(update.Data, pu.Data)

	if int(pu.CacheIndex) < len(c.pointerCache) {
		c.pointerCache[pu.CacheIndex] = update
	}
	if fn := c.loadOnPointer(); fn != nil {
		fn(update)
	}
}

// handleSlowPathPointer dispatches slow-path pointer PDUs (PDUType2Pointer).
// Wire: messageType(u16) + pad(u16) + pointer data.
func (c *Client) handleSlowPathPointer(data []byte) {
	if len(data) < 4 {
		return
	}
	msgType := binary.LittleEndian.Uint16(data[0:2])
	payload := data[4:]

	switch msgType {
	case pointer.MsgPtrSystem:
		sysType, err := pointer.DecodeSystem(payload)
		if err != nil {
			return
		}
		switch sysType {
		case pointer.SystemPtrNull:
			c.handlePointerNull()
		default:
			c.handlePointerDefault()
		}
	case pointer.MsgPtrColor:
		c.handlePointerColor(payload)
	case pointer.MsgPtrNew:
		c.handlePointerNew(payload)
	case pointer.MsgPtrLarge:
		c.handlePointerLarge(payload)
	case pointer.MsgPtrCached:
		c.handlePointerCached(payload)
	case pointer.MsgPtrPosition:
		// Server-side position update — we don't track this
	}
}

// registerChannelHandler registers a handler for a named static virtual channel.
// The handler receives the channel ID and payload (after CHANNEL_PDU_HEADER is stripped).
func (c *Client) registerChannelHandler(name string, handler func(uint16, []byte)) {
	if c.channelHandlers == nil {
		c.channelHandlers = make(map[string]func(uint16, []byte))
	}
	c.channelHandlers[name] = handler
}

// handleVirtualChannelData dispatches data received on a virtual channel.
// Strips the CHANNEL_PDU_HEADER (8 bytes: totalLength u32 + flags u32),
// reassembles chunked data, and delivers the complete payload to the
// registered handler. Data is already decrypted by decryptSlowPath.
func (c *Client) handleVirtualChannelData(channelID uint16, data []byte) {
	name := c.channelMap[channelID]
	if name == "" {
		c.log.LogAttrs(context.Background(), slog.LevelWarn, "VC data on unmapped channel", slog.Int("channelID", int(channelID)), slog.Int("bytes", len(data)))
		return
	}
	handler := c.channelHandlers[name]
	if handler == nil {
		c.log.LogAttrs(context.Background(), slog.LevelWarn, "VC data but no handler registered", slog.String("channel", name), slog.Int("channelID", int(channelID)))
		return
	}
	// Security header already stripped and decrypted by decryptSlowPath.

	// CHANNEL_PDU_HEADER: totalLength(4) + flags(4)
	if len(data) < 8 {
		return
	}

	const (
		channelFlagFirst uint32 = 0x00000001
		channelFlagLast  uint32 = 0x00000002
	)

	flags := binary.LittleEndian.Uint32(data[4:8])
	chunk := data[8:]

	if flags&channelFlagFirst != 0 && flags&channelFlagLast != 0 {
		// Single complete PDU — no reassembly needed (common case)
		if old, exists := c.vcReassembly[channelID]; exists {
			c.vcReassemblyTotal -= len(old)
			delete(c.vcReassembly, channelID)
		}
		handler(channelID, chunk)
		return
	}

	if flags&channelFlagFirst != 0 {
		// Start of multi-chunk sequence. Cap pre-allocation to avoid
		// OOM from a malformed totalLength value (untrusted server data).
		totalLen := binary.LittleEndian.Uint32(data[0:4])
		const maxVCReassembly = 64 * 1024 * 1024        // 64 MB per channel
		const maxVCReassemblyGlobal = 256 * 1024 * 1024 // 256 MB total
		if totalLen > maxVCReassembly {
			c.log.LogAttrs(context.Background(), slog.LevelWarn, "VC reassembly totalLen exceeds limit, dropping",
				slog.String("channel", name), slog.Int("totalLen", int(totalLen)))
			return
		}
		if c.vcReassemblyTotal+int(totalLen) > maxVCReassemblyGlobal {
			c.log.LogAttrs(context.Background(), slog.LevelWarn, "VC reassembly global limit exceeded, dropping",
				slog.String("channel", name), slog.Int("total", c.vcReassemblyTotal), slog.Int("totalLen", int(totalLen)))
			return
		}
		// Evict any stale buffer for this channel before starting a new one.
		if old, exists := c.vcReassembly[channelID]; exists {
			c.vcReassemblyTotal -= len(old)
		}
		buf := make([]byte, 0, totalLen)
		buf = append(buf, chunk...)
		c.vcReassembly[channelID] = buf
		c.vcReassemblyTotal += len(buf)
		return
	}

	// Middle or last chunk — append to reassembly buffer
	buf, ok := c.vcReassembly[channelID]
	if !ok {
		c.log.LogAttrs(context.Background(), slog.LevelWarn, "VC chunk without FIRST, dropping", slog.String("channel", name), util.Hex8("flags", flags))
		return
	}
	oldLen := len(buf)
	buf = append(buf, chunk...)
	c.vcReassemblyTotal += len(buf) - oldLen

	if flags&channelFlagLast != 0 {
		c.vcReassemblyTotal -= len(buf)
		delete(c.vcReassembly, channelID)
		handler(channelID, buf)
	} else {
		c.vcReassembly[channelID] = buf
	}
}

// vcChunkSize is the maximum virtual channel chunk size (MS-RDPBCGR 2.2.1.3.2).
// Data larger than this must be split across multiple CHANNEL_PDU_HEADER packets.
const vcChunkSize = 1600

// maxFragReassembly is the maximum size of a fast-path fragment reassembly buffer.
// Prevents unbounded memory growth from a server streaming FragNext without FragLast.
const maxFragReassembly = 64 * 1024 * 1024 // 64 MB

// sendChannelData sends data on a named static virtual channel.
// Prepends CHANNEL_PDU_HEADER and wraps in MCS/X.224/TPKT.
// Data larger than vcChunkSize is split into multiple chunks per MS-RDPBCGR 2.2.6.1.
func (c *Client) sendChannelData(name string, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Look up channel ID
	var channelID uint16
	for id, n := range c.channelMap {
		if n == name {
			channelID = id
			break
		}
	}
	if channelID == 0 {
		return fmt.Errorf("channel %q not found", name)
	}
	const (
		channelFlagFirst        uint32 = 0x00000001
		channelFlagLast         uint32 = 0x00000002
		channelFlagShowProtocol uint32 = 0x00000010
	)

	showProto := c.channelOpts[channelID]&mcs.ChannelOptionShowProtocol != 0
	totalLen := uint32(len(data))

	for off := 0; off < len(data); {
		end := min(off+vcChunkSize, len(data))
		chunk := data[off:end]

		// CHANNEL_PDU_HEADER flags
		var flags uint32
		if off == 0 {
			flags |= channelFlagFirst
		}
		if end == len(data) {
			flags |= channelFlagLast
		}
		if showProto {
			flags |= channelFlagShowProtocol
		}

		// CHANNEL_PDU_HEADER: totalLength(4) + flags(4) + chunk
		pdu := make([]byte, 8+len(chunk))
		binary.LittleEndian.PutUint32(pdu[0:4], totalLen)
		binary.LittleEndian.PutUint32(pdu[4:8], flags)
		copy(pdu[8:], chunk)

		var payload []byte
		if c.crypto != nil {
			payload = c.crypto.Encrypt(pdu, sec.Encrypt)
		} else {
			payload = pdu
		}

		mcsData := mcs.EncodeSendDataRequest(c.userChannelID, channelID, payload)
		if err := c.tpktConn.Write(x224.EncodeDataTPDU(mcsData)); err != nil {
			return err
		}
		off = end
	}
	return nil
}

// handleFastPathBitmap decodes a fast-path bitmap update and delivers rects.
func (c *Client) handleFastPathBitmap(data []byte) {
	rects, err := pdu.DecodeFastPathBitmapUpdate(data)
	if err != nil {
		c.logFp.LogAttrs(context.Background(), slog.LevelError, "fast-path bitmap decode error", slog.Any("err", err))
		return
	}
	c.processBitmapRects(rects)
}

// processBitmapRects decompresses and delivers bitmap rectangles to the callback.
// Shared by slow-path and fast-path bitmap update handlers.
// Uses a single stack-allocated BitmapUpdate reused per iteration (callback is synchronous).
func (c *Client) processBitmapRects(rects []pdu.BitmapData) {
	onBitmapFn := c.loadOnBitmap()
	if onBitmapFn == nil {
		return
	}

	// Bracket bitmap batch with beginPaint/endPaint so the display renders
	// only after all rects are applied (prevents flicker during FullWindowDrag).
	c.beginPaint()
	defer c.endPaint()

	ctx := context.Background()
	tracing := c.logFp.Enabled(ctx, LevelTrace)
	var batchStart, t0 time.Time
	var decompTotal, fbTotal, cbTotal time.Duration
	if tracing {
		batchStart = time.Now()
	}

	var update BitmapUpdate
	for i := range rects {
		r := &rects[i]
		data := r.Data
		bpp := int(r.BitsPerPixel)
		isCompressed := r.Flags&pdu.BitmapCompression != 0

		c.logFp.LogAttrs(ctx, slog.LevelDebug, "bitmap rect",
			slog.Int("i", i),
			slog.Int("destL", int(r.DestLeft)), slog.Int("destT", int(r.DestTop)),
			slog.Int("destR", int(r.DestRight)), slog.Int("destB", int(r.DestBottom)),
			slog.Int("W", int(r.Width)), slog.Int("H", int(r.Height)),
			slog.Int("bpp", bpp), slog.Int("flags", int(r.Flags)),
			slog.Int("dataLen", len(data)))

		if isCompressed {
			if tracing {
				t0 = time.Now()
			}
			var err error
			if bpp == 32 {
				// 32 bpp uses RDP 6.0 Planar Codec — no TS_CD_HEADER.
				c.decompBuf, err = rle.DecompressPlanar(c.decompBuf, int(r.Width), int(r.Height), data)
			} else {
				rleData := data
				// Strip TS_CD_HEADER (8 bytes) if present and use compressedSize
				// from the header (MS-RDPBCGR 2.2.9.1.1.3.1.2.2).
				if r.Flags&pdu.NoBitmapCompressionHdr == 0 && len(data) > 8 {
					compressedSize := int(binary.LittleEndian.Uint16(data[2:4]))
					rleData = data[8:]
					if compressedSize > 0 && compressedSize < len(rleData) {
						rleData = rleData[:compressedSize]
					}
				}
				c.decompBuf, err = rle.DecompressInto(c.decompBuf, int(r.Width), int(r.Height), bpp, rleData)
			}
			if tracing {
				decompTotal += time.Since(t0)
			}
			if err != nil {
				c.logFp.LogAttrs(ctx, slog.LevelError, "bitmap decompression failed", slog.Any("err", err))
				continue
			}
			data = c.decompBuf
		} else if bpp == 32 {
			// Uncompressed 32bpp wire data is top-down BGRX — convert to bottom-up RGBA.
			w := int(r.Width)
			h := int(r.Height)
			need := w * h * 4
			if cap(c.decompBuf) < need {
				c.decompBuf = make([]byte, need)
			} else {
				c.decompBuf = c.decompBuf[:need]
			}
			rowBytes := w * 4
			for sy := 0; sy < h; sy++ {
				srcOff := sy * rowBytes
				dstOff := (h - 1 - sy) * rowBytes
				for px := 0; px < w; px++ {
					si := srcOff + px*4
					di := dstOff + px*4
					if si+3 < len(data) {
						c.decompBuf[di] = data[si+2]   // R
						c.decompBuf[di+1] = data[si+1] // G
						c.decompBuf[di+2] = data[si]   // B
						c.decompBuf[di+3] = 0xFF       // A
					}
				}
			}
			data = c.decompBuf
		} else {
			// Uncompressed non-32bpp wire data is top-down — flip to bottom-up.
			w := int(r.Width)
			h := int(r.Height)
			bytesPP := bppToBytes(bpp)
			if bytesPP > 0 {
				rowBytes := w * bytesPP
				need := h * rowBytes
				if cap(c.decompBuf) < need {
					c.decompBuf = make([]byte, need)
				} else {
					c.decompBuf = c.decompBuf[:need]
				}
				for sy := 0; sy < h; sy++ {
					srcOff := sy * rowBytes
					dstOff := (h - 1 - sy) * rowBytes
					if srcOff+rowBytes <= len(data) {
						copy(c.decompBuf[dstOff:dstOff+rowBytes], data[srcOff:srcOff+rowBytes])
					}
				}
				data = c.decompBuf
			}
		}

		// Crop bitmap to destination rect (MS-RDPBCGR 2.2.9.1.1.3.1.2.2):
		// Width/Height are the bitmap data dimensions (stride), while
		// DestLeft..DestRight / DestTop..DestBottom define the screen area.
		cx := int(r.DestRight) - int(r.DestLeft) + 1
		cy := int(r.DestBottom) - int(r.DestTop) + 1
		bmpW := int(r.Width)
		bmpH := int(r.Height)
		if cx > bmpW {
			cx = bmpW
		}
		if cy > bmpH {
			cy = bmpH
		}
		if cx <= 0 || cy <= 0 {
			continue
		}

		// Write to framebuffer BEFORE cropping — WriteRectBpp handles bmpW
		// stride natively, reading only cx pixels per bmpW-wide row.
		if c.framebuf != nil {
			if tracing {
				t0 = time.Now()
			}
			c.framebuf.WriteRectBpp(int(r.DestLeft), int(r.DestTop), cx, cy, bpp, bmpW, data)
			if tracing {
				fbTotal += time.Since(t0)
			}
		}

		// 8bpp data is palette-indexed; read back RGBA from framebuffer
		// which already did the palette lookup in WriteRectBpp.
		if bpp == 8 && c.framebuf != nil {
			if tracing {
				t0 = time.Now()
			}
			c.onBitmapFromFB(int(r.DestLeft), int(r.DestTop), cx, cy)
			if tracing {
				cbTotal += time.Since(t0)
			}
			continue
		}

		// Crop bitmap for the OnBitmap callback which expects compact rows.
		if cx < bmpW {
			bytesPP := bppToBytes(bpp)
			if bytesPP > 0 {
				srcStride := bmpW * bytesPP
				dstStride := cx * bytesPP
				for row := 1; row < cy; row++ {
					copy(data[row*dstStride:row*dstStride+dstStride], data[row*srcStride:row*srcStride+dstStride])
				}
				data = data[:cy*dstStride]
			}
		} else if cy < bmpH {
			bytesPP := bppToBytes(bpp)
			if bytesPP > 0 {
				data = data[:cy*bmpW*bytesPP]
			}
		}

		if tracing {
			t0 = time.Now()
		}
		update = BitmapUpdate{
			X:            int(r.DestLeft),
			Y:            int(r.DestTop),
			Width:        cx,
			Height:       cy,
			BitsPerPixel: bpp,
			IsCompressed: false,
			Data:         data,
		}
		onBitmapFn(&update)
		if tracing {
			cbTotal += time.Since(t0)
		}
	}

	if tracing {
		c.logFp.LogAttrs(ctx, LevelTrace, "bitmap batch",
			slog.Int("rects", len(rects)),
			slog.Duration("decompress", decompTotal),
			slog.Duration("framebuf", fbTotal),
			slog.Duration("callback", cbTotal),
			slog.Duration("total", time.Since(batchStart)))
	}
}

// State returns the current connection state
func (c *Client) State() ConnectionState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

func (c *Client) setState(state ConnectionState) {
	c.stateMu.Lock()
	c.state = state
	c.stateMu.Unlock()
}

// callback loaders — read under cbMu.RLock for concurrent access from any goroutine.

func (c *Client) loadOnBitmap() func(*BitmapUpdate) {
	c.cbMu.RLock()
	fn := c.onBitmap
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnStridedBitmap() func(x, y, w, h int, data []byte, stride int) {
	c.cbMu.RLock()
	fn := c.onStridedBitmap
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnH264Frame() func(*egfx.H264Frame) {
	c.cbMu.RLock()
	fn := c.onH264Frame
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnBeginPaint() func() {
	c.cbMu.RLock()
	fn := c.onBeginPaint
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnEndPaint() func() {
	c.cbMu.RLock()
	fn := c.onEndPaint
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnPointer() func(*PointerUpdate) {
	c.cbMu.RLock()
	fn := c.onPointer
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnResize() func(width, height int) {
	c.cbMu.RLock()
	fn := c.onResize
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnDynChannel() func(name string, ch *dvc.DynChannel) {
	c.cbMu.RLock()
	fn := c.onDynChannel
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnDisconnect() func(error) {
	c.cbMu.RLock()
	fn := c.onDisconnect
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnReconnecting() func() {
	c.cbMu.RLock()
	fn := c.onReconnecting
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnReconnected() func() {
	c.cbMu.RLock()
	fn := c.onReconnected
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnClipboardUpdate() func(hasText, hasImage bool) {
	c.cbMu.RLock()
	fn := c.onClipboardUpdate
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnClipboardText() func(text string) {
	c.cbMu.RLock()
	fn := c.onClipboardText
	c.cbMu.RUnlock()
	return fn
}

func (c *Client) loadOnClipboardImage() func(pngData []byte) {
	c.cbMu.RLock()
	fn := c.onClipboardImage
	c.cbMu.RUnlock()
	return fn
}

// OnBitmap sets the callback for bitmap updates
func (c *Client) OnBitmap(fn func(*BitmapUpdate)) {
	c.cbMu.Lock()
	c.onBitmap = fn
	c.cbMu.Unlock()
}

// OnStridedBitmap sets the callback for EGFX bitmap updates with strided surface data.
// Data points directly into the surface buffer; the callback must not retain it.
// stride is the byte offset between consecutive rows (may be wider than w*4).
// This is only called for GFX pipeline updates. Non-GFX updates still use OnBitmap.
func (c *Client) OnStridedBitmap(fn func(x, y, w, h int, data []byte, stride int)) {
	c.cbMu.Lock()
	c.onStridedBitmap = fn
	c.cbMu.Unlock()
}

// OnH264Frame sets the callback for H.264 pass-through frames.
// When set, the client advertises AVC support in EGFX capabilities
// and forwards raw H.264 NAL units instead of decoding them.
// Must be set before Connect().
func (c *Client) OnH264Frame(fn func(*egfx.H264Frame)) {
	c.cbMu.Lock()
	c.onH264Frame = fn
	c.cbMu.Unlock()
}

// OnBeginPaint sets the callback invoked before a batch of display updates
// (EGFX frame flush, fast-path bitmap/order batch, or slow-path update).
// The display can acquire a lock here so that all per-rect bitmap callbacks
// within the batch are applied atomically.
func (c *Client) OnBeginPaint(fn func()) {
	c.cbMu.Lock()
	c.onBeginPaint = fn
	c.cbMu.Unlock()
}

// OnEndPaint sets the callback invoked after all updates in a batch have
// been emitted. The display should release any lock acquired in OnBeginPaint.
func (c *Client) OnEndPaint(fn func()) {
	c.cbMu.Lock()
	c.onEndPaint = fn
	c.cbMu.Unlock()
}

// beginPaint calls the OnBeginPaint callback if not already inside a paint.
// Reentrant-safe: nested calls (e.g. EGFX inside a fast-path PDU) are no-ops.
func (c *Client) beginPaint() {
	if fn := c.loadOnBeginPaint(); !c.painting && fn != nil {
		c.painting = true
		fn()
	}
}

// endPaint calls the OnEndPaint callback if this is the outermost paint.
func (c *Client) endPaint() {
	if c.painting {
		c.painting = false
		if fn := c.loadOnEndPaint(); fn != nil {
			fn()
		}
	}
}

// OnPointer sets the callback for pointer (cursor) updates
func (c *Client) OnPointer(fn func(*PointerUpdate)) {
	c.cbMu.Lock()
	c.onPointer = fn
	c.cbMu.Unlock()
}

// OnResize sets the callback invoked after the server completes a session resize
// (deactivation/reactivation). The new width and height are in pixels.
func (c *Client) OnResize(fn func(width, height int)) {
	c.cbMu.Lock()
	c.onResize = fn
	c.cbMu.Unlock()
}

// OnDynChannel sets the callback for when the server creates a dynamic virtual channel.
func (c *Client) OnDynChannel(fn func(name string, ch *dvc.DynChannel)) {
	c.cbMu.Lock()
	c.onDynChannel = fn
	c.cbMu.Unlock()
}

// OnDisconnect sets the callback for disconnection events.
// With auto-reconnect enabled, this only fires after all attempts are exhausted.
func (c *Client) OnDisconnect(fn func(error)) {
	c.cbMu.Lock()
	c.onDisconnect = fn
	c.cbMu.Unlock()
}

// OnReconnecting sets the callback fired when auto-reconnect starts.
func (c *Client) OnReconnecting(fn func()) {
	c.cbMu.Lock()
	c.onReconnecting = fn
	c.cbMu.Unlock()
}

// OnReconnected sets the callback fired after a successful auto-reconnect.
func (c *Client) OnReconnected(fn func()) {
	c.cbMu.Lock()
	c.onReconnected = fn
	c.cbMu.Unlock()
}

// GetOnDisconnect returns the current disconnect callback.
func (c *Client) GetOnDisconnect() func(error) { return c.loadOnDisconnect() }

// GetOnReconnecting returns the current reconnecting callback.
func (c *Client) GetOnReconnecting() func() { return c.loadOnReconnecting() }

// GetOnReconnected returns the current reconnected callback.
func (c *Client) GetOnReconnected() func() { return c.loadOnReconnected() }

// OnClipboardUpdate sets the callback invoked when the remote clipboard changes.
// hasText is true if the server advertises CF_UNICODETEXT; hasImage is true if
// the server advertises CF_DIB. Call RequestClipboard / RequestClipboardImage
// to fetch the data.
func (c *Client) OnClipboardUpdate(fn func(hasText, hasImage bool)) {
	c.cbMu.Lock()
	c.onClipboardUpdate = fn
	c.cbMu.Unlock()
}

// OnClipboardText sets the callback invoked when clipboard text data arrives
// from the server (in response to RequestClipboard).
func (c *Client) OnClipboardText(fn func(text string)) {
	c.cbMu.Lock()
	c.onClipboardText = fn
	c.cbMu.Unlock()
}

// OnClipboardImage sets the callback invoked when clipboard image data arrives
// from the server (in response to RequestClipboardImage). The data is PNG-encoded.
func (c *Client) OnClipboardImage(fn func(pngData []byte)) {
	c.cbMu.Lock()
	c.onClipboardImage = fn
	c.cbMu.Unlock()
}

// SetClipboardEnabled toggles clipboard forwarding at runtime. When disabled
// the protocol channel stays alive but no data is exchanged. When re-enabled,
// a fresh format list is sent to the server.
func (c *Client) SetClipboardEnabled(enabled bool) {
	if c.clipHandler == nil {
		return
	}
	c.clipHandler.SetEnabled(enabled)
}

// SetClipboard updates the local clipboard content and notifies the server
// that CF_UNICODETEXT is available. The server will request the data when
// the user pastes.
func (c *Client) SetClipboard(text string) error {
	if c.clipHandler == nil {
		return ErrNotConnected
	}
	return c.clipHandler.SetLocalClipboard(text)
}

// SetClipboardImage updates the local clipboard image (PNG-encoded) and
// notifies the server. The server will request CF_DIB when the user pastes.
func (c *Client) SetClipboardImage(pngData []byte) error {
	if c.clipHandler == nil {
		return ErrNotConnected
	}
	return c.clipHandler.SetLocalImage(pngData)
}

// RequestClipboard sends a request to the server for its clipboard text.
// The result is delivered asynchronously via the OnClipboardText callback.
func (c *Client) RequestClipboard() error {
	if c.clipHandler == nil {
		return ErrNotConnected
	}
	return c.clipHandler.RequestRemoteText()
}

// RequestClipboardImage sends a request to the server for its clipboard image.
// The result is delivered asynchronously via the OnClipboardImage callback as PNG.
func (c *Client) RequestClipboardImage() error {
	if c.clipHandler == nil {
		return ErrNotConnected
	}
	return c.clipHandler.RequestRemoteImage()
}

// Done returns a channel that is closed when the connection ends,
// whether by explicit Close() or unexpected disconnect.
func (c *Client) Done() <-chan struct{} {
	c.closeMu.Lock()
	done := c.done
	c.closeMu.Unlock()
	return done
}

// SendKeyboard sends a keyboard event
func (c *Client) SendKeyboard(scancode uint16, pressed bool) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	flags := uint16(0)
	if !pressed {
		flags |= pdu.KbdFlagsRelease
	}
	// Motty divergence: a scancode of the form 0xE0xx is the extended key xx
	// (arrows, Home, the Windows key); upstream dropped the extended flag.
	if scancode&0xFF00 == 0xE000 {
		flags |= pdu.KbdFlagsExtended
		scancode &= 0x00FF
	}
	// Stack-allocated: input header (4) + scancode event (12) = 16 bytes
	var buf [pdu.InputPDUHeaderSize + pdu.ScancodeEventSize]byte
	payload := pdu.AppendInputPDUHeader(buf[:0], 1)
	payload = pdu.AppendScancodeEvent(payload, scancode, flags)
	return c.sendDataPDU(pdu.PDUType2Input, payload)
}

// SendUnicode sends a Unicode keyboard event (TS_UNICODE_KEYBOARD_EVENT,
// MS-RDPBCGR 2.2.8.1.1.3.1.1.1). Use this for characters that don't map
// to a physical scancode (e.g. accented characters, CJK, emoji).
func (c *Client) SendUnicode(codepoint uint16, pressed bool) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	flags := uint16(0)
	if !pressed {
		flags |= pdu.KbdFlagsRelease
	}
	var buf [pdu.InputPDUHeaderSize + pdu.UnicodeEventSize]byte
	payload := pdu.AppendInputPDUHeader(buf[:0], 1)
	payload = pdu.AppendUnicodeEvent(payload, codepoint, flags)
	return c.sendDataPDU(pdu.PDUType2Input, payload)
}

// SendMouse sends a mouse event
func (c *Client) SendMouse(x, y int, buttons uint16) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	// Stack-allocated: input header (4) + mouse event (12) = 16 bytes
	var buf [pdu.InputPDUHeaderSize + pdu.MouseEventSize]byte
	payload := pdu.AppendInputPDUHeader(buf[:0], 1)
	payload = pdu.AppendMouseEvent(payload, buttons, uint16(x), uint16(y))
	return c.sendDataPDU(pdu.PDUType2Input, payload)
}

// SendMouseWheel sends a mouse wheel event. Positive delta scrolls up/right,
// negative scrolls down/left. Set horizontal to true for horizontal scrolling.
// Uses TS_POINTER_EVENT (InputMouse 0x8001) with PTRFLAGS_WHEEL/HWHEEL flags.
// Rotation is encoded as 0x78 (120) per click, matching Windows WHEEL_DELTA.
func (c *Client) SendMouseWheel(x, y int, delta int, horizontal bool) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	var flags uint16
	if horizontal {
		flags = pdu.PtrFlagsHWheel
	} else {
		flags = pdu.PtrFlagsWheel
	}
	if delta < 0 {
		flags |= pdu.PtrFlagsWheelNegative
		// Negative: two's complement in low byte (256 - rotation)
		rotation := min(-delta*0x78, 0xFF)
		flags |= uint16(0x100-rotation) & 0x00FF
	} else {
		rotation := min(delta*0x78, 0xFF)
		flags |= uint16(rotation) & 0x00FF
	}

	var buf [pdu.InputPDUHeaderSize + pdu.MouseEventSize]byte
	payload := pdu.AppendInputPDUHeader(buf[:0], 1)
	payload = pdu.AppendMouseEvent(payload, flags, uint16(x), uint16(y))
	return c.sendDataPDU(pdu.PDUType2Input, payload)
}

// Resize requests the server to change the session resolution dynamically
// via MS-RDPEDISP. If the display control channel isn't ready yet, the
// resize is queued and sent automatically when the channel opens.
func (c *Client) Resize(width, height int) error {
	c.displayMu.Lock()
	defer c.displayMu.Unlock()
	return c.resizeLocked(width, height)
}

// CanResize reports whether the server has opened its display control channel,
// so that ResizeDisplay changes the desktop's size at once and keeps the
// session. A server that has not (Windows before 8, or one that is still
// starting the channel) would only reconnect.
func (c *Client) CanResize() bool { return c.dispReady.Load() }

// ErrResizeUnsupported is returned by ResizeDisplay when the server does not
// support changing the desktop's size in a running session.
var ErrResizeUnsupported = errors.New("rdp: the server cannot change the desktop size in a running session")

// ResizeDisplay asks the server for a width x height desktop whose interface is
// drawn at scale percent (one of the Windows scale levels: 100, 125, 150 ...),
// in the running session. Unlike Resize it never reconnects: it returns
// ErrResizeUnsupported when CanResize is false.
func (c *Client) ResizeDisplay(width, height int, scale uint32) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	if !c.CanResize() {
		return ErrResizeUnsupported
	}
	c.displayMu.Lock()
	defer c.displayMu.Unlock()
	c.opts.DesktopScaleFactor = scale
	return c.resizeLocked(width, height)
}

// resizeLocked is Resize with displayMu held.
func (c *Client) resizeLocked(width, height int) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	// Update stored dimensions so sendConfirmActive() uses the new size
	// during the upcoming reactivation (DISP path) or reconnect (fallback).
	c.opts.Width = uint16(width)
	c.opts.Height = uint16(height)

	if c.dispHandler == nil {
		// Server doesn't support DISP (pre-Win8): reconnect with new dimensions.
		c.log.LogAttrs(context.Background(), slog.LevelInfo, "DISP not available, reconnect-based resize",
			slog.Int("width", width), slog.Int("height", height))
		go c.resizeViaReconnect()
		return nil
	}
	if !c.dispHandler.Ready() {
		// DISP channel opened but caps not received yet — queue.
		c.log.LogAttrs(context.Background(), slog.LevelDebug, "display channel not ready, queuing resize",
			slog.Int("width", width), slog.Int("height", height))
		c.pendingResizeW = uint16(width)
		c.pendingResizeH = uint16(height)
		return nil
	}
	if c.opts.DesktopScaleFactor > 0 {
		if err := c.dispHandler.ResizeWithDPI(uint32(width), uint32(height), c.opts.DesktopScaleFactor, c.opts.DeviceScaleFactor); err != nil {
			c.log.LogAttrs(context.Background(), slog.LevelError, "resize send error", slog.Any("err", err))
			return err
		}
	} else {
		if err := c.dispHandler.Resize(uint32(width), uint32(height)); err != nil {
			c.log.LogAttrs(context.Background(), slog.LevelError, "resize send error", slog.Any("err", err))
			return err
		}
	}
	return nil
}

// ResizeMulti requests a multi-monitor layout change via MS-RDPEDISP.
// Each monitor in the slice describes position, size, and flags. Exactly one
// monitor must have the primary flag (0x01). The bounding box of all monitors
// is used for c.opts.Width/Height during reactivation.
//
// If the display control channel isn't ready yet, the layout is queued and
// sent automatically when the channel opens.
func (c *Client) ResizeMulti(monitors []disp.MonitorLayout) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}

	// Compute bounding box and update stored dimensions.
	// Propagate DPI scale from opts so resize preserves desktop scaling.
	var maxRight, maxBottom int32
	for i := range monitors {
		m := &monitors[i]
		r := m.Left + int32(m.Width)
		b := m.Top + int32(m.Height)
		if r > maxRight {
			maxRight = r
		}
		if b > maxBottom {
			maxBottom = b
		}
		if c.opts.DesktopScaleFactor > 0 && m.DesktopScaleFactor == 0 {
			m.DesktopScaleFactor = c.opts.DesktopScaleFactor
		}
		if c.opts.DeviceScaleFactor > 0 && m.DeviceScaleFactor == 0 {
			m.DeviceScaleFactor = c.opts.DeviceScaleFactor
		}
	}
	c.opts.Width = uint16(maxRight)
	c.opts.Height = uint16(maxBottom)

	if c.dispHandler == nil {
		// Server doesn't support DISP (pre-Win8): reconnect with new dimensions.
		c.log.LogAttrs(context.Background(), slog.LevelInfo, "DISP not available, reconnect-based resize",
			slog.Int("width", int(c.opts.Width)), slog.Int("height", int(c.opts.Height)))
		go c.resizeViaReconnect()
		return nil
	}
	if !c.dispHandler.Ready() {
		c.log.LogAttrs(context.Background(), slog.LevelDebug, "display channel not ready, queuing multi-monitor resize", slog.Int("monitors", len(monitors)))
		c.pendingMonitors = monitors
		return nil
	}
	if err := c.dispHandler.ResizeMulti(monitors); err != nil {
		c.log.LogAttrs(context.Background(), slog.LevelError, "resize multi send error", slog.Any("err", err))
		return err
	}
	return nil
}

// RefreshRect requests the server to redraw the specified screen area.
func (c *Client) RefreshRect(x, y, width, height int) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	// RefreshRect coordinates are inclusive: a w×h rect at (x,y) spans
	// (x,y)..(x+w-1,y+h-1). A zero/negative size underflows x+width-1 to 0xFFFF,
	// turning an "empty" request (e.g. the keepalive's RefreshRect(0,0,0,0)) into a
	// full-screen refresh — which forces the server to repaint the whole desktop.
	// Clamp to a minimal 1×1 so an empty request stays a single pixel.
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return c.sendDataPDU(pdu.PDUType2RefreshRect,
		pdu.EncodeRefreshRect(uint16(x), uint16(y), uint16(x+width-1), uint16(y+height-1)))
}

// Close closes the RDP connection
func (c *Client) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	if c.closed || c.reconnecting {
		return nil
	}
	c.closed = true
	close(c.done)

	c.setState(StateDisconnected)

	if c.tlsConn != nil {
		c.tlsConn.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	if c.egfxHandler != nil {
		c.egfxHandler.Close()
	}

	c.log.LogAttrs(context.Background(), slog.LevelInfo, "connection closed")
	return nil
}

// DumpFramebuffer saves the current framebuffer as a PPM image file.
// Returns the filename written, or an error.
// FramebufferTopDown returns the framebuffer contents as top-down RGBA and its dimensions.
// Returns nil if no framebuffer exists.
func (c *Client) FramebufferTopDown() (pix []byte, w, h int) {
	c.framebufMu.RLock()
	defer c.framebufMu.RUnlock()
	if c.framebuf == nil {
		return nil, 0, 0
	}
	w, h = c.framebuf.Width, c.framebuf.Height
	stride := c.framebuf.Stride
	dstStride := w * 4
	pix = make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		srcOff := (h - 1 - y) * stride
		dstOff := y * dstStride
		copy(pix[dstOff:dstOff+dstStride], c.framebuf.Pixels[srcOff:srcOff+dstStride])
	}
	return pix, w, h
}

// FramebufferWriteTopDown writes the framebuffer as top-down RGBA into dst.
// Returns (width, height). dst must be at least width*height*4 bytes.
// Returns (0, 0) if no framebuffer exists or dst is too small.
// palette returns a pointer to the framebuffer palette for 8bpp color lookup.
func (c *Client) palette() *[256][3]byte {
	if c.framebuf != nil {
		return &c.framebuf.Palette
	}
	return nil
}

// FramebufferDims returns the current framebuffer width and height.
// Returns (0, 0) if no framebuffer is allocated.
func (c *Client) FramebufferDims() (int, int) {
	c.framebufMu.RLock()
	defer c.framebufMu.RUnlock()
	if c.framebuf == nil {
		return 0, 0
	}
	return c.framebuf.Width, c.framebuf.Height
}

func (c *Client) FramebufferWriteTopDown(dst []byte) (int, int) {
	c.framebufMu.RLock()
	defer c.framebufMu.RUnlock()
	if c.framebuf == nil {
		return 0, 0
	}
	w, h := c.framebuf.Width, c.framebuf.Height
	need := w * h * 4
	if len(dst) < need {
		return 0, 0
	}
	stride := c.framebuf.Stride
	dstStride := w * 4
	for y := 0; y < h; y++ {
		srcOff := (h - 1 - y) * stride
		dstOff := y * dstStride
		copy(dst[dstOff:dstOff+dstStride], c.framebuf.Pixels[srcOff:srcOff+dstStride])
	}
	return w, h
}

// ReadRectBGRA copies the screen rectangle (x, y, w, h), clipped to the screen,
// into dst as top-down BGRA rows w*4 bytes apart (fourth byte 0xFF), and returns
// the clipped rectangle; zero width means nothing was copied. Motty addition.
// It reads the framebuffer without locking, so it may only be called from a
// display callback (OnBitmap, OnStridedBitmap, OnResize), which run on the
// goroutine that writes the framebuffer.
func (c *Client) ReadRectBGRA(dst []byte, x, y, w, h int) (rx, ry, rw, rh int) {
	fb := c.framebuf
	if fb == nil {
		return 0, 0, 0, 0
	}
	rx, ry, rw, rh = fb.clipRect(x, y, w, h)
	if rw <= 0 || rh <= 0 || len(dst) < rw*rh*4 {
		return 0, 0, 0, 0
	}
	for row := 0; row < rh; row++ {
		// Row row of the rectangle is screen row ry+row, which the bottom-up
		// framebuffer keeps at index fb.Height-1-(ry+row).
		src := fb.Pixels[(fb.Height-1-(ry+row))*fb.Stride+rx*4:][:rw*4]
		out := dst[row*rw*4:][:rw*4]
		for i := 0; i < len(src); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = src[i+2], src[i+1], src[i], 0xFF
		}
	}
	return rx, ry, rw, rh
}

// SetOutputSuppressed asks the server to stop (or resume) sending screen
// updates, as a minimized window does (MS-RDPBCGR 2.2.11.3). Resuming makes the
// server repaint the whole desktop. Motty addition.
func (c *Client) SetOutputSuppressed(suppressed bool) error {
	if c.State() != StateConnected {
		return ErrNotConnected
	}
	if suppressed {
		return c.sendDataPDU(pdu.PDUType2SuppressOutput, pdu.EncodeSuppressOutput(false, 0, 0, 0, 0))
	}
	w, h := c.FramebufferDims()
	if w < 1 || h < 1 {
		return nil
	}
	return c.sendDataPDU(pdu.PDUType2SuppressOutput, pdu.EncodeSuppressOutput(true, 0, 0, uint16(w-1), uint16(h-1)))
}
