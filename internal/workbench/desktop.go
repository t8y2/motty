package workbench

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
	"motty/internal/desktop"
	"motty/internal/memlimit"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// The tab of a remote desktop (VNC today; see docs/remote-desktop.md): the
// picture the server draws, scaled into the pane, and the pointer, keys, text
// and clipboard that go back. Everything here works on desktop.Session, so a
// second protocol needs no change in this file.

var desktopHits = tree.Feature("desktop")

var (
	hitDesktopFit        = tree.ID(desktopHits, "fit")
	hitDesktopActual     = tree.ID(desktopHits, "actual")
	hitDesktopCapture    = tree.ID(desktopHits, "capture")
	hitDesktopCtrlAltDel = tree.ID(desktopHits, "ctrl-alt-del")
	hitDesktopReconnect  = tree.ID(desktopHits, "reconnect")
	hitDesktopFullscreen = tree.ID(desktopHits, "fullscreen")
	hitDesktopBarPin     = tree.ID(desktopHits, "bar-pin")
	hitDesktopBarDock    = tree.ID(desktopHits, "bar-dock")
	hitDesktopBarDrag    = tree.ID(desktopHits, "bar-drag")
	hitDesktopLatency    = tree.ID(desktopHits, "latency")
	hitDesktopQuality    = tree.ID(desktopHits, "quality")
	hitDesktopResolution = tree.ID(desktopHits, "resolution")
)

const (
	desktopStripHeight = float32(24)
	// desktopEscapeChord releases the keyboard from the remote desktop, and takes
	// it again. It is a fixed chord, because while the keyboard is captured no
	// configurable shortcut of Motty can run to change it.
	desktopEscapeChord = "Ctrl+Alt+G"
	// desktopWheelStep is how far a scroll must travel, in logical pixels, to
	// count as one notch of a wheel.
	desktopWheelStep = float32(40)
	desktopMaxScale  = float32(4)
	// desktopResizeDelay is how long the pane must keep its size before a server
	// that follows it is asked for a desktop of that size: a window being dragged
	// would otherwise ask for a new desktop at every step.
	desktopResizeDelay = 250 * time.Millisecond
)

type desktopView struct {
	session   desktop.Session
	surface   gfx.Surface
	clipboard bool
	// fit scales the picture to the pane; off shows it at its own size, panning
	// with the pointer when it is larger than the pane.
	fit bool
	// released hands the keyboard back to Motty (its shortcuts work, nothing is
	// sent) until the escape chord takes it again.
	released bool
	ended    bool
	active   bool
	pointer  gfx.Point // the pointer in page coordinates, last seen over the picture
	buttons  desktop.Buttons
	// held is the keys pressed on the remote side and not yet released, so a
	// release is sent only for a press that was.
	held  []heldKey
	wheel gfx.Point
	// sentClip is the text this side last gave the remote clipboard and sentImage
	// the picture: an unchanged clipboard is not offered again, and whichever
	// format is offered replaces the other one on the remote side.
	sentClip  string
	sentImage imageDigest
	// files is the transfer of files from this computer to the remote clipboard.
	files desktopFiles
	// chromeless is the host's choice to show only the picture: no bar above it
	// and no status line under it. Their controls move to the tab's menu, and
	// the capture note to a corner of the picture.
	chromeless   bool
	fullscreen   bool
	focusPending bool
	// width and height are the picture's size as last presented.
	width, height int
	// resizer is the session when its server can change the picture's size to
	// follow the pane, nil for one that cannot (VNC).
	resizer desktop.Resizer
	// pointerKind, pointerShape and pointerVersion are what the remote last
	// said about its pointer; cursorImage is the shape made for the system to
	// draw as the pointer, at cursorScale. A session that names no shape
	// leaves all of it empty.
	pointerKind    desktop.PointerKind
	pointerShape   desktop.PointerShape
	pointerVersion int
	cursorImage    *gfx.CursorImage
	cursorScale    float32
	// inverting is whether the remote's pointer shows the picture through its
	// body (see desktop.PointerShape.Invert). No system can draw such a shape
	// as a cursor, so over the picture the workbench paints it itself from
	// pointerPixels: two alternating buffers so the frame last painted keeps an
	// immutable Pix slice while the next one is built, paired with a stable
	// bitmap ID and incrementing version so native renderers re-upload when the
	// inverted pixels change.
	inverting            bool
	pointerInside        bool // whether the pointer is over the picture, not the bars
	pointerPixels        [2][]byte
	pointerPixelSlot     int
	pointerBitmapID      uint64
	pointerBitmapVersion uint32
	pointerBitmapWidth   int
	pointerBitmapHeight  int
	// pixelScale is the device pixels per logical pixel as last painted, and area
	// where the picture was shown, in the page's coordinates.
	pixelScale float32
	area       gfx.Rect
	// asked is the size and scale the server was last given, or had when the
	// picture first came; pending is a different one the pane has taken, waiting
	// since pendingSince to be sure it stays (see desktopResizeDelay).
	asked, pending desktopTarget
	pendingSince   time.Duration
	settling       bool
	// qualityMenu is whether the quality menu of the bar is open.
	qualityMenu bool
	// resolutionMenu is whether the resolution menu of an RDP bar is open.
	resolutionMenu bool
	// dockMenu is whether the fullscreen bar's dock menu is open.
	dockMenu bool
}

// imageDigest identifies the picture a desktop's clipboard was last given, so the
// same one is not offered by every click; the zero value says none was.
type imageDigest [sha256.Size]byte

// desktopTarget is the desktop a pane wants of a server that follows it: as
// many pixels as the pane has, and the scale of the display they are on.
type desktopTarget struct {
	width, height int
	scale         float32
}

// desktopTargetFor is the desktop that fills area exactly at a display scale.
// The width is even, as a server's display channel needs it.
func desktopTargetFor(area gfx.Rect, scale float32) desktopTarget {
	if scale <= 0 {
		scale = 1
	}
	return desktopTarget{
		width:  int(math.Floor(float64(area.Width)*float64(scale)+1e-3)) &^ 1,
		height: int(math.Floor(float64(area.Height)*float64(scale) + 1e-3)),
		scale:  scale,
	}
}

func newDesktopView(session desktop.Session, clipboard bool) *desktopView {
	view := &desktopView{session: session, clipboard: clipboard, fit: true, active: true}
	view.resizer, _ = session.(desktop.Resizer)
	view.files.remote, _ = session.(desktop.FileClipboard)
	trackDesktopPicture(view)
	return view
}

// desktopPictures tracks the remote desktops open in this process so the heap
// soft limit follows the pictures shown. Every window of one process shares the
// same heap, so the registry is shared too, not kept per window.
var desktopPictures struct {
	sync.Mutex
	views map[*desktopView]memlimit.Size
}

func trackDesktopPicture(view *desktopView) {
	desktopPictures.Lock()
	if desktopPictures.views == nil {
		desktopPictures.views = make(map[*desktopView]memlimit.Size)
	}
	desktopPictures.views[view] = memlimit.Size{}
	desktopPictures.Unlock()
	applyDesktopMemoryLimit()
}

func untrackDesktopPicture(view *desktopView) {
	desktopPictures.Lock()
	delete(desktopPictures.views, view)
	desktopPictures.Unlock()
	applyDesktopMemoryLimit()
}

// noteDesktopSize records a picture's size and moves the limit when it changed.
// It does nothing on the frames where the size stayed the same.
func noteDesktopSize(view *desktopView, width, height int) {
	desktopPictures.Lock()
	old, tracked := desktopPictures.views[view]
	size := memlimit.Size{Width: width, Height: height}
	if !tracked || old == size {
		desktopPictures.Unlock()
		return
	}
	desktopPictures.views[view] = size
	desktopPictures.Unlock()
	applyDesktopMemoryLimit()
}

// applyDesktopMemoryLimit gives the runtime the soft limit for every picture
// shown, or removes the limit when no remote desktop is open.
func applyDesktopMemoryLimit() {
	desktopPictures.Lock()
	sizes := make([]memlimit.Size, 0, len(desktopPictures.views))
	for _, size := range desktopPictures.views {
		sizes = append(sizes, size)
	}
	desktopPictures.Unlock()
	memlimit.Apply(memlimit.ForDesktops(sizes))
}

// pixelPerfect reports whether the picture is shown pixel for pixel: the server
// can change its size, so the picture is made the size of the pane and scaling
// it would only blur it.
func (view *desktopView) pixelPerfect() bool {
	return view.resizer != nil && view.resizer.CanResize()
}

// logicalPerPixel is the logical size of one picture pixel shown pixel for pixel.
func (view *desktopView) logicalPerPixel() float32 {
	if view.pixelScale <= 0 {
		return 1
	}
	return 1 / view.pixelScale
}

// oneToOne reports whether the picture is drawn one picture pixel to one device
// pixel: at 100% whatever the server's size is, and in Fit mode for the picture a
// resizing server makes the size of the pane. That is the size of the server's
// own screen; on a scaled display, drawing the picture on logical pixels instead
// magnified it and its edges fell outside the pane.
func (view *desktopView) oneToOne() bool {
	return !view.fit || view.pixelPerfect()
}

func (view *desktopView) close() {
	untrackDesktopPicture(view)
	if view.session != nil {
		view.session.Close()
	}
}

// present brings the picture up to date and reports whether it changed.
func (view *desktopView) present() bool {
	changed := view.session.Present(&view.surface)
	if changed {
		view.width, view.height = view.surface.Size()
		noteDesktopSize(view, view.width, view.height)
	}
	if !view.ended {
		view.presentPointer()
	}
	return changed
}

// presentPointer takes what the remote last said about its pointer, and
// forgets the cursor made from the shape before it when that changed.
func (view *desktopView) presentPointer() {
	source, ok := view.session.(desktop.PointerSource)
	if !ok {
		return
	}
	kind, shape, version := source.PointerShape()
	if version == view.pointerVersion {
		return
	}
	view.pointerVersion, view.pointerKind, view.pointerShape = version, kind, shape
	view.cursorImage = nil
	view.inverting = pointerShapeInverts(kind, shape)
}

// pointerShapeInverts reports whether the remote's shape has pixels that show
// the inverse of the picture behind them, the one thing a system cursor image
// cannot express.
func pointerShapeInverts(kind desktop.PointerKind, shape desktop.PointerShape) bool {
	if kind != desktop.PointerBitmap || shape.Width < 1 || shape.Height < 1 ||
		shape.Width > gfx.MaxCursorImageSize || shape.Height > gfx.MaxCursorImageSize ||
		len(shape.BGRA) != shape.Width*shape.Height*4 || len(shape.Invert) != shape.Width*shape.Height {
		return false
	}
	for _, inverts := range shape.Invert {
		if inverts {
			return true
		}
	}
	return false
}

// pointerCursor is the cursor over the remote's picture: the system draws the
// shape the remote names as its own pointer, hides it where the remote hides
// its own, and shows the ordinary one where the remote asks for it. The
// crosshair stays for a remote that has named nothing. An inverting shape is
// not one a system can draw, so over the picture the workbench paints it itself
// and hides the system's pointer (over: the picture is what the pointer is
// over); off the picture, where the workbench paints nothing, the ordinary
// pointer stays. scale is the device pixels per logical pixel the shape is
// made for: the remote's pixels are the screen's.
func (view *desktopView) pointerCursor(scale float32, over bool) (gfx.PointerCursor, *gfx.CursorImage) {
	if view.ended {
		return gfx.PointerCursorDefault, nil
	}
	switch view.pointerKind {
	case desktop.PointerHidden:
		return gfx.PointerCursorHidden, nil
	case desktop.PointerSystem:
		return gfx.PointerCursorDefault, nil
	case desktop.PointerBitmap:
		if view.inverting {
			if over {
				return gfx.PointerCursorHidden, nil
			}
			return gfx.PointerCursorDefault, nil
		}
		if view.cursorImage == nil || view.cursorScale != scale {
			shape := view.pointerShape
			hotX, hotY := min(max(shape.HotX, 0), shape.Width-1), min(max(shape.HotY, 0), shape.Height-1)
			image, err := gfx.NewCursorImage(shape.Width, shape.Height, hotX, hotY, scale, shape.BGRA)
			if err != nil {
				return gfx.PointerCursorDefault, nil // a shape no system could draw: the ordinary pointer
			}
			view.cursorImage, view.cursorScale = image, scale
		}
		return gfx.PointerCursorImage, view.cursorImage
	}
	return gfx.PointerCursorCrosshair, nil
}

// desktopPointerBitmapIDBase keeps painted-pointer bitmaps in their own
// identity space, below gfx.Surface's 1<<61 tile range and distinct from
// Motty's static icons and blurred thumbnails.
const desktopPointerBitmapIDBase = uint64(1<<60 | 1<<56)

var desktopPointerBitmapIDs atomic.Uint64

func copyPremultipliedBGRA(dst, src []byte) {
	a := src[3]
	switch a {
	case 0:
		dst[0], dst[1], dst[2], dst[3] = 0, 0, 0, 0
	case 255:
		copy(dst, src)
	default:
		alpha := uint32(a)
		dst[0] = byte((uint32(src[0])*alpha + 127) / 255)
		dst[1] = byte((uint32(src[1])*alpha + 127) / 255)
		dst[2] = byte((uint32(src[2])*alpha + 127) / 255)
		dst[3] = a
	}
}

// paintPointer paints the remote's pointer over the picture when its shape
// shows the picture through its body. An inverting pointer has no image a
// system could draw as a cursor, so the workbench paints it: the shape's own
// pixels, with each inverting pixel the inverse of the picture beneath it.
// over says whether the pointer is over the picture and no control it is not
// over holds the press, because the pointer a press belongs to stays with that
// control. origin and scale are where one remote pixel sits and how many page
// pixels it takes (see place).
func (view *desktopView) paintPointer(canvas *gfx.Canvas, origin gfx.Point, scale float32, over bool) {
	shape := view.pointerShape
	pictureWidth, pictureHeight := view.surface.Size()
	if !view.inverting || !view.pointerInside || view.ended || !over ||
		shape.Width < 1 || shape.Height < 1 || pictureWidth < 1 || pictureHeight < 1 {
		return
	}
	if scale <= 0 {
		scale = 1
	}
	// One pointer pixel is one logical pixel, as with the shape the system draws;
	// the picture under it is read one device pixel at a time, so the inverse
	// follows the picture at the resolution it is shown in.
	pixelScale := canvas.PixelScale()
	if pixelScale <= 0 {
		pixelScale = 1
	}
	hotX, hotY := min(max(shape.HotX, 0), shape.Width-1), min(max(shape.HotY, 0), shape.Height-1)
	left := int(math.Floor(float64((view.pointer.X-float32(hotX))*pixelScale) + 0.5))
	top := int(math.Floor(float64((view.pointer.Y-float32(hotY))*pixelScale) + 0.5))
	width := int(math.Floor(float64(shape.Width)*float64(pixelScale) + 0.5))
	height := int(math.Floor(float64(shape.Height)*float64(pixelScale) + 0.5))
	if width < 1 || height < 1 {
		return
	}
	needed := width * height * 4
	slot := view.pointerPixelSlot
	if cap(view.pointerPixels[slot]) < needed {
		view.pointerPixels[slot] = make([]byte, needed)
	}
	pixels := view.pointerPixels[slot][:needed]
	for y := range height {
		pageY := (float64(top+y) + 0.5) / float64(pixelScale)
		remoteY := min(max(int(math.Floor((pageY-float64(origin.Y))/float64(scale))), 0), pictureHeight-1)
		row := min(y*shape.Height/height, shape.Height-1)
		for x := range width {
			at := (y*width + x) * 4
			column := min(x*shape.Width/width, shape.Width-1)
			index := row*shape.Width + column
			if !shape.Invert[index] {
				copyPremultipliedBGRA(pixels[at:at+4], shape.BGRA[index*4:index*4+4])
				continue
			}
			pageX := (float64(left+x) + 0.5) / float64(pixelScale)
			remoteX := min(max(int(math.Floor((pageX-float64(origin.X))/float64(scale))), 0), pictureWidth-1)
			blue, green, red, _, ok := view.surface.Pixel(remoteX, remoteY)
			if !ok {
				pixels[at], pixels[at+1], pixels[at+2], pixels[at+3] = 0, 0, 0, 0
				continue
			}
			pixels[at], pixels[at+1], pixels[at+2], pixels[at+3] = 255-blue, 255-green, 255-red, 255
		}
	}
	if view.pointerBitmapID == 0 {
		view.pointerBitmapID = desktopPointerBitmapIDBase + desktopPointerBitmapIDs.Add(1)
	}
	prev := view.pointerPixels[1-slot]
	if view.pointerBitmapVersion > 0 &&
		view.pointerBitmapWidth == width && view.pointerBitmapHeight == height &&
		bytes.Equal(pixels, prev) {
		pixels = prev
	} else {
		view.pointerBitmapVersion++
		if view.pointerBitmapVersion == 0 {
			view.pointerBitmapVersion = 1
		}
		view.pointerBitmapWidth, view.pointerBitmapHeight = width, height
		view.pointerPixelSlot = 1 - slot
	}
	canvas.DrawBitmapPixels(gfx.Bitmap{
		Pix: pixels, Width: width, Height: height, Stride: width * 4,
		ID: view.pointerBitmapID, Version: view.pointerBitmapVersion,
	}, gfx.Point{X: float32(left) / pixelScale, Y: float32(top) / pixelScale})
}

// desktopGeometry is where a remote-desktop page puts things, in the page's own
// coordinates: the bar under the window bar, the area the picture is shown in,
// and the status line under it. Painting and input both use it.
type desktopGeometry struct {
	area  gfx.Rect
	strip gfx.Rect
}

// desktopLayout places the page for a pane of size. A host that asked for the
// bars gone (Chromeless) gives their room to the picture: the page is the window
// bar and the picture, nothing between.
func desktopLayout(size gfx.Size, chromeless bool) desktopGeometry {
	top := windowBarHeight
	bottom := desktopStripHeight
	if chromeless {
		bottom = 0
	} else {
		top += infoBarHeight
	}
	height := max(size.Height-top-bottom, 0)
	return desktopGeometry{
		area:  gfx.Rect{Y: top, Width: size.Width, Height: height},
		strip: gfx.Rect{Y: top + height, Width: size.Width, Height: min(bottom, max(size.Height-top, 0))},
	}
}

// desktopGeometryAt places the page in a pane: the same geometry desktopLayout
// gives a page-sized area, moved to the pane. The pane's top is where the window
// bar ends, so the page's own windowBarHeight offset is taken back out.
func desktopGeometryAt(bounds gfx.Rect, chromeless bool) desktopGeometry {
	geometry := desktopLayout(gfx.Size{Width: bounds.Width, Height: bounds.Height + windowBarHeight}, chromeless)
	geometry.area.X += bounds.X
	geometry.area.Y += bounds.Y - windowBarHeight
	geometry.strip.X += bounds.X
	geometry.strip.Y += bounds.Y - windowBarHeight
	return geometry
}

// layout is desktopGeometryAt for the pane the view is shown in, or the whole
// window for a fullscreen desktop.
func (view *desktopView) layout(bounds gfx.Rect) desktopGeometry {
	if view.fullscreen {
		return desktopGeometry{area: gfx.Rect{Width: bounds.Width, Height: bounds.Height}}
	}
	return desktopGeometryAt(bounds, view.chromeless)
}

// place says where the picture sits in area and at what scale (logical pixels
// per picture pixel). A picture shown pixel for pixel starts at the top left of
// the area, as the window of a remote machine does, and sits on a device pixel.
func (view *desktopView) place(area gfx.Rect) (origin gfx.Point, scale float32) {
	width, height := float32(view.width), float32(view.height)
	if width <= 0 || height <= 0 || area.Width <= 0 || area.Height <= 0 {
		return gfx.Point{X: area.X, Y: area.Y}, 1
	}
	oneToOne := view.oneToOne()
	scale = view.logicalPerPixel()
	if !oneToOne {
		scale = min(area.Width/width, area.Height/height, desktopMaxScale)
	}
	shownWidth, shownHeight := width*scale, height*scale
	axis := func(start, room, shown, pointer float32) float32 {
		if oneToOne && view.fit {
			return start
		}
		if shown <= room {
			return start + (room-shown)/2
		}
		// Larger than the pane: the part shown follows the pointer, so that the
		// remote pixel under the pointer stays under it.
		fraction := min(max((pointer-start)/room, 0), 1)
		return start - fraction*(shown-room)
	}
	origin = gfx.Point{
		X: axis(area.X, area.Width, shownWidth, view.pointer.X),
		Y: axis(area.Y, area.Height, shownHeight, view.pointer.Y),
	}
	if oneToOne && view.pixelScale > 0 {
		snap := func(value float32) float32 {
			return float32(math.Floor(float64(value)*float64(view.pixelScale)+0.5)) / view.pixelScale
		}
		origin = gfx.Point{X: snap(origin.X), Y: snap(origin.Y)}
	}
	return origin, scale
}

// remote converts a page point to a pixel of the remote picture, clamped to it.
func (view *desktopView) remote(area gfx.Rect, x, y float32) (int, int) {
	origin, scale := view.place(area)
	rx := int(math.Floor(float64((x - origin.X) / scale)))
	ry := int(math.Floor(float64((y - origin.Y) / scale)))
	return min(max(rx, 0), max(view.width-1, 0)), min(max(ry, 0), max(view.height-1, 0))
}

// desktopBounds is the rectangle the desktop is shown in, in window coordinates:
// its pane, or the whole window when it is fullscreen.
func (state *State) desktopBounds(session *Session) gfx.Rect {
	return state.desktopBoundsAt(session, state.viewport)
}

func (state *State) desktopBoundsAt(session *Session, size gfx.Size) gfx.Rect {
	if session.desk != nil && session.desk.fullscreen {
		return gfx.Rect{Width: size.Width, Height: size.Height}
	}
	if bounds, ok := state.paneWindowBounds(session.ID, size); ok {
		return bounds
	}
	return gfx.Rect{Width: size.Width, Height: size.Height}
}

// desktopGeometry is where the session's desktop puts its bars and picture.
func (state *State) desktopGeometry(session *Session) desktopGeometry {
	return session.desk.layout(state.desktopBounds(session))
}

// desktopGeometryAtSize is desktopGeometry for an event's viewport, which may be
// newer than the last paint.
func (state *State) desktopGeometryAtSize(session *Session, size gfx.Size) desktopGeometry {
	return session.desk.layout(state.desktopBoundsAt(session, size))
}

// openDesktop starts showing the desktop a connection has brought up.
func (state *State) openDesktop(session *Session) {
	source, ok := session.conn.(desktopSource)
	var remote desktop.Session
	if ok {
		remote = source.Desktop()
	}
	if remote == nil {
		session.term.ended = true
		session.tone = toneFail
		session.err, session.shellFailed = fmt.Errorf("%s", state.messages.Desktop.ErrNoDesktop), true
		session.finished = state.now()
		// The transport connected but there is no desktop to show: the journal
		// records a failed session rather than a usable one.
		state.submitSessionLogEvent(session, sessionlog.EventShellFailed, 0)
		state.finishSessionLog(session, sessionlog.StatusFailed, sessionlog.ReasonShellFailed)
		state.describe(session)
		return
	}
	if session.desk != nil {
		session.desk.close()
	}
	session.desk = newDesktopView(remote, session.host.Clipboard)
	if _, fixed := session.host.fixedDesktopSize(); fixed {
		// A host with a chosen resolution keeps it: the server is never asked
		// to follow the pane, so the picture is scaled to the pane instead.
		session.desk.resizer = nil
	}
	session.desk.chromeless = session.host.Chromeless
	session.term.ended = false
	session.desk.active = state.desktopVisible(session)
	remote.SetActive(session.desk.active)
	session.desk.present()
	state.goLive(session)
}

// endDesktop keeps the last picture on show, dimmed, behind the page that says
// the connection ended and offers to start it again.
func (state *State) endDesktop(session *Session) {
	if session.desk == nil || session.desk.ended {
		return
	}
	session.desk.ended = true
	session.desk.pointerKind = desktop.PointerSystem
	session.desk.inverting = false
	session.desk.cursorImage = nil
	session.desk.releaseInput()
	session.term.ended = true
}

// desktopVisible reports whether anyone can see the session's picture: the
// window is not minimized, the session is in the active workspace and the
// section that shows sessions is open. A window another window covers still
// counts as visible; Viva does not report occlusion.
func (state *State) desktopVisible(session *Session) bool {
	if state.winChrome.minimized || state.activeSection != sectionTerminal {
		return false
	}
	if fullscreen := state.fullscreenDesktop(); fullscreen != nil {
		return fullscreen.ID == session.ID
	}
	owner, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	return ok && owner == state.deck.ActiveWorkspace()
}

// pumpDesktop moves what the connection delivered to the picture and the clipboard,
// and tells it whether to keep asking for updates.
func (state *State) pumpDesktop(session *Session) {
	view := session.desk
	if view == nil {
		return
	}
	view.present()
	if visible := state.desktopVisible(session); visible != view.active {
		view.active = visible
		view.session.SetActive(visible)
		if !visible {
			view.releaseInput()
		}
	}
	view.session.Bells() // a bell has no sound to make here
	state.finishFileOffers(view)
	receivedFiles := false
	if receiver, ok := view.session.(desktop.FileReceiver); ok {
		state.pumpReceivingFiles(view, receiver)
		if files, ok := receiver.RemoteFiles(); ok {
			state.receiveDesktopFiles(view, files)
			receivedFiles = len(files.Paths) > 0
		}
	}
	if text, ok := view.session.RemoteClipboard(); ok && view.clipboard && !receivedFiles {
		view.sentClip = text // what the remote has: not to be sent back
		if err := state.options.SetClipboardText(text); err != nil {
			state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		}
	}
}

// ClipboardChanged reports that the system clipboard's content changed. It
// may run on any goroutine (the platform's clipboard watcher); the next Wake
// offers the new content to the desktop session the user is working in.
func (state *State) ClipboardChanged() {
	state.clipboardDirty.Store(true)
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

// offerClipboard gives a changed clipboard to the desktop session in front of
// the user: the focused pane's, in a window that is showing. Other sessions,
// on tabs behind or in a window hidden to the tray, are not told what is copied
// elsewhere; they get it when the user comes to them and their own input
// offers it (syncClipboard), and the change stays pending until a desktop
// session is in front.
func (state *State) offerClipboard() {
	if !state.clipboardDirty.Load() || state.activeSection != sectionTerminal ||
		state.winChrome.minimized || (state.windowVisible != nil && !state.windowVisible()) {
		return
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.ID != state.activeSessionID || session.desk == nil {
			continue
		}
		if state.clipboardDirty.CompareAndSwap(true, false) {
			state.syncClipboard(session.desk)
		}
		return
	}
}

// syncClipboard gives the remote the local clipboard if it changed since the
// last time, for a host that shares it: its files, or its text, or the picture a
// clipboard with no words holds.
func (state *State) syncClipboard(view *desktopView) {
	if !view.clipboard || view.ended {
		return
	}
	if view.files.remote != nil && state.syncClipboardFiles(view) {
		return
	}
	if text, err := state.options.ClipboardText(); err == nil && text != "" {
		if text == view.sentClip {
			return
		}
		view.sentClip = text
		view.sentImage = imageDigest{}
		view.files.dropped = false
		_ = view.session.SetClipboard(text)
		return
	}
	// The clipboard holds no words, and some systems answer a text request on a
	// picture with an error. A picture is not an empty clipboard on a host that
	// takes one: offer it, as a paste in a terminal gives the shell a path to it.
	state.syncClipboardImage(view)
}

// syncClipboardImage offers the picture on the system clipboard when it is what
// the clipboard holds. A host whose protocol carries only text (VNC) is never
// asked for it.
func (state *State) syncClipboardImage(view *desktopView) {
	clipboard, ok := view.session.(desktop.ImageClipboard)
	if !ok {
		return
	}
	png, digest, ok := state.clipboardImage()
	if !ok {
		view.sentImage = imageDigest{}
		return
	}
	if digest == view.sentImage {
		return
	}
	view.sentImage = digest
	// The picture replaces what the remote clipboard held, not only the words
	// here: text it still had is not what was copied any more.
	view.sentClip = ""
	view.files.dropped = false
	_ = clipboard.SetClipboardImage(png)
}

// clipboardImage reads the picture on the system clipboard, if one is there.
func (state *State) clipboardImage() (png []byte, digest imageDigest, ok bool) {
	png, err := state.options.ClipboardImagePNG()
	if err != nil || len(png) == 0 {
		return nil, imageDigest{}, false
	}
	return png, imageDigest(sha256.Sum256(png)), true
}

// heldKey is a key the remote side has down: the local key that is holding it
// (Key and Text identify it from press to release) and what was pressed there.
type heldKey struct {
	local  gfx.Key
	text   string
	remote desktop.KeyEvent
}

// remoteModifier is the remote modifier a local modifier key stands for.
func remoteModifier(key gfx.Key) (desktop.Key, bool) {
	switch key {
	case gfx.KeyShiftLeft, gfx.KeyShiftRight:
		return desktop.KeyShift, true
	case gfx.KeyControlLeft, gfx.KeyControlRight:
		return desktop.KeyControl, true
	case gfx.KeyAltLeft, gfx.KeyAltRight:
		return desktop.KeyAlt, true
	case gfx.KeyMetaLeft, gfx.KeyMetaRight:
		return desktop.KeyMeta, true
	}
	return desktop.KeyNone, false
}

func (view *desktopView) holding(local gfx.Key, text string) int {
	for index, entry := range view.held {
		if entry.local == local && entry.text == text {
			return index
		}
	}
	return -1
}

// remoteHolds reports whether another held key keeps the same remote key down,
// as the left and right Shift do.
func (view *desktopView) remoteHolds(event desktop.KeyEvent, except int) bool {
	for index, entry := range view.held {
		if index != except && entry.remote == event {
			return true
		}
	}
	return false
}

// press sends a key going down. A press that repeats a key already down is sent
// again with Repeat set: RDP servers do not repeat a key they see held, and a
// backend whose server does repeat it ignores the flag. A repeat whose first
// press went to a shortcut of Motty's is not sent, because the remote never had
// the key down.
func (view *desktopView) press(local gfx.Key, text string, event desktop.KeyEvent) {
	index := view.holding(local, text)
	if index < 0 {
		if event.Repeat {
			return
		}
		view.held = append(view.held, heldKey{local: local, text: text, remote: event})
		event.Down = true
		_ = view.session.SendKey(event)
		return
	}
	if !event.Repeat {
		return
	}
	repeat := view.held[index].remote
	repeat.Down = true
	repeat.Repeat = true
	_ = view.session.SendKey(repeat)
}

// release sends the end of a key that press sent, and nothing for any other.
func (view *desktopView) release(local gfx.Key, text string) {
	index := view.holding(local, text)
	if index < 0 {
		return
	}
	event := view.held[index].remote
	shared := view.remoteHolds(event, index)
	view.held = slices.Delete(view.held, index, index+1)
	if !shared {
		event.Down = false
		_ = view.session.SendKey(event)
	}
}

// releaseInput lets go of everything held on the remote side, last pressed
// first: a key or button left down by an interrupted gesture would stay down
// there.
func (view *desktopView) releaseInput() {
	view.buttons = 0
	for len(view.held) > 0 {
		last := view.held[len(view.held)-1]
		view.release(last.local, last.text)
	}
}

// tap presses and releases a chord that has no physical keys behind it.
func (view *desktopView) tap(mods []desktop.Key, event desktop.KeyEvent) {
	for _, key := range mods {
		_ = view.session.SendKey(desktop.KeyEvent{Key: key, Down: true})
	}
	event.Down = true
	_ = view.session.SendKey(event)
	event.Down = false
	_ = view.session.SendKey(event)
	for index := len(mods) - 1; index >= 0; index-- {
		_ = view.session.SendKey(desktop.KeyEvent{Key: mods[index]})
	}
}

var desktopKeys = map[gfx.Key]desktop.Key{
	gfx.KeyTab: desktop.KeyTab, gfx.KeyEnter: desktop.KeyEnter, gfx.KeyEscape: desktop.KeyEscape,
	gfx.KeyLeft: desktop.KeyLeft, gfx.KeyRight: desktop.KeyRight, gfx.KeyUp: desktop.KeyUp, gfx.KeyDown: desktop.KeyDown,
	gfx.KeyHome: desktop.KeyHome, gfx.KeyEnd: desktop.KeyEnd, gfx.KeyBackspace: desktop.KeyBackspace,
	gfx.KeyDelete: desktop.KeyDelete, gfx.KeyPageUp: desktop.KeyPageUp, gfx.KeyPageDown: desktop.KeyPageDown,
	gfx.KeyInsert: desktop.KeyInsert,
}

// desktopKeyEvent maps a Viva key event to the remote key it means.
func desktopKeyEvent(event gfx.KeyEvent) (desktop.KeyEvent, bool) {
	switch {
	case event.Key >= gfx.KeyF1 && event.Key <= gfx.KeyF12:
		return desktop.KeyEvent{Key: desktop.KeyF1 + desktop.Key(event.Key-gfx.KeyF1)}, true
	case event.Key == gfx.KeySpace:
		return desktop.KeyEvent{Rune: ' '}, true
	case event.Key != gfx.KeyUnknown:
		key, ok := desktopKeys[event.Key]
		return desktop.KeyEvent{Key: key}, ok
	}
	r, _ := utf8.DecodeRuneInString(event.Text)
	if r == utf8.RuneError || r == 0 {
		return desktop.KeyEvent{}, false
	}
	key := desktop.KeyEvent{Rune: unicode.ToLower(r)}
	if typed, _ := utf8.DecodeRuneInString(event.Typed); typed != utf8.RuneError && typed != key.Rune {
		key.Typed = typed
	}
	return key, true
}

// focusedDesktop is the desktop a keyboard event is for: the session whose
// picture has the focus.
func (state *State) focusedDesktop(target gfx.ID) (*Session, *desktopView) {
	if _, local, ok := state.decodePaneHit(target); ok {
		target = local
	}
	if state.activeSection != sectionTerminal || state.modal != modalNone {
		return nil, nil
	}
	session := state.terminalSession(target)
	if session == nil || session.desk == nil {
		return nil, nil
	}
	return session, session.desk
}

func isEscapeChord(event gfx.KeyEvent) bool {
	return event.Modifiers == gfx.ModifierControl|gfx.ModifierAlt && strings.EqualFold(event.Text, "g")
}

// toggleCapture hands the keyboard to the remote desktop, or back to Motty.
func (state *State) toggleCapture(view *desktopView) {
	view.released = !view.released
	if view.released {
		view.releaseInput()
		state.status = state.messages.Desktop.StatusReleased
	} else {
		state.status = state.messages.Desktop.StatusCaptured
	}
}

// desktopHoldsTheKeyboard reports whether a remote desktop is what the user is typing into: the
// session in front is a live desktop whose keyboard has not been handed back to Motty, in a
// window that is showing, with no dialog of Motty's over it.
func (state *State) desktopHoldsTheKeyboard() bool {
	if state.activeSection != sectionTerminal || state.modal != modalNone || state.winChrome.minimized ||
		(state.windowVisible != nil && !state.windowVisible()) {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.desk == nil || session.desk.ended || session.desk.released {
		return false
	}
	return state.desktopVisible(session)
}

// keepKeyboardOnDesktop gives the keyboard back to the remote desktop in front after a click on
// one of Motty's own controls (a tab, a toolbar button, a menu entry). A press moves the focus to
// the control, but while the desktop holds the keyboard every key, Alt+Tab included, is the
// remote's, and only the focused desktop is sent them: left on the control, Alt+Tab would be a
// Tab through Motty's controls and the modifier press would never leave the window. The picture
// asks for it in the paint that follows, so a control that opens its own field or menu in the
// same click still decides where the keyboard goes.
func (state *State) keepKeyboardOnDesktop() {
	if !state.desktopHoldsTheKeyboard() {
		return
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.desk != nil {
		session.desk.focusPending = true
	}
}

// systemKeyCapture is how the window takes the system's shortcuts (the Windows key and its
// chords, Alt+Tab, Ctrl+Esc) for itself, nil where it cannot, and what it was last asked.
type systemKeyCapture struct {
	take  func(bool) error
	taken bool
}

// syncShortcutCapture has the window take the system's shortcuts while a remote desktop holds
// the keyboard and give them back when it does not, as mstsc does for its own window. Viva keeps
// them for the window only while it has the focus, and any other window keeps its own. The
// escape chord that hands the keyboard back hands the shortcuts back with it.
func (state *State) syncShortcutCapture() {
	capture := &state.systemKeys
	if capture.take == nil {
		return
	}
	if want := !state.appAccessBlocked() && state.desktopHoldsTheKeyboard(); want != capture.taken {
		capture.taken = want
		if err := capture.take(want); err != nil && want {
			// A platform that cannot take them leaves them with the system, as before.
			capture.take = nil
		}
	}
}

// desktopShortcut keeps Motty's own shortcuts from running while the keyboard
// belongs to the remote desktop. Only the escape chord is Motty's then.
func (state *State) desktopShortcut(view *desktopView, event gfx.KeyEvent) gfx.EventResult {
	if isEscapeChord(event) {
		if !event.Repeat {
			state.toggleCapture(view)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	// The paste shortcut of this computer, when what is to be pasted is files: the
	// text system would paste text (and nothing at all from a clipboard that has
	// none, as a file manager's copy on Windows is), so Motty presses the remote's
	// own paste instead. Held until the files are offered, in an event of its own.
	if !event.Release && event.Modifiers == keymap.Primary && strings.EqualFold(event.Text, "v") {
		if !event.Repeat && state.pasteFiles(view) {
			return gfx.EventResult{Handled: true}
		}
	}
	return gfx.EventResult{}
}

// desktopKey sends a raw key to the remote desktop.
func (state *State) desktopKey(session *Session, event gfx.KeyEvent) gfx.EventResult {
	view := session.desk
	if isEscapeChord(event) {
		return state.desktopShortcut(view, event)
	}
	if view.ended {
		if event.Key == gfx.KeyEnter && !event.Repeat && state.canReconnect(session) {
			state.reconnect(session)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	if view.released {
		return gfx.EventResult{}
	}
	if modifier, ok := remoteModifier(event.Key); ok {
		if event.Release {
			view.release(event.Key, event.Text)
		} else {
			view.press(event.Key, event.Text, desktop.KeyEvent{Key: modifier, Repeat: event.Repeat})
		}
		return gfx.EventResult{Handled: true}
	}
	if event.Release {
		view.release(event.Key, event.Text)
		return gfx.EventResult{Handled: true}
	}
	if key, ok := desktopKeyEvent(event); ok {
		if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) != 0 && key.Rune == 'v' {
			state.syncClipboard(view)
		}
		key.Repeat = event.Repeat
		view.press(event.Key, event.Text, key)
	}
	return gfx.EventResult{Handled: true}
}

// desktopPointer sends the pointer to the remote desktop.
func (state *State) desktopPointer(session *Session, event gfx.PointerEvent) gfx.EventResult {
	view := session.desk
	if view.ended {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerExit {
		// The pointer left the picture: a pointer the workbench paints goes with it.
		gone, repaint := view.pointerInside, view.inverting
		view.pointerInside = false
		return gfx.EventResult{Repaint: gone && repaint}
	}
	geometry := state.desktopGeometryAtSize(session, event.Viewport)
	before := view.pointer
	entered := !view.pointerInside
	view.pointer = gfx.Point{X: event.X, Y: event.Y}
	view.pointerInside = true
	moved := view.pointer != before
	panned := moved && !view.fit
	x, y := view.remote(geometry.area, event.X, event.Y)
	var button desktop.Buttons
	switch event.Button {
	case gfx.PointerButtonPrimary:
		button = desktop.ButtonLeft
	case gfx.PointerButtonMiddle:
		button = desktop.ButtonMiddle
	case gfx.PointerButtonSecondary:
		button = desktop.ButtonRight
	}
	switch event.Kind {
	case gfx.PointerDown:
		state.syncClipboard(view)
		view.buttons |= button
	case gfx.PointerUp:
		view.buttons &^= button
	}
	_ = view.session.SendPointer(x, y, view.buttons)
	return gfx.EventResult{Handled: event.Kind != gfx.PointerMove, Repaint: (panned || view.inverting && (moved || entered)) && view.width > 0}
}

// desktopScroll turns scrolling into wheel notches.
func (state *State) desktopScroll(session *Session, event gfx.ScrollEvent) gfx.EventResult {
	view := session.desk
	if view.ended {
		return gfx.EventResult{Handled: true}
	}
	view.wheel.X += event.StepX(desktopWheelStep)
	view.wheel.Y += event.StepY(desktopWheelStep)
	notches := func(accumulated *float32) int {
		whole := int(*accumulated / desktopWheelStep)
		*accumulated -= float32(whole) * desktopWheelStep
		return whole
	}
	wheel := desktop.Wheel{X: notches(&view.wheel.X), Y: notches(&view.wheel.Y)}
	if wheel != (desktop.Wheel{}) {
		x, y := view.remote(state.desktopGeometryAtSize(session, event.Viewport).area, event.X, event.Y)
		_ = view.session.SendWheel(x, y, wheel, view.buttons)
	}
	return gfx.EventResult{Handled: true}
}

// desktopTextInput is the native text surface of a focused desktop: a paste, or
// text a service inserts, is typed on the remote side. The host's input method
// stays off it — the desktop on the other side runs its own, whose candidate
// windows appear in the picture — so the keys that switch and feed a method are
// the remote's raw keys, the way the platform's own client behaves.
func (state *State) desktopTextInput(session *Session) *app.TextInput {
	view, t := session.desk, session.term
	t.input.Bounds = gfx.Rect{X: view.pointer.X, Y: view.pointer.Y, Width: 2, Height: 18}
	t.input.FontSize = 14
	t.input.Color = state.pal.text
	t.input.State = t.preedit
	t.input.AltSendsEscape = true
	t.input.KeyReleases = true
	t.input.DisableIME = true
	if t.input.OnEvent == nil {
		sessionID := session.ID
		t.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
			return state.desktopText(sessionID, event), nil
		}
	}
	return &t.input
}

func (state *State) desktopText(sessionID uint32, event app.TextEvent) app.TextState {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.desk == nil {
		return app.TextState{}
	}
	view, t := session.desk, session.term
	typing := !view.released && !view.ended
	switch event.Kind {
	case app.TextInsert:
		t.preedit = app.TextState{}
		if typing {
			_ = view.session.SendText(event.Text)
		}
	case app.TextPaste, app.TextPasteNoText:
		if !typing {
			break
		}
		// Files are pasted by the remote itself (Ctrl+V): typing their names, which
		// a file manager also leaves on the clipboard as text, would paste nothing.
		if state.pasteFiles(view) {
			break
		}
		if event.Kind == app.TextPaste {
			_ = view.session.SendText(event.Text)
		}
	case app.TextSetMarked:
		runes := utf8.RuneCountInString(event.Text)
		t.preedit = app.TextState{
			Value: event.Text, Selection: event.MarkedSelection,
			Composition: app.TextRange{End: runes}, Composing: runes > 0,
		}
	case app.TextUnmark:
		t.preedit = app.TextState{}
	}
	return t.preedit
}

// clickDesktop handles the controls of a desktop page.
func (state *State) clickDesktop(id gfx.ID) bool {
	if id.Scope() != desktopHits {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.desk == nil {
		return true
	}
	view := session.desk
	switch id {
	case hitDesktopFit:
		view.fit = true
	case hitDesktopActual:
		view.fit = false
	case hitDesktopFullscreen:
		state.toggleDesktopFullscreen(session)
	case hitDesktopBarPin:
		state.setDesktopFullscreenBarAutoHide(!state.desktopBar.autoHide)
	case hitDesktopBarDock:
		view.dockMenu = !view.dockMenu
	case hitDesktopQuality:
		if !view.ended {
			view.qualityMenu = !view.qualityMenu
		}
	case hitDesktopResolution:
		if !view.ended {
			view.resolutionMenu = !view.resolutionMenu
		}
	case hitDesktopCapture:
		state.toggleCapture(view)
	case hitDesktopCtrlAltDel:
		if !view.ended {
			view.tap([]desktop.Key{desktop.KeyControl, desktop.KeyAlt}, desktop.KeyEvent{Key: desktop.KeyDelete})
		}
	case hitDesktopReconnect:
		if state.canReconnect(session) {
			state.reconnect(session)
		}
	}
	return true
}

// declareDesktopBody declares the bars of a remote desktop in its pane: the
// title and state over the picture, the controls that act on it, and the strip
// that names the escape chord under it. The picture itself is a slot the page
// paints, so the controls declared over it are page elements too.
func (state *State) declareDesktopBody(c ui.C, session *Session, bounds gfx.Rect) {
	view, s := session.desk, state.paneBody(session)
	s.declared = true
	pal := state.pal
	canvas := c.Canvas()
	view.present()
	view.pixelScale = canvas.PixelScale()
	view.area = view.layout(bounds).area
	title := session.Title
	if title == "" {
		title = session.Address
	}
	status, _ := state.sessionStatus(session)
	showStatus := session.phase != sshconn.PhaseConnected || view.ended
	s.light, s.capture, s.latency = ui.Box{}, ui.Box{}, ui.Box{}
	s.latencyShown = false
	if !view.chromeless && !view.fullscreen {
		s.latencyBadge, s.latencyShown = state.desktopLatencyBadge(canvas, view)
	}
	if s.pictureDraw == nil || s.pictureSession != session.ID {
		// A pane slot outlives the session it showed: bind the draw to this
		// session's identity and look it up by it, so a later one in the same
		// slot paints its own picture.
		id := session.ID
		s.pictureSession = id
		s.pictureDraw = func(d *ui.DrawContext) {
			if session, ok := state.sessionByIDPointer(id); ok && session.desk != nil {
				state.paintDesktopPicture(d, session)
			}
		}
	}
	if view.chromeless || view.fullscreen {
		ui.Stack(c).Fill().Background(pal.workspace).Children(func() {
			state.declareDesktopInfoBar(c, session, view, s, bounds, title, status, showStatus, false)
			state.declareDesktopStrip(c, session, view, s, true)
			ui.Slot(c).Absolute().Left(0).Right(0).Top(infoBarHeight).Bottom(desktopStripHeight).
				Background(state.ground(state.termPal.terminal)).Draw(s.pictureDraw)
			state.declareDesktopEnded(c, session, view.area.X-bounds.X, view.area.Y-bounds.Y)
			if view.fullscreen {
				state.declareDesktopFullscreenControls(c, view, session)
			}
		})
		return
	}
	ui.Column(c).Fill().Background(pal.workspace).Children(func() {
		state.declareDesktopInfoBar(c, session, view, s, bounds, title, status, showStatus, true)
		ui.Stack(c).Grow(1).Children(func() {
			ui.Slot(c).Fill().Background(state.ground(state.termPal.terminal)).Draw(s.pictureDraw)
			state.declareDesktopEnded(c, session, 0, 0)
		})
		state.declareDesktopStrip(c, session, view, s, false)
	})
}

// declareDesktopInfoBar declares the line over the picture: the light, the
// title and state, the controls that act on the desktop, and the keyboard
// switch. withTools is off where the view shows the picture alone.
func (state *State) declareDesktopInfoBar(c ui.C, session *Session, view *desktopView, s *sessionPaneUI, bounds gfx.Rect, title, status string, showStatus, withTools bool) {
	pal, m := state.pal, state.messages
	canvas := c.Canvas()
	bar := ui.Stack(c).Height(infoBarHeight).Shrink(0).Background(pal.workspace)
	if !withTools {
		bar = bar.Absolute().Left(0).Right(0).Top(0)
	}
	bar.Children(func() {
		ui.Row(c).Fill().PadSides(0, termPadX, 0, termPadX-2).Items(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Gap(8).MarginSides(0, 10, 0, 0).Items(ui.Center).Children(func() {
				s.light = ui.Slot(c).Size(16, 16)
				if withTools && session.busy {
					// The page's spinner paints the light's place while the
					// connection is busy and asks for frames itself.
					s.light = s.light.Children(func() { ui.Spinner(c).Size(16, 16) })
				}
				ui.Text(c, title).FontSize(barTitleSize).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22).
					Width(canvas.TextWidthWeight(title, barTitleSize, gfx.WeightSemibold)+2).Ellipsis().MarginSides(0, 4, 0, 0)
				if showStatus {
					ui.Text(c, status).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(22).
						Width(canvas.TextWidthWeight(status, metrics.FontSizeS, gfx.WeightMedium) + 2)
				}
			})
			ui.Row(c).Gap(barToolGap).Shrink(0).Items(ui.Center).Children(func() {
				if withTools {
					state.declareDesktopTools(c, session, view, bounds)
					captureStyle, captureHint := iconButtonActive, fmt.Sprintf(m.Desktop.TipCapture, desktopEscapeChord)
					if view.released {
						captureStyle, captureHint = iconButtonQuiet, fmt.Sprintf(m.Desktop.TipRelease, desktopEscapeChord)
					}
					s.capture = state.declareIconButton(c, state.paneIdentity(session, hitDesktopCapture), iconKeyboard, captureHint, view.ended, captureStyle).Size(barToolSize, barToolSize).Shrink(0)
				}
				if withTools && state.paneInSplit(session.ID) {
					state.declareSessionMaximizeButton(c, session)
					state.declarePaneBarClose(c, session)
				}
			})
		})
		ui.Row(c).FillWidth().Height(1).Self(ui.End).Background(pal.line)
	})
}

// declareDesktopTools declares the scaling choice, Ctrl+Alt+Del where the pane
// has room for it, and the keyboard switch along the right of the info bar. The
// row sizes itself to its labels; the app's identities are kept, so the central
// click handler still runs the actions.
func (state *State) declareDesktopTools(c ui.C, session *Session, view *desktopView, bounds gfx.Rect) {
	m := state.messages.Desktop
	ui.Row(c).Gap(state.theme.Metrics.GapItem).Shrink(0).Items(ui.Center).Children(func() {
		if bounds.Width >= 760 {
			label := m.CtrlAltDel
			button := ui.Button(c, label).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopCtrlAltDel), label)
			if view.ended {
				button = button.Disabled(true)
			}
		}
		fitLabel, actualLabel, fullscreenLabel := m.Fit, m.Actual, m.Fullscreen
		fit := ui.Button(c, fitLabel).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopFit), fitLabel)
		if view.fit {
			fit = fit.Secondary()
		}
		actual := ui.Button(c, actualLabel).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopActual), actualLabel)
		if !view.fit {
			actual = actual.Secondary()
		}
		var resolution ui.Box
		rdpBar := session.host.Protocol == protocolRDP
		if rdpBar {
			resolution = ui.Button(c, m.Resolution).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopResolution), m.Resolution)
			if view.ended {
				resolution = resolution.Disabled(true)
			}
		}
		fullscreen := ui.Button(c, fullscreenLabel).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopFullscreen), fullscreenLabel)
		if view.ended {
			fullscreen = fullscreen.Disabled(true)
		}
		quality := ui.Button(c, m.Quality).XSmall().Ghost().Hit(state.paneIdentity(session, hitDesktopQuality), m.Quality)
		if view.ended {
			quality = quality.Disabled(true)
		}
		state.declareDesktopQualityMenu(c, session, view, quality)
		if rdpBar {
			state.declareDesktopResolutionMenu(c, session, view, resolution)
		}
	})
}

const (
	desktopEndedCardWidth  = float32(460)
	desktopEndedCardHeight = float32(172)
	desktopEndedCardPad    = float32(20)
)

func (state *State) declareDesktopEnded(c ui.C, session *Session, left, top float32) {
	view := session.desk
	if !view.ended {
		return
	}
	area := view.area
	cardWidth := min(max(area.Width-16, 0), desktopEndedCardWidth)
	cardHeight := min(max(area.Height-16, 0), desktopEndedCardHeight)
	pad := min(desktopEndedCardPad, max((cardHeight-52)/2, 0))
	pal, m := state.pal, state.messages.Desktop
	reason := state.failureLine(session)
	if session.err != nil && reason == "" {
		reason = session.err.Error()
	}
	ui.Stack(c).Absolute().Left(left).Top(top).Width(area.Width).Height(area.Height).Clip().
		Background(withAlpha(pal.shell, 0.78)).Children(func() {
		ui.Slot(c).Fill().Draw(func(d *ui.DrawContext) { d.Canvas.Block(d.Rect) })
		ui.Stack(c).Absolute().Left((area.Width-cardWidth)/2).Top((area.Height-cardHeight)/2).
			Width(cardWidth).Height(cardHeight).Background(pal.surface).Radius(radius.popup).
			Border(1, pal.line).Shadow(gfx.ShadowMD).Clip().Children(func() {
			ui.Column(c).Fill().Pad(pad).Children(func() {
				if cardHeight >= 52 {
					ui.Text(c, m.Disconnected).FontSize(16).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(24).Shrink(0).Ellipsis()
				}
				if reason != "" {
					ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Scrollbars().Children(func() {
						ui.Wrap(c, fmt.Sprintf(m.DisconnectedFmt, reason)).FillWidth().FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(18)
					})
				} else {
					ui.Slot(c).Grow(1).MinHeight(0)
				}
				if state.canReconnect(session) {
					ui.Row(c).FillWidth().Height(28).Shrink(0).Items(ui.Center).Justify(ui.End).Children(func() {
						ui.Button(c, m.Reconnect).Small().Hit(state.paneIdentity(session, hitDesktopReconnect), m.Reconnect)
					})
					if cardHeight >= 140 {
						ui.Text(c, m.ReconnectHint).FontSize(metrics.FontSizeS).Color(pal.faint).LineHeight(18).Shrink(0).Ellipsis()
					}
				}
			})
		})
	})
}

// declareDesktopStrip declares the status line under the picture: what the
// picture shows, the escape chord, and the latency badge. absolute puts it on
// the bottom edge of the stack that holds the picture, for a fullscreen view.
func (state *State) declareDesktopStrip(c ui.C, session *Session, view *desktopView, s *sessionPaneUI, absolute bool) {
	pal, m := state.pal, state.messages
	canvas := c.Canvas()
	strip := ui.Stack(c).Height(desktopStripHeight).Shrink(0).Background(pal.workspace)
	if absolute {
		strip = strip.Absolute().Left(0).Right(0).Bottom(0)
	}
	strip.Children(func() {
		ui.Row(c).FillWidth().Height(1).Self(ui.Start).Background(pal.line)
		ui.Row(c).Fill().PadX(termPadX).Items(ui.Center).Children(func() {
			ui.Text(c, state.desktopStripLeft(view, session)).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).LineHeight(16).Grow(1).Ellipsis()
			if s.latencyShown {
				s.latency = ui.Slot(c).Size(s.latencyBadge.width+12, 16).MarginSides(0, 8, 0, 0).Shrink(0)
			}
			chordText := m.Desktop.StripCaptured
			colour := pal.muted
			if view.released {
				chordText, colour = m.Desktop.StripReleased, pal.accent
			}
			chordText = fmt.Sprintf(chordText, desktopEscapeChord)
			ui.Text(c, chordText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(colour).LineHeight(16).
				Width(canvas.TextWidthWeight(chordText, metrics.FontSizeS, gfx.WeightMedium) + 2).Shrink(0)
		})
	})
}

// paintDesktopDetails paints what a remote desktop's page leaves by hand: the
// light or spinner in the info bar, the latency badge on the status strip, and
// the badges a fullscreen or chromeless view shows over the picture. The
// picture itself and the bars' controls are page elements.
func (state *State) paintDesktopDetails(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
	pal := state.pal
	view, s := session.desk, state.paneBody(session)

	if !view.chromeless && !view.fullscreen {
		if s.latencyShown {
			state.paintDesktopLatency(canvas, s.latency.Rect(), s.latencyBadge, state.paneIdentity(session, hitDesktopLatency))
		}
		// The busy spinner is a page leaf in the light's place; the resting
		// light is the window's own dot.
		if !session.busy {
			kind, _ := state.sessionLight(session)
			_, dot := state.sessionStatus(session)
			spot := s.light.Rect()
			paintLight(canvas, spot.X+spot.Width/2, spot.Y+spot.Height/2, kind, dot, pal.workspace)
		}
	}
	if view.fullscreen {
		state.paintDesktopFullscreenLatency(canvas, session)
	} else if view.chromeless {
		state.paintChromelessLatency(canvas, view, session, view.area)
	}
}

// paintDesktopPicture paints the picture in the slot the page declared for it:
// the ground, the picture clipped to its area, the end notice over it, the
// chromeless capture note, the remote pointer where the workbench draws it, and
// the hit and focus of the picture itself.
func (state *State) paintDesktopPicture(d *ui.DrawContext, session *Session) {
	m := state.messages
	view := session.desk
	canvas := d.Canvas
	area := view.area
	view.pixelScale = canvas.PixelScale()
	view.area = area
	state.followDesktop(canvas, view)
	// The area behind the picture is the terminal's ground, so a picture that
	// does not fill the pane sits on a quiet surface. The bars lay that ground
	// in their flex stack; a chromeless or fullscreen session drops the stack,
	// so the ground is painted here instead.
	if view.chromeless || view.fullscreen {
		canvas.Fill(area, state.ground(state.termPal.terminal))
	}
	canvas.Clip(area)
	var origin gfx.Point
	var scale float32 = 1
	if view.width > 0 && view.height > 0 {
		origin, scale = view.place(area)
		if view.oneToOne() {
			view.surface.DrawPixels(canvas, origin)
		} else {
			view.surface.Draw(canvas, gfx.Rect{X: origin.X, Y: origin.Y, Width: float32(view.width) * scale, Height: float32(view.height) * scale})
		}
	}
	if view.chromeless && !view.fullscreen && !view.released && !view.ended {
		state.paintDesktopCaptureNote(canvas, view, session, area)
	}
	picture := hitTerminalList.At(int(session.ID))
	view.paintPointer(canvas, origin, scale, canvas.Hovered() == picture && (canvas.Pressed().IsZero() || canvas.Pressed() == picture))
	canvas.Unclip()

	name := fmt.Sprintf(m.Desktop.AccessibleFmt, session.Address)
	cursor, cursorImage := view.pointerCursor(view.pixelScale, canvas.Hovered() == hitTerminalList.At(int(session.ID)))
	if view.ended {
		cursor, cursorImage = gfx.PointerCursorDefault, nil
	}
	canvas.HitWithOptions(hitTerminalList.At(int(session.ID)), area, name, gfx.HitOptions{
		Role: gfx.RoleTerminal, Autofocus: session.ID == state.activeSessionID && !view.ended, Cursor: cursor, CursorImage: cursorImage,
	})
	if view.focusPending {
		canvas.RequestFocus(hitTerminalList.At(int(session.ID)))
		view.focusPending = false
	}
}

// paintDesktopCaptureNote is the corner note a chromeless session shows while it
// holds the keyboard: the chord that hands it back, as the status line would say.
// A press of it hands the keyboard back, as the bar's button does.
func (state *State) paintDesktopCaptureNote(canvas *gfx.Canvas, view *desktopView, session *Session, area gfx.Rect) {
	pal, m := state.pal, state.messages
	chord := desktopEscapeChord
	text := canvas.TextWidthWeight(chord, metrics.FontSizeS, gfx.WeightMedium)
	pill := gfx.Rect{
		Width: 8 + 14 + 6 + text + 8, Height: 22,
		X: area.X + area.Width - (8 + 14 + 6 + text + 8) - 8, Y: area.Y + area.Height - 22 - 8,
	}
	canvas.RoundedBorder(pill, radius.item, 1, withAlpha(pal.line, 0.9), withAlpha(pal.shell, 0.92))
	paintIcon(canvas, iconKeyboard, gfx.Rect{X: pill.X + 8, Y: pill.Y + 4, Width: 14, Height: 14}, pal.text)
	canvas.Label(chord, gfx.Rect{X: pill.X + 8 + 14 + 6, Y: pill.Y, Width: text, Height: 22},
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.text})
	hint := fmt.Sprintf(m.Desktop.TipCapture, chord)
	canvas.HitWithOptions(state.paneIdentity(session, hitDesktopCapture), pill, hint, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint})
}

// desktopTargetFor is the desktop a session's pane can show pixel for pixel: the
// picture area of the page it is on, at the display scale last painted.
func (state *State) desktopTargetFor(session *Session) desktopTarget {
	bounds := state.desktopBounds(session)
	if session.desk != nil && session.desk.fullscreen {
		return desktopTargetFor(gfx.Rect{Width: bounds.Width, Height: bounds.Height}, state.pixelScale)
	}
	chromeless := session.host.Chromeless
	if session.desk != nil {
		chromeless = session.desk.chromeless
	}
	return desktopTargetFor(desktopGeometryAt(bounds, chromeless).area, state.pixelScale)
}

// setDesktopChromeless turns a session's bars off or on, from its tab's menu: the
// choice is the host's, so it is written there for the next connection, and every
// session of the host that is open now is brought into step.
func (state *State) setDesktopChromeless(session *Session, chromeless bool) {
	if session.desk == nil || session.host.Chromeless == chromeless {
		return
	}
	for index := range state.hosts {
		if state.hosts[index].ID == session.HostID {
			state.hosts[index].Chromeless = chromeless
		}
	}
	for index := range state.sessions {
		other := &state.sessions[index]
		if other.HostID != session.HostID {
			continue
		}
		// The host copy is written even for a session with no view yet (one
		// waiting at the VNC password prompt), so its openDesktop follows.
		other.host.Chromeless = chromeless
		if other.desk != nil {
			other.desk.chromeless = chromeless
		}
	}
	state.save()
}

// followDesktop keeps a server that can change its desktop's size the size of
// the pane. The pane has to hold a size for desktopResizeDelay before the server
// is asked, and a frame is requested for the end of the wait, so a settled pane
// costs nothing and a dragged window asks once.
func (state *State) followDesktop(canvas *gfx.Canvas, view *desktopView) {
	if view.resizer == nil || view.ended || !view.active || !view.fit || view.width <= 0 || !view.resizer.CanResize() {
		return
	}
	if view.asked == (desktopTarget{}) {
		view.asked = desktopTarget{width: view.width, height: view.height, scale: view.pixelScale}
	}
	target := desktopTargetFor(view.area, view.pixelScale)
	if target.width <= 0 || target.height <= 0 || target == view.asked {
		view.settling = false
		return
	}
	now := canvas.Now()
	if !view.settling || target != view.pending {
		view.pending, view.pendingSince, view.settling = target, now, true
	}
	if wait := view.pendingSince + desktopResizeDelay - now; wait > 0 {
		canvas.RequestFrameIn(wait)
		return
	}
	view.settling = false
	view.asked = target
	_ = view.resizer.Resize(target.width, target.height, target.scale)
}

// desktopStripLeft is the left side of the status line: the remote picture's size
// and the scale it is shown at, and what protects the connection.
func (state *State) desktopStripLeft(view *desktopView, session *Session) string {
	m := state.messages.Desktop
	var parts []string
	if view.width > 0 {
		size := fmt.Sprintf(m.StripSizeFmt, view.width, view.height)
		_, scale := view.place(view.area)
		if view.oneToOne() {
			scale *= max(view.pixelScale, 1) // a picture drawn pixel for pixel is at 100%
		}
		if percent := int(math.Round(float64(scale) * 100)); percent != 100 {
			size += fmt.Sprintf(" · %d%%", percent)
		}
		parts = append(parts, size)
	}
	switch {
	case session.host.tunneled() || session.desktopTunnel:
		parts = append(parts, m.StripEncrypted)
	case view.session.Info().Encrypted:
		if session.host.Protocol == protocolVNC {
			parts = append(parts, m.StripVNCEncrypted)
		} else {
			parts = append(parts, m.StripTLS)
		}
	default:
		parts = append(parts, m.StripPlain)
	}
	if view.clipboard {
		parts = append(parts, m.StripClipboard)
	}
	return strings.Join(parts, " · ")
}

// hostSubtitle is the line under a host's name in the list: the account it signs
// in as, or for a VNC host that signs in to nothing, the protocol.
func (state *State) hostSubtitle(host Host) string {
	switch {
	case host.Protocol == protocolRDP:
		if host.User == "" {
			return state.messages.Desktop.ProtocolRDP
		}
		return host.User
	case host.isDesktop() && !host.ViaSSH && host.User == "":
		return state.messages.Desktop.ProtocolVNC
	}
	return state.login(host)
}

// rdpCertificateDescription is the line the host-key dialog opens with when it
// asks about an RDP server's certificate: that it is new, or, more loudly, that it
// is not the one the user trusted.
func (state *State) rdpCertificateDescription(key sshconn.HostKeyInfo) string {
	m := state.messages.Desktop
	if len(key.Known) > 0 {
		return fmt.Sprintf(m.CertChangedFmt, key.Address, key.Known[0])
	}
	return fmt.Sprintf(m.CertNewFmt, key.Address)
}

func (state *State) vncKeyDescription(key sshconn.HostKeyInfo) string {
	m := state.messages.Desktop
	if len(key.Known) > 0 {
		return fmt.Sprintf(m.VNCKeyChangedFmt, key.Address, key.Known[0])
	}
	return fmt.Sprintf(m.VNCKeyNewFmt, key.Address)
}
