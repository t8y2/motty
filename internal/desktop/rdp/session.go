// Package rdp implements desktop.Session over the RDP client of package
// rdpcore (a trimmed copy of an MIT-licensed library, see
// docs/remote-desktop.md).
package rdp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/vivaui/viva/audio"

	"motty/internal/desktop"
	rdpcore "motty/internal/rdpcore"
)

// ErrAuthFailed is returned by Open when the server refused the account or the
// password: the caller asks the user and opens again.
var ErrAuthFailed = errors.New("rdp: the server refused the account or password")

// ErrAuthRequired is returned by Open when there is no password and no way to
// ask for one.
var ErrAuthRequired = errors.New("rdp: a password is required")

// ErrTLSFailed is returned by Open when the TLS handshake failed. It is the
// library's error, kept here so a caller can tell a handshake a legacy server
// may accept from other failures without importing the client package.
var ErrTLSFailed = rdpcore.ErrTLSFailed

// Defaults of the picture a session asks for when the caller gives no size:
// the client chooses the size when it connects. The workbench gives the size of
// its pane and follows it through Resize; this is only for a caller that does
// not.
const (
	DefaultWidth  = 1600
	DefaultHeight = 900
	// minSize is the smallest side the server's display channel accepts.
	minSize = 200
	// maxSize bounds what a caller may ask for, and what a server may hand
	// back: the largest desktop the frame buffer is built for.
	maxSize = rdpcore.MaxDesktopSize
	// handshakeTimeout bounds everything up to the first picture: dialing, the
	// security negotiation, authentication and the capability exchange.
	handshakeTimeout = 45 * time.Second
	// maxTyped bounds a SendText, which types a character at a time.
	maxTyped = 16 << 10
)

// Session is a desktop.Session on an RDP connection.
type Session struct {
	client *rdpcore.Client
	// closeTransport closes the connection the library last dialed: when the
	// library ends a connection on its own account it leaves the socket open.
	closeTransport func()
	opts           desktop.Options
	info           desktop.Info

	done chan struct{}

	mu          sync.Mutex
	err         error
	closed      bool
	remote      string
	hasText     bool
	remoteFiles desktop.ReceivedFiles
	hasFiles    bool
	// remoteFileList is the server's file list when the caller reads it on
	// demand instead of downloading it, and hasFileList says one arrived.
	remoteFileList desktop.ClipboardFileList
	hasFileList    bool
	// receiving is the download of the server's files under way, when
	// isReceiving; receivingStep is how far it had come when the window was
	// last woken for it.
	receiving     desktop.FileProgress
	isReceiving   bool
	receivingStep int64
	// pointerKind is what the server last said about its pointer,
	// pointerShape its pixels when the kind is PointerBitmap, and
	// pointerVersion how many statements it has made. Guarded by mu.
	pointerKind    desktop.PointerKind
	pointerShape   desktop.PointerShape
	pointerVersion int
	// buttons are the pointer buttons the server believes are down, so a change
	// is sent as the press or release it is; shift says whether Shift is held, so
	// a character that needs it on a US keyboard can add it by itself.
	buttons desktop.Buttons
	shift   bool
	// buffer is the painting goroutine's scratch for the rows of a rectangle
	// read out of the library's framebuffer.
	buffer []byte
	// picture is the size the server last made the desktop and the rectangles
	// changed since the last Present. No pixels are kept here: Present reads
	// them from the library's framebuffer, which is the session's one copy of
	// the picture. Guarded by pictureMu.
	pictureMu     sync.Mutex
	width, height int
	sized         bool
	dirty         []desktop.Rect
	// presentRects is the painting goroutine's copy of dirty for the Present in
	// progress; it may keep its capacity between calls.
	presentRects []desktop.Rect
}

var _ desktop.Session = (*Session)(nil)
var _ desktop.FileReceiver = (*Session)(nil)
var _ desktop.FileLister = (*Session)(nil)

func (session *Session) NetworkRTT() (time.Duration, time.Time) {
	return session.client.NetworkRTT()
}

// applyQuality sets the picture quality a host asked for: the session's color
// depth and the visual effects the server may render.
func applyQuality(config *rdpcore.Options, opts desktop.Options) {
	config.GFX = true
	switch opts.ColorDepth {
	case 16, 24:
		config.Depth = uint16(opts.ColorDepth)
		// The graphics pipeline draws in 32 bits per pixel whatever the session
		// says, so a lower depth is only honoured by the plain bitmap updates the
		// server falls back to without it, as Windows' own client does.
		config.GFX = false
	}
	effects := desktop.DefaultEffects()
	if opts.Effects != nil {
		effects = *opts.Effects
	}
	config.Wallpaper = effects.Wallpaper
	config.FontSmoothing = effects.FontSmoothing
	config.DesktopComposition = effects.Composition
	config.FullWindowDrag = effects.WindowDrag
	config.MenuAnimations = effects.Animations
	config.Theming = effects.Themes
	config.CursorShadow = effects.CursorShadow
}

// Open connects to address through dial and starts reading the server. It
// returns ErrAuthFailed when the server refused the account, and
// ErrAuthRequired when there is no password to try.
func Open(ctx context.Context, dial desktop.Dialer, address string, opts desktop.Options) (*Session, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("rdp: address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("rdp: address %q: bad port", address)
	}
	if opts.Password == "" && opts.AskPassword == nil {
		return nil, ErrAuthRequired
	}
	width, height := opts.Width, opts.Height
	if width <= 0 || height <= 0 {
		width, height = DefaultWidth, DefaultHeight
	}
	width, height = clampSize(width, height)

	session := &Session{opts: opts, done: make(chan struct{}), width: width, height: height, sized: true}
	session.dirty = append(session.dirty, desktop.Rect{X: 0, Y: 0, Width: width, Height: height})

	transport := newSessionTransport(dial, address)
	config := rdpcore.DefaultOptions()
	config.Host, config.Port = host, port
	config.Username, config.Domain, config.Password = opts.Username, opts.Domain, opts.Password
	config.Width, config.Height = uint16(width), uint16(height)
	config.DesktopScaleFactor = scalePercent(opts.Scale)
	config.Clipboard = opts.Clipboard
	config.ConsoleSession = opts.Console
	if opts.StandardSecurity {
		config.Security = rdpcore.SecurityStandard
	}
	config.LegacyTLS = opts.LegacyTLS
	applyQuality(config, opts)
	config.NoAVC = true // no H.264 decoder: the server then sends RemoteFX or ClearCodec
	if opts.Audio {
		// The server picks one of the PCM formats the player accepts; opening
		// happens then, not at connect, so a session that never plays sound
		// never touches the output device.
		config.Sound = func(sampleRate uint32, channels uint16) (io.WriteCloser, error) {
			return audio.Open(audio.Format{SampleRate: int(sampleRate), Channels: int(channels)})
		}
	}
	config.Dial = transport.dialConn
	// The user is asked while the handshake waits, so its timeout is suspended
	// for the question. The certificate comes first and the password after it,
	// as with an SSH host key: nothing secret is sent to a server that was not
	// accepted, and the library reads the password only when it authenticates.
	asking := func(ask func() error) error {
		resume := transport.suspend()
		err := ask()
		resume()
		return err
	}
	config.VerifyCertificate = func(der []byte) error {
		if opts.VerifyCertificate != nil {
			if err := asking(func() error { return opts.VerifyCertificate(address, der) }); err != nil {
				return err
			}
		}
		if config.Password != "" {
			return nil
		}
		return asking(func() error {
			password, err := opts.AskPassword()
			config.Password = password
			return err
		})
	}
	// Standard RDP security sends the account in the first client information
	// PDU, with no certificate to check first and therefore no handshake step
	// to ask at. Ask before connecting; the switch has already said that this
	// layer does not authenticate the server.
	if opts.StandardSecurity && config.Password == "" {
		if err := asking(func() error {
			password, err := opts.AskPassword()
			config.Password = password
			return err
		}); err != nil {
			return nil, err
		}
	}

	client, err := rdpcore.NewClient(config)
	if err != nil {
		return nil, err
	}
	session.client = client
	session.closeTransport = transport.close
	client.OnBitmap(session.onBitmap)
	client.OnStridedBitmap(session.onStrided)
	client.OnResize(func(width, height int) {
		session.syncSize()
		_ = client.RefreshRect(0, 0, width, height)
	})
	client.OnPointer(session.onPointer)
	client.OnNetworkRTTChanged(session.wake)
	client.OnDisconnect(func(err error) { session.finish(err) })
	if opts.Clipboard {
		client.OnClipboardUpdate(func(hasText, _, hasFiles bool) {
			switch {
			case hasFiles && opts.DeferClipboardFiles:
				_ = client.RequestClipboardFileList()
			case hasFiles:
				_ = client.RequestClipboardFiles()
			case hasText:
				_ = client.RequestClipboard()
			}
		})
		client.OnClipboardText(func(text string) {
			session.mu.Lock()
			session.remote, session.hasText = text, true
			session.mu.Unlock()
			session.wake()
		})
		client.OnClipboardFiles(func(files rdpcore.ClipboardFiles, err error) {
			session.mu.Lock()
			session.remoteFiles = desktop.ReceivedFiles{
				Paths: files.Paths, Files: files.Files, Folders: files.Folders,
				Bytes: files.Bytes, Skipped: files.Skipped, Err: err,
			}
			session.hasFiles = true
			session.mu.Unlock()
			session.wake()
		})
		client.OnClipboardFilesProgress(session.onFilesProgress)
		client.OnClipboardFileList(session.onFileList)
	}

	// A reconnection, and a server redirection (GNOME Remote Desktop's remote
	// login hands the session to the user session's daemon), connects again
	// without Open: the window ends there too, the library's heartbeat
	// watches the open connection.
	client.OnReconnected(transport.connected)

	// Cancelling the context must end a handshake that is waiting on the network.
	stop := context.AfterFunc(ctx, func() { client.Close() })
	err = client.Connect()
	cancelled := !stop()
	if err == nil {
		transport.connected()
	}
	if err == nil && cancelled {
		err = ctx.Err()
	}
	if err != nil {
		client.Close()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, translate(err)
	}
	// Standard RDP Security is requested explicitly, so a connected session
	// that asked for it has only RC4 under an unauthenticated server key.
	session.info = desktop.Info{Protocol: "RDP", Name: host, Encrypted: !opts.StandardSecurity}
	return session, nil
}

// sessionTransport opens the connections of a session and bounds each of
// its handshakes. The first shares one window with the password prompt. A
// connection the library makes later on its own (a reconnection, a server
// redirection) gets a window of its own, by which time the first one would
// be running out. Once a session is up its deadline is lifted: from then on
// the library's heartbeat watches the link.
type sessionTransport struct {
	dial    desktop.Dialer
	address string

	mu       sync.Mutex
	current  net.Conn
	deadline time.Time // when the handshake in flight must be done; zero once the window ended
	opened   bool      // the first handshake is over
}

func newSessionTransport(dial desktop.Dialer, address string) *sessionTransport {
	return &sessionTransport{dial: dial, address: address, deadline: time.Now().Add(handshakeTimeout)}
}

func (t *sessionTransport) dialConn(ctx context.Context, network, _ string) (net.Conn, error) {
	// The address the library asks for is the host's own name; the caller says
	// where the bytes really go (the SSH server's loopback, a proxy).
	conn, err := t.dial(ctx, network, t.address)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	deadline := t.deadline
	if t.opened {
		deadline = time.Now().Add(handshakeTimeout)
	}
	conn.SetDeadline(deadline)
	t.current, t.deadline = conn, deadline
	return conn, nil
}

// suspend lifts the window while the user answers a question; the returned
// function restarts the window from then.
func (t *sessionTransport) suspend() (resume func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		t.current.SetDeadline(time.Time{})
	}
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.deadline = time.Now().Add(handshakeTimeout)
		if t.current != nil {
			t.current.SetDeadline(t.deadline)
		}
	}
}

// connected ends the window of the handshake in flight.
func (t *sessionTransport) connected() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.opened = true
	if t.current != nil {
		t.current.SetDeadline(time.Time{})
	}
}

// close shuts the connection the library dialed last: when the library ends
// a connection on its own account it leaves the socket open.
func (t *sessionTransport) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current != nil {
		t.current.Close()
	}
}

// clampSize holds a requested desktop to what the server's display channel
// accepts: an even width, and sides between minSize and maxSize.
func clampSize(width, height int) (int, int) {
	return min(max(width, minSize), maxSize) &^ 1, min(max(height, minSize), maxSize)
}

// ValidSize reports whether a width x height desktop is one a caller may ask
// for: each side within what the display channel accepts. Session rounds an
// odd width down.
func ValidSize(width, height int) bool {
	return width >= minSize && width <= maxSize && height >= minSize && height <= maxSize
}

// scaleLevels are the interface scales Windows offers, in percent.
var scaleLevels = [...]uint32{100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, 500}

// scalePercent is the Windows scale level nearest to scale (a device pixel per
// logical pixel), 100 for zero or a scale below it.
func scalePercent(scale float32) uint32 {
	wanted := float64(scale) * 100
	nearest := scaleLevels[0]
	for _, level := range scaleLevels[1:] {
		if math.Abs(float64(level)-wanted) < math.Abs(float64(nearest)-wanted) {
			nearest = level
		}
	}
	return nearest
}

var _ desktop.Resizer = (*Session)(nil)

// CanResize reports whether the server has opened its display control channel.
func (session *Session) CanResize() bool { return session.client.CanResize() }

// Resize asks the server for a width x height desktop drawn at scale, keeping
// the session. The frame follows when the server has made the new desktop.
func (session *Session) Resize(width, height int, scale float32) error {
	width, height = clampSize(width, height)
	return session.client.ResizeDisplay(width, height, scalePercent(scale))
}

// translate turns the library's authentication failure into this package's.
func translate(err error) error {
	if errors.Is(err, rdpcore.ErrAuthFailed) {
		return fmt.Errorf("%w: %v", ErrAuthFailed, err)
	}
	return err
}

func (session *Session) wake() {
	if session.opts.Notify != nil {
		session.opts.Notify()
	}
}

// finish records why the connection ended, once, and closes Done.
func (session *Session) finish(err error) {
	session.mu.Lock()
	select {
	case <-session.done:
		session.mu.Unlock()
		return
	default:
	}
	if session.closed {
		err = nil
	}
	session.err = err
	session.pointerKind = desktop.PointerSystem
	session.pointerShape = desktop.PointerShape{}
	session.pointerVersion++
	close(session.done)
	session.mu.Unlock()
	session.closeTransport()
	session.wake()
}

// maxDirty is how many changed rectangles are kept before they collapse into
// their bounding box, as desktop.Frame does.
const maxDirty = 16

// rectBandRows is how many rows Present reads out of the framebuffer at a time.
// It is the size of the scratch buffer, not of the picture.
const rectBandRows = 64

// syncSize follows the library's screen size: a resize marks the whole picture
// changed, because the remote repaints it.
func (session *Session) syncSize() {
	w, h := session.client.FramebufferDims()
	if w <= 0 || h <= 0 {
		return
	}
	session.pictureMu.Lock()
	if session.width == w && session.height == h {
		session.pictureMu.Unlock()
		return
	}
	session.width, session.height = w, h
	session.sized = true
	session.dirty = append(session.dirty[:0], desktop.Rect{X: 0, Y: 0, Width: w, Height: h})
	session.pictureMu.Unlock()
	session.wake()
}

// markDirty records a changed rectangle. Beyond maxDirty of them they collapse
// into their bounding box, so a flood of small updates costs one larger read.
func (session *Session) markDirty(x, y, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	session.pictureMu.Lock()
	if len(session.dirty) >= maxDirty {
		union := desktop.Rect{X: x, Y: y, Width: width, Height: height}
		for _, other := range session.dirty {
			union = union.Union(other)
		}
		session.dirty = append(session.dirty[:0], union)
	} else {
		session.dirty = append(session.dirty, desktop.Rect{X: x, Y: y, Width: width, Height: height})
	}
	session.pictureMu.Unlock()
	session.wake()
}

// onBitmap is called on the receive goroutine after the library drew into its
// framebuffer: only the changed rectangle is recorded, and Present reads it.
func (session *Session) onBitmap(update *rdpcore.BitmapUpdate) {
	session.syncSize()
	session.markDirty(update.X, update.Y, update.Width, update.Height)
}

// onStrided is a picture update of the graphics pipeline. The library has
// already drawn it into its framebuffer (WriteRectStridedTopDown), so the data
// handed here is not converted again.
func (session *Session) onStrided(x, y, width, height int, data []byte, stride int) {
	session.syncSize()
	session.markDirty(x, y, width, height)
}

func (session *Session) Info() desktop.Info { return session.info }
func (session *Session) Size() (width, height int) {
	session.pictureMu.Lock()
	defer session.pictureMu.Unlock()
	return session.width, session.height
}

// Present brings dst up to date from the library's framebuffer: the changed
// rectangles are read once, in rows, and no second full-screen copy is kept.
func (session *Session) Present(dst desktop.Presenter) bool {
	session.pictureMu.Lock()
	if session.width <= 0 || session.height <= 0 || (!session.sized && len(session.dirty) == 0) {
		session.pictureMu.Unlock()
		return false
	}
	width, height, sized := session.width, session.height, session.sized
	session.presentRects = append(session.presentRects[:0], session.dirty...)
	session.dirty = session.dirty[:0]
	session.sized = false
	session.pictureMu.Unlock()

	if sized {
		dst.Resize(width, height)
	}
	for _, rect := range session.presentRects {
		need := rect.Width * rectBandRows * 4
		if cap(session.buffer) < need {
			session.buffer = make([]byte, need)
		}
		session.client.ReadRectBGRA(session.buffer[:need], rect.X, rect.Y, rect.Width, rect.Height, rectBandRows, dst.Update)
	}
	return true
}
func (session *Session) Done() <-chan struct{} { return session.done }

func (session *Session) Err() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

func (session *Session) Close() error {
	session.mu.Lock()
	session.closed = true
	session.pointerKind = desktop.PointerSystem
	session.pointerShape = desktop.PointerShape{}
	session.pointerVersion++
	session.mu.Unlock()
	err := session.client.Close()
	// The library reports an ending it did not cause through OnDisconnect;
	// closing is the caller's own ending and has no report.
	session.finish(nil)
	return err
}

// SetActive suppresses the server's picture while nobody sees it, and asks for a
// full repaint when someone does.
func (session *Session) SetActive(active bool) {
	_ = session.client.SetOutputSuppressed(!active)
}

func (session *Session) RemoteClipboard() (string, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	text, ok := session.remote, session.hasText
	session.remote, session.hasText = "", false
	return text, ok
}

// RemoteFiles returns the files the server's clipboard delivered since the
// previous call (see desktop.FileReceiver).
func (session *Session) RemoteFiles() (desktop.ReceivedFiles, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	files, ok := session.remoteFiles, session.hasFiles
	session.remoteFiles, session.hasFiles = desktop.ReceivedFiles{}, false
	return files, ok
}

// onFileList records the server's file list read on demand, or why none
// arrived, for the workbench to promise it to the local clipboard.
func (session *Session) onFileList(files []rdpcore.ClipboardFile, version uint64, err error) {
	list := desktop.ClipboardFileList{Version: version, Err: err}
	for _, file := range files {
		size := int64(-1)
		if file.HasSize {
			size = int64(min(file.Size, math.MaxInt64))
		}
		list.Files = append(list.Files, desktop.FileListing{Name: file.Name, Size: size, Dir: file.Dir})
	}
	session.mu.Lock()
	session.remoteFileList, session.hasFileList = list, true
	session.mu.Unlock()
	session.wake()
}

// RemoteFileList returns the server's file list delivered since the previous
// call (see desktop.FileLister).
func (session *Session) RemoteFileList() (desktop.ClipboardFileList, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	list, ok := session.remoteFileList, session.hasFileList
	session.remoteFileList, session.hasFileList = desktop.ClipboardFileList{}, false
	return list, ok
}

// ReadRemoteFile reads part of a file of that list from the server (see
// desktop.FileLister).
func (session *Session) ReadRemoteFile(version uint64, index int, offset int64, length int) ([]byte, error) {
	return session.client.ReadClipboardFile(version, index, offset, length)
}

// ReleaseRemoteFileList lets the server's clipboard data go once the local
// clipboard no longer holds the promises made from it.
func (session *Session) ReleaseRemoteFileList(version uint64) {
	session.client.ReleaseClipboardFileList(version)
}

// DownloadRemoteFileList reads the list delivered on demand into a staging
// directory instead, for a caller that cannot promise it.
func (session *Session) DownloadRemoteFileList(version uint64) error {
	return session.client.DownloadClipboardFiles(version)
}

// ReceivingFiles reports the server's files being saved now (see
// desktop.FileReceiver).
func (session *Session) ReceivingFiles() (desktop.FileProgress, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.receiving, session.isReceiving
}

// CancelReceivingFiles stops saving the server's files.
func (session *Session) CancelReceivingFiles() { session.client.CancelClipboardFiles() }

// onFilesProgress records how far the server's files have been read. It runs
// on the download's goroutine once per chunk, and wakes the window only when
// what the progress card draws moves: a new file, or another hundredth of the
// whole (another MiB when the whole is not known).
func (session *Session) onFilesProgress(progress rdpcore.ClipboardFilesProgress) {
	next := desktop.FileProgress{Name: strings.ReplaceAll(progress.Name, `\`, "/"), Done: int64(min(progress.Done, math.MaxInt64)), Size: -1}
	step := next.Done >> 20
	if progress.HasTotal {
		next.Size = int64(min(progress.Total, math.MaxInt64))
		step = 0
		if next.Size > 0 {
			step = int64(float64(next.Done) / float64(next.Size) * 100)
		}
	}
	session.mu.Lock()
	changed := progress.Active != session.isReceiving || next.Name != session.receiving.Name || step != session.receivingStep
	session.receiving, session.isReceiving, session.receivingStep = next, progress.Active, step
	if !progress.Active {
		session.receiving = desktop.FileProgress{}
	}
	session.mu.Unlock()
	if changed {
		session.wake()
	}
}

// Bells: RDP has no bell the client hears.
func (session *Session) Bells() int { return 0 }

func (session *Session) SetClipboard(text string) error {
	if !session.opts.Clipboard {
		return nil
	}
	return session.client.SetClipboard(text)
}

// SetClipboardImage offers a picture from this computer's clipboard as what the
// remote clipboard holds, replacing its text (see desktop.ImageClipboard).
func (session *Session) SetClipboardImage(png []byte) error {
	if !session.opts.Clipboard {
		return nil
	}
	return session.client.SetClipboardImage(png)
}

// SetClipboardFiles offers files of this computer to the server's clipboard, as a
// clipboard file transfer (see desktop.FileClipboard).
func (session *Session) SetClipboardFiles(paths []string) (desktop.FileOffer, error) {
	if !session.opts.Clipboard {
		return desktop.FileOffer{}, desktop.ErrClipboardOff
	}
	offer, err := session.client.SetClipboardFiles(paths)
	result := desktop.FileOffer{Files: offer.Files, Folders: offer.Folders, Bytes: offer.Bytes, Skipped: offer.Skipped, FirstSkipped: offer.FirstSkipped}
	switch {
	case err == nil:
		return result, nil
	case errors.Is(err, rdpcore.ErrClipboardFilesUnsupported):
		return result, desktop.ErrFilesUnsupported
	case errors.Is(err, rdpcore.ErrClipboardOff):
		return result, desktop.ErrClipboardOff
	case errors.Is(err, rdpcore.ErrClipboardNotReady):
		return result, desktop.ErrClipboardNotReady
	case errors.Is(err, rdpcore.ErrNothingToOffer):
		return result, desktop.ErrNothingToOffer
	case errors.Is(err, rdpcore.ErrTooManyFiles):
		return result, desktop.ErrTooManyFiles
	case errors.Is(err, rdpcore.ErrFileNameTooLong):
		return result, desktop.ErrPathTooLong
	}
	return result, err
}

// onPointer records what the server last said about its pointer. It runs on
// the connection's read goroutine.
func (session *Session) onPointer(update *rdpcore.PointerUpdate) {
	var kind desktop.PointerKind
	var shape desktop.PointerShape
	switch update.Type {
	case rdpcore.PointerNull:
		kind = desktop.PointerHidden
	case rdpcore.PointerDefault:
		kind = desktop.PointerSystem
	case rdpcore.PointerShape:
		if cached := session.shapeFrom(update); cached.Width > 0 {
			kind = desktop.PointerBitmap
			shape = cached
		} else {
			kind = desktop.PointerHidden // a shape that failed to decode hides
		}
	case rdpcore.PointerCached:
		// The client hands a cached shape on as a PointerShape; a cached index it has
		// no shape for names nothing, and the pointer stays as it was.
		return
	default:
		return
	}
	session.mu.Lock()
	session.pointerKind, session.pointerShape = kind, shape
	session.pointerVersion++
	session.mu.Unlock()
	session.wake()
}

// shapeFrom copies an update into a desktop shape, RGBA to BGRA as the
// presenter takes it, keeping the pixels the remote's pointer inverts.
func (session *Session) shapeFrom(update *rdpcore.PointerUpdate) desktop.PointerShape {
	shape := desktop.PointerShape{
		Width:  int(update.Width),
		Height: int(update.Height),
		HotX:   int(update.HotSpotX),
		HotY:   int(update.HotSpotY),
		BGRA:   make([]byte, len(update.Data)),
	}
	for index := 0; index+3 < len(update.Data); index += 4 {
		shape.BGRA[index] = update.Data[index+2]
		shape.BGRA[index+1] = update.Data[index+1]
		shape.BGRA[index+2] = update.Data[index]
		shape.BGRA[index+3] = update.Data[index+3]
	}
	if len(update.Invert) > 0 {
		shape.Invert = make([]bool, len(update.Invert))
		copy(shape.Invert, update.Invert)
	}
	return shape
}

// PointerShape implements desktop.PointerSource.
func (session *Session) PointerShape() (desktop.PointerKind, desktop.PointerShape, int) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.pointerKind, session.pointerShape, session.pointerVersion
}

// RDP pointer event flags.
const (
	ptrMove   = 0x0800
	ptrDown   = 0x8000
	ptrLeft   = 0x1000
	ptrRight  = 0x2000
	ptrMiddle = 0x4000
)

var buttonFlags = [...]struct {
	button desktop.Buttons
	flag   uint16
}{{desktop.ButtonLeft, ptrLeft}, {desktop.ButtonRight, ptrRight}, {desktop.ButtonMiddle, ptrMiddle}}

// SendPointer sends a move, and a press or release for each button whose state
// changed since the last event.
func (session *Session) SendPointer(x, y int, buttons desktop.Buttons) error {
	x, y = clampCoordinate(x), clampCoordinate(y)
	session.mu.Lock()
	changed := session.buttons ^ buttons
	session.buttons = buttons
	session.mu.Unlock()
	if changed == 0 {
		return session.client.SendMouse(x, y, ptrMove)
	}
	for _, entry := range buttonFlags {
		if changed&entry.button == 0 {
			continue
		}
		flags := entry.flag
		if buttons&entry.button != 0 {
			flags |= ptrDown
		}
		if err := session.client.SendMouse(x, y, flags); err != nil {
			return err
		}
	}
	return nil
}

func clampCoordinate(value int) int { return max(0, min(value, 0xFFFF)) }

// maxNotches bounds what one wheel gesture sends.
const maxNotches = 10

func (session *Session) SendWheel(x, y int, wheel desktop.Wheel, _ desktop.Buttons) error {
	x, y = clampCoordinate(x), clampCoordinate(y)
	if wheel.Y != 0 {
		// A positive RDP rotation is away from the user, up; Wheel.Y is down.
		if err := session.client.SendMouseWheel(x, y, -max(-maxNotches, min(wheel.Y, maxNotches)), false); err != nil {
			return err
		}
	}
	if wheel.X != 0 {
		return session.client.SendMouseWheel(x, y, max(-maxNotches, min(wheel.X, maxNotches)), true)
	}
	return nil
}

// SendKey sends a key as a scancode. A character that has no key on a US
// keyboard is sent as a Unicode event instead. The server does not repeat a key
// it sees held, so a repeat of a key that is still down is sent again as such.
func (session *Session) SendKey(event desktop.KeyEvent) error {
	if event.Rune == 0 {
		code := keyCode(event.Key)
		if code == 0 {
			return nil
		}
		if event.Key == desktop.KeyShift {
			session.mu.Lock()
			session.shift = event.Down
			session.mu.Unlock()
		}
		return session.client.SendKeyboard(code, event.Down, event.Repeat)
	}
	if code, ok := runeCode(event.Rune); ok {
		session.mu.Lock()
		shift := session.shift
		session.mu.Unlock()
		// "!" arrives with Shift held when typed on a US keyboard; typed any other
		// way (a layout where it is unshifted) Shift is not, and is added.
		addShift := code.shift && !shift
		if addShift && event.Down {
			if err := session.client.SendKeyboard(0x2A, true, event.Repeat); err != nil {
				return err
			}
		}
		err := session.client.SendKeyboard(code.code, event.Down, event.Repeat)
		if addShift && !event.Down {
			if releaseErr := session.client.SendKeyboard(0x2A, false, false); err == nil {
				err = releaseErr
			}
		}
		return err
	}
	return session.typeRune(event.Rune, event.Down, event.Repeat)
}

// typeRune sends one character as Unicode events (a surrogate pair above the
// Basic Multilingual Plane), a press or a release.
func (session *Session) typeRune(r rune, down, repeat bool) error {
	if r < 0x20 || r == 0x7f || r > 0x10ffff {
		return nil
	}
	high, low := utf16.EncodeRune(r)
	if high == 0xFFFD && low == 0xFFFD { // in the BMP
		return session.client.SendUnicode(uint16(r), down, repeat)
	}
	if err := session.client.SendUnicode(uint16(high), down, repeat); err != nil {
		return err
	}
	return session.client.SendUnicode(uint16(low), down, repeat)
}

func (session *Session) SendText(text string) error {
	typed := 0
	for _, r := range text {
		if typed++; typed > maxTyped {
			return errors.New("rdp: text too long to type")
		}
		if r == '\n' {
			if err := session.client.SendKeyboard(keyCode(desktop.KeyEnter), true, false); err != nil {
				return err
			}
			if err := session.client.SendKeyboard(keyCode(desktop.KeyEnter), false, false); err != nil {
				return err
			}
			continue
		}
		if err := session.typeRune(r, true, false); err != nil {
			return err
		}
		if err := session.typeRune(r, false, false); err != nil {
			return err
		}
	}
	return nil
}
