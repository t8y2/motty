package workbench

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/desktop"
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
	surface   ui.Surface
	clipboard bool
	// fit scales the picture to the pane; off shows it at its own size, panning
	// with the pointer when it is larger than the pane.
	fit bool
	// released hands the keyboard back to Motty (its shortcuts work, nothing is
	// sent) until the escape chord takes it again.
	released bool
	ended    bool
	active   bool
	pointer  ui.Point // the pointer in page coordinates, last seen over the picture
	buttons  desktop.Buttons
	// held is the keys pressed on the remote side and not yet released, so a
	// release is sent only for a press that was.
	held     []heldKey
	wheel    ui.Point
	sentClip string
	// width and height are the picture's size as last presented.
	width, height int
	// resizer is the session when its server can change the picture's size to
	// follow the pane, nil for one that cannot (VNC).
	resizer desktop.Resizer
	// pixelScale is the device pixels per logical pixel as last painted, and area
	// where the picture was shown, in the page's coordinates.
	pixelScale float32
	area       ui.Rect
	// asked is the size and scale the server was last given, or had when the
	// picture first came; pending is a different one the pane has taken, waiting
	// since pendingSince to be sure it stays (see desktopResizeDelay).
	asked, pending desktopTarget
	pendingSince   time.Duration
	settling       bool
}

// desktopTarget is the desktop a pane wants of a server that follows it: as
// many pixels as the pane has, and the scale of the display they are on.
type desktopTarget struct {
	width, height int
	scale         float32
}

// desktopTargetFor is the desktop that fills area exactly at a display scale.
// The width is even, as a server's display channel needs it.
func desktopTargetFor(area ui.Rect, scale float32) desktopTarget {
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
	return view
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

func (view *desktopView) close() {
	if view.session != nil {
		view.session.Close()
	}
}

// present brings the picture up to date and reports whether it changed.
func (view *desktopView) present() bool {
	changed := view.session.Present(&view.surface)
	if changed {
		view.width, view.height = view.surface.Size()
	}
	return changed
}

// desktopGeometry is where a remote-desktop page puts things, in the page's own
// coordinates: the bar under the window bar, the area the picture is shown in,
// and the status line under it. Painting and input both use it.
type desktopGeometry struct {
	area  ui.Rect
	strip ui.Rect
}

func desktopLayout(size ui.Size) desktopGeometry {
	top := windowBarHeight + infoBarHeight
	height := max(size.Height-top-desktopStripHeight, 0)
	return desktopGeometry{
		area:  ui.Rect{Y: top, Width: size.Width, Height: height},
		strip: ui.Rect{Y: top + height, Width: size.Width, Height: min(desktopStripHeight, max(size.Height-top, 0))},
	}
}

// place says where the picture sits in area and at what scale (logical pixels
// per picture pixel). A picture shown pixel for pixel starts at the top left of
// the area, as the window of a remote machine does, and sits on a device pixel.
func (view *desktopView) place(area ui.Rect) (origin ui.Point, scale float32) {
	width, height := float32(view.width), float32(view.height)
	if width <= 0 || height <= 0 || area.Width <= 0 || area.Height <= 0 {
		return ui.Point{X: area.X, Y: area.Y}, 1
	}
	pixelPerfect := view.pixelPerfect()
	scale = 1
	switch {
	case pixelPerfect:
		scale = view.logicalPerPixel()
	case view.fit:
		scale = min(area.Width/width, area.Height/height, desktopMaxScale)
	}
	shownWidth, shownHeight := width*scale, height*scale
	axis := func(start, room, shown, pointer float32) float32 {
		if pixelPerfect && view.fit {
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
	origin = ui.Point{
		X: axis(area.X, area.Width, shownWidth, view.pointer.X),
		Y: axis(area.Y, area.Height, shownHeight, view.pointer.Y),
	}
	if pixelPerfect && view.pixelScale > 0 {
		snap := func(value float32) float32 {
			return float32(math.Floor(float64(value)*float64(view.pixelScale)+0.5)) / view.pixelScale
		}
		origin = ui.Point{X: snap(origin.X), Y: snap(origin.Y)}
	}
	return origin, scale
}

// remote converts a page point to a pixel of the remote picture, clamped to it.
func (view *desktopView) remote(area ui.Rect, x, y float32) (int, int) {
	origin, scale := view.place(area)
	rx := int(math.Floor(float64((x - origin.X) / scale)))
	ry := int(math.Floor(float64((y - origin.Y) / scale)))
	return min(max(rx, 0), max(view.width-1, 0)), min(max(ry, 0), max(view.height-1, 0))
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
	if text, ok := view.session.RemoteClipboard(); ok && view.clipboard {
		view.sentClip = text // what the remote has: not to be sent back
		if err := state.options.SetClipboardText(text); err != nil {
			state.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		}
	}
}

// syncClipboard gives the remote the local clipboard if it changed since the
// last time, for a host that shares it.
func (state *State) syncClipboard(view *desktopView) {
	if !view.clipboard || view.ended {
		return
	}
	text, err := state.options.ClipboardText()
	if err != nil || text == "" || text == view.sentClip {
		return
	}
	view.sentClip = text
	_ = view.session.SetClipboard(text)
}

// heldKey is a key the remote side has down: the local key that is holding it
// (Key and Text identify it from press to release) and what was pressed there.
type heldKey struct {
	local  ui.Key
	text   string
	remote desktop.KeyEvent
}

// remoteModifier is the remote modifier a local modifier key stands for.
func remoteModifier(key ui.Key) (desktop.Key, bool) {
	switch key {
	case ui.KeyShiftLeft, ui.KeyShiftRight:
		return desktop.KeyShift, true
	case ui.KeyControlLeft, ui.KeyControlRight:
		return desktop.KeyControl, true
	case ui.KeyAltLeft, ui.KeyAltRight:
		return desktop.KeyAlt, true
	case ui.KeyMetaLeft, ui.KeyMetaRight:
		return desktop.KeyMeta, true
	}
	return desktop.KeyNone, false
}

func (view *desktopView) holding(local ui.Key, text string) int {
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

// press sends a key going down once; a key that is already down is the system's
// repeat, which the remote makes for itself while it holds the key.
func (view *desktopView) press(local ui.Key, text string, event desktop.KeyEvent) {
	if view.holding(local, text) >= 0 {
		return
	}
	view.held = append(view.held, heldKey{local: local, text: text, remote: event})
	event.Down = true
	_ = view.session.SendKey(event)
}

// release sends the end of a key that press sent, and nothing for any other.
func (view *desktopView) release(local ui.Key, text string) {
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

var desktopKeys = map[ui.Key]desktop.Key{
	ui.KeyTab: desktop.KeyTab, ui.KeyEnter: desktop.KeyEnter, ui.KeyEscape: desktop.KeyEscape,
	ui.KeyLeft: desktop.KeyLeft, ui.KeyRight: desktop.KeyRight, ui.KeyUp: desktop.KeyUp, ui.KeyDown: desktop.KeyDown,
	ui.KeyHome: desktop.KeyHome, ui.KeyEnd: desktop.KeyEnd, ui.KeyBackspace: desktop.KeyBackspace,
	ui.KeyDelete: desktop.KeyDelete, ui.KeyPageUp: desktop.KeyPageUp, ui.KeyPageDown: desktop.KeyPageDown,
	ui.KeyInsert: desktop.KeyInsert,
}

// desktopKeyEvent maps a Viva key event to the remote key it means.
func desktopKeyEvent(event ui.KeyEvent) (desktop.KeyEvent, bool) {
	switch {
	case event.Key >= ui.KeyF1 && event.Key <= ui.KeyF12:
		return desktop.KeyEvent{Key: desktop.KeyF1 + desktop.Key(event.Key-ui.KeyF1)}, true
	case event.Key == ui.KeySpace:
		return desktop.KeyEvent{Rune: ' '}, true
	case event.Key != ui.KeyUnknown:
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
func (state *State) focusedDesktop(target ui.ID) (*Session, *desktopView) {
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

func isEscapeChord(event ui.KeyEvent) bool {
	return event.Modifiers == ui.ModifierControl|ui.ModifierAlt && strings.EqualFold(event.Text, "g")
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

// desktopShortcut keeps Motty's own shortcuts from running while the keyboard
// belongs to the remote desktop. Only the escape chord is Motty's then.
func (state *State) desktopShortcut(view *desktopView, event ui.KeyEvent) ui.EventResult {
	if isEscapeChord(event) {
		if !event.Repeat {
			state.toggleCapture(view)
		}
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

// desktopKey sends a raw key to the remote desktop.
func (state *State) desktopKey(session *Session, event ui.KeyEvent) ui.EventResult {
	view := session.desk
	if isEscapeChord(event) {
		return state.desktopShortcut(view, event)
	}
	if view.ended {
		if event.Key == ui.KeyEnter && !event.Repeat && state.canReconnect(session) {
			state.reconnect(session)
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return ui.EventResult{Handled: true}
	}
	if view.released {
		return ui.EventResult{}
	}
	if modifier, ok := remoteModifier(event.Key); ok {
		if event.Release {
			view.release(event.Key, event.Text)
		} else {
			view.press(event.Key, event.Text, desktop.KeyEvent{Key: modifier})
		}
		return ui.EventResult{Handled: true}
	}
	if event.Release {
		view.release(event.Key, event.Text)
		return ui.EventResult{Handled: true}
	}
	if key, ok := desktopKeyEvent(event); ok {
		if event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 && key.Rune == 'v' {
			state.syncClipboard(view)
		}
		view.press(event.Key, event.Text, key)
	}
	return ui.EventResult{Handled: true}
}

// desktopPointer sends the pointer to the remote desktop.
func (state *State) desktopPointer(session *Session, event ui.PointerEvent) ui.EventResult {
	view := session.desk
	if view.ended || event.Kind == ui.PointerExit {
		return ui.EventResult{}
	}
	geometry := desktopLayout(event.Viewport)
	panned := false
	if !view.fit {
		before := view.pointer
		view.pointer = ui.Point{X: event.X, Y: event.Y}
		panned = view.pointer != before
	}
	x, y := view.remote(geometry.area, event.X, event.Y)
	var button desktop.Buttons
	switch event.Button {
	case ui.PointerButtonPrimary:
		button = desktop.ButtonLeft
	case ui.PointerButtonMiddle:
		button = desktop.ButtonMiddle
	case ui.PointerButtonSecondary:
		button = desktop.ButtonRight
	}
	switch event.Kind {
	case ui.PointerDown:
		state.syncClipboard(view)
		view.buttons |= button
	case ui.PointerUp:
		view.buttons &^= button
	}
	_ = view.session.SendPointer(x, y, view.buttons)
	return ui.EventResult{Handled: event.Kind != ui.PointerMove, Repaint: panned && view.width > 0}
}

// desktopScroll turns scrolling into wheel notches.
func (state *State) desktopScroll(session *Session, event ui.ScrollEvent) ui.EventResult {
	view := session.desk
	if view.ended {
		return ui.EventResult{Handled: true}
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
		x, y := view.remote(desktopLayout(event.Viewport).area, event.X, event.Y)
		_ = view.session.SendWheel(x, y, wheel, view.buttons)
	}
	return ui.EventResult{Handled: true}
}

// desktopTextInput is the native text surface of a focused desktop: what an input
// method commits, or a paste, is typed on the remote side. The input method's
// window opens at the pointer.
func (state *State) desktopTextInput(session *Session) *app.TextInput {
	view, t := session.desk, session.term
	t.input.Bounds = ui.Rect{X: view.pointer.X, Y: view.pointer.Y, Width: 2, Height: 18}
	t.input.FontSize = 14
	t.input.Color = state.pal.text
	t.input.State = t.preedit
	t.input.AltSendsEscape = true
	t.input.KeyReleases = true
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
	case app.TextPaste:
		if typing {
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
func (state *State) clickDesktop(id ui.ID) bool {
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

// paintDesktopPage draws a remote desktop: a bar with its name and state and the
// scaling and keyboard controls, the picture, and a status line.
func (state *State) paintDesktopPage(canvas *ui.Canvas, size ui.Size, session *Session) {
	pal, m := state.pal, state.messages
	view := session.desk
	view.present()
	view.pixelScale = canvas.PixelScale()
	geometry := desktopLayout(size)
	view.area = geometry.area
	state.followDesktop(canvas, view)
	title := session.Title
	if title == "" {
		title = session.Address
	}
	status, dot := state.sessionStatus(session)
	showStatus := session.phase != sshconn.PhaseConnected || view.ended

	// The controls on the right: the scaling choice, Ctrl+Alt+Del where there is
	// room, and the keyboard switch.
	tools := []components.ButtonAction{
		{ID: hitDesktopFit, Label: m.Desktop.Fit, Variant: components.ButtonGhost},
		{ID: hitDesktopActual, Label: m.Desktop.Actual, Variant: components.ButtonGhost},
	}
	if view.fit {
		tools[0].Variant = components.ButtonSecondary
	} else {
		tools[1].Variant = components.ButtonSecondary
	}
	if size.Width >= 620 {
		tools = append([]components.ButtonAction{{ID: hitDesktopCtrlAltDel, Label: m.Desktop.CtrlAltDel, Variant: components.ButtonGhost, Disabled: view.ended}}, tools...)
	}
	toolsWidth := float32(0)
	for _, tool := range tools {
		toolsWidth += canvas.TextWidthWeight(tool.Label, metrics.FontSizeS, ui.WeightMedium) + 24 + 4
	}

	var light, toolsSlot, captureSlot flex.Box
	f := state.terminalPageLayout.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().PadSides(windowBarHeight, 0, 0, 0).Children(func() {
		f.Stack().Height(infoBarHeight).Shrink(0).Background(pal.workspace).Children(func() {
			f.Row().Fill().PadSides(0, termPadX, 0, termPadX-2).Items(flex.Center).Children(func() {
				f.Row().Grow(1).Gap(8).MarginSides(0, 10, 0, 0).Items(flex.Center).Children(func() {
					light = f.Slot().Size(16, 16)
					f.Text(title).FontSize(barTitleSize).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22).
						Width(canvas.TextWidthWeight(title, barTitleSize, ui.WeightSemibold)+2).Ellipsis().MarginSides(0, 4, 0, 0)
					if showStatus {
						f.Text(status).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(22).
							Width(canvas.TextWidthWeight(status, metrics.FontSizeS, ui.WeightMedium) + 2)
					}
				})
				f.Row().Gap(barToolGap).Shrink(0).Items(flex.Center).Children(func() {
					toolsSlot = f.Slot().Size(toolsWidth, 24).Shrink(0)
					captureSlot = f.Slot().Size(barToolSize, barToolSize).Shrink(0)
				})
			})
			f.Row().FillWidth().Height(1).Self(flex.End).Background(pal.line)
		})
		f.Row().Grow(1).Background(state.ground(state.termPal.terminal))
		f.Stack().Height(desktopStripHeight).Shrink(0).Background(pal.workspace).Children(func() {
			f.Row().FillWidth().Height(1).Self(flex.Start).Background(pal.line)
			f.Row().Fill().PadX(termPadX).Items(flex.Center).Children(func() {
				f.Text(state.desktopStripLeft(view, session)).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.faint).LineHeight(16).Grow(1).Ellipsis()
				chordText := m.Desktop.StripCaptured
				colour := pal.muted
				if view.released {
					chordText, colour = m.Desktop.StripReleased, pal.accent
				}
				chordText = fmt.Sprintf(chordText, desktopEscapeChord)
				f.Text(chordText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(colour).LineHeight(16).
					Width(canvas.TextWidthWeight(chordText, metrics.FontSizeS, ui.WeightMedium) + 2).Shrink(0)
			})
		})
	})
	f.End()

	if session.busy {
		components.PaintSpinner(canvas, state.Theme(), light.Rect(), components.SpinnerPhase(canvas.Now()/spinnerStep))
		canvas.RequestFrameIn(spinnerStep)
	} else {
		kind, _ := state.sessionLight(session)
		spot := light.Rect()
		paintLight(canvas, spot.X+spot.Width/2, spot.Y+spot.Height/2, kind, dot, pal.workspace)
	}
	components.BeginButtonRow(canvas, state.theme, toolsSlot.Rect()).Size(components.ButtonSizeXSmall).Paint(tools...)
	captureStyle, captureHint := iconButtonActive, fmt.Sprintf(m.Desktop.TipCapture, desktopEscapeChord)
	if view.released {
		captureStyle, captureHint = iconButtonQuiet, fmt.Sprintf(m.Desktop.TipRelease, desktopEscapeChord)
	}
	state.paintIconButton(canvas, hitDesktopCapture, iconKeyboard, captureSlot.Rect(), captureHint, view.ended, captureStyle, pal.workspace)

	// The picture, clipped to its area. The area behind it is the terminal's
	// ground, so a picture that does not fill the pane sits on a quiet surface.
	area := geometry.area
	canvas.Clip(area)
	if view.width > 0 && view.height > 0 {
		origin, scale := view.place(area)
		if view.pixelPerfect() {
			view.surface.DrawPixels(canvas, origin)
		} else {
			view.surface.Draw(canvas, ui.Rect{X: origin.X, Y: origin.Y, Width: float32(view.width) * scale, Height: float32(view.height) * scale})
		}
	}
	if view.ended {
		state.paintDesktopEnded(canvas, area, session)
	}
	canvas.Unclip()

	name := fmt.Sprintf(m.Desktop.AccessibleFmt, session.Address)
	canvas.HitWithOptions(hitTerminalList.At(int(session.ID)), area, name, ui.HitOptions{
		Role: ui.RoleTerminal, Autofocus: session.ID == state.activeSessionID && !view.ended, Cursor: ui.PointerCursorCrosshair,
	})
}

// desktopTargetFor is the desktop a session's pane can show pixel for pixel: the
// picture area of the page it is on, at the display scale last painted.
func (state *State) desktopTargetFor(session *Session) desktopTarget {
	size := state.viewport
	if workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID)); ok {
		if fraction, _, ok := state.paneFractionBounds(workspaceID, paneID); ok {
			size = paneVirtualSize(pixelBounds(state.workspacePixelBoundsFor(workspaceID, state.viewport), fraction))
		}
	}
	return desktopTargetFor(desktopLayout(size).area, state.pixelScale)
}

// followDesktop keeps a server that can change its desktop's size the size of
// the pane. The pane has to hold a size for desktopResizeDelay before the server
// is asked, and a frame is requested for the end of the wait, so a settled pane
// costs nothing and a dragged window asks once.
func (state *State) followDesktop(canvas *ui.Canvas, view *desktopView) {
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
		if view.pixelPerfect() {
			scale *= max(view.pixelScale, 1) // a picture drawn pixel for pixel is at 100%
		}
		if percent := int(math.Round(float64(scale) * 100)); percent != 100 {
			size += fmt.Sprintf(" · %d%%", percent)
		}
		parts = append(parts, size)
	}
	switch {
	case session.host.tunneled():
		parts = append(parts, m.StripEncrypted)
	case view.session.Info().Encrypted:
		parts = append(parts, m.StripTLS)
	default:
		parts = append(parts, m.StripPlain)
	}
	if view.clipboard {
		parts = append(parts, m.StripClipboard)
	}
	return strings.Join(parts, " · ")
}

// paintDesktopEnded dims the last picture and says the connection ended, with the
// way to start it again.
func (state *State) paintDesktopEnded(canvas *ui.Canvas, area ui.Rect, session *Session) {
	pal, m := state.pal, state.messages
	canvas.Fill(area, withAlpha(pal.shell, 0.78))
	width := min(max(area.Width-32, 120), 420)
	x := area.X + (area.Width-width)/2
	y := area.Y + max((area.Height-132)/2, 8)
	canvas.Label(m.Desktop.Disconnected, ui.Rect{X: x, Y: y, Width: width, Height: 24}, ui.TextStyle{Size: 16, Weight: ui.WeightSemibold, Color: pal.text})
	reason := state.failureLine(session)
	if session.err != nil && reason == "" {
		reason = session.err.Error()
	}
	if reason != "" {
		canvas.Label(fmt.Sprintf(m.Desktop.DisconnectedFmt, reason), ui.Rect{X: x, Y: y + 30, Width: width, Height: 40}, ui.TextStyle{Size: metrics.FontSizeS, Color: pal.muted})
	}
	if state.canReconnect(session) {
		components.BeginButtonRow(canvas, state.theme, ui.Rect{X: x, Y: y + 78, Width: width, Height: 28}).Paint(
			components.ButtonAction{ID: hitDesktopReconnect, Label: m.Desktop.Reconnect})
		canvas.Label(m.Desktop.ReconnectHint, ui.Rect{X: x, Y: y + 112, Width: width, Height: 18}, ui.TextStyle{Size: metrics.FontSizeS, Color: pal.faint})
	}
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
	case host.isDesktop() && !host.ViaSSH:
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
